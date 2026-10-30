// Package demo provides self-contained sample data and simulated playback
// for the browser build. Its artwork is generated locally.
package demo

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lunguini/smolify/internal/catalog"
	"github.com/lunguini/smolify/internal/model"
)

const demoUser = "demo"

type Backend struct {
	mu         sync.Mutex
	events     chan model.Event
	playlists  []model.Playlist
	listings   map[string]model.Listing
	tracks     map[string]model.Track
	allTracks  []model.Track
	liked      map[string]time.Time
	queue      []string
	status     model.Status
	shuffle    bool
	repeat     model.Repeat
	lastPlayed []model.Played
}

func New() *Backend {
	b := &Backend{
		events:   make(chan model.Event, 128),
		listings: map[string]model.Listing{}, tracks: map[string]model.Track{},
		liked: map[string]time.Time{}, status: model.Status{Username: demoUser, Stopped: true, Paused: true, Volume: 40, VolumeSteps: 100},
	}
	b.buildLibrary()
	b.emit(model.Ready{})
	return b
}

func (b *Backend) buildLibrary() {
	tracks := []model.Track{
		{URI: "spotify:track:demo-01", Name: "Blue Static", Artists: []string{"Nova Vale"}, ArtistURIs: []string{"spotify:artist:demo-nova"}, Album: "Signals at Dusk", AlbumURI: "spotify:album:demo-signals", Cover: "demo:cover:signals", Duration: 214 * time.Second, TrackNumber: 1, Disc: 1},
		{URI: "spotify:track:demo-02", Name: "Slow Orbit", Artists: []string{"Nova Vale", "Iris North"}, ArtistURIs: []string{"spotify:artist:demo-nova", "spotify:artist:demo-iris"}, Album: "Signals at Dusk", AlbumURI: "spotify:album:demo-signals", Cover: "demo:cover:signals", Duration: 197 * time.Second, TrackNumber: 2, Disc: 1},
		{URI: "spotify:track:demo-03", Name: "Glass Bridges", Artists: []string{"Iris North"}, ArtistURIs: []string{"spotify:artist:demo-iris"}, Album: "Signals at Dusk", AlbumURI: "spotify:album:demo-signals", Cover: "demo:cover:signals", Duration: 243 * time.Second, TrackNumber: 3, Disc: 1},
		{URI: "spotify:track:demo-04", Name: "Small Constellations", Artists: []string{"Milo Fern"}, ArtistURIs: []string{"spotify:artist:demo-milo"}, Album: "Paper Planets", AlbumURI: "spotify:album:demo-paper", Cover: "demo:cover:paper", Duration: 188 * time.Second, TrackNumber: 1, Disc: 1},
		{URI: "spotify:track:demo-05", Name: "A Window Open", Artists: []string{"Milo Fern"}, ArtistURIs: []string{"spotify:artist:demo-milo"}, Album: "Paper Planets", AlbumURI: "spotify:album:demo-paper", Cover: "demo:cover:paper", Duration: 226 * time.Second, TrackNumber: 2, Disc: 1},
		{URI: "spotify:track:demo-06", Name: "Soft Landing", Artists: []string{"June Arcade"}, ArtistURIs: []string{"spotify:artist:demo-june"}, Album: "After the Rain", AlbumURI: "spotify:album:demo-rain", Cover: "demo:cover:rain", Duration: 203 * time.Second, TrackNumber: 1, Disc: 1},
		{URI: "spotify:track:demo-07", Name: "Quiet Machines", Artists: []string{"June Arcade", "Nova Vale"}, ArtistURIs: []string{"spotify:artist:demo-june", "spotify:artist:demo-nova"}, Album: "After the Rain", AlbumURI: "spotify:album:demo-rain", Cover: "demo:cover:rain", Duration: 239 * time.Second, TrackNumber: 2, Disc: 1},
	}
	b.allTracks = tracks
	for _, track := range tracks {
		b.tracks[track.URI] = track
	}
	b.playlists = []model.Playlist{
		{URI: "spotify:playlist:demo-night", Name: "Evening Circuit", Description: "Slow lights and steady rhythms", Owner: "Demo", Image: "demo:cover:signals", Length: 4},
		{URI: "spotify:playlist:demo-paper", Name: "Paper Planets", Description: "Small songs for long walks", Owner: "Demo", Image: "demo:cover:paper", Length: 3},
		{URI: "spotify:playlist:demo-rain", Name: "After the Rain", Description: "A softer kind of momentum", Owner: "Demo", Image: "demo:cover:rain", Length: 4},
	}
	b.listings[b.playlists[0].URI] = makeListing(b.playlists[0].URI, []model.Track{tracks[0], tracks[1], tracks[5], tracks[6]})
	b.listings[b.playlists[1].URI] = makeListing(b.playlists[1].URI, []model.Track{tracks[3], tracks[4], tracks[2]})
	b.listings[b.playlists[2].URI] = makeListing(b.playlists[2].URI, []model.Track{tracks[5], tracks[6], tracks[1], tracks[3]})
	b.liked[tracks[1].URI] = time.Now().Add(-48 * time.Hour)
	b.liked[tracks[4].URI] = time.Now().Add(-24 * time.Hour)
}

