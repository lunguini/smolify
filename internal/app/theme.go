package app

import (
	"image"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/flatte/window"
)

// The palette: cards on a black window, one green accent.
var (
	accent    = lipgloss.Color("#1ed760")
	ink       = lipgloss.Color("#000000")           // the window behind the cards
	cardBg    = color.NRGBA{0x12, 0x12, 0x12, 0xe6} // a little of the blur shows
	railBg    = color.NRGBA{0x18, 0x18, 0x18, 0xe6} // the library, a shade apart
	tileBg    = lipgloss.Color("#282828")           // a cover not loaded yet
	danger    = lipgloss.Color("#f15e6c")
	white     = lipgloss.Color("#ffffff")
	subtle    = layout.TextStyle{Role: layout.TextSubtle}
	strong    = layout.TextStyle{Bold: true}
	caption   = layout.TextStyle{Role: layout.TextSubtle, Size: 11, Bold: true}
	secondary = layout.TextStyle{Role: layout.TextSubtle, Size: 12} // a line about the line above
)

// WindowTheme is Smolify's look in a window: Spotify-dark, the accent for
// focus and default buttons, a soft white for selected rows.
func WindowTheme() window.Theme {
	return window.Theme{
		Background: ink, Surface: lipgloss.Color("#282828"),
		Text: lipgloss.Color("#e9e9e9"), Subtle: lipgloss.Color("#a7a7a7"), Title: white,
		Border: lipgloss.Color("#2f2f2f"), Field: lipgloss.Color("#242424"),
		Accent: accent, Selection: color.NRGBA{0xff, 0xff, 0xff, 0x1a},
		Track:    lipgloss.Color("#4d4d4d"),
		FontSize: 13, TitleSize: 15, LineSpacing: 1.35, Radius: 6,
	}
}

// tintOf is a header color drawn from a cover: its average color, darkened
// so white text stays readable on it.
func tintOf(im image.Image) color.Color {
	b := im.Bounds()
	if b.Empty() {
		return nil
	}
	const n = 24 // samples per side
	var r, g, bl, count uint64
	for y := range n {
		for x := range n {
			c := color.NRGBAModel.Convert(im.At(b.Min.X+x*b.Dx()/n, b.Min.Y+y*b.Dy()/n)).(color.NRGBA)
			r, g, bl, count = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), count+1
		}
	}
	dark := func(v uint64) uint8 { return uint8(v / count * 55 / 100) }
	return color.NRGBA{dark(r), dark(g), dark(bl), 0xff}
}
