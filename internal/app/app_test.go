package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

// fake is a Backend that records requests: the ones that change things in
// calls, track listings and liked lookups in reads.
type fake struct {
	calls     []string
	reads     []string
	playlists []player.Playlist
	status    player.Status
	fail      error
	listings  map[string]player.Listing
	liked     map[string]bool

	// recent is the recently played lists; the Web API's results by
	// query, and its failure.
	recent  []player.Played
	results map[string]spotify.Results
	webFail error
}

func (f *fake) Configure(_ context.Context, a player.Audio) error {
	f.calls = append(f.calls, fmt.Sprintf("configure %d %v %v %v", a.Bitrate, a.Normalise, a.Crossfade, a.Autoplay))
	return f.fail
}
func (f *fake) SetDeviceName(_ context.Context, name string) error {
	f.calls = append(f.calls, "rename "+name)
	return f.fail
}
func (f *fake) SignOut(context.Context) error {
	f.calls = append(f.calls, "sign out")
	return f.fail
}

func (f *fake) Search(_ context.Context, q string, _ int) (spotify.Results, error) {
	f.reads = append(f.reads, "search "+q)
	return f.results[q], f.webFail
}
func (f *fake) RecentlyPlayed(_ context.Context, user string, _ int) ([]player.Played, error) {
	f.reads = append(f.reads, "recent "+user)
	return f.recent, f.fail
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
func (f *fake) SetRepeat(_ context.Context, r player.Repeat) error {
	return f.record("repeat " + bstr(r.Context) + " " + bstr(r.Track))
}

func (f *fake) ContextTracks(_ context.Context, uri string) (player.Listing, error) {
	f.reads = append(f.reads, "tracks "+uri)
	if l, ok := f.listings[uri]; ok {
		return l, f.fail
	}
	return player.Listing{URI: uri, Ready: true}, f.fail
}
func (f *fake) Queue(_ context.Context, uri string) error { return f.record("queue " + uri) }
func (f *fake) SetLiked(_ context.Context, uris []string, on bool) error {
	return f.record("like " + strings.Join(uris, ",") + " " + bstr(on))
}
func (f *fake) Liked(_ context.Context, uris []string) ([]bool, error) {
	f.reads = append(f.reads, "liked "+strings.Join(uris, ","))
	out := make([]bool, len(uris))
	for i, u := range uris {
		out[i] = f.liked[u]
	}
	return out, f.fail
}

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
	f := &fake{playlists: library, status: player.Status{Volume: 40, VolumeSteps: 100, Paused: true}, listings: map[string]player.Listing{"spotify:playlist:a": bassHouse}}
	c := &covers{}
	d := flatest.Start(New(f, c, f, nil), 100)
	d.State().page = pageLibrary
	d.State().searchDelay = 0
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
	if !strings.Contains(plain(d.Frame()), "Loading") {
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
	if !regexp.MustCompile(`Your Library +2`).MatchString(plain(fr)) || !strings.Contains(plain(fr), "bass house") {
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
	// Space on a fresh idle player also starts its selected playlist.
	d, f = ready(t)
	f.calls = nil
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: ' '})
	d.Settle()
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "play ") {
		t.Fatalf("space: %v", f.calls)
	}
}

// bassHouse is playlist a's listing: song, and two more.
var bassHouse = player.Listing{URI: "spotify:playlist:a", Ready: true, Length: 3, Cached: 3, Tracks: []player.Track{
	song,
	{URI: "spotify:track:2", Name: "Gravity", Artists: []string{"Dua"}, Album: "Fall", Duration: 185 * time.Second},
	{URI: "spotify:episode:3", Name: "Talk", Artists: []string{"Host"}, Album: "Show", Duration: 61 * time.Minute},
}}

var song = player.Track{URI: "spotify:track:1", Name: "Overthink", Artists: []string{"Kuo", "Lin"}, Album: "Thinking", Duration: 200 * time.Second, Position: 10 * time.Second, ContextURI: "spotify:playlist:a", Cover: "https://img/t"}

