package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Search looks q up across tracks, albums, artists, and playlists.
func (c *Client) Search(ctx context.Context, q string, limit int) (Results, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return Results{}, nil
	}
	limit = min(max(limit, 1), 50)
	body, err := json.Marshal(map[string]any{
		"operationName": "searchDesktop",
		"variables": map[string]any{
			"searchTerm": q, "offset": 0, "limit": limit, "numberOfTopResults": 5,
			"includeAudiobooks": false, "includeArtistHasConcertsField": false,
			"includePreReleases": false, "includeLocalConcertsField": false, "includeAuthors": false,
		},
		"extensions": map[string]any{"persistedQuery": map[string]any{
			"version": 1, "sha256Hash": "eef7cc54888d91bdd6802623477873caa3948ae173a0c34fd86827b267e94c03",
		}},
	})
	if err != nil {
		return Results{}, err
	}
	var out searchResponse
	if err := c.post(ctx, body, &out); err != nil {
		return Results{}, err
	}
	if len(out.Errors) > 0 {
		return Results{}, fmt.Errorf("Spotify search: %s", out.Errors[0].Message)
	}
	if out.Data.Search == nil || out.Data.Search.Tracks == nil {
		return Results{}, errors.New("Spotify search: missing results")
	}
	s := out.Data.Search
	r := Results{}
	for _, item := range s.Tracks.Items {
		t := item.Item.Data
		if t.Kind != "Track" || !strings.HasPrefix(t.URI, "spotify:track:") {
			continue
		}
		r.Tracks = append(r.Tracks, Track{URI: t.URI, Name: t.Name, DurationMS: t.Duration.Milliseconds, Artists: searchArtists(t.Artists), Album: searchAlbum(t.Album)})
	}
	for _, item := range s.Albums.Items {
		if item.Data.Kind == "Album" && item.Data.URI != "" {
			r.Albums = append(r.Albums, searchAlbum(item.Data))
		}
	}
	for _, item := range s.Artists.Items {
		if item.Data.Kind == "Artist" && item.Data.URI != "" {
			r.Artists = append(r.Artists, Artist{URI: item.Data.URI, Name: item.Data.Profile.Name})
		}
	}
	for _, item := range s.Playlists.Items {
		p := item.Data
		if p.Kind != "Playlist" || p.URI == "" {
			continue
		}
		pl := Playlist{URI: p.URI, Name: p.Name, Description: p.Description}
		pl.Owner.DisplayName = p.Owner.Data.Name
		for _, image := range p.Images.Items {
			pl.Images = append(pl.Images, image.Sources...)
		}
		r.Playlists = append(r.Playlists, pl)
	}
	return r, nil
}

type searchResponse struct {
	Data struct {
		Search *searchPage `json:"searchV2"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type searchPage struct {
	Tracks *page[struct {
		Item struct {
			Data searchTrack `json:"data"`
		} `json:"item"`
	}] `json:"tracksV2"`
	Albums page[struct {
		Data desktopAlbum `json:"data"`
	}] `json:"albumsV2"`
	Artists page[struct {
		Data desktopArtist `json:"data"`
	}] `json:"artists"`
	Playlists page[struct {
		Data searchPlaylist `json:"data"`
	}] `json:"playlists"`
}

type desktopArtist struct {
	Kind    string `json:"__typename"`
	URI     string `json:"uri"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

type desktopAlbum struct {
	Kind    string              `json:"__typename"`
	URI     string              `json:"uri"`
	Name    string              `json:"name"`
	Artists page[desktopArtist] `json:"artists"`
	Cover   struct {
		Sources []Image `json:"sources"`
	} `json:"coverArt"`
}

type searchTrack struct {
	Kind     string `json:"__typename"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Duration struct {
		Milliseconds int `json:"totalMilliseconds"`
	} `json:"duration"`
	Artists page[desktopArtist] `json:"artists"`
	Album   desktopAlbum        `json:"albumOfTrack"`
}

type searchPlaylist struct {
	Kind        string `json:"__typename"`
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Images      page[struct {
		Sources []Image `json:"sources"`
	}] `json:"images"`
	Owner struct {
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	} `json:"ownerV2"`
}

func searchArtists(p page[desktopArtist]) []Artist {
	artists := make([]Artist, 0, len(p.Items))
	for _, a := range p.Items {
		if a.URI != "" {
			artists = append(artists, Artist{URI: a.URI, Name: a.Profile.Name})
		}
	}
	return artists
}

func searchAlbum(a desktopAlbum) Album {
	return Album{URI: a.URI, Name: a.Name, Images: a.Cover.Sources, Artists: searchArtists(a.Artists)}
}
