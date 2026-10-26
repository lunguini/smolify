package spotify

import "time"

type page[T any] struct {
	Items []T `json:"items"`
}

// Track is the search metadata used by the app.
type Track struct {
	URI        string   `json:"uri"`
	Name       string   `json:"name"`
	DurationMS int      `json:"duration_ms"`
	Artists    []Artist `json:"artists"`
	Album      Album    `json:"album"`
}

// SavedTrack is a track in the user's Liked Songs collection. AddedAt is zero
// when the service does not include a saved timestamp for that item.
type SavedTrack struct {
	Track   Track
	AddedAt time.Time
}

// SavedTracksPage is one bounded page of Liked Songs. Total is Spotify's
// reported collection size; Items preserves one slot per returned raw item,
// including entries whose metadata is unavailable.
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

// Cover is the URL of the smallest of images at least px wide, or the
// largest when none is ("": none).
func Cover(images []Image, px int) string {
	best := -1
	for i, im := range images {
		switch {
		case best < 0:
			best = i
		case im.Width >= px && (images[best].Width < px || im.Width < images[best].Width):
			best = i
		case images[best].Width < px && im.Width > images[best].Width:
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return images[best].URL
}
