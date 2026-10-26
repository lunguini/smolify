package app

import (
	"context"
	"strings"
	"time"

	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

const uriRadioPrefix = "spotify:station:track:"

// openBrowse opens an album, artist, playlist, or station by its real Spotify
// URI. The page lives outside the library rail and participates in history.
func openBrowse(s *State, uri, name, cover string) {
	if !validBrowseURI(uri) {
		return
	}
	s.page, s.focus = pageBrowse, paneTracks
	if s.discovery.Pages == nil {
		s.discovery.Pages = make(map[string]discoveryPage)
	}
	p := s.discovery.Pages[uri]
	p.URI = uri
	p.Name = nonEmpty(name, p.Name)
	p.Image = nonEmpty(cover, p.Image)
	switch {
	case strings.HasPrefix(uri, "spotify:artist:"):
		p.Kind = "artist"
	case strings.HasPrefix(uri, "spotify:album:"):
		p.Kind = "album"
	case strings.HasPrefix(uri, uriRadioPrefix):
		p.Kind = "radio"
	default:
		p.Kind = "playlist"
	}
	s.discovery.Pages[uri] = p
	show(s, uri)
	if p.Loading {
		return
	}
	if listing, ok := s.listings[uri]; ok && listing.Complete() && !s.stale[uri] && p.Name != "" {
		fetchCover(s, p.Image)
		return
	}
	switch p.Kind {
	case "artist":
		loadArtistPage(s, uri)
	case "album":
		loadAlbumPage(s, uri)
	default:
		loadBrowseContext(s, uri)
	}
}

// browseSelected exposes the active browse header to the shared playlist frame.
func browseSelected(s *State) (player.Playlist, bool) {
	p, ok := s.discovery.Pages[s.shown]
	if !ok || s.page != pageBrowse {
		return player.Playlist{}, false
	}
	return player.Playlist{URI: p.URI, Name: p.Name, Image: p.Image, Length: s.listings[s.shown].Length}, true
}

func loadBrowseContext(s *State, uri string) {
	p := s.discovery.Pages[uri]
	p.Loading, p.Err = true, ""
	s.discovery.Pages[uri] = p
	be := s.be
	accountLatest(s, "browse-"+uri, func(ctx context.Context) (player.Listing, error) { return be.ContextTracks(ctx, uri) }, func(s *State, listing player.Listing, err error) {
		p := s.discovery.Pages[uri]
		p.Loading = false
		if err != nil {
			p.Err = err.Error()
		} else {
			p.Err = ""
			askLiked(s, listing.Tracks)
		}
		s.discovery.Pages[uri] = p
		if err == nil {
			showDiscoveryListing(s, uri, listing)
		}
	})
}

func loadArtistPage(s *State, uri string) {
	web, ok := s.web.(artistWeb)
	if !ok {
		p := s.discovery.Pages[uri]
		p.Err = "Artist details are unavailable"
		s.discovery.Pages[uri] = p
		return
	}
	p := s.discovery.Pages[uri]
	p.Loading = true
	s.discovery.Pages[uri] = p
	accountLatest(s, "browse-"+uri, func(ctx context.Context) (spotify.ArtistOverview, error) { return web.Artist(ctx, uri) }, func(s *State, value spotify.ArtistOverview, err error) {
		p := s.discovery.Pages[uri]
		p.Loading = false
		if err != nil {
			p.Err = err.Error()
		} else {
			p.Name, p.Image = value.Artist.Name, value.Image
			tracks := make([]player.Track, 0, len(value.TopTracks))
			for _, t := range value.TopTracks {
				tracks = append(tracks, player.Track{URI: t.URI, Name: t.Name, Duration: timeDurationMS(t.DurationMS), Album: t.Album.Name, AlbumURI: t.Album.URI, Artists: artistNames(t.Artists), ArtistURIs: artistURIs(t.Artists), Cover: spotify.Cover(t.Album.Images, 128)})
			}
			showDiscoveryListing(s, uri, player.Listing{URI: uri, Ready: true, Length: len(tracks), Cached: len(tracks), Tracks: tracks})
			p.Err = ""
			askLiked(s, tracks)
		}
		s.discovery.Pages[uri] = p
		fetchCover(s, p.Image)
	})
}

func loadAlbumPage(s *State, uri string) {
	web, ok := s.web.(albumWeb)
	if !ok {
		loadBrowseContext(s, uri)
		return
	}
	p := s.discovery.Pages[uri]
	p.Loading = true
	s.discovery.Pages[uri] = p
	accountLatest(s, "browse-"+uri, func(ctx context.Context) (spotify.AlbumDetails, error) { return web.Album(ctx, uri) }, func(s *State, value spotify.AlbumDetails, err error) {
		p := s.discovery.Pages[uri]
		p.Loading = false
		if err != nil {
			p.Err = err.Error()
		} else {
			p.Album = &value
			p.Name, p.Image = value.Album.Name, spotify.Cover(value.Album.Images, 256)
			tracks := make([]player.Track, 0, len(value.Tracks))
			for _, t := range value.Tracks {
				tracks = append(tracks, player.Track{URI: t.URI, Name: t.Name, Duration: timeDurationMS(t.DurationMS), Album: value.Album.Name, AlbumURI: uri, Artists: artistNames(t.Artists), ArtistURIs: artistURIs(t.Artists), Cover: p.Image})
			}
			showDiscoveryListing(s, uri, player.Listing{URI: uri, Ready: true, Length: value.Total, Cached: len(tracks), Tracks: tracks})
			p.Err = ""
			askLiked(s, tracks)
		}
		s.discovery.Pages[uri] = p
		fetchCover(s, p.Image)
	})
}

func showDiscoveryListing(s *State, uri string, listing player.Listing) {
	replaceListing(s, uri, listing)
	delete(s.stale, uri)
	saveCache(s)
}

func tickBrowse(s *State) {
	p, ok := s.discovery.Pages[s.shown]
	if !ok || p.Loading || p.Err != "" || p.Kind == "artist" {
		return
	}
	if l := s.listings[s.shown]; !l.Complete() || s.stale[s.shown] {
		loadBrowseContext(s, s.shown)
	}
}

func retryBrowse(s *State) {
	p := s.discovery.Pages[s.shown]
	if p.Loading {
		return
	}
	p.Err = ""
	s.discovery.Pages[s.shown] = p
	switch p.Kind {
	case "artist":
		loadArtistPage(s, s.shown)
	case "album":
		loadAlbumPage(s, s.shown)
	default:
		loadBrowseContext(s, s.shown)
	}
}

// songRadioURI turns only a valid track URI into Spotify's supported station
// context URI. go-librespot resolves station contexts and pages them lazily.
func songRadioURI(trackURI string) string {
	if !strings.HasPrefix(trackURI, "spotify:track:") || strings.TrimPrefix(trackURI, "spotify:track:") == "" {
		return ""
	}
	return uriRadioPrefix + strings.TrimPrefix(trackURI, "spotify:track:")
}

func discoveryCommand(s *State, id string) bool {
	if id != "song-radio" && id != "open-track-album" && id != "open-track-artist" {
		return false
	}
	t, ok := s.selectedTrack()
	if !ok {
		return true
	}
	if id == "open-track-album" {
		if t.AlbumURI == "" {
			return true
		}
		visit(s)
		openBrowse(s, t.AlbumURI, t.Album, t.Cover)
		return true
	}
	if id == "open-track-artist" {
		if len(t.ArtistURIs) == 0 {
			return true
		}
		name := ""
		if len(t.Artists) > 0 {
			name = t.Artists[0]
		}
		visit(s)
		openBrowse(s, t.ArtistURIs[0], name, "")
		return true
	}
	uri := songRadioURI(t.URI)
	if uri == "" {
		fail(s, "Could not start Song Radio: invalid track")
		return true
	}
	visit(s)
	openBrowse(s, uri, t.Name+" Radio", t.Cover)
	requestPlay(s, uri, "", player.Track{})
	return true
}

func discoveryClick(s *State, uri string) bool {
	uri = strings.TrimPrefix(uri, "browse:")
	uri, _, _ = strings.Cut(uri, "#")
	if !validBrowseURI(uri) {
		return false
	}
	var name, cover string
	for _, track := range s.listings[s.shown].Tracks {
		if track.AlbumURI == uri {
			name, cover = track.Album, track.Cover
			break
		}
		for i, artist := range track.ArtistURIs {
			if artist == uri && i < len(track.Artists) {
				name = track.Artists[i]
				break
			}
		}
	}
	visit(s)
	openBrowse(s, uri, name, cover)
	return true
}

func validBrowseURI(uri string) bool {
	for _, prefix := range []string{"spotify:album:", "spotify:artist:", "spotify:playlist:", uriRadioPrefix} {
		if rest, ok := strings.CutPrefix(uri, prefix); ok && strings.TrimSpace(rest) != "" {
			return true
		}
	}
	return false
}

func artistNames(in []spotify.Artist) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, a.Name)
	}
	return out
}
func artistURIs(in []spotify.Artist) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, a.URI)
	}
	return out
}
func timeDurationMS(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }

