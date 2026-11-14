package webspotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lunguini/smolify/internal/model"
)

type playbackResponse struct {
	Device struct {
		ID            string `json:"id"`
		VolumePercent int    `json:"volume_percent"`
	} `json:"device"`
	RepeatState string `json:"repeat_state"`
	Shuffle     bool   `json:"shuffle_state"`
	Context     *struct {
		URI string `json:"uri"`
	} `json:"context"`
	ProgressMS int       `json:"progress_ms"`
	IsPlaying  bool      `json:"is_playing"`
	Item       *rawTrack `json:"item"`
}

// PlaybackState returns the current documented Web API player state.
func (c *API) PlaybackState(ctx context.Context) (model.Status, string, error) {
	body, err := c.request(ctx, "GET", "/me/player", nil, nil)
	if err != nil {
		return model.Status{}, "", err
	}
	status := model.Status{VolumeSteps: 100, Stopped: true}
	if len(body) == 0 {
		return status, "", nil // Spotify returns 204 when there is no active device.
	}
	var raw playbackResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return model.Status{}, "", err
	}
	status.Paused, status.Stopped = !raw.IsPlaying, raw.Item == nil
	status.Volume = raw.Device.VolumePercent
	status.Shuffle = raw.Shuffle
	switch raw.RepeatState {
	case "context":
		status.Repeat.Context = true
	case "track":
		status.Repeat.Track = true
	}
	if raw.Context != nil {
		status.ContextURI = raw.Context.URI
	}
	if raw.Item != nil {
		track := modelFromRaw(*raw.Item)
		track.Position = time.Duration(raw.ProgressMS) * time.Millisecond
		track.ContextURI = status.ContextURI
		status.Track = &track
	}
	return status, raw.Device.ID, nil
}

func (c *API) TransferPlayback(ctx context.Context, deviceID string, play bool) error {
	body := map[string]any{"device_ids": []string{deviceID}, "play": play}
	_, err := c.request(ctx, "PUT", "/me/player", nil, body)
	return err
}

// StartPlayback starts a playlist, album, artist, or track on deviceID.
// likedTracks supplies documented track URIs for a saved-library context.
func (c *API) StartPlayback(ctx context.Context, deviceID, uri, skipTo string, likedTracks []string) error {
	query := deviceQuery(deviceID)
	body := map[string]any{}
	switch {
	case isLikedURI(uri):
		uris, err := c.likedQueue(ctx, likedTracks, skipTo)
		if err != nil {
			return err
		}
		if len(uris) == 0 {
			return errorsNew("Liked Songs has no playable tracks")
		}
		body["uris"] = uris
		if skipTo != "" {
			body["offset"] = map[string]string{"uri": skipTo}
		}
	case hasPrefix(uri, "spotify:track:"):
		body["uris"] = []string{uri}
	case hasPrefix(uri, "spotify:playlist:") || hasPrefix(uri, "spotify:album:") || hasPrefix(uri, "spotify:artist:"):
		body["context_uri"] = uri
		if skipTo != "" {
			body["offset"] = map[string]string{"uri": skipTo}
		}
	default:
		return errorsNew("unsupported playback context")
	}
	_, err := c.request(ctx, "PUT", "/me/player/play", query, body)
	return err
}

func (c *API) Pause(ctx context.Context, deviceID string) error {
	_, err := c.request(ctx, "PUT", "/me/player/pause", deviceQuery(deviceID), nil)
	return err
}

func (c *API) Resume(ctx context.Context, deviceID string) error {
	_, err := c.request(ctx, "PUT", "/me/player/play", deviceQuery(deviceID), map[string]any{})
	return err
}

func (c *API) Next(ctx context.Context, deviceID string) error {
	_, err := c.request(ctx, "POST", "/me/player/next", deviceQuery(deviceID), nil)
	return err
}

func (c *API) Previous(ctx context.Context, deviceID string) error {
	_, err := c.request(ctx, "POST", "/me/player/previous", deviceQuery(deviceID), nil)
	return err
}

func (c *API) Seek(ctx context.Context, deviceID string, position time.Duration) error {
	query := deviceQuery(deviceID)
	query.Set("position_ms", strconv.FormatInt(max(position.Milliseconds(), 0), 10))
	_, err := c.request(ctx, "PUT", "/me/player/seek", query, nil)
	return err
}

func (c *API) SetVolume(ctx context.Context, deviceID string, percent int) error {
	query := deviceQuery(deviceID)
	query.Set("volume_percent", strconv.Itoa(min(max(percent, 0), 100)))
	_, err := c.request(ctx, "PUT", "/me/player/volume", query, nil)
	return err
}

func (c *API) SetShuffle(ctx context.Context, deviceID string, enabled bool) error {
	query := deviceQuery(deviceID)
	query.Set("state", strconv.FormatBool(enabled))
	_, err := c.request(ctx, "PUT", "/me/player/shuffle", query, nil)
	return err
}

func (c *API) SetRepeat(ctx context.Context, deviceID string, repeat model.Repeat) error {
	state := "off"
	if repeat.Track {
		state = "track"
	} else if repeat.Context {
		state = "context"
	}
	query := deviceQuery(deviceID)
	query.Set("state", state)
	_, err := c.request(ctx, "PUT", "/me/player/repeat", query, nil)
	return err
}

func (c *API) AddToQueue(ctx context.Context, deviceID, uri string) error {
	query := deviceQuery(deviceID)
	query.Set("uri", uri)
	_, err := c.request(ctx, "POST", "/me/player/queue", query, nil)
	return err
}

func deviceQuery(deviceID string) url.Values {
	query := url.Values{}
	if deviceID != "" {
		query.Set("device_id", deviceID)
	}
	return query
}

// Helpers keep syscall-free API files small and are easy for TinyGo to lower.
func errorsNew(message string) error      { return errors.New("spotify Web API: " + message) }
func hasPrefix(value, prefix string) bool { return strings.HasPrefix(value, prefix) }

// maxPlayURIs bounds the track list sent to start a saved-library queue.
const maxPlayURIs = 100

// likedQueue returns up to maxPlayURIs saved tracks around skipTo, so
// Next and Previous have a queue to move through. Tracks the caller has not
// loaded are fetched until skipTo is found.
func (c *API) likedQueue(ctx context.Context, known []string, skipTo string) ([]string, error) {
	uris := append([]string(nil), known...)
	if !containsString(uris, skipTo) || len(uris) == 0 {
		uris = uris[:0]
		for offset := 0; ; offset += pageSize {
			page, err := c.LikedTracks(ctx, offset, pageSize)
			if err != nil {
				return nil, err
			}
			for _, item := range page.Items {
				if hasPrefix(item.Track.URI, "spotify:track:") {
					uris = append(uris, item.Track.URI)
				}
			}
			found := skipTo == "" || containsString(uris, skipTo)
			if len(page.Items) == 0 || offset+len(page.Items) >= page.Total ||
				(found && len(uris)-indexString(uris, skipTo) >= maxPlayURIs) {
				break
			}
		}
	}
	// Start the window at skipTo, but pull it back near the end of the list
	// so the earlier tracks stay reachable with Previous.
	if i := indexString(uris, skipTo); i > 0 {
		uris = uris[max(0, min(i, len(uris)-maxPlayURIs)):]
	}
	if len(uris) > maxPlayURIs {
		uris = uris[:maxPlayURIs]
	}
	return uris, nil
}

func containsString(list []string, s string) bool { return indexString(list, s) >= 0 }

func indexString(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}
