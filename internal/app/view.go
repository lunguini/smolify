package app

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/model"
)

// pick is gui in a window and term in a terminal: sizes in points round up
// to whole cells in a terminal, which is tighter on room.
func pick(g bool, gui, term int) int {
	if g {
		return gui
	}
	return term
}

// headerCover is a list header's cover size in points: as tall as the text
// and buttons beside it.
const headerCover = 152

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
	// termRailWidth is the rail's width in a terminal: wider, so more of a
	// playlist's name shows beside its cover.
	termRailWidth = 40
)

// rail is the library rail's width in columns.
func rail(g bool) int { return pick(g, railWidth, termRailWidth) }

// card is a rounded panel on the window (the look's Content unless nb has
// a background).
func card(nb layout.NodeBase, children ...layout.Node) layout.Col {
	if nb.Box.Background == nil && nb.Box.Fill == layout.FillNone {
		nb.Box.Fill = layout.FillContent
	}
	nb.Box.Round = true
	return layout.Col{NodeBase: nb, Children: children}
}

// headerToolsColumns is the terminal width from which Find and sort sit
// on the playlist header's actions row; narrower, a row of their own.
const headerToolsColumns = 120

// navLines is a terminal rail's Home or Search key's height: its label
// with a line of room above and below. The keys share the rail's lines:
// one between them and one under Search, each meeting the rail's divider.
const navLines = 3

// navRules are the lines of the rail's lines that split Home from Search
// and Search from the library: where the divider beside them has a tee.
var navRules = map[int]string{navLines: "┤", 2*navLines + 1: "┤"}

// navKey is a terminal's key for a page, as wide as the rail, its label
// in the accent while the page is shown.
func navKey(s *State, id, label string, p page) layout.Node {
	b := layout.Button{NodeBase: layout.NodeBase{ID: id, W: layout.Grow(1), H: layout.Fixed(navLines), PadLeft: 2, PadTop: 1}, Label: label, Plain: true, Style: subtle}
	if s.page == p {
		b.Style = layout.TextStyle{Bold: true, Color: layout.ColorAccent}
	}
	if s.web == nil && p == pageSearch {
		b.Disabled = true
	}
	return b
}

// terminalRoot is the signed-in screen in a terminal: sections split by
// single lines that run to the terminal's edges, which frame the whole.
func terminalRoot(s *State, width int) layout.Node {
	pal := s.look().colors()
	pad := func(n layout.Node) layout.Node {
		return layout.Col{NodeBase: layout.NodeBase{PadLeft: 1, PadRight: 1}, Children: []layout.Node{n}}
	}
	return layout.Col{Children: []layout.Node{
		terminalTopBar(s),
		layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1)}, Children: []layout.Node{
			railView(s, false),
			newVRule(navRules, pal.border),
			layout.Col{NodeBase: layout.NodeBase{ID: "center-card", W: layout.Grow(1)}, Children: []layout.Node{centerView(s, false, width)}},
		}},
		newHRule(map[int]string{termRailWidth: "┴"}, pal.border),
		pad(playerBar(s, false, width)),
	}}
}

// View draws the sign-in screen until the player is ready, then the
// library, the selected playlist, and the player bar.
func View(s *State, ctx flatte.RenderContext) flatte.Frame {
	g := ctx.Graphical
	frame := flatte.Frame{Title: "Smolify", Theme: &s.look().Design}
	switch {
	case !s.ready && s.pairing.Code != "":
		frame.Root = pairingView(s)
	case !s.ready:
		frame.Root = loadingView(s, g)
	default:
		if s.track != nil {
			frame.Title = s.track.Name + " · " + strings.Join(s.track.Artists, ", ")
		}
		frame.Root = layout.Col{NodeBase: layout.NodeBase{PadLeft: 1, PadRight: 1, PadBottom: 1}, Children: []layout.Node{
			topBar(s, g),
			layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1), Gap: 1}, Children: []layout.Node{
				railView(s, g),
				card(layout.NodeBase{ID: "center-card", W: layout.Grow(1), NoPadding: true}, centerView(s, g, ctx.Width)),
			}},
			playerBar(s, g, ctx.Width),
		}}
		if !g {
			frame.Root = terminalRoot(s, ctx.Width)
		}
		// Pictures are a terminal's choice; the half-blocks under them
		// stand alone when the user prefers them (and with images off,
		// covers carry no picture at all).
		frame.CellImages = !g && s.settings.Images != imagesPictures
		if !g && s.focus == paneSearch {
			// A blinking bar draws the eye to the search field.
			frame.CursorStyle = &flatte.CursorStyle{Shape: flatte.CursorShapeBar, Blink: true}
		}
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
		layout.Label{Wrap: true, Text: "Approve Smolify as a speaker for your account. Open catalog.com/pair and enter:"},
		layout.Label{},
		layout.Label{Text: spaced(p.Code), Selectable: true, Style: layout.TextStyle{Bold: true, Size: 34, Color: layout.ColorAccent}},
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
// the playing one in violet.
func railView(s *State, g bool) layout.Node {
	pal := s.look().colors()
	lib := s.library()
	rows := s.rail.Rows(idRail, func(i int, selected bool) layout.Node {
		pl := lib[i]
		name := layout.TextStyle{}
		playing := pl.URI == s.context && s.track != nil
		if playing {
			name.Color = layout.ColorAccent
		}
		about := "Playlist"
		if o := ownerName(pl.Owner, s.username); o != "" {
			about += " · " + o
		}
		art := thumb(s, pl.Image, 32, g)
		if s.username != "" && pl.URI == model.LikedSongs(s.username) {
			art = likedTile(s, 32, g)
		}
		row := layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Fixed(railRowLines), Gap: 1, PadLeft: 1, PadRight: 1}, Align: layout.AlignCenter, Children: []layout.Node{
			art,
			twoLines(layout.NodeBase{W: layout.Grow(1), Tooltip: pl.Name}, pl.Name, name, about, secondary),
		}}
		if g {
			row.H = layout.Points(48)
			row.PadTop, row.PadBottom = 1, 1
		}
		if playing {
			row.Children = append(row.Children, layout.Icon{Name: layout.IconSpeaker, Text: "♪ ", Style: layout.TextStyle{Color: layout.ColorAccent}})
		}
		return row
	})
	rows.ID, rows.W, rows.H = idRail, layout.Grow(1), layout.Grow(1)
	withHover(&rows, s.page == pageLibrary, g, pal)
	if !g {
		roomyRows(&rows, pal) // a line between entries, easier to read
	} // the center shows Home or search, not a list
	count := ""
	if n := len(lib); n > 0 {
		count = strconv.Itoa(n)
	}
	nav := []layout.Node{
		navItem(s, g, idHome, layout.IconHome, "⌂ ", "Home", pageHome),
		navItem(s, g, idSearch, layout.IconSearch, "⌕ ", "Search", pageSearch),
	}
	library := []layout.Node{
		libraryHeader(count, g),
		layout.Label{},
		layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1), Gap: 1}, Children: []layout.Node{rows, listBar(s.rail, idRailBar)}},
	}
	if !g { // the app's name is in the top bar; a line splits the sections
		return layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(termRailWidth)}, Children: []layout.Node{
			navKey(s, idHome, "⌂ Home", pageHome),
			newHRule(nil, pal.border),
			navKey(s, idSearch, "⌕ Search", pageSearch),
			newHRule(nil, pal.border),
			layout.Col{NodeBase: layout.NodeBase{H: layout.Grow(1), PadLeft: 1, PadRight: 1, PadTop: 1}, Children: library},
		}}
	}
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(railWidth), Gap: 1}, Children: []layout.Node{
		brandMark(g),
		card(layout.NodeBase{PadLeft: 1, PadRight: 1, PadTop: 1, PadBottom: 1, Box: layout.Box{Background: pal.rail}}, nav...),
		card(layout.NodeBase{H: layout.Grow(1), PadLeft: 1, PadRight: 1, PadTop: 1, Box: layout.Box{Background: pal.rail}}, library...),
	}}
}