func TestNowPlaying(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	event(d, player.Playing{URI: song.URI, ContextURI: song.ContextURI})
	fr := d.Frame()
	if !regexp.MustCompile(`bass house +♪`).MatchString(plain(fr)) {
		t.Errorf("the playing list is not marked:\n%s", plain(fr))
	}
	for _, want := range []string{"Overthink", "Kuo, Lin", "0:10", "3:20", "⏸"} {
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
	want := []string{"playpause", "next", "prev", "shuffle on", "repeat on off", "seek 18s", "volume 45"}
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
}

func TestWheelScrollsTheListNotTheSelection(t *testing.T) {
	d, f, _ := start(t)
	long := player.Listing{URI: "spotify:playlist:a", Ready: true}
	for i := range 40 {
		long.Tracks = append(long.Tracks, player.Track{URI: "spotify:track:" + strconv.Itoa(i), Name: "Song " + strconv.Itoa(i), Duration: time.Minute})
	}
	long.Length, long.Cached = 40, 40
	f.listings[long.URI] = long
	d.State().compact = true
	event(d, player.Ready{})
	d.Send(flatte.MenuEvent{ID: "none", Target: idTracks + ":1"}) // select row 1
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	r := comp.Rects[idTracks+":5"]
	for range 3 {
		d.Send(flatte.MouseEvent{X: r.X + 2, Y: r.Y, Button: flatte.MouseWheelDown, Action: flatte.MousePress})
	}
	fr := d.Settle()
	s := d.State()
	if s.tracks.Offset() != 3 || s.tracks.Cursor() != 1 {
		t.Fatalf("offset %d cursor %d, want 3 and 1", s.tracks.Offset(), s.tracks.Cursor())
	}
	if strings.Contains(plain(fr), "Song 2 ") || !strings.Contains(plain(fr), "Song 3") {
		t.Fatalf("not scrolled:\n%s", plain(fr))
	}
	// The next arrow brings the selection back.
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	if s.tracks.Cursor() != 2 || s.tracks.Offset() != 2 {
		t.Fatalf("offset %d cursor %d after ↓", s.tracks.Offset(), s.tracks.Cursor())
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

func TestTrackTablePlaysFromATrack(t *testing.T) {
	d, f := ready(t)
	d.State().compact = true
	fr := d.Frame()
	for _, want := range []string{"#  ", "Title", "Album", "◷", "Overthink  Kuo, Lin", "Thinking", "3:20", "Gravity  Dua", "61:00"} {
		if !strings.Contains(plain(fr), want) {
			t.Errorf("table missing %q:\n%s", want, plain(fr))
		}
	}
	if len(f.reads) == 0 || f.reads[0] != "tracks spotify:playlist:a" {
		t.Fatalf("reads %v", f.reads)
	}
	// Only songs are asked about Liked Songs; the episode is not.
	if !strings.Contains(strings.Join(f.reads, ","), "liked spotify:track:1,spotify:track:2") || strings.Contains(strings.Join(f.reads, ","), "episode") {
		t.Fatalf("liked reads %v", f.reads)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:playlist:a spotify:track:2" {
		t.Fatalf("calls %v", f.calls)
	}
	if d.State().rail.Cursor() != 0 {
		t.Fatal("the arrows moved the library while the tracks had focus")
	}
	// Tab visits the playlist filter, sorting, then the library.
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	if d.State().focus != paneTrackFilter {
		t.Fatal("playlist filter missing from tab order")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	if d.State().focus != paneSort {
		t.Fatal("sort control missing from tab order")
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyTab})
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	if d.State().rail.Cursor() != 1 || d.State().tracks.Cursor() != 0 {
		t.Fatalf("rail %d tracks %d", d.State().rail.Cursor(), d.State().tracks.Cursor())
	}
}

func TestTrackClicksSelectThenPlay(t *testing.T) {
	d, f := ready(t)
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	r, ok := comp.Rects[idTracks+":1"]
	if !ok {
		t.Fatalf("no row 1 in %v", comp.Rects)
	}
	click := func() {
		d.Send(flatte.MouseEvent{X: r.X + 2, Y: r.Y, Button: flatte.MouseLeft, Action: flatte.MousePress})
		d.Settle()
	}
	click()
	if d.State().tracks.Cursor() != 1 || d.State().focus != paneTracks || len(f.calls) != 0 {
		t.Fatalf("first click: cursor %d focus %v calls %v", d.State().tracks.Cursor(), d.State().focus, f.calls)
	}
	click()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:playlist:a spotify:track:2" {
		t.Fatalf("second click: %v", f.calls)
	}
}

func TestPartialListingIsAskedAgain(t *testing.T) {
	d, f, _ := start(t)
	partial := player.Listing{URI: "spotify:playlist:a", Length: 3, Cached: 0}
	f.listings["spotify:playlist:a"] = partial
	d.State().compact = true
	event(d, player.Ready{})
	if !strings.Contains(plain(d.Frame()), "Listing the tracks…") {
		t.Fatalf("partial:\n%s", plain(d.Frame()))
	}
	// Ready, but metadata still coming: rows show as placeholders.
	f.listings["spotify:playlist:a"] = player.Listing{URI: partial.URI, Ready: true, Length: 3, Cached: 1, Tracks: []player.Track{song, {URI: "spotify:track:2"}, {URI: "spotify:episode:3"}}}
	d.Advance(time.Second)
	if fr := plain(d.Frame()); !strings.Contains(fr, "Overthink") || !regexp.MustCompile(`2 +…`).MatchString(fr) {
		t.Fatalf("placeholders:\n%s", fr)
	}
	f.listings["spotify:playlist:a"] = bassHouse
	d.Advance(time.Second)
	n := strings.Count(strings.Join(f.reads, ","), "tracks spotify:playlist:a")
	d.Advance(3 * time.Second)
	if strings.Count(strings.Join(f.reads, ","), "tracks spotify:playlist:a") != n || !strings.Contains(plain(d.Frame()), "Gravity") {
		t.Fatalf("a complete listing was asked again: %v", f.reads)
	}
}

func TestTrackContextMenu(t *testing.T) {
	d, f := ready(t)
	items := ContextMenu(d.State(), idTracks+":1")
	if len(items) != 8 || items[0].ID != cmdPlayTrack || items[1].ID != cmdQueue || items[3].Label != "Save to Liked Songs" || items[3].Disabled {
		t.Fatalf("menu %+v", items)
	}
	if ep := ContextMenu(d.State(), idTracks+":2"); !ep[3].Disabled {
		t.Fatal("an episode can be liked")
	}
	if ContextMenu(d.State(), idTracks+":9") != nil {
		t.Fatal("a menu for no row")
	}
	d.Send(flatte.MenuEvent{ID: cmdQueue, Target: idTracks + ":1"})
	fr := d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "queue spotify:track:2" || !strings.Contains(plain(fr), "Added “Gravity” to the queue.") {
		t.Fatalf("queue: %v\n%s", f.calls, plain(fr))
	}
	d.Send(flatte.MenuEvent{ID: cmdLikeTrack, Target: idTracks + ":1"})
	d.Settle()
	if f.calls[1] != "like spotify:track:2 on" || !d.State().liked["spotify:track:2"] {
		t.Fatalf("like: %v", f.calls)
	}
	if ContextMenu(d.State(), idTracks+":1")[3].Label != "Remove from Liked Songs" {
		t.Fatal("menu does not follow liked")
	}
	if !strings.Contains(plain(d.Frame()), "♥") {
		t.Fatalf("no heart:\n%s", plain(d.Frame()))
	}
}

func TestLikeThePlayingTrack(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	m := Menus(d.State())[0].Items
	like := m[len(m)-1]
	if like.ID != cmdLike || like.Disabled || like.Label != "Save to Liked Songs" {
		t.Fatalf("like item %+v", like)
	}
	if !regexp.MustCompile(`Overthink +♡`).MatchString(plain(d.Frame())) {
		t.Fatalf("no outline heart:\n%s", plain(d.Frame()))
	}
	// A failed request puts the heart back and says why.
	f.fail = errors.New("offline")
	d.Send(flatte.MenuEvent{ID: cmdLike})
	if !d.State().liked[song.URI] {
		t.Fatal("not shown at once")
	}
	fr := d.Settle()
	if d.State().liked[song.URI] || !strings.Contains(plain(fr), "Could not update Liked Songs: offline") {
		t.Fatalf("after failure: liked %v\n%s", d.State().liked[song.URI], plain(fr))
	}
	// The heart is the control too.
	f.fail = nil
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	r := comp.Rects[idLike]
	d.Send(flatte.MouseEvent{X: r.X, Y: r.Y, Button: flatte.MouseLeft, Action: flatte.MousePress})
	d.Settle()
	if !d.State().liked[song.URI] || f.calls[len(f.calls)-1] != "like spotify:track:1 on" || !regexp.MustCompile(`Overthink +♥`).MatchString(plain(d.Frame())) {
		t.Fatalf("heart click: %v", f.calls)
	}
}

func TestLikedSongsInTheLibrary(t *testing.T) {
	d, f, _ := start(t)
	f.status.Username = "adrian"
	liked := player.LikedSongs("adrian")
	f.listings[liked] = player.Listing{URI: liked, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{song}}
	f.liked = map[string]bool{song.URI: true}
	event(d, player.Ready{})
	s := d.State()
	lib := s.library()
	if len(lib) != 3 || lib[0].URI != "spotify:user:adrian:collection" || lib[0].Name != "Liked Songs" {
		t.Fatalf("library %+v", lib)
	}
	// The list shown before the account was known stays selected.
	if pl, _ := s.selected(); pl.URI != "spotify:playlist:a" || s.rail.Cursor() != 1 {
		t.Fatalf("selected %+v at %d", pl, s.rail.Cursor())
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyUp})
	fr := d.Settle()
	if !strings.Contains(plain(fr), "Liked Songs") || !strings.Contains(plain(fr), "You · 1 song") || !strings.Contains(plain(fr), "♥") {
		t.Fatalf("liked songs:\n%s", plain(fr))
	}
	// Unliking from Liked Songs lists it again.
	f.listings[liked] = player.Listing{URI: liked, Ready: true}
	d.Send(flatte.MenuEvent{ID: cmdLikeTrack, Target: idTracks + ":0"})
	fr = d.Settle()
	if !strings.Contains(plain(fr), "No tracks here yet.") {
		t.Fatalf("after unlike:\n%s", plain(fr))
	}
}

func TestScrollBarDragScrollsTheList(t *testing.T) {
	d, f, _ := start(t)
	long := player.Listing{URI: "spotify:playlist:a", Ready: true, Length: 100, Cached: 100}
	for i := range 100 {
		long.Tracks = append(long.Tracks, player.Track{URI: "spotify:track:" + strconv.Itoa(i), Name: "Song " + strconv.Itoa(i), Duration: time.Minute})
	}
	f.listings[long.URI] = long
	event(d, player.Ready{})
	fr := d.Frame()
	if !strings.Contains(plain(fr), "█") {
		t.Fatalf("no thumb on a long list:\n%s", plain(fr))
	}
	_, comp := flatte.ComposeFrame(fr, 100, 30)
	bar := comp.Rects[idTrackBar]
	if bar.H < 5 {
		t.Fatalf("bar %+v", bar)
	}
	d.Send(flatte.MouseEvent{X: bar.X, Y: bar.Y, Button: flatte.MouseLeft, Action: flatte.MousePress, Target: idTrackBar, TargetRect: bar})
	d.Send(flatte.MouseEvent{X: bar.X, Y: bar.Y + bar.H - 1, Button: flatte.MouseLeft, Action: flatte.MouseMotion})
	s := d.State()
	if s.tracks.Offset() != 100-s.tracks.Height() || s.tracks.Cursor() != 0 {
		t.Fatalf("dragged to the bottom: offset %d cursor %d (height %d)", s.tracks.Offset(), s.tracks.Cursor(), s.tracks.Height())
	}
	d.Send(flatte.MouseEvent{X: bar.X, Y: bar.Y, Button: flatte.MouseLeft, Action: flatte.MouseRelease})
	if s.drag != "" {
		t.Fatal("drag not ended")
	}
	// The library's list fits: its bar is blank and pressing it is harmless.
	d.Send(flatte.MouseEvent{Button: flatte.MouseLeft, Action: flatte.MousePress, Target: idRailBar, TargetRect: comp.Rects[idRailBar]})
	if s.rail.Offset() != 0 {
		t.Fatalf("rail offset %d", s.rail.Offset())
	}
}

// found is a search answer: a song on an album, and one with no album.
var found = spotify.Results{Tracks: []spotify.Track{
	{URI: "spotify:track:9", Name: "Overture", DurationMS: 95000, Artists: []spotify.Artist{{Name: "Orchestra"}},
		Album: spotify.Album{URI: "spotify:album:o", Name: "Openings", Images: []spotify.Image{{URL: "https://img/o", Width: 300}}}},
	{URI: "spotify:track:8", Name: "Over Again", DurationMS: 61000, Artists: []spotify.Artist{{Name: "Solo"}}},
}}

func typeText(d *flatest.Driver[State], text string) {
	for _, r := range text {
		d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: r})
	}
	d.Settle()
}

