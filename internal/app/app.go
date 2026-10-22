// Package app is Smolify's UI: one state, pure views, and handlers, drawn by
// Flatte in a terminal or a window. It drives the player through Backend.
package app

import (
	"context"
	"fmt"
	"image"
	"io"
	"strings"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/player"
)

// Backend is the player as the UI uses it (player.Player, or a fake in
// tests). Requests block; the UI calls them off the loop.
type Backend interface {
	Events() <-chan player.Event
	Status(ctx context.Context) (player.Status, error)
	Playlists(ctx context.Context) ([]player.Playlist, error)
	Play(ctx context.Context, uri, skipTo string) error
	PlayPause(ctx context.Context) error
	Next(ctx context.Context) error
	Prev(ctx context.Context) error
	Seek(ctx context.Context, position time.Duration) error
	SetVolume(ctx context.Context, v int) error
	SetShuffle(ctx context.Context, on bool) error
	SetRepeat(ctx context.Context, on bool) error
	ContextTracks(ctx context.Context, uri string) (player.Listing, error)
	Queue(ctx context.Context, uri string) error
	SetLiked(ctx context.Context, uris []string, liked bool) error
	Liked(ctx context.Context, uris []string) ([]bool, error)
}

// Images fetches cover art.
type Images interface {
	Fetch(ctx context.Context, url string) (image.Image, error)
}

// State is the whole UI.
type State struct {
	be  Backend
	img Images
	// fx is the session's Effects, kept so player events (which arrive in a
	// fold) can start requests — the capture-fx idiom.
	fx flatte.Effects[State]

	// pairing is the sign-in code to show (zero: none); ready reports the
	// player signed in.
	pairing player.Pairing
	ready   bool

	track    *player.Track
	playing  bool
	position time.Duration
	volume   int
	volMax   int
	shuffle  bool
	repeat   player.Repeat
	context  string // the playing context's URI

	username  string // the signed-in account ("": not known yet)
	playlists []player.Playlist
	rail      flatui.List
	loaded    bool // the library has been read

	// listings are context tracks by context URI, as far as listed; listing
	// marks requests in flight. tracks is the track table's cursor over the
	// selected list's listing, shown the URI it belongs to.
	listings map[string]player.Listing
	listing  map[string]bool
	tracks   flatui.List
	shown    string
	focus    pane

	// liked is Liked Songs membership by track URI (absent: not known);
	// asking marks requests in flight.
	liked  map[string]bool
	asking map[string]bool

	// covers are fetched cover images by URL; fetching marks requests in
	// flight.
	covers   map[string]image.Image
	fetching map[string]bool

	// drag is the slider or scroll bar being dragged ("" none), and
	// dragValue where (a slider's); sliders are their rects in the last
	// layout.
	drag      string
	dragValue float64
	sliders   map[string]layout.Rect

	status string // the last error
	notice string // the last confirmation ("Added to queue")

	// debug, when set, records what the UI receives and sends.
	debug io.Writer
}

// pane is where the arrow keys act.
type pane int

const (
	paneLibrary pane = iota
	paneTracks
)

