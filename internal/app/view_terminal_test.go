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

func TestTerminalPlayerPiecesShareTheirLines(t *testing.T) {
	s := sampleApp().State
	for _, width := range []int{160, 100} {
		_, c := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: width}), width, 40)
		timeline, controls, volume := c.Rects["player-timeline"], c.Rects["player-controls"], c.Rects["player-volume-row"]
		var cover layout.Rect
		for _, r := range c.TerminalImages {
			if r.Rect.Y >= timeline.Y-1 && r.Rect.X < 4 { // the cover, at the left edge
				cover = r.Rect
			}
		}
		if volume.Y != timeline.Y || cover.Y != timeline.Y || cover.Y+cover.H != controls.Y+controls.H {
			t.Errorf("width %d: timeline %v controls %v volume %v cover %v", width, timeline, controls, volume, cover)
		}
	}
}

func TestEqualizerMovesWhilePlayingAndRests(t *testing.T) {
	seen := map[string]bool{}
	for f := range 12 {
		bars := equalizerBars(f, true)
		if n := len([]rune(bars)); n != 3 {
			t.Fatalf("frame %d is %d bars", f, n)
		}
		seen[bars] = true
	}
	if len(seen) < 6 {
		t.Fatalf("only %d distinct frames in 12", len(seen))
	}
	if equalizerBars(0, false) != equalizerBars(7, false) {
		t.Fatal("a paused equalizer moves")
	}
}

func TestPlayingRowIsDimlyOutlinedNotWashed(t *testing.T) {
	s := sampleApp().State
	fr, _ := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 160}), 160, 40)
	if !strings.Contains(fr.Content, "38;2;79;68;117m╭") {
		t.Fatal("no dim outline on the playing row")
	}
	if !strings.Contains(plain(fr), equalizerBars(0, s.playing)) {
		t.Fatalf("no equalizer for the playing song:\n%s", plain(fr))
	}
}

func TestFindOpensSmoothlyIntoTheFilterField(t *testing.T) {
	s := &State{}
	if filterWidth(s) != filterColumns {
		t.Fatal("a field not opening is not at its full width")
	}
	s.filterScope = &flatte.Scope{}
	last := 0
	for s.filterGrow = 0; s.filterGrow <= filterSteps; s.filterGrow++ {
		w := filterWidth(s)
		if w < last || w < findColumns || w > filterColumns {
			t.Fatalf("frame %d: width %d after %d", s.filterGrow, w, last)
		}
		last = w
	}
	if last != filterColumns {
		t.Fatalf("ends at %d, want %d", last, filterColumns)
	}
}

func TestTerminalTopBarStandsOnTheDivider(t *testing.T) {
	s := sampleApp().State
	fr, _ := flatte.ComposeFrame(View(s, flatte.RenderContext{Width: 160}), 160, 40)
	lines := strings.Split(plain(fr), "\n")
	// One line of keys under the terminal's top edge, sharing their sides;
	// the divider under them their bottom edge, crossing the rail's at ┼.
	d := barLines // the divider's line
	if len(lines) < d+2 || strings.Count(lines[d], "┴") != 3 || []rune(lines[d])[termRailWidth] != '┼' ||
		[]rune(lines[0])[termRailWidth] != '│' || []rune(lines[d+1])[termRailWidth] != '│' || strings.Contains(lines[0], "╭") ||
		!strings.Contains(lines[d-1], "Settings") {
		t.Fatalf("the keys do not share the borders:\n%s", strings.Join(lines[:d+2], "\n"))
	}
}
