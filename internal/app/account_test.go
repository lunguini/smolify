package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

func TestResetAccountDropsQueuedAccountWork(t *testing.T) {
	d, f, _ := start(t)
	s := d.State()
	s.username = "old"
	s.back = []place{{page: pageHome}}
	s.forward = []place{{page: pageSearch}}
	s.query = flatui.TextField{Value: "old search"}
	s.liked[song.URI] = true
	s.track = &song
	s.context = song.ContextURI
	s.volume = 77
	s.settings.Compact = true
	s.compact = true
	loadLibrary(s)
	refreshStatus(s)
	fetchCover(s, "old-cover")
	f.results = map[string]spotify.Results{"old search": found}
	search(s, 0)
	resetAccount(s)
	d.Settle()
	if s.username != "" || s.loaded || len(s.playlists) != 0 || len(s.listings) != 0 || len(s.liked) != 0 || len(s.covers) != 0 || s.track != nil || s.context != "" || s.volume != 0 || s.query.Value != "" || len(s.back) != 0 || len(s.forward) != 0 {
		t.Fatalf("old account survived reset: user %q loaded %v playlists %v listings %v likes %v covers %v track %v query %q", s.username, s.loaded, s.playlists, s.listings, s.liked, s.covers, s.track, s.query.Value)
	}
	if !s.settings.Compact || !s.compact || s.be != f || s.fx.Ctx() == nil {
		t.Fatal("reset lost app configuration")
	}
	// A new account can load normally after the fence.
	f.status.Username = "new"
	event(d, player.Ready{})
	if s.username != "new" || !s.loaded {
		t.Fatalf("new account did not load: %q %v", s.username, s.loaded)
	}
}

type loadingHome struct {
	*fake
	asked int
}

func (f *loadingHome) ContextTracks(ctx context.Context, uri string) (player.Listing, error) {
	f.asked++
	if f.asked < 2 {
		return player.Listing{URI: uri, Length: 1}, nil
	}
	return player.Listing{URI: uri, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{song}}, nil
}
func TestHomeWaitsForColdTrackDetails(t *testing.T) {
	f := &loadingHome{fake: &fake{recent: []player.Played{{ContextURI: "spotify:playlist:a", TrackURI: song.URI}}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tracks, err := recentTracks(ctx, f, "user")
	if err != nil || len(tracks) != 1 || tracks[0].Name != song.Name || f.asked != 2 {
		t.Fatalf("tracks %+v err %v asked %d", tracks, err, f.asked)
	}
}
func TestHomeWaitHonorsCancellation(t *testing.T) {
	f := &fake{recent: []player.Played{{ContextURI: "cold", TrackURI: song.URI}}, listings: map[string]player.Listing{"cold": {URI: "cold", Length: 1}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := recentTracks(ctx, f, "user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}
func TestWindowNotifiesLibraryFailure(t *testing.T) {
	f := &fake{fail: errors.New("offline")}
	d := flatest.Start(Windowed(New(f, nil, f, nil)), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	event(d, player.Ready{})
	n := d.Notifications()
	if len(n) != 1 || !strings.Contains(n[0].Body, "Could not load the library: offline") {
		t.Fatalf("notifications %+v", n)
	}
}

type resetWeb struct {
	*fake
	resets int
}

func (w *resetWeb) Reset() { w.resets++ }
func TestAccountResetForgetsWebAuthentication(t *testing.T) {
	f := &fake{}
	web := &resetWeb{fake: f}
	d := flatest.Start(New(f, nil, web, nil), 100)
	resetAccount(d.State())
	if web.resets != 1 {
		t.Fatalf("web resets %d", web.resets)
	}
}

type failedSignOut struct{ *fake }

func (f *failedSignOut) SignOut(context.Context) error {
	return errors.New("credentials could not be cleared")
}
func TestFailedSignOutReloadsRecoveredAccount(t *testing.T) {
	f := &failedSignOut{fake: &fake{playlists: library, status: player.Status{Username: "old", VolumeSteps: 100}, listings: map[string]player.Listing{"spotify:playlist:a": bassHouse}}}
	d := flatest.Start(New(f, nil, f, nil), 100)
	event(d, player.Ready{})
	resetAccount(d.State())
	send(d.State(), "sign-out", func(ctx context.Context, be Backend) error { return be.SignOut(ctx) })
	d.Settle()
	s := d.State()
	if !s.ready || s.username != "old" || len(s.playlists) != len(library) {
		t.Fatalf("recovered account not restored: ready %v username %q playlists %v", s.ready, s.username, s.playlists)
	}
}

func TestAccountResetDropsResultReturningAfterReset(t *testing.T) {
	d, _, _ := start(t)
	s := d.State()
	accountGo(s, "old-result", func(context.Context) (string, error) { resetAccount(s); return "old-user", nil }, func(s *State, user string, _ error) { s.username = user })
	d.Settle()
	if s.username != "" {
		t.Fatalf("late result restored %q", s.username)
	}
}

func TestHomeCanFindRepeatedTrackInLaterContext(t *testing.T) {
	f := &fake{recent: []player.Played{{ContextURI: "removed", TrackURI: song.URI}, {ContextURI: "kept", TrackURI: song.URI}}, listings: map[string]player.Listing{"removed": {URI: "removed", Ready: true}, "kept": {URI: "kept", Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{song}}}}
	tracks, err := recentTracks(context.Background(), f, "user")
	if err != nil || len(tracks) != 1 || tracks[0].ContextURI != "kept" {
		t.Fatalf("tracks %+v err %v", tracks, err)
	}
}

func TestHomeKeepsResolvedTracksWhenAnotherContextTimesOut(t *testing.T) {
	f := &fake{recent: []player.Played{{ContextURI: "ready", TrackURI: song.URI}, {ContextURI: "cold", TrackURI: "spotify:track:cold"}}, listings: map[string]player.Listing{"ready": {URI: "ready", Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{song}}, "cold": {URI: "cold", Length: 1}}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	tracks, err := recentTracks(ctx, f, "user")
	if !errors.Is(err, context.DeadlineExceeded) || len(tracks) != 1 || tracks[0].URI != song.URI {
		t.Fatalf("tracks %+v err %v", tracks, err)
	}
}
