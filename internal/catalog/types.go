// Package catalog contains provider-neutral catalog records used by the UI.
// Spotify adapters may use these DTOs without making the app depend on a
// particular transport or endpoint implementation.
package catalog

import "time"

// Track is catalog metadata for a track.
type Track struct {
	URI        string   `json:"uri"`
	Name       string   `json:"name"`
	DurationMS int      `json:"duration_ms"`
	Artists    []Artist `json:"artists"`
	Album      Album    `json:"album"`
}

// SavedTrack is a track in the user's saved collection.
type SavedTrack struct {
	Track   Track
	AddedAt time.Time
}

// SavedTracksPage is one bounded page of saved tracks.
type SavedTracksPage struct {
	Total  int
	Items  []SavedTrack
	Offset int
	Limit  int
}

type Artist struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type Album struct {
	URI     string   `json:"uri"`
	Name    string   `json:"name"`
	Images  []Image  `json:"images"`
	Artists []Artist `json:"artists"`
}

type Playlist struct {
	URI         string  `json:"uri"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Images      []Image `json:"images"`
	Owner       struct {
		DisplayName string `json:"display_name"`
	} `json:"owner"`
}

type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Cover chooses the smallest image at least px wide, or the largest image
// when none reaches that width.
func Cover(images []Image, px int) string {
	best := -1
	for i, image := range images {
		switch {
		case best < 0:
			best = i
		case image.Width >= px && (images[best].Width < px || image.Width < images[best].Width):
			best = i
		case images[best].Width < px && image.Width > images[best].Width:
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return images[best].URL
}

// Results are the matching catalog sections returned by search.
type Results struct {
	Tracks    []Track
	Albums    []Album
	Artists   []Artist
	Playlists []Playlist
}

// HomeFeed is a set of catalog cards shown on Home.
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

// ArtistOverview carries an artist and its track listing.
type ArtistOverview struct {
	Artist    Artist
	Image     string
	TopTracks []Track
}

// AlbumDetails contains album metadata and its track listing.
type AlbumDetails struct {
	Album  Album
	Tracks []Track
	Total  int
}
