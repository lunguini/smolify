package app

import (
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lunguini/flatte/flatui/layout"
)

// A terminal's sections are split by single lines that end at the
// terminal's own edges, as panes in a terminal multiplexer: one line
// between two sections rather than a border around each, and a tee where
// a line meets another.

// hrule is a horizontal line across its width, with a tee at each column
// of tees (┬, ┴, …), counted from its left.
type hrule struct {
	layout.NodeBase
	tees map[int]string
}

func newHRule(tees map[int]string) hrule {
	return hrule{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Fixed(1)}, tees: tees}
}

func (h hrule) Size() (layout.Size, layout.Size) { return h.W, h.H }

func (h hrule) Render(r layout.Rect) string {
	var b strings.Builder
	for x := range r.W {
		if tee, ok := h.tees[x]; ok {
			b.WriteString(tee)
		} else {
			b.WriteString("─")
		}
	}
	return lipgloss.NewStyle().Foreground(searchBorder).Render(b.String())
}

// vrule is a vertical line down its height, with a tee at each line of
// tees (├, ┤, …), counted from its top.
type vrule struct {
	layout.NodeBase
	tees map[int]string
}

func newVRule(tees map[int]string) vrule {
	return vrule{NodeBase: layout.NodeBase{W: layout.Fixed(1), H: layout.Grow(1)}, tees: tees}
}

func (v vrule) Size() (layout.Size, layout.Size) { return v.W, v.H }

func (v vrule) Render(r layout.Rect) string {
	lines := make([]string, r.H)
	for y := range lines {
		lines[y] = "│"
		if tee, ok := v.tees[y]; ok {
			lines[y] = tee
		}
	}
	return lipgloss.NewStyle().Foreground(searchBorder).Render(strings.Join(lines, "\n"))
}

// The selected entry of a list in a terminal is outlined, rounded as the
// search field is, in the accent: its top and bottom edges in the lines of
// room above and below it, its sides in its outer columns.

// The playing entry is outlined too, dimly, in place of the window's
// violet wash; where the two meet, the selection's edge is drawn.

// outline is how an entry is outlined: not, as playing, or as selected.
type outline int

const (
	outlineNone outline = iota
	outlinePlaying
	outlineSelected
)

func (o outline) color() color.Color {
	if o == outlinePlaying {
		return playingEdge
	}
	return brand
}

// outlineEdge is a line of room between two list rows: the top edge
// (╭──╮) of an outlined entry below it, or the bottom edge (╰──╯) of one
// above; blank between plain entries.
type outlineEdge struct {
	layout.NodeBase
	below bool // the edge under the entry above
	kind  outline
}

func (e outlineEdge) Size() (layout.Size, layout.Size) { return layout.Grow(1), layout.Fixed(1) }

func (e outlineEdge) Render(r layout.Rect) string {
	if e.kind == outlineNone || r.W < 2 {
		return strings.Repeat(" ", max(r.W, 0))
	}
	left, right := "╭", "╮"
	if e.below {
		left, right = "╰", "╯"
	}
	return lipgloss.NewStyle().Foreground(e.kind.color()).Render(left + strings.Repeat("─", r.W-2) + right)
}

// outlineSide is a side of an entry's outline, down its height.
type outlineSide struct {
	layout.NodeBase
	kind outline
}

func (outlineSide) Size() (layout.Size, layout.Size) { return layout.Fixed(1), layout.Grow(1) }

func (o outlineSide) Render(r layout.Rect) string {
	return lipgloss.NewStyle().Foreground(o.kind.color()).Render(strings.TrimSuffix(strings.Repeat("│\n", max(r.H, 0)), "\n"))
}