// logf writes a debug line when a debug log is set.
func (s *State) logf(format string, args ...any) {
	if s.debug != nil {
		fmt.Fprintf(s.debug, time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
	}
}

// Node IDs.
const (
	idRail     = "rail"
	idTracks   = "tracks"
	idRailBar  = "rail-bar"
	idTrackBar = "tracks-bar"
	idPlay     = "play"
	idPrev     = "prev"
	idNext     = "next"
	idShuffle  = "shuffle"
	idRepeat   = "repeat"
	idProgress = "progress"
	idVolume   = "volume"
	idPlayList = "play-playlist"
	idLike     = "like"
	idOpenPair = "open-pair"
)

// New is the app for backend be. debug, when not nil, receives a log of
// player events, commands, and request failures.
func New(be Backend, img Images, debug io.Writer) flatte.App[State] {
	s := &State{
		be: be, img: img, volMax: 100, debug: debug,
		covers: map[string]image.Image{}, fetching: map[string]bool{},
		listings: map[string]player.Listing{}, listing: map[string]bool{},
		liked: map[string]bool{}, asking: map[string]bool{},
	}
	return flatte.App[State]{
		State: s, Init: Init, Handle: Handle, View: View, Menus: Menus, ContextMenu: ContextMenu,
	}
}

// Init follows the player's events and ticks the position while playing.
func Init(s *State, fx flatte.Effects[State]) {
	s.fx = fx
	if events := s.be.Events(); events != nil {
		flatte.Stream(fx, "player", func(ctx context.Context, send func(player.Event)) {
			for {
				select {
				case ev := <-events:
					send(ev)
				case <-ctx.Done():
					return
				}
			}
		}, onPlayer)
	}
	_ = flatte.Every(fx, "tick", time.Second, tick)
}

// tick advances the position of a playing track by a second, and asks
// again for a selected list the player is still listing.
func tick(s *State, _ time.Time) {
	if s.playing && s.track != nil && s.drag != idProgress {
		s.position = min(s.position+time.Second, s.track.Duration)
	}
	if pl, ok := s.selected(); ok {
		if l, ok := s.listings[pl.URI]; ok && !l.Complete() {
			loadTracks(s, pl.URI)
		}
	}
}

// onPlayer folds a player event into state.
func onPlayer(s *State, ev player.Event) {
	s.logf("event %T %+v", ev, ev)
	switch e := ev.(type) {
	case player.Pairing:
		s.pairing = e
	case player.Ready:
		s.pairing, s.ready = player.Pairing{}, true
		if !s.loaded {
			loadLibrary(s)
			refreshStatus(s)
		}
	case player.Track:
		s.ready = true
		s.track, s.position = &e, e.Position
		if e.ContextURI != "" {
			s.context = e.ContextURI
		}
		fetchCover(s, e.Cover)
		askLiked(s, []player.Track{e})
	case player.Playing:
		s.playing, s.context = true, nonEmpty(e.ContextURI, s.context)
	case player.Paused:
		s.playing = false
	case player.Stopped:
		s.playing = false
	case player.Seek:
		s.position = e.Position
	case player.Volume:
		s.volume, s.volMax = e.Value, max(e.Max, 1)
	case player.Shuffle:
		s.shuffle = e.On
	case player.Repeat:
		s.repeat = e
	case player.PlaybackError:
		s.status = "Could not play this track (" + e.Kind + ")."
	case player.Failed:
		s.status = "The player stopped: " + e.Err.Error()
	}
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// loadLibrary reads the playlists.
func loadLibrary(s *State) {
	s.loaded = true
	be := s.be
	flatte.Go(s.fx, "library", func(ctx context.Context) ([]player.Playlist, error) {
		return be.Playlists(ctx)
	}, func(s *State, pls []player.Playlist, err error) {
		if err != nil {
			s.status, s.loaded = "Could not load the library: "+err.Error(), false
			return
		}
		s.playlists = pls
		libraryChanged(s)
	})
}

// refreshStatus reads the player's state (a track already loaded, the
// volume and modes).
func refreshStatus(s *State) {
	be := s.be
	flatte.Go(s.fx, "status", func(ctx context.Context) (player.Status, error) {
		return be.Status(ctx)
	}, func(s *State, st player.Status, err error) {
		if err != nil {
			return // events will tell
		}
		s.playing = !st.Paused && !st.Stopped && st.Track != nil
		s.volume, s.volMax = st.Volume, max(st.VolumeSteps, 1)
		s.shuffle, s.repeat = st.Shuffle, st.Repeat
		s.context = nonEmpty(st.ContextURI, s.context)
		if st.Track != nil {
			s.track, s.position = st.Track, st.Track.Position
			fetchCover(s, st.Track.Cover)
			askLiked(s, []player.Track{*st.Track})
		}
		if st.Username != "" && s.username == "" {
			s.username = st.Username
			libraryChanged(s)
		}
	})
}

// libraryChanged follows a change to the library's entries, keeping the
// same list selected when it is still there.
func libraryChanged(s *State) {
	lib := s.library()
	s.rail.SetCount(len(lib))
	for i, pl := range lib {
		if pl.URI == s.shown {
			s.rail.Select(i)
			break
		}
	}
	showSelected(s)
}

// library is the rail's entries: Liked Songs (once the account is known),
// then the playlists.
func (s *State) library() []player.Playlist {
	if s.username == "" {
		return s.playlists
	}
	liked := player.Playlist{URI: player.LikedSongs(s.username), Name: "Liked Songs", Owner: s.username}
	return append([]player.Playlist{liked}, s.playlists...)
}

// loadTracks asks for the tracks of the context uri, unless they are
// listed already or asked for.
func loadTracks(s *State, uri string) {
	if l, ok := s.listings[uri]; s.listing[uri] || ok && l.Complete() {
		return
	}
	s.listing[uri] = true
	be := s.be
	flatte.Go(s.fx, "tracks", func(ctx context.Context) (player.Listing, error) {
		return be.ContextTracks(ctx, uri)
	}, func(s *State, l player.Listing, err error) {
		delete(s.listing, uri)
		if err != nil {
			s.logf("tracks %s: %v", uri, err)
			s.status = "Could not list the tracks: " + err.Error()
			return
		}
		s.listings[uri] = l
		if uri == s.shown {
			s.tracks.SetCount(len(l.Tracks))
		}
		askLiked(s, l.Tracks)
	})
}

// askLiked asks whether the tracks not known yet are in Liked Songs.
func askLiked(s *State, tracks []player.Track) {
	var uris []string
	for _, t := range tracks {
		_, known := s.liked[t.URI]
		if strings.HasPrefix(t.URI, "spotify:track:") && !known && !s.asking[t.URI] {
			s.asking[t.URI] = true
			uris = append(uris, t.URI)
		}
	}
	if len(uris) == 0 {
		return
	}
	be := s.be
	flatte.Go(s.fx, "liked", func(ctx context.Context) ([]bool, error) {
		return be.Liked(ctx, uris)
	}, func(s *State, liked []bool, err error) {
		for i, uri := range uris {
			delete(s.asking, uri)
			if err == nil && i < len(liked) {
				s.liked[uri] = liked[i]
			}
		}
		if err != nil {
			s.logf("liked: %v", err)
		}
	})
}

// setLiked saves the track uri to Liked Songs or removes it, shown at once
// and put back if the request fails.
func setLiked(s *State, uri string, on bool) {
	if !strings.HasPrefix(uri, "spotify:track:") {
		return
	}
	was, known := s.liked[uri]
	s.liked[uri] = on
	if s.username != "" {
		delete(s.listings, player.LikedSongs(s.username)) // listed again when shown
	}
	be := s.be
	flatte.Go(s.fx, "like", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, be.SetLiked(ctx, []string{uri}, on)
	}, func(s *State, _ struct{}, err error) {
		s.logf("request like %s %v: err=%v", uri, on, err)
		if err != nil {
			if known {
				s.liked[uri] = was
			} else {
				delete(s.liked, uri)
			}
			s.status = "Could not update Liked Songs: " + err.Error()
		}
		if s.username != "" && s.shown == player.LikedSongs(s.username) {
			loadTracks(s, s.shown)
		}
	})
}

// fetchCover fetches the cover at url once.
func fetchCover(s *State, url string) {
	if url == "" || s.img == nil || s.covers[url] != nil || s.fetching[url] {
		return
	}
	s.fetching[url] = true
	img := s.img
	flatte.Go(s.fx, "cover", func(ctx context.Context) (image.Image, error) {
		return img.Fetch(ctx, url)
	}, func(s *State, im image.Image, err error) {
		delete(s.fetching, url)
		if err == nil && im != nil {
			s.covers[url] = im
		}
	})
}

// send runs a player request off the loop; a failure becomes the status.
func send(s *State, name string, req func(context.Context, Backend) error) {
	be := s.be
	flatte.Go(s.fx, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, req(ctx, be)
	}, func(s *State, _ struct{}, err error) {
		s.logf("request %s: err=%v", name, err)
		if err != nil {
			s.status = "Could not " + strings.ReplaceAll(name, "-", " ") + ": " + err.Error()
		}
	})
}

