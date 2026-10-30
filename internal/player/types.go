//go:build !js || !wasm

package player

import "github.com/lunguini/smolify/internal/model"

// Type aliases keep the native player package's public data paths stable
// while allowing app and browser builds to depend only on model.
type (
	Audio         = model.Audio
	Track         = model.Track
	Status        = model.Status
	Playlist      = model.Playlist
	Listing       = model.Listing
	Played        = model.Played
	Event         = model.Event
	Pairing       = model.Pairing
	Ready         = model.Ready
	WillPlay      = model.WillPlay
	Playing       = model.Playing
	Paused        = model.Paused
	Stopped       = model.Stopped
	Seek          = model.Seek
	Volume        = model.Volume
	Shuffle       = model.Shuffle
	Repeat        = model.Repeat
	PlaybackError = model.PlaybackError
	Failed        = model.Failed
)

// LikedSongs is retained here for existing native consumers.
func LikedSongs(username string) string { return model.LikedSongs(username) }