// roomyRows puts a line of room before each of rows' entries in a
// terminal, and outlines the selected entry (see outlineEdge).
func roomyRows(rows *layout.Col) {
	if len(rows.Children) == 0 {
		return
	}
	kindOf := func(n layout.Node) outline {
		r, ok := n.(layout.Row)
		if !ok {
			return outlineNone
		}
		if r.Box.Selected && r.Box.Background == termSelectBg {
			return outlineSelected
		}
		for _, child := range r.Children {
			if inner, ok := child.(layout.Row); ok && inner.Box.Background == playingBg {
				return outlinePlaying
			}
		}
		return outlineNone
	}
	marked := make([]outline, len(rows.Children))
	for i, c := range rows.Children {
		kind := kindOf(c)
		if kind == outlineNone {
			continue
		}
		marked[i] = kind
		// The outline marks it instead of a fill; its sides take a column of
		// the entry's padding each, so its content stays put.
		r := c.(layout.Row)
		r.Box.Background = nil
		inner := make([]layout.Node, 0, len(r.Children)+2)
		inner = append(inner, outlineSide{kind: kind})
		for _, child := range r.Children {
			if row, ok := child.(layout.Row); ok {
				row.Box.Background = nil
				row.PadLeft, row.PadRight = max(row.PadLeft-1, 0), max(row.PadRight-1, 0)
				child = row
			}
			inner = append(inner, child)
		}
		r.Children = append(inner, outlineSide{kind: kind})
		rows.Children[i] = r
	}
	// A line of room before the first entry too, for its top edge.
	out := make([]layout.Node, 0, 2*len(rows.Children)+1)
	for i, c := range rows.Children {
		above := outlineNone
		if i > 0 {
			above = marked[i-1]
		}
		// The brighter of the two outlines meeting here draws its edge.
		edge := outlineEdge{kind: marked[i]}
		if above > marked[i] {
			edge = outlineEdge{kind: above, below: true}
		}
		out = append(out, edge, c)
	}
	rows.Children, rows.Gap = out, 0
}

// equalizerStep is how often the playing entry's equalizer moves.
const equalizerStep = 220 * time.Millisecond

// equalizerBars is the terminal's playing indicator at frame f: three bars
// rising and falling at their own paces while playing, still when paused.
func equalizerBars(f int, playing bool) string {
	if !playing {
		return "▃▅▂"
	}
	levels := []rune("▁▂▃▄▅▆▇")
	var b strings.Builder
	for i, pace := range []int{3, 5, 4} {
		// A triangle wave over the levels, each bar its own speed and phase.
		n := len(levels)*2 - 2
		step := (f*pace + i*4) % n
		if step >= len(levels) {
			step = n - step
		}
		b.WriteRune(levels[step])
	}
	return b.String()
}

// The terminal's top bar shares its borders with the divider under it and
// the terminal's edges (see terminalTopBar).

// barFill is a stretch of the top bar with nothing framed in it: blank
// above, its share of the divider on its last line (from tee on, when set).
type barFill struct {
	layout.NodeBase
	tee string
}

func newBarFill(w layout.Size, tee string) barFill {
	return barFill{NodeBase: layout.NodeBase{W: w, H: layout.Fixed(3)}, tee: tee}
}

func (f barFill) Size() (layout.Size, layout.Size) { return f.W, f.H }

func (f barFill) Render(r layout.Rect) string {
	if r.W <= 0 || r.H <= 0 {
		return ""
	}
	line := strings.Repeat("─", r.W)
	if f.tee != "" {
		line = f.tee + strings.Repeat("─", r.W-1)
	}
	lines := make([]string, r.H)
	for i := range lines[:r.H-1] {
		lines[i] = strings.Repeat(" ", r.W)
	}
	lines[r.H-1] = lipgloss.NewStyle().Foreground(searchBorder).Render(line)
	return strings.Join(lines, "\n")
}

// keySide is a top bar key's left side, down its height.
type keySide struct {
	layout.NodeBase
	color color.Color
}

func (keySide) Size() (layout.Size, layout.Size) { return layout.Fixed(1), layout.Grow(1) }

func (k keySide) Render(r layout.Rect) string {
	return lipgloss.NewStyle().Foreground(k.color).Render(strings.TrimSuffix(strings.Repeat("│\n", max(r.H, 0)), "\n"))
}

// withHeight is f h lines tall: its divider on the last.
func (f barFill) withHeight(h int) barFill {
	f.H = layout.Fixed(h)
	return f
}
