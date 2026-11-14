//go:build !js || !wasm

package player

import (
	"time"

	"github.com/devgianlu/go-librespot/daemon"

	"github.com/lunguini/smolify/internal/model"
)

func ms(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

// trackOf converts the player's track.
func trackOf(t daemon.ApiTrack) model.Track {
	tr := model.Track{
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
func eventOf(ev *daemon.ApiEvent, repeat *model.Repeat) (model.Event, bool) {
	switch d := ev.Data.(type) {
	case daemon.ApiEventDataMetadata:
		return trackOf(daemon.ApiTrack(d)), true
	case daemon.ApiEventDataWillPlay:
		return model.WillPlay{URI: d.Uri, ContextURI: d.ContextUri}, true
	case daemon.ApiEventDataPlaying:
		return model.Playing{URI: d.Uri, ContextURI: d.ContextUri}, true
	case daemon.ApiEventDataPaused:
		return model.Paused{URI: d.Uri, ContextURI: d.ContextUri}, true
	case daemon.ApiEventDataNotPlaying, daemon.ApiEventDataStopped:
		return model.Stopped{}, true
	case daemon.ApiEventDataSeek:
		return model.Seek{Position: ms(int64(d.Position)), Duration: ms(int64(d.Duration))}, true
	case daemon.ApiEventDataVolume:
		return model.Volume{Value: int(d.Value), Max: int(d.Max)}, true
	case daemon.ApiEventDataShuffleContext:
		return model.Shuffle{On: d.Value}, true
	case daemon.ApiEventDataRepeatContext:
		repeat.Context = d.Value
		return *repeat, true
	case daemon.ApiEventDataRepeatTrack:
		repeat.Track = d.Value
		return *repeat, true
	case daemon.ApiEventDataPlaybackError:
		return model.PlaybackError{URI: d.Uri, Kind: d.Kind, Unplayable: d.Unplayable, Message: d.Message}, true
	}
	if ev.Type == daemon.ApiEventTypePlaybackReady {
		return model.Ready{}, true
	}
	return nil, false
}
