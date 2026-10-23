package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/flatte/window"

	"github.com/lunguini/smolify/internal/player"
)

// headerCover is a list header's cover size in points.
const headerCover = 112

// ownerName is how a playlist's owner shows: "You", "Spotify", a
// readable username, or nothing for the ID-like ones the player reports
// for accounts without one.
func ownerName(owner, you string) string {
	switch {
	case owner == "":
		return ""
	case owner == you:
		return "You"
	case owner == "spotify":
		return "Spotify"
	case accountID(owner):
		return ""
	}
	return owner
}

// accountID reports a username that is an account ID: all digits, or 25
// lowercase letters and digits.
func accountID(name string) bool {
	digits, alnum := true, true
	for _, r := range name {
		digits = digits && r >= '0' && r <= '9'
		alnum = alnum && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z')
	}
	return digits || alnum && len(name) == 25
}

// railWidth is the library rail's width in columns; railRowLines is a
// library row's height: a cover beside a name and a line about it.
const (
	railWidth    = 32
	railRowLines = 2
)

// card is a rounded panel on the window (cardBg unless nb has a
// background).
func card(nb layout.NodeBase, children ...layout.Node) layout.Col {
	if nb.Box.Background == nil {
		nb.Box.Background = cardBg
	}
	nb.Box.Round = true
	return layout.Col{NodeBase: nb, Children: children}
}

// View draws the sign-in screen until the player is ready, then the
// library, the selected playlist, and the player bar.
func View(s *State, _ flatte.RenderContext) flatte.Frame {
	frame := flatte.Frame{Title: "Smolify"}
	switch {
	case !s.ready && s.pairing.Code != "":
		frame.Root = pairingView(s)
	case !s.ready:
		frame.Root = centered(
			layout.Label{Text: "Smolify", Style: layout.TextStyle{Role: layout.TextTitle, Size: 28}},
			layout.Label{},
			layout.Label{Text: statusOr(s, "Connecting to Spotify…"), Style: subtle},
		)
	default:
		if s.track != nil {
			frame.Title = s.track.Name + " · " + strings.Join(s.track.Artists, ", ")
		}
		frame.Root = layout.Col{NodeBase: layout.NodeBase{PadLeft: 1, PadRight: 1, PadBottom: 1}, Children: []layout.Node{
			topBar(s),
			layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1), Gap: 1}, Children: []layout.Node{
				railView(s),
				card(layout.NodeBase{W: layout.Grow(1)}, centerView(s)),
			}},
			playerBar(s),
		}}
	}
	return frame
}

func statusOr(s *State, fallback string) string {
	if s.status != "" {
		return s.status
	}
	return fallback
}

// centered puts children in a column in the middle of the frame.
func centered(children ...layout.Node) layout.Node {
	col := layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(52)}, Children: children}
	return layout.Col{Children: []layout.Node{
		layout.NewSpacer(),
		layout.Row{Children: []layout.Node{layout.NewSpacer(), col, layout.NewSpacer()}},
		layout.NewSpacer(),
	}}
}

// pairingView asks the user to approve this device.
func pairingView(s *State) layout.Node {
	p := s.pairing
	return centered(
		layout.Label{Text: "Sign in to Spotify", Style: layout.TextStyle{Role: layout.TextTitle, Size: 24}},
		layout.Label{},
		layout.Label{Wrap: true, Text: "Approve Smolify as a speaker for your account. Open spotify.com/pair and enter:"},
		layout.Label{},
		layout.Label{Text: spaced(p.Code), Selectable: true, Style: layout.TextStyle{Bold: true, Size: 34, Fg: accent}},
		layout.Label{},
		layout.Row{NodeBase: layout.NodeBase{Gap: 2}, Children: []layout.Node{
			layout.Button{NodeBase: layout.NodeBase{ID: idOpenPair, Tooltip: p.URL}, Label: "Open in Browser", Default: true},
		}},
		layout.Label{},
		layout.Label{Text: "or open " + p.URL, Selectable: true, Wrap: true, Style: subtle},
		layout.Label{Text: "The code expires at " + p.Expires.Local().Format(time.Kitchen) + ". Needs Spotify Premium.", Style: subtle},
	)
}

