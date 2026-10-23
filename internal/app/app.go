// Package app is Smolify's UI: one state, pure views, and handlers, drawn by
// Flatte in a terminal or a window. It drives the player through Backend.
package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
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
	// Configure changes how the player plays, restarting its session.
	Configure(ctx context.Context, a player.Audio) error
	SetDeviceName(ctx context.Context, name string) error
	// SignOut forgets the sign-in; the player then reports Pairing.
	SignOut(ctx context.Context) error
	// RecentlyPlayed is the lists username played lately, newest first.
	RecentlyPlayed(ctx context.Context, username string, limit int) ([]player.Played, error)
}

// Web is Spotify's Web API as the UI uses it (spotify.Client, or a fake in
// tests): what the player does not serve.
type Web interface {
	Search(ctx context.Context, q string, limit int) (spotify.Results, error)
}

// Images fetches cover art.
type Images interface {
	Fetch(ctx context.Context, url string) (image.Image, error)
}

// State is the whole UI.
type State struct {
	be  Backend
	img Images
	web Web
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
	stale    map[string]bool // listed, but changed since: asked again until complete
	tracks   flatui.List
	shown    string
	focus    pane

	// back and forward are the places visited before and after this one.
	back, forward []place
	// compact shows tracks one line each, without thumbnails.
	compact bool

	// page is what the center shows; query is the search field, and
	// searchDelay how long typing must pause before it searches.
	page        page
	query       flatui.TextField
	searchDelay time.Duration

	// liked is Liked Songs membership by track URI (absent: not known);
	// asking marks requests in flight.
	liked  map[string]bool
	asking map[string]bool

	// covers are fetched cover images by URL, and tints their header colors;
	// fetching marks requests in flight.
	covers map[string]image.Image
	tints  map[string]color.Color
	// thumbs are covers shrunk for rows, fetched for the rows on screen.
	thumbs   map[string]image.Image
	thumbing map[string]bool
	fetching map[string]bool

	// drag is the slider or scroll bar being dragged ("" none), and
	// dragValue where (a slider's); sliders are their rects in the last
	// layout.
	drag      string
	dragValue float64
	sliders   map[string]layout.Rect

	status string // the last error
	notice string // the last confirmation ("Added to queue")

	// settings are the Settings page's, saved at settingsPath ("": not
	// saved); deviceName edits the device name; settingCursor is the row
	// the keyboard is on and settingsScroll the page's scroll (at most
	// settingsMax lines).
	settings                    Settings
	settingsPath                string
	deviceName                  flatui.TextField
	settingCursor               int
	settingsScroll, settingsMax int
	settingRects                map[string]layout.Rect // the page's rows, as last laid out
	// windowed is a window app, which tells errors and confirmations as
	// system notifications instead of on screen; away is set while the app
	// is not in front (FocusEvent), when a new track is told too.
	windowed, away bool

	// debug, when set, records what the UI receives and sends.
	debug io.Writer
}

// pane is where the keys act.
type pane int

const (
	paneLibrary pane = iota
	paneTracks
	paneSearch   // the search field: keys type
	paneSettings // the Settings page's rows
	paneDevice   // the device name field: keys type
)

// page is what the center shows.
type page int

const (
	pageLibrary page = iota // the list selected in the library
	pageHome
	pageSearch
	pageSettings
)

// The listings of Home (recently played) and of search results, beside the
// context URIs of real lists.
const (
	uriHome   = "smolify:home"
	uriSearch = "smolify:search"
)

// place is somewhere to go back to: a page, and for the library the list.
type place struct {
	page page
	list string
}

// here is where the app is.
func (s *State) here() place {
	p := place{page: s.page}
	if pl, ok := s.selected(); ok && s.page == pageLibrary {
		p.list = pl.URI
	}
	return p
}

