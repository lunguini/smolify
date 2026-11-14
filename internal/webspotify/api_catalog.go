package webspotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lunguini/smolify/internal/catalog"
	"github.com/lunguini/smolify/internal/model"
)

const pageSize = 50

type rawTrack struct {
	ID         string           `json:"id"`
	URI        string           `json:"uri"`
	Type       string           `json:"type"`
	Name       string           `json:"name"`
	DurationMS int              `json:"duration_ms"`
	TrackNo    int              `json:"track_number"`
	DiscNo     int              `json:"disc_number"`
	Artists    []catalog.Artist `json:"artists"`
	Album      rawAlbum         `json:"album"`
}

type rawAlbum struct {
	ID      string           `json:"id"`
	URI     string           `json:"uri"`
	Name    string           `json:"name"`
	Images  []catalog.Image  `json:"images"`
	Artists []catalog.Artist `json:"artists"`
	Total   int              `json:"total_tracks"`
	Tracks  rawTrackPage     `json:"tracks"`
}

type rawTrackPage struct {
	Items  []rawTrack `json:"items"`
	Total  int        `json:"total"`
	Offset int        `json:"offset"`
}

type searchPage struct {
	Items []json.RawMessage `json:"items"`
	Total int               `json:"total"`
}

type searchResponse struct {
	Tracks    searchPage `json:"tracks"`
	Albums    searchPage `json:"albums"`
	Artists   searchPage `json:"artists"`
	Playlists searchPage `json:"playlists"`
}

type rawPlaylist struct {
	ID          string          `json:"id"`
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Images      []catalog.Image `json:"images"`
	Owner       struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	Items struct {
		Total int `json:"total"`
	} `json:"items"`
	Tracks struct {
		Total int `json:"total"`
	} `json:"tracks"`
}

type rawPlaylistPage struct {
	Items  []rawPlaylist `json:"items"`
	Total  int           `json:"total"`
	Offset int           `json:"offset"`
}

// Search searches tracks, albums, artists, and playlists. The Web API's
// current Development Mode limit is ten results per type per request, so
// larger app requests are paged with offset.
func (c *API) Search(ctx context.Context, query string, limit int) (catalog.Results, error) {
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return catalog.Results{}, nil
	}
	limit = min(limit, 30)
	var result catalog.Results
	for offset := 0; offset < limit; offset += 10 {
		count := min(10, limit-offset)
		body, err := c.request(ctx, "GET", "/search", url.Values{
			"q": {query}, "type": {"track,album,artist,playlist"},
			"limit": {strconv.Itoa(count)}, "offset": {strconv.Itoa(offset)},
		}, nil)
		if err != nil {
			return catalog.Results{}, err
		}
		var raw searchResponse
		if err := json.Unmarshal(body, &raw); err != nil {
			return catalog.Results{}, fmt.Errorf("spotify Web API: decode search: %w", err)
		}
		for _, item := range raw.Tracks.Items {
			var track rawTrack
			if json.Unmarshal(item, &track) == nil && track.URI != "" && (track.Type == "" || track.Type == "track") {
				result.Tracks = append(result.Tracks, catalogTrack(track))
			}
		}
		for _, item := range raw.Albums.Items {
			var album rawAlbum
			if json.Unmarshal(item, &album) == nil && album.URI != "" {
				result.Albums = append(result.Albums, catalogAlbum(album))
			}
		}
		for _, item := range raw.Artists.Items {
			var artist catalog.Artist
			if json.Unmarshal(item, &artist) == nil && artist.URI != "" {
				result.Artists = append(result.Artists, artist)
			}
		}
		for _, item := range raw.Playlists.Items {
			var playlist rawPlaylist
			if json.Unmarshal(item, &playlist) == nil && playlist.URI != "" {
				result.Playlists = append(result.Playlists, catalogPlaylist(playlist))
			}
		}
		if len(raw.Tracks.Items)+len(raw.Albums.Items)+len(raw.Artists.Items)+len(raw.Playlists.Items) == 0 {
			break
		}
	}
	return result, nil
}

// Profile is the current user's documented profile data.
func (c *API) Profile(ctx context.Context) (id, displayName string, err error) {
	body, err := c.request(ctx, "GET", "/me", nil, nil)
	if err != nil {
		return "", "", err
	}
	var raw struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", "", err
	}
	if raw.ID == "" {
		return "", "", errors.New("spotify Web API: profile has no id")
	}
	if raw.DisplayName == "" {
		raw.DisplayName = raw.ID
	}
	return raw.ID, raw.DisplayName, nil
}

