//go:build !js || !wasm

package app

import (
	"context"
	"fmt"
	"github.com/lunguini/flatte/window"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

func TestHomeStartupAndCenteredSplash(t *testing.T) {
	s := New(nil, nil, nil, nil).State
	if s.page != pageHome || s.shown != uriHome {
		t.Fatal("Startup does not select Home")
	}
	scene := layout.LayoutScene(View(s, flatte.RenderContext{Width: 110, Graphical: true}).Root, 1100, 800, linkMetrics)
	stack := scene.Rects["loading-stack"]
	if stack.Y+stack.H/2 != 400 {
		t.Fatalf("Stack not vertically centered: %+v", stack)
	}
	for _, id := range []string{"loading-logo", "loading-name", "loading-message"} {
		r := scene.Rects[id]
		if r.W == 0 || r.H == 0 || r.X+r.W/2 != 550 {
			t.Fatalf("%s not horizontally centered: %+v", id, r)
		}
	}
	logo, name, message := scene.Rects["loading-logo"], scene.Rects["loading-name"], scene.Rects["loading-message"]
	if logo.Y+logo.H >= name.Y || name.Y+name.H >= message.Y {
		t.Fatal("Loading stack order/spacing is wrong")
	}
}

type countingHomeWeb struct {
	*catalogFake
	calls int
}

func (w *countingHomeWeb) Home(ctx context.Context) (spotify.HomeFeed, error) {
	w.calls++
	return w.catalogFake.Home(ctx)
}

func TestHomeReturnsReuseSessionData(t *testing.T) {
	f := &fake{playlists: library, status: player.Status{Username: "test-user", VolumeSteps: 100}, listings: map[string]player.Listing{library[0].URI: bassHouse}}
	w := &countingHomeWeb{catalogFake: &catalogFake{fake: f, feed: spotify.HomeFeed{Sections: []spotify.HomeSection{{Title: "For you", Items: []spotify.HomeItem{{URI: "spotify:playlist:mix", Name: "Daily Mix", Kind: "playlist"}}}}}}}
	d := flatest.Start(New(f, nil, w, nil), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	event(d, player.Ready{})
	if d.State().page != pageHome || w.calls != 1 {
		t.Fatalf("Startup Home requests=%d page=%v", w.calls, d.State().page)
	}
	for range 3 {
		d.State().page = pageLibrary
		showSelected(d.State())
		d.Settle()
		d.Send(flatte.MenuEvent{ID: cmdHome})
		d.Settle()
	}
	if w.calls != 1 {
		t.Fatalf("Returning to Home made %d requests", w.calls)
	}
	n := 0
	for _, read := range f.reads {
		if strings.HasPrefix(read, "recent ") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Recent history was reloaded %d times", n)
	}
}

func TestHomeGridUsesMeasuredPaneWidth(t *testing.T) {
	s := New(nil, nil, nil, nil).State
	s.discovery.HomeWidthPoints = 420
	// A cover fills its card's width, and the cards and their 12pt
	// gutters fill the pane: no frame around a cover, no slack beside a card.
	for _, width := range []float64{420, 1000, 1337} {
		s.discovery.HomeWidthPoints = width
		cols, card, _, _, art := homeGridGeometry(s, true, 110)
		if art != card {
			t.Errorf("%vpt: cover %d in a %dpt card", width, art, card)
		}
		if card < 150 || card > 210 {
			t.Errorf("%vpt: card %dpt", width, card)
		}
		if used := cols*card + (cols-1)*12; float64(used) > width || width-float64(used) >= float64(cols) {
			t.Errorf("%vpt: %d cards of %dpt use %dpt", width, cols, card, used)
		}
	}
	s.discovery.HomeWidthPoints = 420
	narrow, _, _, _, _ := homeGridGeometry(s, true, 110)
	s.discovery.HomeWidthPoints = 1000
	wide, _, _, _, _ := homeGridGeometry(s, true, 110)
	if narrow != 2 || wide <= 4 {
		t.Fatalf("columns: narrow=%d wide=%d", narrow, wide)
	}
}

func TestWindowHomeStartupPreview(t *testing.T) {
	dir := os.Getenv("SMOLIFY_SNAPSHOT")
	if dir == "" {
		t.Skip("opt-in native preview")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := sampleApp()
	a.State.ready = false
	write := func(name string, w, h int) {
		img, err := window.Snapshot(a, snapOptions(w, h), 2)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, img); err != nil {
			t.Fatal(err)
		}
	}
	write("smolify-loading.png", 110, 34)
	s := a.State
	s.ready = true
	s.page = pageHome
	s.shown = uriHome
	s.focus = paneTracks
	s.covers["preview-cover"] = s.covers["c:1"]
	var items []spotify.HomeItem
	for i, name := range []string{"Daily Mix 1", "Daily Mix 2", "Daily Mix 3", "Discover Weekly", "Release Radar", "On Repeat"} {
		items = append(items, spotify.HomeItem{URI: fmt.Sprintf("spotify:playlist:preview%d", i), Name: name, Kind: "playlist", Image: "preview-cover"})
	}
	s.discovery.Home = spotify.HomeFeed{Sections: []spotify.HomeSection{{Title: "Made for you", Items: items}}}
	write("smolify-home-wide.png", 140, 34)
	write("smolify-home-narrow.png", 90, 34)
}

// In a window a Home card is its cover, full width and flush to the top,
// over a short strip for the name and kind; the cards fill the row.
func TestWindowHomeCardsAreTheirCovers(t *testing.T) {
	s := sampleApp().State
	s.discovery.Home = spotify.HomeFeed{Sections: []spotify.HomeSection{{Title: "Made for you", Items: []spotify.HomeItem{
		{URI: "a", Name: "Daily Mix 1", Kind: "Playlist", Image: "c:1"}, {URI: "b", Name: "Discover", Kind: "Playlist", Image: "c:2"},
		{URI: "c", Name: "Shores", Kind: "Album", Image: "c:t"}, {URI: "d", Name: "Radar", Kind: "Playlist"}}}}}
	s.page = pageHome
	m := layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32}
	scene := layout.LayoutScene(View(s, flatte.RenderContext{Graphical: true, Width: 137}).Root, 1100, 800, m)
	s.discovery.HomeWidthPoints = scene.Rects[idHomeContent].W // as the LayoutEvent reports it
	scene = layout.LayoutScene(View(s, flatte.RenderContext{Graphical: true, Width: 137}).Root, 1100, 800, m)
	content := scene.Rects[idHomeContent]
	cols, card, _, _, art := homeGridGeometry(s, true, 137)
	first, last := scene.Rects[homeCardID(0)], scene.Rects[homeCardID(cols-1)]
	if first.X != content.X || last.X+last.W != content.X+float64(cols*card+(cols-1)*12) || content.X+content.W-(last.X+last.W) >= float64(cols) {
		t.Errorf("cards %v … %v in content %v", first, last, content)
	}
	if art != card || first.W != float64(card) {
		t.Errorf("cover %d in a %v card", art, first)
	}
	if strip := first.H - float64(art); strip < 30 || strip > 52 {
		t.Errorf("name strip %vpt", strip)
	}
}