func TestSearchAsYouType(t *testing.T) {
	d, f := ready(t)
	f.results = map[string]spotify.Results{"over": found}
	d.State().compact = true
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: '/'})
	s := d.State()
	// The field takes the keyboard; the page stays until something is typed.
	if s.page != pageLibrary || s.focus != paneSearch {
		t.Fatalf("page %v focus %v", s.page, s.focus)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: ' '})
	if s.page != pageLibrary {
		t.Fatalf("a space opened search: page %v", s.page)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyBackspace})
	// Plain keys type: s, r, space do not toggle shuffle, repeat, or play.
	typeText(d, "over")
	if len(f.calls) != 0 || s.shuffle || s.query.Value != "over" {
		t.Fatalf("typing ran commands %v (query %q)", f.calls, s.query.Value)
	}
	fr := plain(d.Frame())
	for _, want := range []string{"Search", "over", "Overture  Orchestra", "Openings", "1:35", "Over Again  Solo"} {
		if !strings.Contains(fr, want) {
			t.Errorf("results missing %q:\n%s", want, fr)
		}
	}
	if !strings.Contains(strings.Join(f.reads, ","), "search over") {
		t.Fatalf("reads %v", f.reads)
	}
	// Enter goes to the results; Enter there plays the song in its album.
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if s.focus != paneTracks {
		t.Fatalf("focus %v after Enter", s.focus)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:album:o spotify:track:9" {
		t.Fatalf("calls %v", f.calls)
	}
	// A song with no album plays by itself.
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Settle()
	if f.calls[1] != "play spotify:track:8 " {
		t.Fatalf("calls %v", f.calls)
	}
	// ↑ from the first result is the field again; clearing it clears them.
	d.Send(flatte.KeyEvent{Key: flatte.KeyUp})
	d.Send(flatte.KeyEvent{Key: flatte.KeyUp})
	if s.focus != paneSearch {
		t.Fatalf("focus %v", s.focus)
	}
	for range 4 {
		d.Send(flatte.KeyEvent{Key: flatte.KeyBackspace})
	}
	// Clearing the field goes back to where search began, still typing.
	if fr := plain(d.Settle()); s.page != pageLibrary || s.shown != "spotify:playlist:a" || s.focus != paneSearch || strings.Contains(fr, "Overture") {
		t.Fatalf("cleared: page %v shown %q focus %v:\n%s", s.page, s.shown, s.focus, fr)
	}
	// Esc with no results leaves for the library, where keys are commands.
	d.Send(flatte.KeyEvent{Key: flatte.KeyEscape})
	if s.focus != paneLibrary || d.Quit() {
		t.Fatalf("focus %v quit %v", s.focus, d.Quit())
	}
}

