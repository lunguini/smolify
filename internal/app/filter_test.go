package app

import (
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/smolify/internal/player"
)

func TestTrackFilterMatchesMetadataWithoutRequests(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	f.calls, f.reads = nil, nil
	for _, query := range []string{"grAvItY", "dua", "FaLl"} {
		changeTrackFilter(s, func(field *flatui.TextField) { *field = flatui.TextField{Value: query, Cursor: len(query)} })
		l := displayListing(s, s.listings[s.shown])
		if len(l.Tracks) != 1 || l.Tracks[0].URI != "spotify:track:2" {
			t.Fatalf("query %q results %v", query, l.Tracks)
		}
	}
	d.Settle()
	if len(f.calls) != 0 || len(f.reads) != 0 {
		t.Fatalf("filter made requests calls=%v reads=%v", f.calls, f.reads)
	}
	if len(s.listings[s.shown].Tracks) != 3 {
		t.Fatal("filter mutated canonical listing")
	}
	for _, uri := range []string{uriHome, uriSearch} {
		l := player.Listing{URI: uri, Tracks: []player.Track{song}}
		if len(displayListing(s, l).Tracks) != 1 {
			t.Fatalf("filter changed %s", uri)
		}
	}
}

func TestTrackFilterSortedSelectionPlaybackAndClear(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	chooseSort(s, sortTitleDesc)
	s.tracks.Select(0)
	changeTrackFilter(s, func(field *flatui.TextField) { field.Value = "a" }) // Gravity and Talk
	if s.tracks.Count() != 2 {
		t.Fatalf("filtered count %d", s.tracks.Count())
	}
	if track, ok := s.selectedTrack(); !ok || track.Name != "Talk" {
		t.Fatalf("selection %+v", track)
	}
	s.tracks.Select(1)
	command(s, cmdPlayTrack, s.fx)
	d.Settle()
	if got := f.calls[len(f.calls)-1]; got != "play spotify:playlist:a spotify:track:2" {
		t.Fatalf("played %s", got)
	}
	changeTrackFilter(s, func(field *flatui.TextField) { *field = flatui.TextField{} })
	if track, ok := s.selectedTrack(); !ok || track.Name != "Gravity" {
		t.Fatalf("clear lost selected track %+v", track)
	}
}

func TestTrackFilterNativeEditingAndFocus(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	f.calls, f.reads = nil, nil
	s.focus = paneTrackFilter
	d.Send(flatte.PasteEvent{Text: "gravitY"})
	if s.trackFilter.Value != "gravitY" || s.tracks.Count() != 1 {
		t.Fatal("paste did not filter")
	}
	d.Send(char('a', flatte.ModSuper))
	if s.trackFilter.SelectedText() != "gravitY" {
		t.Fatal("native select-all missing")
	}
	d.Send(char('x', flatte.ModSuper))
	if s.trackFilter.Value != "" || s.tracks.Count() != 3 {
		t.Fatal("cut failed to clear filter")
	}
	d.Send(flatte.PasteEvent{Text: "unmatched"})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	if s.trackFilter.Value != "" || s.focus != paneTracks || s.trackFilterOpen || d.Quit() {
		t.Fatal("Escape did not clear and collapse filter")
	}
	s.focus = paneTrackFilter
	d.Send(char('s', 0))
	if s.trackFilter.Value != "s" || s.shuffle {
		t.Fatal("typing toggled shuffle")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	if s.focus != paneSort {
		t.Fatal("Tab did not focus sorting")
	}
	s.focus = paneTrackFilter
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if s.focus != paneTracks {
		t.Fatal("Enter did not focus tracks")
	}
	d.Settle()
	if len(f.calls) != 0 || len(f.reads) != 0 {
		t.Fatalf("editing made backend requests: %v %v", f.calls, f.reads)
	}
}

func TestTrackFilterRefreshAndListSwitch(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	changeTrackFilter(s, func(field *flatui.TextField) { field.Value = "new" })
	if s.tracks.Count() != 0 {
		t.Fatal("unexpected initial match")
	}
	s.stale[s.shown] = true
	f.listings[s.shown] = player.Listing{URI: s.shown, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{{URI: "spotify:track:new", Name: "New Song"}}}
	loadTracks(s, s.shown)
	d.Settle()
	if track, ok := s.selectedTrack(); !ok || track.Name != "New Song" {
		t.Fatalf("refresh did not update filtered rows %+v", track)
	}
	show(s, "spotify:playlist:b")
	if strings.TrimSpace(s.trackFilter.Value) != "" {
		t.Fatal("switching playlists kept old filter")
	}
}

func TestTrackFilterContextActionsUseVisibleRows(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	changeTrackFilter(s, func(field *flatui.TextField) { field.Value = "Gravity" })
	s.liked["spotify:track:2"] = true
	menu := ContextMenu(s, idTracks+":0")
	if len(menu) < 4 || menu[3].Label != "Remove from Liked Songs" {
		t.Fatalf("wrong filtered context: %+v", menu)
	}
	f.calls = nil
	d.Send(flatte.MenuEvent{ID: cmdQueue, Target: idTracks + ":0"})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "queue spotify:track:2" {
		t.Fatalf("queued wrong filtered row %v", f.calls)
	}
	d.Send(flatte.MenuEvent{ID: cmdLikeTrack, Target: idTracks + ":0"})
	d.Settle()
	if got := f.calls[len(f.calls)-1]; got != "like spotify:track:2 off" {
		t.Fatalf("liked wrong filtered row %s", got)
	}
}