// libraryHeader is the library card's title row: in a window a library
// icon, the title, and the playlist count, all on one center line.
func libraryHeader(count string, g bool) layout.Node {
	title := layout.Label{Text: "Your Library", Style: layout.TextStyle{Bold: true, Size: 14}}
	row := layout.Row{NodeBase: layout.NodeBase{Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		title, layout.NewSpacer(), layout.Label{Text: count, Style: subtle},
	}}
	if g {
		row.H = layout.Points(32)
		icon := layout.Icon{NodeBase: layout.NodeBase{W: layout.Points(24), H: layout.Points(24)}, Name: layout.IconLibrary, Text: "▥ ", Style: subtle}
		row.Children = append([]layout.Node{icon}, row.Children...)
	}
	return row
}

// narrowed is the player's timeline: in a window, the middle two thirds
// of the space between the track and the volume, so it is not as wide as
// the window.
func narrowed(g bool, timeline layout.Row) layout.Node {
	if !g {
		return timeline
	}
	timeline.W = layout.Grow(4)
	return layout.Row{Children: []layout.Node{layout.NewSpacer(), timeline, layout.NewSpacer()}}
}

// How list rows look in a window: rowGap points between their fills, and
// a two-line row's lines set at rowLeading.
const (
	rowGap     = 2
	rowLeading = 1.1
)

// twoLines is a title over its subtitle, set close (rowLeading) so the row
// holding them centers them as one block; both lines truncate.
func twoLines(nb layout.NodeBase, title string, titleStyle layout.TextStyle, sub string, subStyle layout.TextStyle) layout.Col {
	titleStyle.Leading, subStyle.Leading = rowLeading, rowLeading
	return layout.Col{NodeBase: nb, Children: []layout.Node{
		layout.Label{Text: title, Truncate: true, Style: titleStyle},
		layout.Label{Text: sub, Truncate: true, Style: subStyle},
	}}
}

// withHover gives a list's rows a hover fill and rounded corners, and
// drops their selection unless selected.
func withHover(rows *layout.Col, selected, g bool, pal colors) {
	for i, c := range rows.Children {
		if r, ok := c.(layout.Row); ok {
			r.Box.Hover, r.Box.Round, r.Box.InsetY = pal.hover, true, rowGap
			r.Box.Selected = r.Box.Selected && selected
			if r.Box.Selected && !g { // a terminal draws no selection of its own
				r.Box.Background = pal.termSelect
			}
			rows.Children[i] = r
		}
	}
}

// navItem is a rail entry for a page, white when shown. icon draws in a
// window, text in a terminal (two columns: glyph, space).
func navItem(s *State, g bool, id string, icon layout.IconName, text, label string, p page) layout.Node {
	pal := s.look().colors()
	st := subtle
	if s.page == p {
		st = layout.TextStyle{Bold: true, Color: layout.ColorPrimary}
	}
	if s.web == nil && p == pageSearch {
		st = subtle
	}
	row, icn := layout.NodeBase{ID: id, Gap: 1, PadLeft: 1, Box: layout.Box{Hover: pal.hover, Round: true}}, layout.NodeBase{}
	if g {
		row.H, icn.W, icn.H = layout.Points(32), layout.Points(24), layout.Points(24)
	}
	return layout.Row{NodeBase: row, Align: layout.AlignCenter, Children: []layout.Node{
		layout.Icon{NodeBase: icn, Name: icon, Text: text, Style: st},
		layout.Label{Text: label, Style: layout.TextStyle{Bold: true, Fg: st.Fg, Color: st.Color, Role: st.Role}},
	}}
}

// likedTile is Liked Songs' cover: a white heart on Smolify's art (in a
// terminal, which draws no backdrops, one picture of both).
func likedTile(s *State, size int, g bool) layout.Node {
	if !g {
		return picture(s, g, layout.Image{NodeBase: layout.NodeBase{W: layout.Points(size), H: layout.Points(size)}, Source: likedArt(), Text: coverText(size), Fit: layout.ImageCover})
	}
	heart := layout.Icon{NodeBase: layout.NodeBase{W: layout.Points(size * 2 / 3), H: layout.Points(size * 2 / 3)}, Name: layout.IconHeart, Text: "♥ ", Style: layout.TextStyle{Fg: white}}
	return layout.Row{NodeBase: layout.NodeBase{W: layout.Points(size), H: layout.Points(size), Box: layout.Box{Background: artVeil, Round: true, Backdrop: &layout.Backdrop{Source: smolArt()}}}, Align: layout.AlignCenter, Children: []layout.Node{
		layout.NewSpacer(), heart, layout.NewSpacer(),
	}}
}

// brandMark identifies the app above the sidebar navigation.
func brandMark(g bool) layout.Node {
	name := layout.Label{Text: "Smolify", Style: layout.TextStyle{Bold: true, Color: layout.ColorAccent}}
	mark := layout.Row{NodeBase: layout.NodeBase{ID: "brand", PadLeft: 2, Gap: 1}, Align: layout.AlignCenter}
	if g {
		mark.H = layout.Points(44)
		name.Style = layout.TextStyle{Bold: true, Size: 17, Color: layout.ColorPrimary}
		mark.Children = append(mark.Children, layout.Image{NodeBase: layout.NodeBase{W: layout.Points(logoPoints), H: layout.Points(logoPoints)}, Source: logo(), Fit: layout.ImageContain})
	}
	mark.Children = append(mark.Children, name)
	return mark
}

// framedPlayerColumns is the terminal width from which the player's
// controls are framed buttons with their icons drawn as shapes; narrower,
// single glyphs.
const framedPlayerColumns = 120

// worded gives a terminal icon button a word after its glyph: one cell
// of symbol is hard to see and to hit. A GUI draws the icon alone.
func worded(b layout.Button, g bool, word string) layout.Button {
	if g {
		return b
	}
	b.Label += " " + word
	b.W, b.H = layout.Auto(), layout.Fixed(1)
	return b
}

// topBar sits in the window's title strip, right of its buttons: back
// and forward, and the search field in the middle.
// barLines is the terminal top bar's height above the divider under it.
const barLines = 3

// historyColumns is a back or forward key's width: its side, then an odd
// number of columns about twice the bar's height.
const historyColumns = 1 + 2*barLines + 1

