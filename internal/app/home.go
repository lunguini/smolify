package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spotify"
)

// Discovery clients are optional so apps and tests that only implement Search
// keep working without fake Home or catalog data.
type homeWeb interface {
	Home(context.Context) (spotify.HomeFeed, error)
}
type artistWeb interface {
	Artist(context.Context, string) (spotify.ArtistOverview, error)
}
type albumWeb interface {
	Album(context.Context, string) (spotify.AlbumDetails, error)
}

type discoveryState struct {
	Home            spotify.HomeFeed
	HomeRequested   bool
	HomeLoading     bool
	HomeError       string
	HomeFallback    bool
	Pages           map[string]discoveryPage
	RecentRequested bool
	HomeScroll      int
	HomeCursor      int
	HomeRects       map[string]layout.Rect
	HomeMax         int
	HomeWidthPoints float64
}

type discoveryPage struct {
	URI, Name, Image, Kind string
	Artist                 *spotify.ArtistOverview
	Album                  *spotify.AlbumDetails
	Loading                bool
	Err                    string
}

// loadHomeFeed fetches one bounded, account-scoped Spotify Home response per
// account lifetime; only an explicit Retry bypasses the request guard.
func loadHomeFeed(s *State, retry ...bool) {
	force := len(retry) > 0 && retry[0]
	if s.discovery.HomeRequested && !force {
		return
	}
	s.discovery.HomeRequested = true
	web, ok := s.web.(homeWeb)
	if !ok {
		showHomePlaylistFallback(s, "Spotify Home is unavailable")
		return
	}
	if s.discovery.HomeLoading {
		return
	}
	s.discovery.HomeLoading, s.discovery.HomeError = true, ""
	accountLatest(s, "discovery-home", web.Home, func(s *State, feed spotify.HomeFeed, err error) {
		s.discovery.HomeLoading = false
		if err != nil {
			showHomePlaylistFallback(s, err.Error())
			return
		}
		s.discovery.Home, s.discovery.HomeError, s.discovery.HomeFallback = feed, "", false
	})
}

func showHomePlaylistFallback(s *State, reason string) {
	if len(s.discovery.Home.Sections) > 0 {
		s.discovery.HomeError = reason
		return
	}
	playlists := homePlaylistFallback(s.playlists)
	if len(playlists) == 0 {
		s.discovery.HomeError = reason
		return
	}
	section := spotify.HomeSection{Title: "Made for you"}
	for _, p := range playlists {
		section.Items = append(section.Items, spotify.HomeItem{URI: p.URI, Name: p.Name, Kind: "playlist", Image: p.Image})
	}
	s.discovery.Home = spotify.HomeFeed{Sections: []spotify.HomeSection{section}}
	s.discovery.HomeError = reason
	s.discovery.HomeFallback = true
}

// homePlaylistFallback finds only the account's actual mix-like playlists.
// It is used when private Home is unavailable and never fabricates playlist IDs.
func homePlaylistFallback(playlists []player.Playlist) []player.Playlist {
	needles := []string{"daily mix", "discover weekly", "release radar", "made for you"}
	var out []player.Playlist
	seen := make(map[string]bool)
	for _, p := range playlists {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		match := false
		for _, n := range needles {
			if strings.Contains(name, n) {
				match = true
				break
			}
		}
		if match && p.URI != "" && !seen[p.URI] {
			out = append(out, p)
			seen[p.URI] = true
		}
	}
	return out
}

const (
	idHomeScroll  = "home-scroll"
	idHomeContent = "home-scroll.content"
)

// homeSections keeps recent songs available alongside Spotify's own shelves.
func homeSections(s *State) []spotify.HomeSection {
	sections := make([]spotify.HomeSection, 0, len(s.discovery.Home.Sections)+1)
	recent := spotify.HomeSection{Title: "Recently played"}
	for _, track := range s.listings[uriHome].Tracks {
		item := spotify.HomeItem{URI: track.URI, Name: track.Name, Kind: "track", Image: track.Cover}
		for i, name := range track.Artists {
			artist := spotify.Artist{Name: name}
			if i < len(track.ArtistURIs) {
				artist.URI = track.ArtistURIs[i]
			}
			item.Artists = append(item.Artists, artist)
		}
		if item.URI != "" {
			recent.Items = append(recent.Items, item)
		}
	}
	sections = append(sections, s.discovery.Home.Sections...)
	if len(recent.Items) > 0 {
		sections = append(sections, recent)
	}
	return sections
}