// Playlists pages through the signed-in user's playlist library.
func (c *API) Playlists(ctx context.Context) ([]model.Playlist, error) {
	var out []model.Playlist
	for offset := 0; ; offset += pageSize {
		body, err := c.request(ctx, "GET", "/me/playlists", url.Values{
			"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)},
		}, nil)
		if err != nil {
			return nil, err
		}
		var page rawPlaylistPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("spotify Web API: decode playlists: %w", err)
		}
		for _, raw := range page.Items {
			pl := catalogPlaylist(raw)
			length := raw.Items.Total
			if length == 0 {
				length = raw.Tracks.Total // accepted for older response variants
			}
			out = append(out, model.Playlist{
				URI: pl.URI, Name: pl.Name, Description: pl.Description,
				Owner: nonempty(pl.Owner.DisplayName, raw.Owner.ID),
				Image: catalog.Cover(pl.Images, 320), Length: length,
			})
		}
		if len(page.Items) == 0 || offset+len(page.Items) >= page.Total {
			return out, nil
		}
	}
}

// LikedTracks returns one documented page of saved tracks.
func (c *API) LikedTracks(ctx context.Context, offset, limit int) (catalog.SavedTracksPage, error) {
	limit = min(max(limit, 1), pageSize)
	body, err := c.request(ctx, "GET", "/me/tracks", url.Values{
		"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(max(offset, 0))},
	}, nil)
	if err != nil {
		return catalog.SavedTracksPage{}, err
	}
	var raw struct {
		Total  int `json:"total"`
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
		Items  []struct {
			AddedAt string    `json:"added_at"`
			Track   *rawTrack `json:"track"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return catalog.SavedTracksPage{}, fmt.Errorf("spotify Web API: decode saved tracks: %w", err)
	}
	page := catalog.SavedTracksPage{Total: raw.Total, Offset: raw.Offset, Limit: raw.Limit}
	for _, item := range raw.Items {
		saved := catalog.SavedTrack{}
		if item.Track != nil {
			saved.Track = catalogTrack(*item.Track)
		}
		if item.AddedAt != "" {
			saved.AddedAt, _ = time.Parse(time.RFC3339Nano, item.AddedAt)
		}
		page.Items = append(page.Items, saved)
	}
	return page, nil
}

// SetLiked saves or removes tracks using the current URI-based library API.
func (c *API) SetLiked(ctx context.Context, uris []string, liked bool) error {
	method := "DELETE"
	if liked {
		method = "PUT"
	}
	for start := 0; start < len(uris); start += 40 {
		end := min(start+40, len(uris))
		query := url.Values{"uris": {strings.Join(uris[start:end], ",")}}
		if _, err := c.request(ctx, method, "/me/library", query, nil); err != nil {
			return err
		}
	}
	return nil
}

// Liked reports whether each track URI is saved, using the URI-based
// contains endpoint.
func (c *API) Liked(ctx context.Context, uris []string) ([]bool, error) {
	var out []bool
	for start := 0; start < len(uris); start += 40 {
		end := min(start+40, len(uris))
		body, err := c.request(ctx, "GET", "/me/library/contains", url.Values{"uris": {strings.Join(uris[start:end], ",")}}, nil)
		if err != nil {
			return nil, err
		}
		var page []bool
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("spotify Web API: decode library membership: %w", err)
		}
		if len(page) != end-start {
			return nil, errors.New("spotify Web API: unexpected library membership count")
		}
		out = append(out, page...)
	}
	return out, nil
}

// ContextTracks reads saved tracks, album tracks, or playlist items. The
// playlist endpoint is the documented /items route.
func (c *API) ContextTracks(ctx context.Context, uri string) (model.Listing, error) {
	if isLikedURI(uri) {
		return c.likedListing(ctx, uri)
	}
	if id, ok := uriID(uri, "spotify:album:"); ok {
		return c.albumListing(ctx, uri, id)
	}
	if id, ok := uriID(uri, "spotify:playlist:"); ok {
		return c.playlistListing(ctx, uri, id)
	}
	return model.Listing{}, errors.New("spotify Web API: unsupported context URI")
}

func (c *API) likedListing(ctx context.Context, uri string) (model.Listing, error) {
	listing := model.Listing{URI: uri, Ready: true}
	for offset := 0; ; offset += pageSize {
		page, err := c.LikedTracks(ctx, offset, pageSize)
		if err != nil {
			return model.Listing{}, err
		}
		listing.Length = page.Total
		for _, saved := range page.Items {
			track := modelFromCatalog(saved.Track)
			track.AddedAt = saved.AddedAt
			listing.Tracks = append(listing.Tracks, track)
		}
		listing.Cached = len(listing.Tracks)
		if len(page.Items) == 0 || offset+len(page.Items) >= page.Total {
			return listing, nil
		}
	}
}

func (c *API) playlistListing(ctx context.Context, uri, id string) (model.Listing, error) {
	listing := model.Listing{URI: uri, Ready: true}
	for offset := 0; ; offset += pageSize {
		body, err := c.request(ctx, "GET", "/playlists/"+url.PathEscape(id)+"/items", url.Values{
			"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)},
		}, nil)
		if err != nil {
			return model.Listing{}, err
		}
		var page struct {
			Items []struct {
				Item *rawTrack `json:"item"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return model.Listing{}, err
		}
		listing.Length = page.Total
		for _, item := range page.Items {
			if item.Item == nil || (item.Item.Type != "" && item.Item.Type != "track") {
				listing.Tracks = append(listing.Tracks, model.Track{})
				continue
			}
			listing.Tracks = append(listing.Tracks, modelFromRaw(*item.Item))
		}
		listing.Cached = len(listing.Tracks)
		if len(page.Items) == 0 || offset+len(page.Items) >= page.Total {
			return listing, nil
		}
	}
}

