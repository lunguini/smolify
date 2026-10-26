package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/lunguini/flatte"
	"github.com/lunguini/smolify/internal/librarycache"
	"github.com/lunguini/smolify/internal/player"
)

const regularListingTrackCap = 2000

// cacheableListing rejects regular player listings whose completeness is
// ambiguous because the backend caps enumeration at 2,000 tracks.
func cacheableListing(s *State, uri string, listing player.Listing) bool {
	if !listing.Complete() || listing.Length < 0 || listing.Cached < 0 || len(listing.Tracks) != listing.Length {
		return false
	}
	if likedURI(s, uri) {
		return true // the web page API reports the real total independently
	}
	for _, pl := range s.playlists {
		if pl.URI == uri {
			if pl.Length > 0 {
				return listing.Length == pl.Length
			}
			return listing.Length < regularListingTrackCap
		}
	}
	if strings.HasPrefix(uri, "spotify:playlist:") {
		return listing.Length < regularListingTrackCap
	}
	return false
}

// WithCache enables a persistent library cache rooted in dir. The cache is
// keyed by the signed-in Spotify username/account identifier.
func WithCache(app flatte.App[State], dir string) flatte.App[State] {
	app.State.cache = librarycache.New(dir)
	return app
}

func hydrateCache(s *State, account string) {
	if s.cache == nil || account == "" || s.cacheLoaded == account {
		return
	}
	s.cacheLoaded = account
	s.cacheLoading = true
	cache := s.cache
	epoch := cache.Generation(account)
	s.cacheEpoch = epoch
	accountGo(s, "library-cache-load", func(ctx context.Context) (librarycache.Snapshot, error) {
		return cache.Load(account)
	}, func(s *State, snapshot librarycache.Snapshot, err error) {
		if s.cache != cache || s.username != account || s.cacheEpoch != epoch {
			return
		}
		s.cacheLoading = false
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				// Corrupt cache is ignored and replaced only after a successful
				// library read has supplied safe current data.
				s.logf("library cache %s: %v", account, err)
			}
			if s.libraryRead {
				saveCache(s)
			}
			return
		}
		applyCacheSnapshot(s, account, snapshot)
		if s.libraryRead {
			saveCache(s)
		}
	})
}

func applyCacheSnapshot(s *State, account string, snapshot librarycache.Snapshot) {
	if !s.libraryRead && len(s.playlists) == 0 {
		s.playlists = snapshot.Playlists
	}
	for uri, listing := range snapshot.Listings {
		if _, alreadyLoaded := s.listings[uri]; !alreadyLoaded && listing.URI == uri && cacheableListing(s, uri, listing) {
			s.listings[uri] = listing
			s.stale[uri] = true // always refresh on startup, even at equal counts
			if uri == player.LikedSongs(account) {
				for _, t := range listing.Tracks {
					if t.URI != "" {
						if _, known := s.liked[t.URI]; known {
							continue // preserve an optimistic or confirmed local membership update
						}
						s.liked[t.URI] = true
					}
				}
			}
		}
	}
	if len(snapshot.Playlists) > 0 {
		s.loaded = true
	}
	libraryChanged(s)
}

func saveCache(s *State) {
	if s.cache == nil || s.username == "" || s.cacheLoading || s.likedWrites > 0 {
		return
	}
	account, cache, generation := s.username, s.cache, s.cacheEpoch
	revision := s.cacheRevision[account] + 1
	s.cacheRevision[account] = revision
	snapshot := librarycache.Snapshot{Playlists: append([]player.Playlist(nil), s.playlists...), Listings: map[string]player.Listing{}}
	for uri, listing := range s.listings {
		if cacheableListing(s, uri, listing) && !virtual(uri) {
			listing.Tracks = append([]player.Track(nil), listing.Tracks...)
			snapshot.Listings[uri] = listing
		}
	}
	accountGo(s, "library-cache-save", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, cache.Save(account, generation, revision, snapshot)
	}, func(s *State, _ struct{}, err error) {
		if err != nil {
			s.logf("library cache save: %v", err)
		}
	})
}
