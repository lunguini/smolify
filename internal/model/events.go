package model

import "time"

// Event is a player or browser playback event. The set is closed.
type Event interface{ isEvent() }

// Pairing asks the user to approve a device.
type Pairing struct {
	URL, Code string
	Expires   time.Time
}

// Ready reports that the player is ready for requests.
type Ready struct{}

// Track is also the metadata event for a loaded track.
func (Track) isEvent() {}

// WillPlay identifies the track a player is about to load. Playing and
// Paused report confirmed playback state.
type (
	WillPlay struct{ URI, ContextURI string }
	Playing  struct{ URI, ContextURI string }
	Paused   struct{ URI, ContextURI string }
	Stopped  struct{}
	Seek     struct{ Position, Duration time.Duration }
	Volume   struct{ Value, Max int }
	Shuffle  struct{ On bool }
	Repeat   struct{ Context, Track bool }
)

// PlaybackError reports a track that failed to play.
type PlaybackError struct {
	URI, Kind  string
	Unplayable bool
	Message    string
}

// Failed reports that the player stopped with an error.
type Failed struct{ Err error }

func (Pairing) isEvent()       {}
func (Ready) isEvent()         {}
func (WillPlay) isEvent()      {}
func (Playing) isEvent()       {}
func (Paused) isEvent()        {}
func (Stopped) isEvent()       {}
func (Seek) isEvent()          {}
func (Volume) isEvent()        {}
func (Shuffle) isEvent()       {}
func (Repeat) isEvent()        {}
func (PlaybackError) isEvent() {}
func (Failed) isEvent()        {}
