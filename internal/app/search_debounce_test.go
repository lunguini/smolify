package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/smolify/internal/spotify"
)

// searchRequests is a real-worker backend: channels, rather than the
// synchronous app fake's request log, make observations race-safe.
type searchRequests struct{ queries chan string }

func (w searchRequests) Search(ctx context.Context, query string, _ int) (spotify.Results, error) {
	select {
	case w.queries <- query:
		return spotify.Results{}, nil
	case <-ctx.Done():
		return spotify.Results{}, ctx.Err()
	}
}

func debounceState(t *testing.T) (*State, <-chan string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan flatte.StateUpdate[State], 32)
	queries := make(chan string, 32)
	var workers sync.WaitGroup
	dispatch := func(work func()) {
		workers.Add(1)
		go func() { defer workers.Done(); work() }()
	}
	s := New(&fake{}, nil, searchRequests{queries}, nil).State
	s.fx = flatte.NewHarnessEffects(ctx, updates, nil, dispatch, nil)
	s.focus = paneSearch
	t.Cleanup(func() { cancel(); workers.Wait() })
	return s, queries
}

func debounceType(s *State, text string) {
	for _, r := range text {
		searchKey(s, flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: r}, s.fx)
	}
}

func noSearchBefore(t *testing.T, queries <-chan string, wait time.Duration) {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case q := <-queries:
		t.Fatalf("search %q sent before typing paused", q)
	case <-timer.C:
	}
}

func nextSearch(t *testing.T, queries <-chan string) string {
	t.Helper()
	select {
	case q := <-queries:
		return q
	case <-time.After(2 * time.Second):
		t.Fatal("debounced search never sent")
		return ""
	}
}

func TestSearchDebouncesTypingWithRealWorkers(t *testing.T) {
	s, queries := debounceState(t)
	if s.searchDelay != 300*time.Millisecond {
		t.Fatalf("default debounce %v", s.searchDelay)
	}
	debounceType(s, "d")
	noSearchBefore(t, queries, 100*time.Millisecond)
	debounceType(s, "aft punk")
	// This crosses the first character's original deadline, but remains
	// before the final character's deadline: typing must reset the wait.
	noSearchBefore(t, queries, 240*time.Millisecond)
	if q := nextSearch(t, queries); q != "daft punk" {
		t.Fatalf("searched %q", q)
	}
	noSearchBefore(t, queries, 150*time.Millisecond)
}

func TestSearchEnterBypassesDebounceWithRealWorkers(t *testing.T) {
	s, queries := debounceState(t)
	s.searchDelay = time.Hour
	debounceType(s, "daft punk")
	searchKey(s, flatte.KeyEvent{Key: flatte.KeyEnter}, s.fx)
	if q := nextSearch(t, queries); q != "daft punk" {
		t.Fatalf("Enter searched %q", q)
	}
	noSearchBefore(t, queries, 50*time.Millisecond)
}

func TestClearingSearchCancelsDebounceWithRealWorkers(t *testing.T) {
	s, queries := debounceState(t)
	s.searchDelay = 100 * time.Millisecond
	debounceType(s, "daft punk")
	s.query.SelectAll()
	searchKey(s, flatte.KeyEvent{Key: flatte.KeyBackspace}, s.fx)
	noSearchBefore(t, queries, 200*time.Millisecond)
	if s.query.Value != "" || s.searchPending {
		t.Fatal("cleared search remained pending")
	}
}