// terminalTopBar is the top bar in a terminal: the logo and name over the
// rail, then the history keys, the search field, and Settings. The bar
// shares its borders: the terminal's top and right edges, one line between
// two keys, the divider under it as their bottom edge (┴), and the rail's
// divider as the first key's left side (┼ where they meet).
func terminalTopBar(s *State) layout.Node {
	pal := s.look().colors()
	centered := func(n layout.Node) layout.Node {
		return layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: []layout.Node{layout.NewSpacer(), n, layout.NewSpacer()}}
	}
	// key is one of the bar's keys: its left side, its content, and its
	// stretch of the divider, from junction on.
	// Keys are three lines tall, their content on the middle one: room
	// from the terminal's top edge and from the divider, and a bigger
	// target.
	key := func(id string, w layout.Size, junction string, content layout.Node) layout.Node {
		return layout.Col{NodeBase: layout.NodeBase{ID: id, W: w, H: layout.Fixed(barLines + 1)}, Children: []layout.Node{
			layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Fixed(barLines)}, Children: []layout.Node{
				keySide{color: pal.border},
				layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), PadTop: barLines - 2}, Children: []layout.Node{content}},
			}},
			newBarFill(layout.Grow(1), junction, pal.border).withHeight(1),
		}}
	}
	history := func(arrow string, can bool) layout.Node {
		style := layout.TextStyle{Bold: true}
		if !can {
			style = subtle
		}
		return centered(layout.Label{Text: arrow, Style: style})
	}
	focused := s.focus == paneSearch
	field := s.query.Layout(idQuery, focused)
	field.Clearable = true
	field.Placeholder = "What do you want to play?"
	field.W = layout.Grow(1)
	icon := layout.Icon{NodeBase: layout.NodeBase{H: layout.Fixed(1)}, Name: layout.IconSearch, Text: "⌕ ", Style: subtle}
	if focused {
		icon.Style = layout.TextStyle{Color: layout.ColorAccent}
	}
	search := layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), Gap: 1, PadLeft: 1, PadRight: 1}, Children: []layout.Node{icon, field}}
	settings := layout.Label{Text: "⚙ Settings", Style: layout.TextStyle{Bold: true}}
	if s.page == pageSettings {
		settings.Style.Color = layout.ColorAccent
	}
	brandMark := layout.Col{NodeBase: layout.NodeBase{ID: "brand", W: layout.Fixed(termRailWidth), H: layout.Fixed(barLines + 1)}, Children: []layout.Node{
		layout.Row{NodeBase: layout.NodeBase{H: layout.Fixed(barLines), PadLeft: 1, PadTop: barLines - 2, Gap: 1}, Children: []layout.Node{
			// The logo a line tall, square on screen, beside the name.
			picture(s, false, layout.Image{NodeBase: layout.NodeBase{W: layout.Fixed(2), H: layout.Fixed(1)}, Source: logo(), Text: "♫", Fit: layout.ImageContain}),
			layout.Label{Text: "Smolify", Style: layout.TextStyle{Bold: true, Color: layout.ColorAccent}},
		}},
		newBarFill(layout.Grow(1), "", pal.border).withHeight(1),
	}}
	return layout.Row{NodeBase: layout.NodeBase{H: layout.Fixed(barLines + 1)}, Children: []layout.Node{
		brandMark,
		// About square on screen (twice as many columns inside as lines),
		// odd inside so the arrow centers.
		key(idBack, layout.Fixed(historyColumns), "┼", history("❮", len(s.back) > 0)),
		key(idForward, layout.Fixed(historyColumns), "┴", history("❯", len(s.forward) > 0)),
		key(idSearchBox, layout.Grow(1), "┴", search),
		key(idSettings, layout.Fixed(14), "┴", centered(settings)),
	}}
}

func topBar(s *State, g bool) layout.Node {
	pal := s.look().colors()
	nav := func(id string, icon layout.IconName, label, tip string, can bool) layout.Node {
		b := layout.Button{NodeBase: layout.NodeBase{ID: id, Tooltip: tip, W: layout.Points(32), H: layout.Points(32)}, Icon: icon, Label: label, Plain: true, Disabled: !can}
		if !g {
			b.H = layout.Fixed(1) // centered on the framed search field's line
			b.Label = map[string]string{"‹": "←", "›": "→"}[label]
		}
		return b
	}
	focused := s.focus == paneSearch
	field := s.query.Layout(idQuery, focused)
	field.Clearable = true
	field.Placeholder = "What do you want to play?"
	icon := layout.Icon{NodeBase: layout.NodeBase{W: layout.Points(24), H: layout.Points(24)}, Name: layout.IconSearch, Text: "⌕ ", Style: subtle}
	// One rounded box holds the icon and the field; it lights up while the
	// field has the keyboard. A terminal draws the two side by side.
	var search layout.Node = layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{icon, field}}
	bar := layout.NodeBase{Gap: 1}
	// The rail's slot: empty under a window's buttons, the app's name in a
	// terminal, which has none.
	var lead layout.Node = layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(rail(g))}}
	if !g {
		lead = layout.Row{NodeBase: layout.NodeBase{ID: "brand", W: layout.Fixed(rail(g)), PadLeft: 1, Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
			// The logo as tall as the bar (square on screen), beside the name.
			picture(s, g, layout.Image{NodeBase: layout.NodeBase{W: layout.Fixed(6), H: layout.Fixed(3)}, Source: logo(), Text: "♫", Fit: layout.ImageContain}),
			layout.Label{Text: "Smolify", Style: layout.TextStyle{Bold: true, Color: layout.ColorAccent}},
		}}
		// A framed field, lit while it has the keyboard.
		icon.W, icon.H = layout.Auto(), layout.Fixed(1)
		box := layout.Box{Border: layout.BorderRounded, BorderColor: pal.border}
		if focused {
			box.BorderColor, icon.Style = pal.accent, layout.TextStyle{Color: layout.ColorAccent}
		}
		search = layout.Row{NodeBase: layout.NodeBase{ID: idSearchBox, W: layout.Grow(1), Gap: 1, PadLeft: 1, PadRight: 1, Box: box}, Align: layout.AlignCenter, Children: []layout.Node{icon, field}}
	}
	if g {
		bar.H = layout.Points(48)
		field.Bare = true
		box := layout.Box{Background: pal.search, Hover: pal.hover, Round: true}
		if focused {
			box.Background, icon.Style = pal.searchFocus, layout.TextStyle{Color: layout.ColorPrimary}
		}
		search = layout.Row{NodeBase: layout.NodeBase{ID: idSearchBox, W: layout.Fixed(52), H: layout.Points(34), Gap: 1, PadLeft: 1, PadRight: 1, Box: box}, Align: layout.AlignCenter, Children: []layout.Node{icon, field}}
	}
	gear := layout.Button{NodeBase: layout.NodeBase{ID: idSettings, Tooltip: "Settings (⌘,)", W: layout.Points(32), H: layout.Points(32)}, Icon: layout.IconGear, Label: "⚙", Plain: true}
	gear = worded(gear, g, "Settings")
	// Settings stays at the right edge.
	settingsSlot := layout.Row{NodeBase: layout.NodeBase{W: layout.Points(72)}, Align: layout.AlignCenter, Children: []layout.Node{layout.NewSpacer(), gear}}
	if !g {
		settingsSlot.W = layout.Auto()
	}
	if s.page == pageSettings {
		gear.Style = layout.TextStyle{Color: layout.ColorAccent}
	}

	var searchArea layout.Node = search
	if g {
		searchArea = layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Align: layout.AlignCenter, Children: []layout.Node{layout.NewSpacer(), search, layout.NewSpacer()}}
	}

	return layout.Row{NodeBase: bar, Align: layout.AlignCenter, Children: []layout.Node{
		lead,
		nav(idBack, layout.IconChevronLeft, "‹", "Back (⌘[)", len(s.back) > 0),
		nav(idForward, layout.IconChevronRight, "›", "Forward (⌘])", len(s.forward) > 0),
		searchArea,
		settingsSlot,
	}}
}

