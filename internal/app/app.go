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
	"slices"
	"strings"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/librarycache"
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

// Web is Spotify search as the UI uses it (spotify.Client, or a fake in tests).
type Web interface {
	Search(ctx context.Context, q string, limit int) (spotify.Results, error)
}

type likedTracksWeb interface {
	LikedTracks(ctx context.Context, offset, limit int) (spotify.SavedTracksPage, error)
}

type likedLoad struct {
	generation uint64
	offset     int
	total      int
	tracks     []player.Track
	original   *player.Listing
	restarts   int
	failures   int
	inFlight   bool
	retryAt    time.Time
	err        string
}

const likedPageSize = 50

// Images fetches cover art.
type Images interface {
	Fetch(ctx context.Context, url string) (image.Image, error)
}

// State is the whole UI.
type State struct {
	discovery       discoveryState
	cacheLoading    bool
	libraryRead     bool
	be              Backend
	img             Images
	web             Web
	cache           *librarycache.Store
	cacheEpoch      uint64
	cacheLoaded     string
	cacheRevision   map[string]uint64
	cacheLikesDirty bool
	// fx is the session's Effects, kept so player events (which arrive in a
	// fold) can start requests — the capture-fx idiom.
	fx               flatte.Effects[State]
	account          *flatte.Scope
	playerEvents     *flatte.Scope
	playerEventsDone chan struct{}

	// pairing is the sign-in code to show (zero: none); ready reports the
	// player signed in.
	pairing player.Pairing
	ready   bool

	track    *player.Track
	pending  *playIntent
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
	listings        map[string]player.Listing
	listing         map[string]bool
	stale           map[string]bool // listed, but changed since: asked again until complete
	likedLoads      map[string]*likedLoad
	likedGeneration map[string]uint64
	likeChanges     map[string]*likeChange
	likedWrites     int
	tracks          flatui.List
	shown           string
	focus           pane
	trackSort       flatui.Select
	trackFilter     flatui.TextField
	trackFilterOpen bool

	// back and forward are the places visited before and after this one.
	back, forward []place
	// compact shows tracks one line each, without thumbnails.
	compact bool

	// page is what the center shows; query is the search field, and
	// searchDelay how long typing must pause before it searches.
	page        page
	query       flatui.TextField
	searchDelay time.Duration
	// searchTerm identifies the pending or completed result; failures can
	// be retried with Enter, while completed results need no second fetch.
	searchTerm    string
	searchPending bool
	searchFailed  bool

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
	activeAudio                 player.Audio
	applyingAudio               bool
	settingQuality              flatui.Select
	settingCrossfade            flatui.Select
	settingsPath                string
	settingsWriter              *settingsWriter
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
	paneSort        // the song ordering dropdown
	paneTrackFilter // the current playlist filter
	paneSearch      // the search field: keys type
	paneSettings    // the Settings page's rows
	paneDevice      // the device name field: keys type
	panePlayer      // default playback focus after a background click
)

// focusPlayer releases transient controls without changing list selections.
func focusPlayer(s *State) {
	s.trackSort.Close()
	s.focus = panePlayer
}

// page is what the center shows.
type page int