func makeListing(uri string, tracks []model.Track) model.Listing {
	return model.Listing{URI: uri, Ready: true, Length: len(tracks), Cached: len(tracks), Tracks: append([]model.Track(nil), tracks...)}
}

func (b *Backend) emit(event model.Event) {
	select {
	case b.events <- event:
	default:
	}
}

func (b *Backend) Events() <-chan model.Event { return b.events }

// Run emits a seek event each second while simulated playback is active.
func (b *Backend) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.Advance(time.Second)
		}
	}
}

// Advance moves simulated playback by elapsed. It is also exposed so tests
// can drive the demo clock without sleeping.
func (b *Backend) Advance(elapsed time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.Track == nil || b.status.Paused || b.status.Stopped || elapsed <= 0 {
		return
	}
	position := b.status.Track.Position + elapsed
	if position >= b.status.Track.Duration {
		position = b.status.Track.Duration
		b.status.Track.Position = position
		b.emit(model.Seek{Position: position, Duration: b.status.Track.Duration})
		b.nextLocked()
		return
	}
	b.status.Track.Position = position
	b.emit(model.Seek{Position: position, Duration: b.status.Track.Duration})
}

func (b *Backend) Status(context.Context) (model.Status, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	status := b.status
	if status.Track != nil {
		track := *status.Track
		status.Track = &track
	}
	status.Shuffle, status.Repeat = b.shuffle, b.repeat
	return status, nil
}

func (b *Backend) Playlists(context.Context) ([]model.Playlist, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]model.Playlist(nil), b.playlists...), nil
}

func (b *Backend) ContextTracks(_ context.Context, uri string) (model.Listing, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if uri == model.LikedSongs(demoUser) {
		tracks := b.likedTracksLocked()
		return makeListing(uri, tracks), nil
	}
	if listing, ok := b.listings[uri]; ok {
		listing.Tracks = append([]model.Track(nil), listing.Tracks...)
		return listing, nil
	}
	if strings.HasPrefix(uri, "spotify:album:") {
		var tracks []model.Track
		for _, track := range b.allTracks {
			if track.AlbumURI == uri {
				tracks = append(tracks, track)
			}
		}
		return makeListing(uri, tracks), nil
	}
	if strings.HasPrefix(uri, "spotify:station:track:") {
		trackURI := strings.TrimPrefix(uri, "spotify:station:track:")
		if !strings.HasPrefix(trackURI, "spotify:track:") {
			trackURI = "spotify:track:" + trackURI
		}
		if track, ok := b.tracks[trackURI]; ok {
			return makeListing(uri, []model.Track{track}), nil
		}
	}
	return model.Listing{URI: uri, Ready: true}, nil
}

func (b *Backend) Play(_ context.Context, uri, skipTo string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var track model.Track
	contextURI := uri
	if strings.HasPrefix(uri, "spotify:track:") {
		var ok bool
		track, ok = b.tracks[uri]
		if !ok {
			return errors.New("demo track not found")
		}
		contextURI = track.ContextURI
	} else {
		listing, ok := b.listings[uri]
		if uri == model.LikedSongs(demoUser) {
			listing, ok = makeListing(uri, b.likedTracksLocked()), true
		}
		if strings.HasPrefix(uri, "spotify:album:") {
			var albumTracks []model.Track
			for _, candidate := range b.allTracks {
				if candidate.AlbumURI == uri {
					albumTracks = append(albumTracks, candidate)
				}
			}
			listing, ok = makeListing(uri, albumTracks), len(albumTracks) > 0
		}
		if !ok || len(listing.Tracks) == 0 {
			return errors.New("demo playlist has no tracks")
		}
		track = listing.Tracks[0]
		if skipTo != "" {
			for _, candidate := range listing.Tracks {
				if candidate.URI == skipTo {
					track = candidate
					break
				}
			}
		}
	}
	b.startLocked(track, contextURI)
	return nil
}