func homeItems(s *State) []spotify.HomeItem {
	var items []spotify.HomeItem
	for _, section := range homeSections(s) {
		items = append(items, section.Items...)
	}
	return items
}

func homeCardID(index int) string { return "home-card:" + strconv.Itoa(index) }

// homeView draws bounded shelves inside the center pane. IDs identify card
// occurrences, since Spotify can recommend the same album in several shelves.
func homeView(s *State, g bool, width int) layout.Node {
	perRow, _, gap, pad, coverSize := homeGridGeometry(s, g, width)
	body := []layout.Node{layout.Label{Text: "Home", Style: layout.TextStyle{Bold: true, Size: float64(pick(g, 30, 0)), Fg: white}}}
	if s.discovery.Home.Greeting != "" {
		body = append(body, layout.Label{Text: s.discovery.Home.Greeting, Style: subtle})
	}
	if s.discovery.HomeLoading {
		body = append(body, layout.Label{Text: "Loading your Spotify recommendations…", Style: subtle})
	}
	if s.discovery.HomeError != "" {
		message := "Could not refresh personalized Home."
		if s.discovery.HomeFallback {
			message = "Personalized Home is unavailable; showing your saved mixes."
		}
		body = append(body, layout.Row{NodeBase: layout.NodeBase{NoPadding: true, Gap: 1}, Children: []layout.Node{
			layout.Label{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Text: message, Wrap: true, Style: subtle},
			layout.Button{NodeBase: layout.NodeBase{ID: "home-retry"}, Label: "Retry", Disabled: s.discovery.HomeLoading},
		}})
	}
	sections := homeSections(s)
	index := 0
	for _, section := range sections {
		body = append(body, layout.Label{NodeBase: layout.NodeBase{PadTop: 1}, Text: section.Title, Style: strong})
		for start := 0; start < len(section.Items); start += perRow {
			end := min(start+perRow, len(section.Items))
			cards := make([]layout.Node, 0, end-start)
			for _, item := range section.Items[start:end] {
				detail := item.Kind
				if len(item.Artists) > 0 {
					detail = item.Artists[0].Name
				}

				nb := layout.NodeBase{ID: homeCardID(index), W: layout.Grow(1), Pad: pad, Tooltip: item.Name,
					Box: layout.Box{Interactive: true, Round: true, Selected: s.focus == paneTracks && index == s.discovery.HomeCursor}}
				if nb.Box.Selected {
					nb.Box.Background = playingBg
				}
				cards = append(cards, card(nb, homeArtwork(s, item.Image, coverSize),
					layout.Label{Text: item.Name, Truncate: true, Style: strong}, layout.Label{Text: detail, Truncate: true, Style: secondary}))
				index++
			}
			for len(cards) < perRow {
				cards = append(cards, layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), NoPadding: true}})
			}
			body = append(body, layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), NoPadding: true, Gap: gap}, Children: cards})
		}
	}
	if len(sections) == 0 && !s.discovery.HomeLoading && s.discovery.HomeError == "" {
		body = append(body, layout.Label{Text: "Your recommendations and recently played songs will appear here.", Wrap: true, Style: subtle})
	}
	return layout.Scroll{NodeBase: layout.NodeBase{ID: idHomeScroll, W: layout.Grow(1), H: layout.Grow(1), Pad: pick(g, 2, 1)}, OffsetY: s.discovery.HomeScroll,
		Child: layout.Col{NodeBase: layout.NodeBase{ID: idHomeContent, W: layout.Grow(1)}, Children: body}}
}

// homeGridGeometry sizes a real card grid from the center pane's measured
// width. In a GUI, PointRects supplies native points; in a terminal the
// scroll content's solved rectangle supplies columns. The fallback is used
// only on the first frame before LayoutEvent arrives.
func homeGridGeometry(s *State, graphical bool, frameWidth int) (columns, cardWidth, gap, pad, coverSize int) {
	if graphical {
		width := s.discovery.HomeWidthPoints
		if width <= 0 {
			width = float64(max(12, frameWidth-railWidth-8) * layout.PointsPerColumn)
		}
		gap, pad = 3, 3 // 12pt gutters and insets at the default 4pt step.
		const minCard, maxCard = 150, 210
		cols := max(1, int((width+12)/float64(minCard+12)))
		for cols > 1 && (width-float64((cols-1)*12))/float64(cols) < minCard {
			cols--
		}
		cw := min(maxCard, int((width-float64((cols-1)*12))/float64(cols)))
		const defaultStepPoints = 4
		return cols, cw, gap, pad, max(1, cw-2*pad*defaultStepPoints)
	}
	width := s.discovery.HomeRects[idHomeContent].W
	if width <= 0 {
		width = max(12, frameWidth-railWidth-8)
	}
	gap, pad = 1, 1
	const minCard = 18
	cols := max(1, (width+gap)/(minCard+gap))
	cw := max(minCard, (width-gap*(cols-1))/cols)
	cover := max(1, (cw-2*pad)*layout.PointsPerColumn)
	return cols, cw, gap, pad, cover
}

