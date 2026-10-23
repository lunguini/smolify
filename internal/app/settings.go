package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/player"
)

// Settings are what the Settings page changes, kept in settings.json next
// to the player's state.
type Settings struct {
	// NotifyErrors posts errors and confirmations as notifications (in a
	// window, which shows them nowhere else).
	NotifyErrors bool `json:"notify_errors"`
	// NotifyTracks posts each new track while Smolify is not in front.
	NotifyTracks bool `json:"notify_tracks"`

	Bitrate   int  `json:"bitrate"` // kbps: 96, 160, or 320
	Normalise bool `json:"normalise"`
	Crossfade int  `json:"crossfade_seconds"`
	Autoplay  bool `json:"autoplay"`

	DeviceName string `json:"device_name"`
	Compact    bool   `json:"compact_rows"`
}

// DefaultSettings are a first launch's settings.
func DefaultSettings() Settings {
	return Settings{NotifyErrors: true, NotifyTracks: true, Bitrate: 320, Normalise: true, Autoplay: true, DeviceName: "Smolify"}
}

// Audio is how the settings ask the player to play.
func (st Settings) Audio() player.Audio {
	return player.Audio{Bitrate: st.Bitrate, Normalise: st.Normalise, Crossfade: time.Duration(st.Crossfade) * time.Second, Autoplay: st.Autoplay}
}

// SettingsPath is where settings live in dir (the player's state dir).
func SettingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

// LoadSettings reads the settings at path: the defaults when there are
// none yet, with any setting the file lacks at its default.
func LoadSettings(path string) (Settings, error) {
	st := DefaultSettings()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return DefaultSettings(), err
	}
	if strings.TrimSpace(st.DeviceName) == "" {
		st.DeviceName = DefaultSettings().DeviceName
	}
	return st, nil
}

// saveSettings writes st to path atomically (0600).
func saveSettings(path string, st Settings) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WithSettings is app with settings st, saved to path as they change
// ("": not saved, as in tests).
func WithSettings(app flatte.App[State], path string, st Settings) flatte.App[State] {
	s := app.State
	s.settings, s.settingsPath, s.compact = st, path, st.Compact
	s.deviceName = flatui.TextField{Value: st.DeviceName, Cursor: len(st.DeviceName)}
	return app
}

// The settings, as rows of the Settings page: one ID each, clicked or
// changed with the keyboard.
const (
	setNotifyErrors = "set:notify-errors"
	setNotifyTracks = "set:notify-tracks"
	setQuality      = "set:quality"
	setNormalise    = "set:normalise"
	setCrossfade    = "set:crossfade"
	setAutoplay     = "set:autoplay"
	setDeviceName   = "set:device-name"
	setCompact      = "set:compact"
	setSignOut      = "set:sign-out"
)

// settingIDs are the rows in page order.
var settingIDs = []string{setNotifyErrors, setNotifyTracks, setQuality, setNormalise, setCrossfade, setAutoplay, setDeviceName, setCompact, setSignOut}

// The choices of the two multi-way settings, in order.
var (
	qualities  = []choice{{"Low", 96}, {"Normal", 160}, {"High", 320}}
	crossfades = []choice{{"Off", 0}, {"2 s", 2}, {"5 s", 5}, {"10 s", 10}}
)

type choice struct {
	label string
	value int
}

// pick is the index of value among cs (the last when absent).
func pickOf(cs []choice, value int) int {
	for i, c := range cs {
		if c.value == value {
			return i
		}
	}
	return len(cs) - 1
}

// changeSetting changes setting id: a toggle flips, a choice moves by
// step (wrapping; a click on choice n passes n through choose instead).
func changeSetting(s *State, id string, step int) {
	st := s.settings
	switch id {
	case setNotifyErrors:
		st.NotifyErrors = !st.NotifyErrors
	case setNotifyTracks:
		st.NotifyTracks = !st.NotifyTracks
	case setNormalise:
		st.Normalise = !st.Normalise
	case setAutoplay:
		st.Autoplay = !st.Autoplay
	case setCompact:
		st.Compact = !st.Compact
	case setQuality:
		st.Bitrate = qualities[wrap(pickOf(qualities, st.Bitrate)+step, len(qualities))].value
	case setCrossfade:
		st.Crossfade = crossfades[wrap(pickOf(crossfades, st.Crossfade)+step, len(crossfades))].value
	case setDeviceName:
		s.focus = paneDevice
		return
	case setSignOut:
		confirmSignOut(s)
		return
	default:
		return
	}
	applySettings(s, st)
}

// chooseSetting sets a choice setting to its option n (a click on it).
func chooseSetting(s *State, id string, n int) {
	st := s.settings
	switch {
	case id == setQuality && n >= 0 && n < len(qualities):
		st.Bitrate = qualities[n].value
	case id == setCrossfade && n >= 0 && n < len(crossfades):
		st.Crossfade = crossfades[n].value
	default:
		return
	}
	applySettings(s, st)
}

func wrap(i, n int) int { return ((i % n) + n) % n }

