package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/smolify/internal/player"
)

func TestTrackSortStableAndPositionRestoresSourceOrder(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	source := player.Listing{URI: s.shown, Ready: true, Length: 4, Cached: 4, Tracks: []player.Track{
		{URI: "z", Name: "zebra", Artists: []string{"Alpha"}, Album: "Last", Duration: 3 * time.Minute},
		{URI: "missing"},
		{URI: "a", Name: "Alpha", Artists: []string{"Zulu"}, Album: "First", Duration: time.Minute},
		{URI: "a2", Name: "alpha", Artists: []string{"Zulu"}, Album: "First", Duration: 2 * time.Minute},
	}}
	s.listings[s.shown] = source
	for mode, want := range map[int]string{sortPositionAsc: "z,missing,a,a2", sortPositionDesc: "a2,a,missing,z", sortTitleAsc: "a,a2,z,missing", sortTitleDesc: "z,a,a2,missing", sortArtistAsc: "z,a,a2,missing", sortArtistDesc: "a,a2,z,missing", sortAlbumAsc: "a,a2,z,missing", sortAlbumDesc: "z,a,a2,missing", sortDurationAsc: "a,a2,z,missing", sortDurationDesc: "z,a2,a,missing"} {
		chooseSort(s, mode)
		l := displayListing(s, source)
		var got []string
		for _, track := range l.Tracks {
			got = append(got, track.URI)
		}
		if strings.Join(got, ",") != want {
			t.Errorf("mode %d = %v want %s", mode, got, want)
		}
	}
	if source.Tracks[0].URI != "z" || source.Tracks[1].URI != "missing" {
		t.Fatal("sort mutated backend order")
	}
	chooseSort(s, sortPositionAsc)
	if got := displayListing(s, source).Tracks; got[0].URI != "z" || got[1].URI != "missing" {
		t.Fatal("position order lost")
	}
}

func TestSortKeepsSelectionAndRoutesPlaybackAndContext(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	s.focus = paneTracks
	s.tracks.Select(1) // Gravity
	chooseSort(s, sortTitleAsc)
	if track, ok := s.selectedTrack(); !ok || track.Name != "Gravity" || s.tracks.Cursor() != 0 {
		t.Fatalf("selection %+v at %d", track, s.tracks.Cursor())
	}
	s.tracks.Select(1) // Overthink
	command(s, cmdPlayTrack, s.fx)
	d.Settle()
	if got := f.calls[len(f.calls)-1]; got != "play spotify:playlist:a spotify:track:1" {
		t.Fatalf("played %q", got)
	}
	s.liked["spotify:track:1"] = true
	menu := ContextMenu(s, idTracks+":1")
	if len(menu) < 4 || menu[3].Label != "Remove from Liked Songs" {
		t.Fatalf("wrong row context %+v", menu)
	}
	// A complete refresh changes source order without moving the selected track.
	s.stale[s.shown] = true
	f.listings[s.shown] = player.Listing{URI: s.shown, Ready: true, Length: 3, Cached: 3, Tracks: []player.Track{bassHouse.Tracks[2], bassHouse.Tracks[1], bassHouse.Tracks[0]}}
	loadTracks(s, s.shown)
	d.Settle()
	if track, ok := s.selectedTrack(); !ok || track.Name != "Overthink" {
		t.Fatalf("refresh moved selection %+v", track)
	}
}

func TestSortDropdownKeyboardAndOutsideDismissal(t *testing.T) {
	d, _ := ready(t)
	clickSort := func(id string) {
		_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
		r, ok := comp.Rects[id]
		if !ok || r.W == 0 || r.H == 0 {
			t.Fatalf("missing sort target %s: %v", id, r)
		}
		d.Send(flatte.MouseEvent{Button: flatte.MouseLeft, Action: flatte.MousePress, X: r.X + r.W/2, Y: r.Y + r.H/2})
	}
	s := d.State()
	s.focus = paneSort
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: ' '})
	if !s.trackSort.IsOpen() {
		t.Fatal("space routed to playback instead of focused sort")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	s.focus = paneSort
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if !s.trackSort.IsOpen() {
		t.Fatal("Enter did not open sort")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if s.trackSort.IsOpen() || s.trackSort.Selected() != sortPositionDesc {
		t.Fatalf("sort selected=%d open=%v", s.trackSort.Selected(), s.trackSort.IsOpen())
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	if d.Quit() || s.trackSort.IsOpen() {
		t.Fatal("Escape did not dismiss sort")
	}
	clickSort(idSort)
	if !s.trackSort.IsOpen() {
		t.Fatal("click did not open sort")
	}
	albumDescIndex := slices.Index(s.trackSort.Options(), sortOptions[sortAlbumDesc])
	clickSort(idSort + ":option:" + fmt.Sprint(albumDescIndex))
	if sortMode(s) != sortAlbumDesc || s.trackSort.IsOpen() {
		t.Fatal("click did not select option")
	}
	clickSort(idSort)
	d.Send(flatte.MouseEvent{Button: flatte.MouseLeft, Action: flatte.MousePress, X: 0, Y: 0})
	if s.trackSort.IsOpen() {
		t.Fatal("outside click kept menu open")
	}
}

func TestPlaylistSortDoesNotChangeSearchRankingOrHomeRecency(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	chooseSort(s, sortTitleAsc)
	for _, uri := range []string{uriHome, uriSearch} {
		l := player.Listing{URI: uri, Tracks: []player.Track{{URI: "first", Name: "Zulu"}, {URI: "second", Name: "Alpha"}}}
		if got := displayListing(s, l); got.Tracks[0].URI != "first" {
			t.Fatalf("%s reordered: %v", uri, got.Tracks)
		}
	}
}

func TestDefaultPositionSortAndReverse(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	if s.trackSort.Selected() != sortPositionAsc || s.trackSort.Value() != "Position ↑" {
		t.Fatalf("default sort %q", s.trackSort.Value())
	}
	source := player.Listing{URI: s.shown, Ready: true}
	for i := range 11 {
		source.Tracks = append(source.Tracks, player.Track{URI: fmt.Sprintf("track-%d", 11-i), Name: fmt.Sprintf("Song %d", i+1)})
	}
	got := displayListing(s, source)
	if got.Tracks[0].URI != "track-11" || got.Tracks[10].URI != "track-1" {
		t.Fatal("position used lexical track IDs")
	}
	chooseSort(s, sortPositionDesc)
	got = displayListing(s, source)
	if got.Tracks[0].URI != "track-1" || got.Tracks[10].URI != "track-11" || source.Tracks[0].URI != "track-11" {
		t.Fatal("reverse position mutated or lost source order")
	}
}

func TestOriginalPositionNumbersSurviveFilterAndDuplicateTracks(t *testing.T) {
	source := []player.Track{{URI: "dup"}, {URI: "other"}, {URI: "dup"}, {URI: "last"}}
	for _, tc := range []struct {
		display []player.Track
		reverse bool
		want    []int
	}{
		{[]player.Track{source[1], source[3]}, false, []int{2, 4}},
		{[]player.Track{source[0], source[2]}, false, []int{1, 3}},
		{[]player.Track{source[3], source[2], source[1], source[0]}, true, []int{4, 3, 2, 1}},
	} {
		if got := originalPositionNumbers(source, tc.display, tc.reverse); !slices.Equal(got, tc.want) {
			t.Fatalf("numbers %v want %v", got, tc.want)
		}
	}
}
