package app

import (
	"strings"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
)

const (
	idTrackFilter       = "track-filter"
	idTrackFilterBox    = "track-filter-box"
	idTrackFilterToggle = "track-filter-toggle"
	cmdTrackFilter      = "filter-songs"
)

func filterView(s *State, g bool) layout.Node {
	focused := s.focus == paneTrackFilter
	if !s.trackFilterOpen && strings.TrimSpace(s.trackFilter.Value) == "" && !focused {
		size := layout.NodeBase{ID: idTrackFilterToggle, Tooltip: "Find in playlist (⌘⇧F)", W: layout.Fixed(3), H: layout.Fixed(1)}
		if g {
			size.W, size.H = layout.Auto(), layout.Auto()
		}
		return layout.Button{NodeBase: size, Icon: layout.IconSearch, Label: "⌕", Plain: true}
	}
	field := s.trackFilter.Layout(idTrackFilter, focused)
	field.Clearable = true
	field.Placeholder = "Filter songs"
	field.W = layout.Grow(1)
	icon := layout.Icon{NodeBase: layout.NodeBase{W: layout.Fixed(2)}, Name: layout.IconSearch, Text: "⌕ ", Style: subtle}
	base := layout.NodeBase{ID: idTrackFilterBox, W: layout.Fixed(18), H: layout.Fixed(1), Gap: 1}
	if g {
		icon.W, icon.H = layout.Points(18), layout.Points(18)
		base.W, base.H = layout.Points(180), layout.Auto()
	}
	return layout.Row{NodeBase: base, Align: layout.AlignCenter, Children: []layout.Node{icon, field}}
}

func openTrackFilter(s *State) {
	s.trackFilterOpen = true
	s.focus = paneTrackFilter
	s.trackSort.Close()
}

func blurTrackFilter(s *State) {
	if s.focus == paneTrackFilter {
		s.focus = paneTracks
	}
	if strings.TrimSpace(s.trackFilter.Value) == "" {
		s.trackFilterOpen = false
	}
}

func collapseEmptyTrackFilter(s *State) {
	if s.focus != paneTrackFilter && strings.TrimSpace(s.trackFilter.Value) == "" {
		s.trackFilterOpen = false
	}
}

func filterTracks(s *State, l player.Listing) player.Listing {
	query := strings.ToLower(strings.TrimSpace(s.trackFilter.Value))
	if query == "" || virtual(l.URI) {
		return l
	}
	tracks := make([]player.Track, 0, len(l.Tracks))
	for _, track := range l.Tracks {
		if strings.Contains(strings.ToLower(track.Name), query) || strings.Contains(strings.ToLower(strings.Join(track.Artists, ", ")), query) || strings.Contains(strings.ToLower(track.Album), query) {
			tracks = append(tracks, track)
		}
	}
	l.Tracks = tracks
	return l
}

func changeTrackFilter(s *State, edit func(*flatui.TextField)) {
	selected, ok := s.selectedTrack()
	edit(&s.trackFilter)
	if s.focus == paneTrackFilter || strings.TrimSpace(s.trackFilter.Value) != "" {
		s.trackFilterOpen = true
	}
	tracks := displayListing(s, s.listings[s.shown]).Tracks
	s.tracks.SetCount(len(tracks))
	if ok {
		if i := indexOf(tracks, selected.URI); i >= 0 {
			s.tracks.Select(i)
			return
		}
	}
	s.tracks.Select(0)
}

func filterKey(s *State, key flatte.KeyEvent, fx flatte.Effects[State]) {
	switch key.Key {
	case flatte.KeyCtrlC:
		fx.Quit()
	case flatte.KeyEnter, flatte.KeyDown:
		s.focus = paneTracks
	case flatte.KeyTab:
		s.focus = paneSort
	case flatte.KeyEscape:
		changeTrackFilter(s, func(field *flatui.TextField) { *field = flatui.TextField{} })
		s.trackFilterOpen = false
		s.focus = paneTracks
	default:
		changeTrackFilter(s, func(field *flatui.TextField) {
			if !clipboardKey(field, key, fx) {
				editField(field, key)
			}
		})
	}
}