// applySettings makes st the settings: saved, and passed on to whatever
// they control — the rows at once, the player (restarting its session) when
// the audio changed, its Connect name when that did.
func applySettings(s *State, st Settings) {
	old := s.settings
	s.settings, s.compact = st, st.Compact // row heights follow at the next layout
	if st.Audio() != old.Audio() {
		a := st.Audio()
		send(s, "change-audio", func(ctx context.Context, be Backend) error { return be.Configure(ctx, a) })
	}
	if st.DeviceName != old.DeviceName {
		name := st.DeviceName
		send(s, "rename-device", func(ctx context.Context, be Backend) error { return be.SetDeviceName(ctx, name) })
	}
	if path := s.settingsPath; path != "" {
		flatte.Go(s.fx, "save-settings", func(context.Context) (struct{}, error) {
			return struct{}{}, saveSettings(path, st)
		}, func(s *State, _ struct{}, err error) {
			if err != nil {
				fail(s, "Could not save the settings: "+err.Error())
			}
		})
	}
}

// confirmSignOut asks before forgetting the sign-in.
func confirmSignOut(s *State) {
	flatte.Alert(s.fx, "sign-out", flatte.AlertDialog{
		Title:   "Sign out of Spotify?",
		Message: "Smolify forgets this device's sign-in and asks you to pair it again.",
		Buttons: []string{"Sign Out", "Cancel"},
	}, func(s *State, button int) {
		if button != 0 {
			return
		}
		s.ready, s.loaded, s.playlists, s.track, s.playing = false, false, nil, nil, false
		s.rail.SetCount(0)
		s.page, s.focus = pageLibrary, paneLibrary
		send(s, "sign-out", func(ctx context.Context, be Backend) error { return be.SignOut(ctx) })
	})
}

// openSettings shows the Settings page.
func openSettings(s *State) {
	if s.page != pageSettings {
		visit(s)
	}
	s.page, s.focus = pageSettings, paneSettings
}

// settingsKey moves through the settings and changes them: ↑/↓ pick a row,
// Space/Enter flip or step it, ←/→ step a choice, Esc goes back.
func settingsKey(s *State, k flatte.KeyEvent) bool {
	i := max(s.settingCursor, 0)
	switch k.Key {
	case flatte.KeyUp:
		s.settingCursor = max(i-1, 0)
		revealSetting(s)
	case flatte.KeyDown:
		s.settingCursor = min(i+1, len(settingIDs)-1)
		revealSetting(s)
	case flatte.KeyLeft:
		changeSetting(s, settingIDs[i], -1)
	case flatte.KeyRight, flatte.KeyEnter:
		changeSetting(s, settingIDs[i], 1)
	case flatte.KeyCharacter:
		if k.Rune != ' ' || k.Mod != 0 {
			return false
		}
		changeSetting(s, settingIDs[i], 1)
	case flatte.KeyEscape:
		travel(s, false)
	default:
		return false
	}
	return true
}

// deviceKey edits the device name: Enter keeps it, Esc puts it back.
func deviceKey(s *State, k flatte.KeyEvent, fx flatte.Effects[State]) {
	switch k.Key {
	case flatte.KeyEnter:
		name := strings.TrimSpace(s.deviceName.Value)
		if name == "" {
			name = s.settings.DeviceName
		}
		s.deviceName = flatui.TextField{Value: name, Cursor: len(name)}
		s.focus = paneSettings
		if name != s.settings.DeviceName {
			st := s.settings
			st.DeviceName = name
			applySettings(s, st)
		}
	case flatte.KeyEscape, flatte.KeyTab:
		s.deviceName = flatui.TextField{Value: s.settings.DeviceName, Cursor: len(s.settings.DeviceName)}
		s.focus = paneSettings
	default:
		if clipboardKey(&s.deviceName, k, fx) {
			return
		}
		if k.Key == flatte.KeyCharacter && k.Mod&^flatte.ModShift == 0 && len(s.deviceName.Value) >= 64 {
			return // long enough
		}
		s.deviceName.Edit(k)
	}
}

