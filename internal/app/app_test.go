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
}

func TestWheelScrollsTheListNotTheSelection(t *testing.T) {
	d, f, _ := start(t)
	long := player.Listing{URI: "spotify:playlist:a", Ready: true}
	for i := range 40 {
		long.Tracks = append(long.Tracks, player.Track{URI: "spotify:track:" + strconv.Itoa(i), Name: "Song " + strconv.Itoa(i), Duration: time.Minute})
	}
	long.Length, long.Cached = 40, 40
	f.listings[long.URI] = long
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
	fr := d.Frame()
	for _, want := range []string{"#  ", "Title", "Album", "Time", "Overthink  Kuo, Lin", "Thinking", "3:20", "Gravity  Dua", "61:00"} {
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
	// Tab goes back to the library, where the arrows move the selection.
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
	event(d, player.Ready{})
	if !strings.Contains(plain(d.Frame()), "Listing the tracks…") {
		t.Fatalf("partial:\n%s", plain(d.Frame()))
	}
	// Ready, but metadata still coming: rows show as placeholders.
	f.listings["spotify:playlist:a"] = player.Listing{URI: partial.URI, Ready: true, Length: 3, Cached: 1, Tracks: []player.Track{song, {URI: "spotify:track:2"}, {URI: "spotify:episode:3"}}}
	d.Advance(time.Second)
	if fr := plain(d.Frame()); !strings.Contains(fr, "Overthink") || !strings.Contains(fr, "2    …") {
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
	if len(items) != 4 || items[0].ID != cmdPlayTrack || items[1].ID != cmdQueue || items[3].Label != "Save to Liked Songs" || items[3].Disabled {
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
	if !strings.Contains(plain(d.Frame()), "Overthink ♡") {
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
	if !d.State().liked[song.URI] || f.calls[len(f.calls)-1] != "like spotify:track:1 on" || !strings.Contains(plain(d.Frame()), "Overthink ♥") {
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
	if !strings.Contains(plain(fr), "Liked Songs") || !strings.Contains(plain(fr), "adrian · 1 song") || !strings.Contains(plain(fr), "♥") {
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