// selected is the list under the rail's cursor.
func (s *State) selected() (player.Playlist, bool) {
	lib := s.library()
	i := s.rail.Cursor()
	if i < 0 || i >= len(lib) {
		return player.Playlist{}, false
	}
	return lib[i], true
}

// selectedTrack is the track under the table's cursor, in the selected
// list.
func (s *State) selectedTrack() (player.Track, bool) {
	l, ok := s.listings[s.shown]
	i := s.tracks.Cursor()
	if !ok || i < 0 || i >= len(l.Tracks) {
		return player.Track{}, false
	}
	return l.Tracks[i], true
}

// Commands, from keys, clicks, and menus.
const (
	cmdPlayPause  = "play-pause"
	cmdNext       = "next"
	cmdPrev       = "previous"
	cmdShuffle    = "shuffle"
	cmdRepeat     = "repeat"
	cmdVolumeUp   = "volume-up"
	cmdVolumeDown = "volume-down"
	cmdSeekAhead  = "seek-ahead"
	cmdSeekBack   = "seek-back"
	cmdPlayList   = "play-playlist"
	cmdPlayTrack  = "play-track"
	cmdQueue      = "queue"
	cmdLike       = "like"       // the playing track
	cmdLikeTrack  = "like-track" // the selected track
	cmdQuit       = "quit"
)