// spaced puts a space between a code's characters, for reading aloud.
func spaced(code string) string { return strings.Join(strings.Split(code, ""), " ") }

// railView is the navigation and the library: each list with its cover,
// the playing one in the accent.
func railView(s *State) layout.Node {
	lib := s.library()
	rows := s.rail.Rows(idRail, func(i int, selected bool) layout.Node {
		pl := lib[i]
		name := layout.TextStyle{}
		if pl.URI == s.context && s.track != nil {
			name.Fg = accent
		}
		about := "Playlist"
		if o := ownerName(pl.Owner, s.username); o != "" {
			about += " · " + o
		}
		art := cover(s, pl.Image, 32)
		if s.username != "" && pl.URI == player.LikedSongs(s.username) {
			art = likedTile(32)
		}
		row := layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
			art,
			layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: []layout.Node{
				layout.Label{NodeBase: layout.NodeBase{Tooltip: pl.Name}, Text: pl.Name, Truncate: true, Style: name},
				layout.Label{Text: about, Truncate: true, Style: secondary},
			}},
		}}
		if name.Fg != nil { // playing from here
			row.Children = append(row.Children, layout.Icon{Text: "♪", Style: layout.TextStyle{Fg: accent}})
		}
		return row
	})
	rows.ID, rows.W, rows.H = idRail, layout.Grow(1), layout.Grow(1)
	if s.page != pageLibrary { // the center shows Home or search, not a list
		for i, c := range rows.Children {
			if r, ok := c.(layout.Row); ok {
				r.Box.Selected = false
				rows.Children[i] = r
			}
		}
	}
	count := ""
	if n := len(lib); n > 0 {
		count = strconv.Itoa(n)
	}
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(railWidth)}, Children: []layout.Node{
		card(layout.NodeBase{H: layout.Grow(1), PadLeft: 1, PadRight: 1, PadTop: 1, Box: layout.Box{Background: railBg}},
			layout.Row{Children: []layout.Node{
				layout.Label{Text: "Your Library", Style: layout.TextStyle{Bold: true, Size: 15}},
				layout.NewSpacer(),
				layout.Label{Text: count, Style: subtle},
			}},
			layout.Label{},
			layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1)}, Children: []layout.Node{rows, listBar(s.rail, idRailBar)}},
		),
	}}
}

// likedTile is Liked Songs' cover: a heart on violet.
func likedTile(size int) layout.Node {
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Points(size), H: layout.Points(size), Box: layout.Box{Background: lipgloss.Color("#4f3bb0"), Round: true}}, Children: []layout.Node{
		layout.NewSpacer(),
		layout.Row{Children: []layout.Node{layout.NewSpacer(), layout.Label{Text: "♥", Style: layout.TextStyle{Fg: white}}, layout.NewSpacer()}},
		layout.NewSpacer(),
	}}
}

// topBar sits in the window's title strip, right of its buttons: Home and
// the search field.
func topBar(s *State) layout.Node {
	home := layout.Button{NodeBase: layout.NodeBase{ID: idHome, Tooltip: "Home (h)", W: layout.Points(32), H: layout.Points(32)},
		Icon: layout.IconHome, Label: "⌂", Plain: true, Disabled: s.web == nil}
	if s.page == pageHome {
		home.Style.Fg = white
	}
	field := s.query.Layout(idQuery, s.focus == paneSearch)
	field.Placeholder = "What do you want to play?"
	field.W = layout.Fixed(46)
	return layout.Row{NodeBase: layout.NodeBase{H: layout.Points(32), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		layout.Col{NodeBase: layout.NodeBase{W: layout.Points(window.TitleBarButtonsWidth)}},
		layout.NewSpacer(),
		home,
		field,
		layout.NewSpacer(),
	}}
}

