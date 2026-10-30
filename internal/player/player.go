// Package player embeds go-librespot as Smolify's player: a Spotify Connect
// device that plays through the Mac's audio output, driven in-process —
// requests go to it over a channel and its events come back on Events, with
// no HTTP server. It has no UI types.
package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/devgianlu/go-librespot/daemon"
	"github.com/sirupsen/logrus"
)

// Options configures the player.
type Options struct {
	// Dir holds the player's state: its device ID and, after pairing, the
	// stored credentials (0600).
	Dir string
	// DeviceName is how it shows among Spotify Connect devices.
	DeviceName string
	// Volume is the initial volume, 0–100 (0: 40).
	Volume int
	// Audio is how it plays.
	Audio Audio
}

// Audio is how the player plays. Changing it (Configure) restarts the
// session, so it is not something to change per track.
type Audio struct {
	// Bitrate is the stream quality in kbps: 96, 160, or 320 (others: 320).
	Bitrate int
	// Normalise evens out loudness between tracks.
	Normalise bool
	// Crossfade blends the end of a track into the next (0: none).
	Crossfade time.Duration
	// Autoplay keeps playing similar songs when a list ends.
	Autoplay bool
}

// bitrate is a's bitrate, 320 unless it is one Spotify streams.
func (a Audio) bitrate() int {
	switch a.Bitrate {
	case 96, 160:
		return a.Bitrate
	}
	return 320
}

// Player is the embedded player. Its methods may be called from any
// goroutine; they block until the player answers (or ctx ends). Requests
// are bound to one session, wait for sign-in, and end when that session
// exits or the caller cancels its context.
type Player struct {
	events chan Event
	ctx    context.Context

	mu               sync.Mutex
	ops              sync.Mutex // serializes Configure and SignOut; Close never waits on it
	opts             Options
	session          *playerSession
	closed           bool
	operation        context.CancelFunc
	newApp           func(*daemon.Options) (sessionApp, error)
	clearCredentials func(string) error

	ctMu    sync.Mutex
	ct      string
	ctUntil time.Time
}

type sessionApp interface {
	Run(context.Context) error
}

type playerSession struct {
	srv       *server
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	ready     chan struct{}
	readyOnce sync.Once
	stopOnce  sync.Once
	stopped   chan struct{}
	stopErr   error
}

// stop cancels all requests immediately, then waits a bounded time for Run.
func (s *playerSession) stop() error {
	s.cancel()
	s.stopOnce.Do(func() {
		// App.Run owns App.Close on cancellation. Waiting for Run also avoids
		// calling the daemon's unsynchronized Close concurrently with it.
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			s.stopErr = errors.New("player: session did not stop")
		}
		close(s.stopped)
	})
	<-s.stopped
	return s.stopErr
}

// dir is the player's state dir.
func (p *Player) dir() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opts.Dir
}

// Start starts the player. It signs in with stored credentials, or reports
// Pairing on Events and waits for the user to approve the device. Call
// Close when done.
func Start(ctx context.Context, opts Options) (*Player, error) {
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, err
	}
	// go-librespot's audio output logs through logrus directly; anything it
	// writes would land on the terminal frame.
	logrus.SetOutput(io.Discard)
	p := &Player{events: make(chan Event, 256), ctx: ctx, opts: opts}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.launch(); err != nil {
		return nil, err
	}
	return p, nil
}

