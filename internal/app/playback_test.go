package app

import (
	"context"
	"errors"
	"github.com/lunguini/flatte"
	"github.com/lunguini/smolify/internal/player"
	"testing"
	"time"
)

func TestRequestedTrackStaysPendingUntilMatchingPlayback(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	s.tracks.Select(1)
	command(s, cmdPlayTrack, s.fx)
	if !loadingPlay(s) || !pendingTrack(s, bassHouse.Tracks[1], s.shown) {
		t.Fatal("requested song has no immediate intent")
	}
	if s.track.URI != song.URI {
		t.Fatal("request overwrote confirmed metadata")
	}
	d.Settle()
	if !loadingPlay(s) || len(f.calls) != 1 {
		t.Fatal("command acknowledgement cleared loading")
	}
	event(d, player.Track{URI: "spotify:track:2", Name: "Gravity", ContextURI: s.shown})
	if !loadingPlay(s) {
		t.Fatal("metadata alone cleared loading")
	}
	event(d, player.Playing{URI: "spotify:track:2", ContextURI: s.shown})
	if loadingPlay(s) || !s.playing || s.track.URI != "spotify:track:2" {
		t.Fatal("matching playback did not finish intent")
	}
}

func TestNewPlayIntentRejectsOldAcknowledgementsAndEvents(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	s.tracks.Select(0)
	command(s, cmdPlayTrack, s.fx)
	s.tracks.Select(1)
	command(s, cmdPlayTrack, s.fx)
	for _, ev := range []player.Event{song, player.Playing{URI: song.URI, ContextURI: song.ContextURI}, player.Paused{URI: song.URI}, player.Stopped{}, player.PlaybackError{URI: song.URI, Kind: "old failure"}} {
		event(d, ev)
	}
	if !pendingTrack(s, bassHouse.Tracks[1], s.shown) {
		t.Fatal("old events cleared newer request")
	}
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:playlist:a spotify:track:2" {
		t.Fatalf("obsolete request dispatched: %v", f.calls)
	}
	event(d, player.Playing{URI: "spotify:track:2", ContextURI: s.shown})
	if loadingPlay(s) {
		t.Fatal("matching playback still loading")
	}
}

func TestPendingPlayFailureTimeoutAndReset(t *testing.T) {
	t.Run("request failure", func(t *testing.T) {
		d, f := ready(t)
		s := d.State()
		f.fail = errors.New("offline")
		command(s, cmdPlayTrack, s.fx)
		d.Settle()
		if loadingPlay(s) || s.status != "Could not play: offline" {
			t.Fatalf("pending=%v status=%q", loadingPlay(s), s.status)
		}
	})
	t.Run("matching playback failure", func(t *testing.T) {
		d, _ := ready(t)
		s := d.State()
		command(s, cmdPlayTrack, s.fx)
		d.Settle()
		event(d, player.PlaybackError{URI: song.URI, Kind: "unavailable"})
		if loadingPlay(s) {
			t.Fatal("failed song still loading")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		d, _ := ready(t)
		s := d.State()
		command(s, cmdPlayTrack, s.fx)
		d.Settle()
		before := loadingIndicator(s)
		d.Advance(100 * time.Millisecond)
		if loadingIndicator(s) == before {
			t.Fatal("spinner did not animate")
		}
		d.Advance(30 * time.Second)
		if loadingPlay(s) || s.status == "" {
			t.Fatal("pending song never timed out")
		}
	})
	t.Run("reset", func(t *testing.T) {
		d, _ := ready(t)
		s := d.State()
		command(s, cmdPlayTrack, s.fx)
		resetAccount(s)
		d.Advance(time.Second)
		d.Settle()
		if loadingPlay(s) || s.status != "" {
			t.Fatal("old pending request survived signout")
		}
	})
}

func TestPlayPauseDoesNotDuplicatePendingRequest(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	command(s, cmdPlayTrack, s.fx)
	d.Send(flatte.MenuEvent{ID: cmdPlayPause})
	d.Settle()
	if len(f.calls) != 1 || !loadingPlay(s) {
		t.Fatalf("calls=%v pending=%v", f.calls, loadingPlay(s))
	}
}

func TestPlayPlaylistSelectsCanonicalFirstTrackWhenSorted(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	chooseSort(s, sortTitleAsc)
	s.tracks.Select(0)
	command(s, cmdPlayList, s.fx)
	if !loadingPlaylist(s, s.shown) || !pendingTrack(s, bassHouse.Tracks[0], s.shown) {
		t.Fatal("playlist has no first-track intent")
	}
	if got, _ := s.selectedTrack(); got.URI != bassHouse.Tracks[0].URI {
		t.Fatalf("selected %+v", got)
	}
}

type delayedPlayCall struct{ reply chan error }
type delayedPlayBackend struct {
	*fake
	requests chan delayedPlayCall
}

func (b *delayedPlayBackend) Play(ctx context.Context, uri, skip string) error {
	call := delayedPlayCall{reply: make(chan error, 1)}
	select {
	case b.requests <- call:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-call.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestQueuedOldPlayFailureCannotClearNewRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan flatte.StateUpdate[State], 64)
	be := &delayedPlayBackend{fake: &fake{}, requests: make(chan delayedPlayCall, 2)}
	s := New(be, nil, nil, nil).State
	s.fx = flatte.NewHarnessEffects(ctx, updates, nil, nil, nil)
	defer clearPendingPlay(s)
	getCall := func() delayedPlayCall {
		select {
		case call := <-be.requests:
			return call
		case <-time.After(2 * time.Second):
			t.Fatal("play request not dispatched")
			return delayedPlayCall{}
		}
	}
	getResult := func() flatte.StateUpdate[State] {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case update := <-updates:
				if update.Name() == "play" {
					return update
				}
			case <-timer.C:
				t.Fatal("play result not queued")
				return nil
			}
		}
	}
	requestPlay(s, "spotify:album:a", song.URI, song)
	old := getCall()
	old.reply <- errors.New("older request failed")
	oldResult := getResult()
	next := bassHouse.Tracks[1]
	requestPlay(s, "spotify:album:b", next.URI, next)
	current := s.pending
	second := getCall()
	oldResult.Apply(s)
	if s.pending != current || s.status != "" {
		t.Fatal("queued older failure changed newer request")
	}
	second.reply <- nil
	getResult().Apply(s)
	if s.pending != current {
		t.Fatal("acknowledgement cleared new loading intent")
	}
	onPlayer(s, player.Playing{URI: next.URI, ContextURI: "spotify:album:b"})
	if loadingPlay(s) {
		t.Fatal("matching playback did not confirm")
	}
}