// centerView shows the page: the selected list, Home, or search.
func centerView(s *State) layout.Node {
	switch s.page {
	case pageHome:
		return virtualView(s, "Home", "Recently played", nil)
	case pageSearch:
		return virtualView(s, "Search", "Songs", nil)
	}
	pl, ok := s.selected()
	if !ok {
		msg := "Loading your library…"
		if s.loaded && len(s.playlists) == 0 {
			msg = "No playlists in your library yet."
		}
		return layout.Col{NodeBase: layout.NodeBase{Pad: 2, W: layout.Grow(1)}, Children: []layout.Node{
			layout.Label{Text: statusOr(s, msg), Style: subtle},
		}}
	}
	l, listed := s.listings[pl.URI]
	n := pl.Length
	if listed && l.Ready {
		n = l.Length
	}
	meta := []layout.Run{{Text: plural(n, "song"), Style: subtle}}
	if o := ownerName(pl.Owner, s.username); o != "" {
		meta = append([]layout.Run{{Text: o, Style: strong}, {Text: " · ", Style: subtle}}, meta...)
	}
	info := []layout.Node{
		layout.Label{Text: "Playlist", Style: layout.TextStyle{Bold: true, Size: 12}},
		layout.Label{NodeBase: layout.NodeBase{Tooltip: pl.Name}, Text: pl.Name, Truncate: true, Style: layout.TextStyle{Bold: true, Size: 30, Fg: white}},
	}
	if pl.Description != "" {
		info = append(info, layout.Label{Text: pl.Description, Wrap: true, Style: subtle})
	}
	play := layout.Button{NodeBase: layout.NodeBase{ID: idPlayList, Tooltip: "Play this playlist", W: layout.Points(32), H: layout.Points(32)}, Icon: layout.IconPlay, Label: "▶", Default: true}
	info = append(info, layout.Row{NodeBase: layout.NodeBase{Gap: 2}, Align: layout.AlignCenter, Children: []layout.Node{play, layout.Label{Runs: meta}}})
	art := cover(s, pl.Image, headerCover)
	if s.username != "" && pl.URI == player.LikedSongs(s.username) {
		art = likedTile(headerCover)
	}
	header := layout.Row{NodeBase: layout.NodeBase{Gap: 3, PadLeft: 2, PadRight: 2, PadTop: 1, PadBottom: 1, Box: layout.Box{Background: s.tints[pl.Image], Round: true}}, Align: layout.AlignEnd, Children: []layout.Node{
		art,
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: info},
	}}
	var note layout.Node = layout.Label{}
	switch {
	case s.status != "":
		note = layout.Label{Text: s.status, Style: layout.TextStyle{Fg: danger}}
	case s.notice != "":
		note = layout.Label{Text: s.notice, Style: subtle}
	}
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Grow(1)}, Children: []layout.Node{
		header,
		layout.Col{NodeBase: layout.NodeBase{PadLeft: 2, PadRight: 2, H: layout.Grow(1)}, Children: []layout.Node{
			note,
			trackTable(s, l, listed),
		}},
	}}
}

