package app

import (
	"context"
	"errors"
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/player"
)

// fake is a Backend that records requests.
type fake struct {
	calls     []string
	playlists []player.Playlist
	status    player.Status
	fail      error
}

func (f *fake) Events() <-chan player.Event { return nil }
func (f *fake) Status(context.Context) (player.Status, error) {
	f.calls = append(f.calls, "status")
	return f.status, nil
}
func (f *fake) Playlists(context.Context) ([]player.Playlist, error) {
	f.calls = append(f.calls, "playlists")
	return f.playlists, f.fail
}
func (f *fake) record(c string) error { f.calls = append(f.calls, c); return f.fail }
func (f *fake) Play(_ context.Context, uri, skip string) error {
	return f.record("play " + uri + " " + skip)
}
func (f *fake) PlayPause(context.Context) error { return f.record("playpause") }
func (f *fake) Next(context.Context) error      { return f.record("next") }
func (f *fake) Prev(context.Context) error      { return f.record("prev") }
func (f *fake) Seek(_ context.Context, d time.Duration) error {
	return f.record("seek " + d.String())
}
func (f *fake) SetVolume(_ context.Context, v int) error {
	return f.record("volume " + strconv.Itoa(v))
}
func (f *fake) SetShuffle(_ context.Context, on bool) error { return f.record("shuffle " + bstr(on)) }
func (f *fake) SetRepeat(_ context.Context, on bool) error  { return f.record("repeat " + bstr(on)) }

func bstr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// covers is an Images that returns a 2×2 image for any URL.
type covers struct{ fetched []string }

func (c *covers) Fetch(_ context.Context, url string) (image.Image, error) {
	c.fetched = append(c.fetched, url)
	return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
}

var library = []player.Playlist{
	{URI: "spotify:playlist:a", Name: "bass house", Owner: "adrian", Length: 42, Image: "https://img/a"},
	{URI: "spotify:playlist:b", Name: "Car Music", Owner: "adrian", Length: 7, Description: "For the road."},
}

func start(t *testing.T) (*flatest.Driver[State], *fake, *covers) {
	t.Helper()
	f := &fake{playlists: library, status: player.Status{Volume: 40, VolumeSteps: 100, Paused: true}}
	c := &covers{}
	d := flatest.Start(New(f, c, nil), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	return d, f, c
}

// event delivers a player event as the stream would, and settles.
func event(d *flatest.Driver[State], ev player.Event) flatte.Frame {
	onPlayer(d.State(), ev)
	return d.Settle()
}

func plain(f flatte.Frame) string { return flatest.CleanFrame(f.Content) }

func TestPairingThenLibrary(t *testing.T) {
	d, f, c := start(t)
	if !strings.Contains(plain(d.Frame()), "Connecting to Spotify") {
		t.Fatalf("splash:\n%s", plain(d.Frame()))
	}
	fr := event(d, player.Pairing{URL: "https://spotify.com/pair?code=AB12", Code: "AB12", Expires: time.Date(2026, 1, 1, 13, 5, 0, 0, time.UTC)})
	for _, want := range []string{"Sign in to Spotify", "A B 1 2", "[ Open in Browser ]", "https://spotify.com/pair?code=AB12"} {
		if !strings.Contains(plain(fr), want) {
			t.Errorf("pairing screen missing %q:\n%s", want, plain(fr))
		}
	}
	fr = event(d, player.Ready{})
	s := d.State()
	if !s.ready || s.pairing.Code != "" || len(s.playlists) != 2 {
		t.Fatalf("after ready: ready %v pairing %+v playlists %d", s.ready, s.pairing, len(s.playlists))
	}
	if len(f.calls) < 2 || !strings.Contains(strings.Join(f.calls, ","), "playlists") || !strings.Contains(strings.Join(f.calls, ","), "status") {
		t.Fatalf("calls %v", f.calls)
	}
	if !strings.Contains(plain(fr), "Your Library  2") || !strings.Contains(plain(fr), "bass house") {
		t.Fatalf("library:\n%s", plain(fr))
	}
	// The selected playlist's cover is fetched and drawn.
	if len(c.fetched) != 1 || c.fetched[0] != "https://img/a" || s.covers["https://img/a"] == nil {
		t.Fatalf("covers %v", c.fetched)
	}
	// A second Ready (a reconnect) does not reload.
	event(d, player.Ready{})
	if n := strings.Count(strings.Join(f.calls, ","), "playlists"); n != 1 {
		t.Fatalf("library loaded %d times", n)
	}
}

func ready(t *testing.T) (*flatest.Driver[State], *fake) {
	d, f, _ := start(t)
	event(d, player.Ready{})
	f.calls = nil
	return d, f
}

func TestBrowseAndPlay(t *testing.T) {
	d, f := ready(t)
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	fr := d.Frame()
	if !strings.Contains(plain(fr), "Car Music") || !strings.Contains(plain(fr), "For the road.") || !strings.Contains(plain(fr), "adrian · 7 songs") {
		t.Fatalf("selected playlist:\n%s", plain(fr))
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:playlist:b " {
		t.Fatalf("calls %v", f.calls)
	}
	// Space with nothing loaded plays the selected playlist too.
	f.calls = nil
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: ' '})
	d.Settle()
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "play ") {
		t.Fatalf("space: %v", f.calls)
	}
}