// launch starts a session with p.opts. Callers hold p.mu.
func (p *Player) launch() error {
	if p.closed {
		return errors.New("player: closed")
	}
	opts := p.opts
	vol := opts.Volume
	if vol <= 0 {
		vol = 40
	}
	ctx, cancel := context.WithCancel(p.ctx)
	session := &playerSession{ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), stopped: make(chan struct{})}
	session.srv = &server{requests: make(chan daemon.ApiRequest)}
	session.srv.emit = func(e Event) {
		if _, ok := e.(Ready); ok {
			session.readyOnce.Do(func() { close(session.ready) })
		}
		p.mu.Lock()
		current := p.session == session && !p.closed && session.ctx.Err() == nil
		if current {
			p.emit(e)
		}
		p.mu.Unlock()
	}
	newApp := p.newApp
	if newApp == nil {
		newApp = func(o *daemon.Options) (sessionApp, error) { return daemon.New(o) }
	}
	app, err := newApp(&daemon.Options{
		Logger:     quietLogger{},
		StateStore: fileStore{dir: opts.Dir},
		APIServer:  session.srv,
		Config: &daemon.Config{
			DeviceName:            opts.DeviceName,
			DeviceType:            "computer",
			AudioBackend:          "audio-toolbox",
			Bitrate:               opts.Audio.bitrate(),
			NormalisationDisabled: !opts.Audio.Normalise,
			CrossfadeDuration:     int(opts.Audio.Crossfade / time.Millisecond),
			DisableAutoplay:       !opts.Audio.Autoplay,
			VolumeSteps:           100,
			InitialVolume:         uint32(min(vol, 100)),
			SkipDebounce:          600 * time.Millisecond,
			ImageSize:             "default",
			Metadata:              daemon.MetadataConfig{Enabled: true, MaxTracks: 2000},
			Credentials:           daemon.CredentialsConfig{Type: "device_auth"},
		},
	})
	if err != nil {
		cancel()
		return err
	}
	p.session = session
	go func() {
		defer close(session.done)
		defer cancel()
		if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			session.srv.emit(Failed{Err: err})
		}
	}()
	return nil
}

// Events delivers the player's events, in order. It is never closed.
func (p *Player) Events() <-chan Event { return p.events }

// emit queues an event, dropping it rather than blocking the player when
// nobody reads (the UI catches up from Status).
func (p *Player) emit(ev Event) {
	select {
	case p.events <- ev:
	default:
	}
}

// Close stops the player.
func (p *Player) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	if p.operation != nil {
		p.operation()
	}
	session := p.session
	p.mu.Unlock()
	if session != nil {
		_ = session.stop()
	}
}

// Configure restarts the session and restores playback after sign-in.
func (p *Player) Configure(ctx context.Context, a Audio) error {
	p.ops.Lock()
	defer p.ops.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed || p.opts.Audio == a {
		p.mu.Unlock()
		return nil
	}
	op, cancel := context.WithTimeout(ctx, 30*time.Second)
	p.operation = cancel
	previous := p.session
	p.mu.Unlock()
	defer cancel()
	defer func() { p.mu.Lock(); p.operation = nil; p.mu.Unlock() }()

	statusCtx, statusCancel := context.WithTimeout(op, 3*time.Second)
	v, statusErr := previous.request(statusCtx, daemon.ApiRequestTypeStatus, nil)
	statusCancel()
	var st Status
	if statusErr == nil {
		st, statusErr = statusOf(v)
	}
	if err := op.Err(); err != nil {
		return err
	}
	if err := previous.stop(); err != nil {
		return err
	}
	p.mu.Lock()
	if err := op.Err(); err != nil {
		// Cancellation may land while the old session is stopping. Keep the
		// device alive even when there is no following Configure to replace it.
		restartErr := p.launch()
		p.mu.Unlock()
		return errors.Join(err, restartErr)
	}
	previousAudio := p.opts.Audio
	p.opts.Audio = a
	err := p.launch()
	if err != nil {
		p.opts.Audio = previousAudio
	}
	next := p.session
	p.mu.Unlock()
	if err != nil {
		return err
	}
	// Even an idle restart waits for readiness, so subsequent settings
	// changes cannot race the new session's sign-in.
	if err := next.waitReady(op); err != nil {
		return err
	}
	if statusErr != nil {
		return nil
	}
	return next.resume(op, st)
}

