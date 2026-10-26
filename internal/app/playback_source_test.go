package app

import (
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
)

func TestPlaybackSourceReturnsToPlayingPlaylistWithoutPlaying(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: library[0].URI})
	s.page = pageHome
	show(s, uriHome)
	s.trackFilter.Value = "hidden filter"
	f.calls = nil
	Handle(s, flatte.MouseEvent{Target: idPlaybackSource, Button: flatte.MouseLeft, Action: flatte.MousePress}, s.fx)
	d.Settle()
	if s.page != pageLibrary || s.shown != library[0].URI || s.trackFilter.Value != "" {
		t.Fatalf("Wrong source: page=%v shown=%q filter=%q", s.page, s.shown, s.trackFilter.Value)
	}
	if track, ok := s.selectedTrack(); !ok || track.URI != song.URI {
		t.Fatalf("Playing track not selected: %+v", track)
	}
	if len(s.back) == 0 || s.back[len(s.back)-1].page != pageHome {
		t.Fatal("Source return lost browsing history")
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "play ") {
			t.Fatalf("Source return restarted playback: %v", f.calls)
		}
	}
	scene := layout.LayoutScene(playerBar(s, true), 1100, 120, linkMetrics)
	button, volume := scene.Rects[idPlaybackSource], scene.Rects[idVolume]
	if button.W == 0 || button.X+button.W > volume.X {
		t.Fatalf("Source button is not beside volume: %+v %+v", button, volume)
	}
}

func TestPlaybackSourceUsesAlbumAndDisablesUnknownContext(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	event(d, song)
	s.context = "spotify:album:original"
	f.listings[s.context] = player.Listing{URI: s.context, Ready: true}
	s.track.AlbumURI = s.context
	s.track.Album = "Original album"
	command(s, cmdPlaybackSource, s.fx)
	d.Settle()
	if s.page != pageBrowse || s.shown != s.context {
		t.Fatal("Album source was not opened")
	}
	s.context = ""
	s.track.ContextURI = ""
	source, _ := playbackSource(s)
	if source.URI != "" {
		t.Fatal("Unknown source was inferred from album metadata")
	}
}

func TestPlaybackSourceClearsFilterOnSamePlaylist(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	playing := bassHouse.Tracks[1]
	event(d, playing)
	event(d, player.Playing{URI: playing.URI, ContextURI: library[0].URI})
	changeTrackFilter(s, func(field *flatui.TextField) { field.Value = "Overthink" })
	showPlaybackSource(s)
	if s.tracks.Count() != len(bassHouse.Tracks) || s.trackFilter.Value != "" {
		t.Fatal("Clearing the filter did not restore the list")
	}
	if selected, ok := s.selectedTrack(); !ok || selected.URI != playing.URI {
		t.Fatalf("Playing track not selected after clearing filter: %+v", selected)
	}
}
