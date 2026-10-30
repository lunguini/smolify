package app

import (
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/smolify/internal/player"
)

func startWindowedMedia(t *testing.T) (*flatest.Driver[State], *fake, flatte.App[State]) {
	t.Helper()
	f := &fake{playlists: library, status: player.Status{Volume: 40, VolumeSteps: 100, Paused: true}, listings: map[string]player.Listing{"spotify:playlist:a": bassHouse}}
	a := New(f, &covers{}, f, nil)
	if a.NowPlaying == nil {
		t.Fatal("terminal app did not supply media metadata")
	}
	a = Windowed(a)
	d := flatest.Start(a, 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	event(d, player.Ready{})
	f.calls = nil
	return d, f, a
}

func TestWindowedNowPlayingUsesConfirmedTrackAndClearsAfterReset(t *testing.T) {
	d, _, app := startWindowedMedia(t)
	s := d.State()
	if app.NowPlaying == nil {
		t.Fatal("Windowed did not opt into native media callbacks")
	}
	if got := app.NowPlaying(s); got != nil {
		t.Fatalf("metadata before a confirmed track: %+v", got)
	}
	confirmed := player.Track{
		URI: "spotify:track:confirmed", Name: "Song", Artists: []string{"Artist A", "Artist B"},
		Album: "Record", Duration: 220 * time.Second,
	}
	event(d, confirmed)
	if got := app.NowPlaying(s); got != nil {
		t.Fatalf("candidate Track metadata published before Playing: %+v", got)
	}
	event(d, player.Playing{URI: confirmed.URI, ContextURI: "spotify:playlist:a"})
	event(d, player.Seek{Position: 42 * time.Second})
	got := app.NowPlaying(s)
	if got == nil || got.Title != "Song" || got.Artist != "Artist A, Artist B" || got.Album != "Record" || got.Duration != confirmed.Duration || got.Position != s.position || !got.Playing || !got.CanNext || !got.CanPrevious {
		t.Fatalf("now playing metadata %+v", got)
	}
	event(d, player.Stopped{})
	if got := app.NowPlaying(s); got != nil {
		t.Fatalf("metadata survived Stopped: %+v", got)
	}
	resetAccount(s)
	if got := app.NowPlaying(s); got != nil {
		t.Fatalf("metadata survived account reset: %+v", got)
	}
}

func TestMediaCommandsAreIdempotentAndRequireConfirmedTrack(t *testing.T) {
	d, f, _ := startWindowedMedia(t)
	s := d.State()
	media := func(command flatte.MediaCommand) {
		t.Helper()
		Handle(s, flatte.MediaEvent{Command: command}, s.fx)
		d.Settle()
	}

	media(flatte.MediaTogglePlayPause)
	if len(f.calls) != 0 {
		t.Fatalf("media toggle without a track started playback: %v", f.calls)
	}
	event(d, player.Track{URI: "spotify:track:confirmed", Name: "Song"})
	media(flatte.MediaTogglePlayPause)
	media(flatte.MediaNext)
	if len(f.calls) != 0 {
		t.Fatalf("candidate track accepted media commands before Playing: %v", f.calls)
	}
	event(d, player.Playing{URI: "spotify:track:confirmed"})
	media(flatte.MediaPlay) // already playing
	if len(f.calls) != 0 {
		t.Fatalf("idempotent Play sent a command: %v", f.calls)
	}
	media(flatte.MediaPause)
	if nowPlaying(s) == nil || !nowPlaying(s).Playing {
		t.Fatal("optimistic Pause changed backend-confirmed media state")
	}
	media(flatte.MediaPause) // already paused
	event(d, player.Paused{URI: "spotify:track:confirmed"})
	media(flatte.MediaPlay)
	if nowPlaying(s) == nil || nowPlaying(s).Playing {
		t.Fatal("optimistic Play changed backend-confirmed media state")
	}
	event(d, player.Playing{URI: "spotify:track:confirmed"})
	media(flatte.MediaTogglePlayPause)
	event(d, player.Paused{URI: "spotify:track:confirmed"})
	media(flatte.MediaNext)
	media(flatte.MediaPrevious)
	want := []string{"playpause", "playpause", "playpause", "next", "prev"}
	if len(f.calls) != len(want) {
		t.Fatalf("commands %v, want %v", f.calls, want)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Fatalf("commands %v, want %v", f.calls, want)
		}
	}
	s.pending = &playIntent{track: player.Track{URI: "spotify:track:pending"}}
	media(flatte.MediaNext)
	if len(f.calls) != len(want) {
		t.Fatalf("Next interrupted pending playback: %v", f.calls)
	}
}
