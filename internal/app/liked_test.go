package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

type likedWeb struct {
	*fake
	items     []spotify.SavedTrack
	total     int
	failAt    int
	calls     []int
	changeAt  int
	changedTo int
}

type blockingLikeBackend struct {
	*fake
	started chan struct{}
	release chan struct{}
}

func (b *blockingLikeBackend) SetLiked(context.Context, []string, bool) error {
	close(b.started)
	<-b.release
	return nil
}

func (w *likedWeb) LikedTracks(_ context.Context, offset, limit int) (spotify.SavedTracksPage, error) {
	w.calls = append(w.calls, offset)
	if w.failAt >= 0 && w.failAt == offset {
		return spotify.SavedTracksPage{}, fmt.Errorf("page failed")
	}
	total := w.total
	if w.changeAt >= 0 && w.changeAt == offset {
		total = w.changedTo
		w.total = total
		w.changeAt = -1
	}
	end := min(offset+limit, len(w.items))
	if offset > end {
		end = offset
	}
	return spotify.SavedTracksPage{Offset: offset, Limit: limit, Total: total, Items: append([]spotify.SavedTrack(nil), w.items[offset:end]...)}, nil
}

func likedItem(i int) spotify.SavedTrack {
	date := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
	return spotify.SavedTrack{Track: spotify.Track{
		URI: fmt.Sprintf("spotify:track:%d", i), Name: fmt.Sprintf("Song %d", i),
		DurationMS: 180000, Artists: []spotify.Artist{{URI: "spotify:artist:a", Name: "Artist"}},
		Album: spotify.Album{URI: "spotify:album:a", Name: "Album"},
	}, AddedAt: date}
}

