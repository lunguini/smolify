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
