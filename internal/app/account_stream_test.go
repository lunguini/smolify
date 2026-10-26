package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/smolify/internal/player"
)

// streamBackend keeps the real stream channel separate from the synchronous
// fake's mutable request log. Only its channels cross worker boundaries.
type streamBackend struct {
	*fake
	events       chan player.Event
	retired      <-chan struct{}
	signOutCalls chan bool
}

func (b *streamBackend) Events() <-chan player.Event { return b.events }
func (b *streamBackend) SignOut(ctx context.Context) error {
	retired := false
	select {
	case <-b.retired:
		retired = true
	default:
	}
	b.signOutCalls <- retired
	select {
	case b.events <- player.Pairing{Code: "NEW", URL: "https://spotify.com/pair"}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func streamUpdate(t *testing.T, updates <-chan flatte.StateUpdate[State]) flatte.StateUpdate[State] {
	t.Helper()
	select {
	case update := <-updates:
		return update
	case <-time.After(2 * time.Second):
		t.Fatal("player stream update did not arrive")
		return nil
	}
}

func TestPlayerStreamFencesQueuedOldEventAndResumesAfterSignOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan flatte.StateUpdate[State], 8)
	backend := &streamBackend{fake: &fake{}, events: make(chan player.Event, 8), signOutCalls: make(chan bool, 1)}
	s := New(backend, nil, nil, nil).State
	s.fx = flatte.NewHarnessEffects(ctx, updates, nil, nil, nil)
	followPlayer(s)
	backend.retired = s.playerEventsDone
	backend.events <- player.Track{URI: "spotify:track:old", Name: "Old account"}
	oldUpdate := streamUpdate(t, updates)
	resetAccount(s)
	oldUpdate.Apply(s)
	if s.track != nil || s.ready {
		t.Fatal("queued old-account event survived stream cancellation")
	}
	send(s, "sign-out", func(ctx context.Context, be Backend) error { return be.SignOut(ctx) })
	select {
	case retired := <-backend.signOutCalls:
		if !retired {
			t.Fatal("sign-out called before the old stream retired")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sign-out did not run")
	}
	streamUpdate(t, updates).Apply(s) // sign-out completion starts the new stream
	streamUpdate(t, updates).Apply(s) // the new session's pairing event
	if s.pairing.Code != "NEW" || s.track != nil {
		t.Fatalf("new stream state: pairing %+v track %+v", s.pairing, s.track)
	}
	cancel()
	select {
	case <-s.playerEventsDone:
	case <-time.After(2 * time.Second):
		t.Fatal("new stream did not stop on app cancellation")
	}
}

func TestSignOutWaitsForCanceledPlayerSourceToExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan flatte.StateUpdate[State], 8)
	sourceMayStart := make(chan struct{})
	defer func() {
		select {
		case <-sourceMayStart:
		default:
			close(sourceMayStart)
		}
	}()
	var dispatched atomic.Int32
	dispatch := func(work func()) {
		first := dispatched.Add(1) == 1
		go func() {
			if first {
				<-sourceMayStart
			}
			work()
		}()
	}
	backend := &streamBackend{fake: &fake{}, events: make(chan player.Event, 8), signOutCalls: make(chan bool, 1)}
	s := New(backend, nil, nil, nil).State
	s.fx = flatte.NewHarnessEffects(ctx, updates, nil, dispatch, nil)
	followPlayer(s)
	backend.retired = s.playerEventsDone
	resetAccount(s)
	send(s, "sign-out", func(ctx context.Context, be Backend) error { return be.SignOut(ctx) })
	// The stream worker is intentionally not scheduled yet. Cancelling its
	// scope cannot close its done channel until the worker actually exits.
	select {
	case <-backend.signOutCalls:
		t.Fatal("sign-out skipped the source retirement barrier")
	case <-time.After(20 * time.Millisecond):
	}
	close(sourceMayStart)
	select {
	case retired := <-backend.signOutCalls:
		if !retired {
			t.Fatal("sign-out ran before source exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sign-out remained blocked after source exit")
	}
	streamUpdate(t, updates).Apply(s)
	streamUpdate(t, updates).Apply(s)
	if s.pairing.Code != "NEW" {
		t.Fatalf("pairing %+v", s.pairing)
	}
	cancel()
	select {
	case <-s.playerEventsDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stream remained alive on quit")
	}
}
