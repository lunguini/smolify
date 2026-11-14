package app

import (
	"strings"

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

// outlineEdge is a line of room between two list rows: the selected
// entry's top edge (╭──╮) when it is below, its bottom edge (╰──╯) when
// above, blank beside no selection.
type outlineEdge struct {
	layout.NodeBase
	below    bool // the edge under the selected entry
	selected bool
}

func (e outlineEdge) Size() (layout.Size, layout.Size) { return layout.Grow(1), layout.Fixed(1) }

func (e outlineEdge) Render(r layout.Rect) string {
	if !e.selected || r.W < 2 {
		return strings.Repeat(" ", max(r.W, 0))
	}
	left, right := "╭", "╮"
	if e.below {
		left, right = "╰", "╯"
	}
	return lipgloss.NewStyle().Foreground(brand).Render(left + strings.Repeat("─", r.W-2) + right)
}

// outlineSide is a side of the selected entry's outline, down its height.
type outlineSide struct{ layout.NodeBase }

func (outlineSide) Size() (layout.Size, layout.Size) { return layout.Fixed(1), layout.Grow(1) }

func (outlineSide) Render(r layout.Rect) string {
	return lipgloss.NewStyle().Foreground(brand).Render(strings.TrimSuffix(strings.Repeat("│\n", max(r.H, 0)), "\n"))
}

// roomyRows puts a line of room before each of rows' entries in a
// terminal, and outlines the selected entry (see outlineEdge).
func roomyRows(rows *layout.Col) {
	if len(rows.Children) == 0 {
		return
	}
	selected := func(n layout.Node) bool {
		r, ok := n.(layout.Row)
		return ok && r.Box.Selected && r.Box.Background == termSelectBg
	}
	marked := make([]bool, len(rows.Children))
	for i, c := range rows.Children {
		if !selected(c) {
			continue
		}
		marked[i] = true
		// The outline marks it instead of a fill; its sides take a column of
		// the entry's padding each, so its content stays put.
		r := c.(layout.Row)
		r.Box.Background = nil
		inner := make([]layout.Node, 0, len(r.Children)+2)
		inner = append(inner, outlineSide{})
		for _, child := range r.Children {
			if row, ok := child.(layout.Row); ok {
				row.Box.Background = nil
				row.PadLeft, row.PadRight = max(row.PadLeft-1, 0), max(row.PadRight-1, 0)
				child = row
			}
			inner = append(inner, child)
		}
		r.Children = append(inner, outlineSide{})
		rows.Children[i] = r
	}
	// A line of room before the first entry too, for its top edge.
	out := make([]layout.Node, 0, 2*len(rows.Children)+1)
	for i, c := range rows.Children {
		above := i > 0 && marked[i-1]
		out = append(out, outlineEdge{selected: above || marked[i], below: above}, c)
	}
	rows.Children, rows.Gap = out, 0
}
