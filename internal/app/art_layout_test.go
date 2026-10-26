package app

import (
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"
	"testing"
)

func TestFallbackThumbnailMusicNoteIsCentered(t *testing.T) {
	for _, url := range []string{"", "https://not-loaded"} {
		tile := cover(&State{}, url, 32)
		scene := layout.LayoutScene(tile, 32, 32, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
		found := false
		for _, raw := range scene.Ops {
			if op, ok := raw.(layout.TextOp); ok && op.Text == "♫" {
				found = true
				if op.Rect.X+op.Rect.W/2 != 16 || op.Rect.Y+op.Rect.H/2 != 16 {
					t.Fatalf("note not centered for %q: %v", url, op.Rect)
				}
			}
		}
		if !found {
			t.Fatal("missing fallback note")
		}
		frame := layout.Compose(tile, 4, 2).Frame
		if got := flatest.CleanFrame(frame); got != coverText(32) {
			t.Fatalf("terminal thumbnail changed: %q", got)
		}
	}
}
