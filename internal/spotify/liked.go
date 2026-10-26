package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// fetchLibraryTracksHash is the verified Pathfinder query hash from
// https://github.com/FrancescoGrazioso/Meld/blob/main/docs/spotify-gql-hashes.json
// (registry last updated 2026-10-22). It is private Spotify API surface and
// may rotate independently of this client.
const fetchLibraryTracksHash = "087278b20b743578a6262c2b0b4bcd20d879c503cc359a2285baf083ef944240"

// LikedTracks returns one bounded page of the user's Liked Songs.
func (c *Client) LikedTracks(ctx context.Context, offset, limit int) (SavedTracksPage, error) {
	if offset < 0 {
		return SavedTracksPage{}, errors.New("spotify: negative liked-tracks offset")
	}
	limit = min(max(limit, 1), 50)
	body, err := json.Marshal(map[string]any{
		"operationName": "fetchLibraryTracks",
		"variables":     map[string]int{"offset": offset, "limit": limit},
		"extensions":    map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": fetchLibraryTracksHash}},
	})
	if err != nil {
		return SavedTracksPage{}, err
	}
	var out likedTracksResponse
	if err := c.post(ctx, body, &out); err != nil {
		return SavedTracksPage{}, err
	}
	if len(out.Errors) > 0 {
		return SavedTracksPage{}, fmt.Errorf("Spotify liked tracks: %s", out.Errors[0].Message)
	}
	if out.Data.Me == nil || out.Data.Me.Library == nil || out.Data.Me.Library.Tracks == nil {
		return SavedTracksPage{}, errors.New("Spotify liked tracks: missing results")
	}
	data := out.Data.Me.Library.Tracks
	if data.Total == nil {
		return SavedTracksPage{}, errors.New("Spotify liked tracks: missing total count")
	}
	if *data.Total < 0 {
		return SavedTracksPage{}, fmt.Errorf("Spotify liked tracks: invalid total count %d", *data.Total)
	}
	if len(data.Items) > limit {
		return SavedTracksPage{}, fmt.Errorf("Spotify liked tracks: returned %d items for limit %d", len(data.Items), limit)
	}
	if offset+len(data.Items) > *data.Total {
		return SavedTracksPage{}, fmt.Errorf("Spotify liked tracks: page ends at %d past total %d", offset+len(data.Items), *data.Total)
	}
	if len(data.Items) == 0 && *data.Total > offset {
		return SavedTracksPage{}, errors.New("Spotify liked tracks: empty page before reported total")
	}
	page := SavedTracksPage{Total: *data.Total, Offset: offset, Limit: limit, Items: make([]SavedTrack, 0, len(data.Items))}
	for _, item := range data.Items {
		track := Track{}
		if item.Track != nil {
			track = likedTrack(item.Track)
		}
		page.Items = append(page.Items, SavedTrack{Track: track, AddedAt: parseAddedAt(item.AddedAt)})
	}
	return page, nil
}

type likedTracksResponse struct {
	Data struct {
		Me *struct {
			Library *struct {
				Tracks *struct {
					Total *int `json:"totalCount"`
					Items []struct {
						AddedAt json.RawMessage `json:"addedAt"`
						Track   *struct {
							URI  string      `json:"_uri"`
							Data searchTrack `json:"data"`
						} `json:"track"`
					} `json:"items"`
				} `json:"tracks"`
			} `json:"library"`
		} `json:"me"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func likedTrack(w *struct {
	URI  string      `json:"_uri"`
	Data searchTrack `json:"data"`
}) Track {
	d := w.Data
	uri := w.URI
	if uri == "" {
		uri = d.URI
	}
	return Track{URI: uri, Name: d.Name, DurationMS: d.Duration.Milliseconds, Artists: searchArtists(d.Artists), Album: searchAlbum(d.Album)}
}

func parseAddedAt(value json.RawMessage) time.Time {
	if len(value) == 0 || string(value) == "null" {
		return time.Time{}
	}
	var timestamp string
	if err := json.Unmarshal(value, &timestamp); err != nil {
		var nested struct {
			ISOString string `json:"isoString"`
		}
		if json.Unmarshal(value, &nested) != nil {
			return time.Time{}
		}
		timestamp = nested.ISOString
	}
	if timestamp == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}