func (b *Backend) startLocked(track model.Track, contextURI string) {
	track.Position, track.ContextURI = 0, contextURI
	b.status.Track, b.status.ContextURI = &track, contextURI
	b.status.Paused, b.status.Stopped = false, false
	b.emit(model.WillPlay{URI: track.URI, ContextURI: contextURI})
	b.emit(track)
	b.emit(model.Seek{Position: 0, Duration: track.Duration})
	b.emit(model.Playing{URI: track.URI, ContextURI: contextURI})
	b.lastPlayed = append([]model.Played{{ContextURI: contextURI, TrackURI: track.URI, At: time.Now()}}, b.lastPlayed...)
}

func (b *Backend) PlayPause(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.Track == nil {
		if len(b.playlists) == 0 {
			return errors.New("demo library is empty")
		}
		listing := b.listings[b.playlists[0].URI]
		b.startLocked(listing.Tracks[0], b.playlists[0].URI)
		return nil
	}
	b.status.Paused = !b.status.Paused
	if b.status.Paused {
		b.emit(model.Paused{URI: b.status.Track.URI, ContextURI: b.status.ContextURI})
	} else {
		b.emit(model.Playing{URI: b.status.Track.URI, ContextURI: b.status.ContextURI})
	}
	return nil
}

func (b *Backend) Next(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextLocked()
	return nil
}

func (b *Backend) nextLocked() {
	if len(b.queue) > 0 {
		uri := b.queue[0]
		b.queue = b.queue[1:]
		if track, ok := b.tracks[uri]; ok {
			b.startLocked(track, b.status.ContextURI)
			return
		}
	}
	if b.status.Track == nil {
		if len(b.playlists) > 0 {
			listing := b.listings[b.playlists[0].URI]
			b.startLocked(listing.Tracks[0], b.playlists[0].URI)
		}
		return
	}
	if b.repeat.Track {
		b.startLocked(*b.status.Track, b.status.ContextURI)
		return
	}
	listing, err := b.contextListingLocked(b.status.ContextURI)
	if err != nil || len(listing.Tracks) == 0 {
		b.status.Stopped, b.status.Paused = true, true
		b.emit(model.Stopped{})
		return
	}
	index := 0
	for i, track := range listing.Tracks {
		if track.URI == b.status.Track.URI {
			index = i + 1
			break
		}
	}
	if b.shuffle && len(listing.Tracks) > 1 {
		index = (index + 2) % len(listing.Tracks)
	}
	if index >= len(listing.Tracks) {
		if !b.repeat.Context {
			b.status.Stopped, b.status.Paused = true, true
			b.emit(model.Stopped{})
			return
		}
		index = 0
	}
	b.startLocked(listing.Tracks[index], b.status.ContextURI)
}

func (b *Backend) Prev(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.Track == nil {
		return nil
	}
	if b.status.Track.Position > 3*time.Second {
		b.status.Track.Position = 0
		b.emit(model.Seek{Position: 0, Duration: b.status.Track.Duration})
		return nil
	}
	listing, err := b.contextListingLocked(b.status.ContextURI)
	if err != nil || len(listing.Tracks) == 0 {
		return nil
	}
	index := 0
	for i, track := range listing.Tracks {
		if track.URI == b.status.Track.URI {
			index = i - 1
			break
		}
	}
	if index < 0 {
		if b.repeat.Context {
			index = len(listing.Tracks) - 1
		} else {
			index = 0
		}
	}
	b.startLocked(listing.Tracks[index], b.status.ContextURI)
	return nil
}

func (b *Backend) Seek(_ context.Context, position time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status.Track == nil {
		return nil
	}
	position = min(max(position, 0), b.status.Track.Duration)
	b.status.Track.Position = position
	b.emit(model.Seek{Position: position, Duration: b.status.Track.Duration})
	return nil
}

func (b *Backend) SetVolume(_ context.Context, volume int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status.Volume = min(max(volume, 0), 100)
	b.emit(model.Volume{Value: b.status.Volume, Max: 100})
	return nil
}