// resume restores controls even when playback was stopped. Requests stay
// bound to this session, so a later restart cannot receive stale commands.
func (s *playerSession) resume(ctx context.Context, st Status) error {
	request := func(t daemon.ApiRequestType, data any) error { _, err := s.request(ctx, t, data); return err }
	if err := request(daemon.ApiRequestTypeSetVolume, daemon.ApiSetVolume{Volume: int32(max(st.Volume, 0))}); err != nil {
		return err
	}
	playing := st.Track != nil && !st.Stopped
	if playing {
		list := st.ContextURI
		if list == "" {
			list = st.Track.URI
		}
		if err := request(daemon.ApiRequestTypePlay, daemon.ApiPlay{
			Uri: list, SkipToUri: st.Track.URI, Position: st.Track.Position.Milliseconds(), Paused: st.Paused,
		}); err != nil {
			return err
		}
	}
	for _, r := range []struct {
		t  daemon.ApiRequestType
		on bool
	}{
		{daemon.ApiRequestTypeSetShufflingContext, st.Shuffle},
		{daemon.ApiRequestTypeSetRepeatingContext, st.Repeat.Context},
		{daemon.ApiRequestTypeSetRepeatingTrack, st.Repeat.Track},
	} {
		if err := request(r.t, r.on); err != nil {
			return err
		}
	}
	return nil
}

// SetDeviceName renames the player among Spotify Connect devices, now and
// for later sessions.
func (p *Player) SetDeviceName(ctx context.Context, name string) error {
	p.mu.Lock()
	p.opts.DeviceName = name
	p.mu.Unlock()
	_, err := p.request(ctx, daemon.ApiRequestSetDeviceName, name)
	return err
}

// SignOut forgets the stored sign-in and restarts the session, which then
// reports Pairing as on first launch.
func (p *Player) SignOut(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A pending Configure can be waiting for sign-in; interrupt it before
	// waiting for the lifecycle slot, just as Close does.
	p.mu.Lock()
	if p.operation != nil {
		p.operation()
	}
	p.mu.Unlock()
	p.ops.Lock()
	defer p.ops.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	previous, dir := p.session, p.opts.Dir
	clear := p.clearCredentials
	p.mu.Unlock()
	if err := previous.stop(); err != nil {
		return err
	}
	if clear == nil {
		clear = SignOut
	}
	clearErr := clear(dir)
	p.mu.Lock()
	defer p.mu.Unlock()
	// Whether clearing succeeded or failed, bring a session back. On a
	// failure it can reuse the old credentials instead of leaving silence.
	for {
		select {
		case <-p.events:
			continue
		default:
			goto drained
		}
	}
drained:
	return errors.Join(clearErr, p.launch())
}

// ErrSuperseded reports a command a newer one replaced before it ran: the
// newer one's result is what counts.
var ErrSuperseded = daemon.ErrSuperseded

// request sends one request and waits for its reply.
func (p *Player) request(ctx context.Context, t daemon.ApiRequestType, data any) (any, error) {
	p.mu.Lock()
	session, closed := p.session, p.closed
	p.mu.Unlock()
	if closed || session == nil {
		return nil, errors.New("player: closed")
	}
	return session.request(ctx, t, data)
}

func (s *playerSession) waitReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return errors.New("player: session ended")
	case <-s.ready:
		if s.ctx.Err() != nil {
			return errors.New("player: session ended")
		}
		return nil
	}
}

func (s *playerSession) request(ctx context.Context, t daemon.ApiRequestType, data any) (any, error) {
	if err := s.waitReady(ctx); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	req, wait := daemon.NewApiRequest(t, data)
	select {
	case <-requestCtx.Done():
		return nil, requestCtx.Err()
	case s.srv.requests <- req:
	}
	return wait(requestCtx)
}

// Status is the player's state now.
type Status struct {
	Username            string
	Track               *Track
	Paused, Stopped     bool
	Volume, VolumeSteps int
	Shuffle             bool
	Repeat              Repeat
	ContextURI          string
}

func (p *Player) Status(ctx context.Context) (Status, error) {
	v, err := p.request(ctx, daemon.ApiRequestTypeStatus, nil)
	if err != nil {
		return Status{}, err
	}
	return statusOf(v)
}

