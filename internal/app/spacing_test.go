package app

import (
	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
	"strings"
	"testing"
)

func TestPlaylistHeaderIsFlushAndSummaryStaysByTitle(t *testing.T) {
	s := sampleApp().State
	root := View(s, flatte.RenderContext{Width: 110, Graphical: true}).Root
	scene := layout.LayoutScene(root, 1100, 800, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
	panel, head := scene.Rects["center-card"], scene.Rects["playlist-header"]
	if head.W == 0 || head.X != panel.X || head.Y != panel.Y || head.W != panel.W {
		t.Fatalf("header has outer padding: panel %v header %v", panel, head)
	}
	title, summary, actions := scene.Rects["playlist-title"], scene.Rects["playlist-summary"], scene.Rects["playlist-actions"]
	description := scene.Rects["playlist-description"]
	if description.Y-title.Y-title.H > 8 || summary.Y-description.Y-description.H > 8 || actions.Y-summary.Y-summary.H < 8 {
		t.Fatalf("summary not grouped with title: title %v summary %v actions %v", title, summary, actions)
	}
	brand, home := scene.Rects["brand"], scene.Rects[idHome]
	if brand.W == 0 || brand.X >= scene.Rects[idTracks].X || brand.Y >= home.Y {
		t.Fatalf("brand not above sidebar navigation: brand %v home %v", brand, home)
	}
}

func TestGUIRowsHaveVerticalRoomAndTerminalDensityStays(t *testing.T) {
	s := sampleApp().State
	scene := layout.LayoutScene(View(s, flatte.RenderContext{Graphical: true}).Root, 1100, 800, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
	if r := scene.Rects[idTracks+":0"]; r.H != 48 {
		t.Fatalf("GUI track row %v", r)
	}
	if r := scene.Rects[idRail+":0"]; r.H != 48 {
		t.Fatalf("GUI library row %v", r)
	}
	_, terminal := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 110}), 110, 40)
	if terminal.Rects[idTracks+":0"].H != 2 || terminal.Rects[idRail+":0"].H != 2 {
		t.Fatal("terminal rows changed density")
	}
	s.compact = true
	compact := layout.LayoutScene(View(s, flatte.RenderContext{Graphical: true}).Root, 1100, 800, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
	if compact.Rects[idTracks+":0"].H != 30 {
		t.Fatal("compact row lost its own height")
	}
}

func TestTerminalDescriptionDoesNotClipHeaderActions(t *testing.T) {
	s := sampleApp().State
	frame, _ := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 80}), 80, 30)
	text := plain(frame)
	if !strings.Contains(text, "Position ↑") || !strings.Contains(text, "[▶]") {
		t.Fatalf("header actions clipped:\n%s", text)
	}
}

func TestPlayerTimelineAboveControlsAndVolumeAligned(t *testing.T) {
	s := sampleApp().State
	m := layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32}
	scene := layout.LayoutScene(playerBar(s, true), 1100, 120, m)
	seek, volume, play := scene.Rects[idProgress], scene.Rects[idVolume], scene.Rects[idPlay]
	if seek.H == 0 || volume.H == 0 || play.H == 0 {
		t.Fatal("missing player controls")
	}
	if seek.Y+seek.H > play.Y {
		t.Fatalf("timeline is not above buttons: seek %v play %v", seek, play)
	}
	if seek.Y+seek.H/2 != volume.Y+volume.H/2 {
		t.Fatalf("volume and seek not aligned: seek %v volume %v", seek, volume)
	}
	frame, comp := flatte.ComposeFrame(flatte.Frame{Root: playerBar(s, false)}, 110, 8)
	if comp.Rects[idProgress].Y >= comp.Rects[idPlay].Y || comp.Rects[idProgress].Y != comp.Rects[idVolume].Y {
		t.Fatalf("terminal player misaligned:\n%s", plain(frame))
	}
}

func TestListsAdoptExactNativeMeasurements(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	e := flatte.LayoutEvent{
		Rects:      map[string]layout.Rect{idTracks: {H: 6}, idTracks + ":0": {H: 1}, idRail: {H: 6}, idRail + ":0": {H: 1}},
		PointRects: map[string]layout.PointRect{idTracks: {H: 100.9}, idTracks + ":0": {H: 30.4}, idRail: {H: 100.9}, idRail + ":0": {H: 30.4}},
	}
	Handle(s, e, s.fx)
	if s.tracks.Height() != 3 || s.rail.Height() != 3 {
		t.Fatalf("native rows tracks=%d rail=%d", s.tracks.Height(), s.rail.Height())
	}
}
