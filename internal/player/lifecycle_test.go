package player

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devgianlu/go-librespot/daemon"
)

type fakeSession struct {
	server     daemon.ApiServer
	ready      <-chan struct{}
	status     *daemon.ApiStatus
	requests   chan daemon.ApiRequest
	started    chan struct{}
	failStatus bool
	statusSeen chan struct{}
	blockExit  <-chan struct{}
}

func (f *fakeSession) Run(ctx context.Context) error {
	close(f.started)
	select {
	case <-f.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	f.server.Emit(&daemon.ApiEvent{Type: daemon.ApiEventTypePlaybackReady})
	for {
		select {
		case req := <-f.server.Receive():
			if req.Type == daemon.ApiRequestTypeStatus {
				select {
				case f.statusSeen <- struct{}{}:
				default:
				}
				if f.failStatus {
					continue
				}
				req.Reply(f.status, nil)
			} else {
				f.requests <- req
				req.Reply(nil, nil)
			}
		case <-ctx.Done():
			if f.blockExit != nil {
				<-f.blockExit
			}
			return ctx.Err()
		}
	}
}
func readyGate() <-chan struct{} { ch := make(chan struct{}); close(ch); return ch }
func testLifecyclePlayer(t *testing.T, nextReady <-chan struct{}, st *daemon.ApiStatus) (*Player, *fakeSession, <-chan *fakeSession) {
	t.Helper()
	created := make(chan *fakeSession, 10)
	p := &Player{ctx: context.Background(), opts: Options{Dir: t.TempDir()}, events: make(chan Event, 256)}
	n := 0
	p.newApp = func(o *daemon.Options) (sessionApp, error) {
		gate := readyGate()
		if n > 0 {
			gate = nextReady
		}
		n++
		f := &fakeSession{server: o.APIServer, ready: gate, status: st, requests: make(chan daemon.ApiRequest, 30), started: make(chan struct{}), statusSeen: make(chan struct{}, 10)}
		created <- f
		return f, nil
	}
	p.mu.Lock()
	err := p.launch()
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	first := <-created
	<-first.started
	t.Cleanup(p.Close)
	return p, first, created
}
func awaitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation hung")
		return nil
	}
}
func TestConfigureWaitsForReadyAndRestoresPlayback(t *testing.T) {
	gate := make(chan struct{})
	uri := "spotify:album:a"
	st := &daemon.ApiStatus{ContextUri: &uri, Track: &daemon.ApiTrack{Uri: "spotify:track:t", Position: 12000}, Paused: true, Volume: 0, VolumeSteps: 100, ShuffleContext: true, RepeatContext: true, RepeatTrack: true}
	p, _, created := testLifecyclePlayer(t, gate, st)
	done := make(chan error, 1)
	go func() { done <- p.Configure(context.Background(), Audio{Bitrate: 96}) }()
	next := <-created
	<-next.started
	select {
	case err := <-done:
		t.Fatalf("Configure returned before ready: %v", err)
	case req := <-next.requests:
		t.Fatalf("request before ready: %v", req.Type)
	case <-time.After(20 * time.Millisecond):
	}
	close(gate)
	if err := awaitResult(t, done); err != nil {
		t.Fatal(err)
	}
	var got []daemon.ApiRequest
	for len(next.requests) > 0 {
		got = append(got, <-next.requests)
	}
	want := []daemon.ApiRequestType{daemon.ApiRequestTypeSetVolume, daemon.ApiRequestTypePlay, daemon.ApiRequestTypeSetShufflingContext, daemon.ApiRequestTypeSetRepeatingContext, daemon.ApiRequestTypeSetRepeatingTrack}
	if len(got) != len(want) {
		t.Fatalf("requests %+v", got)
	}
	for i, r := range got {
		if r.Type != want[i] {
			t.Fatalf("request %d = %s, want %s", i, r.Type, want[i])
		}
	}
	play := got[1].Data.(daemon.ApiPlay)
	if play.Uri != uri || play.SkipToUri != st.Track.Uri || play.Position != 12000 || !play.Paused {
		t.Fatalf("restored play %+v", play)
	}
	if v := got[0].Data.(daemon.ApiSetVolume).Volume; v != 0 {
		t.Fatalf("mute restored as %d", v)
	}
}
func TestCloseInterruptsConfigureStatusAndReady(t *testing.T) {
	for _, phase := range []string{"status", "ready"} {
		t.Run(phase, func(t *testing.T) {
			p, first, created := testLifecyclePlayer(t, make(chan struct{}), &daemon.ApiStatus{Stopped: true})
			first.failStatus = phase == "status"
			done := make(chan error, 1)
			go func() { done <- p.Configure(context.Background(), Audio{Bitrate: 96}) }()
			if phase == "ready" {
				<-created
			} else {
				<-first.statusSeen
			}
			closed := make(chan error, 1)
			go func() { p.Close(); closed <- nil }()
			awaitResult(t, closed)
			awaitResult(t, done)
		})
	}
}
func TestSignOutInterruptsConfigureAndRecoversCredentialFailure(t *testing.T) {
	p, _, created := testLifecyclePlayer(t, make(chan struct{}), &daemon.ApiStatus{Stopped: true})
	done := make(chan error, 1)
	go func() { done <- p.Configure(context.Background(), Audio{Bitrate: 96}) }()
	<-created
	p.clearCredentials = func(string) error { return errors.New("disk failed") }
	out := make(chan error, 1)
	go func() { out <- p.SignOut(context.Background()) }()
	if err := awaitResult(t, out); err == nil || err.Error() != "disk failed" {
		t.Fatalf("signout %v", err)
	}
	awaitResult(t, done)
	select {
	case <-created:
	default:
		t.Fatal("credential failure left no replacement session")
	}
}
func TestRequestsEndWhenSessionDies(t *testing.T) {
	p, _, _ := testLifecyclePlayer(t, readyGate(), &daemon.ApiStatus{Stopped: true})
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()
	s.cancel()
	if _, err := p.Status(context.Background()); err == nil {
		t.Fatal("dead session status succeeded")
	}
}