func TestSearchPasteAndErrors(t *testing.T) {
	d, f := ready(t)
	f.results = map[string]spotify.Results{"over again": found}
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	d.Send(flatte.PasteEvent{Text: "over\nagain"})
	if fr := plain(d.Settle()); !strings.Contains(fr, "Overture") {
		t.Fatalf("paste:\n%s", fr)
	}
	f.webFail = spotify.Error{Status: 403, Message: "Insufficient client scope"}
	typeText(d, "!")
	if fr := plain(d.Frame()); !strings.Contains(fr, "Could not search: Spotify: Insufficient client scope (403)") {
		t.Fatalf("error:\n%s", fr)
	}
	f.webFail = nil
	f.results["nothing"] = spotify.Results{}
	d.State().query = flatui.TextField{}
	typeText(d, "nothing")
	if fr := plain(d.Frame()); !strings.Contains(fr, "Nothing found.") || strings.Contains(fr, "Could not search") {
		t.Fatalf("empty:\n%s", fr)
	}
}

func TestMenusWhileTyping(t *testing.T) {
	d, _ := ready(t)
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "x")
	for _, m := range Menus(d.State()) {
		for _, it := range m.Items {
			for _, k := range it.Keys {
				if k.Mod&(flatte.ModCtrl|flatte.ModSuper|flatte.ModAlt) == 0 {
					t.Errorf("%s keeps the plain key %+v while typing", it.Label, k)
				}
			}
		}
	}
	search := menuItem(t, d.State(), cmdSearch)
	if !search.Checked || len(search.Keys) != 2 {
		t.Fatalf("search item %+v", search)
	}
}