func (b *Backend) SetShuffle(_ context.Context, enabled bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.shuffle, b.status.Shuffle = enabled, enabled
	b.emit(model.Shuffle{On: enabled})
	return nil
}

func (b *Backend) SetRepeat(_ context.Context, repeat model.Repeat) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.repeat, b.status.Repeat = repeat, repeat
	b.emit(repeat)
	return nil
}

func (b *Backend) Queue(_ context.Context, uri string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.tracks[uri]; !ok {
		return errors.New("demo track not found")
	}
	b.queue = append(b.queue, uri)
	return nil
}

func (b *Backend) SetLiked(_ context.Context, uris []string, liked bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, uri := range uris {
		if liked {
			b.liked[uri] = time.Now()
		} else {
			delete(b.liked, uri)
		}
	}
	return nil
}

func (b *Backend) Liked(_ context.Context, uris []string) ([]bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]bool, len(uris))
	for i, uri := range uris {
		_, out[i] = b.liked[uri]
	}
	return out, nil
}

func (b *Backend) Configure(context.Context, model.Audio) error { return nil }
func (b *Backend) SetDeviceName(context.Context, string) error  { return nil }
func (b *Backend) SignOut(context.Context) error                { return nil }
func (b *Backend) SupportsRadio() bool                          { return true }
func (b *Backend) SupportsSignOut() bool                        { return false }

func (b *Backend) RecentlyPlayed(_ context.Context, username string, limit int) ([]model.Played, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if username != demoUser {
		return nil, nil
	}
	return append([]model.Played(nil), b.lastPlayed[:min(len(b.lastPlayed), max(limit, 0))]...), nil
}

func (b *Backend) contextListingLocked(uri string) (model.Listing, error) {
	if uri == model.LikedSongs(demoUser) {
		return makeListing(uri, b.likedTracksLocked()), nil
	}
	listing, ok := b.listings[uri]
	if !ok {
		return model.Listing{}, errors.New("demo context not found")
	}
	return listing, nil
}

func (b *Backend) likedTracksLocked() []model.Track {
	var tracks []model.Track
	for _, track := range b.allTracks {
		if _, ok := b.liked[track.URI]; ok {
			track.AddedAt = b.liked[track.URI]
			tracks = append(tracks, track)
		}
	}
	sort.SliceStable(tracks, func(i, j int) bool { return tracks[i].AddedAt.After(tracks[j].AddedAt) })
	return tracks
}

func (b *Backend) Search(_ context.Context, query string, limit int) (catalog.Results, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	query = strings.ToLower(strings.TrimSpace(query))
	var result catalog.Results
	seenAlbums, seenArtists, seenPlaylists := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, track := range b.allTracks {
		if !contains(query, track.Name, strings.Join(track.Artists, " "), track.Album) {
			continue
		}
		if len(result.Tracks) < limit {
			result.Tracks = append(result.Tracks, catalogTrack(track))
		}
		if !seenAlbums[track.AlbumURI] && len(result.Albums) < limit {
			result.Albums = append(result.Albums, catalogAlbumFromTrack(track))
			seenAlbums[track.AlbumURI] = true
		}
		for i, artist := range track.Artists {
			uri := ""
			if i < len(track.ArtistURIs) {
				uri = track.ArtistURIs[i]
			}
			if !seenArtists[uri] && len(result.Artists) < limit {
				result.Artists = append(result.Artists, catalog.Artist{URI: uri, Name: artist})
				seenArtists[uri] = true
			}
		}
	}
	for _, playlist := range b.playlists {
		if contains(query, playlist.Name, playlist.Description) && !seenPlaylists[playlist.URI] && len(result.Playlists) < limit {
			result.Playlists = append(result.Playlists, catalogPlaylistFromModel(playlist))
			seenPlaylists[playlist.URI] = true
		}
	}
	return result, nil
}

func (b *Backend) LikedTracks(_ context.Context, offset, limit int) (catalog.SavedTracksPage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	tracks := b.likedTracksLocked()
	page := catalog.SavedTracksPage{Total: len(tracks), Offset: offset, Limit: limit}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(tracks) || limit <= 0 {
		return page, nil
	}
	end := min(offset+limit, len(tracks))
	for _, track := range tracks[offset:end] {
		page.Items = append(page.Items, catalog.SavedTrack{Track: catalogTrack(track), AddedAt: track.AddedAt})
	}
	return page, nil
}