const (
	seekStep   = 5 * time.Second
	volumeStep = 5
)

// command runs a command.
func command(s *State, id string, fx flatte.Effects[State]) {
	s.logf("command %s (track loaded: %v, playing: %v)", id, s.track != nil, s.playing)
	s.notice = ""
	switch id {
	case cmdPlayPause:
		if s.track == nil {
			command(s, cmdPlayList, fx)
			return
		}
		s.playing = !s.playing // shown at once; the player's event confirms
		send(s, "play-pause", func(ctx context.Context, be Backend) error { return be.PlayPause(ctx) })
	case cmdNext:
		send(s, "skip", func(ctx context.Context, be Backend) error { return be.Next(ctx) })
	case cmdPrev:
		send(s, "go-back", func(ctx context.Context, be Backend) error { return be.Prev(ctx) })
	case cmdShuffle:
		on := !s.shuffle
		s.shuffle = on
		send(s, "shuffle", func(ctx context.Context, be Backend) error { return be.SetShuffle(ctx, on) })
	case cmdRepeat:
		on := !s.repeat.Context
		s.repeat.Context = on
		send(s, "repeat", func(ctx context.Context, be Backend) error { return be.SetRepeat(ctx, on) })
	case cmdVolumeUp, cmdVolumeDown:
		step := volumeStep * s.volMax / 100
		if id == cmdVolumeDown {
			step = -step
		}
		setVolume(s, s.volume+step)
	case cmdSeekAhead, cmdSeekBack:
		if s.track == nil {
			return
		}
		d := seekStep
		if id == cmdSeekBack {
			d = -d
		}
		seek(s, s.position+d)
	case cmdPlayList:
		pl, ok := s.selected()
		if !ok {
			return
		}
		s.status = ""
		send(s, "play", func(ctx context.Context, be Backend) error { return be.Play(ctx, pl.URI, "") })
	case cmdPlayTrack:
		t, ok := s.selectedTrack()
		if !ok {
			return
		}
		ctxURI := s.shown
		s.status = ""
		send(s, "play", func(ctx context.Context, be Backend) error { return be.Play(ctx, ctxURI, t.URI) })
	case cmdQueue:
		t, ok := s.selectedTrack()
		if !ok {
			return
		}
		s.notice = "Added “" + nonEmpty(t.Name, "the track") + "” to the queue."
		send(s, "add-to-queue", func(ctx context.Context, be Backend) error { return be.Queue(ctx, t.URI) })
	case cmdLike:
		if s.track != nil {
			setLiked(s, s.track.URI, !s.liked[s.track.URI])
		}
	case cmdLikeTrack:
		if t, ok := s.selectedTrack(); ok {
			setLiked(s, t.URI, !s.liked[t.URI])
		}
	case cmdQuit:
		fx.Quit()
	}
}

func setVolume(s *State, v int) {
	v = min(max(v, 0), s.volMax)
	s.volume = v
	be := s.be
	flatte.Latest(s.fx, "volume", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, be.SetVolume(ctx, v)
	}, func(*State, struct{}, error) {})
}