// settingsView is the Settings page: sections of rows, each a name and a
// line about it on the left and its control on the right.
func settingsView(s *State, g bool) layout.Node {
	st := s.settings
	cursor := ""
	if s.focus == paneSettings && s.settingCursor >= 0 && s.settingCursor < len(settingIDs) {
		cursor = settingIDs[s.settingCursor]
	}
	row := func(id, label, about string, control layout.Node) layout.Node {
		r := layout.Row{NodeBase: layout.NodeBase{ID: id, Gap: 2, PadLeft: 1, PadRight: 1, Box: layout.Box{Hover: hoverBg, Round: true, Selected: id == cursor, InsetY: rowGap}}, Align: layout.AlignCenter, Children: []layout.Node{
			twoLines(layout.NodeBase{W: layout.Grow(1)}, label, layout.TextStyle{}, about, secondary),
			control,
		}}
		if g {
			r.PadTop, r.PadBottom = 0, 0
			r.H = layout.Points(48)
		}
		return r
	}
	toggle := func(id string, on bool) layout.Node {
		return layout.Toggle{NodeBase: layout.NodeBase{ID: id + ":toggle"}, Kind: layout.ToggleSwitch, Checked: on}
	}
	choices := func(id string, cs []choice, value int) layout.Node {
		at := pickOf(cs, value)
		r := layout.Row{NodeBase: layout.NodeBase{Gap: 1}, Align: layout.AlignCenter}
		for i, c := range cs {
			b := layout.Button{NodeBase: layout.NodeBase{ID: id + ":" + strconv.Itoa(i)}, Label: c.label, Plain: i != at}
			if i == at {
				b.Style = layout.TextStyle{Bg: brand, Fg: ink}
				b.Default = true
			}
			r.Children = append(r.Children, b)
		}
		return r
	}
	name := s.deviceName.Layout(setDeviceName+":field", s.focus == paneDevice)
	name.W = layout.Fixed(24)
	section := func(title string) layout.Node {
		return layout.Col{NodeBase: layout.NodeBase{PadTop: 1, PadLeft: 1}, Children: []layout.Node{layout.Label{Text: title, Style: caption}}}
	}
	body := []layout.Node{
		layout.Label{Text: "Settings", Style: layout.TextStyle{Bold: true, Size: 30, Fg: white}},
		section("NOTIFICATIONS"),
		row(setNotifyErrors, "Errors and confirmations", "As notifications instead of on screen, in a window", toggle(setNotifyErrors, st.NotifyErrors)),
		row(setNotifyTracks, "Now playing", "Each new song while Smolify is in the background", toggle(setNotifyTracks, st.NotifyTracks)),
		section("AUDIO QUALITY"),
		row(setQuality, "Streaming quality", "96, 160, or 320 kbps, from the next song", choices(setQuality, qualities, st.Bitrate)),
		section("PLAYBACK"),
		row(setNormalise, "Normalize volume", "The same loudness for every song", toggle(setNormalise, st.Normalise)),
		row(setCrossfade, "Crossfade", "Blend the end of a song into the next", choices(setCrossfade, crossfades, st.Crossfade)),
		row(setAutoplay, "Autoplay", "Keep playing similar songs when a list ends", toggle(setAutoplay, st.Autoplay)),
		section("DEVICE AND ACCOUNT"),
		row(setDeviceName, "Device name", "How Smolify shows in Spotify Connect (Enter to keep)", name),
		row(setCompact, "Compact rows", "One line per song", toggle(setCompact, st.Compact)),
		row(setSignOut, "Sign out", "Forget this device's sign-in", layout.Button{NodeBase: layout.NodeBase{ID: setSignOut + ":button"}, Label: "Sign Out", Style: layout.TextStyle{Fg: danger}}),
	}
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Grow(1), Pad: pick(g, 2, 1)}, Children: []layout.Node{
		layout.Scroll{NodeBase: layout.NodeBase{ID: idSettingsScroll, W: layout.Grow(1), H: layout.Grow(1)}, OffsetY: s.settingsScroll, Child: layout.Col{NodeBase: layout.NodeBase{ID: idSettingsContent, W: layout.Grow(1)}, Children: body}},
	}}
}

// clampSettings bounds the page's scroll by its content, as last laid out
// (positions are read content-relative, so they hold at any scroll).
func clampSettings(s *State) {
	view, okV := s.settingRects[idSettingsScroll]
	content, okC := s.settingRects[idSettingsContent]
	if !okV || !okC {
		return
	}
	s.settingsMax = max(content.H-view.H, 0)
	s.settingsScroll = layout.ClampScroll(s.settingsScroll, view.H, content.H)
}

// revealSetting scrolls the keyboard's row into view.
func revealSetting(s *State) {
	view, okV := s.settingRects[idSettingsScroll]
	content, okC := s.settingRects[idSettingsContent]
	row, okR := s.settingRects[settingIDs[s.settingCursor]]
	if !okV || !okC || !okR {
		return
	}
	top := row.Y - content.Y
	switch {
	case s.settingCursor == 0:
		s.settingsScroll = 0 // the first row brings back the page's title
	case top < s.settingsScroll:
		s.settingsScroll = top
	case top+row.H > s.settingsScroll+view.H:
		s.settingsScroll = top + row.H - view.H
	}
	clampSettings(s)
}

// settingClicked handles a click in the Settings page: on a row, its
// control, or a choice.
func settingClicked(s *State, target string) bool {
	if !strings.HasPrefix(target, "set:") {
		return false
	}
	id, part, _ := strings.Cut(strings.TrimPrefix(target, "set:"), ":")
	id = "set:" + id
	for i, sid := range settingIDs {
		if sid == id {
			s.settingCursor = i
		}
	}
	s.focus = paneSettings
	if n, err := strconv.Atoi(part); err == nil {
		chooseSetting(s, id, n)
		return true
	}
	switch {
	case id == setDeviceName:
		s.focus = paneDevice
	case part == "" && (id == setQuality || id == setCrossfade):
		// A click on the row, not a choice: nothing to change.
	default:
		changeSetting(s, id, 1)
	}
	return true
}
