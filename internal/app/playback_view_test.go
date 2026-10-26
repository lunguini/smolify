package app

import (
	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
	"testing"
)

func TestPendingPlayHighlightsRequestedRowAndReplacesPlayGlyph(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: s.shown})
	s.tracks.Select(1)
	command(s, cmdPlayTrack, s.fx)
	requested := trackRow(s, 1, bassHouse.Tracks[1], true, false).(layout.Row)
	old := trackRow(s, 0, bassHouse.Tracks[0], true, false).(layout.Row)
	if requested.Box.Background != playingBg || old.Box.Background != nil {
		t.Fatal("requested row not the loading highlight")
	}
	scene := layout.LayoutScene(playerBar(s, true), 1100, 120, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
	found := false
	for _, op := range scene.Ops {
		if c, ok := op.(layout.ControlOp); ok && c.ID == idPlay {
			found = true
			if c.Icon != layout.IconNone || c.Label != loadingIndicator(s) || !c.Disabled {
				t.Fatalf("loading play control %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("no play control")
	}
	if item := menuItem(t, s, cmdPlayPause); !item.Disabled || item.Label != "Loading…" {
		t.Fatalf("pending menu %+v", item)
	}
	event(d, player.Playing{URI: bassHouse.Tracks[1].URI, ContextURI: s.shown})
	if loadingPlay(s) {
		t.Fatal("still loading after matching start")
	}
}

func TestPendingPlaybackSnapshot(t *testing.T) {
	d, _ := ready(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	d.State().tracks.Select(1)
	frame := d.Send(flatte.MenuEvent{ID: cmdPlayTrack})
	flatest.AssertGoldenFrame(t, "testdata/play-loading.golden", frame)
}

func TestPlayerFocusIsReflectedInPlayControl(t *testing.T) {
	d, _ := ready(t)
	s := d.State()
	s.focus = panePlayer
	scene := layout.LayoutScene(playerBar(s, true), 1100, 120, layout.Metrics{Column: 8, Line: 20, Step: 4, Border: 1, DefaultPadding: true, ControlHeight: 32})
	for _, raw := range scene.Ops {
		if op, ok := raw.(layout.ControlOp); ok && op.ID == idPlay {
			if !op.Focused {
				t.Fatal("player focus has no visible focus indicator")
			}
			return
		}
	}
	t.Fatal("missing play control")
}