var song = player.Track{URI: "spotify:track:1", Name: "Overthink", Artists: []string{"Kuo", "Lin"}, Duration: 200 * time.Second, Position: 10 * time.Second, ContextURI: "spotify:playlist:a", Cover: "https://img/t"}

func TestNowPlaying(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	fr := d.Frame()
	for _, want := range []string{"Overthink", "Kuo, Lin", "0:10", "3:20", "⏸", "♪ bass house"} {
		if !strings.Contains(plain(fr), want) {
			t.Errorf("player bar missing %q:\n%s", want, plain(fr))
		}
	}
	if fr.Title != "Overthink · Kuo, Lin" {
		t.Errorf("title %q", fr.Title)
	}
	// The position ticks while playing, and stops when paused.
	d.Advance(3 * time.Second)
	if d.State().position != 13*time.Second {
		t.Fatalf("position %v", d.State().position)
	}
	event(d, player.Paused{})
	d.Advance(3 * time.Second)
	if d.State().position != 13*time.Second || strings.Contains(plain(d.Frame()), "⏸") {
		t.Fatalf("paused: position %v", d.State().position)
	}
	// Shortcuts (routed by the menus): space, n, p, s, r, →, +.
	for _, k := range []flatte.KeyEvent{char(' ', 0), char('n', 0), char('p', 0), char('s', 0), char('r', 0), key(flatte.KeyRight, 0), char('+', 0)} {
		d.Send(k)
	}
	d.Settle()
	want := []string{"playpause", "next", "prev", "shuffle on", "repeat on", "seek 18s", "volume 45"}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v, want %v", f.calls, want)
	}
	if !d.State().playing || !d.State().shuffle || !d.State().repeat.Context {
		t.Fatal("optimistic state not shown")
	}
}

func TestSlidersSeekAndSetVolumeOnRelease(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	pr := comp.Rects[idProgress]
	if pr.W < 10 {
		t.Fatalf("progress rect %+v", pr)
	}
	press := func(r layout.Rect, x int) {
		d.Send(flatte.MouseEvent{X: r.X + x, Y: r.Y, Button: flatte.MouseLeft, Action: flatte.MousePress})
	}
	press(pr, 0)
	d.Send(flatte.MouseEvent{X: pr.X + pr.W - 1, Y: pr.Y, Button: flatte.MouseLeft, Action: flatte.MouseMotion})
	if len(f.calls) != 0 || !strings.Contains(plain(d.Frame()), "3:20  3:20") && d.State().dragValue != 1 {
		t.Fatalf("dragging sought early: %v", f.calls)
	}
	d.Send(flatte.MouseEvent{X: pr.X + pr.W/2, Y: pr.Y, Button: flatte.MouseLeft, Action: flatte.MouseRelease})
	d.Settle()
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "seek 1m3") && !strings.HasPrefix(f.calls[0], "seek 1m4") {
		t.Fatalf("seek on release: %v", f.calls)
	}
	f.calls = nil
	vr := comp.Rects[idVolume]
	press(vr, vr.W-1)
	d.Send(flatte.MouseEvent{X: vr.X + vr.W - 1, Y: vr.Y, Button: flatte.MouseLeft, Action: flatte.MouseRelease})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "volume 100" {
		t.Fatalf("volume: %v", f.calls)
	}
}