func TestHomeIsRecentlyPlayed(t *testing.T) {
	d, f := ready(t)
	event(d, song)
	d.State().username = "adrian" // the account is known: Liked Songs joins the rail
	libraryChanged(d.State())
	f.listings["spotify:album:o"] = player.Listing{URI: "spotify:album:o", Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{
		{URI: "spotify:track:9", Name: "Overture", Artists: []string{"Orchestra"}, Album: "Openings", Duration: 95 * time.Second},
	}}
	f.recent = []player.Played{
		{ContextURI: "spotify:album:o", TrackURI: "spotify:track:9"},
		{ContextURI: "spotify:playlist:a", TrackURI: "spotify:track:2"},
		{ContextURI: "spotify:playlist:a", TrackURI: "spotify:track:9"},    // again: listed once
		{ContextURI: "spotify:artist:gone", TrackURI: "spotify:track:404"}, // not listable: left out
	}
	f.liked = map[string]bool{"spotify:track:9": true}
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: 'h'})
	fr := plain(d.Settle())
	s := d.State()
	if s.page != pageHome || strings.Count(fr, "Overture") != 1 || !strings.Contains(fr, "Recently played") || !strings.Contains(fr, "♥") {
		t.Fatalf("home:\n%s", fr)
	}
	// The rail shows no selected list while Home is up.
	if strings.Contains(fr, " songs") {
		t.Fatalf("a playlist header on Home:\n%s", fr)
	}
	// A recently played song plays where it was played from.
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Settle()
	if len(f.calls) != 1 || f.calls[0] != "play spotify:album:o spotify:track:9" {
		t.Fatalf("calls %v", f.calls)
	}
	if got := d.State().listings[uriHome].Tracks; len(got) != 2 || got[1].Name != "Gravity" || got[1].ContextURI != "spotify:playlist:a" {
		t.Fatalf("home tracks %+v", got)
	}
	// Back to the library with a click on a list: Car Music, wherever it is
	// now that the account is known (Liked Songs comes first).
	row := -1
	for i, pl := range s.library() {
		if pl.Name == "Car Music" {
			row = i
		}
	}
	_, comp := flatte.ComposeFrame(d.Frame(), 100, 30)
	r, ok := comp.Rects[idRail+":"+strconv.Itoa(row)]
	if !ok {
		t.Fatalf("Car Music (row %d) not on screen", row)
	}
	d.Send(flatte.MouseEvent{X: r.X + 1, Y: r.Y, Button: flatte.MouseLeft, Action: flatte.MousePress})
	if fr := plain(d.Settle()); s.page != pageLibrary || !strings.Contains(fr, "For the road.") || !strings.Contains(fr, " songs") || len(f.calls) != 1 {
		t.Fatalf("page %v calls %v:\n%s", s.page, f.calls, fr)
	}
}