// centerView shows the page: the selected list, Home, or search.
func centerView(s *State, g bool, width int) layout.Node {
	pal := s.look().colors()
	switch s.page {
	case pageHome:
		return homeView(s, g, width)
	case pageSearch:
		return virtualView(s, g, width, "Search", "Songs", nil)
	case pageSettings:
		return settingsView(s, g, width)
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
	if stage := s.likedLoads[pl.URI]; stage != nil && stage.total > 0 {
		n = stage.total
	}
	stage := s.likedLoads[pl.URI]
	isPagedLiked := likedURI(s, pl.URI) && hasLikedTracks(s)
	meta := []layout.Run{{Text: plural(n, "song"), Style: subtle}}
	if isPagedLiked && !listed && (stage == nil || stage.total == 0) {
		meta = []layout.Run{{Text: "Loading songs…", Style: subtle}}
	} else if stage != nil && stage.total > 0 {
		meta = append(meta, layout.Run{Text: fmt.Sprintf(" · %d loaded", len(stage.tracks)), Style: subtle})
	} else if listed && l.Complete() {
		meta = append(meta, layout.Run{Text: " · " + total(l.Tracks), Style: subtle})
	} else if isPagedLiked && listed && l.Length > len(l.Tracks) {
		meta = append(meta, layout.Run{Text: fmt.Sprintf(" · %d loaded", len(l.Tracks)), Style: subtle})
	} else if isPagedLiked && stage != nil {
		meta = append(meta, layout.Run{Text: " · refreshing", Style: subtle})
	}
	if isPagedLiked && stage != nil && stage.total == 0 {
		if listed {
			meta = []layout.Run{{Text: plural(l.Length, "song") + " · refreshing", Style: subtle}}
		} else {
			meta = []layout.Run{{Text: "Loading songs…", Style: subtle}}
		}
	}
	if stage != nil && stage.err != "" {
		meta = append(meta, layout.Run{Text: " · retrying", Style: subtle})
	}
	if o := ownerName(pl.Owner, s.username); o != "" {
		meta = append([]layout.Run{{Text: o, Style: strong}, {Text: " · ", Style: subtle}}, meta...)
	}
	info := []layout.Node{
		layout.Label{Text: browseHeading(s), Style: layout.TextStyle{Bold: true, Size: 12, Leading: 1.1}},
	}
	if !g { // a line of room between the kind and the name
		info = append(info, layout.Label{})
	}
	info = append(info,
		layout.Label{NodeBase: layout.NodeBase{ID: "playlist-title", Tooltip: pl.Name}, Text: pl.Name, Truncate: true, Style: layout.TextStyle{Bold: true, Size: 30, Color: layout.ColorPrimary, Leading: 1.05}},
	)
	descriptionStyle := subtle
	descriptionStyle.Leading = 1.1
	if pl.Description != "" {
		info = append(info, layout.Label{NodeBase: layout.NodeBase{ID: "playlist-description"}, Text: pl.Description, Truncate: true, Style: descriptionStyle})
	}
	info = append(info, layout.Label{NodeBase: layout.NodeBase{ID: "playlist-summary"}, Runs: meta, Style: layout.TextStyle{Leading: 1.1}}, layout.NewSpacer())
	shuffle := subtle
	if s.shuffle {
		shuffle = layout.TextStyle{Color: layout.ColorAccent}
	}
	playlistPlay := layout.Button{NodeBase: layout.NodeBase{ID: idPlayList, Tooltip: "Play this playlist", W: layout.Points(pick(g, 40, 32)), H: layout.Points(pick(g, 40, 32))}, Icon: layout.IconPlay, Label: "▶", Default: true}
	if loadingPlaylist(s, pl.URI) {
		playlistPlay.Icon, playlistPlay.Label, playlistPlay.Disabled = layout.IconNone, loadingIndicator(s), true
		playlistPlay.Tooltip = "Loading playback…"
		playlistPlay.Plain = !g
		playlistPlay.Style = layout.TextStyle{Color: layout.ColorPrimary, Size: float64(pick(g, 22, 0))}
	}
	actions := layout.Row{NodeBase: layout.NodeBase{ID: "playlist-actions", Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		playlistPlay,
		worded(layout.Button{NodeBase: layout.NodeBase{ID: idShufflePlay, Tooltip: "Shuffle (s)", W: layout.Points(32), H: layout.Points(32)}, Icon: layout.IconShuffle, Label: "⇄", Plain: true, Style: shuffle}, g, "Shuffle"),
	}}
	if g {
		actions.Children = append(actions.Children, layout.NewSpacer(), filterView(s, g), sortView(s))
	}
	if !g && !playlistPlay.Disabled {
		// Rounded keys, as the player's: the play key lit.
		for i, c := range actions.Children {
			b := c.(layout.Button)
			b.H, b.PadLeft, b.PadRight = layout.Fixed(3), 1, 1
			if b.W.Kind == layout.SizePoints {
				b.W = layout.Auto()
			}
			b.Box = layout.Box{Border: layout.BorderRounded, BorderColor: pal.border}
			if b.ID == idPlayList {
				// About square on screen (a cell is twice as tall as
				// wide), the glyph centered.
				b.W = layout.Fixed(7)
				b.Box.BorderColor, b.Default = pal.accent, false
				b.Plain, b.Style = true, layout.TextStyle{Bold: true, Color: layout.ColorPrimary}
			}
			actions.Children[i] = b
		}
	}
	// A wide terminal keeps the song tools on the actions' row, so no
	// empty band sits between the cover and the songs.
	toolsBeside := !g && width >= headerToolsColumns
	if toolsBeside {
		actions.Children = append(actions.Children, layout.NewSpacer(), filterView(s, g), framedSort(s))
		actions.W = layout.Grow(1)
	}
	info = append(info, actions)
	size := pick(g, headerCover, 128) // 16×8 cells: room for half-block art
	art, wash := cover(s, pl.Image, size, g), s.covers[pl.Image]
	if pl.Image == "" {
		wash = smolArt()
	}
	if s.username != "" && pl.URI == model.LikedSongs(s.username) {
		art, wash = likedTile(s, size, g), smolArt()
	}
	box := layout.Box{Background: pal.tint(s.tints[pl.Image]), Round: true}
	if wash != nil {
		// The cover itself, blurred to a wash and dimmed so white text
		// reads on it; a tint would flatten it to one color.
		box.Backdrop = &layout.Backdrop{Source: wash, Blur: 40}
		box.Background = pal.dim
	}
	if !g {
		// A terminal draws no blurred wash: the dimming alone would be a
		// dark band around the cover.
		box = layout.Box{}
	}
	pad := pick(g, 2, 0)
	infoHeight := layout.Auto()
	if g {
		infoHeight = layout.Points(size)
	}
	header := layout.Row{NodeBase: layout.NodeBase{ID: "playlist-header", NoPadding: true, Gap: 3, PadLeft: pick(g, 2, 1), PadRight: 2, PadTop: pad, PadBottom: pad, Box: box}, Children: []layout.Node{
		art,
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), H: infoHeight}, Children: info},
	}}
	var note layout.Node = layout.Label{}
	switch {
	case s.page == pageBrowse && s.discovery.Pages[s.shown].Err != "":
		why := "Could not load this page."
		if err := s.discovery.Pages[s.shown].Err; !isTransient(err) {
			why = "Could not load this page: " + err
		}
		note = layout.Row{NodeBase: layout.NodeBase{NoPadding: true, Gap: 1}, Children: []layout.Node{layout.Label{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Text: why, Wrap: true, Style: subtle}, layout.Button{NodeBase: layout.NodeBase{ID: "browse-retry"}, Label: "Retry"}}}
	case g && s.settings.NotifyErrors: // a window tells these as notifications
	case s.status != "":
		note = layout.Label{Text: s.status, Style: layout.TextStyle{Color: layout.ColorDanger}}
	case s.notice != "":
		note = layout.Label{Text: s.notice, Style: subtle}
	}
	children := []layout.Node{header}
	if !g && !toolsBeside {
		children = append(children, layout.Row{NodeBase: layout.NodeBase{ID: "song-tools", PadLeft: 1, PadRight: 1, Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{layout.NewSpacer(), filterView(s, g), framedSort(s)}})
	}
	children = append(children, layout.Col{NodeBase: layout.NodeBase{PadLeft: 1, PadRight: 1, H: layout.Grow(1)}, Children: []layout.Node{note, trackTable(s, l, listed, g, width)}})
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Grow(1)}, Children: children}
}

