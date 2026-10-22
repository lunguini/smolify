package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"
)

var (
	accent = lipgloss.Color("#1ed760")
	subtle = layout.TextStyle{Role: layout.TextSubtle}
	strong = layout.TextStyle{Bold: true}
)

// railWidth is the library rail's width in columns.
const railWidth = 30

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
		frame.Root = layout.Col{Children: []layout.Node{
			layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1)}, Children: []layout.Node{
				railView(s),
				centerView(s),
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

// railView is the library: the playlists, the selected one marked, the
// playing one with a note.
func railView(s *State) layout.Node {
	rows := s.rail.Rows(idRail, func(i int, selected bool) layout.Node {
		pl := s.playlists[i]
		st := layout.TextStyle{}
		if selected {
			st = layout.TextStyle{Bold: true}
		}
		mark := layout.Icon{Text: "  "}
		if pl.URI == s.context && s.track != nil {
			mark = layout.Icon{Text: "♪ ", Style: layout.TextStyle{Fg: accent}}
			st.Fg = accent
		}
		return layout.Row{Children: []layout.Node{mark, layout.Label{Text: pl.Name, Style: st}}}
	})
	rows.ID, rows.H = idRail, layout.Grow(1)
	title := "Your Library"
	if len(s.playlists) > 0 {
		title += "  " + strconv.Itoa(len(s.playlists))
	}
	return layout.Col{
		NodeBase: layout.NodeBase{W: layout.Fixed(railWidth), PadLeft: 1, PadRight: 1, PadTop: 1, Box: layout.Box{Background: lipgloss.Color("#121212")}},
		Children: []layout.Node{
			layout.Label{Text: title, Style: layout.TextStyle{Bold: true, Size: 15}},
			layout.Label{},
			rows,
		},
	}
}

// centerView shows the selected playlist.
func centerView(s *State) layout.Node {
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
	info := []layout.Node{
		layout.Label{Text: "PLAYLIST", Style: subtle},
		layout.Label{Text: pl.Name, Wrap: true, Style: layout.TextStyle{Bold: true, Size: 30}},
	}
	if pl.Description != "" {
		info = append(info, layout.Label{Text: pl.Description, Wrap: true, Style: subtle})
	}
	meta := plural(pl.Length, "song")
	if pl.Owner != "" {
		meta = pl.Owner + " · " + meta
	}
	info = append(info,
		layout.Label{Text: meta, Style: subtle},
		layout.Label{},
		layout.Row{Children: []layout.Node{
			layout.Button{NodeBase: layout.NodeBase{ID: idPlayList, Tooltip: "Play this playlist"}, Label: "▶ Play", Default: true},
		}},
	)
	if s.status != "" {
		info = append(info, layout.Label{}, layout.Label{Text: s.status, Wrap: true, Style: layout.TextStyle{Fg: lipgloss.Color("#f15e6c")}})
	}
	return layout.Col{NodeBase: layout.NodeBase{Pad: 2, W: layout.Grow(1)}, Children: []layout.Node{
		layout.Row{NodeBase: layout.NodeBase{Gap: 3}, Children: []layout.Node{
			cover(s, pl.Image, 128),
			layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: info},
		}},
	}}
}

// cover is the image at url, size points square — in a terminal (which
// shows an image's text) and until it loads, a framed ♫.
func cover(s *State, url string, size int) layout.Node {
	nb := layout.NodeBase{W: layout.Points(size), H: layout.Points(size), Box: layout.Box{Border: layout.BorderRounded, BorderColor: lipgloss.Color("#333333")}}
	if im := s.covers[url]; im != nil {
		return layout.Image{NodeBase: layout.NodeBase{W: nb.W, H: nb.H}, Source: im, Text: coverText(size), Fit: layout.ImageCover}
	}
	return layout.Col{NodeBase: nb, Children: []layout.Node{
		layout.NewSpacer(),
		layout.Row{Children: []layout.Node{layout.NewSpacer(), layout.Label{Text: "♫", Style: subtle}, layout.NewSpacer()}},
		layout.NewSpacer(),
	}}
}

// coverText is a cover's terminal spelling: a framed ♫ filling its cells.
func coverText(size int) string {
	w := max((size+layout.PointsPerColumn-1)/layout.PointsPerColumn, 3)
	h := max((size+layout.PointsPerLine-1)/layout.PointsPerLine, 2)
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
	now := layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(railWidth + 2), Gap: 1}, Children: []layout.Node{
		cover(s, coverURL, 48),
		layout.Col{Children: []layout.Node{
			layout.Label{Text: title, Style: strong},
			layout.Label{Text: artists, Style: subtle},
		}},
	}}

	play := "▶"
	if s.playing {
		play = "⏸"
	}
	modeStyle := func(on bool) layout.TextStyle {
		if on {
			return layout.TextStyle{Fg: accent, Size: 15}
		}
		return layout.TextStyle{Role: layout.TextSubtle, Size: 15}
	}
	// The transport is glyphs, the play button the one solid control.
	glyph := func(id, text, tip string, st layout.TextStyle) layout.Node {
		return layout.Icon{NodeBase: layout.NodeBase{ID: id, Tooltip: tip}, Text: text, Style: st}
	}
	plain := layout.TextStyle{Size: 15}
	controls := layout.Row{NodeBase: layout.NodeBase{Gap: 3}, Children: []layout.Node{
		layout.NewSpacer(),
		glyph(idShuffle, "⇄", "Shuffle (s)", modeStyle(s.shuffle)),
		glyph(idPrev, "⏮", "Previous (p)", plain),
		layout.Button{NodeBase: layout.NodeBase{ID: idPlay, Tooltip: "Play/Pause (space)"}, Label: play, Default: true},
		glyph(idNext, "⏭", "Next (n)", plain),
		glyph(idRepeat, "↻", "Repeat (r)", modeStyle(s.repeat.Context)),
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
		layout.Slider{NodeBase: layout.NodeBase{ID: idProgress}, Value: progress, Fill: accent, Disabled: s.track == nil},
		layout.Label{Text: clock(dur), Style: subtle},
	}}

	vol := 0.0
	if s.volMax > 0 {
		vol = float64(s.volume) / float64(s.volMax)
	}
	if s.drag == idVolume {
		vol = s.dragValue
	}
	volume := layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(20), Gap: 1}, Children: []layout.Node{
		layout.Label{Text: "vol", Style: subtle},
		layout.Slider{NodeBase: layout.NodeBase{ID: idVolume, Tooltip: "Volume (+/-)"}, Value: vol},
	}}

	return layout.Col{
		NodeBase: layout.NodeBase{PadLeft: 1, PadRight: 1, Box: layout.Box{Background: lipgloss.Color("#181818")}},
		Children: []layout.Node{
			layout.Row{NodeBase: layout.NodeBase{Gap: 2}, Children: []layout.Node{
				now,
				layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: []layout.Node{controls, timeline}},
				layout.Col{Children: []layout.Node{layout.Label{}, volume}},
			}},
		},
	}
}

// clock formats d as m:ss.
func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