func TestNoWebAPI(t *testing.T) {
	f := &fake{playlists: library, status: player.Status{VolumeSteps: 100}, listings: map[string]player.Listing{}}
	d := flatest.Start(New(f, nil, nil, nil), 100)
	d.Send(flatte.ResizeEvent{Width: 100, Height: 30})
	event(d, player.Ready{})
	// Home comes from the player; only search needs the Web API.
	if menuItem(t, d.State(), cmdHome).Disabled || !menuItem(t, d.State(), cmdSearch).Disabled {
		t.Fatal("Home disabled, or Search enabled, without the Web API")
	}
}

func TestOwnerNames(t *testing.T) {
	for owner, want := range map[string]string{
		"adrian": "You", "spotify": "Spotify", "mixmasta91": "mixmasta91", "": "",
		"1124806938": "", "6qxhced3b0l62lh3fedtbv14y": "", "liquicityrecords": "liquicityrecords",
	} {
		if got := ownerName(owner, "adrian"); got != want {
			t.Errorf("ownerName(%q) = %q, want %q", owner, got, want)
		}
	}
}

func TestUnlikeEditsLikedSongsInPlace(t *testing.T) {
	d, f, _ := start(t)
	f.status.Username = "adrian"
	liked := player.LikedSongs("adrian")
	two := bassHouse.Tracks[1]
	full := player.Listing{URI: liked, Ready: true, Length: 2, Cached: 2, Tracks: []player.Track{song, two}}
	f.listings[liked] = full
	f.liked = map[string]bool{song.URI: true, two.URI: true}
	event(d, player.Ready{})
	d.Send(flatte.KeyEvent{Key: flatte.KeyUp}) // to Liked Songs
	d.Settle()
	s := d.State()
	d.Send(flatte.MenuEvent{ID: "none", Target: idTracks + ":1"}) // the cursor on Gravity
	// The player lists Liked Songs again from scratch after the change.
	f.listings[liked] = player.Listing{URI: liked, Length: 1}
	d.Send(flatte.MenuEvent{ID: cmdLikeTrack, Target: idTracks + ":0"}) // unlike Overthink
	if l := s.listings[liked]; len(l.Tracks) != 1 || l.Tracks[0].URI != two.URI {
		t.Fatalf("not removed at once: %+v", l.Tracks)
	}
	fr := plain(d.Settle())
	if !strings.Contains(fr, "Gravity") || strings.Contains(fr, "Overthink  Kuo") || strings.Contains(fr, "Listing the tracks") {
		t.Fatalf("while listed again:\n%s", fr)
	}
	// Complete again: the new listing replaces it, the cursor on Gravity.
	f.listings[liked] = player.Listing{URI: liked, Ready: true, Length: 1, Cached: 1, Tracks: []player.Track{two}}
	d.Advance(time.Second)
	if s.stale[liked] || s.tracks.Cursor() != 0 || s.listings[liked].Tracks[0].URI != two.URI {
		t.Fatalf("stale %v cursor %d", s.stale[liked], s.tracks.Cursor())
	}
	// Liking the playing song puts it back on top.
	event(d, song)
	d.Send(flatte.MenuEvent{ID: cmdLike})
	if l := s.listings[liked]; len(l.Tracks) != 2 || l.Tracks[0].URI != song.URI || l.Length != 2 {
		t.Fatalf("liked again: %+v", l.Tracks)
	}
}

func TestSupersededIsNotAnError(t *testing.T) {
	d, f := ready(t)
	f.fail = fmt.Errorf("play: %w", player.ErrSuperseded)
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if fr := plain(d.Settle()); d.State().status != "" || strings.Contains(fr, "superseded") {
		t.Fatalf("status %q", d.State().status)
	}
}