// virtualView is Home or search: a title, a line or field under it, and
// the tracks.
func virtualView(s *State, g bool, width int, title, sub string, field layout.Node) layout.Node {
	head := []layout.Node{layout.Label{Text: title, Style: layout.TextStyle{Bold: true, Size: 30, Color: layout.ColorPrimary}}}
	if sub != "" {
		head = append(head, layout.Label{Text: sub, Style: subtle})
	}
	if field != nil {
		head = append(head, layout.Label{}, field)
	}
	switch {
	case g && s.settings.NotifyErrors: // a window tells these as notifications
	case s.status != "":
		head = append(head, layout.Label{}, layout.Label{Text: s.status, Wrap: true, Style: layout.TextStyle{Color: layout.ColorDanger}})
	case s.notice != "":
		head = append(head, layout.Label{}, layout.Label{Text: s.notice, Wrap: true, Style: subtle})
	}
	l, listed := s.listings[s.shown]
	var table layout.Node
	switch {
	case s.web == nil:
		table = layout.Label{Text: "Not available.", Style: subtle}
	case s.page == pageSearch && strings.TrimSpace(s.query.Value) == "":
		table = layout.Label{Text: "Type to search songs.", Style: subtle}
	case s.page == pageSearch && s.searchFailed:
		table = layout.Label{Text: "Search failed. Press Enter to retry.", Style: subtle}
	case s.page == pageSearch && s.searchPending:
		table = layout.Label{Text: "Searching…", Style: subtle}
	case s.page == pageHome && !listed:
		table = layout.Label{Text: "Loading…", Style: subtle}
	case l.Length == 0 && s.page == pageSearch:
		table = layout.Label{Text: "Nothing found.", Style: subtle}
	default:
		table = trackTable(s, l, listed, g, width)
	}
	children := append(head, layout.Label{}, table)
	return layout.Col{NodeBase: layout.NodeBase{PadLeft: 2, PadRight: 2, PadTop: 2, W: layout.Grow(1), H: layout.Grow(1)}, Children: children}
}

// trackTable is the selected list's tracks: number, title and artists,
// album, liked, and duration.
func trackTable(s *State, l model.Listing, listed bool, g bool, width int) layout.Node {
	pal := s.look().colors()
	if stage := s.likedLoads[s.shown]; stage != nil {
		if !listed && stage.total == 0 && stage.offset == 0 && len(stage.tracks) == 0 {
			if stage.err != "" {
				return layout.Label{Text: "Could not load Liked Songs: " + stage.err + " (retrying)", Wrap: true, Style: subtle}
			}
			return layout.Label{Text: "Loading Liked Songs…", Style: subtle}
		}
		if !listed {
			l = model.Listing{URI: s.shown, Ready: true, Length: stage.total, Cached: len(stage.tracks), Tracks: stage.tracks}
			listed = true
		}
	}
	switch {
	case !listed || !l.Ready && len(l.Tracks) == 0:
		return layout.Label{Text: "Listing the tracks…", Style: subtle}
	case l.Length == 0:
		return layout.Label{Text: "No tracks here yet.", Style: subtle}
	}
	source := l.Tracks
	l = displayListing(s, l)
	numbers := originalPositionNumbers(source, l.Tracks, sortMode(s) == sortPositionDesc && !virtual(l.URI))
	if (s.page == pageLibrary || s.page == pageBrowse) && strings.TrimSpace(s.trackFilter.Value) != "" && len(l.Tracks) == 0 {
		if l.Complete() {
			return layout.Label{Text: "No songs match your filter.", Style: subtle}
		}
		return layout.Label{Text: "No matches in loaded songs yet; still listing.", Style: subtle}
	}
	dateColumn := g && width >= 90 && likedURI(s, l.URI) && hasLikedTracks(s)
	dateHead := layout.Node(layout.Label{})
	if dateColumn {
		dateHead = layout.Label{NodeBase: layout.NodeBase{ID: "track-date-heading"}, Text: "Date added", Style: caption}
	}
	header := s.trackCells(g, dateColumn, layout.Label{Text: "#", Style: caption}, layout.Label{}, layout.Label{Text: "Title", Style: caption},
		layout.Label{Text: "Album", Style: caption}, dateHead, layout.Icon{}, layout.Icon{Name: layout.IconClock, Text: "◷ ", Style: caption})
	header.H = layout.Size{} // the headings' own height, not a row's
	rule := layout.Label{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Text: strings.Repeat("─", 400), Style: layout.TextStyle{Fg: pal.rule}}
	rows := s.tracks.Rows(idTracks, func(i int, _ bool) layout.Node {
		if i >= len(l.Tracks) {
			return layout.Label{}
		}
		return trackRow(s, numbers[i]-1, l.Tracks[i], g, dateColumn)
	})
	rows.ID, rows.W, rows.H = idTracks, layout.Grow(1), layout.Grow(1)
	withHover(&rows, true, g, pal)
	if !g && !s.compact {
		roomyRows(&rows, pal) // a line between songs, easier to read
	}
	return layout.Col{NodeBase: layout.NodeBase{H: layout.Grow(1)}, Children: []layout.Node{
		header,
		rule,
		layout.Row{NodeBase: layout.NodeBase{H: layout.Grow(1), Gap: 1}, Children: []layout.Node{rows, listBar(s.tracks, idTrackBar)}},
	}}
}

