//go:build js && wasm

package librarycache

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/lunguini/smolify/internal/model"
)

const storagePrefix = "smolify.librarycache."

// Store serializes browser localStorage writes and fences operations queued
// before Purge.
type Store struct {
	mu              sync.Mutex
	generation      map[string]uint64
	revision        map[string]uint64
	writtenRevision map[string]uint64
	pendingPurge    map[string]bool
	purgeEpoch      map[string]uint64
	purgeRevision   map[string]uint64
}

func New(string) *Store {
	return &Store{
		generation: map[string]uint64{}, revision: map[string]uint64{},
		writtenRevision: map[string]uint64{}, pendingPurge: map[string]bool{},
		purgeEpoch: map[string]uint64{}, purgeRevision: map[string]uint64{},
	}
}

func (s *Store) key(account string) string { return storagePrefix + accountKey(account) }

func (s *Store) Load(account string) (out Snapshot, err error) {
	if account == "" {
		return out, errors.New("library cache: empty account")
	}
	s.mu.Lock()
	pending := s.pendingPurge[account]
	s.mu.Unlock()
	if pending {
		return out, ErrNotExist
	}
	value, err := storageGet(s.key(account))
	if err != nil {
		return out, err
	}
	if value == "" {
		return out, ErrNotExist
	}
	var d document
	if err := json.Unmarshal([]byte(value), &d); err != nil {
		return out, err
	}
	if d.Version != version || d.Account != account {
		return out, errors.New("library cache: incompatible snapshot")
	}
	if d.Data.Listings == nil {
		d.Data.Listings = map[string]model.Listing{}
	}
	return d.Data, nil
}

func (s *Store) Generation(account string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation[account]
}

func (s *Store) Save(account string, generation, revision uint64, data Snapshot) error {
	if account == "" {
		return errors.New("library cache: empty account")
	}
	s.mu.Lock()
	if s.generation[account] != generation || revision < s.revision[account] {
		s.mu.Unlock()
		return nil
	}
	s.revision[account] = revision
	s.mu.Unlock()
	b, err := json.Marshal(document{Version: version, Account: account, Data: data})
	if err != nil {
		return err
	}
	if err := storageSet(s.key(account), string(b)); err != nil {
		return err
	}
	s.mu.Lock()
	if s.generation[account] == generation && revision > s.writtenRevision[account] {
		s.writtenRevision[account] = revision
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) PreparePurge(account string) func() error {
	if account == "" {
		return func() error { return nil }
	}
	s.mu.Lock()
	s.generation[account]++
	epoch := s.generation[account]
	s.revision[account], s.writtenRevision[account] = 0, 0
	s.pendingPurge[account] = true
	s.purgeEpoch[account], s.purgeRevision[account] = epoch, 0
	s.mu.Unlock()
	return func() error {
		s.mu.Lock()
		current := s.pendingPurge[account] && s.generation[account] == epoch && s.purgeEpoch[account] == epoch
		remove := current && s.writtenRevision[account] == s.purgeRevision[account]
		s.mu.Unlock()
		var err error
		if remove {
			err = storageRemove(s.key(account))
		}
		s.mu.Lock()
		if s.pendingPurge[account] && s.generation[account] == epoch && s.purgeEpoch[account] == epoch {
			s.pendingPurge[account] = false
		}
		s.mu.Unlock()
		return err
	}
}

func (s *Store) Purge(account string) error { return s.PreparePurge(account)() }

func (s *Store) PurgeAll() error {
	storage, err := localStorage()
	if err != nil {
		return err
	}
	for i := storage.Get("length").Int() - 1; i >= 0; i-- {
		key := storage.Call("key", i)
		if key.Type() == js.TypeString && len(key.String()) >= len(storagePrefix) && key.String()[:len(storagePrefix)] == storagePrefix {
			storage.Call("removeItem", key.String())
		}
	}
	s.mu.Lock()
	for account := range s.generation {
		s.generation[account]++
	}
	for account := range s.revision {
		s.revision[account], s.writtenRevision[account] = 0, 0
		s.pendingPurge[account] = false
	}
	s.mu.Unlock()
	return nil
}

func localStorage() (storage js.Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("library cache: localStorage unavailable: %v", recovered)
		}
	}()
	window := js.Global().Get("window")
	if window.IsUndefined() || window.IsNull() {
		return js.Undefined(), errors.New("library cache: browser window unavailable")
	}
	storage = window.Get("localStorage")
	if storage.IsUndefined() || storage.IsNull() {
		return js.Undefined(), errors.New("library cache: localStorage unavailable")
	}
	return storage, nil
}

func storageGet(key string) (value string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("library cache: read localStorage: %v", recovered)
		}
	}()
	storage, err := localStorage()
	if err != nil {
		return "", err
	}
	got := storage.Call("getItem", key)
	if got.IsNull() || got.IsUndefined() {
		return "", nil
	}
	return got.String(), nil
}

func storageSet(key, value string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("library cache: write localStorage: %v", recovered)
		}
	}()
	storage, err := localStorage()
	if err != nil {
		return err
	}
	storage.Call("setItem", key, value)
	return nil
}

func storageRemove(key string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("library cache: clear localStorage: %v", recovered)
		}
	}()
	storage, err := localStorage()
	if err != nil {
		return err
	}
	storage.Call("removeItem", key)
	return nil
}