// virtualView is Home or search: a title, a line or field under it, and
// the tracks.
func virtualView(s *State, title, sub string, field layout.Node) layout.Node {
	head := []layout.Node{layout.Label{Text: title, Style: layout.TextStyle{Bold: true, Size: 30, Fg: white}}}
	if sub != "" {
		head = append(head, layout.Label{Text: sub, Style: subtle})
	}
	if field != nil {
		head = append(head, layout.Label{}, field)
	}
	if s.status != "" {
		head = append(head, layout.Label{}, layout.Label{Text: s.status, Wrap: true, Style: layout.TextStyle{Fg: danger}})
	} else if s.notice != "" {
		head = append(head, layout.Label{}, layout.Label{Text: s.notice, Wrap: true, Style: subtle})
	}
	l, listed := s.listings[s.shown]
	var table layout.Node
	switch {
	case s.web == nil:
		table = layout.Label{Text: "Not available.", Style: subtle}
	case s.page == pageSearch && strings.TrimSpace(s.query.Value) == "":
		table = layout.Label{Text: "Type to search songs.", Style: subtle}
	case s.page == pageSearch && !listed:
		table = layout.Label{Text: "Searching…", Style: subtle}
	case s.page == pageHome && !listed:
		table = layout.Label{Text: "Loading…", Style: subtle}
	case l.Length == 0 && s.page == pageSearch:
		table = layout.Label{Text: "Nothing found.", Style: subtle}
	default:
		table = trackTable(s, l, listed)
	}
	children := append(head, layout.Label{}, table)
	return layout.Col{NodeBase: layout.NodeBase{PadLeft: 2, PadRight: 2, PadTop: 2, W: layout.Grow(1), H: layout.Grow(1)}, Children: children}
}

// trackTable is the selected list's tracks: number, title and artists,
// album, liked, and duration.
func trackTable(s *State, l player.Listing, listed bool) layout.Node {
	switch {
	case !listed || !l.Ready && len(l.Tracks) == 0:
		return layout.Label{Text: "Listing the tracks…", Style: subtle}
	case l.Length == 0:
		return layout.Label{Text: "No tracks here yet.", Style: subtle}
	}
	header := trackCells(layout.Label{Text: "#", Style: caption}, layout.Label{Text: "TITLE", Style: caption},
		layout.Label{Text: "ALBUM", Style: caption}, layout.Icon{}, layout.Label{Text: "TIME", Style: caption})
	rule := layout.Label{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Text: strings.Repeat("─", 400), Style: layout.TextStyle{Fg: lipgloss.Color("#2a2a2a")}}
	rows := s.tracks.Rows(idTracks, func(i int, _ bool) layout.Node {
		if i >= len(l.Tracks) {
			return layout.Label{}
		}
		return trackRow(s, i, l.Tracks[i])
	})
	rows.ID, rows.W, rows.H = idTracks, layout.Grow(1), layout.Grow(1)
	return layout.Col{NodeBase: layout.NodeBase{H: layout.Grow(1)}, Children: []layout.Node{
		header,
		rule,
		layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1)}, Children: []layout.Node{rows, listBar(s.tracks, idTrackBar)}},
	}}
}

// trackCells lays out one row of the table.
func trackCells(num, title, album, liked, dur layout.Node) layout.Row {
	return layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), Gap: 2}, Children: []layout.Node{
		layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(3)}, Children: []layout.Node{num}},
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(3)}, Children: []layout.Node{title}},
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(2)}, Children: []layout.Node{album}},
		layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(2)}, Children: []layout.Node{liked}},
		layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(5)}, Children: []layout.Node{layout.NewSpacer(), dur}},
	}}
}

// trackRow is track t at index i; the playing one in the accent with a
// note for its number.
func trackRow(s *State, i int, t player.Track) layout.Node {
	if t.Name == "" {
		placeholder := "…" // still being listed
		if s.listings[s.shown].Complete() {
			placeholder = "Unavailable" // listed, but Spotify has nothing on it
		}
		return trackCells(layout.Label{Text: strconv.Itoa(i + 1), Style: subtle}, layout.Label{Text: placeholder, Style: subtle},
			layout.Label{}, layout.Icon{}, layout.Label{})
	}
	num := layout.Label{Text: strconv.Itoa(i + 1), Style: subtle}
	name := layout.TextStyle{}
	if s.track != nil && s.track.URI == t.URI && (s.context == s.shown || virtual(s.shown)) {
		num = layout.Label{Text: "♪", Style: layout.TextStyle{Fg: accent}}
		name.Fg = accent
	}
	artists := strings.Join(t.Artists, ", ")
	title := layout.Label{NodeBase: layout.NodeBase{Tooltip: t.Name + " — " + artists}, Truncate: true, Runs: []layout.Run{
		{Text: t.Name, Style: name},
		{Text: "  " + artists, Style: secondary},
	}}
	album := layout.Label{NodeBase: layout.NodeBase{Tooltip: t.Album}, Text: t.Album, Truncate: true, Style: subtle}
	return trackCells(num, title, album, heart(s.liked[t.URI], ""), layout.Label{Text: clock(t.Duration), Style: subtle})
}

