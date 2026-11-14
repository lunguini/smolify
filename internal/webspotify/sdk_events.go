package webspotify

import (
	"strings"
	"time"

	"github.com/lunguini/smolify/internal/model"
)

const (
	SDKReady          = "ready"
	SDKNotReady       = "not_ready"
	SDKStateChanged   = "player_state_changed"
	SDKInitError      = "initialization_error"
	SDKAuthError      = "authentication_error"
	SDKAccountError   = "account_error"
	SDKPlaybackError  = "playback_error"
	SDKAutoplayFailed = "autoplay_failed"
)

// SDKState is the DOM-free portion of Web Playback SDK state used by Smolify.
type SDKState struct {
	Track      model.Track
	ContextURI string
	Position   time.Duration
	Duration   time.Duration
	Paused     bool
	Empty      bool
}

// SDKEvent is an SDK callback after the JS boundary has extracted its values.
type SDKEvent struct {
	Type     string
	DeviceID string
	State    SDKState
	Message  string
}

// MapSDKState converts a browser player state into the same events used by
// the desktop player. It has no DOM or syscall/js dependency.
func MapSDKState(state SDKState) []model.Event {
	if state.Empty || state.Track.URI == "" {
		return []model.Event{model.Stopped{}}
	}
	track := state.Track
	track.ContextURI = state.ContextURI
	track.Position = state.Position
	out := []model.Event{
		track,
		model.Seek{Position: state.Position, Duration: state.Duration},
	}
	if state.Paused {
		out = append(out, model.Paused{URI: track.URI, ContextURI: state.ContextURI})
	} else {
		out = append(out, model.Playing{URI: track.URI, ContextURI: state.ContextURI})
	}
	return out
}

// MapSDKError gives SDK errors a predictable app-level kind, including
// Premium-account failures.
func MapSDKError(kind, message string) model.Event {
	kind = strings.TrimSpace(kind)
	if message == "" {
		message = "Spotify browser playback failed"
	}
	if kind == SDKAccountError {
		return model.PlaybackError{Kind: "account", Message: message}
	}
	if kind == SDKAuthError {
		return model.PlaybackError{Kind: "authentication", Message: message}
	}
	if kind == SDKInitError {
		return model.PlaybackError{Kind: "initialization", Message: message}
	}
	if kind == SDKAutoplayFailed {
		return model.PlaybackError{Kind: "autoplay", Message: message}
	}
	return model.PlaybackError{Kind: "playback", Message: message}
}