// trackCells lays out one row of the table: number, cover (unless
// compact), title, album, liked, and duration.
func (s *State) trackCells(g, dateColumn bool, num, art, title, album, date, liked, dur layout.Node) layout.Row {
	cells := []layout.Node{
		layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(3)}, Children: []layout.Node{layout.NewSpacer(), num}},
	}
	if !s.compact {
		cells = append(cells, layout.Col{NodeBase: layout.NodeBase{W: layout.Points(32)}, Children: []layout.Node{art}})
	}
	cells = append(cells,
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(3)}, Children: []layout.Node{title}},
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(2)}, Children: []layout.Node{album}},
	)
	if dateColumn {
		// Dates keep the same readable width in both row densities.
		cells = append(cells, layout.Col{NodeBase: layout.NodeBase{W: layout.Points(112)}, Children: []layout.Node{date}})
	}
	cells = append(cells,
		layout.Col{NodeBase: layout.NodeBase{W: layout.Fixed(2)}, Children: []layout.Node{liked}},
		layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(5)}, Children: []layout.Node{layout.NewSpacer(), dur}},
	)
	// As tall as the list pages by (trackRowLines), with the cells centered
	// in it.
	row := layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Fixed(s.trackRowLines()), Gap: 2, PadLeft: 1, PadRight: 2}, Align: layout.AlignCenter, Children: cells}
	if g {
		row.H = layout.Points(pick(s.compact, 30, 48))
		row.PadTop, row.PadBottom = 1, 1
	}
	return row
}

// trackRow is track t at its original zero-based playlist position i: in two lines with its cover (one when
// compact); the playing one washed in violet with bars for its number.
func trackRow(s *State, i int, t model.Track, g, dateColumn bool) layout.Node {
	pal := s.look().colors()
	num := layout.Node(layout.Label{Text: strconv.Itoa(i + 1), Style: subtle})
	requested := pendingTrack(s, t, s.shown)
	if requested {
		num = layout.Label{Text: loadingIndicator(s), Style: layout.TextStyle{Color: layout.ColorAccent, Size: float64(pick(g, 18, 0))}}
	}
	if t.Name == "" {
		placeholder := "…" // still being listed
		if s.listings[s.shown].Complete() {
			placeholder = "Unavailable" // listed, but Spotify has nothing on it
		}
		row := s.trackCells(g, dateColumn, num, layout.Label{}, layout.Label{Text: placeholder, Style: subtle}, layout.Label{}, layout.Label{}, layout.Icon{}, layout.Label{})
		if requested {
			row.Box = layout.Box{Background: pal.playing, Interactive: true, Round: true, InsetY: rowGap}
		}
		return row
	}
	name := layout.TextStyle{Leading: rowLeading}
	playing := !loadingPlay(s) && s.track != nil && s.track.URI == t.URI && (s.context == s.shown || virtual(s.shown))
	if playing {
		num = layout.Icon{Name: layout.IconEqualizer, Text: "♪ ", Style: layout.TextStyle{Color: layout.ColorAccent}, Animate: s.playing}
		if !g { // three bars, flush right with the numbers, moving while it plays
			num = layout.Label{Text: equalizerBars(s.eqFrame, s.playing), Style: layout.TextStyle{Color: layout.ColorAccent}}
		}
		name.Color = layout.ColorAccent
	}
	if requested {
		name.Color = layout.ColorAccent
	}
	artists := strings.Join(t.Artists, ", ")
	tip := layout.NodeBase{Tooltip: t.Name + " — " + artists}
	var title layout.Node
	if s.compact {
		title = layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), NoPadding: true, Tooltip: tip.Tooltip}, Children: []layout.Node{
			layout.Label{NodeBase: layout.NodeBase{NoPadding: true}, Text: t.Name + "  ", Truncate: true, Style: name},
			artistLine(t, i, false, pal),
		}}
	} else {
		title = layout.Col{NodeBase: tip, Children: []layout.Node{
			layout.Label{NodeBase: layout.NodeBase{NoPadding: true}, Text: t.Name, Truncate: true, Style: name},
			artistLine(t, i, true, pal),
		}}
	}
	albumBase := layout.NodeBase{Tooltip: t.Album, NoPadding: true}
	if strings.HasPrefix(t.AlbumURI, "spotify:album:") {
		albumBase.ID = trackLinkID(t.AlbumURI, i, "album")
		albumBase.Tooltip = "Open " + t.Album
		albumBase.Box = layout.Box{Interactive: true, Round: true, Hover: color.Transparent}
	}
	album := layout.Label{NodeBase: albumBase, Text: t.Album, Truncate: true, Style: subtle}
	if albumBase.ID != "" {
		album.HoverFg = pal.accent
	}
	date := layout.Node(layout.Label{})
	if dateColumn {
		date = layout.Label{NodeBase: layout.NodeBase{Tooltip: addedDateTooltip(t.AddedAt)}, Text: addedDate(t.AddedAt), Style: subtle}
	}
	row := s.trackCells(g, dateColumn, num, thumb(s, t.Cover, 32, g), title, album, date, rowHeart(s.liked[t.URI], t.URI), layout.Label{Text: clock(t.Duration), Style: subtle})
	if playing || requested {
		row.Box = layout.Box{Background: pal.playing, Interactive: true, Round: true, InsetY: rowGap}
	}
	return row
}

// artistLine keeps each visible artist independently targetable while using
// the same comma-separated text, truncation, and subdued style as the row.
func artistLine(t model.Track, row int, fill bool, pal colors) layout.Node {
	style := secondary
	style.Leading = rowLeading
	base := layout.NodeBase{NoPadding: true}
	if fill {
		base.W = layout.Grow(1)
	}
	children := make([]layout.Node, 0, len(t.Artists))
	for i, name := range t.Artists {
		text := name
		if i > 0 {
			text = ", " + text
		}
		nb := layout.NodeBase{NoPadding: true}
		if i < len(t.ArtistURIs) && strings.HasPrefix(t.ArtistURIs[i], "spotify:artist:") {
			nb.ID = trackLinkID(t.ArtistURIs[i], row, strconv.Itoa(i))
			nb.Tooltip = "Open " + name
			nb.Box = layout.Box{Interactive: true, Round: true, Hover: color.Transparent}
		}
		label := layout.Label{NodeBase: nb, Text: text, Truncate: true, Style: style}
		if nb.ID != "" {
			label.HoverFg = pal.accent
		}
		children = append(children, label)
	}
	if len(children) == 0 {
		return layout.Label{NodeBase: base, Text: "", Style: style}
	}
	return layout.Row{NodeBase: base, Children: children}
}

// heart marks a liked track. With an id it is the like control, an outline
// when not liked.
// idRowLike prefixes a song row's heart: the track's URI follows, so a
// click likes or unlikes that song without playing or selecting it.
const idRowLike = "row-like:"

