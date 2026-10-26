package app

import (
	"testing"

	"github.com/lunguini/flatte/flatui/layout"
)

func TestPlaybackSourceUsesIcon(t *testing.T) {
	scene := layout.LayoutScene(playerBar(sampleApp().State, true), 1100, 120, linkMetrics)
	for _, value := range scene.Ops {
		op, ok := value.(layout.ControlOp)
		if !ok || op.ID != idPlaybackSource {
			continue
		}
		if op.Icon != layout.IconLibrary || op.Label == "Playlist" {
			t.Fatalf("Source control is not an icon: %+v", op)
		}
		return
	}
	t.Fatal("Source control missing")
}

func TestDateColumnFitsFullDateInBothDensities(t *testing.T) {
	for _, compact := range []bool{false, true} {
		s := sampleApp().State
		s.compact = compact
		date := layout.Label{NodeBase: layout.NodeBase{ID: "date-value"}, Text: "Sep 30, 2026", Style: subtle}
		row := s.trackCells(true, true, layout.Label{}, layout.Label{}, layout.Label{Text: "Song"}, layout.Label{Text: "Album"}, date, layout.Label{}, layout.Label{})
		scene := layout.LayoutScene(row, 1000, 48, linkMetrics)
		r := scene.Rects["date-value"]
		if r.W < 96 {
			t.Fatalf("Date clipped in compact=%v: %+v", compact, r)
		}
	}
}
