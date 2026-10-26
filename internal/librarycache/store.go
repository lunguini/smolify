// Package librarycache stores complete account library snapshots on disk.
package librarycache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lunguini/smolify/internal/player"
)

const version = 1

// Snapshot contains only complete remote listings. Incomplete player metadata
// enumerations are deliberately excluded by the app before Save.
type Snapshot struct {
	Playlists []player.Playlist         `json:"playlists"`
	Listings  map[string]player.Listing `json:"listings"`
}

type document struct {
	Version int      `json:"version"`
	Account string   `json:"account"`
	Data    Snapshot `json:"data"`
}

// Store serializes writes and fences writes queued before Purge.
type Store struct {
	dir             string
	stateMu         sync.Mutex
	writerMu        sync.Mutex
	generation      map[string]uint64
	revision        map[string]uint64 // highest requested revision
	writtenRevision map[string]uint64 // last revision installed on disk
	pendingPurge    map[string]bool
	purgeEpoch      map[string]uint64
	purgeRevision   map[string]uint64
}

func New(dir string) *Store {
	return &Store{
		dir: dir, generation: map[string]uint64{}, revision: map[string]uint64{},
		writtenRevision: map[string]uint64{}, pendingPurge: map[string]bool{},
		purgeEpoch: map[string]uint64{}, purgeRevision: map[string]uint64{},
	}
}

func accountKey(account string) string {
	h := sha256.Sum256([]byte(account))
	return hex.EncodeToString(h[:])
}

func (s *Store) path(account string) string { return filepath.Join(s.dir, accountKey(account)+".json") }

func (s *Store) Load(account string) (Snapshot, error) {
	var out Snapshot
	if account == "" {
		return out, errors.New("library cache: empty account")
	}
	s.stateMu.Lock()
	pending := s.pendingPurge[account]
	s.stateMu.Unlock()
	if pending {
		return out, os.ErrNotExist
	}
	b, err := os.ReadFile(s.path(account))
	if err != nil {
		return out, err
	}
	var d document
	if err := json.Unmarshal(b, &d); err != nil {
		return out, err
	}
	if d.Version != version || d.Account != account {
		return out, errors.New("library cache: incompatible snapshot")
	}
	if d.Data.Listings == nil {
		d.Data.Listings = map[string]player.Listing{}
	}
	return d.Data, nil
}

// Generation identifies the current account cache epoch. A caller captures
// it before starting background work and supplies it to Save.
func (s *Store) Generation(account string) uint64 {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.generation[account]
}

func (s *Store) Save(account string, generation, revision uint64, data Snapshot) error {
	if account == "" {
		return errors.New("library cache: empty account")
	}
	s.stateMu.Lock()
	if s.generation[account] != generation {
		s.stateMu.Unlock()
		return nil
	}
	if revision < s.revision[account] {
		s.stateMu.Unlock()
		return nil
	}
	s.revision[account] = revision
	s.stateMu.Unlock()

	s.writerMu.Lock()
	defer s.writerMu.Unlock()
	s.stateMu.Lock()
	current := s.generation[account] == generation && s.revision[account] == revision
	s.stateMu.Unlock()
	if !current {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(document{Version: version, Account: account, Data: data})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".library-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	current = s.generation[account] == generation && s.revision[account] == revision
	s.stateMu.Unlock()
	if !current {
		return nil
	}
	if err := os.Rename(tmp, s.path(account)); err != nil {
		return fmt.Errorf("library cache: replace snapshot: %w", err)
	}
	s.stateMu.Lock()
	sameGeneration := s.generation[account] == generation
	if sameGeneration && revision > s.writtenRevision[account] {
		// Revision ordering applies to pending snapshots, but an older complete
		// snapshot remains valid if a newer save later fails.
		s.writtenRevision[account] = revision
	}
	s.stateMu.Unlock()
	if !sameGeneration {
		// The writer lock means no newer snapshot can have replaced this path.
		if err := os.Remove(s.path(account)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// PreparePurge fences old writes immediately without waiting for file I/O.
// The returned cleanup can run off the UI loop. A snapshot saved in the new
// generation before cleanup starts is retained.
func (s *Store) PreparePurge(account string) func() error {
	if account == "" {
		return func() error { return nil }
	}
	s.stateMu.Lock()
	s.generation[account]++
	epoch := s.generation[account]
	s.revision[account] = 0
	s.writtenRevision[account] = 0
	s.pendingPurge[account] = true
	s.purgeEpoch[account] = epoch
	s.purgeRevision[account] = 0
	s.stateMu.Unlock()
	return func() error {
		s.writerMu.Lock()
		defer s.writerMu.Unlock()
		s.stateMu.Lock()
		current := s.pendingPurge[account] && s.generation[account] == epoch && s.purgeEpoch[account] == epoch
		remove := current && s.writtenRevision[account] == s.purgeRevision[account]
		s.stateMu.Unlock()
		var err error
		if remove {
			err = os.Remove(s.path(account))
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		s.stateMu.Lock()
		if s.pendingPurge[account] && s.generation[account] == epoch && s.purgeEpoch[account] == epoch {
			s.pendingPurge[account] = false
		}
		s.stateMu.Unlock()
		return err
	}
}

// Purge is the synchronous form used by the CLI and store tests.
func (s *Store) Purge(account string) error {
	return s.PreparePurge(account)()
}

// PurgeAll removes the snapshots in this dedicated cache directory and fences
// all writes already in flight. It is used by the explicit CLI sign-out.
func (s *Store) PurgeAll() error {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()
	s.stateMu.Lock()
	accounts := map[string]struct{}{}
	for account := range s.generation {
		accounts[account] = struct{}{}
	}
	for account := range s.revision {
		accounts[account] = struct{}{}
	}
	for account := range s.writtenRevision {
		accounts[account] = struct{}{}
	}
	for account := range accounts {
		s.generation[account]++
		s.revision[account] = 0
		s.writtenRevision[account] = 0
		s.pendingPurge[account] = true
	}
	s.stateMu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		s.clearAllPurges()
		return nil
	}
	if err != nil {
		s.clearAllPurges()
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.clearAllPurges()
			return err
		}
	}
	s.clearAllPurges()
	return nil
}

func (s *Store) clearAllPurges() {
	s.stateMu.Lock()
	for account := range s.pendingPurge {
		s.pendingPurge[account] = false
	}
	s.stateMu.Unlock()
}
