package player

import (
	"time"

	"github.com/devgianlu/go-librespot/daemon"
)

// Event is something the player reports. The set is closed.
type Event interface{ isEvent() }

// Pairing asks the user to approve this device: open URL (or enter Code at
// spotify.com/pair) before Expires. A zero Pairing means pairing is over.
type Pairing struct {
	URL, Code string
	Expires   time.Time
}

// Ready reports the player signed in and ready to play.
type Ready struct{}

// Track is the track loaded now, with its position when reported.
type Track struct {
	URI, Name         string
	Artists           []string
	ArtistURIs        []string
	Album, AlbumURI   string
	Cover             string    // cover image URL ("": none)
	AddedAt           time.Time // Liked Songs timestamp; zero when unavailable.
	Duration          time.Duration
	Position          time.Duration
	ContextURI        string
	TrackNumber, Disc int
}

// WillPlay identifies the track the player is about to load. Playing,
// Paused, and Stopped report the actual playback state.
type (
	WillPlay struct{ URI, ContextURI string }
	Playing  struct{ URI, ContextURI string }
	Paused   struct{ URI, ContextURI string }
	Stopped  struct{}
)

// Seek reports a new position in the track.
type Seek struct{ Position, Duration time.Duration }

// Volume reports the volume, Value out of Max.
type Volume struct{ Value, Max int }

// Shuffle and Repeat report the play modes.
type (
	Shuffle struct{ On bool }
	Repeat  struct{ Context, Track bool }
)

// PlaybackError reports a track that failed to play.
type PlaybackError struct {
	URI, Kind  string
	Unplayable bool
}

// Failed reports that the player stopped with an error.
type Failed struct{ Err error }

func (Pairing) isEvent()       {}
func (Ready) isEvent()         {}
func (Track) isEvent()         {}
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

func ms(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

// trackOf converts the player's track.
func trackOf(t daemon.ApiTrack) Track {
	tr := Track{
		URI: t.Uri, Name: t.Name, Artists: t.ArtistNames, ArtistURIs: t.ArtistUris,
		Album: t.AlbumName, AlbumURI: t.AlbumUri,
		Duration: ms(int64(t.Duration)), Position: ms(t.Position),
		TrackNumber: t.TrackNumber, Disc: t.DiscNumber,
	}
	if t.AlbumCoverUrl != nil {
		tr.Cover = *t.AlbumCoverUrl
	}
	return tr
}

// eventOf converts a player event; ok is false for ones Smolify ignores.
// repeat holds the last repeat modes, which the player reports one at a
// time.
func eventOf(ev *daemon.ApiEvent, repeat *Repeat) (Event, bool) {
	switch d := ev.Data.(type) {
	case daemon.ApiEventDataMetadata:
		return trackOf(daemon.ApiTrack(d)), true
	case daemon.ApiEventDataWillPlay:
		return WillPlay{URI: d.Uri, ContextURI: d.ContextUri}, true
	case daemon.ApiEventDataPlaying:
		return Playing{URI: d.Uri, ContextURI: d.ContextUri}, true
	case daemon.ApiEventDataPaused:
		return Paused{URI: d.Uri, ContextURI: d.ContextUri}, true
	case daemon.ApiEventDataNotPlaying, daemon.ApiEventDataStopped:
		return Stopped{}, true
	case daemon.ApiEventDataSeek:
		return Seek{Position: ms(int64(d.Position)), Duration: ms(int64(d.Duration))}, true
	case daemon.ApiEventDataVolume:
		return Volume{Value: int(d.Value), Max: int(d.Max)}, true
	case daemon.ApiEventDataShuffleContext:
		return Shuffle{On: d.Value}, true
	case daemon.ApiEventDataRepeatContext:
		repeat.Context = d.Value
		return *repeat, true
	case daemon.ApiEventDataRepeatTrack:
		repeat.Track = d.Value
		return *repeat, true
	case daemon.ApiEventDataPlaybackError:
		return PlaybackError{URI: d.Uri, Kind: d.Kind, Unplayable: d.Unplayable}, true
	}
	if ev.Type == daemon.ApiEventTypePlaybackReady {
		return Ready{}, true
	}
	return nil, false
}