func statusOf(v any) (Status, error) {
	s, ok := v.(*daemon.ApiStatus)
	if !ok {
		return Status{}, fmt.Errorf("player: unexpected status %T", v)
	}
	st := Status{
		Username: s.Username, Paused: s.Paused, Stopped: s.Stopped,
		Volume: int(s.Volume), VolumeSteps: int(s.VolumeSteps), Shuffle: s.ShuffleContext,
		Repeat: Repeat{Context: s.RepeatContext, Track: s.RepeatTrack},
	}
	if s.ContextUri != nil {
		st.ContextURI = *s.ContextUri
	}
	if s.Track != nil {
		t := trackOf(*s.Track)
		st.Track = &t
	}
	return st, nil
}

// Playlist is a playlist in the user's library.
type Playlist struct {
	URI, Name, Description, Owner string
	Image                         string // cover URL ("": a mosaic in Spotify's apps)
	Length                        int
	Folder                        []string
}

// Playlists is the user's whole library of playlists, in library order.
func (p *Player) Playlists(ctx context.Context) ([]Playlist, error) {
	var out []Playlist
	for offset := 0; ; {
		v, err := p.request(ctx, daemon.ApiRequestTypeLibraryPlaylists, daemon.ApiRequestDataLibraryPlaylists{Offset: offset, Limit: 100})
		if err != nil {
			return nil, err
		}
		page, ok := v.(*daemon.ApiLibraryPlaylists)
		if !ok {
			return nil, fmt.Errorf("player: unexpected playlists %T", v)
		}
		for _, pl := range page.Items {
			x := Playlist{URI: pl.Uri, Name: pl.Name, Description: pl.Description, Owner: pl.OwnerUsername, Length: int(pl.Length), Folder: pl.Folder}
			if pl.ImageUrl != nil {
				x.Image = *pl.ImageUrl
			}
			out = append(out, x)
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return out, nil
		}
	}
}

// Token is an access token for Spotify's Web API, signed in as the player
// is. The player refreshes it on every ask; callers cache it.
func (p *Player) Token(ctx context.Context) (string, error) {
	v, err := p.request(ctx, daemon.ApiRequestTypeToken, nil)
	if err != nil {
		return "", err
	}
	t, ok := v.(*daemon.ApiToken)
	if !ok || t.Token == "" {
		return "", fmt.Errorf("player: unexpected token %T", v)
	}
	return t.Token, nil
}

// Listing is a context's tracks as far as the player has resolved them:
// Tracks has Length entries once Ready, and an entry not yet resolved has
// only its URI. Ask again while !Complete.
type Listing struct {
	URI    string
	Ready  bool
	Length int
	Cached int
	Tracks []Track
}

// Complete reports that every track is listed with its metadata.
func (l Listing) Complete() bool { return l.Ready && l.Cached >= l.Length }

// ContextTracks lists the tracks of a playlist, album, or Liked Songs. The
// player enumerates in the background, so a first answer is often partial.
func (p *Player) ContextTracks(ctx context.Context, uri string) (Listing, error) {
	v, err := p.request(ctx, daemon.ApiRequestTypeContextTracks, daemon.ApiRequestDataContextTracks{Uri: uri})
	if err != nil {
		return Listing{}, err
	}
	r, ok := v.(*daemon.ApiContextTracks)
	if !ok {
		return Listing{}, fmt.Errorf("player: unexpected tracks %T", v)
	}
	return listingOf(r), nil
}

func listingOf(r *daemon.ApiContextTracks) Listing {
	l := Listing{URI: r.Uri, Ready: r.Ready, Length: r.Length, Cached: r.Cached, Tracks: make([]Track, len(r.Tracks))}
	for i, it := range r.Tracks {
		if it.Track != nil {
			l.Tracks[i] = trackOf(*it.Track)
		}
		l.Tracks[i].URI = it.Uri
	}
	return l
}

// Queue adds the track uri to the queue.
func (p *Player) Queue(ctx context.Context, uri string) error {
	_, err := p.request(ctx, daemon.ApiRequestTypeAddToQueue, uri)
	return err
}

// maxLiked is how many tracks one liked request may name.
const maxLiked = 50

