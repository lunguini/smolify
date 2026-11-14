//go:build !js || !wasm

package spotify

import "github.com/lunguini/smolify/internal/catalog"

type page[T any] struct {
	Items []T `json:"items"`
}

// Track is the search metadata used by the app.
type Track = catalog.Track

// SavedTrack is a track in the user's Liked Songs collection. AddedAt is zero
// when the service does not include a saved timestamp for that item.
type SavedTrack = catalog.SavedTrack

// SavedTracksPage is one bounded page of Liked Songs. Total is Spotify's
// reported collection size; Items preserves one slot per returned raw item,
// including entries whose metadata is unavailable.
type SavedTracksPage = catalog.SavedTracksPage

type Artist = catalog.Artist

type Album = catalog.Album

type Playlist = catalog.Playlist

type Image = catalog.Image

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