// rowHeart is a song row's heart: filled when liked, empty when not,
// either a click away from the other.
func rowHeart(liked bool, uri string) layout.Node {
	icon := layout.Icon{Name: layout.IconHeartOutline, Text: "♡ ", Style: subtle}
	tip := "Save to Liked Songs"
	if liked {
		icon = layout.Icon{Name: layout.IconHeart, Text: "♥ ", Style: layout.TextStyle{Color: layout.ColorAccent}}
		tip = "Remove from Liked Songs"
	}
	if !strings.HasPrefix(uri, "spotify:track:") {
		return layout.Icon{Text: "  "}
	}
	return layout.Row{NodeBase: layout.NodeBase{ID: idRowLike + uri, Tooltip: tip, W: layout.Fixed(2)}, Children: []layout.Node{icon}}
}

func heart(liked bool, id string) layout.Node {
	if id == "" {
		if liked {
			return layout.Icon{Name: layout.IconHeart, Text: "♥ ", Style: layout.TextStyle{Color: layout.ColorAccent}}
		}
		return layout.Icon{Text: "  "}
	}
	b := layout.Button{NodeBase: layout.NodeBase{ID: id, Tooltip: "Save to Liked Songs (l)", W: layout.Points(24), H: layout.Points(24)},
		Icon: layout.IconHeartOutline, Label: "♡", Plain: true, Style: subtle}
	if liked {
		b.Tooltip, b.Icon, b.Label, b.Style = "Remove from Liked Songs (l)", layout.IconHeart, "♥", layout.TextStyle{Color: layout.ColorAccent}
	}
	return b
}

// thumb is the row thumbnail of the cover at url, size points square.
func thumb(s *State, url string, size int, g bool) layout.Node {
	if im := s.thumbs[url]; im != nil {
		return picture(s, g, layout.Image{NodeBase: layout.NodeBase{W: layout.Points(size), H: layout.Points(size), Box: layout.Box{Round: true}}, Source: im, Text: coverText(size), Fit: layout.ImageCover})
	}
	return cover(&State{settings: s.settings}, url, size, g) // a loading tile, or Smolify's art
}

