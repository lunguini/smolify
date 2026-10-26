package app

import (
	"cmp"
	"slices"
	"strings"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/smolify/internal/player"
)

const idSort = "track-sort"
const cmdSort = "sort-songs"

const (
	sortPositionAsc = iota
	sortPositionDesc
	sortAddedAtAsc
	sortAddedAtDesc
	sortTitleAsc
	sortTitleDesc
	sortArtistAsc
	sortArtistDesc
	sortAlbumAsc
	sortAlbumDesc
	sortDurationAsc
	sortDurationDesc
)

var sortOptions = []string{
	"Position ↑", "Position ↓", "Date added ↑", "Date added ↓", "Title A–Z", "Title Z–A", "Artist A–Z", "Artist Z–A",
	"Album A–Z", "Album Z–A", "Duration: shortest", "Duration: longest",
}

var regularSortOptions = []string{
	"Position ↑", "Position ↓", "Title A–Z", "Title Z–A", "Artist A–Z", "Artist Z–A",
	"Album A–Z", "Album Z–A", "Duration: shortest", "Duration: longest",
}

func likedURI(s *State, uri string) bool {
	return s.username != "" && uri == player.LikedSongs(s.username)
}

func hasLikedTracks(s *State) bool {
	_, ok := s.web.(likedTracksWeb)
	return ok
}

func sortMode(s *State) int {
	for mode, option := range sortOptions {
		if s.trackSort.Value() == option {
			return mode
		}
	}
	return sortPositionAsc
}

func selectSortMode(s *State, mode int) {
	options := regularSortOptions
	if likedURI(s, s.shown) && hasLikedTracks(s) {
		options = sortOptions
	}
	value := "Position ↑"
	if mode >= 0 && mode < len(sortOptions) {
		value = sortOptions[mode]
	}
	for i, option := range options {
		if option == value {
			s.trackSort.Select(i)
			return
		}
	}
	s.trackSort.Select(0)
}

func modeAt(s *State, index int) int {
	options := regularSortOptions
	if likedURI(s, s.shown) && hasLikedTracks(s) {
		options = sortOptions
	}
	if index < 0 || index >= len(options) {
		return sortPositionAsc
	}
	for mode, option := range sortOptions {
		if options[index] == option {
			return mode
		}
	}
	return sortPositionAsc
}

// displayListing derives the display order without changing the server's
// canonical playlist. Actions use the same order as the rows they target.
func displayListing(s *State, l player.Listing) player.Listing {
	l = filterTracks(s, l)
	mode := sortMode(s)
	if mode == sortPositionAsc || virtual(l.URI) {
		return l
	}
	if mode == sortPositionDesc {
		l.Tracks = slices.Clone(l.Tracks)
		slices.Reverse(l.Tracks)
		return l
	}
	type keyedTrack struct {
		track player.Track
		key   string
	}
	keyed := make([]keyedTrack, len(l.Tracks))
	for i, t := range l.Tracks {
		var key string
		switch mode {
		case sortTitleAsc, sortTitleDesc:
			key = t.Name
		case sortArtistAsc, sortArtistDesc:
			key = strings.Join(t.Artists, ", ")
		case sortAlbumAsc, sortAlbumDesc:
			key = t.Album
		}
		keyed[i] = keyedTrack{track: t, key: strings.ToLower(key)}
	}
	slices.SortStableFunc(keyed, func(a, b keyedTrack) int {
		if a.track.Name == "" || b.track.Name == "" {
			if a.track.Name == "" && b.track.Name == "" {
				return 0
			}
			if a.track.Name == "" {
				return 1
			}
			return -1
		}
		order := strings.Compare(a.key, b.key)
		if mode == sortAddedAtAsc || mode == sortAddedAtDesc {
			at, bt := a.track.AddedAt, b.track.AddedAt
			switch {
			case at.IsZero() && bt.IsZero():
				return 0
			case at.IsZero():
				return 1
			case bt.IsZero():
				return -1
			default:
				order = at.Compare(bt)
			}
		} else if mode == sortDurationAsc || mode == sortDurationDesc {
			order = cmp.Compare(a.track.Duration, b.track.Duration)
		}
		if mode%2 == 1 && mode != sortAddedAtDesc {
			order = -order
		} else if mode == sortAddedAtDesc && !a.track.AddedAt.IsZero() && !b.track.AddedAt.IsZero() {
			order = -order
		}
		return order
	})
	l.Tracks = make([]player.Track, len(keyed))
	for i, t := range keyed {
		l.Tracks[i] = t.track
	}
	return l
}

