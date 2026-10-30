package app

import (
	"strings"

	"github.com/lunguini/flatte"
)

// nowPlaying is system media metadata derived only from the player's confirmed
// track state. A pending play request never supplies track metadata.
func nowPlaying(s *State) *flatte.NowPlaying {
	if s == nil || !s.ready || s.track == nil || loadingPlay(s) || s.mediaTrackURI == "" || s.track.URI != s.mediaTrackURI {
		return nil
	}
	track := s.track
	return &flatte.NowPlaying{
		Title: track.Name, Artist: strings.Join(track.Artists, ", "), Album: track.Album,
		Duration: track.Duration, Position: s.position, Playing: s.mediaPlaying,
		CanNext: true, CanPrevious: true,
	}
}

func handleMedia(s *State, e flatte.MediaEvent, fx flatte.Effects[State]) {
	// Global media buttons must never start whichever playlist happens to be
	// selected when no confirmed track is active.
	if !s.ready || s.track == nil || s.mediaTrackURI == "" || s.track.URI != s.mediaTrackURI {
		return
	}
	switch e.Command {
	case flatte.MediaPlay:
		if !s.playing && !loadingPlay(s) {
			command(s, cmdPlayPause, fx)
		}
	case flatte.MediaPause:
		if s.playing && !loadingPlay(s) {
			command(s, cmdPlayPause, fx)
		}
	case flatte.MediaTogglePlayPause:
		if !loadingPlay(s) {
			command(s, cmdPlayPause, fx)
		}
	case flatte.MediaNext:
		if !loadingPlay(s) {
			command(s, cmdNext, fx)
		}
	case flatte.MediaPrevious:
		if !loadingPlay(s) {
			command(s, cmdPrev, fx)
		}
	}
}
