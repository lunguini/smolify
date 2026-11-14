// Package model contains the player and catalog data shared by Smolify's
// native and browser hosts. It has no player, filesystem, or host imports.
package model

import (
	"errors"
	"time"
)

// Audio is how a player plays. Browser players may ignore settings they do
// not expose.
type Audio struct {
	Bitrate   int
	Normalise bool
	Crossfade time.Duration
	Autoplay  bool
}

// Track is a playable item and its catalog metadata.
type Track struct {
	URI, Name         string
	Artists           []string
	ArtistURIs        []string
	Album, AlbumURI   string
	Cover             string
	AddedAt           time.Time
	Duration          time.Duration
	Position          time.Duration
	ContextURI        string
	TrackNumber, Disc int
}

// Status is the current player state.
type Status struct {
	Username            string
	Track               *Track
	Paused, Stopped     bool
	Volume, VolumeSteps int
	Shuffle             bool
	Repeat              Repeat
	ContextURI          string
}

// Playlist is a playlist in the user's library.
type Playlist struct {
	URI, Name, Description, Owner string
	Image                         string
	Length                        int
	Folder                        []string
}

// Listing is a context's tracks as far as they have been resolved.
type Listing struct {
	URI    string
	Ready  bool
	Length int
	Cached int
	Tracks []Track
}

// Complete reports that every track is listed with its metadata.
func (l Listing) Complete() bool { return l.Ready && l.Cached >= l.Length }

// Played is a recently played context with its last track and time.
type Played struct {
	ContextURI, TrackURI string
	At                   time.Time
}

// ErrSuperseded reports a command that a newer command replaced before it
// ran.
var ErrSuperseded = errors.New("player: command superseded")

// LikedSongs is the context URI of a user's Liked Songs.
func LikedSongs(username string) string { return "spotify:user:" + username + ":collection" }