func TestUnknownPlaylistLoadingMatchesItsContext(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.listings[s.shown] = player.Listing{URI: s.shown}
	command(s, cmdPlayList, s.fx)
	d.Settle()
	scope := s.pending.scope
	event(d, player.Playing{URI: "other", ContextURI: "other-context"})
	if !loadingPlaylist(s, s.shown) {
		t.Fatal("unrelated playback cleared playlist loading")
	}
	event(d, player.Playing{URI: "new-song", ContextURI: s.shown})
	if loadingPlay(s) || scope.Ctx().Err() == nil {
		t.Fatal("confirmed playlist left its spinner running")
	}
}

func TestUnknownPlaylistBindsRealWillPlayBeforeMetadataOrFailure(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.listings[s.shown] = player.Listing{URI: s.shown}
	command(s, cmdPlayList, s.fx)
	d.Settle()
	event(d, player.WillPlay{URI: "old", ContextURI: "other-playlist"})
	if s.pending.track.URI != "" {
		t.Fatal("unrelated context bound the pending song")
	}
	event(d, player.WillPlay{URI: "new", ContextURI: s.shown})
	if s.pending.track.URI != "new" {
		t.Fatal("will_play did not bind the song")
	}
	// Real metadata has no context: the typed will_play established attribution.
	event(d, player.Track{URI: "new", Name: "New song"})
	if !loadingPlay(s) || s.pending.track.Name != "New song" {
		t.Fatal("metadata did not enrich loading song")
	}
	event(d, player.PlaybackError{URI: "new", Kind: "unavailable"})
	if loadingPlay(s) {
		t.Fatal("real playback error was ignored")
	}
}

func TestPendingPlayIgnoresDuplicateCommandsButAllowsReplacement(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	command(s, cmdPlayTrack, s.fx)
	d.Settle()
	pending := s.pending
	command(s, cmdPlayTrack, s.fx)
	command(s, cmdPlayList, s.fx)
	command(s, cmdPlayPause, s.fx)
	d.Settle()
	if len(f.calls) != 1 || s.pending != pending {
		t.Fatalf("duplicate request: calls %v pending replaced=%v", f.calls, s.pending != pending)
	}
	s.tracks.Select(1)
	command(s, cmdPlayTrack, s.fx)
	d.Settle()
	if len(f.calls) != 2 || s.pending == pending || !pendingTrack(s, bassHouse.Tracks[1], s.shown) {
		t.Fatal("a different song did not replace the request")
	}
}

func TestUnknownPlayTargetFollowsLatestWillPlay(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.listings[s.shown] = player.Listing{URI: s.shown}
	command(s, cmdPlayList, s.fx)
	event(d, player.WillPlay{URI: "older", ContextURI: s.shown})
	event(d, player.WillPlay{URI: "latest", ContextURI: s.shown})
	if s.pending.track.URI != "latest" {
		t.Fatal("unknown target locked to an earlier will_play")
	}
	event(d, player.Playing{URI: "latest", ContextURI: s.shown})
	if loadingPlay(s) {
		t.Fatal("latest actual playback left spinner running")
	}
}

func TestExplicitPlayTargetRejectsOtherWillPlayAndPreservesBrowsing(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.tracks.Select(1)
	command(s, cmdPlayTrack, s.fx)
	origin := s.shown
	event(d, player.WillPlay{URI: song.URI, ContextURI: origin})
	if s.pending.track.URI != bassHouse.Tracks[1].URI {
		t.Fatal("explicit target changed")
	}
	s.shown = "other-list"
	s.listings[s.shown] = player.Listing{Tracks: []player.Track{song, bassHouse.Tracks[1]}}
	s.tracks.Select(0)
	event(d, player.WillPlay{URI: bassHouse.Tracks[1].URI, ContextURI: origin})
	if s.tracks.Cursor() != 0 {
		t.Fatal("pending play moved selection in unrelated list")
	}
}
