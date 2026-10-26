package app

import (
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"
)

func filterScene(s *State) layout.Scene {
	return layout.LayoutScene(View(s, flatte.RenderContext{Width: 110, Graphical: true}).Root, 850, 720, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
}

func TestPlaylistFilterStartsAsCompactIconAndExpandsBesideSort(t *testing.T) {
	s := sampleApp().State
	scene := filterScene(s)
	toggle, sort := scene.Rects[idTrackFilterToggle], scene.Rects[idSort]
	if toggle.W != 32 || toggle.H != 32 || sort.H != 32 || toggle.X+toggle.W > sort.X {
		t.Fatalf("collapsed filter/sort geometry: filter %v sort %v", toggle, sort)
	}
	if _, ok := scene.Rects[idTrackFilter]; ok {
		t.Fatal("filter input visible before opening")
	}
	openTrackFilter(s)
	scene = filterScene(s)
	field, box, sort := scene.Rects[idTrackFilter], scene.Rects[idTrackFilterBox], scene.Rects[idSort]
	if box.W != 180 || box.H != 32 || sort.H != 32 || field.W < 48 || box.X+box.W > sort.X {
		t.Fatalf("expanded filter/sort geometry: field %v box %v sort %v", field, box, sort)
	}
	s.trackSort.Open()
	scene = filterScene(s)
	if scene.Rects[idSort].H != 32 {
		t.Fatal("opening sort changed its button height")
	}
	// The compact tool row remains one line in a terminal.
	s.trackSort.Close()
	frame, comp := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 80}), 80, 30)
	if comp.Rects[idTrackFilterBox].H != 1 || comp.Rects[idSort].H != 1 {
		t.Fatalf("terminal controls not one line:\n%s", plain(frame))
	}
	s.trackFilter.Value = "no-matching-song-349810"
	if got := labels(View(s, flatte.RenderContext{Graphical: true}).Root); !strings.Contains(got, "No songs match your filter.") {
		t.Fatalf("empty filter not explained: %s", got)
	}
}

func TestPlaylistFilterOpensFromRenderedIconAndClosesOnEmptyBlur(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	click := func(id string) {
		_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
		r, ok := comp.Rects[id]
		if !ok || r.W == 0 || r.H == 0 {
			t.Fatalf("missing hit target %s: %v", id, r)
		}
		d.Send(flatte.MouseEvent{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: flatte.MouseLeft, Action: flatte.MousePress})
	}
	click(idTrackFilterToggle)
	if !s.trackFilterOpen || s.focus != paneTrackFilter {
		t.Fatal("filter icon did not expand/focus field")
	}
	click(idSort)
	if s.trackFilterOpen {
		t.Fatal("empty filter did not collapse on outside click")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	d.Send(flatte.MenuEvent{ID: cmdTrackFilter})
	if !s.trackFilterOpen || s.focus != paneTrackFilter {
		t.Fatal("shortcut did not expand/focus field")
	}
	d.Send(flatte.PasteEvent{Text: "Gravity"})
	click(idSort)
	if !s.trackFilterOpen || s.trackFilter.Value != "Gravity" || s.tracks.Count() != 1 {
		t.Fatal("blur hid or cleared active filter")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	d.Send(flatte.MenuEvent{ID: cmdTrackFilter})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	if s.trackFilterOpen || s.trackFilter.Value != "" || s.focus != paneTracks || s.tracks.Count() != 3 || d.Quit() {
		t.Fatal("Escape did not clear/collapse and restore list")
	}
}

func TestEmptyTrackFilterCollapsesOnKeyboardBlur(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	openTrackFilter(s)
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	if s.trackFilterOpen || s.focus != paneSort {
		t.Fatal("empty field did not collapse on keyboard blur")
	}
	openTrackFilter(s)
	d.Send(flatte.PasteEvent{Text: "Gravity"})
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	if !s.trackFilterOpen || s.focus != paneSort || s.trackFilter.Value != "Gravity" {
		t.Fatal("keyboard blur hid active query")
	}
}

func TestPlaylistFilterSnapshotPreservesOriginalPosition(t *testing.T) {
	d, _ := ready(t)
	d.Send(flatte.MenuEvent{ID: cmdTrackFilter})
	frame := d.Send(flatte.PasteEvent{Text: "Gravity"})
	flatest.AssertGoldenFrame(t, "testdata/playlist-filter.golden", frame)
}

func TestToolbarControlsFollowSharedNativeHeight(t *testing.T) {
	s := sampleApp().State
	metrics := layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 40}
	scene := layout.LayoutScene(View(s, flatte.RenderContext{Width: 110, Graphical: true}).Root, 850, 720, metrics)
	if scene.Rects[idTrackFilterToggle].H != 40 || scene.Rects[idTrackFilterToggle].W != 40 || scene.Rects[idSort].H != 40 {
		t.Fatal("toolbar copied a fixed regular control height")
	}
	openTrackFilter(s)
	scene = layout.LayoutScene(View(s, flatte.RenderContext{Width: 110, Graphical: true}).Root, 850, 720, metrics)
	if scene.Rects[idTrackFilterBox].H != 40 || scene.Rects[idSort].H != 40 {
		t.Fatal("expanded filter did not inherit control height")
	}
}

func TestToolbarRegularControlsGrowTogetherForBodyFont(t *testing.T) {
	s := sampleApp().State
	metrics := layout.Metrics{Column: 8, Line: 40, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32}
	root := func() layout.Node {
		return layout.Row{Align: layout.AlignCenter, Children: []layout.Node{filterView(s, true), sortView(s)}}
	}
	scene := layout.LayoutScene(root(), 600, 100, metrics)
	if scene.Rects[idTrackFilterToggle].H != 48 || scene.Rects[idSort].H != 48 {
		t.Fatal("regular icon and dropdown differ for larger body text")
	}
	openTrackFilter(s)
	scene = layout.LayoutScene(root(), 600, 100, metrics)
	if scene.Rects[idTrackFilterBox].H != 48 || scene.Rects[idSort].H != 48 {
		t.Fatal("regular field and dropdown differ for larger body text")
	}
}
