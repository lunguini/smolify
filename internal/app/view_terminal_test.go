package app

import (
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
)

// In a terminal the search field is framed, its frame lights up in the
// brand color while it has the keyboard, and the app's name takes the
// rail's slot in the top bar.
func TestTerminalSearchFieldLightsUpWhenFocused(t *testing.T) {
	border := func(f pane) string {
		c := layout.Compose(topBar(&State{focus: f}, false), 120, 3)
		i := strings.Index(c.Frame, "╭")
		if i < 0 {
			t.Fatalf("no frame around the search field:\n%s", c.Frame)
		}
		return c.Frame[max(0, i-24):i]
	}
	if got := border(paneLibrary); !strings.Contains(got, "38;2;74;74;74m") {
		t.Errorf("unfocused frame color %q", got)
	}
	if got := border(paneSearch); !strings.Contains(got, "38;2;157;123;255m") {
		t.Errorf("focused frame color %q", got)
	}
	if c := layout.Compose(topBar(&State{}, false), 120, 3); !strings.Contains(c.Frame, "Smolify") {
		t.Errorf("no app name in the terminal top bar:\n%s", c.Frame)
	}
}

// The caret blinks as a bar while the search field has the keyboard.
func TestTerminalSearchCaretBlinks(t *testing.T) {
	d, _ := ready(t)
	if c := d.Frame().CursorStyle; c != nil && c.Blink {
		t.Fatalf("blinking caret without search focus: %+v", c)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: '/'})
	if c := d.Frame().CursorStyle; c == nil || !c.Blink || c.Shape != flatte.CursorShapeBar {
		t.Fatalf("search caret %+v, want a blinking bar", c)
	}
}
