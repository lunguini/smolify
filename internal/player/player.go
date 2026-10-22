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
}

// Player is the embedded player. Its methods may be called from any
// goroutine; they block until the player answers (or ctx ends).
type Player struct {
	app    *daemon.App
	srv    *server
	events chan Event
	cancel context.CancelFunc
	done   chan struct{}
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
	vol := opts.Volume
	if vol <= 0 {
		vol = 40
	}
	p := &Player{events: make(chan Event, 256), done: make(chan struct{})}
	p.srv = &server{requests: make(chan daemon.ApiRequest), emit: p.emit}
	app, err := daemon.New(&daemon.Options{
		Logger:     quietLogger{},
		StateStore: fileStore{dir: opts.Dir},
		APIServer:  p.srv,
		Config: &daemon.Config{
			DeviceName:    opts.DeviceName,
			DeviceType:    "computer",
			AudioBackend:  "audio-toolbox",
			Bitrate:       320,
			VolumeSteps:   100,
			InitialVolume: uint32(min(vol, 100)),
			SkipDebounce:  600 * time.Millisecond,
			ImageSize:     "default",
			Metadata:      daemon.MetadataConfig{Enabled: true, MaxTracks: 2000},
			Credentials:   daemon.CredentialsConfig{Type: "device_auth"},
		},
	})
	if err != nil {
		return nil, err
	}
	p.app = app
	ctx, p.cancel = context.WithCancel(ctx)
	go func() {
		defer close(p.done)
		if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			p.emit(Failed{Err: err})
		}
	}()
	return p, nil
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
	p.cancel()
	_ = p.app.Close()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
	}
}

// request sends one request and waits for its reply.
func (p *Player) request(ctx context.Context, t daemon.ApiRequestType, data any) (any, error) {
	req, wait := daemon.NewApiRequest(t, data)
	select {
	case p.srv.requests <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return wait(ctx)
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

func (p *Player) SetRepeat(ctx context.Context, on bool) error {
	_, err := p.request(ctx, daemon.ApiRequestTypeSetRepeatingContext, on)
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
