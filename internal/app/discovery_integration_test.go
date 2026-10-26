package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

type catalogFake struct {
	*fake
	feed   spotify.HomeFeed
	artist spotify.ArtistOverview
	album  spotify.AlbumDetails
	err    error
}

func (f *catalogFake) Home(context.Context) (spotify.HomeFeed, error) { return f.feed, f.err }
func (f *catalogFake) Artist(context.Context, string) (spotify.ArtistOverview, error) {
	return f.artist, f.err
}
func (f *catalogFake) Album(context.Context, string) (spotify.AlbumDetails, error) {
	return f.album, f.err
}

func TestPersonalizedHomeCardsKeyboardAndBrowseHistory(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	web := &catalogFake{fake: f, feed: spotify.HomeFeed{Sections: []spotify.HomeSection{
		{Title: "Made for you", Items: []spotify.HomeItem{{URI: "spotify:album:a", Name: "A", Kind: "album"}}},
		{Title: "Jump back in", Items: []spotify.HomeItem{{URI: "spotify:album:a", Name: "A", Kind: "album"}, {URI: "spotify:artist:x", Name: "X", Kind: "artist"}}},
	}}, album: spotify.AlbumDetails{Album: spotify.Album{URI: "spotify:album:a", Name: "A"}, Total: 1, Tracks: []spotify.Track{{URI: "spotify:track:a", Name: "First", DurationMS: 1000}}}}
	s.web = web
	s.username = "test-user"
	d.Send(flatte.MenuEvent{ID: cmdHome})
	d.Settle()
	if s.discovery.HomeLoading || len(homeItems(s)) != 3 {
		t.Fatalf("Home did not load: %+v", s.discovery)
	}
	// The same album in two shelves must have distinct targets.
	nativeSceneIDsAt(t, s, homeCardID(0))
	nativeSceneIDsAt(t, s, homeCardID(1))
	d.Send(key(flatte.KeyDown, 0))
	d.Send(key(flatte.KeyEnter, 0))
	d.Settle()
	if s.page != pageBrowse || s.shown != "spotify:album:a" || !s.listings[s.shown].Complete() {
		t.Fatalf("Enter did not open selected album: %v %q", s.page, s.shown)
	}
	d.Send(flatte.MenuEvent{ID: cmdBack})
	d.Settle()
	if s.page != pageHome {
		t.Fatalf("Back page %v", s.page)
	}
	d.Send(flatte.MenuEvent{ID: cmdForward})
	d.Settle()
	if s.page != pageBrowse || s.shown != "spotify:album:a" {
		t.Fatal("Forward lost album")
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "play ") {
			t.Fatalf("Browsing started playback: %v", f.calls)
		}
	}
}

func TestRecentHomeSongPlaysItsOriginalContext(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	s.page, s.focus = pageHome, paneTracks
	show(s, uriHome)
	t1 := song
	t1.ContextURI = "spotify:playlist:original"
	setVirtual(s, uriHome, []player.Track{t1})
	from := len(s.back)
	d.Send(key(flatte.KeyEnter, 0))
	d.Settle()
	if len(s.back) != from || s.page != pageHome {
		t.Fatal("Playing a recent song changed navigation")
	}
	if !strings.Contains(strings.Join(f.calls, "|"), "play spotify:playlist:original "+song.URI) {
		t.Fatalf("Wrong recent context: %v", f.calls)
	}
}

func TestBrowsePartialRefreshKeepsSelectionAndErrorsCanRetry(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	uri := "spotify:playlist:outside"
	f.listings[uri] = player.Listing{URI: uri, Ready: true, Length: 3, Cached: 2, Tracks: []player.Track{{URI: "one", Name: "One"}, {URI: "two", Name: "Two"}}}
	openBrowse(s, uri, "Outside", "")
	d.Settle()
	s.tracks.Select(1)
	f.listings[uri] = player.Listing{URI: uri, Ready: true, Length: 3, Cached: 3, Tracks: []player.Track{{URI: "zero", Name: "Zero"}, {URI: "one", Name: "One"}, {URI: "two", Name: "Two"}}}
	tick(s, time.Now())
	d.Settle()
	track, _ := s.selectedTrack()
	if track.URI != "two" || !s.listings[uri].Complete() {
		t.Fatalf("Partial refresh moved selected track: %+v", track)
	}
	// Catalog errors stay in the page and expose a retry action.
	web := &catalogFake{fake: f, err: errors.New("offline")}
	s.web = web
	openBrowse(s, "spotify:artist:x", "X", "")
	d.Settle()
	if !strings.Contains(plain(d.Frame()), "Could not load this page:") {
		t.Fatal("Browse error invisible")
	}
	web.err = nil
	web.artist = spotify.ArtistOverview{Artist: spotify.Artist{URI: "spotify:artist:x", Name: "X"}, TopTracks: []spotify.Track{{URI: "spotify:track:x", Name: "Recovered"}}}
	Handle(s, flatte.MouseEvent{Button: flatte.MouseLeft, Action: flatte.MousePress, Target: "browse-retry"}, s.fx)
	d.Settle()
	if s.discovery.Pages[s.shown].Err != "" || len(s.listings[s.shown].Tracks) != 1 {
		t.Fatal("Browse retry did not recover")
	}
}

func TestSongRadioUsesRealStationContext(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	s.tracks.Select(0)
	d.Send(flatte.MenuEvent{ID: "song-radio", Target: idTracks + ":0"})
	d.Settle()
	want := songRadioURI(song.URI)
	if want == "" || s.shown != want || s.page != pageBrowse {
		t.Fatalf("Radio page %q", s.shown)
	}
	if !strings.Contains(strings.Join(f.calls, "|"), "play "+want+" ") {
		t.Fatalf("Radio did not play station: %v", f.calls)
	}
	if songRadioURI("spotify:episode:no") != "" {
		t.Fatal("Episode accepted as song radio")
	}
}

func TestSettingsDropdownClickAwayReturnsPlaybackFocus(t *testing.T) {
	d, f := ready(t)
	s := d.State()
	event(d, song)
	openSettings(s)
	s.settingQuality.Open()
	Handle(s, flatte.MouseEvent{Button: flatte.MouseLeft, Action: flatte.MousePress, Target: "center-card"}, s.fx)
	if s.settingQuality.IsOpen() || s.focus != panePlayer {
		t.Fatal("Click away retained settings dropdown")
	}
	f.calls = nil
	d.Send(char(' ', 0))
	d.Settle()
	if strings.Join(f.calls, ",") != "playpause" {
		t.Fatalf("Space didn't reach player: %v", f.calls)
	}
}
