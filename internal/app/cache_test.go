package app

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/smolify/internal/librarycache"
	"github.com/lunguini/smolify/internal/player"
)

func TestCacheableListingRejectsPlayerCapButAllowsFullLikedPages(t *testing.T) {
	s := &State{username: "alice", playlists: []player.Playlist{{URI: "playlist:known", Length: 2300}, {URI: "playlist:small", Length: 40}}}
	complete := func(uri string, n int) player.Listing {
		tracks := make([]player.Track, n)
		for i := range tracks {
			tracks[i].URI = "spotify:track:" + strconv.Itoa(i+1)
		}
		return player.Listing{URI: uri, Ready: true, Length: n, Cached: n, Tracks: tracks}
	}
	for _, tc := range []struct {
		uri string
		l   player.Listing
		ok  bool
	}{
		{"playlist:known", complete("playlist:known", 2000), false},
		{"playlist:small", complete("playlist:small", 40), true},
		{"playlist:small", complete("playlist:small", 2000), false},
		{"playlist:unknown", complete("playlist:unknown", 2000), false},
		{"spotify:playlist:external", complete("spotify:playlist:external", 45), true},
		{"spotify:playlist:external", complete("spotify:playlist:external", 2000), false},
		{"spotify:album:external", complete("spotify:album:external", 3), false},
		{player.LikedSongs("alice"), complete(player.LikedSongs("alice"), 2300), true},
		{"playlist:empty-count", player.Listing{URI: "playlist:empty-count", Ready: true, Length: 1, Cached: 1}, false},
		{"playlist:negative", player.Listing{URI: "playlist:negative", Ready: true, Length: -1, Cached: 0}, false},
		{"playlist:short", player.Listing{URI: "playlist:short", Ready: true, Length: 2, Cached: 2, Tracks: []player.Track{{URI: "spotify:track:1"}}}, false},
	} {
		if got := cacheableListing(s, tc.uri, tc.l); got != tc.ok {
			t.Errorf("cacheableListing(%q)=%v want %v", tc.uri, got, tc.ok)
		}
	}
}

func TestSaveCacheWaitsForHydrationMerge(t *testing.T) {
	d := flatest.Start(New(&fake{}, nil, nil, nil), 100)
	s := d.State()
	s.cache = librarycache.New(t.TempDir())
	s.username, s.page = "alice", pageHome
	s.cacheEpoch = s.cache.Generation(s.username)
	uri := player.LikedSongs("alice")
	cached := player.Listing{URI: uri, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{{URI: "spotify:track:cached"}}}
	before := librarycache.Snapshot{
		Playlists: []player.Playlist{{URI: "playlist:one", Name: "Cached"}},
		Listings:  map[string]player.Listing{uri: cached},
	}
	if err := s.cache.Save("alice", s.cacheEpoch, 1, before); err != nil {
		t.Fatal(err)
	}

	// A remote library fold can happen while the asynchronous disk read has
	// not yet returned. Its early save must not replace the unread snapshot.
	s.cacheLoading = true
	s.playlists = nil
	s.listings = map[string]player.Listing{}
	saveCache(s)
	d.Settle()
	got, err := s.cache.Load("alice")
	if err != nil || got.Playlists[0].Name != "Cached" || got.Listings[uri].Tracks[0].URI != "spotify:track:cached" {
		t.Fatalf("cache changed before hydration completed: %+v err=%v", got, err)
	}

	// Once the cached snapshot is merged and the remote library is known,
	// ordinary persistence can write the fresher app state.
	applyCacheSnapshot(s, "alice", got)
	s.playlists[0].Name = "Remote"
	updated := s.listings[uri]
	updated.Tracks[0].URI = "spotify:track:remote"
	s.listings[uri] = updated
	s.cacheLoading = false
	s.libraryRead = true
	saveCache(s)
	d.Settle()
	got, err = s.cache.Load("alice")
	if err != nil || got.Playlists[0].Name != "Remote" || got.Listings[uri].Tracks[0].URI != "spotify:track:remote" {
		t.Fatalf("post-hydration snapshot not saved: %+v err=%v", got, err)
	}
}

