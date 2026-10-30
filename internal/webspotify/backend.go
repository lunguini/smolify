package webspotify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/lunguini/smolify/internal/catalog"
	"github.com/lunguini/smolify/internal/model"
)

// SDK is the browser-only Web Playback SDK bridge.
type SDK interface {
	Events() <-chan SDKEvent
	Connect(context.Context) error
	Disconnect() error
	ActivateElement() error
	SetName(context.Context, string) error
}

// Client combines the official Web API and Web Playback SDK behind the
// interfaces consumed by internal/app.
type Client struct {
	API  *API
	Auth *Auth
	SDK  SDK

	mu        sync.Mutex
	events    chan model.Event
	deviceID  string // only the SDK device receives transport commands
	username  string
	status    model.Status
	likedURIs []string
	started   bool
}

func NewClient(api *API, auth *Auth, sdk SDK) *Client {
	return &Client{
		API: api, Auth: auth, SDK: sdk, events: make(chan model.Event, 128),
		status: model.Status{Stopped: true, Volume: 40, VolumeSteps: 100, Paused: true},
	}
}

// Start starts the SDK event loop and connects the browser as a Connect
// device. It emits Ready so the app can load the account library immediately.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = true
	c.mu.Unlock()
	c.emit(model.Ready{})
	if c.SDK == nil {
		return errors.New("spotify Web Playback SDK is unavailable")
	}
	go c.watchSDK(ctx)
	if err := c.SDK.Connect(ctx); err != nil {
		c.emit(MapSDKError(SDKInitError, err.Error()))
		return err
	}
	go c.pollPlayback(ctx)
	return nil
}

func (c *Client) Events() <-chan model.Event { return c.events }

func (c *Client) emit(event model.Event) {
	select {
	case c.events <- event:
	default:
	}
}

func (c *Client) watchSDK(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.SDK.Events():
			if !ok {
				return
			}
			c.handleSDK(ctx, event)
		}
	}
}

func (c *Client) handleSDK(ctx context.Context, event SDKEvent) {
	switch event.Type {
	case SDKReady:
		c.mu.Lock()
		c.deviceID = event.DeviceID
		c.mu.Unlock()
		if event.DeviceID != "" {
			go func() {
				if err := c.API.TransferPlayback(ctx, event.DeviceID, true); err != nil {
					c.emit(MapSDKError(SDKPlaybackError, err.Error()))
					return
				}
				c.refreshPlayback(ctx)
			}()
		}
	case SDKNotReady:
		c.mu.Lock()
		if c.deviceID == event.DeviceID {
			c.deviceID = ""
		}
		c.mu.Unlock()
		c.emit(MapSDKError(SDKPlaybackError, "The browser playback device is not ready"))
	case SDKStateChanged:
		c.applySDKState(event.State)
	case SDKInitError, SDKAuthError, SDKAccountError, SDKPlaybackError, SDKAutoplayFailed:
		c.emit(MapSDKError(event.Type, event.Message))
	}
}

func (c *Client) applySDKState(state SDKState) {
	if len(state.Track.Artists) > 0 && len(state.Track.ArtistURIs) == 0 {
		state.Track.ArtistURIs = make([]string, len(state.Track.Artists))
	}
	if state.Track.URI == "" {
		state.Empty = true
	}
	for _, event := range MapSDKState(state) {
		c.emit(event)
	}
	c.mu.Lock()
	if state.Empty {
		c.status.Track, c.status.Stopped = nil, true
	} else {
		track := state.Track
		track.ContextURI, track.Position = state.ContextURI, state.Position
		c.status.Track = &track
		c.status.ContextURI = state.ContextURI
		c.status.Paused, c.status.Stopped = state.Paused, false
	}
	c.mu.Unlock()
}

func (c *Client) pollPlayback(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshPlayback(ctx)
		}
	}
}

func (c *Client) refreshPlayback(ctx context.Context) {
	status, _, err := c.API.PlaybackState(ctx)
	if err != nil {
		var apiErr HTTPError
		if !errors.As(err, &apiErr) || apiErr.Status != 204 {
			c.emit(model.Failed{Err: err})
		}
		return
	}
	c.mu.Lock()
	status.Username = c.username
	status.VolumeSteps = 100
	c.status = status
	c.mu.Unlock()
	if status.Track == nil || status.Stopped {
		c.emit(model.Stopped{})
		return
	}
	c.emit(*status.Track)
	c.emit(model.Seek{Position: status.Track.Position, Duration: status.Track.Duration})
	if status.Paused {
		c.emit(model.Paused{URI: status.Track.URI, ContextURI: status.ContextURI})
	} else {
		c.emit(model.Playing{URI: status.Track.URI, ContextURI: status.ContextURI})
	}
	c.emit(model.Volume{Value: status.Volume, Max: 100})
	c.emit(model.Shuffle{On: status.Shuffle})
	c.emit(status.Repeat)
}

func (c *Client) Status(ctx context.Context) (model.Status, error) {
	c.mu.Lock()
	username := c.username
	c.mu.Unlock()
	if username == "" {
		id, _, err := c.API.Profile(ctx)
		if err != nil {
			return model.Status{}, err
		}
		c.mu.Lock()
		c.username = id
		c.status.Username = id
		username = id
		c.mu.Unlock()
	}
	status, _, err := c.API.PlaybackState(ctx)
	if err != nil {
		return model.Status{}, err
	}
	status.Username, status.VolumeSteps = username, 100
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
	return status, nil
}

