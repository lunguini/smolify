package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// HomeFeed is Spotify's personalized Home response, normalized to playable
// catalog cards. Unsupported cards are omitted instead of inventing URIs.
type HomeFeed struct {
	Greeting string
	Sections []HomeSection
}

type HomeSection struct {
	URI   string
	Title string
	Items []HomeItem
}

// HomeItem is a playlist, album, artist, or track shown on Home.
type HomeItem struct {
	URI, Name, Kind, Image string
	Artists                []Artist
}

// ArtistOverview carries the header and Spotify's popular tracks for an
// artist. The private API may omit either field for unavailable artists.
type ArtistOverview struct {
	Artist    Artist
	Image     string
	TopTracks []Track
}

// AlbumDetails contains album metadata and its first bounded track page.
type AlbumDetails struct {
	Album  Album
	Tracks []Track
	Total  int
}

// Home reads Spotify's own personalized Home shelves. sectionItemsLimit is
// deliberately bounded to keep the response small.
func (c *Client) Home(ctx context.Context) (HomeFeed, error) {
	tz := time.Local.String()
	if tz == "" || tz == "Local" {
		tz = "UTC"
	}
	body, err := discoveryBody("home", "5fb7da7a03ee2776c0670856609cb85a189a3cecc36cc160b91cde79063b9cf1", map[string]any{
		"homeEndUserIntegration":         "INTEGRATION_WEB_PLAYER",
		"timeZone":                       tz,
		"sp_t":                           "",
		"facet":                          "",
		"sectionItemsLimit":              10,
		"includeEpisodeContentRatingsV2": false,
	})
	if err != nil {
		return HomeFeed{}, err
	}
	var raw struct {
		Data struct {
			Home struct {
				Greeting struct {
					Label string `json:"transformedLabel"`
				} `json:"greeting"`
				SectionContainer struct {
					Sections page[struct {
						URI  string `json:"uri"`
						Data struct {
							Kind  string `json:"__typename"`
							Title struct {
								Transformed string `json:"transformedLabel"`
								Base        string `json:"translatedBaseText"`
								Text        string `json:"text"`
							} `json:"title"`
						} `json:"data"`
						SectionItems page[struct {
							Content struct {
								Kind string          `json:"__typename"`
								Data json.RawMessage `json:"data"`
							} `json:"content"`
						}] `json:"sectionItems"`
					}] `json:"sections"`
				} `json:"sectionContainer"`
			} `json:"home"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.post(ctx, body, &raw); err != nil {
		return HomeFeed{}, err
	}
	if len(raw.Errors) > 0 {
		return HomeFeed{}, fmt.Errorf("Spotify Home: %s", raw.Errors[0].Message)
	}
	feed := HomeFeed{Greeting: raw.Data.Home.Greeting.Label}
	for _, s := range raw.Data.Home.SectionContainer.Sections.Items {
		section := HomeSection{URI: s.URI, Title: firstNonempty(s.Data.Title.Transformed, s.Data.Title.Base, s.Data.Title.Text)}
		for _, item := range s.SectionItems.Items {
			card, ok := parseHomeItem(item.Content.Kind, item.Content.Data)
			if ok {
				section.Items = append(section.Items, card)
			}
		}
		if len(section.Items) > 0 {
			feed.Sections = append(feed.Sections, section)
		}
	}
	if len(feed.Sections) == 0 {
		return feed, errors.New("Spotify Home: no supported shelf cards")
	}
	return feed, nil
}

// Artist loads the Spotify artist overview and its popular tracks from the
// same query Spotify uses for artist pages.
func (c *Client) Artist(ctx context.Context, uri string) (ArtistOverview, error) {
	if !strings.HasPrefix(uri, "spotify:artist:") {
		return ArtistOverview{}, errors.New("Spotify artist: invalid URI")
	}
	body, err := discoveryBody("queryArtistOverview", "9f8134ef565e78621f1e1793555bd6633c5ac144ae0f89604ed3ae3f80b3c8e6", map[string]any{"uri": uri, "locale": ""})
	if err != nil {
		return ArtistOverview{}, err
	}
	var raw struct {
		Data struct {
			Artist struct {
				Profile struct {
					Name string `json:"name"`
				} `json:"profile"`
				Visuals struct {
					Avatar struct {
						Sources []Image `json:"sources"`
					} `json:"avatarImage"`
				} `json:"visuals"`
				Discography struct {
					TopTracks page[struct {
						Track searchTrack `json:"track"`
					}] `json:"topTracks"`
				} `json:"discography"`
			} `json:"artistUnion"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.post(ctx, body, &raw); err != nil {
		return ArtistOverview{}, err
	}
	if len(raw.Errors) > 0 {
		return ArtistOverview{}, fmt.Errorf("Spotify artist: %s", raw.Errors[0].Message)
	}
	if raw.Data.Artist.Profile.Name == "" {
		return ArtistOverview{}, errors.New("Spotify artist: missing details")
	}
	out := ArtistOverview{Artist: Artist{URI: uri, Name: raw.Data.Artist.Profile.Name}, Image: Cover(raw.Data.Artist.Visuals.Avatar.Sources, 256)}
	for _, item := range raw.Data.Artist.Discography.TopTracks.Items {
		if strings.HasPrefix(item.Track.URI, "spotify:track:") {
			out.TopTracks = append(out.TopTracks, convertDiscoveryTrack(item.Track))
		}
	}
	return out, nil
}

// Album follows every bounded album page, preserving unavailable item slots.
func (c *Client) Album(ctx context.Context, uri string) (AlbumDetails, error) {
	first, err := c.albumPage(ctx, uri, 0)
	if err != nil {
		return AlbumDetails{}, err
	}
	for offset := len(first.Tracks); offset < first.Total; {
		if err := ctx.Err(); err != nil {
			return AlbumDetails{}, err
		}
		next, err := c.albumPage(ctx, uri, offset)
		if err != nil {
			return AlbumDetails{}, err
		}
		if next.Total != first.Total || len(next.Tracks) == 0 {
			return AlbumDetails{}, errors.New("Spotify album changed while loading; try again")
		}
		first.Tracks = append(first.Tracks, next.Tracks...)
		offset += len(next.Tracks)
	}
	return first, nil
}

func (c *Client) albumPage(ctx context.Context, uri string, offset int) (AlbumDetails, error) {
	if !strings.HasPrefix(uri, "spotify:album:") {
		return AlbumDetails{}, errors.New("Spotify album: invalid URI")
	}
	body, err := discoveryBody("getAlbum", "0b6ce3511e1b02e4a40fa95440f992acbe85080eb308af9194322d708fc1914f", map[string]any{"uri": uri, "locale": "", "offset": offset, "limit": 50})
	if err != nil {
		return AlbumDetails{}, err
	}
	var raw struct {
		Data struct {
			Album struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				Cover struct {
					Sources []Image `json:"sources"`
				} `json:"coverArt"`
				Artists page[desktopArtist] `json:"artists"`
				Tracks  struct {
					Total *int `json:"totalCount"`
					Items []struct {
						Track searchTrack `json:"track"`
					} `json:"items"`
				} `json:"tracksV2"`
			} `json:"albumUnion"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.post(ctx, body, &raw); err != nil {
		return AlbumDetails{}, err
	}
	if len(raw.Errors) > 0 {
		return AlbumDetails{}, fmt.Errorf("Spotify album: %s", raw.Errors[0].Message)
	}
	a := raw.Data.Album
	if a.Name == "" {
		return AlbumDetails{}, errors.New("Spotify album: missing details")
	}
	if a.Tracks.Total == nil || *a.Tracks.Total < 0 || len(a.Tracks.Items) > 50 || offset+len(a.Tracks.Items) > *a.Tracks.Total || len(a.Tracks.Items) == 0 && offset < *a.Tracks.Total {
		return AlbumDetails{}, errors.New("Spotify album: inconsistent track page")
	}
	out := AlbumDetails{Album: Album{URI: uri, Name: a.Name, Images: a.Cover.Sources, Artists: searchArtists(a.Artists)}, Total: *a.Tracks.Total}
	for _, item := range a.Tracks.Items {
		track := convertDiscoveryTrack(item.Track)
		track.Album = out.Album
		out.Tracks = append(out.Tracks, track)
	}
	return out, nil
}

func discoveryBody(operation, hash string, variables map[string]any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"operationName": operation,
		"variables":     variables,
		"extensions":    map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": hash}},
	})
}

func parseHomeItem(wrapper string, data json.RawMessage) (HomeItem, bool) {
	var card struct {
		URI    string `json:"uri"`
		Name   string `json:"name"`
		Images struct {
			Items []struct {
				Sources []Image `json:"sources"`
			} `json:"items"`
		} `json:"images"`
		Cover struct {
			Sources []Image `json:"sources"`
		} `json:"coverArt"`
		Visuals struct {
			Avatar struct {
				Sources []Image `json:"sources"`
			} `json:"avatarImage"`
		} `json:"visuals"`
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
		Artists page[desktopArtist] `json:"artists"`
	}
	if json.Unmarshal(data, &card) != nil || card.URI == "" {
		return HomeItem{}, false
	}
	item := HomeItem{URI: card.URI, Name: card.Name}
	switch wrapper {
	case "PlaylistResponseWrapper":
		item.Kind = "playlist"
		if len(card.Images.Items) > 0 {
			item.Image = Cover(card.Images.Items[0].Sources, 256)
		}
	case "AlbumResponseWrapper":
		item.Kind, item.Image, item.Artists = "album", Cover(card.Cover.Sources, 256), searchArtists(card.Artists)
	case "ArtistResponseWrapper":
		item.Kind, item.Name, item.Image = "artist", card.Profile.Name, Cover(card.Visuals.Avatar.Sources, 256)
	default:
		return HomeItem{}, false
	}
	return item, item.Name != ""
}

func convertDiscoveryTrack(t searchTrack) Track {
	return Track{URI: t.URI, Name: t.Name, DurationMS: t.Duration.Milliseconds, Artists: searchArtists(t.Artists), Album: searchAlbum(t.Album)}
}

func firstNonempty(values ...string) string {
	for _, s := range values {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