// visit records leaving here for somewhere else (call before going).
func visit(s *State) {
	if n := len(s.back); n > 0 && s.back[n-1] == s.here() {
		return
	}
	s.back, s.forward = append(s.back, s.here()), nil
	if len(s.back) > 100 {
		s.back = s.back[1:]
	}
}

// goTo shows place p.
func goTo(s *State, p place) {
	switch p.page {
	case pageHome:
		openHome(s)
	case pageSearch:
		openSearch(s)
	case pageSettings:
		s.page, s.focus = pageSettings, paneSettings
	default:
		s.page, s.focus = pageLibrary, paneLibrary
		for i, pl := range s.library() {
			if pl.URI == p.list {
				s.rail.Select(i)
			}
		}
		showSelected(s)
	}
}

// travel goes back (or forward) a place.
func travel(s *State, forward bool) {
	from, to := &s.back, &s.forward
	if forward {
		from, to = to, from
	}
	n := len(*from)
	if n == 0 {
		return
	}
	p := (*from)[n-1]
	*from = (*from)[:n-1]
	*to = append(*to, s.here())
	goTo(s, p)
}

// typing is whether plain keys go to a field or a form rather than being
// shortcuts: the search and device name fields, and the Settings rows
// (where Space, ←, and → change a setting).
func (s *State) typing() bool {
	return s.focus == paneSearch || s.focus == paneDevice || s.page == pageSettings && s.focus == paneSettings
}

// trackRowLines is a track row's height in lines.
func (s *State) trackRowLines() int {
	if s.compact {
		return 1
	}
	return 2
}

// virtual reports a listing that is not a playable context.
func virtual(uri string) bool { return uri == uriHome || uri == uriSearch }

