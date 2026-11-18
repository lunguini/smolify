//go:build !js || !wasm

package webspotify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lunguini/smolify/internal/model"
)

// fakeSDK fails the first failures Connect calls and counts attempts.
type fakeSDK struct {
	mu       sync.Mutex
	connects int
	failures int
	delay    time.Duration
	events   chan SDKEvent
}

func (f *fakeSDK) Events() <-chan SDKEvent { return f.events }

func (f *fakeSDK) Connect(context.Context) error {
	f.mu.Lock()
	f.connects++
	attempt := f.connects
	delay := f.delay
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if attempt <= f.failures {
		return errors.New("sdk connect failed")
	}
	return nil
}

func (f *fakeSDK) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects
}

func (f *fakeSDK) Disconnect() error                     { return nil }
func (f *fakeSDK) ActivateElement() error                { return nil }
func (f *fakeSDK) SetName(context.Context, string) error { return nil }

func startClient(t *testing.T, sdk SDK) *Client {
	t.Helper()
	auth := &Auth{Store: &testSessionStore{}}
	c := NewClient(nil, auth, sdk)
	// A failed start leaves its loops unstarted; cancel them either way.
	t.Cleanup(func() {
		c.mu.Lock()
		cancel := c.loopCancel
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	return c
}

// A connection that fails is retried by the next Start, and a connected SDK
// is not connected again.
func TestStartRetriesAfterFailedConnect(t *testing.T) {
	sdk := &fakeSDK{failures: 1, events: make(chan SDKEvent)}
	c := startClient(t, sdk)
	ctx := context.Background()
	if err := c.Start(ctx); err == nil {
		t.Fatal("first Start succeeded with a failing Connect")
	}
	if got := sdk.count(); got != 1 {
		t.Fatalf("connects after a failed Start = %d, want 1", got)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("retry Start: %v", err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("started Start: %v", err)
	}
	if got := sdk.count(); got != 2 {
		t.Fatalf("connects after a retry and a repeat Start = %d, want 2", got)
	}
}

// Two Starts in flight connect once; the second reports the attempt instead
// of opening a second connection.
func TestConcurrentStartsConnectOnce(t *testing.T) {
	sdk := &fakeSDK{delay: 50 * time.Millisecond, events: make(chan SDKEvent)}
	c := startClient(t, sdk)
	ctx := context.Background()
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.Start(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	started := 0
	for err := range errs {
		if err == nil {
			started++
		}
	}
	if got := sdk.count(); got != 1 {
		t.Fatalf("concurrent Starts connected %d times, want 1", got)
	}
	if started != 1 {
		t.Fatalf("concurrent Starts succeeded %d times, want 1", started)
	}
}

// Signing out stops the loops; the next Start reconnects and delivers SDK
// events again.
func TestStartRestartsSDKLoopAfterSignOut(t *testing.T) {
	sdk := &fakeSDK{events: make(chan SDKEvent, 4)}
	c := startClient(t, sdk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SignOut(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start after SignOut: %v", err)
	}
	if got := sdk.count(); got != 2 {
		t.Fatalf("connects after SignOut = %d, want 2", got)
	}
	sdk.events <- SDKEvent{Type: SDKStateChanged, State: SDKState{Track: model.Track{URI: "spotify:track:x"}}}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-c.Events():
			if track, ok := ev.(model.Track); ok && track.URI == "spotify:track:x" {
				return
			}
		case <-deadline:
			t.Fatal("SDK events did not resume after SignOut")
		}
	}
}