// heart marks a liked track. With an id it is the like control, an outline
// when not liked.
func heart(liked bool, id string) layout.Node {
	if id == "" {
		if liked {
			return layout.Icon{Name: layout.IconHeart, Text: "♥ ", Style: layout.TextStyle{Fg: accent}}
		}
		return layout.Icon{Text: "  "}
	}
	b := layout.Button{NodeBase: layout.NodeBase{ID: id, Tooltip: "Save to Liked Songs (l)", W: layout.Points(24), H: layout.Points(24)},
		Icon: layout.IconHeartOutline, Label: "♡", Plain: true, Style: subtle}
	if liked {
		b.Tooltip, b.Icon, b.Label, b.Style = "Remove from Liked Songs (l)", layout.IconHeart, "♥", layout.TextStyle{Fg: accent}
	}
	return b
}

// cover is the image at url, size points square — in a terminal (which
// shows an image's text) and until it loads, a ♫ on a tile.
func cover(s *State, url string, size int) layout.Node {
	nb := layout.NodeBase{W: layout.Points(size), H: layout.Points(size), Box: layout.Box{Background: tileBg, Round: true}}
	if im := s.covers[url]; im != nil {
		return layout.Image{NodeBase: layout.NodeBase{W: nb.W, H: nb.H}, Source: im, Text: coverText(size), Fit: layout.ImageCover}
	}
	note := layout.Row{Children: []layout.Node{layout.NewSpacer(), layout.Label{Text: "♫", Style: subtle}, layout.NewSpacer()}}
	if size < 3*layout.PointsPerLine { // a thumbnail: the note on top, as its text has it
		return layout.Col{NodeBase: nb, Children: []layout.Node{note, layout.NewSpacer()}}
	}
	return layout.Col{NodeBase: nb, Children: []layout.Node{layout.NewSpacer(), note, layout.NewSpacer()}}
}