// logf writes a debug line when a debug log is set.
func (s *State) logf(format string, args ...any) {
	if s.debug != nil {
		fmt.Fprintf(s.debug, time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
	}
}

// Node IDs.
const (
	idRail        = "rail"
	idTracks      = "tracks"
	idRailBar     = "rail-bar"
	idTrackBar    = "tracks-bar"
	idPlay        = "play"
	idPrev        = "prev"
	idNext        = "next"
	idShuffle     = "shuffle"
	idRepeat      = "repeat"
	idProgress    = "progress"
	idVolume      = "volume"
	idPlayList    = "play-playlist"
	idLike        = "like"
	idHome        = "home"
	idBack        = "back"
	idForward     = "forward"
	idShufflePlay = "shuffle-list"
	idSearch      = "search"
	idSettings    = "settings-button"
	idSearchBox   = "search-box"
	// idSettingsScroll is the Settings page's scroll.
	idSettingsScroll  = "settings-scroll"
	idSettingsContent = "settings-scroll.content"
	idQuery           = "query"
	idOpenPair        = "open-pair"
)

// New is the app for backend be, with web for search and Home (nil:
// neither). debug, when not nil, receives a log of player events,
// commands, and request failures.
func New(be Backend, img Images, web Web, debug io.Writer) flatte.App[State] {
	s := &State{
		be: be, img: img, web: web, volMax: 100, debug: debug, searchDelay: 300 * time.Millisecond,
		covers: map[string]image.Image{}, tints: map[string]color.Color{}, thumbs: map[string]image.Image{}, thumbing: map[string]bool{}, fetching: map[string]bool{},
		listings: map[string]player.Listing{}, listing: map[string]bool{}, stale: map[string]bool{},
		liked: map[string]bool{}, asking: map[string]bool{},
		settings: DefaultSettings(),
	}
	s.deviceName = flatui.TextField{Value: s.settings.DeviceName, Cursor: len(s.settings.DeviceName)}
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
	if l, ok := s.listings[s.shown]; ok && (!l.Complete() || s.stale[s.shown]) && !virtual(s.shown) {
		loadTracks(s, s.shown)
	}
}

// Windowed is app as a window app: errors and confirmations become
// system notifications rather than lines on screen.
func Windowed(app flatte.App[State]) flatte.App[State] {
	app.State.windowed = true
	return app
}

// fail reports an error: on screen until the next success, and in a
// window as a notification (once per distinct message).
func fail(s *State, msg string) {
	if s.windowed && s.settings.NotifyErrors && msg != s.status {
		s.fx.Notify(flatte.Notification{Title: "Smolify", Body: msg})
	}
	s.status = msg
}

// tell reports a confirmation, as fail does an error.
func tell(s *State, msg string) {
	if s.windowed && s.settings.NotifyErrors {
		s.fx.Notify(flatte.Notification{Title: "Smolify", Body: msg})
	}
	s.notice = msg
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
		if s.away && s.settings.NotifyTracks && (s.track == nil || s.track.URI != e.URI) {
			s.fx.Notify(flatte.Notification{Title: nonEmpty(e.Name, "Now playing"), Body: strings.Join(e.Artists, ", ")})
		}
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
		fail(s, "Could not play this track ("+e.Kind+").")
	case player.Failed:
		fail(s, "The player stopped: "+e.Err.Error())
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
			if s.page == pageHome {
				openHome(s) // Home waited for the account
			}
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
// listed already (and not stale) or asked for. A stale list stays shown
// until a complete listing replaces it, with the cursor kept on its track.
func loadTracks(s *State, uri string) {
	if l, ok := s.listings[uri]; s.listing[uri] || ok && l.Complete() && !s.stale[uri] {
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
			fail(s, "Could not list the tracks: "+err.Error())
			return
		}
		if old, ok := s.listings[uri]; ok && s.stale[uri] {
			if !l.Complete() {
				return // still being listed again: keep what is shown
			}
			delete(s.stale, uri)
			if uri == s.shown {
				at := s.tracks.Cursor()
				s.listings[uri] = l
				s.tracks.SetCount(len(l.Tracks))
				if at < len(old.Tracks) {
					if i := indexOf(l.Tracks, old.Tracks[at].URI); i >= 0 {
						s.tracks.Select(i)
					}
				}
				askLiked(s, l.Tracks)
				return
			}
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

func indexOf(tracks []player.Track, uri string) int {
	for i, t := range tracks {
		if t.URI == uri {
			return i
		}
	}
	return -1
}

// setLiked saves track t to Liked Songs or removes it, shown at once — in
// Liked Songs too, one row in or out — and put back if the request fails;
// Liked Songs is then listed again behind what is shown.
func setLiked(s *State, t player.Track, on bool) {
	uri := t.URI
	if !strings.HasPrefix(uri, "spotify:track:") {
		return
	}
	was, known := s.liked[uri]
	s.liked[uri] = on
	if s.username != "" {
		likedSongs := player.LikedSongs(s.username)
		if l, ok := s.listings[likedSongs]; ok {
			s.listings[likedSongs] = withLiked(l, t, on)
			if s.shown == likedSongs {
				s.tracks.SetCount(len(s.listings[likedSongs].Tracks))
			}
			s.stale[likedSongs] = true
		}
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
			fail(s, "Could not update Liked Songs: "+err.Error())
		}
		if s.username != "" && s.shown == player.LikedSongs(s.username) {
			loadTracks(s, s.shown)
		}
	})
}

// withLiked is Liked Songs listing l with t added at the top, or removed.
func withLiked(l player.Listing, t player.Track, on bool) player.Listing {
	tracks := make([]player.Track, 0, len(l.Tracks)+1)
	if on && indexOf(l.Tracks, t.URI) < 0 {
		tracks = append(tracks, t)
	}
	for _, x := range l.Tracks {
		if on || x.URI != t.URI {
			tracks = append(tracks, x)
		}
	}
	l.Cached += len(tracks) - len(l.Tracks)
	l.Length, l.Tracks = len(tracks), tracks
	return l
}

// fetchCover fetches the cover at url once.
func fetchCover(s *State, url string) {
	if url == "" || s.img == nil || s.covers[url] != nil || s.fetching[url] {
		return
	}
	s.fetching[url] = true
	img := s.img
	type fetched struct {
		im   image.Image
		tint color.Color
	}
	flatte.Go(s.fx, "cover", func(ctx context.Context) (fetched, error) {
		im, err := img.Fetch(ctx, url)
		if err != nil || im == nil {
			return fetched{}, err
		}
		im = shrink(im, coverPixels)
		return fetched{im, tintOf(im)}, nil
	}, func(s *State, f fetched, err error) {
		delete(s.fetching, url)
		if err == nil && f.im != nil {
			s.covers[url], s.tints[url] = f.im, f.tint
		}
	})
}

// Cover sizes kept, in pixels: a header or player bar cover (at 2x), and
// a row's thumbnail; maxThumbs bounds how many thumbnails are kept.
const (
	coverPixels = 320
	thumbPixels = 96
	maxThumbs   = 600
)

// fetchThumb fetches the thumbnail of the cover at url once (while kept).
func fetchThumb(s *State, url string) {
	if url == "" || s.img == nil || s.thumbs[url] != nil || s.thumbing[url] {
		return
	}
	if len(s.thumbs) >= maxThumbs {
		s.thumbs = map[string]image.Image{} // fetched again as rows show
	}
	s.thumbing[url] = true
	img := s.img
	flatte.Go(s.fx, "thumb", func(ctx context.Context) (image.Image, error) {
		im, err := img.Fetch(ctx, url)
		if err != nil || im == nil {
			return nil, err
		}
		return shrink(im, thumbPixels), nil
	}, func(s *State, im image.Image, err error) {
		delete(s.thumbing, url)
		if err == nil && im != nil {
			s.thumbs[url] = im
		}
	})
}

// fetchVisible fetches the thumbnails of the rows on screen.
func fetchVisible(s *State) {
	lib := s.library()
	for i := s.rail.Offset(); i < min(s.rail.Offset()+s.rail.Height(), len(lib)); i++ {
		fetchThumb(s, lib[i].Image)
	}
	if s.compact {
		return
	}
	tracks := s.listings[s.shown].Tracks
	for i := s.tracks.Offset(); i < min(s.tracks.Offset()+s.tracks.Height(), len(tracks)); i++ {
		fetchThumb(s, tracks[i].Cover)
	}
}

// send runs a player request off the loop; a failure becomes the status.
func send(s *State, name string, req func(context.Context, Backend) error) {
	be := s.be
	flatte.Go(s.fx, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, req(ctx, be)
	}, func(s *State, _ struct{}, err error) {
		s.logf("request %s: err=%v", name, err)
		if err != nil && !errors.Is(err, player.ErrSuperseded) { // a newer command took over
			fail(s, "Could not "+strings.ReplaceAll(name, "-", " ")+": "+err.Error())
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
	cmdSettings   = "settings"
	cmdLike       = "like"       // the playing track
	cmdLikeTrack  = "like-track" // the selected track
	cmdHome       = "home"
	cmdSearch     = "search"
	cmdBack       = "back"
	cmdForward    = "forward"
	cmdCompact    = "compact"
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
		ctxURI, skip := s.shown, t.URI
		if virtual(ctxURI) { // where it was played from, else its album
			ctxURI = nonEmpty(t.ContextURI, t.AlbumURI)
			if ctxURI == "" {
				ctxURI, skip = t.URI, ""
			}
		}
		s.status = ""
		send(s, "play", func(ctx context.Context, be Backend) error { return be.Play(ctx, ctxURI, skip) })
	case cmdHome:
		if s.page != pageHome {
			visit(s)
		}
		openHome(s)
	case cmdSearch:
		focusSearch(s)
	case cmdBack, cmdForward:
		travel(s, id == cmdForward)
	case cmdCompact:
		changeSetting(s, setCompact, 1)
	case cmdSettings:
		openSettings(s)
	case cmdQueue:
		t, ok := s.selectedTrack()
		if !ok {
			return
		}
		tell(s, "Added “"+nonEmpty(t.Name, "the track")+"” to the queue.")
		send(s, "add-to-queue", func(ctx context.Context, be Backend) error { return be.Queue(ctx, t.URI) })
	case cmdLike:
		if s.track != nil {
			setLiked(s, *s.track, !s.liked[s.track.URI])
		}
	case cmdLikeTrack:
		if t, ok := s.selectedTrack(); ok {
			setLiked(s, t, !s.liked[t.URI])
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
	case flatte.FocusEvent:
		s.away = !e.Focused
		return
	case flatte.LayoutEvent:
		if e.Window != "" {
			return
		}
		if r, ok := e.Rects[idRail]; ok {
			s.rail.SetHeight(max(r.H/railRowLines, 1))
		}
		if r, ok := e.Rects[idTracks]; ok {
			s.tracks.SetHeight(max(r.H/s.trackRowLines(), 1))
		}
		if _, ok := e.Rects[idSettingsScroll]; ok {
			s.settingRects = e.Rects
			clampSettings(s)
		}
		defer fetchVisible(s)
		s.sliders = map[string]layout.Rect{}
		for _, id := range []string{idProgress, idVolume, idRailBar, idTrackBar} {
			if r, ok := e.Rects[id]; ok {
				s.sliders[id] = r
			}
		}
	case flatte.MenuEvent:
		// A context menu acts on the row clicked.
		if i, ok := rowIndex(idRail, e.Target); ok {
			visit(s)
			s.page = pageLibrary
			s.rail.Select(i)
			showSelected(s)
		}
		if i, ok := rowIndex(idTracks, e.Target); ok {
			s.tracks.Select(i)
			s.focus = paneTracks
		}
		command(s, e.ID, fx)
	case flatte.KeyEvent:
		if s.focus == paneDevice {
			deviceKey(s, e, fx)
			return
		}
		if s.page == pageSettings && s.focus == paneSettings && settingsKey(s, e) {
			return
		}
		if s.focus == paneSearch {
			searchKey(s, e, fx)
			return
		}
		handleKey(s, e, fx)
	case flatte.PasteEvent:
		text := strings.ReplaceAll(e.Text, "\n", " ")
		switch s.focus {
		case paneSearch:
			for _, r := range text {
				s.query.Insert(r)
			}
			queueSearch(s)
		case paneDevice:
			for _, r := range text {
				if len(s.deviceName.Value) < 64 {
					s.deviceName.Insert(r)
				}
			}
		}
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
	case flatte.KeyTab: // search field, tracks, library, around
		switch {
		case s.focus == paneLibrary && s.page == pageSearch:
			s.focus = paneSearch
		case s.focus == paneLibrary && len(s.listings[s.shown].Tracks) > 0:
			s.focus = paneTracks
		default:
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
	case p == paneTracks && up && s.page == pageSearch && s.tracks.Cursor() == 0:
		s.focus = paneSearch // above the results is the field
	case p == paneTracks && up:
		s.tracks.MoveUp()
	case p == paneTracks:
		s.tracks.MoveDown()
	case up:
		s.rail.MoveUp()
		s.page = pageLibrary
		showSelected(s)
	default:
		s.rail.MoveDown()
		s.page = pageLibrary
		showSelected(s)
	}
}

// showSelected shows the selected list, when the library is the page: its
// cover, and its tracks.
func showSelected(s *State) {
	pl, ok := s.selected()
	if !ok || s.page != pageLibrary {
		return
	}
	fetchCover(s, pl.Image)
	show(s, pl.URI)
	loadTracks(s, pl.URI)
}

// show puts the listing uri in the track table, from the top when it is a
// different one.
func show(s *State, uri string) {
	if uri != s.shown {
		s.shown = uri
		s.tracks.SetCount(len(s.listings[uri].Tracks))
		s.tracks.Select(0)
	}
}

// openHome shows Home: the recently played tracks, asked for afresh.
func openHome(s *State) {
	s.page, s.focus = pageHome, paneTracks
	show(s, uriHome)
	if s.username == "" {
		return // asked again once the account is known
	}
	be, user := s.be, s.username
	flatte.Latest(s.fx, "home", func(ctx context.Context) ([]player.Track, error) {
		return recentTracks(ctx, be, user)
	}, func(s *State, tracks []player.Track, err error) {
		if err != nil {
			s.logf("recently played: %v", err)
			fail(s, "Could not load Home: "+err.Error())
			return
		}
		setVirtual(s, uriHome, tracks)
	})
}

// homeLists bounds how many recently played lists Home looks into.
const homeLists = 20

// recentTracks are the tracks last played in the lists played lately,
// each once, played where it was played from. A list the player cannot
// list, or whose track it no longer has, is left out.
func recentTracks(ctx context.Context, be Backend, user string) ([]player.Track, error) {
	played, err := be.RecentlyPlayed(ctx, user, homeLists)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []player.Track
	for _, p := range played {
		if p.TrackURI == "" || seen[p.TrackURI] {
			continue
		}
		l, err := be.ContextTracks(ctx, p.ContextURI)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		for _, t := range l.Tracks {
			if t.URI == p.TrackURI && t.Name != "" {
				seen[t.URI] = true
				t.ContextURI = p.ContextURI
				out = append(out, t)
				break
			}
		}
	}
	return out, nil
}

// openSearch shows search with the field focused.
func openSearch(s *State) {
	s.page, s.focus = pageSearch, paneSearch
	show(s, uriSearch)
}

// setVirtual sets the listing of Home or search to tracks.
func setVirtual(s *State, uri string, tracks []player.Track) {
	s.listings[uri] = player.Listing{URI: uri, Ready: true, Length: len(tracks), Cached: len(tracks), Tracks: tracks}
	if s.shown == uri {
		s.tracks.SetCount(len(tracks))
		s.tracks.Select(0)
	}
	askLiked(s, tracks)
}

// searchKey edits the search field. Enter searches at once and Esc, Tab,
// and ↓ go to the results.
func searchKey(s *State, k flatte.KeyEvent, fx flatte.Effects[State]) {
	switch k.Key {
	case flatte.KeyEscape, flatte.KeyTab, flatte.KeyDown:
		s.focus = paneTracks
		if k.Key == flatte.KeyEscape && len(s.listings[uriSearch].Tracks) == 0 {
			s.focus = paneLibrary
		}
		return
	case flatte.KeyEnter:
		search(s, 0)
		if len(s.listings[uriSearch].Tracks) > 0 {
			s.focus = paneTracks
		}
		return
	case flatte.KeyCtrlC:
		fx.Quit()
		return
	}
	if clipboardKey(&s.query, k, fx) || editField(&s.query, k) {
		queueSearch(s)
	}
}

// editField applies an editing key to f, reporting whether the value
// changed (a move or selection changes only the cursor).
func editField(f *flatui.TextField, k flatte.KeyEvent) bool {
	before := f.Value
	return f.Edit(k) && f.Value != before
}

// clipboardKey copies (⌘C) or cuts (⌘X) f's selection, reporting whether
// the value changed. Pastes arrive as PasteEvents.
func clipboardKey(f *flatui.TextField, k flatte.KeyEvent, fx flatte.Effects[State]) bool {
	if k.Key != flatte.KeyCharacter || k.Mod != flatte.ModSuper || (k.Rune != 'c' && k.Rune != 'x') {
		return false
	}
	text := f.SelectedText()
	if text == "" {
		return false
	}
	fx.SetClipboard(text)
	if k.Rune == 'x' {
		f.Backspace() // deletes the selection
		return true
	}
	return false
}

// queueSearch searches once typing pauses: the first character opens the
// search page and clearing the field leaves it.
func queueSearch(s *State) {
	searched := strings.TrimSpace(s.query.Value) != ""
	switch {
	case searched && s.page != pageSearch:
		visit(s)
		openSearch(s)
	case !searched && s.page == pageSearch:
		leaveSearch(s)
	}
	search(s, s.searchDelay)
}

// focusSearch puts the keyboard in the search field, on whatever page is
// shown; the search page opens only for a query (one already there, or
// the first character typed).
func focusSearch(s *State) {
	s.focus = paneSearch
	if strings.TrimSpace(s.query.Value) != "" && s.page != pageSearch {
		visit(s)
		openSearch(s)
	}
}

// leaveSearch goes back to where search was opened from (Home when there
// is nowhere to go back to), keeping the keyboard in the field.
func leaveSearch(s *State) {
	if n := len(s.back); n > 0 {
		p := s.back[n-1]
		s.back = s.back[:n-1]
		goTo(s, p)
	} else {
		openHome(s)
	}
	s.focus = paneSearch
}

// search looks the query up after delay; a newer search supersedes it, and
// an empty query clears the results.
func search(s *State, delay time.Duration) {
	q := strings.TrimSpace(s.query.Value)
	if q == "" || s.web == nil {
		flatte.Cancel(s.fx, "search")
		delete(s.listings, uriSearch)
		if s.shown == uriSearch {
			s.tracks.SetCount(0)
		}
		return
	}
	web := s.web
	flatte.Latest(s.fx, "search", func(ctx context.Context) (spotify.Results, error) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return spotify.Results{}, ctx.Err()
			}
		}
		return web.Search(ctx, q, 30)
	}, func(s *State, r spotify.Results, err error) {
		s.logf("search %q: %d tracks, err=%v", q, len(r.Tracks), err)
		if err != nil {
			fail(s, "Could not search: "+err.Error())
			return
		}
		s.status = ""
		tracks := make([]player.Track, len(r.Tracks))
		for i, t := range r.Tracks {
			tracks[i] = trackFrom(t)
		}
		setVirtual(s, uriSearch, tracks)
	})
}

// trackFrom is a Web API track as the player's.
func trackFrom(t spotify.Track) player.Track {
	out := player.Track{
		URI: t.URI, Name: t.Name, Album: t.Album.Name, AlbumURI: t.Album.URI,
		Duration: time.Duration(t.DurationMS) * time.Millisecond, Cover: spotify.Cover(t.Album.Images, 128),
	}
	for _, a := range t.Artists {
		out.Artists = append(out.Artists, a.Name)
		out.ArtistURIs = append(out.ArtistURIs, a.URI)
	}
	return out
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
	if settingClicked(s, m.Target) {
		return
	}
	if i, ok := rowIndex(idRail, m.Target); ok {
		if i == s.rail.Cursor() && s.focus == paneLibrary && s.page == pageLibrary {
			command(s, cmdPlayList, fx) // a click on the selected one plays it
			return
		}
		visit(s)
		s.focus, s.page = paneLibrary, pageLibrary
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
	case idHome:
		command(s, cmdHome, fx)
	case idShufflePlay:
		command(s, cmdShuffle, fx)
	case idBack:
		command(s, cmdBack, fx)
	case idForward:
		command(s, cmdForward, fx)
	case idSettings:
		command(s, cmdSettings, fx)
	case idSearch, idQuery, idSearchBox:
		command(s, cmdSearch, fx)
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
	case s.page == pageSettings && !strings.HasPrefix(m.Target, idRail) && m.Target != idProgress:
		s.settingsScroll = min(max(s.settingsScroll+rows*2, 0), s.settingsMax)
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