// menuItem is the menu bar item id.
func menuItem(t *testing.T, s *State, id string) flatte.MenuItem {
	t.Helper()
	for _, m := range Menus(s) {
		for _, it := range m.Items {
			if it.ID == id {
				return it
			}
		}
	}
	t.Fatalf("no menu item %q", id)
	return flatte.MenuItem{}
}

func TestComfortableRowsAndThumbnails(t *testing.T) {
	d, f, c := start(t)
	tracks := bassHouse
	tracks.Tracks = append([]player.Track(nil), bassHouse.Tracks...)
	tracks.Tracks[1].Cover = "https://img/fall"
	f.listings[tracks.URI] = tracks
	event(d, player.Ready{})
	fr := plain(d.Settle())
	// Title and artists on two lines.
	if !regexp.MustCompile(`Gravity +Fall`).MatchString(fr) || !strings.Contains(fr, "Dua") || strings.Contains(fr, "Gravity  Dua") {
		t.Fatalf("two-line rows:\n%s", fr)
	}
	// The covers of the rows on screen are fetched as thumbnails.
	d.Settle()
	if !strings.Contains(strings.Join(c.fetched, ","), "https://img/fall") || d.State().thumbs["https://img/fall"] == nil {
		t.Fatalf("thumbnails %v", c.fetched)
	}
	// View ▸ Compact Rows: one line each.
	d.Send(flatte.MenuEvent{ID: cmdCompact})
	if fr := plain(d.Settle()); !strings.Contains(fr, "Gravity  Dua") || !menuItem(t, d.State(), cmdCompact).Checked {
		t.Fatalf("compact:\n%s", fr)
	}
}

func TestBackAndForward(t *testing.T) {
	d, f := ready(t)
	_ = f
	s := d.State()
	if !menuItem(t, s, cmdBack).Disabled {
		t.Fatal("back enabled with nowhere to go")
	}
	// Library list a → Home → list b (a click) → back, back, forward.
	d.Send(flatte.MenuEvent{ID: cmdHome})
	d.Send(flatte.MenuEvent{ID: cmdPlayList, Target: idRail + ":1"})
	d.Settle()
	if s.page != pageLibrary || s.shown != "spotify:playlist:b" {
		t.Fatalf("page %v shown %q", s.page, s.shown)
	}
	d.Send(flatte.MenuEvent{ID: cmdBack})
	if s.page != pageHome {
		t.Fatalf("back: page %v", s.page)
	}
	d.Send(flatte.MenuEvent{ID: cmdBack})
	if s.page != pageLibrary || s.shown != "spotify:playlist:a" || len(s.back) != 0 {
		t.Fatalf("back again: page %v shown %q back %v", s.page, s.shown, s.back)
	}
	d.Send(flatte.MenuEvent{ID: cmdForward})
	if s.page != pageHome || menuItem(t, s, cmdForward).Disabled {
		t.Fatalf("forward: page %v", s.page)
	}
	// Going somewhere new drops the forward places.
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "x")
	if len(s.forward) != 0 {
		t.Fatalf("forward kept %v", s.forward)
	}
}

func TestSearchReplacesPreviousQueryWhilePending(t *testing.T) {
	d, f := ready(t)
	f.results = map[string]spotify.Results{"over": found, "over!": {}}
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "over")
	fr := plain(d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: '!'}))
	if !strings.Contains(fr, "Searching…") || strings.Contains(fr, "Overture") {
		t.Fatalf("new query retained old rows or lacked loading state:\n%s", fr)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if len(f.calls) != 0 {
		t.Fatalf("played stale result: %v", f.calls)
	}
	if fr := plain(d.Settle()); !strings.Contains(fr, "Nothing found.") {
		t.Fatalf("empty result:\n%s", fr)
	}
}

func TestFirstSearchFailureStopsLoadingAndEnterRetries(t *testing.T) {
	d, f := ready(t)
	f.webFail = errors.New("offline")
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "over")
	fr := plain(d.Frame())
	if strings.Contains(fr, "Searching…") || !strings.Contains(fr, "Search failed.") || !strings.Contains(fr, "Could not search: offline") {
		t.Fatalf("failed search:\n%s", fr)
	}
	f.webFail = nil
	f.results = map[string]spotify.Results{"over": found}
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	if fr := plain(d.Settle()); !strings.Contains(fr, "Overture") || strings.Contains(fr, "Search failed.") {
		t.Fatalf("retry:\n%s", fr)
	}
}

func TestEnterOnSearchResultsDoesNotSearchAgain(t *testing.T) {
	d, f := ready(t)
	f.results = map[string]spotify.Results{"over": found}
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "over")
	f.reads = nil
	d.Send(flatte.KeyEvent{Key: flatte.KeyEnter})
	d.Send(flatte.KeyEvent{Key: flatte.KeyDown})
	d.Settle()
	if len(f.reads) != 0 || d.State().tracks.Cursor() != 1 || d.State().focus != paneTracks {
		t.Fatalf("Enter reloaded results: reads %v cursor %d focus %v", f.reads, d.State().tracks.Cursor(), d.State().focus)
	}
}