const (
	pageLibrary page = iota // the list selected in the library
	pageHome
	pageSearch
	pageSettings
	pageBrowse // album, artist, station, or playlist outside the library rail
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
	if pl, ok := s.selected(); ok && (s.page == pageLibrary || s.page == pageBrowse) {
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
	case pageBrowse:
		p := s.discovery.Pages[p.list]
		openBrowse(s, p.URI, p.Name, p.Image)
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
	return s.focus == paneSearch || s.focus == paneDevice || s.focus == paneTrackFilter || s.page == pageSettings && s.focus == paneSettings
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
	idRail           = "rail"
	idTracks         = "tracks"
	idRailBar        = "rail-bar"
	idTrackBar       = "tracks-bar"
	idPlay           = "play"
	idPrev           = "prev"
	idNext           = "next"
	idShuffle        = "shuffle"
	idRepeat         = "repeat"
	idProgress       = "progress"
	idVolume         = "volume"
	idPlaybackSource = "playback-source"
	idPlayList       = "play-playlist"
	idLike           = "like"
	idHome           = "home"
	idBack           = "back"
	idForward        = "forward"
	idShufflePlay    = "shuffle-list"
	idSearch         = "search"
	idSettings       = "settings-button"
	idSearchBox      = "search-box"
	// idSettingsScroll is the Settings page's scroll.
	idSettingsScroll  = "settings-scroll"
	idSettingsContent = "settings-scroll.content"
	idQuery           = "query"
	idOpenPair        = "open-pair"
)

// New is the app for backend be, with web for search (nil:
// neither). debug, when not nil, receives a log of player events,
// commands, and request failures.
func New(be Backend, img Images, web Web, debug io.Writer) flatte.App[State] {
	s := &State{
		be: be, img: img, web: web, volMax: 100, debug: debug, searchDelay: 300 * time.Millisecond,
		covers: map[string]image.Image{}, tints: map[string]color.Color{}, thumbs: map[string]image.Image{}, thumbing: map[string]bool{}, fetching: map[string]bool{},
		listings: map[string]player.Listing{}, listing: map[string]bool{}, stale: map[string]bool{},
		likedLoads: map[string]*likedLoad{}, likedGeneration: map[string]uint64{},
		liked: map[string]bool{}, asking: map[string]bool{}, likeChanges: map[string]*likeChange{},
		page: pageHome, shown: uriHome, focus: paneTracks,
		settings:      DefaultSettings(),
		cacheRevision: map[string]uint64{},
	}
	s.trackSort.SetOptions(regularSortOptions)
	s.activeAudio = s.settings.Audio()
	initAudioSettings(s)
	s.deviceName = flatui.TextField{Value: s.settings.DeviceName, Cursor: len(s.settings.DeviceName)}
	return flatte.App[State]{
		State: s, Init: Init, Handle: Handle, View: View, Menus: Menus, ContextMenu: ContextMenu,
	}
}

// Init follows the player's events and ticks the position while playing.
func Init(s *State, fx flatte.Effects[State]) {
	s.fx = fx
	followPlayer(s)
	_ = flatte.Every(fx, "tick", time.Second, tick)
}

// followPlayer starts a separately scoped stream so sign-out can fence
// player events already queued on the UI loop as well as request results.
func followPlayer(s *State) {
	events := s.be.Events()
	if events == nil {
		return
	}
	scope := flatte.NewScope(s.fx, "player")
	done := make(chan struct{})
	s.playerEvents, s.playerEventsDone = scope, done
	flatte.ScopeStream(scope, s.fx, func(ctx context.Context, send func(player.Event)) {
		defer close(done)
		for {
			if ctx.Err() != nil {
				return
			}
			select {
			case ev, ok := <-events:
				if !ok {
					return
				}
				send(ev)
			case <-ctx.Done():
				return
			}
		}
	}, onPlayer)
}

// tick advances the position of a playing track by a second, and asks
// again for a selected list the player is still listing.
func tick(s *State, _ time.Time) {
	if s.playing && s.track != nil && s.drag != idProgress {
		s.position = min(s.position+time.Second, s.track.Duration)
	}
	if s.page == pageBrowse {
		tickBrowse(s)
		return
	}
	if !(likedURI(s, s.shown) && s.likedWrites > 0) {
		if s.likedLoads[s.shown] != nil && likedURI(s, s.shown) {
			loadLikedTracks(s, s.shown)
		} else if l, ok := s.listings[s.shown]; ok && (!l.Complete() || s.stale[s.shown]) && !virtual(s.shown) {
			loadTracks(s, s.shown)
		}
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
	case player.WillPlay:
		bindPendingPlay(s, e.URI, e.ContextURI)
	case player.Track:
		enrichPendingPlay(s, e)
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
		confirmPendingPlay(s, e)
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
		if loadingPlay(s) && !pendingMatches(s, e.URI, "") {
			return
		}
		if pendingMatches(s, e.URI, "") {
			clearPendingPlay(s)
		}
		fail(s, "Could not play this track ("+e.Kind+").")
	case player.Failed:
		s.playing = false
		clearPendingPlay(s)
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
	accountGo(s, "library", func(ctx context.Context) ([]player.Playlist, error) {
		return be.Playlists(ctx)
	}, func(s *State, pls []player.Playlist, err error) {
		if err != nil {
			s.loaded = false
			fail(s, "Could not load the library: "+err.Error())
			return
		}
		s.playlists = pls
		s.libraryRead = true
		libraryChanged(s)
		saveCache(s)
	})
}

// refreshStatus reads the player's state (a track already loaded, the
// volume and modes).
func refreshStatus(s *State) {
	be := s.be
	accountGo(s, "status", func(ctx context.Context) (player.Status, error) {
		return be.Status(ctx)
	}, func(s *State, st player.Status, err error) {
		if err != nil {
			return // events will tell
		}
		s.ready = true
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
			hydrateCache(s, st.Username)
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
	if likedURI(s, uri) {
		if s.likedWrites > 0 {
			return
		}
		if _, ok := s.web.(likedTracksWeb); ok {
			loadLikedTracks(s, uri)
			return
		}
	}
	if l, ok := s.listings[uri]; s.listing[uri] || ok && l.Complete() && !s.stale[uri] {
		return
	}
	s.listing[uri] = true
	be := s.be
	accountGo(s, "tracks", func(ctx context.Context) (player.Listing, error) { return be.ContextTracks(ctx, uri) }, func(s *State, l player.Listing, err error) {
		delete(s.listing, uri)
		if err != nil {
			s.logf("tracks %s: %v", uri, err)
			fail(s, "Could not list the tracks: "+err.Error())
			return
		}
		if s.stale[uri] {
			if !l.Complete() {
				return
			}
			delete(s.stale, uri)
		}
		replaceListing(s, uri, l)
		askLiked(s, l.Tracks)
	})
}

func loadLikedTracks(s *State, uri string) {
	if l, ok := s.listings[uri]; ok && l.Complete() && !s.stale[uri] {
		return
	}
	stage := s.likedLoads[uri]
	if stage == nil {
		s.likedGeneration[uri]++
		stage = &likedLoad{generation: s.likedGeneration[uri]}
		if old, ok := s.listings[uri]; ok {
			copyOld := old
			stage.original = &copyOld
		}
		s.likedLoads[uri] = stage
	}
	if stage.inFlight || !stage.retryAt.IsZero() && time.Now().Before(stage.retryAt) {
		return
	}
	web := s.web.(likedTracksWeb)
	generation := stage.generation
	offset := stage.offset
	stage.inFlight = true
	accountGo(s, "liked-tracks:"+uri, func(ctx context.Context) (spotify.SavedTracksPage, error) {
		return web.LikedTracks(ctx, offset, likedPageSize)
	}, func(s *State, page spotify.SavedTracksPage, err error) {
		current := s.likedLoads[uri]
		if current == nil || current.generation != generation || s.likedGeneration[uri] != generation {
			return
		}
		current.inFlight = false
		if err != nil {
			current.err = err.Error()
			current.failures++
			current.retryAt = time.Now().Add(likedRetryDelay(err, current.failures))
			if current.offset > 0 && current.failures >= 3 {
				resetLikedPageStage(s, uri, current)
			}
			s.logf("liked tracks %s offset %d: %v", uri, offset, err)
			return
		}
		if page.Offset != offset || page.Limit <= 0 || len(page.Items) > page.Limit || page.Total < 0 {
			current.err = "Spotify returned an inconsistent page; retrying"
			current.failures++
			current.retryAt = time.Now().Add(likedRetryDelay(nil, current.failures))
			if current.failures >= 3 {
				resetLikedPageStage(s, uri, current)
				current.failures = 0
			}
			return
		}
		if current.offset > 0 && page.Total != current.total {
			if current.restarts >= 2 {
				current.err = "Liked Songs changed while paging; retrying"
				current.failures++
				current.retryAt = time.Now().Add(likedRetryDelay(nil, current.failures))
				resetLikedPageStage(s, uri, current)
				current.restarts, current.failures = 0, 0
				return
			}
			current.restarts++
			resetLikedPageStage(s, uri, current)
			loadLikedTracks(s, uri)
			return
		}
		if len(page.Items) == 0 && offset < page.Total || offset+len(page.Items) > page.Total {
			current.err = "Spotify returned an inconsistent page; retrying"
			current.failures++
			current.retryAt = time.Now().Add(likedRetryDelay(nil, current.failures))
			if current.failures >= 3 {
				resetLikedPageStage(s, uri, current)
				current.failures = 0
			}
			return
		}
		current.err, current.retryAt, current.failures = "", time.Time{}, 0
		current.total = max(page.Total, 0)
		for _, saved := range page.Items {
			track := savedTrack(saved)
			current.tracks = append(current.tracks, track)
			if strings.HasPrefix(track.URI, "spotify:track:") {
				s.liked[track.URI] = true
			}
		}
		current.offset += len(page.Items)
		if current.offset < current.total && len(page.Items) > 0 {
			if current.original == nil {
				replaceListing(s, uri, player.Listing{URI: uri, Ready: true, Length: current.total, Cached: len(current.tracks), Tracks: slices.Clone(current.tracks)})
			}
			loadLikedTracks(s, uri)
			return
		}
		listing := player.Listing{URI: uri, Ready: true, Length: current.total, Cached: len(current.tracks), Tracks: current.tracks}
		allIdentified := true
		pageLiked := make(map[string]bool, len(listing.Tracks))
		for _, track := range listing.Tracks {
			if strings.HasPrefix(track.URI, "spotify:track:") {
				pageLiked[track.URI] = true
			} else {
				allIdentified = false
			}
		}
		if allIdentified {
			for trackURI, knownLiked := range s.liked {
				if knownLiked && !pageLiked[trackURI] {
					s.liked[trackURI] = false
				}
			}
		}
		delete(s.likedLoads, uri)
		delete(s.stale, uri)
		replaceListing(s, uri, listing)
		saveCache(s)
	})
}

func resetLikedPageStage(s *State, uri string, stage *likedLoad) {
	stage.offset, stage.total, stage.tracks = 0, 0, nil
	if stage.original == nil {
		delete(s.listings, uri)
		if s.shown == uri {
			s.tracks.SetCount(0)
		}
	} else {
		s.listings[uri] = *stage.original
	}
}

func savedTrack(saved spotify.SavedTrack) player.Track {
	track := saved.Track
	artists, artistURIs := make([]string, 0, len(track.Artists)), make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		artists = append(artists, artist.Name)
		artistURIs = append(artistURIs, artist.URI)
	}
	return player.Track{
		URI: track.URI, Name: track.Name, Artists: artists, ArtistURIs: artistURIs,
		Album: track.Album.Name, AlbumURI: track.Album.URI,
		Cover: spotify.Cover(track.Album.Images, 128), Duration: time.Duration(track.DurationMS) * time.Millisecond,
		AddedAt: saved.AddedAt,
	}
}

func likedRetryDelay(err error, failures int) time.Duration {
	var spotifyErr spotify.Error
	if errors.As(err, &spotifyErr) && spotifyErr.Status == 429 {
		if seconds, parseErr := time.ParseDuration(spotifyErr.RetryAfter + "s"); parseErr == nil && seconds > 0 {
			return seconds
		}
	}
	return min(5*time.Second*time.Duration(1<<min(max(failures-1, 0), 4)), time.Minute)
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
	accountGo(s, "liked", func(ctx context.Context) ([]bool, error) {
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
	delta := len(tracks) - len(l.Tracks)
	l.Cached = max(0, l.Cached+delta)
	if l.Ready {
		l.Length = max(0, l.Length+delta)
	}
	l.Tracks = tracks
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
	accountGo(s, "cover", func(ctx context.Context) (fetched, error) {
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
	accountGo(s, "thumb", func(ctx context.Context) (image.Image, error) {
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
	tracks := displayListing(s, s.listings[s.shown]).Tracks
	for i := s.tracks.Offset(); i < min(s.tracks.Offset()+s.tracks.Height(), len(tracks)); i++ {
		fetchThumb(s, tracks[i].Cover)
	}
}

// send runs a player request off the loop; a failure becomes the status.
func send(s *State, name string, req func(context.Context, Backend) error) {
	be := s.be
	done := s.playerEventsDone
	accountGo(s, name, func(ctx context.Context) (struct{}, error) {
		if name == "sign-out" && done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return struct{}{}, ctx.Err()
			}
		}
		return struct{}{}, req(ctx, be)
	}, func(s *State, _ struct{}, err error) {
		if name == "sign-out" {
			followPlayer(s)
		}
		s.logf("request %s: err=%v", name, err)
		if err != nil && !errors.Is(err, player.ErrSuperseded) { // a newer command took over
			fail(s, "Could not "+strings.ReplaceAll(name, "-", " ")+": "+err.Error())
			if name == "sign-out" {
				loadLibrary(s)
				refreshStatus(s)
			}
		}
	})
}

// sendLatest supersedes an earlier setting change while retaining account fencing.
func sendLatest(s *State, name string, req func(context.Context, Backend) error) {
	be := s.be
	accountLatest(s, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, req(ctx, be)
	}, func(s *State, _ struct{}, err error) {
		s.logf("request %s: err=%v", name, err)
		if err != nil && !errors.Is(err, player.ErrSuperseded) && !errors.Is(err, context.Canceled) {
			fail(s, "Could not "+strings.ReplaceAll(name, "-", " ")+": "+err.Error())
		}
	})
}

// selected is the list under the rail's cursor.
func (s *State) selected() (player.Playlist, bool) {
	if s.page == pageBrowse {
		return browseSelected(s)
	}
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
	l = displayListing(s, l)
	i := s.tracks.Cursor()
	if !ok || i < 0 || i >= len(l.Tracks) {
		return player.Track{}, false
	}
	return l.Tracks[i], true
}

// Commands, from keys, clicks, and menus.
const (
	cmdPlayPause      = "play-pause"
	cmdNext           = "next"
	cmdPrev           = "previous"
	cmdShuffle        = "shuffle"
	cmdRepeat         = "repeat"
	cmdVolumeUp       = "volume-up"
	cmdVolumeDown     = "volume-down"
	cmdSeekAhead      = "seek-ahead"
	cmdSeekBack       = "seek-back"
	cmdPlayList       = "play-playlist"
	cmdPlaybackSource = "show-playback-source"
	cmdPlayTrack      = "play-track"
	cmdQueue          = "queue"
	cmdSettings       = "settings"
	cmdLike           = "like"       // the playing track
	cmdLikeTrack      = "like-track" // the selected track
	cmdHome           = "home"
	cmdSearch         = "search"
	cmdBack           = "back"
	cmdForward        = "forward"
	cmdCompact        = "compact"
	cmdQuit           = "quit"
)

const (
	seekStep   = 5 * time.Second
	volumeStep = 5
)

// command runs a command.
func command(s *State, id string, fx flatte.Effects[State]) {
	s.logf("command %s (track loaded: %v, playing: %v)", id, s.track != nil, s.playing)
	s.notice = ""
	if discoveryCommand(s, id) {
		return
	}
	switch id {
	case cmdPlayPause:
		if loadingPlay(s) {
			return
		}
		if s.track == nil {
			command(s, cmdPlayList, fx)
			return
		}
		s.playing = !s.playing // shown at once; the player's event confirms
		send(s, "play-pause", func(ctx context.Context, be Backend) error { return be.PlayPause(ctx) })
	case cmdNext:
		clearPendingPlay(s)
		send(s, "skip", func(ctx context.Context, be Backend) error { return be.Next(ctx) })
	case cmdPrev:
		clearPendingPlay(s)
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
		if !ok || loadingPlaylist(s, pl.URI) {
			return
		}
		var first player.Track
		if l := s.listings[pl.URI]; !s.shuffle && len(l.Tracks) > 0 {
			first = l.Tracks[0]
		}
		requestPlay(s, pl.URI, "", first)
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
		requestPlay(s, ctxURI, skip, t)
	case cmdPlaybackSource:
		showPlaybackSource(s)
	case cmdHome:
		if s.page != pageHome {
			visit(s)
		}
		openHome(s)
	case cmdSearch:
		focusSearch(s)
	case cmdBack, cmdForward:
		travel(s, id == cmdForward)
	case cmdTrackFilter:
		openTrackFilter(s)
	case cmdSort:
		s.focus = paneSort
		s.trackSort.Toggle()
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
	accountLatest(s, "volume", func(ctx context.Context) (struct{}, error) {
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
	accountLatest(s, "seek", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, be.Seek(ctx, to)
	}, func(*State, struct{}, error) {})
}

// Handle routes events.
func Handle(s *State, ev flatte.Event, fx flatte.Effects[State]) {
	defer collapseEmptyTrackFilter(s)
	switch e := ev.(type) {
	case flatte.FocusEvent:
		s.away = !e.Focused
		return
	case flatte.LayoutEvent:
		if e.Window != "" {
			return
		}
		s.rail.Adopt(e, idRail)
		s.tracks.Adopt(e, idTracks)
		if _, ok := e.Rects[idSettingsScroll]; ok {
			s.settingRects = e.Rects
			clampSettings(s)
		}
		if _, ok := e.Rects[idHomeScroll]; ok {
			s.discovery.HomeRects = e.Rects
			s.discovery.HomeWidthPoints = e.PointRects[idHomeContent].W
			clampHomeScroll(s)
			fetchVisibleHomeCovers(s)
		}
		defer fetchVisible(s)
		s.sliders = map[string]layout.Rect{}
		for _, id := range []string{idProgress, idVolume, idRailBar, idTrackBar} {
			if r, ok := e.Rects[id]; ok {
				s.sliders[id] = r
			}
		}
	case flatte.MenuEvent:
		if e.ID != cmdSort {
			s.trackSort.Close()
		}
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
		if homeKey(s, e) {
			return
		}
		if s.focus == paneTrackFilter {
			filterKey(s, e, fx)
			return
		}
		if sortKey(s, e) {
			return
		}
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
		case paneTrackFilter:
			changeTrackFilter(s, func(field *flatui.TextField) {
				for _, r := range text {
					field.Insert(r)
				}
			})
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
	case flatte.KeyTab: // search field, tracks, sort, library, around
		switch {
		case s.focus == paneTracks && s.page == pageLibrary:
			s.focus = paneTrackFilter
		case s.focus == paneLibrary && s.page == pageSearch:
			s.focus = paneSearch
		case s.focus == paneLibrary && len(s.listings[s.shown].Tracks) > 0:
			s.focus = paneTracks
		default:
			s.focus = paneLibrary
		}
	case flatte.KeyUp, flatte.KeyDown:
		if s.focus != panePlayer {
			move(s, s.focus, k.Key == flatte.KeyUp)
		}
	case flatte.KeyEnter:
		if s.focus == panePlayer {
			command(s, cmdPlayPause, fx)
		} else if s.focus == paneTracks {
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
	s.trackSort.Close()
	if uri != s.shown {
		if likedURI(s, uri) && hasLikedTracks(s) {
			s.trackSort.SetOptions(sortOptions)
			s.trackSort.Select(sortAddedAtDesc)
		} else {
			s.trackSort.SetOptions(regularSortOptions)
			s.trackSort.Select(sortPositionAsc)
		}
		s.trackFilter = flatui.TextField{}
		s.trackFilterOpen = false
		s.shown = uri
		s.tracks.SetCount(len(s.listings[uri].Tracks))
		s.tracks.Select(0)
	}
}

// openHome restores this session's Home without repeating its requests.
func openHome(s *State) {
	s.page, s.focus = pageHome, paneTracks
	show(s, uriHome)
	if s.username == "" {
		return // asked again once the account is known
	}
	loadHomeFeed(s)
	if s.discovery.RecentRequested {
		return
	}
	s.discovery.RecentRequested = true
	be, user := s.be, s.username
	accountLatest(s, "home", func(ctx context.Context) ([]player.Track, error) {
		return recentTracks(ctx, be, user)
	}, func(s *State, tracks []player.Track, err error) {
		if err != nil {
			if len(tracks) > 0 {
				setVirtual(s, uriHome, tracks)
			}
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
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	played, err := be.RecentlyPlayed(ctx, user, homeLists)
	if err != nil {
		return nil, err
	}
	found := map[string]player.Track{}
	finished := map[int]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return recentResult(played, found, err)
		}
		pending := false
		listings := map[string]player.Listing{}
		for i, p := range played {
			if p.TrackURI == "" || finished[i] || found[p.TrackURI].URI != "" {
				continue
			}
			l, ok := listings[p.ContextURI]
			if !ok {
				l, err = be.ContextTracks(ctx, p.ContextURI)
				if err != nil {
					if ctx.Err() != nil {
						return recentResult(played, found, ctx.Err())
					}
					finished[i] = true
					continue
				}
				listings[p.ContextURI] = l
			}
			for _, t := range l.Tracks {
				if t.URI == p.TrackURI && t.Name != "" {
					t.ContextURI = p.ContextURI
					found[t.URI], finished[i] = t, true
					break
				}
			}
			if !finished[i] {
				if l.Complete() {
					finished[i] = true
				} else {
					pending = true
				}
			}
		}
		if !pending {
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return recentResult(played, found, ctx.Err())
		case <-timer.C:
		}
	}
	return recentResult(played, found, nil)
}

func recentResult(played []player.Played, found map[string]player.Track, err error) ([]player.Track, error) {
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	seen := map[string]bool{}
	var out []player.Track
	for _, p := range played {
		if t, ok := found[p.TrackURI]; ok && !seen[p.TrackURI] {
			out = append(out, t)
			seen[p.TrackURI] = true
		}
	}
	return out, err
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
		if s.searchFailed {
			s.status = ""
		}
		s.searchTerm, s.searchPending, s.searchFailed = "", false, false
		delete(s.listings, uriSearch)
		if s.shown == uriSearch {
			s.tracks.SetCount(0)
		}
		return
	}
	if q == s.searchTerm && !s.searchPending && !s.searchFailed {
		return // Enter on completed results only moves focus.
	}
	if s.searchFailed {
		s.status = ""
	}
	s.searchTerm, s.searchPending, s.searchFailed = q, true, false
	delete(s.listings, uriSearch)
	if s.shown == uriSearch {
		s.tracks.SetCount(0)
	}
	web := s.web
	accountLatest(s, "search", func(ctx context.Context) (spotify.Results, error) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return spotify.Results{}, ctx.Err()
			}
		}
		return web.Search(ctx, q, 30)
	}, func(s *State, r spotify.Results, err error) {
		s.searchPending = false
		s.logf("search %q: %d tracks, err=%v", q, len(r.Tracks), err)
		if err != nil {
			s.searchFailed = true
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

// trackFrom converts search metadata to a player track.
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
	if s.drag == "" && handleFieldMouse(s, m) {
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
		up := m.Button == flatte.MouseWheelUp
		if s.trackSort.IsOpen() {
			if m.Target == idSort+":menu" || strings.HasPrefix(m.Target, idSort+":option:") {
				if up {
					s.trackSort.MoveUp()
				} else {
					s.trackSort.MoveDown()
				}
			}
			return // an open popup owns the wheel, including clicks outside it
		}
		if s.settingQuality.IsOpen() || s.settingCrossfade.IsOpen() {
			switch {
			case s.settingQuality.IsOpen() && strings.HasPrefix(m.Target, setQuality+":select:option:"):
				if up {
					s.settingQuality.MoveUp()
				} else {
					s.settingQuality.MoveDown()
				}
			case s.settingCrossfade.IsOpen() && strings.HasPrefix(m.Target, setCrossfade+":select:option:"):
				if up {
					s.settingCrossfade.MoveUp()
				} else {
					s.settingCrossfade.MoveDown()
				}
			}
			return // don't scroll settings under an open dropdown
		}
		wheel(s, m, fx)
		return
	}
	if m.Button != flatte.MouseLeft || m.Action != flatte.MousePress {
		return
	}
	focusBefore := s.focus
	if m.Target != idTrackFilter && m.Target != idTrackFilterBox && m.Target != idTrackFilterToggle {
		blurTrackFilter(s)
	}
	if sortClick(s, m.Target) {
		return
	}
	if !strings.HasPrefix(m.Target, setQuality+":select") {
		s.settingQuality.Close()
	}
	if !strings.HasPrefix(m.Target, setCrossfade+":select") {
		s.settingCrossfade.Close()
	}
	if settingClicked(s, m.Target) {
		return
	}
	if homeClick(s, m.Target) {
		return
	}
	if m.Target == "browse-retry" {
		retryBrowse(s)
		return
	}
	if uri, ok := strings.CutPrefix(m.Target, "browse:"); ok {
		if discoveryClick(s, uri) {
			return
		}
	}
	if i, ok := rowIndex(idRail, m.Target); ok {
		if i == s.rail.Cursor() && focusBefore == paneLibrary && s.page == pageLibrary {
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
		if i == s.tracks.Cursor() && focusBefore == paneTracks {
			command(s, cmdPlayTrack, fx)
			return
		}
		s.focus = paneTracks
		s.tracks.Select(i)
		return
	}
	switch m.Target {
	case idTrackFilterToggle:
		openTrackFilter(s)
	case idTrackFilter, idTrackFilterBox:
		openTrackFilter(s)
		if m.HasCaret {
			s.trackFilter.SetCursor(m.Caret)
		}
	case idPlay:
		focusPlayer(s)
		command(s, cmdPlayPause, fx)
	case idPrev:
		focusPlayer(s)
		command(s, cmdPrev, fx)
	case idNext:
		focusPlayer(s)
		command(s, cmdNext, fx)
	case idShuffle:
		focusPlayer(s)
		command(s, cmdShuffle, fx)
	case idRepeat:
		focusPlayer(s)
		command(s, cmdRepeat, fx)
	case idPlaybackSource:
		command(s, cmdPlaybackSource, fx)
	case idPlayList:
		focusPlayer(s)
		command(s, cmdPlayList, fx)
	case idLike:
		focusPlayer(s)
		command(s, cmdLike, fx)
	case idHome:
		command(s, cmdHome, fx)
	case idShufflePlay:
		focusPlayer(s)
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
		if m.Target == idRailBar {
			s.focus = paneLibrary
		} else {
			s.focus = paneTracks
		}
		s.drag = m.Target
		scrollBar(s, m.Target, m.Y-m.TargetRect.Y, m.TargetRect.H)
	case idProgress, idVolume:
		focusPlayer(s)
		if m.Target == idProgress && s.track == nil {
			return
		}
		s.drag = m.Target
		s.dragValue = layout.Slider{}.ValueAt(m.X-m.TargetRect.X, m.TargetRect.W)
	default:
		focusPlayer(s)
	}
}

// wheel acts only on a target that is itself scrollable or a player slider.
func wheel(s *State, m flatte.MouseEvent, fx flatte.Effects[State]) {
	up := m.Button == flatte.MouseWheelUp
	rows := 1
	if up {
		rows = -1
	}
	_, railRow := rowIndex(idRail, m.Target)
	_, trackRow := rowIndex(idTracks, m.Target)
	settingTarget := false
	for _, id := range settingIDs {
		if m.Target == id || strings.HasPrefix(m.Target, id+":") {
			settingTarget = true
			break
		}
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
	case m.Target == idRail || railRow || m.Target == idRailBar:
		s.rail.Scroll(rows)
	case (s.page == pageLibrary || s.page == pageBrowse || s.page == pageSearch) && (m.Target == idTracks || trackRow || m.Target == idTrackBar):
		s.tracks.Scroll(rows)
	case (s.page == pageLibrary || s.page == pageBrowse || s.page == pageSearch) && strings.HasPrefix(m.Target, "browse:"):
		s.tracks.Scroll(rows)
	case s.page == pageHome && (m.Target == idHomeScroll || m.Target == idHomeContent || strings.HasPrefix(m.Target, "home-card:")):
		scrollHome(s, rows*3)
	case s.page == pageSettings && (m.Target == idSettingsScroll || m.Target == idSettingsContent || settingTarget):
		s.settingsScroll = min(max(s.settingsScroll+rows*2, 0), s.settingsMax)
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