func (c *Client) Playlists(ctx context.Context) ([]model.Playlist, error) {
	return c.API.Playlists(ctx)
}

func (c *Client) Play(ctx context.Context, uri, skipTo string) error {
	c.mu.Lock()
	deviceID := c.deviceID
	liked := append([]string(nil), c.likedURIs...)
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	if c.SDK != nil {
		_ = c.SDK.ActivateElement()
	}
	c.emit(model.WillPlay{URI: nonempty(skipTo, uri), ContextURI: uri})
	return c.API.StartPlayback(ctx, deviceID, uri, skipTo, liked)
}

func (c *Client) PlayPause(ctx context.Context) error {
	c.mu.Lock()
	paused, deviceID, hasTrack := c.status.Paused, c.deviceID, c.status.Track != nil
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	if !hasTrack {
		return errors.New("there is no confirmed track to pause or resume")
	}
	if paused {
		return c.API.Resume(ctx, deviceID)
	}
	return c.API.Pause(ctx, deviceID)
}

func (c *Client) Next(ctx context.Context) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.Next(ctx, deviceID)
}

func (c *Client) Prev(ctx context.Context) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.Previous(ctx, deviceID)
}

func (c *Client) Seek(ctx context.Context, position time.Duration) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.Seek(ctx, deviceID, position)
}

func (c *Client) SetVolume(ctx context.Context, volume int) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.SetVolume(ctx, deviceID, volume)
}

func (c *Client) SetShuffle(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.SetShuffle(ctx, deviceID, enabled)
}

func (c *Client) SetRepeat(ctx context.Context, repeat model.Repeat) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.SetRepeat(ctx, deviceID, repeat)
}

func (c *Client) ContextTracks(ctx context.Context, uri string) (model.Listing, error) {
	listing, err := c.API.ContextTracks(ctx, uri)
	if err == nil && isLikedURI(uri) {
		uris := make([]string, 0, len(listing.Tracks))
		for _, track := range listing.Tracks {
			if strings.HasPrefix(track.URI, "spotify:track:") {
				uris = append(uris, track.URI)
			}
		}
		c.mu.Lock()
		c.likedURIs = uris
		c.mu.Unlock()
	}
	return listing, err
}

func (c *Client) Queue(ctx context.Context, uri string) error {
	c.mu.Lock()
	deviceID := c.deviceID
	c.mu.Unlock()
	if deviceID == "" {
		return errors.New("browser playback device is not ready yet")
	}
	return c.API.AddToQueue(ctx, deviceID, uri)
}

func (c *Client) SetLiked(ctx context.Context, uris []string, liked bool) error {
	return c.API.SetLiked(ctx, uris, liked)
}

func (c *Client) Liked(ctx context.Context, uris []string) ([]bool, error) {
	return c.API.Liked(ctx, uris)
}

func (c *Client) Configure(context.Context, model.Audio) error {
	return errors.New("browser audio quality is managed by the Spotify player")
}

// Song Radio depends on an endpoint Spotify currently marks deprecated; keep
// the action disabled instead of using a private endpoint.
func (c *Client) SupportsRadio() bool   { return false }
func (c *Client) SupportsSignOut() bool { return true }

func (c *Client) SetDeviceName(ctx context.Context, name string) error {
	if c.SDK == nil {
		return errors.New("browser playback device is unavailable")
	}
	return c.SDK.SetName(ctx, name)
}

func (c *Client) SignOut(context.Context) error {
	if c.SDK != nil {
		_ = c.SDK.Disconnect()
	}
	if c.Auth == nil || c.Auth.Store == nil {
		return errors.New("spotify auth session store is unavailable")
	}
	if err := c.Auth.Store.ClearToken(); err != nil {
		return err
	}
	c.mu.Lock()
	c.username, c.deviceID, c.likedURIs = "", "", nil
	c.status = model.Status{Stopped: true, Paused: true, Volume: 40, VolumeSteps: 100}
	c.mu.Unlock()
	c.emit(model.Stopped{})
	return nil
}

func (c *Client) RecentlyPlayed(ctx context.Context, _ string, limit int) ([]model.Played, error) {
	return c.API.RecentlyPlayed(ctx, limit)
}

// Search and catalog methods satisfy the app.Web optional interfaces.
func (c *Client) Search(ctx context.Context, query string, limit int) (catalog.Results, error) {
	return c.API.Search(ctx, query, limit)
}

func (c *Client) LikedTracks(ctx context.Context, offset, limit int) (catalog.SavedTracksPage, error) {
	return c.API.LikedTracks(ctx, offset, limit)
}

func (c *Client) Home(ctx context.Context) (catalog.HomeFeed, error) {
	return c.API.Home(ctx)
}

func (c *Client) Artist(ctx context.Context, uri string) (catalog.ArtistOverview, error) {
	return c.API.Artist(ctx, uri)
}

func (c *Client) Album(ctx context.Context, uri string) (catalog.AlbumDetails, error) {
	return c.API.Album(ctx, uri)
}
