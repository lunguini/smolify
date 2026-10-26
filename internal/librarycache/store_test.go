package librarycache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunguini/smolify/internal/player"
)

func TestStoreRoundTripAccountIsolationAndPermissions(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	data := Snapshot{Playlists: []player.Playlist{{URI: "spotify:playlist:a", Name: "A"}}, Listings: map[string]player.Listing{
		"spotify:user:alice:collection": {URI: "spotify:user:alice:collection", Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{{URI: "spotify:track:1", AddedAt: time.Unix(1, 0)}}},
	}}
	if err := s.Save("alice", 0, 1, data); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("alice")
	if err != nil || got.Playlists[0].Name != "A" || got.Listings["spotify:user:alice:collection"].Tracks[0].AddedAt != data.Listings["spotify:user:alice:collection"].Tracks[0].AddedAt {
		t.Fatalf("load snapshot=%+v err=%v", got, err)
	}
	if _, err := s.Load("bob"); !os.IsNotExist(err) {
		t.Fatalf("bob read alice cache: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, accountKey("alice")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode %o, want 600", info.Mode().Perm())
	}
}

func TestStoreCorruptAndWrongAccountCachesAreRejected(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("alice"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("alice"); err == nil {
		t.Fatal("corrupt cache accepted")
	}
	if err := os.WriteFile(s.path("alice"), []byte(`{"version":1,"account":"bob","data":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("alice"); err == nil {
		t.Fatal("cross-account cache accepted")
	}
}

func TestStoreSaveOrderingAndPurgeFence(t *testing.T) {
	s := New(t.TempDir())
	newer := Snapshot{Playlists: []player.Playlist{{Name: "new"}}}
	older := Snapshot{Playlists: []player.Playlist{{Name: "old"}}}
	if err := s.Save("alice", 0, 2, newer); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("alice", 0, 1, older); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("alice")
	if err != nil || got.Playlists[0].Name != "new" {
		t.Fatalf("older save replaced newer: %+v %v", got, err)
	}
	if err := s.Purge("alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("alice", 0, 3, older); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("alice"); !os.IsNotExist(err) {
		t.Fatalf("late write recreated purged cache: %v", err)
	}
}

func TestPreparePurgeFencesBlockedWriteWithoutWaitingForDisk(t *testing.T) {
	s := New(t.TempDir())
	s.writerMu.Lock()
	saveDone := make(chan error, 1)
	go func() { saveDone <- s.Save("alice", 0, 1, Snapshot{Playlists: []player.Playlist{{Name: "old"}}}) }()
	waitFor(t, func() bool {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
		return s.revision["alice"] == 1
	})

	prepareDone := make(chan func() error, 1)
	go func() { prepareDone <- s.PreparePurge("alice") }()
	var cleanup func() error
	select {
	case cleanup = <-prepareDone:
	case <-time.After(time.Second):
		s.writerMu.Unlock()
		t.Fatal("PreparePurge waited for the writer lock")
	}
	if got := s.Generation("alice"); got != 1 {
		s.writerMu.Unlock()
		t.Fatalf("generation after prepare = %d, want 1", got)
	}
	s.writerMu.Unlock()
	if err := <-saveDone; err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("alice"); !os.IsNotExist(err) {
		t.Fatalf("old write survived purge: %v", err)
	}
}

func TestNewGenerationSaveBeforePurgeCleanupSurvives(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Save("alice", 0, 1, Snapshot{Playlists: []player.Playlist{{Name: "old"}}}); err != nil {
		t.Fatal(err)
	}
	cleanup := s.PreparePurge("alice")
	if _, err := s.Load("alice"); !os.IsNotExist(err) {
		t.Fatalf("Load exposed the old snapshot while purge was pending: %v", err)
	}
	if err := s.Save("alice", 1, 1, Snapshot{Playlists: []player.Playlist{{Name: "new"}}}); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("alice")
	if err != nil || len(got.Playlists) != 1 || got.Playlists[0].Name != "new" {
		t.Fatalf("cleanup removed a new-account snapshot: %+v %v", got, err)
	}
}

func waitFor(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(time.Millisecond)
	}
}
