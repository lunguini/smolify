package app

import (
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

func TestNewTracksAreToldOnlyWhileAway(t *testing.T) {
	d, _ := ready(t)
	event(d, song)
	if n := d.Notifications(); len(n) != 0 {
		t.Fatalf("told while in front: %v", n)
	}
	d.Send(flatte.FocusEvent{Focused: false})
	event(d, song) // the same track again (a seek, say): nothing new
	next := song
	next.URI, next.Name, next.Artists = "spotify:track:2", "Gravity", []string{"Dua"}
	event(d, next)
	if n := d.Notifications(); len(n) != 1 || n[0] != (flatte.Notification{Title: "Gravity", Body: "Dua"}) {
		t.Fatalf("notifications %v", n)
	}
	d.Send(flatte.FocusEvent{Focused: true})
	event(d, song)
	if n := d.Notifications(); len(n) != 1 {
		t.Fatalf("told in front again: %v", n)
	}
}

func TestAWindowTellsErrorsAsNotifications(t *testing.T) {
	f := &fake{playlists: library, status: player.Status{Volume: 40, VolumeSteps: 100}, listings: map[string]player.Listing{"spotify:playlist:a": bassHouse}}
	d := flatest.Start(Windowed(New(f, &covers{}, f, nil)), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	s := d.State()
	fail(s, "Could not search: offline")
	fail(s, "Could not search: offline") // the same again: once
	tell(s, "Added “Glass” to the queue.")
	want := []flatte.Notification{{Title: "Smolify", Body: "Could not search: offline"}, {Title: "Smolify", Body: "Added “Glass” to the queue."}}
	if n := d.Notifications(); len(n) != 2 || n[0] != want[0] || n[1] != want[1] {
		t.Fatalf("notifications %v", n)
	}
	// A terminal app keeps them on screen and posts nothing.
	d2, _ := ready(t)
	fail(d2.State(), "Could not search: offline")
	if len(d2.Notifications()) != 0 {
		t.Fatal("a terminal app notified an error")
	}
}

func TestAWindowShowsNoErrorBanner(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.status = "Could not list the tracks: offline"
	text := func(g bool) string {
		return labels(View(s, flatte.RenderContext{Width: 100, Graphical: g}).Root)
	}
	if !strings.Contains(text(false), s.status) {
		t.Fatal("the terminal lost the error line")
	}
	if strings.Contains(text(true), s.status) {
		t.Fatal("a window still shows the error line")
	}
}

// labels is the text of every Label in a tree, one per line.
func labels(n layout.Node) string {
	var b strings.Builder
	var walk func(n layout.Node)
	walk = func(n layout.Node) {
		switch n := n.(type) {
		case layout.Label:
			b.WriteString(n.Text + "\n")
		case layout.Row:
			for _, c := range n.Children {
				walk(c)
			}
		case layout.Col:
			for _, c := range n.Children {
				walk(c)
			}
		case layout.Scroll:
			walk(n.Child)
		}
	}
	walk(n)
	return b.String()
}

func TestSearchFieldEditsLikeAField(t *testing.T) {
	d, f := ready(t)
	f.results = map[string]spotify.Results{}
	s := d.State()
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "daft punk")
	// ⌘← goes to the start of the text, not to the previous track; ⌘A
	// selects all; typing replaces it.
	for _, k := range []flatte.KeyEvent{key(flatte.KeyLeft, flatte.ModSuper), char('a', flatte.ModSuper)} {
		d.Send(k)
	}
	if s.query.SelectedText() != "daft punk" || strings.Contains(strings.Join(f.calls, ","), "prev") {
		t.Fatalf("selection %q calls %v", s.query.SelectedText(), f.calls)
	}
	// While typing, the menus keep no editing chords.
	for _, m := range Menus(s) {
		for _, it := range m.Items {
			for _, k := range it.Keys {
				if editingKey(k.Key) {
					t.Errorf("%s keeps %+v while typing", it.Label, k)
				}
			}
		}
	}
	typeText(d, "air")
	if s.query.Value != "air" {
		t.Fatalf("query %q", s.query.Value)
	}
	// ⌥⌫ deletes a word.
	typeText(d, " moon")
	d.Send(key(flatte.KeyBackspace, flatte.ModAlt))
	if s.query.Value != "air " {
		t.Fatalf("query %q after ⌥⌫", s.query.Value)
	}
}