func sortView(s *State) layout.Node {
	tooltip := "Sort songs: " + s.trackSort.Value()
	mode := sortMode(s)
	if mode == sortPositionAsc || mode == sortPositionDesc {
		tooltip = "Sort by original playlist position (#)"
	} else if mode == sortAddedAtAsc || mode == sortAddedAtDesc {
		tooltip = "Sort by when tracks were added to Liked Songs"
	}
	return s.trackSort.LayoutWithButton(layout.Button{NodeBase: layout.NodeBase{ID: idSort, Tooltip: tooltip}, Focused: s.focus == paneSort})
}

func chooseSort(s *State, mode int) {
	selected, ok := s.selectedTrack()
	selectSortMode(s, mode)
	keepTrack(s, selected.URI, ok)
}

func keepTrack(s *State, uri string, selected bool) {
	l := displayListing(s, s.listings[s.shown])
	s.tracks.SetCount(len(l.Tracks))
	if selected {
		if i := indexOf(l.Tracks, uri); i >= 0 {
			s.tracks.Select(i)
			return
		}
	}
	if strings.TrimSpace(s.trackFilter.Value) != "" {
		s.tracks.Select(0)
	}
}

// replaceListing also remaps the selected track as partially loaded metadata
// changes a sorted list's order.
func replaceListing(s *State, uri string, l player.Listing) {
	selected, ok := s.selectedTrack()
	s.listings[uri] = l
	if uri == s.shown {
		keepTrack(s, selected.URI, ok)
	}
}

func sortKey(s *State, k flatte.KeyEvent) bool {
	if s.focus != paneSort && !s.trackSort.IsOpen() {
		return false
	}
	switch k.Key {
	case flatte.KeyCtrlC:
		return false
	case flatte.KeyEscape:
		s.trackSort.Close()
		s.focus = paneTracks
	case flatte.KeyTab:
		s.trackSort.Close()
		s.focus = paneLibrary
	case flatte.KeyUp:
		if s.trackSort.IsOpen() {
			s.trackSort.MoveUp()
		} else {
			s.trackSort.Open()
		}
	case flatte.KeyDown:
		if s.trackSort.IsOpen() {
			s.trackSort.MoveDown()
		} else {
			s.trackSort.Open()
		}
	case flatte.KeyEnter:
		if s.trackSort.IsOpen() {
			chooseSort(s, modeAt(s, s.trackSort.Highlighted()))
		} else {
			s.trackSort.Open()
		}
	default:
		if k.Key == flatte.KeyCharacter && k.Rune == ' ' && k.Mod == 0 {
			s.trackSort.Toggle()
		} else {
			return s.trackSort.IsOpen()
		}
	}
	return true
}

func sortClick(s *State, target string) bool {
	if target == idSort || strings.HasPrefix(target, idSort+":option:") {
		selected, ok := s.selectedTrack()
		if s.trackSort.Click(target, idSort) {
			keepTrack(s, selected.URI, ok)
			s.focus = paneSort
			return true
		}
	}
	if s.trackSort.IsOpen() {
		s.trackSort.Close()
	}
	return false
}

// originalPositionNumbers keeps the # column tied to source playlist
// positions. Occurrence queues distinguish repeated copies of one song.
func originalPositionNumbers(source, displayed []player.Track, reverse bool) []int {
	positions := make(map[string][]int, len(source))
	for i, track := range source {
		positions[track.URI] = append(positions[track.URI], i+1)
	}
	if reverse {
		for _, numbers := range positions {
			slices.Reverse(numbers)
		}
	}
	numbers := make([]int, len(displayed))
	for i, track := range displayed {
		queue := positions[track.URI]
		numbers[i] = i + 1
		if len(queue) > 0 {
			numbers[i] = queue[0]
			positions[track.URI] = queue[1:]
		}
	}
	return numbers
}
