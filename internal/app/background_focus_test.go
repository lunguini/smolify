package app

import (
	"strings"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/smolify/internal/player"
)

func focusClickNode(t *testing.T, d *flatest.Driver[State], id string, height int) {
	t.Helper()
	_, comp := flatte.ComposeFrame(d.Frame(), 100, height)
	r, ok := comp.Rects[id]
	if !ok || r.W == 0 || r.H == 0 {
		t.Fatalf("missing node %s: %v", id, r)
	}
	d.Send(flatte.MouseEvent{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: flatte.MouseLeft, Action: flatte.MousePress})
}

func focusClickBackground(t *testing.T, d *flatest.Driver[State], id string, height int) {
	t.Helper()
	_, comp := flatte.ComposeFrame(d.Frame(), 100, height)
	r, ok := comp.Rects[id]
	if !ok || r.W < 2 || r.H < 2 {
		t.Fatalf("missing background %s: %v", id, r)
	}
	x, y := r.X+1, r.Y+r.H-1
	target, _ := comp.HitTest(x, y)
	if _, row := rowIndex(id, target); row {
		t.Fatalf("background point hit item %s", target)
	}
	d.Send(flatte.MouseEvent{X: x, Y: y, Button: flatte.MouseLeft, Action: flatte.MousePress})
}

func TestOutsideSortClickReturnsKeyboardToPlayer(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	f.calls = nil
	focusClickNode(t, d, idSort, 30)
	if !d.State().trackSort.IsOpen() {
		t.Fatal("sort did not open")
	}
	focusClickBackground(t, d, idTracks, 30)
	if d.State().trackSort.IsOpen() || d.State().focus != panePlayer {
		t.Fatal("background kept dropdown focus")
	}
	d.Send(char(' ', 0))
	d.Settle()
	if d.State().trackSort.IsOpen() || strings.Join(f.calls, ",") != "playpause" {
		t.Fatalf("Space did not control player: %v", f.calls)
	}
	rail, track := d.State().rail.Cursor(), d.State().tracks.Cursor()
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	if d.State().rail.Cursor() != rail || d.State().tracks.Cursor() != track {
		t.Fatal("player arrows moved a list")
	}
}

func TestOutsideFieldsReturnToPlayerWithoutEditing(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	d.Send(flatte.MenuEvent{ID: cmdTrackFilter})
	d.Send(flatte.PasteEvent{Text: "Gravity"})
	focusClickBackground(t, d, idTracks, 30)
	if d.State().focus != panePlayer || d.State().trackFilter.Value != "Gravity" || !d.State().trackFilterOpen {
		t.Fatal("active filter lost visibility or kept keyboard")
	}
	f.calls = nil
	d.Send(char(' ', 0))
	d.Settle()
	if d.State().trackFilter.Value != "Gravity" || strings.Join(f.calls, ",") != "playpause" {
		t.Fatal("Space edited blurred filter")
	}
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	focusClickBackground(t, d, idTracks, 30)
	if d.State().focus != panePlayer {
		t.Fatal("global search retained keyboard")
	}
}

func TestExplicitClickIntentKeepsItsOwnFocus(t *testing.T) {
	d, f := ready(t)
	focusClickNode(t, d, idSort, 30)
	focusClickNode(t, d, idTracks+":1", 30)
	if d.State().focus != paneTracks || d.State().tracks.Cursor() != 1 || d.State().trackSort.IsOpen() {
		t.Fatal("song click lost intent")
	}
	d.Send(flatte.MenuEvent{ID: cmdTrackFilter})
	d.Send(char(' ', 0))
	if d.State().trackFilter.Value != " " || d.State().focus != paneTrackFilter || len(f.calls) != 0 {
		t.Fatal("focused search did not own Space")
	}
	focusClickNode(t, d, idPlay, 30)
	if d.State().focus != panePlayer {
		t.Fatal("play button click left text/dropdown focused")
	}
}

func TestDeviceFieldBackgroundReleasesKeyboard(t *testing.T) {
	d, f, _ := settingsDriver(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	d.Send(flatte.MenuEvent{ID: cmdSettings})
	focusClickNode(t, d, setDeviceName+":field", settingsHeight)
	before := d.State().deviceName.Value
	focusClickBackground(t, d, idSettingsScroll, settingsHeight)
	if d.State().focus != panePlayer {
		t.Fatal("device field retained keyboard on background click")
	}
	f.calls = nil
	d.Send(char(' ', 0))
	d.Settle()
	if d.State().deviceName.Value != before || strings.Join(f.calls, ",") != "playpause" {
		t.Fatalf("blurred device field handled Space: %v", f.calls)
	}
}