func (c *API) albumListing(ctx context.Context, uri, id string) (model.Listing, error) {
	album, err := c.albumRaw(ctx, id)
	if err != nil {
		return model.Listing{}, err
	}
	listing := model.Listing{URI: uri, Ready: true, Length: album.Total}
	for offset := 0; ; offset += pageSize {
		body, err := c.request(ctx, "GET", "/albums/"+url.PathEscape(id)+"/tracks", url.Values{
			"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)},
		}, nil)
		if err != nil {
			return model.Listing{}, err
		}
		var page rawTrackPage
		if err := json.Unmarshal(body, &page); err != nil {
			return model.Listing{}, err
		}
		if page.Total > 0 {
			listing.Length = page.Total
		}
		for _, raw := range page.Items {
			raw.Album = album
			listing.Tracks = append(listing.Tracks, modelFromRaw(raw))
		}
		listing.Cached = len(listing.Tracks)
		if len(page.Items) == 0 || offset+len(page.Items) >= listing.Length {
			return listing, nil
		}
	}
}

// Album loads a documented album resource and its track listing.
func (c *API) Album(ctx context.Context, uri string) (catalog.AlbumDetails, error) {
	id, ok := uriID(uri, "spotify:album:")
	if !ok {
		return catalog.AlbumDetails{}, errors.New("spotify Web API: invalid album URI")
	}
	raw, err := c.albumRaw(ctx, id)
	if err != nil {
		return catalog.AlbumDetails{}, err
	}
	tracks := make([]catalog.Track, 0, raw.Total)
	for offset := 0; ; offset += pageSize {
		pageBody, err := c.request(ctx, "GET", "/albums/"+url.PathEscape(id)+"/tracks", url.Values{
			"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)},
		}, nil)
		if err != nil {
			return catalog.AlbumDetails{}, err
		}
		var page rawTrackPage
		if err := json.Unmarshal(pageBody, &page); err != nil {
			return catalog.AlbumDetails{}, err
		}
		if page.Total > 0 {
			raw.Total = page.Total
		}
		for _, track := range page.Items {
			track.Album = raw
			tracks = append(tracks, catalogTrack(track))
		}
		if len(page.Items) == 0 || offset+len(page.Items) >= raw.Total {
			break
		}
	}
	return catalog.AlbumDetails{Album: catalogAlbum(raw), Tracks: tracks, Total: raw.Total}, nil
}

func (c *API) albumRaw(ctx context.Context, id string) (rawAlbum, error) {
	body, err := c.request(ctx, "GET", "/albums/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return rawAlbum{}, err
	}
	var album rawAlbum
	if err := json.Unmarshal(body, &album); err != nil {
		return rawAlbum{}, err
	}
	if album.URI == "" {
		album.URI = "spotify:album:" + id
	}
	return album, nil
}