// playbackSource names the confirmed playback context, independently of the
// page currently being browsed. Never infer a playlist from the album alone.
func playbackSource(s *State) (player.Playlist, string) {
	if s.track == nil {
		return player.Playlist{}, "Playlist"
	}
	uri := nonEmpty(s.context, s.track.ContextURI)
	if uri == "" {
		return player.Playlist{}, "Playlist"
	}
	for _, list := range s.library() {
		if list.URI == uri {
			return list, "Playlist"
		}
	}
	if !validBrowseURI(uri) {
		return player.Playlist{}, "Playlist"
	}
	label := "Playlist"
	if strings.HasPrefix(uri, "spotify:album:") {
		label = "Album"
	}
	if strings.HasPrefix(uri, "spotify:artist:") {
		label = "Artist"
	}
	if strings.HasPrefix(uri, uriRadioPrefix) {
		label = "Radio"
	}
	source := player.Playlist{URI: uri}
	if page, ok := s.discovery.Pages[uri]; ok {
		source.Name, source.Image = page.Name, page.Image
	}
	if uri == s.track.AlbumURI {
		source.Name, source.Image = nonEmpty(source.Name, s.track.Album), nonEmpty(source.Image, s.track.Cover)
	}
	return source, label
}

func showPlaybackSource(s *State) {
	source, _ := playbackSource(s)
	if source.URI == "" {
		return
	}
	if s.shown != source.URI || (s.page != pageLibrary && s.page != pageBrowse) {
		visit(s)
	}
	s.trackFilter.Value = ""
	s.trackFilter.SetCursor(0)
	s.trackFilterOpen = false
	found := false
	for i, list := range s.library() {
		if list.URI == source.URI {
			s.page, s.focus = pageLibrary, paneTracks
			s.rail.Select(i)
			showSelected(s)
			found = true
			break
		}
	}
	if !found {
		openBrowse(s, source.URI, source.Name, source.Image)
	}
	if s.track != nil {
		listing := displayListing(s, s.listings[source.URI])
		s.tracks.SetCount(len(listing.Tracks))
		if index := indexOf(listing.Tracks, s.track.URI); index >= 0 {
			s.tracks.Select(index)
		}
	}
}