func TestHydrateCacheShowsCompleteDataAndMarksItForRefresh(t *testing.T) {
	s := New(nil, nil, nil, nil).State
	s.cache = librarycache.New(t.TempDir())
	s.username = "alice"
	s.page = pageHome // avoid starting a playlist request from this fixture
	uri := player.LikedSongs("alice")
	l := player.Listing{URI: uri, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{{URI: "spotify:track:1"}}}
	if err := s.cache.Save("alice", 0, 1, librarycache.Snapshot{
		Playlists: []player.Playlist{{URI: "playlist:one", Name: "One"}},
		Listings:  map[string]player.Listing{uri: l},
	}); err != nil {
		t.Fatal(err)
	}
	applyCacheSnapshot(s, "alice", librarycache.Snapshot{
		Playlists: []player.Playlist{{URI: "playlist:one", Name: "One"}},
		Listings:  map[string]player.Listing{uri: l},
	})
	if got := s.listings[uri]; !got.Complete() || got.Tracks[0].URI != "spotify:track:1" {
		t.Fatalf("cached listing not hydrated: %+v", got)
	}
	if !s.stale[uri] {
		t.Fatal("cached listing was not scheduled for remote refresh")
	}
	if !s.liked["spotify:track:1"] {
		t.Fatal("cached Liked Songs membership was not indexed")
	}
	if len(s.playlists) != 1 || !s.loaded {
		t.Fatalf("cached library not hydrated: playlists=%v loaded=%v", s.playlists, s.loaded)
	}
}

func TestHydrateCacheClearsLoadingAfterMerge(t *testing.T) {
	d := flatest.Start(New(&fake{}, nil, nil, nil), 100)
	s := d.State()
	s.cache = librarycache.New(t.TempDir())
	s.username, s.page = "alice", pageHome
	uri := player.LikedSongs("alice")
	l := player.Listing{URI: uri, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{{URI: "spotify:track:1"}}}
	if err := s.cache.Save("alice", s.cache.Generation("alice"), 1, librarycache.Snapshot{
		Listings: map[string]player.Listing{uri: l},
	}); err != nil {
		t.Fatal(err)
	}
	hydrateCache(s, "alice")
	if !s.cacheLoading {
		t.Fatal("cache loading flag was not set before the disk read")
	}
	d.Settle()
	if s.cacheLoading || !s.listings[uri].Complete() {
		t.Fatalf("cache load did not finish and merge: loading=%v listing=%+v", s.cacheLoading, s.listings[uri])
	}
}

func TestConfirmedLikeUpdatesDiskAndFailedUnlikeKeepsOriginalOrder(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	s.username, s.page = "alice", pageHome
	s.cache = librarycache.New(t.TempDir())
	s.cacheEpoch = s.cache.Generation(s.username)
	uri := player.LikedSongs(s.username)
	first := player.Track{URI: "spotify:track:first", Name: "First"}
	second := player.Track{URI: "spotify:track:second", Name: "Second"}
	s.listings[uri] = player.Listing{URI: uri, Ready: true, Length: 2, Cached: 2, Tracks: []player.Track{first, second}}
	s.liked[first.URI], s.liked[second.URI] = true, true
	saveCache(s)
	d.Settle()
	f.fail = fmt.Errorf("offline")
	setLiked(s, second, false)
	d.Settle()
	if tracks := s.listings[uri].Tracks; len(tracks) != 2 || tracks[0].URI != first.URI || tracks[1].URI != second.URI {
		t.Fatalf("Failed unlike changed source order: %+v", tracks)
	}
	cached, err := s.cache.Load(s.username)
	if err != nil || len(cached.Listings[uri].Tracks) != 2 {
		t.Fatalf("Failed unlike reached cache: %+v %v", cached, err)
	}
	f.fail = nil
	setLiked(s, second, false)
	d.Settle()
	cached, err = s.cache.Load(s.username)
	if err != nil || len(cached.Listings[uri].Tracks) != 1 || cached.Listings[uri].Tracks[0].URI != first.URI {
		t.Fatalf("Confirmed unlike was not saved: %+v %v", cached, err)
	}
}

func TestRapidFailedLikeToggleDoesNotPersistOptimisticMembership(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	s.username, s.page = "alice", pageHome
	uri := player.LikedSongs(s.username)
	s.listings[uri] = player.Listing{URI: uri, Ready: true}
	s.liked[song.URI] = false
	f.fail = fmt.Errorf("offline")
	setLiked(s, song, true)
	setLiked(s, song, false)
	d.Settle()
	if s.liked[song.URI] || len(s.listings[uri].Tracks) != 0 || s.likedWrites != 0 {
		t.Fatalf("Failed toggle retained optimistic data: liked=%v listing=%+v writes=%d", s.liked[song.URI], s.listings[uri], s.likedWrites)
	}
}

func TestCacheHydrationDoesNotReverseKnownUnlike(t *testing.T) {
	s := New(nil, nil, nil, nil).State
	s.username, s.page = "alice", pageHome
	trackURI := "spotify:track:already-unliked"
	s.liked[trackURI] = false
	likedURI := player.LikedSongs(s.username)
	applyCacheSnapshot(s, s.username, librarycache.Snapshot{Listings: map[string]player.Listing{
		likedURI: {URI: likedURI, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{{URI: trackURI}}},
	}})
	if s.liked[trackURI] {
		t.Fatal("Disk hydration reversed a newer unlike")
	}
}