func TestSearchSupersededAndClearedBeforeCompletion(t *testing.T) {
	d, f := ready(t)
	f.results = map[string]spotify.Results{"over": found, "over!": {}}
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	d.Send(flatte.PasteEvent{Text: "over"})
	d.Send(flatte.KeyEvent{Key: flatte.KeyCharacter, Rune: '!'})
	if fr := plain(d.Settle()); strings.Contains(fr, "Overture") || !strings.Contains(fr, "Nothing found.") {
		t.Fatalf("superseded result applied:\n%s", fr)
	}
	d.Send(flatte.KeyEvent{Key: flatte.KeyBackspace})
	for range 4 {
		d.Send(flatte.KeyEvent{Key: flatte.KeyBackspace})
	}
	d.Settle()
	if _, exists := d.State().listings[uriSearch]; exists || d.State().page == pageSearch {
		t.Fatal("cleared query applied a pending result")
	}
}

func TestSearchSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail error
	}{{name: "search"}, {name: "search-error", fail: errors.New("offline")}} {
		t.Run(tc.name, func(t *testing.T) {
			d, f := ready(t)
			f.results, f.webFail = map[string]spotify.Results{"over": found}, tc.fail
			d.Send(flatte.MenuEvent{ID: cmdSearch})
			typeText(d, "over")
			flatest.AssertGoldenFrame(t, "testdata/"+tc.name+".golden", d.Frame())
		})
	}
}

func TestClearingFailedSearchClearsItsError(t *testing.T) {
	d, f := ready(t)
	f.webFail = errors.New("offline")
	d.Send(flatte.MenuEvent{ID: cmdSearch})
	typeText(d, "over")
	d.State().query.SelectAll()
	d.Send(flatte.KeyEvent{Key: flatte.KeyBackspace})
	if fr := plain(d.Settle()); strings.Contains(fr, "Could not search:") || d.State().status != "" || d.State().page != pageLibrary {
		t.Fatalf("failed query left its error after clearing:\n%s", fr)
	}
}

func TestRepeatCyclesListOneOff(t *testing.T) {
	d, f := ready(t)
	for range 3 {
		d.Send(char('r', 0))
		d.Settle()
	}
	want := []string{"repeat on off", "repeat off on", "repeat off off"}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v, want %v", f.calls, want)
	}
	if r := d.State().repeat; r.Context || r.Track {
		t.Fatalf("repeat %+v after a full cycle", r)
	}
	d.Send(char('r', 0))
	d.Send(char('r', 0))
	d.Settle()
	if !strings.Contains(plain(d.Frame()), "↻1") {
		t.Fatalf("repeat-one not shown:\n%s", plain(d.Frame()))
	}
}

func TestNewContextRereadsModes(t *testing.T) {
	d, f := ready(t)
	d.Send(char('r', 0))
	d.Settle()
	// The player turned repeat off when the new context began, unreported.
	f.status = player.Status{}
	f.calls = nil
	event(d, player.Playing{URI: "spotify:track:1", ContextURI: "spotify:playlist:b"})
	if r := d.State().repeat; r.Context || r.Track {
		t.Fatalf("repeat %+v kept after the player dropped it", r)
	}
	f.calls = nil
	event(d, player.Playing{URI: "spotify:track:2", ContextURI: "spotify:playlist:b"})
	if strings.Contains(strings.Join(f.calls, ","), "status") {
		t.Fatalf("same context reread modes: %v", f.calls)
	}
}

func TestPlaybackFailureNamesTheCause(t *testing.T) {
	key := playbackFailure(player.PlaybackError{Kind: "restricted", Message: "load: failed retrieving aes key with code 2"})
	if !strings.Contains(key, "audio key") || !strings.Contains(key, "code 2") {
		t.Fatalf("key refusal: %q", key)
	}
	region := playbackFailure(player.PlaybackError{Kind: "restricted", Message: "media is restricted"})
	if !strings.Contains(region, "media is restricted") {
		t.Fatalf("region: %q", region)
	}
	if got := playbackFailure(player.PlaybackError{Kind: "restricted"}); got != "Could not play this track (restricted)." {
		t.Fatalf("bare: %q", got)
	}
}

func TestTerminalShowsTheSelectedRow(t *testing.T) {
	d, _ := ready(t)
	d.State().page = pageLibrary
	d.State().rail.Select(1)
	fr := d.Settle()
	if !strings.Contains(fr.Content, "48;2;59;51;85") {
		t.Fatalf("no selection background in the terminal frame")
	}
}
