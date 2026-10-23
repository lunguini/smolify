package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/smolify/internal/player"
)

func TestSettingsLoadAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := LoadSettings(path)
	if err != nil || st != DefaultSettings() {
		t.Fatalf("no file: %+v %v", st, err)
	}
	st.Bitrate, st.Crossfade, st.DeviceName = 160, 5, "Desk"
	if err := saveSettings(path, st); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	if back, err := LoadSettings(path); err != nil || back != st {
		t.Fatalf("read back %+v %v", back, err)
	}
	// A file from an older Smolify keeps the defaults it lacks.
	os.WriteFile(path, []byte(`{"bitrate": 96}`), 0o600)
	if back, _ := LoadSettings(path); back.Bitrate != 96 || !back.NotifyTracks || back.DeviceName != "Smolify" {
		t.Fatalf("partial %+v", back)
	}
	os.WriteFile(path, []byte(`{`), 0o600)
	if back, err := LoadSettings(path); err == nil || back != DefaultSettings() {
		t.Fatalf("broken file: %+v %v", back, err)
	}
	if a := (Settings{Bitrate: 160, Normalise: true, Crossfade: 2}).Audio(); a != (player.Audio{Bitrate: 160, Normalise: true, Crossfade: 2 * time.Second}) {
		t.Fatalf("audio %+v", a)
	}
}

func settingsDriver(t *testing.T) (*flatest.Driver[State], *fake, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	f := &fake{playlists: library, status: player.Status{Volume: 40, VolumeSteps: 100}, listings: map[string]player.Listing{"spotify:playlist:a": bassHouse}}
	d := flatest.Start(WithSettings(New(f, &covers{}, f, nil), path, DefaultSettings()), 100)
	d.State().searchDelay = 0
	d.Send(flatte.ResizeEvent{Width: 100, Height: settingsHeight})
	event(d, player.Ready{})
	f.calls = nil
	return d, f, path
}

// settingsHeight is tall enough for the whole Settings page.
const settingsHeight = 60

// click presses the left button on the middle of node id, as the user
// would (the driver hit-tests the point).
func click(t *testing.T, d *flatest.Driver[State], id string) {
	t.Helper()
	_, comp := flatte.ComposeFrame(d.Frame(), 100, settingsHeight)
	r, ok := comp.Rects[id]
	if !ok {
		t.Fatalf("no %q on screen", id)
	}
	d.Send(flatte.MouseEvent{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: flatte.MouseLeft, Action: flatte.MousePress})
	d.Send(flatte.MouseEvent{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: flatte.MouseLeft, Action: flatte.MouseRelease})
	d.Settle()
}

func TestSettingsPageByClickAndKeys(t *testing.T) {
	d, f, path := settingsDriver(t)
	s := d.State()
	click(t, d, idSettings)
	if s.page != pageSettings || s.focus != paneSettings {
		t.Fatalf("page %v focus %v", s.page, s.focus)
	}
	fr := plain(d.Frame())
	for _, want := range []string{"Settings", "NOTIFICATIONS", "Streaming quality", "Crossfade", "Device name", "Sign Out"} {
		if !strings.Contains(fr, want) {
			t.Errorf("page missing %q:\n%s", want, fr)
		}
	}
	// A click on a choice picks it, restarts the player with it, and saves.
	click(t, d, setQuality+":1")
	if s.settings.Bitrate != 160 || !strings.Contains(strings.Join(f.calls, ","), "configure 160 true 0s true") {
		t.Fatalf("bitrate %d calls %v", s.settings.Bitrate, f.calls)
	}
	if saved, _ := LoadSettings(path); saved.Bitrate != 160 {
		t.Fatalf("saved %+v", saved)
	}
	// Keys: ↓ to Normalize volume, space flips it; → steps crossfade.
	s.settingCursor = 0
	for range 3 {
		d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: ' '})
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	d.Send(flatte.KeyEvent{Key: flatte.KeyRight})
	d.Settle()
	if s.settings.Normalise || s.settings.Crossfade != 2 {
		t.Fatalf("normalise %v crossfade %d", s.settings.Normalise, s.settings.Crossfade)
	}
	// Notifications and compact rows touch only the app, not the player.
	calls := len(f.calls)
	click(t, d, setNotifyTracks+":toggle")
	click(t, d, setCompact)
	if s.settings.NotifyTracks || !s.compact || len(f.calls) != calls {
		t.Fatalf("notify %v compact %v calls %v", s.settings.NotifyTracks, s.compact, f.calls[calls:])
	}
	// Esc goes back where Settings was opened from.
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	if s.page != pageLibrary {
		t.Fatalf("esc: page %v", s.page)
	}
}