func seek(s *State, to time.Duration) {
	if s.track == nil {
		return
	}
	to = min(max(to, 0), s.track.Duration)
	s.position = to
	be := s.be
	flatte.Latest(s.fx, "seek", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, be.Seek(ctx, to)
	}, func(*State, struct{}, error) {})
}

// Handle routes events.
func Handle(s *State, ev flatte.Event, fx flatte.Effects[State]) {
	switch e := ev.(type) {
	case flatte.LayoutEvent:
		if e.Window != "" {
			return
		}
		if r, ok := e.Rects[idRail]; ok {
			s.rail.SetHeight(max(r.H, 1))
		}
		if r, ok := e.Rects[idTracks]; ok {
			s.tracks.SetHeight(max(r.H, 1))
		}
		s.sliders = map[string]layout.Rect{}
		for _, id := range []string{idProgress, idVolume, idRailBar, idTrackBar} {
			if r, ok := e.Rects[id]; ok {
				s.sliders[id] = r
			}
		}
	case flatte.MenuEvent:
		// A context menu acts on the row clicked.
		if i, ok := rowIndex(idRail, e.Target); ok {
			s.rail.Select(i)
			showSelected(s)
		}
		if i, ok := rowIndex(idTracks, e.Target); ok {
			s.tracks.Select(i)
			s.focus = paneTracks
		}
		command(s, e.ID, fx)
	case flatte.KeyEvent:
		handleKey(s, e, fx)
	case flatte.MouseEvent:
		handleMouse(s, e, fx)
	}
}

// handleKey is navigation; the playback keys are the menus' shortcuts, which
// every host routes as MenuEvents before Handle sees them.
func handleKey(s *State, k flatte.KeyEvent, fx flatte.Effects[State]) {
	switch k.Key {
	case flatte.KeyEscape, flatte.KeyCtrlC:
		fx.Quit()
	case flatte.KeyTab: // two panes, so Shift+Tab is the same
		if s.focus == paneLibrary && len(s.listings[s.shown].Tracks) > 0 {
			s.focus = paneTracks
		} else {
			s.focus = paneLibrary
		}
	case flatte.KeyUp, flatte.KeyDown:
		move(s, s.focus, k.Key == flatte.KeyUp)
	case flatte.KeyEnter:
		if s.focus == paneTracks {
			command(s, cmdPlayTrack, fx)
		} else {
			command(s, cmdPlayList, fx)
		}
	case flatte.KeyCharacter:
		if k.Rune == 'q' && k.Mod == 0 {
			fx.Quit()
		}
	}
}

// move moves the cursor of pane p.
func move(s *State, p pane, up bool) {
	switch {
	case p == paneTracks && up:
		s.tracks.MoveUp()
	case p == paneTracks:
		s.tracks.MoveDown()
	case up:
		s.rail.MoveUp()
		showSelected(s)
	default:
		s.rail.MoveDown()
		showSelected(s)
	}
}

// showSelected shows the selected list: its cover, and its tracks from the
// top when it is a different list.
func showSelected(s *State) {
	pl, ok := s.selected()
	if !ok {
		return
	}
	fetchCover(s, pl.Image)
	if pl.URI != s.shown {
		s.shown = pl.URI
		s.tracks.SetCount(len(s.listings[pl.URI].Tracks))
		s.tracks.Select(0)
	}
	loadTracks(s, pl.URI)
}