func TestVolumeFollowsTheDrag(t *testing.T) {
	d, f := ready(t)
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	vr := comp.Rects[idVolume]
	d.Send(flatte.MouseEvent{X: vr.X, Y: vr.Y, Button: flatte.MouseLeft, Action: flatte.MousePress})
	d.Send(flatte.MouseEvent{X: vr.X + vr.W - 1, Y: vr.Y, Button: flatte.MouseLeft, Action: flatte.MouseMotion})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "volume 100" || d.State().volume != 100 {
		t.Fatalf("volume mid-drag: %v (volume %d)", f.calls, d.State().volume)
	}
}

func TestWheelNudgesSliders(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	wheel := func(id string, b flatte.MouseButton) {
		r := comp.Rects[id]
		d.Send(flatte.MouseEvent{X: r.X + 1, Y: r.Y, Button: b, Action: flatte.MousePress})
		d.Settle()
	}
	at, vol := d.State().position, d.State().volume
	wheel(idProgress, flatte.MouseWheelUp)
	if d.State().position != at+seekStep {
		t.Fatalf("wheel up on progress: %v, want %v (calls %v)", d.State().position, at+seekStep, f.calls)
	}
	wheel(idProgress, flatte.MouseWheelDown)
	wheel(idVolume, flatte.MouseWheelUp)
	if d.State().position != at || d.State().volume != vol+volumeStep {
		t.Fatalf("position %v, volume %d", d.State().position, d.State().volume)
	}
	cursor := d.State().rail.Cursor()
	wheel(idRail, flatte.MouseWheelDown)
	if d.State().rail.Cursor() != cursor+1 {
		t.Fatalf("wheel over the library: cursor %d", d.State().rail.Cursor())
	}
}

func TestErrorsShow(t *testing.T) {
	d, f := ready(t)
	f.fail = errors.New("no device")
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	fr := d.Settle()
	if !strings.Contains(plain(fr), "Could not play: no device") {
		t.Fatalf("error:\n%s", plain(fr))
	}
	event(d, player.PlaybackError{Kind: "unavailable", Unplayable: true})
	if d.State().status != "Could not play this track (unavailable)." {
		t.Fatalf("status %q", d.State().status)
	}
}

func TestContextMenuPlaysTheClickedPlaylist(t *testing.T) {
	d, f := ready(t)
	items := ContextMenu(d.State(), "rail:1")
	if len(items) != 1 || items[0].ID != cmdPlayList || !strings.Contains(items[0].Label, "Car Music") {
		t.Fatalf("menu %+v", items)
	}
	if ContextMenu(d.State(), "elsewhere") != nil {
		t.Fatal("a menu outside the library")
	}
	d.Send(flatte.MenuEvent{ID: cmdPlayList, Target: "rail:1"})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:playlist:b " {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestMenusFollowState(t *testing.T) {
	d, _ := ready(t)
	m := Menus(d.State())[0].Items
	if m[0].Label != "Play" || m[1].Disabled != true {
		t.Fatalf("idle menu %+v", m[:2])
	}
	event(d, song)
	event(d, player.Playing{})
	m = Menus(d.State())[0].Items
	if m[0].Label != "Pause" || m[1].Disabled {
		t.Fatalf("playing menu %+v", m[:2])
	}
}

func TestMainSnapshot(t *testing.T) {
	d, _ := ready(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	event(d, player.Volume{Value: 40, Max: 100})
	flatest.AssertGoldenFrame(t, "testdata/main.golden", d.Frame())
}

func TestClock(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0:00", 61 * time.Second: "1:01", 3599 * time.Second: "59:59", 1499 * time.Millisecond: "0:01"} {
		if got := clock(d); got != want {
			t.Errorf("clock(%v) = %q, want %q", d, got, want)
		}
	}
}