func TestSettingsDeviceNameAndSignOut(t *testing.T) {
	d, f, _ := settingsDriver(t)
	s := d.State()
	d.Send(flatte.MenuEvent{ID: cmdSettings})
	click(t, d, setDeviceName)
	if s.focus != paneDevice {
		t.Fatalf("focus %v", s.focus)
	}
	for range len("Smolify") {
		d.Send(flatte.KeyEvent{Key: flatte.KeyBackspace})
	}
	typeText(d, "Desk q") // q types, it does not quit
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Settle()
	if s.settings.DeviceName != "Desk q" || s.focus != paneSettings || d.Quit() || !strings.Contains(strings.Join(f.calls, ","), "rename Desk q") {
		t.Fatalf("name %q focus %v calls %v", s.settings.DeviceName, s.focus, f.calls)
	}
	// Esc while editing puts the name back.
	click(t, d, setDeviceName)
	typeText(d, "zz")
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	if s.deviceName.Value != "Desk q" || s.focus != paneSettings {
		t.Fatalf("esc: %q focus %v", s.deviceName.Value, s.focus)
	}
	// Sign out asks first; Cancel does nothing, Sign Out signs out.
	click(t, d, setSignOut+":button")
	if _, ok := d.Dialog(); !ok {
		t.Fatal("no confirmation")
	}
	d.Answer(flatte.DialogResult{Button: 1})
	d.Settle()
	if strings.Contains(strings.Join(f.calls, ","), "sign out") || !s.ready {
		t.Fatal("cancel signed out")
	}
	click(t, d, setSignOut+":button")
	d.Answer(flatte.DialogResult{Button: 0})
	d.Settle()
	if !strings.Contains(strings.Join(f.calls, ","), "sign out") || s.ready || len(s.playlists) != 0 {
		t.Fatalf("sign out: calls %v ready %v", f.calls, s.ready)
	}
}

func TestSettingsGateNotifications(t *testing.T) {
	f := &fake{playlists: library, status: player.Status{Volume: 40, VolumeSteps: 100}, listings: map[string]player.Listing{"spotify:playlist:a": bassHouse}}
	st := DefaultSettings()
	st.NotifyErrors, st.NotifyTracks = false, false
	d := flatest.Start(Windowed(WithSettings(New(f, &covers{}, f, nil), "", st)), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	s := d.State()
	fail(s, "Could not search: offline")
	d.Send(flatte.FocusEvent{Focused: false})
	event(d, song)
	if n := d.Notifications(); len(n) != 0 {
		t.Fatalf("notified with notifications off: %v", n)
	}
	// With error notifications off, a window shows the error on screen again.
	s.page, s.shown = pageLibrary, "spotify:playlist:a"
	if root := View(s, flatte.RenderContext{Width: 100, Graphical: true}).Root; !strings.Contains(labels(root), "Could not search: offline") {
		t.Fatal("the error is shown nowhere")
	}
}

func TestSettingsKeyboardScrollsInAShortTerminal(t *testing.T) {
	d, _, _ := settingsDriver(t)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	d.Send(flatte.MenuEvent{ID: cmdSettings})
	d.Settle()
	if strings.Contains(plain(d.Frame()), "Sign Out") {
		t.Skip("the page fits; nothing to scroll")
	}
	for range len(settingIDs) {
		d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
		d.Settle()
	}
	if fr := plain(d.Frame()); !strings.Contains(fr, "Sign Out") || d.State().settingsScroll == 0 {
		t.Fatalf("scroll %d, the last row not shown:\n%s", d.State().settingsScroll, fr)
	}
	for range len(settingIDs) {
		d.Send(flatte.KeyEvent{Key: flatte.KeyUp})
		d.Settle()
	}
	if d.State().settingsScroll != 0 {
		t.Fatalf("back at the top, scroll %d", d.State().settingsScroll)
	}
	// The wheel scrolls within limits.
	for range 50 {
		d.Send(flatte.MouseEvent{X: 60, Y: 15, Button: flatte.MouseWheelDown})
	}
	if s := d.State(); s.settingsScroll != s.settingsMax || s.settingsMax == 0 {
		t.Fatalf("wheel: scroll %d max %d", s.settingsScroll, s.settingsMax)
	}
}