// Artist loads artist metadata. The Web API's artist top-tracks endpoint is
// unavailable to current Development Mode apps, so no unofficial substitute
// is queried.
func (c *API) Artist(ctx context.Context, uri string) (catalog.ArtistOverview, error) {
	id, ok := uriID(uri, "spotify:artist:")
	if !ok {
		return catalog.ArtistOverview{}, errors.New("spotify Web API: invalid artist URI")
	}
	body, err := c.request(ctx, "GET", "/artists/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return catalog.ArtistOverview{}, err
	}
	var raw struct {
		ID     string          `json:"id"`
		URI    string          `json:"uri"`
		Name   string          `json:"name"`
		Images []catalog.Image `json:"images"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return catalog.ArtistOverview{}, err
	}
	return catalog.ArtistOverview{Artist: catalog.Artist{URI: nonempty(raw.URI, uri), Name: raw.Name}, Image: catalog.Cover(raw.Images, 320)}, nil
}

// Home builds a small documented Home shelf from the user's own playlists.
func (c *API) Home(ctx context.Context) (catalog.HomeFeed, error) {
	playlists, err := c.Playlists(ctx)
	if err != nil {
		return catalog.HomeFeed{}, err
	}
	section := catalog.HomeSection{Title: "Your playlists"}
	for _, p := range playlists {
		section.Items = append(section.Items, catalog.HomeItem{URI: p.URI, Name: p.Name, Kind: "playlist", Image: p.Image})
	}
	if len(section.Items) == 0 {
		return catalog.HomeFeed{}, nil
	}
	return catalog.HomeFeed{Sections: []catalog.HomeSection{section}}, nil
}

type historyPage struct {
	Items []struct {
		PlayedAt string   `json:"played_at"`
		Track    rawTrack `json:"track"`
		Context  struct {
			URI string `json:"uri"`
		} `json:"context"`
	} `json:"items"`
}

// RecentlyPlayed returns unique contexts ordered by their latest play, using
// only Spotify's documented recently-played endpoint.
func (c *API) RecentlyPlayed(ctx context.Context, limit int) ([]model.Played, error) {
	limit = min(max(limit, 1), 50)
	body, err := c.request(ctx, "GET", "/me/player/recently-played", url.Values{"limit": {strconv.Itoa(limit)}}, nil)
	if err != nil {
		return nil, err
	}
	var page historyPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]model.Played, 0, limit)
	for _, item := range page.Items {
		if item.Context.URI == "" || seen[item.Context.URI] {
			continue
		}
		seen[item.Context.URI] = true
		at, _ := time.Parse(time.RFC3339Nano, item.PlayedAt)
		out = append(out, model.Played{ContextURI: item.Context.URI, TrackURI: item.Track.URI, At: at})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func catalogTrack(track rawTrack) catalog.Track {
	artists := append([]catalog.Artist(nil), track.Artists...)
	return catalog.Track{
		URI: track.URI, Name: track.Name, DurationMS: track.DurationMS,
		Artists: artists, Album: catalogAlbum(track.Album),
	}
}

func catalogAlbum(album rawAlbum) catalog.Album {
	return catalog.Album{URI: album.URI, Name: album.Name, Images: album.Images, Artists: album.Artists}
}

func catalogPlaylist(playlist rawPlaylist) catalog.Playlist {
	var result catalog.Playlist
	result.URI, result.Name, result.Description = playlist.URI, playlist.Name, playlist.Description
	result.Images = playlist.Images
	result.Owner.DisplayName = playlist.Owner.DisplayName
	return result
}

func modelFromCatalog(track catalog.Track) model.Track {
	result := model.Track{
		URI: track.URI, Name: track.Name, Album: track.Album.Name, AlbumURI: track.Album.URI,
		Cover: catalog.Cover(track.Album.Images, 128), Duration: time.Duration(track.DurationMS) * time.Millisecond,
	}
	for _, artist := range track.Artists {
		result.Artists = append(result.Artists, artist.Name)
		result.ArtistURIs = append(result.ArtistURIs, artist.URI)
	}
	return result
}

func modelFromRaw(track rawTrack) model.Track {
	result := modelFromCatalog(catalogTrack(track))
	result.TrackNumber, result.Disc = track.TrackNo, track.DiscNo
	return result
}

func uriID(uri, prefix string) (string, bool) {
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(uri, prefix)
	return id, id != "" && !strings.Contains(id, ":")
}

func isLikedURI(uri string) bool {
	return strings.HasPrefix(uri, "spotify:user:") && strings.HasSuffix(uri, ":collection")
}

func nonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
