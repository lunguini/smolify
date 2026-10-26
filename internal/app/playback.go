package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/smolify/internal/player"
)

// playIntent is the requested next track, separate from confirmed player
// metadata. An API acknowledgement does not mean audio has started.
type playIntent struct {
	track             player.Track
	contextURI, shown string
	spinner           flatui.Spinner
	scope             *flatte.Scope
	stopAccount       func() bool
	remaining         time.Duration
	flexibleTarget    bool // originally unknown (e.g. a shuffled playlist)
}

func loadingPlay(s *State) bool { return s.pending != nil }
func loadingIndicator(s *State) string {
	if s.pending == nil {
		return ""
	}
	return s.pending.spinner.View()
}
func loadingPlaylist(s *State, uri string) bool {
	return s.pending != nil && (s.pending.contextURI == uri || s.pending.shown == uri)
}
func pendingTrack(s *State, t player.Track, shown string) bool {
	p := s.pending
	return p != nil && t.URI != "" && t.URI == p.track.URI && (shown == p.shown || shown == p.contextURI || virtual(shown))
}

func clearPendingPlay(s *State) {
	if p := s.pending; p != nil {
		if p.scope != nil {
			p.scope.Cancel()
		}
		if p.stopAccount != nil {
			p.stopAccount()
		}
		s.pending = nil
	}
	flatte.Cancel(s.fx, "play")
}

func requestPlay(s *State, uri, skip string, track player.Track) {
	if p := s.pending; p != nil && p.contextURI == uri && p.track.URI == track.URI {
		return
	}
	clearPendingPlay(s)
	scope := flatte.NewScope(s.fx, "play-loading")
	p := &playIntent{track: track, contextURI: uri, shown: s.shown, spinner: flatui.NewSpinner(flatui.SpinnerDots), scope: scope, remaining: 30 * time.Second, flexibleTarget: track.URI == ""}
	p.stopAccount = context.AfterFunc(accountScope(s).Ctx(), scope.Cancel)
	s.pending = p
	s.status = ""
	if p.track.URI != "" {
		if i := indexOf(displayListing(s, s.listings[s.shown]).Tracks, p.track.URI); i >= 0 {
			s.tracks.Select(i)
		}
	}
	_ = flatte.ScopeEvery(scope, s.fx, 100*time.Millisecond, func(s *State, _ time.Time) {
		if s.pending != p {
			return
		}
		p.remaining -= 100 * time.Millisecond
		if p.remaining <= 0 {
			clearPendingPlay(s)
			fail(s, "Could not play: playback did not start in time.")
			return
		}
		p.spinner.Tick()
	})
	be := s.be
	accountLatest(s, "play", func(ctx context.Context) (struct{}, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return struct{}{}, be.Play(ctx, uri, skip)
	}, func(s *State, _ struct{}, err error) {
		if s.pending != p {
			return
		}
		s.logf("request play: err=%v", err)
		if err != nil {
			clearPendingPlay(s)
			if !errors.Is(err, player.ErrSuperseded) && !errors.Is(err, context.Canceled) {
				fail(s, "Could not play: "+err.Error())
			}
		}
	})
}

func pendingMatches(s *State, uri, contextURI string) bool {
	p := s.pending
	if p == nil || uri == "" {
		return false
	}
	if contextURI != "" && p.contextURI != "" && contextURI != p.contextURI {
		return false
	}
	if p.track.URI != "" {
		return uri == p.track.URI
	}
	return contextURI != "" && contextURI == p.contextURI
}

// confirmPendingPlay runs only for a matching actual Playing event. When
// metadata has not arrived yet, the requested row supplies its known data.
func confirmPendingPlay(s *State, e player.Playing) {
	if !pendingMatches(s, e.URI, e.ContextURI) {
		return
	}
	if s.track == nil || s.track.URI != e.URI {
		track := s.pending.track
		track.URI, track.ContextURI = e.URI, e.ContextURI
		track.Position = 0
		s.track, s.position = &track, 0
	}
	clearPendingPlay(s)
}

// bindPendingPlay attributes an unknown playlist/shuffle target using the
// daemon's will_play context before metadata or a loading failure arrives.
func bindPendingPlay(s *State, uri, contextURI string) {
	p := s.pending
	if p == nil || uri == "" || contextURI != p.contextURI {
		return
	}
	if !p.flexibleTarget && p.track.URI != "" && p.track.URI != uri {
		return
	}
	if p.track.URI != uri {
		p.track = player.Track{URI: uri, ContextURI: contextURI}
	}
	p.track.URI, p.track.ContextURI = uri, contextURI
	origin := s.listings[p.shown].Tracks
	if i := indexOf(origin, uri); i >= 0 {
		p.track = origin[i]
	}
	if s.shown == p.shown || s.shown == p.contextURI {
		if i := indexOf(displayListing(s, s.listings[s.shown]).Tracks, uri); i >= 0 {
			s.tracks.Select(i)
		}
	}
}

func enrichPendingPlay(s *State, t player.Track) {
	if p := s.pending; p != nil && t.URI != "" && t.URI == p.track.URI {
		if t.ContextURI != "" && t.ContextURI != p.contextURI {
			return
		}
		p.track = t
	}
}
