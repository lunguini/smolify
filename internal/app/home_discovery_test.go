package app

import (
	"reflect"
	"testing"

	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

func TestHomePlaylistFallbackUsesOnlyRealAccountMixes(t *testing.T) {
	input := []player.Playlist{
		{URI: "spotify:playlist:daily", Name: "Daily Mix 2"},
		{URI: "spotify:playlist:discover", Name: "Discover Weekly"},
		{URI: "spotify:playlist:radar", Name: "Release Radar"},
		{URI: "spotify:playlist:made", Name: "Made for You"},
		{URI: "spotify:playlist:daily", Name: "Daily Mix 2"},
		{URI: "", Name: "Made for You"},
	}
	got := homePlaylistFallback(input)
	want := []player.Playlist{input[0], input[1], input[2], input[3]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fallback = %#v, want %#v", got, want)
	}
}

func TestHomeFallbackRetainsOnlyAccountPlaylistURIs(t *testing.T) {
	s := &State{playlists: []player.Playlist{{URI: "spotify:playlist:one", Name: "Discover Weekly", Image: "cover"}, {URI: "spotify:playlist:other", Name: "Road Trip"}}}
	showHomePlaylistFallback(s, "private Home unavailable")
	if !s.discovery.HomeFallback || s.discovery.HomeError == "" || len(s.discovery.Home.Sections) != 1 || len(s.discovery.Home.Sections[0].Items) != 1 {
		t.Fatalf("fallback = %+v", s.discovery)
	}
	if got := s.discovery.Home.Sections[0].Items[0].URI; got != "spotify:playlist:one" {
		t.Fatalf("fallback URI = %q", got)
	}
}

func TestHomeRefreshFailureKeepsPreviouslyLoadedSpotifyShelves(t *testing.T) {
	s := &State{
		playlists: []player.Playlist{{URI: "spotify:playlist:saved", Name: "Daily Mix 1"}},
		discovery: discoveryState{Home: spotify.HomeFeed{Sections: []spotify.HomeSection{{Title: "Made for you", Items: []spotify.HomeItem{{URI: "spotify:playlist:personal", Name: "Personal mix"}}}}}},
	}
	showHomePlaylistFallback(s, "offline")
	if len(s.discovery.Home.Sections) != 1 || s.discovery.Home.Sections[0].Items[0].URI != "spotify:playlist:personal" || s.discovery.HomeError != "offline" {
		t.Fatalf("failed refresh lost last Home: %+v", s.discovery)
	}
}