func handleMouse(s *State, m flatte.MouseEvent, fx flatte.Effects[State]) {
	if m.Window != "" {
		return
	}
	// A scroll bar drag scrolls its list; a slider drag follows the pointer
	// and acts on release.
	if s.drag == idRailBar || s.drag == idTrackBar {
		r := s.sliders[s.drag]
		switch m.Action {
		case flatte.MouseMotion:
			scrollBar(s, s.drag, m.Y-r.Y, r.H)
		case flatte.MouseRelease:
			s.drag = ""
		}
		return
	}
	if s.drag != "" {
		r := s.sliders[s.drag]
		v := layout.Slider{}.ValueAt(m.X-r.X, r.W)
		switch {
		case m.Action == flatte.MouseMotion:
			s.dragValue = v
			if s.drag == idVolume { // volume follows the drag; seeking waits for release
				setVolume(s, int(v*float64(s.volMax)+0.5))
			}
		case m.Action == flatte.MouseRelease:
			release(s, s.drag, v)
			s.drag = ""
		}
		return
	}
	if m.Button == flatte.MouseWheelUp || m.Button == flatte.MouseWheelDown {
		wheel(s, m, fx)
		return
	}
	if m.Button != flatte.MouseLeft || m.Action != flatte.MousePress {
		return
	}
	if i, ok := rowIndex(idRail, m.Target); ok {
		if i == s.rail.Cursor() && s.focus == paneLibrary {
			command(s, cmdPlayList, fx) // a click on the selected one plays it
			return
		}
		s.focus = paneLibrary
		s.rail.Select(i)
		showSelected(s)
		return
	}
	if i, ok := rowIndex(idTracks, m.Target); ok {
		if i == s.tracks.Cursor() && s.focus == paneTracks {
			command(s, cmdPlayTrack, fx)
			return
		}
		s.focus = paneTracks
		s.tracks.Select(i)
		return
	}
	switch m.Target {
	case idPlay:
		command(s, cmdPlayPause, fx)
	case idPrev:
		command(s, cmdPrev, fx)
	case idNext:
		command(s, cmdNext, fx)
	case idShuffle:
		command(s, cmdShuffle, fx)
	case idRepeat:
		command(s, cmdRepeat, fx)
	case idPlayList:
		command(s, cmdPlayList, fx)
	case idLike:
		command(s, cmdLike, fx)
	case idOpenPair:
		openURL(s, s.pairing.URL)
	case idRailBar, idTrackBar:
		s.drag = m.Target
		scrollBar(s, m.Target, m.Y-m.TargetRect.Y, m.TargetRect.H)
	case idProgress, idVolume:
		if m.Target == idProgress && s.track == nil {
			return
		}
		s.drag = m.Target
		s.dragValue = layout.Slider{}.ValueAt(m.X-m.TargetRect.X, m.TargetRect.W)
	}
}

// wheel scrolls the list under the pointer (leaving its selection), or
// nudges the slider under it.
func wheel(s *State, m flatte.MouseEvent, fx flatte.Effects[State]) {
	up := m.Button == flatte.MouseWheelUp
	rows := 1
	if up {
		rows = -1
	}
	switch {
	case m.Target == idVolume && up:
		command(s, cmdVolumeUp, fx)
	case m.Target == idVolume:
		command(s, cmdVolumeDown, fx)
	case m.Target == idProgress && up:
		command(s, cmdSeekAhead, fx)
	case m.Target == idProgress:
		command(s, cmdSeekBack, fx)
	case strings.HasPrefix(m.Target, idTracks):
		s.tracks.Scroll(rows)
	default:
		s.rail.Scroll(rows)
	}
}

// list is the list a scroll bar scrolls.
func (s *State) list(bar string) *flatui.List {
	if bar == idTrackBar {
		return &s.tracks
	}
	return &s.rail
}

// listBar is the scroll bar of list l.
func listBar(l flatui.List, id string) layout.ScrollBar {
	return layout.ScrollBar{NodeBase: layout.NodeBase{ID: id}, Offset: l.Offset(), Visible: l.Height(), Total: l.Count()}
}

// scrollBar scrolls the list of bar for a press or drag at line y of the
// bar, height lines tall, leaving its selection.
func scrollBar(s *State, bar string, y, height int) {
	l := s.list(bar)
	l.Scroll(listBar(*l, bar).OffsetAt(y, height) - l.Offset())
}

// release acts on a slider let go at value v.
func release(s *State, id string, v float64) {
	switch id {
	case idProgress:
		if s.track != nil {
			seek(s, time.Duration(v*float64(s.track.Duration)))
		}
	case idVolume:
		setVolume(s, int(v*float64(s.volMax)+0.5))
	}
}

// rowIndex is the row index of a list row target ("rail:3" in "rail").
func rowIndex(list, target string) (int, bool) {
	rest, ok := strings.CutPrefix(target, list+":")
	if !ok {
		return 0, false
	}
	n := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, rest != ""
}
