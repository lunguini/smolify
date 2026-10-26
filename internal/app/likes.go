package app

import (
	"context"
	"strings"

	"github.com/lunguini/smolify/internal/player"
)

// A track has one in-flight write. Repeated clicks change its desired value;
// the next request follows acknowledgement, keeping rollback and disk state
// based on confirmed membership rather than another optimistic click.
type likeChange struct {
	track                     player.Track
	baseline                  player.Listing
	known, confirmed, desired bool
}

func setLiked(s *State, t player.Track, on bool) {
	if !strings.HasPrefix(t.URI, "spotify:track:") {
		return
	}
	change := s.likeChanges[t.URI]
	first := change == nil
	if first {
		was, known := s.liked[t.URI]
		change = &likeChange{track: t, baseline: s.listings[player.LikedSongs(s.username)], known: known, confirmed: was}
		s.likeChanges[t.URI] = change
		s.likedWrites++
	}
	change.desired = on
	s.liked[t.URI] = on
	if s.username != "" {
		uri := player.LikedSongs(s.username)
		s.likedGeneration[uri]++
		delete(s.likedLoads, uri)
		if listing, ok := s.listings[uri]; ok {
			replaceListing(s, uri, withLiked(listing, t, on))
			s.stale[uri] = true
		}
	}
	if first {
		writeLike(s, t.URI, change)
	}
}

func writeLike(s *State, uri string, change *likeChange) {
	target, be := change.desired, s.be
	accountGo(s, "like:"+uri, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, be.SetLiked(ctx, []string{uri}, target)
	}, func(s *State, _ struct{}, err error) {
		if s.likeChanges[uri] != change {
			return
		}
		if err == nil {
			change.confirmed, change.known = target, true
			s.cacheLikesDirty = true
		} else {
			fail(s, "Could not update Liked Songs: "+err.Error())
			if change.desired == target {
				change.desired = change.confirmed
			}
		}
		if change.desired != change.confirmed {
			writeLike(s, uri, change)
			return
		}
		if change.known {
			s.liked[uri] = change.confirmed
		} else {
			delete(s.liked, uri)
		}
		likedURI := player.LikedSongs(s.username)
		if listing, ok := s.listings[likedURI]; ok {
			if change.confirmed {
				listing = restoreLikedTrack(listing, change.baseline, change.track)
			} else {
				listing = withLiked(listing, change.track, false)
			}
			replaceListing(s, likedURI, listing)
			s.stale[likedURI] = true
		}
		delete(s.likeChanges, uri)
		s.likedWrites = max(0, s.likedWrites-1)
		if s.likedWrites == 0 && s.cacheLikesDirty {
			s.cacheLikesDirty = false
			saveCache(s)
		}
		if s.username != "" && s.shown == likedURI && s.likedWrites == 0 {
			loadTracks(s, likedURI)
		}
	})
}

// Restore the original occurrence relative to surviving neighbors. Other
// tracks' successful updates are preserved when a concurrent unlike fails.
func restoreLikedTrack(current, before player.Listing, t player.Track) player.Listing {
	if indexOf(current.Tracks, t.URI) >= 0 {
		return current
	}
	original := indexOf(before.Tracks, t.URI)
	if original < 0 {
		return withLiked(current, t, true)
	}
	position := len(current.Tracks)
	for _, next := range before.Tracks[original+1:] {
		if i := indexOf(current.Tracks, next.URI); i >= 0 {
			position = i
			break
		}
	}
	if position == len(current.Tracks) {
		for i := original - 1; i >= 0; i-- {
			if at := indexOf(current.Tracks, before.Tracks[i].URI); at >= 0 {
				position = at + 1
				break
			}
		}
	}
	tracks := make([]player.Track, 0, len(current.Tracks)+1)
	tracks = append(tracks, current.Tracks[:position]...)
	tracks = append(tracks, before.Tracks[original])
	tracks = append(tracks, current.Tracks[position:]...)
	current.Tracks = tracks
	current.Cached++
	if current.Ready {
		current.Length++
	}
	return current
}