// coverText is a cover's terminal spelling: a framed ♫ filling its cells,
// or for a thumbnail (under three lines) a ♫ on its first line.
func coverText(size int) string {
	w := max((size+layout.PointsPerColumn-1)/layout.PointsPerColumn, 3)
	h := max((size+layout.PointsPerLine-1)/layout.PointsPerLine, 1)
	if h < 3 {
		lines := make([]string, h)
		lines[0] = strings.Repeat(" ", w-1-(w-1)/2) + "♫" + strings.Repeat(" ", (w-1)/2) // where a centered tile puts it
		for i := 1; i < h; i++ {
			lines[i] = strings.Repeat(" ", w)
		}
		return strings.Join(lines, "\n")
	}
	inner := w - 2
	lines := make([]string, h)
	lines[0] = "╭" + strings.Repeat("─", inner) + "╮"
	for i := 1; i < h-1; i++ {
		row := strings.Repeat(" ", inner)
		if i == (h-1)/2 {
			row = strings.Repeat(" ", (inner-1)/2) + "♫" + strings.Repeat(" ", inner-1-(inner-1)/2)
		}
		lines[i] = "│" + row + "│"
	}
	lines[h-1] = "╰" + strings.Repeat("─", inner) + "╯"
	return strings.Join(lines, "\n")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// playerBar is the now-playing bar: the track, the transport, the progress,
// and the volume.
func playerBar(s *State) layout.Node {
	title, artists := "Nothing playing", "Pick a playlist and press space"
	coverURL := ""
	if t := s.track; t != nil {
		title, artists, coverURL = t.Name, strings.Join(t.Artists, ", "), t.Cover
	}
	now := layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(railWidth), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		cover(s, coverURL, 48),
		layout.Col{Children: []layout.Node{
			layout.Label{Text: title, Style: strong},
			layout.Label{Text: artists, Style: subtle},
		}},
	}}
	if t := s.track; t != nil && strings.HasPrefix(t.URI, "spotify:track:") {
		now.Children = append(now.Children, layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(2)}, Children: []layout.Node{heart(s.liked[t.URI], idLike)}})
	}

	sq := func(id, tip string, pts int) layout.NodeBase {
		return layout.NodeBase{ID: id, Tooltip: tip, W: layout.Points(pts), H: layout.Points(pts)}
	}
	mode := func(on bool) layout.TextStyle {
		if on {
			return layout.TextStyle{Fg: accent}
		}
		return subtle
	}
	play := layout.Button{NodeBase: sq(idPlay, "Play/Pause (space)", 32), Icon: layout.IconPlay, Label: "▶", Default: true}
	if s.playing {
		play.Icon, play.Label = layout.IconPause, "⏸"
	}
	controls := layout.Row{NodeBase: layout.NodeBase{Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		layout.NewSpacer(),
		layout.Button{NodeBase: sq(idShuffle, "Shuffle (s)", 32), Icon: layout.IconShuffle, Label: "⇄", Plain: true, Style: mode(s.shuffle)},
		layout.Button{NodeBase: sq(idPrev, "Previous (p)", 32), Icon: layout.IconPrevious, Label: "⏮", Plain: true, Style: subtle},
		play,
		layout.Button{NodeBase: sq(idNext, "Next (n)", 32), Icon: layout.IconNext, Label: "⏭", Plain: true, Style: subtle},
		layout.Button{NodeBase: sq(idRepeat, "Repeat (r)", 32), Icon: layout.IconRepeat, Label: "↻", Plain: true, Style: mode(s.repeat.Context)},
		layout.NewSpacer(),
	}}

	var pos, dur time.Duration
	progress := 0.0
	if t := s.track; t != nil && t.Duration > 0 {
		pos, dur = s.position, t.Duration
		progress = float64(pos) / float64(dur)
		if s.drag == idProgress {
			progress = s.dragValue
			pos = time.Duration(progress * float64(dur))
		}
	}
	timeline := layout.Row{NodeBase: layout.NodeBase{Gap: 1}, Children: []layout.Node{
		layout.Label{Text: clock(pos), Style: subtle},
		layout.Slider{NodeBase: layout.NodeBase{ID: idProgress}, Value: progress, Fill: white, Disabled: s.track == nil},
		layout.Label{Text: clock(dur), Style: subtle},
	}}

	vol := 0.0
	if s.volMax > 0 {
		vol = float64(s.volume) / float64(s.volMax)
	}
	if s.drag == idVolume {
		vol = s.dragValue
	}
	volume := layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(20), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		layout.Icon{Name: layout.IconSpeaker, Text: "🔈", Style: subtle},
		layout.Slider{NodeBase: layout.NodeBase{ID: idVolume, Tooltip: "Volume (+/-)"}, Value: vol, Fill: white},
	}}

	return layout.Col{
		NodeBase: layout.NodeBase{PadTop: 1},
		Children: []layout.Node{
			layout.Row{NodeBase: layout.NodeBase{Gap: 2}, Align: layout.AlignCenter, Children: []layout.Node{
				now,
				layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: []layout.Node{controls, timeline}},
				volume,
			}},
		},
	}
}

// clock formats d as m:ss.
func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
