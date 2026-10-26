package app

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/flatte/window"

	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

// TestWindowSnapshot renders the main window offscreen, for looking at the
// design: SMOLIFY_SNAPSHOT=<dir> go test -run TestWindowSnapshot ./internal/app
func TestWindowSnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	a := sampleApp()
	img, err := window.Snapshot(a, snapOptions(110, 34), 2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "smolify-main.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	// Liked Songs and a playlist without a cover, on Smolify's own art.
	s := a.State
	s.username = "adrian"
	s.discovery.Pages = make(map[string]discoveryPage)
	// Settings, with the search field focused to show its focused look.
	s.page, s.focus = pageSettings, paneSettings
	s.settings.Bitrate = 160 // staged so the explicit Apply state is visible
	s.settingQuality.Select(1)
	s.settingCursor = 2
	if img, err := window.Snapshot(a, snapOptions(110, 40), 2); err == nil {
		if f, err := os.Create(filepath.Join(dir, "smolify-settings.png")); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	if img, err := window.Snapshot(a, snapOptions(90, 34), 2); err == nil {
		if f, err := os.Create(filepath.Join(dir, "smolify-settings-narrow.png")); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	s.focus = paneSearch
	s.query = flatui.TextField{Value: "daft punk"}
	s.query.SelectAll()
	if img, err := window.Snapshot(a, snapOptions(110, 34), 2); err == nil {
		if f, err := os.Create(filepath.Join(dir, "smolify-search-focus.png")); err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	s.page, s.focus, s.query = pageLibrary, paneLibrary, flatui.TextField{}
	for _, page := range []struct{ name, uri string }{{"smolify-liked.png", player.LikedSongs(s.username)}, {"smolify-nocover.png", "p:3"}} {
		l := s.listings["p:1"]
		l.URI = page.uri
		s.listings[page.uri], s.shown = l, page.uri
		libraryChanged(s)
		name := page.name
		img, err := window.Snapshot(a, snapOptions(110, 34), 2)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func TestWindowLikedDatesSnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	f := &fake{}
	w := &likedWeb{fake: f, total: 3, failAt: -1, changeAt: -1, items: []spotify.SavedTrack{likedItem(0), likedItem(1), likedItem(2)}}
	a := Windowed(New(f, nil, w, nil))
	s := a.State
	s.username, s.page, s.ready, s.focus = "adrian", pageLibrary, true, paneTracks
	s.playlists = library
	uri := player.LikedSongs(s.username)
	tracks := []player.Track{savedTrack(w.items[0]), savedTrack(w.items[1]), savedTrack(w.items[2])}
	s.listings[uri] = player.Listing{URI: uri, Ready: true, Length: len(tracks), Cached: len(tracks), Tracks: tracks}
	s.shown = uri
	s.tracks.SetCount(len(tracks))
	s.rail.SetCount(len(s.library()))
	s.rail.Select(0)
	s.trackSort.SetOptions(sortOptions)
	s.trackSort.Select(sortAddedAtDesc)
	for _, size := range []struct {
		name string
		w, h int
	}{{"smolify-liked-dates.png", 110, 34}, {"smolify-liked-dates-narrow.png", 70, 30}} {
		scene := layout.LayoutScene(View(s, flatte.RenderContext{Width: size.w, Graphical: true}).Root, float64(size.w*8), float64(size.h*20),
			layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
		_, hasDate := scene.Rects["track-date-heading"]
		if hasDate != (size.w >= 90) {
			t.Fatalf("Date added heading at width %d: present=%v", size.w, hasDate)
		}
		img, err := window.Snapshot(a, snapOptions(size.w, size.h), 2)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, size.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
	}
}

func TestWindowDiscoverySnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	a := sampleApp()
	s := a.State
	s.username = "adrian"
	s.discovery.Pages = make(map[string]discoveryPage)
	s.page, s.focus, s.ready = pageHome, paneTracks, true
	items := make([]spotify.HomeItem, 0, 8)
	for i := range 6 {
		uri := fmt.Sprintf("spotify:playlist:mix%d", i)
		items = append(items, spotify.HomeItem{URI: uri, Name: fmt.Sprintf("Daily Mix %d", i+1), Kind: "playlist", Image: "home-cover"})
	}
	items = append(items,
		spotify.HomeItem{URI: "spotify:playlist:discover", Name: "Discover Weekly", Kind: "playlist", Image: "home-cover"},
		spotify.HomeItem{URI: "spotify:playlist:radar", Name: "Release Radar", Kind: "playlist", Image: "home-cover"},
	)
	s.discovery.Home = spotify.HomeFeed{Greeting: "Good evening", Sections: []spotify.HomeSection{{Title: "Made for you", Items: items}}}
	s.covers["home-cover"] = art(color.RGBA{80, 130, 180, 255}, color.RGBA{100, 40, 150, 255})
	for _, size := range []struct {
		name string
		w, h int
	}{{"smolify-home-discovery.png", 110, 34}, {"smolify-home-discovery-narrow.png", 90, 34}} {
		img, err := window.Snapshot(a, snapOptions(size.w, size.h), 2)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, size.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
	}
	for _, kind := range []string{"artist", "album"} {
		uri := "spotify:" + kind + ":snapshot"
		s.page, s.shown = pageBrowse, uri
		page := discoveryPage{URI: uri, Name: "Discovery Example", Image: "home-cover", Kind: kind}
		if kind == "artist" {
			page.Artist = &spotify.ArtistOverview{Artist: spotify.Artist{URI: uri, Name: page.Name}, Image: "home-cover"}
		} else {
			page.Album = &spotify.AlbumDetails{Album: spotify.Album{URI: uri, Name: page.Name, Images: []spotify.Image{{URL: "home-cover"}}, Artists: []spotify.Artist{{URI: "spotify:artist:example", Name: "Example artist"}}}, Total: 9}
		}
		s.discovery.Pages[uri] = page
		tracks := make([]player.Track, 0, 9)
		for i := range 9 {
			albumURI := fmt.Sprintf("spotify:album:single%d", i)
			albumName := fmt.Sprintf("Single %d", i+1)
			artists, artistURIs := []string{"Discovery Example"}, []string{uri}
			if kind == "album" {
				albumURI, albumName = uri, page.Name
				artists, artistURIs = []string{"Example artist"}, []string{"spotify:artist:example"}
			}
			tracks = append(tracks, player.Track{URI: fmt.Sprintf("spotify:track:snapshot%d", i), Name: fmt.Sprintf("Sample song %d", i+1), Artists: artists, ArtistURIs: artistURIs, Album: albumName, AlbumURI: albumURI, Cover: "home-cover", Duration: 3 * time.Minute, TrackNumber: i + 1})
		}
		s.listings[uri] = player.Listing{URI: uri, Ready: true, Length: len(tracks), Cached: len(tracks), Tracks: tracks}
		s.tracks.SetCount(len(tracks))
		s.tracks.SetHeight(12)
		s.tracks.Select(0)
		img, err := window.Snapshot(a, snapOptions(110, 34), 2)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "smolify-"+kind+"-discovery.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
	}
}

// BenchmarkWindowFrame renders the main window at a run of sizes, as a
// live resize does.
func BenchmarkWindowFrame(b *testing.B) {
	a := sampleApp()
	for i := 0; b.Loop(); i++ {
		if _, err := window.Snapshot(a, snapOptions(90+i%40, 30+i%10), 2); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWindowScroll renders a 2000-track playlist scrolled a row
// further each frame, as a wheel does.
func BenchmarkWindowScroll(b *testing.B) {
	a := sampleApp()
	s := a.State
	l := player.Listing{URI: "p:1", Ready: true, Length: 2000, Cached: 2000}
	for i := range 2000 {
		l.Tracks = append(l.Tracks, player.Track{
			URI: fmt.Sprintf("spotify:track:%d", i), Name: fmt.Sprintf("A fairly long song title number %d (Extended Mix)", i),
			Artists: []string{fmt.Sprintf("Artist %d", i%300), "Featured Guest"}, Album: fmt.Sprintf("Album with a long name %d", i%150),
			Duration: time.Duration(150+i%200) * time.Second,
		})
	}
	s.listings["p:1"] = l
	s.tracks.SetCount(2000)
	s.tracks.SetHeight(20)
	for b.Loop() {
		s.tracks.Scroll(1)
		if _, err := window.Snapshot(a, snapOptions(110, 34), 1); err != nil {
			b.Fatal(err)
		}
	}
}

// snapOptions is the main window at w×h cells, in Smolify's theme.
func snapOptions(w, h int) window.Options {
	theme := WindowTheme()
	return window.Options{Width: w, Height: h, Theme: &theme, Fonts: WindowFonts()}
}

// sampleApp is the app with a library, a listed playlist, and a track
// playing.
func sampleApp() flatte.App[State] {
	f := &fake{}
	a := New(f, nil, f, nil)
	s := a.State
	s.page = pageLibrary
	s.ready = true
	s.playlists = []player.Playlist{
		{URI: "p:1", Name: "OverThink Radio", Owner: "Spotify", Length: 50, Image: "c:1", Description: "With Kuo, Lin and more. Updated daily."},
		{URI: "p:2", Name: "bass house", Owner: "adrian", Length: 42, Image: "c:2"},
		{URI: "p:3", Name: "Podcasts", Owner: "adrian", Length: 12},
		{URI: "p:4", Name: "Insane — Alastor (Hazbin Hotel)", Owner: "adrian", Length: 3},
		{URI: "p:5", Name: "Your Top Songs 2023", Owner: "Spotify", Length: 101},
		{URI: "p:6", Name: "Chill Bath Mix", Owner: "Spotify", Length: 50},
		{URI: "p:7", Name: "Car Music", Owner: "adrian", Length: 7},
	}
	s.rail.SetCount(len(s.playlists))
	s.rail.SetHeight(20)
	s.shown = "p:1"
	names := []struct{ name, artist, album string }{
		{"OverThink", "Kuo, Lin", "OverThink"}, {"Gravity", "Dua", "Fall"}, {"Low Tide", "Mara", "Shores"},
		{"Neon Rain", "Kuo", "City Lights"}, {"Paper Planes", "Lin, Vee", "Paper Planes"}, {"Slow Motion", "Odd Hours", "Late"},
		{"Glass", "Mara", "Shores"}, {"Northbound", "The Fields", "Maps"}, {"Static", "Vee", "Static"},
	}
	l := player.Listing{URI: "p:1", Ready: true, Length: len(names), Cached: len(names)}
	for i, n := range names {
		l.Tracks = append(l.Tracks, player.Track{URI: fmt.Sprintf("spotify:track:%d", i), Name: n.name, Artists: []string{n.artist}, Album: n.album, Duration: time.Duration(150+i*17) * time.Second})
	}
	s.listings["p:1"] = l
	s.tracks.SetCount(len(l.Tracks))
	s.tracks.SetHeight(12)
	s.tracks.Select(2)
	s.liked["spotify:track:0"], s.liked["spotify:track:4"] = true, true
	s.covers["c:1"] = art(color.RGBA{230, 80, 120, 255}, color.RGBA{60, 40, 160, 255})
	if path := os.Getenv("SMOLIFY_SNAPSHOT_COVER"); path != "" {
		// A real photo as the header cover, for judging the backdrop.
		if f, err := os.Open(path); err == nil {
			if im, _, err := image.Decode(f); err == nil {
				s.covers["c:1"] = shrink(im, coverPixels)
			}
			f.Close()
		}
	}
	s.tints["c:1"] = tintOf(s.covers["c:1"])
	s.thumbs["c:1"], s.thumbs["c:2"], s.thumbs["c:t"] = s.covers["c:1"], s.covers["c:2"], s.covers["c:t"]
	s.covers["c:2"] = art(color.RGBA{30, 200, 140, 255}, color.RGBA{20, 40, 60, 255})
	s.covers["c:t"] = art(color.RGBA{250, 180, 60, 255}, color.RGBA{120, 30, 60, 255})
	s.track = &player.Track{URI: "spotify:track:0", Name: "OverThink", Artists: []string{"Kuo", "Lin"}, Duration: 213 * time.Second, Cover: "c:t", ContextURI: "p:1"}
	s.context, s.playing, s.position, s.volume = "p:1", true, 74*time.Second, 60
	return a
}

// art is a 300×300 diagonal gradient with a soft disc, standing in for
// cover art.
func art(a, b color.RGBA) image.Image {
	const n = 300
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			t := float64(x+y) / (2 * n)
			mix := func(p, q uint8) uint8 { return uint8(float64(p) + (float64(q)-float64(p))*t) }
			c := color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
			if math.Hypot(float64(x)-n*0.62, float64(y)-n*0.4) < n*0.22 {
				c = color.RGBA{255, 240, 220, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestWindowSortSnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	a := sampleApp()
	a.State.trackSort.Open()
	img, err := window.Snapshot(a, snapOptions(110, 40), 2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "smolify-sort.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestWindowPendingPlaybackSnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	a := sampleApp()
	s := a.State
	track := s.listings[s.shown].Tracks[1]
	s.tracks.Select(1)
	s.pending = &playIntent{track: track, contextURI: s.shown, shown: s.shown, spinner: flatui.NewSpinner(flatui.SpinnerDots)}
	img, err := window.Snapshot(a, snapOptions(110, 34), 2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "smolify-loading.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestWindowPlaylistFilterSnapshot(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("set SMOLIFY_SNAPSHOT=<dir> to render")
	}
	a := sampleApp()
	s := a.State
	changeTrackFilter(s, func(field *flatui.TextField) { *field = flatui.TextField{Value: "mara", Cursor: 4} })
	s.focus = paneTrackFilter
	img, err := window.Snapshot(a, snapOptions(110, 34), 2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "smolify-playlist-filter.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