// total is how long tracks last together: "52 min" or "3 h 12 min".
func total(tracks []model.Track) string {
	var d time.Duration
	for _, t := range tracks {
		d += t.Duration
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return strconv.Itoa(m) + " min"
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

// picture is im, or in a terminal with images turned off its text alone.
func picture(s *State, g bool, im layout.Image) layout.Image {
	if !g && s.settings.Images == imagesOff {
		im.Source = nil
	}
	return im
}

// cover is the image at url, size points square — in a terminal (which
// shows an image's text) and until it loads, a ♫ on a tile; with no url
// (a playlist without a cover), the ♫ on Smolify's own art.
func cover(s *State, url string, size int, g bool) layout.Node {
	pal := s.look().colors()
	// A blank picture behind the tile rounds it as the covers it stands for.
	nb := layout.NodeBase{W: layout.Points(size), H: layout.Points(size), Box: layout.Box{Background: pal.tile, Round: true, Backdrop: &layout.Backdrop{Source: blankArt(pal.tile)}}}
	if im := s.covers[url]; im != nil {
		return picture(s, g, layout.Image{NodeBase: layout.NodeBase{W: nb.W, H: nb.H, Box: layout.Box{Round: true}}, Source: im, Text: coverText(size), Fit: layout.ImageCover})
	}
	if url == "" && !g { // a terminal draws no backdrops: the art as a picture
		return picture(s, g, layout.Image{NodeBase: layout.NodeBase{W: nb.W, H: nb.H}, Source: smolArt(), Text: coverText(size), Fit: layout.ImageCover})
	}
	style := subtle
	if url == "" {
		nb.Box.Background, nb.Box.Backdrop = artVeil, &layout.Backdrop{Source: smolArt()}
		style = layout.TextStyle{Fg: white, Size: float64(size) / 3} // near the heart on Liked Songs
		if size < 3*layout.PointsPerLine {
			style.Size = 0
		}
	}
	note := layout.Row{Children: []layout.Node{layout.NewSpacer(), layout.Label{Text: "♫", Style: style}, layout.NewSpacer()}}
	if size < 3*layout.PointsPerLine {
		note.W = layout.Grow(1)
		return layout.Row{NodeBase: nb, Align: layout.AlignCenter, Children: []layout.Node{note}}
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
func playerBar(s *State, g bool, width int) layout.Node {
	pal := s.look().colors()
	title, artists := "Nothing playing", "Pick a playlist and press space"
	coverURL := ""
	if t := s.track; t != nil {
		title, artists, coverURL = t.Name, strings.Join(t.Artists, ", "), t.Cover
	}
	framed := !g && width >= framedPlayerColumns
	// In a terminal the cover is as tall as the timeline and the controls
	// under it together: five lines beside framed keys, three beside plain.
	coverSize := 48
	switch {
	case g:
	case framed:
		coverSize = 5 * layout.PointsPerLine
	default:
		coverSize = 3 * layout.PointsPerLine
	}
	now := layout.Row{NodeBase: layout.NodeBase{W: layout.Fixed(rail(g)), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		cover(s, coverURL, coverSize, g),
		layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: []layout.Node{
			layout.Label{Text: title, Truncate: true, Style: strong},
			layout.Label{Text: artists, Truncate: true, Style: secondary},
		}},
	}}
	if t := s.track; t != nil && strings.HasPrefix(t.URI, "spotify:track:") {
		now.Children = append(now.Children, heart(s.liked[t.URI], idLike))
	}

	sq := func(id, tip string, pts int) layout.NodeBase {
		return layout.NodeBase{ID: id, Tooltip: tip, W: layout.Points(pts), H: layout.Points(pts)}
	}
	mode := func(on bool) layout.TextStyle {
		if on {
			return layout.TextStyle{Color: layout.ColorAccent}
		}
		return subtle
	}
	play := layout.Button{NodeBase: sq(idPlay, "Play/Pause (space)", pick(g, 36, 32)), Icon: layout.IconPlay, Label: "▶", Default: true, Focused: s.focus == panePlayer}
	if s.playing {
		play.Icon, play.Label = layout.IconPause, "⏸"
	}
	if loadingPlay(s) {
		play.Icon, play.Label, play.Disabled = layout.IconNone, loadingIndicator(s), true
		play.Tooltip = "Loading playback…"
		play.Plain = !g
		play.Style = layout.TextStyle{Color: layout.ColorPrimary, Size: float64(pick(g, 22, 0))}
	}
	skip := subtle // a terminal's glyphs are small enough: full brightness
	if !g {
		skip = layout.TextStyle{}
	}
	controls := layout.Row{NodeBase: layout.NodeBase{ID: "player-controls", Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		layout.NewSpacer(),
		layout.Button{NodeBase: sq(idShuffle, "Shuffle (s)", 32), Icon: layout.IconShuffle, Label: "⇄", Plain: true, Style: mode(s.shuffle)},
		layout.Button{NodeBase: sq(idPrev, "Previous (p)", 32), Icon: layout.IconPrevious, Label: "⏮", Plain: true, Style: skip},
		play,
		layout.Button{NodeBase: sq(idNext, "Next (n)", 32), Icon: layout.IconNext, Label: "⏭", Plain: true, Style: skip},
		layout.Button{NodeBase: sq(idRepeat, repeatTip(s.repeat), 32), Icon: repeatIcon(s.repeat), Label: repeatLabel(s.repeat), Plain: true, Style: mode(s.repeat.Context || s.repeat.Track)},
		layout.NewSpacer(),
	}}

	if framed {
		controls.Gap = 2
		// A terminal glyph is one cell: framed with room inside, Flatte
		// draws each icon's shape there instead (as a picture where the
		// terminal shows them), the play button's lit like a key.
		for i, c := range controls.Children {
			b, ok := c.(layout.Button)
			if !ok {
				continue
			}
			// Square on screen (a cell is twice as tall as it is wide), with
			// room inside for the icon's picture.
			b.W, b.H = layout.Fixed(8), layout.Fixed(4)
			b.Box = layout.Box{Border: layout.BorderRounded, BorderColor: pal.border}
			if b.ID == idPlay {
				b.Box.BorderColor, b.Default = pal.accent, false
				b.Plain, b.Style = true, layout.TextStyle{Bold: true, Color: layout.ColorPrimary}
			}
			controls.Children[i] = b
		}
	}
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
	timeline := layout.Row{NodeBase: layout.NodeBase{ID: "player-timeline", Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		layout.Label{Text: clock(pos), Style: subtle},
		layout.Slider{NodeBase: layout.NodeBase{ID: idProgress}, Value: progress, Fill: pal.accent, Disabled: s.track == nil},
		layout.Label{Text: clock(dur), Style: subtle},
	}}

	vol := 0.0
	if s.volMax > 0 {
		vol = float64(s.volume) / float64(s.volMax)
	}
	if s.drag == idVolume {
		vol = s.dragValue
	}
	// In a window the volume is as wide as the track beside the rail, so
	// the controls sit in the window's middle.
	slider := layout.Slider{NodeBase: layout.NodeBase{ID: idVolume, Tooltip: "Volume (+/-)"}, Value: vol, Fill: pal.accent}
	source, sourceLabel := playbackSource(s)
	sourceTip := "Return to the current " + strings.ToLower(sourceLabel)
	if source.Name != "" {
		sourceTip = "Return to " + source.Name
	}
	sourceButton := layout.Button{NodeBase: layout.NodeBase{ID: idPlaybackSource, Tooltip: sourceTip, W: layout.Points(24), H: layout.Points(20), NoPadding: true}, Icon: layout.IconLibrary, Label: "≡", Plain: true, Disabled: source.URI == "", Style: subtle}
	if !g {
		sourceButton.H = layout.Fixed(1)
		sourceButton.W = layout.Fixed(2)
	}
	volume := layout.Row{NodeBase: layout.NodeBase{ID: "player-volume-row", W: layout.Fixed(20), Gap: 1}, Align: layout.AlignCenter, Children: []layout.Node{
		sourceButton, layout.Icon{Name: layout.IconSpeaker, Text: "🔈", Style: subtle}, slider,
	}}
	if g {
		slider.W = layout.Fixed(16)
		volume.W, volume.Children = layout.Fixed(railWidth), []layout.Node{layout.NewSpacer(), sourceButton, volume.Children[1], slider}
	}

	volumeWidth := volume.W
	volume.W = layout.Grow(1)
	volumeColumn := layout.Col{NodeBase: layout.NodeBase{W: volumeWidth}, Children: []layout.Node{volume, layout.Col{NodeBase: layout.NodeBase{H: layout.Points(pick(g, 36, 32))}}}}
	if !g { // on the timeline's line, the controls' lines under it free
		volumeColumn.Children = volumeColumn.Children[:1]
	}
	barAlign := layout.AlignCenter
	if !g { // the cover, timeline, and volume share a top line
		barAlign = layout.AlignStart
	}
	return layout.Col{
		NodeBase: layout.NodeBase{PadTop: 1},
		Children: []layout.Node{
			layout.Row{NodeBase: layout.NodeBase{Gap: 2}, Align: barAlign, Children: []layout.Node{
				now,
				layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Children: []layout.Node{narrowed(g, timeline), controls}},
				volumeColumn,
			}},
		},
	}
}

// clock formats d as m:ss.
func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func addedDate(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	return at.Format("Jan 2, 2006")
}

func addedDateTooltip(at time.Time) string {
	if at.IsZero() {
		return "Date added unavailable"
	}
	return "Added " + at.Format(time.RFC3339)
}

func browseHeading(s *State) string {
	if s.page != pageBrowse {
		return "Playlist"
	}
	switch s.discovery.Pages[s.shown].Kind {
	case "artist":
		return "Artist · Popular tracks"
	case "album":
		return "Album"
	case "radio":
		return "Song Radio"
	default:
		return "Playlist"
	}
}

func trackLinkID(uri string, row int, part string) string {
	return "browse:" + uri + "#" + strconv.Itoa(row) + ":" + part
}

// loadingView centers the complete brand stack, including its artwork.
func loadingView(s *State, g bool) layout.Node {
	// A terminal draws the logo as a picture (16×8 cells, square on
	// screen), or its ♫ where images are off.
	var mark layout.Node = picture(s, g, layout.Image{NodeBase: layout.NodeBase{ID: "loading-logo", W: layout.Fixed(16), H: layout.Fixed(8)}, Source: logo(), Text: "♫", Fit: layout.ImageContain})
	if g {
		mark = layout.Image{NodeBase: layout.NodeBase{ID: "loading-logo", W: layout.Points(loadingLogoPoints), H: layout.Points(loadingLogoPoints)}, Source: logo(), Fit: layout.ImageContain}
	}
	stack := layout.Col{NodeBase: layout.NodeBase{ID: "loading-stack", W: layout.Grow(1), NoPadding: true, Gap: 2}, Align: layout.AlignCenter, Children: []layout.Node{
		mark,
		layout.Label{NodeBase: layout.NodeBase{ID: "loading-name"}, Text: "Smolify", Style: layout.TextStyle{Bold: true, Size: 28, Color: layout.ColorPrimary}},
		layout.Label{NodeBase: layout.NodeBase{ID: "loading-message"}, Text: statusOr(s, "Loading…"), Style: subtle},
	}}
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Grow(1), NoPadding: true}, Children: []layout.Node{layout.NewSpacer(), stack, layout.NewSpacer()}}
}

// repeatLabel is the repeat button's terminal text: ↻1 while one song repeats.
func repeatIcon(r model.Repeat) layout.IconName {
	if r.Track {
		return layout.IconRepeatOne
	}
	return layout.IconRepeat
}

func repeatLabel(r model.Repeat) string {
	if r.Track {
		return "↻1"
	}
	return "↻"
}

// repeatTip names the mode the repeat button is in.
func repeatTip(r model.Repeat) string {
	switch {
	case r.Track:
		return "Repeating this song (r)"
	case r.Context:
		return "Repeating the list (r)"
	}
	return "Repeat (r)"
}