func (b *Backend) Home(context.Context) (catalog.HomeFeed, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	section := catalog.HomeSection{Title: "Made for this demo"}
	for _, playlist := range b.playlists {
		section.Items = append(section.Items, catalog.HomeItem{URI: playlist.URI, Name: playlist.Name, Kind: "playlist", Image: playlist.Image})
	}
	return catalog.HomeFeed{Sections: []catalog.HomeSection{section}}, nil
}

func (b *Backend) Album(_ context.Context, uri string) (catalog.AlbumDetails, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var tracks []catalog.Track
	var album catalog.Album
	for _, track := range b.allTracks {
		if track.AlbumURI == uri {
			if album.URI == "" {
				album = catalogAlbumFromTrack(track)
			}
			tracks = append(tracks, catalogTrack(track))
		}
	}
	if album.URI == "" {
		return catalog.AlbumDetails{}, errors.New("demo album not found")
	}
	return catalog.AlbumDetails{Album: album, Tracks: tracks, Total: len(tracks)}, nil
}

func (b *Backend) Artist(_ context.Context, uri string) (catalog.ArtistOverview, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var overview catalog.ArtistOverview
	for _, track := range b.allTracks {
		for i, name := range track.Artists {
			if i < len(track.ArtistURIs) && track.ArtistURIs[i] == uri {
				overview.Artist = catalog.Artist{URI: uri, Name: name}
				overview.Image = "demo:cover:" + strings.TrimPrefix(uri, "spotify:artist:demo-")
				overview.TopTracks = append(overview.TopTracks, catalogTrack(track))
			}
		}
	}
	if overview.Artist.URI == "" {
		return catalog.ArtistOverview{}, errors.New("demo artist not found")
	}
	return overview, nil
}

func (b *Backend) Fetch(ctx context.Context, url string) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Cover(url), nil
}

// Cover generates abstract, deterministic artwork for demo records.
func Cover(key string) image.Image {
	const size = 320
	base := palette(key)
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size) * 0.5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-center, float64(y)-center
			d := math.Sqrt(dx*dx+dy*dy) / center
			wave := math.Sin(math.Sqrt(dx*dx+dy*dy)*0.105 + math.Atan2(dy, dx)*3)
			glow := math.Max(0, 1-d)
			factor := 0.45 + 0.28*glow + 0.12*wave
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(clamp(float64(base.R)*factor + float64(base.G)*glow*0.08)),
				G: uint8(clamp(float64(base.G)*factor + float64(base.B)*glow*0.1)),
				B: uint8(clamp(float64(base.B)*factor + float64(base.R)*glow*0.12)), A: 255,
			})
		}
	}
	return img
}

func palette(key string) color.RGBA {
	var sum uint32
	for _, c := range key {
		sum = sum*33 + uint32(c)
	}
	return color.RGBA{R: uint8(65 + sum%90), G: uint8(50 + (sum>>5)%95), B: uint8(110 + (sum>>11)%110), A: 255}
}

func clamp(value float64) float64 { return math.Min(math.Max(value, 0), 255) }

func catalogTrack(track model.Track) catalog.Track {
	result := catalog.Track{URI: track.URI, Name: track.Name, DurationMS: int(track.Duration.Milliseconds())}
	for i, name := range track.Artists {
		artist := catalog.Artist{Name: name}
		if i < len(track.ArtistURIs) {
			artist.URI = track.ArtistURIs[i]
		}
		result.Artists = append(result.Artists, artist)
	}
	result.Album = catalogAlbumFromTrack(track)
	return result
}

func catalogAlbumFromTrack(track model.Track) catalog.Album {
	return catalog.Album{URI: track.AlbumURI, Name: track.Album, Images: []catalog.Image{{URL: track.Cover, Width: 320, Height: 320}}}
}

func catalogPlaylistFromModel(playlist model.Playlist) catalog.Playlist {
	var result catalog.Playlist
	result.URI, result.Name, result.Description = playlist.URI, playlist.Name, playlist.Description
	result.Images = []catalog.Image{{URL: playlist.Image, Width: 320, Height: 320}}
	result.Owner.DisplayName = playlist.Owner
	return result
}

func contains(query string, values ...string) bool {
	if query == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