// fetchVisibleHomeCovers starts requests only for cards in the visible Home
// viewport. Call after adopting LayoutEvent.Rects; off-screen shelves remain
// cheap until the user scrolls them into view.
func fetchVisibleHomeCovers(s *State) {
	view, ok := s.discovery.HomeRects[idHomeScroll]
	if !ok {
		return
	}
	for i, item := range homeItems(s) {
		r, ok := s.discovery.HomeRects[homeCardID(i)]
		if ok && r.Y+r.H > view.Y && r.Y < view.Y+view.H {
			fetchCover(s, item.Image)
		}
	}
}

func clampHomeScroll(s *State) {
	view, okV := s.discovery.HomeRects[idHomeScroll]
	content, okC := s.discovery.HomeRects[idHomeContent]
	if !okV || !okC {
		return
	}
	s.discovery.HomeMax = max(content.H-view.H, 0)
	s.discovery.HomeScroll = layout.ClampScroll(s.discovery.HomeScroll, view.H, content.H)
}

func scrollHome(s *State, delta int) {
	s.discovery.HomeScroll = min(max(s.discovery.HomeScroll+delta, 0), s.discovery.HomeMax)
}

func revealHomeCard(s *State) {
	view, ok := s.discovery.HomeRects[idHomeScroll]
	if !ok {
		return
	}
	r, ok := s.discovery.HomeRects[homeCardID(s.discovery.HomeCursor)]
	if !ok {
		return
	}
	if r.Y < view.Y {
		scrollHome(s, r.Y-view.Y)
	} else if r.Y+r.H > view.Y+view.H {
		scrollHome(s, r.Y+r.H-view.Y-view.H)
	}
}

func openHomeItem(s *State, index int) {
	items := homeItems(s)
	if index < 0 || index >= len(items) {
		return
	}
	s.discovery.HomeCursor = index
	item := items[index]
	if item.Kind == "track" {
		for _, track := range s.listings[uriHome].Tracks {
			if track.URI == item.URI {
				context := nonEmpty(track.ContextURI, track.AlbumURI)
				skip := track.URI
				if context == "" {
					context, skip = track.URI, ""
				}
				requestPlay(s, context, skip, track)
				return
			}
		}
		return
	}
	visit(s)
	openBrowse(s, item.URI, item.Name, item.Image)
}

func homeClick(s *State, target string) bool {
	if target == "home-retry" {
		loadHomeFeed(s, true)
		return true
	}
	index, ok := rowIndex("home-card", target)
	if !ok {
		return false
	}
	openHomeItem(s, index)
	return true
}

func homeKey(s *State, k flatte.KeyEvent) bool {
	if s.page != pageHome || s.focus != paneTracks {
		return false
	}
	n := len(homeItems(s))
	if n == 0 {
		return false
	}
	switch k.Key {
	case flatte.KeyUp:
		s.discovery.HomeCursor = max(0, s.discovery.HomeCursor-1)
	case flatte.KeyDown:
		s.discovery.HomeCursor = min(n-1, s.discovery.HomeCursor+1)
	case flatte.KeyHome:
		s.discovery.HomeCursor = 0
	case flatte.KeyEnd:
		s.discovery.HomeCursor = n - 1
	case flatte.KeyEnter:
		openHomeItem(s, s.discovery.HomeCursor)
		return true
	default:
		return false
	}
	revealHomeCard(s)
	return true
}

// Artwork follows its allocated card width even during the initial layout
// before measured pane geometry arrives.
func homeArtwork(s *State, url string, size int) layout.Node {
	node := cover(s, url, size)
	switch v := node.(type) {
	case layout.Image:
		v.W = layout.Grow(1)
		return v
	case layout.Row:
		v.W = layout.Grow(1)
		return v
	case layout.Col:
		v.W = layout.Grow(1)
		return v
	default:
		return node
	}
}