func startLiked(t *testing.T, web *likedWeb) (*flatest.Driver[State], string) {
	t.Helper()
	f := web.fake
	f.playlists = library
	f.status = player.Status{Username: "test-user", Volume: 40, VolumeSteps: 100, Paused: true}
	d := flatest.Start(New(f, nil, web, nil), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	event(d, player.Ready{})
	d.State().page = pageLibrary
	d.State().rail.Select(0)
	showSelected(d.State())
	d.Settle()
	return d, player.LikedSongs("test-user")
}

func TestLikedTracksPageAllResultsAndDateDefault(t *testing.T) {
	f := &fake{listings: map[string]player.Listing{}}
	w := &likedWeb{fake: f, total: 2057, failAt: -1, changeAt: -1}
	for i := range 2057 {
		w.items = append(w.items, likedItem(i))
	}
	d, uri := startLiked(t, w)
	s := d.State()
	l := s.listings[uri]
	if !l.Complete() || l.Length != 2057 || l.Cached != 2057 || len(l.Tracks) != 2057 {
		t.Fatalf("liked listing user=%q shown=%q calls=%v total/cache/tracks=%d/%d/%d complete=%v", s.username, s.shown, w.calls, l.Length, l.Cached, len(l.Tracks), l.Complete())
	}
	if len(w.calls) != 42 || w.calls[0] != 0 || w.calls[len(w.calls)-1] != 2050 {
		t.Fatalf("page offsets %v", w.calls)
	}
	for _, track := range l.Tracks {
		if !s.liked[track.URI] {
			t.Fatalf("Full paging lost membership for %q", track.URI)
		}
	}
	if s.trackSort.Value() != "Date added ↓" {
		t.Fatalf("Liked Songs default sort %q", s.trackSort.Value())
	}
	if l.Tracks[0].AddedAt != w.items[0].AddedAt || l.Tracks[2056].URI != w.items[2056].Track.URI {
		t.Fatalf("timestamps or final item missing: first=%+v last=%+v", l.Tracks[0], l.Tracks[2056])
	}
	chooseSort(s, sortAddedAtAsc)
	if got := displayListing(s, l).Tracks; got[0].URI != "spotify:track:0" || got[2056].URI != "spotify:track:2056" {
		t.Fatalf("ascending added order first/last=%s/%s", got[0].URI, got[len(got)-1].URI)
	}
	chooseSort(s, sortAddedAtDesc)
	if got := displayListing(s, l).Tracks; got[0].URI != "spotify:track:2056" || got[2056].URI != "spotify:track:0" {
		t.Fatalf("descending added order first/last=%s/%s", got[0].URI, got[len(got)-1].URI)
	}
	if s.trackSort.Selected() != sortAddedAtDesc {
		t.Fatalf("visible Date Added selection index %d", s.trackSort.Selected())
	}
}

func TestLikedTracksIncompleteCountAndHonestFilterCopy(t *testing.T) {
	f := &fake{listings: map[string]player.Listing{}}
	w := &likedWeb{fake: f, total: 51, failAt: 50, changeAt: -1}
	for i := range 51 {
		w.items = append(w.items, likedItem(i))
	}
	d, uri := startLiked(t, w)
	s := d.State()
	l := s.listings[uri]
	if l.Complete() || l.Length != 51 || l.Cached != 50 || len(l.Tracks) != 50 {
		t.Fatalf("incomplete liked listing total/cache/tracks=%d/%d/%d complete=%v", l.Length, l.Cached, len(l.Tracks), l.Complete())
	}
	if stage := s.likedLoads[uri]; stage == nil || stage.total != 51 || len(stage.tracks) != 50 || stage.err == "" {
		t.Fatalf("page stage %+v", stage)
	}
	s.trackFilter.Value = "no-such-song"
	copy := plain(d.Frame())
	if !strings.Contains(copy, "51 songs") || !strings.Contains(copy, "50 loaded") {
		t.Fatalf("header did not separate total and fetched count:\n%s", copy)
	}
	if !strings.Contains(copy, "No matches in loaded songs yet; still listing.") || strings.Contains(copy, "No songs match your filter.") {
		t.Fatalf("misleading partial filter state:\n%s", copy)
	}
}

func TestLikedTracksInitialFailureRetriesFromTick(t *testing.T) {
	f := &fake{listings: map[string]player.Listing{}}
	w := &likedWeb{fake: f, total: 1, failAt: 0, changeAt: -1, items: []spotify.SavedTrack{likedItem(0)}}
	d, uri := startLiked(t, w)
	s := d.State()
	if _, ok := s.listings[uri]; ok || s.likedLoads[uri] == nil || s.likedLoads[uri].err == "" {
		t.Fatalf("initial failure state listing=%v stage=%+v", s.listings[uri], s.likedLoads[uri])
	}
	w.failAt = -1
	s.likedLoads[uri].retryAt = time.Time{}
	tick(s, time.Now())
	d.Settle()
	if l := s.listings[uri]; !l.Complete() || len(l.Tracks) != 1 {
		t.Fatalf("retry did not complete listing: %+v", l)
	}
}

func TestRepeatedLikedPageErrorsRestartFromFirstPage(t *testing.T) {
	f := &fake{listings: map[string]player.Listing{}}
	w := &likedWeb{fake: f, total: 51, failAt: 50, changeAt: -1}
	for i := range 51 {
		w.items = append(w.items, likedItem(i))
	}
	d, uri := startLiked(t, w)
	s := d.State()
	for range 2 {
		s.likedLoads[uri].retryAt = time.Time{}
		tick(s, time.Now())
		d.Settle()
	}
	stage := s.likedLoads[uri]
	if stage == nil || stage.offset != 0 || len(stage.tracks) != 0 || len(w.calls) != 4 || w.calls[3] != 50 {
		t.Fatalf("repeated errors did not reset staging: stage=%+v calls=%v", stage, w.calls)
	}
}

func TestLikedTracksCountChangeRestartsInsteadOfMixingPages(t *testing.T) {
	f := &fake{listings: map[string]player.Listing{}}
	w := &likedWeb{fake: f, total: 51, failAt: -1, changeAt: 50, changedTo: 52}
	for i := range 52 {
		w.items = append(w.items, likedItem(i))
	}
	d, uri := startLiked(t, w)
	l := d.State().listings[uri]
	if !l.Complete() || l.Length != 52 || len(l.Tracks) != 52 {
		t.Fatalf("listing after changed total total/cache/tracks=%d/%d/%d", l.Length, l.Cached, len(l.Tracks))
	}
	if fmt.Sprint(w.calls) != "[0 50 0 50]" {
		t.Fatalf("paging did not restart cleanly: %v", w.calls)
	}
}

func TestDateAddedSortKeepsMissingLastAndIsStable(t *testing.T) {
	d, uri := startLiked(t, &likedWeb{fake: &fake{listings: map[string]player.Listing{}}, total: 0, failAt: -1, changeAt: -1})
	s := d.State()
	first := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	last := first.Add(time.Hour)
	source := player.Listing{URI: uri, Tracks: []player.Track{
		{URI: "equal-a", Name: "A", AddedAt: first},
		{URI: "missing", Name: "M"},
		{URI: "newer", Name: "N", AddedAt: last},
		{URI: "equal-b", Name: "B", AddedAt: first},
	}}
	s.listings[uri] = source
	chooseSort(s, sortAddedAtAsc)
	if got := displayListing(s, source).Tracks; got[0].URI != "equal-a" || got[1].URI != "equal-b" || got[2].URI != "newer" || got[3].URI != "missing" {
		t.Fatalf("ascending timestamps: %+v", got)
	}
	chooseSort(s, sortAddedAtDesc)
	if got := displayListing(s, source).Tracks; got[0].URI != "newer" || got[1].URI != "equal-a" || got[2].URI != "equal-b" || got[3].URI != "missing" {
		t.Fatalf("descending timestamps: %+v", got)
	}
}

func TestLikedRefreshWaitsForPendingLikeWrite(t *testing.T) {
	f := &fake{listings: map[string]player.Listing{}}
	f.status = player.Status{Username: "test-user", Volume: 40, VolumeSteps: 100, Paused: true}
	w := &likedWeb{fake: f, total: 1, failAt: -1, changeAt: -1, items: []spotify.SavedTrack{likedItem(0)}}
	be := &blockingLikeBackend{fake: f, started: make(chan struct{}), release: make(chan struct{})}
	d := flatest.Start(New(be, nil, w, nil), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	event(d, player.Ready{})
	d.State().page = pageLibrary
	d.State().rail.Select(0)
	showSelected(d.State())
	d.Settle()
	s, uri := d.State(), player.LikedSongs("test-user")
	setLiked(s, s.listings[uri].Tracks[0], false)
	tick(s, time.Now())
	if len(w.calls) != 1 || s.likedWrites != 1 || !s.stale[uri] {
		t.Fatalf("refresh raced write: calls=%v writes=%d stale=%v", w.calls, s.likedWrites, s.stale[uri])
	}
	settled := make(chan struct{})
	go func() {
		d.Settle()
		close(settled)
	}()
	select {
	case <-be.started:
	case <-time.After(time.Second):
		t.Fatal("SetLiked request did not start")
	}
	if len(w.calls) != 1 {
		t.Fatalf("refresh started before SetLiked completed: calls=%v", w.calls)
	}
	close(be.release)
	select {
	case <-settled:
	case <-time.After(time.Second):
		t.Fatal("refresh did not settle after SetLiked completed")
	}
	if len(w.calls) != 2 || s.likedWrites != 0 {
		t.Fatalf("post-write refresh calls=%v writes=%d", w.calls, s.likedWrites)
	}
}

func TestFullLikedRefreshReconcilesCachedMembership(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			web := &likedWeb{fake: &fake{listings: map[string]player.Listing{}}, failAt: -1, changeAt: -1}
			if unknown {
				web.total = 51
				for i := range 51 {
					web.items = append(web.items, likedItem(i))
				}
				web.items[0] = spotify.SavedTrack{}
			}
			d, uri := startLiked(t, web)
			s := d.State()
			s.liked["spotify:track:removed-elsewhere"] = true
			s.stale[uri] = true
			loadTracks(s, uri)
			d.Settle()
			if got := s.liked["spotify:track:removed-elsewhere"]; got != unknown {
				t.Fatalf("cached membership=%v unknown slots=%v", got, unknown)
			}
		})
	}
}