func TestConfigureCanceledBeforeStartDoesNotRestart(t *testing.T) {
	p, _, created := testLifecyclePlayer(t, readyGate(), &daemon.ApiStatus{Stopped: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Configure(ctx, Audio{Bitrate: 96}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Configure = %v", err)
	}
	select {
	case <-created:
		t.Fatal("canceled change restarted player")
	default:
	}
}

func TestSessionDeathInterruptsAcceptedRequest(t *testing.T) {
	p, first, _ := testLifecyclePlayer(t, readyGate(), &daemon.ApiStatus{Stopped: true})
	first.failStatus = true
	done := make(chan error, 1)
	go func() { _, err := p.Status(context.Background()); done <- err }()
	<-first.statusSeen
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	session.cancel()
	if err := awaitResult(t, done); err == nil {
		t.Fatal("request to dead session succeeded")
	}
}

func TestConfigureRecoversDeadSession(t *testing.T) {
	p, _, created := testLifecyclePlayer(t, readyGate(), &daemon.ApiStatus{Stopped: true})
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	session.cancel()
	if err := p.Configure(context.Background(), Audio{Bitrate: 96}); err != nil {
		t.Fatal(err)
	}
	<-created
	if _, err := p.Status(context.Background()); err != nil {
		t.Fatalf("replacement session unavailable: %v", err)
	}
}

func TestSignOutDrainsOldEventsAndRejectsLateEvents(t *testing.T) {
	p, first, _ := testLifecyclePlayer(t, readyGate(), &daemon.ApiStatus{Stopped: true})
	if _, err := p.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.server.Emit(&daemon.ApiEvent{Type: daemon.ApiEventTypeMetadata, Data: daemon.ApiEventDataMetadata{Uri: "spotify:track:old", Name: "Old account"}})
	if err := p.SignOut(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.server.Emit(&daemon.ApiEvent{Type: daemon.ApiEventTypeMetadata, Data: daemon.ApiEventDataMetadata{Uri: "spotify:track:late", Name: "Late old account"}})
	if _, err := p.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	for len(p.events) > 0 {
		if e := <-p.events; e != (Ready{}) {
			t.Fatalf("old event survived signout: %+v", e)
		}
	}
}

func TestCancelDuringStopKeepsPlayerRunning(t *testing.T) {
	p, first, created := testLifecyclePlayer(t, readyGate(), &daemon.ApiStatus{Stopped: true})
	exit := make(chan struct{})
	first.blockExit = exit
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Configure(ctx, Audio{Bitrate: 96}) }()
	<-first.statusSeen
	p.mu.Lock()
	old := p.session
	p.mu.Unlock()
	<-old.ctx.Done()
	cancel()
	close(exit)
	if err := awaitResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Configure = %v", err)
	}
	select {
	case <-created:
	default:
		t.Fatal("cancellation during stop left no player")
	}
	if _, err := p.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
}
