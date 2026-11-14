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

// pillCap is a line of room between two list rows that rounds the
// selected one beside it: half a line of its highlight, cornered with
// quadrants (▗▄▄▖ above it, ▝▀▀▘ below), so the selection reads as a
// rounded pill in the highlight's own color. Blank beside no selection.
type pillCap struct {
	layout.NodeBase
	below    bool
	selected bool
}

func (p pillCap) Size() (layout.Size, layout.Size) { return layout.Grow(1), layout.Fixed(1) }

func (p pillCap) Render(r layout.Rect) string {
	if !p.selected || r.W < 2 {
		return strings.Repeat(" ", max(r.W, 0))
	}
	left, fill, right := "▗", "▄", "▖"
	if p.below {
		left, fill, right = "▝", "▀", "▘"
	}
	return lipgloss.NewStyle().Foreground(termSelectBg).Render(left + strings.Repeat(fill, r.W-2) + right)
}

// roomyRows puts a line of room between rows' entries in a terminal, the
// selected entry's rounded above and below (see pillCap).
func roomyRows(rows *layout.Col) {
	if len(rows.Children) == 0 {
		return
	}
	selected := func(n layout.Node) bool {
		r, ok := n.(layout.Row)
		return ok && r.Box.Selected && r.Box.Background == termSelectBg
	}
	out := make([]layout.Node, 0, 2*len(rows.Children))
	for i, c := range rows.Children {
		if selected(c) {
			// The selection's color shows through the entry (a playing
			// song keeps its violet title), matching the caps around it.
			r := c.(layout.Row)
			r.Children = append([]layout.Node(nil), r.Children...)
			for j, child := range r.Children {
				if inner, ok := child.(layout.Row); ok {
					inner.Box.Background = nil
					r.Children[j] = inner
				}
			}
			c = r
			rows.Children[i] = r
		}
		if i > 0 {
			above, below := selected(rows.Children[i-1]), selected(c)
			out = append(out, pillCap{selected: above || below, below: above})
		}
		out = append(out, c)
	}
	rows.Children, rows.Gap = out, 0
}
