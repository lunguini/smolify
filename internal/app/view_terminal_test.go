package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/model"
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

// A wide terminal frames the player's five controls and words Shuffle
// and Repeat; a narrow one keeps single glyphs so they all fit.
func TestTerminalPlayerControlsGrowWithWidth(t *testing.T) {
	s := sampleApp().State
	wide := layout.Compose(playerBar(s, false, 160), 160, 8)
	if n := strings.Count(wide.Frame, "╭"); n < 5 || len(wide.TerminalImages) < 5 {
		t.Errorf("wide player: %d frames, %d icon pictures\n%s", n, len(wide.TerminalImages), flatest.CleanFrame(wide.Frame))
	}
	narrow := layout.Compose(playerBar(s, false, 100), 100, 8)
	if strings.Contains(narrow.Frame, "╭") || !strings.Contains(flatest.CleanFrame(narrow.Frame), "⇄") {
		t.Errorf("narrow player is framed or lost its glyphs:\n%s", flatest.CleanFrame(narrow.Frame))
	}
}

func TestArrivingPagesKeepAListScrolledAwayFromItsSelection(t *testing.T) {
	s := &State{listings: map[string]model.Listing{}}
	var tracks []model.Track
	for i := range 100 {
		tracks = append(tracks, model.Track{URI: "spotify:track:" + strconv.Itoa(i)})
	}
	s.shown = "p"
	s.listings["p"] = model.Listing{URI: "p", Tracks: tracks[:50]}
	s.tracks.SetCount(50)
	s.tracks.SetHeight(10)
	s.tracks.Select(2)
	s.tracks.Scroll(30) // dragging the scroll bar down, away from row 2
	replaceListing(s, "p", model.Listing{URI: "p", Tracks: tracks})
	if s.tracks.Offset() != 30 || s.tracks.Cursor() != 2 {
		t.Fatalf("a page arriving moved the view to %d (selection %d), want it left at 30", s.tracks.Offset(), s.tracks.Cursor())
	}
	s.tracks.Scroll(-30) // back on the selection: arrivals keep it in view
	replaceListing(s, "p", model.Listing{URI: "p", Tracks: tracks})
	if s.tracks.Offset() != 0 {
		t.Fatalf("offset %d with the selection in view", s.tracks.Offset())
	}
}

func TestWideTerminalHeaderHoldsTheSongTools(t *testing.T) {
	s := sampleApp().State
	_, wide := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 160}), 160, 40)
	play, sort := wide.Rects[idPlayList], wide.Rects[idSort]
	if play.Y != sort.Y {
		t.Fatalf("wide: play at line %d, sort at %d; want one row", play.Y, sort.Y)
	}
	_, narrow := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 100}), 100, 40)
	if narrow.Rects[idSort].Y <= narrow.Rects[idPlayList].Y {
		t.Fatal("narrow: the song tools crowd the actions row")
	}
}

func TestTerminalSettingsRowsStayNarrow(t *testing.T) {
	s := sampleApp().State
	initAudioSettings(s)
	s.page, s.focus = pageSettings, paneSettings
	_, comp := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 200}), 200, 50)
	if w := comp.Rects[setQuality].W; w > settingsColumns {
		t.Fatalf("a setting is %d columns wide on a 200-column screen", w)
	}
}