// SetLiked adds the tracks to Liked Songs, or removes them.
func (p *Player) SetLiked(ctx context.Context, uris []string, liked bool) error {
	for chunk := range slices.Chunk(uris, maxLiked) {
		if _, err := p.request(ctx, daemon.ApiRequestTypeSetLiked, daemon.ApiSetLiked{Uris: chunk, Liked: liked}); err != nil {
			return err
		}
	}
	return nil
}

// Liked tells for each of uris whether it is in Liked Songs.
func (p *Player) Liked(ctx context.Context, uris []string) ([]bool, error) {
	out := make([]bool, 0, len(uris))
	for chunk := range slices.Chunk(uris, maxLiked) {
		v, err := p.request(ctx, daemon.ApiRequestTypeGetLiked, chunk)
		if err != nil {
			return nil, err
		}
		r, ok := v.(*daemon.ApiLikedStates)
		if !ok || len(r.Items) != len(chunk) {
			return nil, fmt.Errorf("player: unexpected liked states %T", v)
		}
		for _, it := range r.Items {
			out = append(out, it.Liked)
		}
	}
	return out, nil
}

// LikedSongs is the context URI of the user's Liked Songs.
func LikedSongs(username string) string { return "spotify:user:" + username + ":collection" }

// Play plays the context uri (a playlist, album, or track), starting at the
// track skipTo when set.
func (p *Player) Play(ctx context.Context, uri, skipTo string) error {
	_, err := p.request(ctx, daemon.ApiRequestTypePlay, daemon.ApiPlay{Uri: uri, SkipToUri: skipTo})
	return err
}

func (p *Player) PlayPause(ctx context.Context) error {
	_, err := p.request(ctx, daemon.ApiRequestTypePlayPause, nil)
	return err
}

func (p *Player) Next(ctx context.Context) error {
	_, err := p.request(ctx, daemon.ApiRequestTypeNext, daemon.ApiNext{})
	return err
}

func (p *Player) Prev(ctx context.Context) error {
	_, err := p.request(ctx, daemon.ApiRequestTypePrev, nil)
	return err
}

// Seek moves to position in the track.
func (p *Player) Seek(ctx context.Context, position time.Duration) error {
	_, err := p.request(ctx, daemon.ApiRequestTypeSeek, daemon.ApiSeek{Position: position.Milliseconds()})
	return err
}

// SetVolume sets the volume out of the status's VolumeSteps.
func (p *Player) SetVolume(ctx context.Context, v int) error {
	_, err := p.request(ctx, daemon.ApiRequestTypeSetVolume, daemon.ApiSetVolume{Volume: int32(max(v, 0))})
	return err
}

func (p *Player) SetShuffle(ctx context.Context, on bool) error {
	_, err := p.request(ctx, daemon.ApiRequestTypeSetShufflingContext, on)
	return err
}

// SetRepeat sets both repeat modes: the whole context, and the one track
// (which wins while it is on).
func (p *Player) SetRepeat(ctx context.Context, r Repeat) error {
	if _, err := p.request(ctx, daemon.ApiRequestTypeSetRepeatingContext, r.Context); err != nil {
		return err
	}
	_, err := p.request(ctx, daemon.ApiRequestTypeSetRepeatingTrack, r.Track)
	return err
}

// server is the in-process daemon.ApiServer: requests over a channel, events
// to emit, pairing codes as Pairing events.
type server struct {
	requests chan daemon.ApiRequest
	emit     func(Event)
	repeat   Repeat
}

func (s *server) Emit(ev *daemon.ApiEvent) {
	if e, ok := eventOf(ev, &s.repeat); ok {
		s.emit(e)
	}
}

func (s *server) Receive() <-chan daemon.ApiRequest { return s.requests }

func (s *server) SetAuthCode(auth *daemon.ApiDeviceAuth) {
	if auth == nil {
		s.emit(Pairing{})
		return
	}
	s.emit(Pairing{URL: auth.Url, Code: auth.Code, Expires: auth.ExpiresAt})
}

func (s *server) Close() error { return nil }
