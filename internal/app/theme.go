package app

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	_ "image/png"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/flatte/window"
)

// The palette: cards on a black window and Smolify's violet, the one
// accent.
var (
	brand         = lipgloss.Color("#9d7bff")
	ink           = lipgloss.Color("#000000")           // the window behind the cards
	cardBg        = color.NRGBA{0x12, 0x12, 0x12, 0xe6} // a little of the blur shows
	railBg        = color.NRGBA{0x18, 0x18, 0x18, 0xe6} // the library, a shade apart
	tileBg        = lipgloss.Color("#282828")           // a cover not loaded yet
	playingBg     = color.NRGBA{0x9d, 0x7b, 0xff, 0x1f} // the playing row, violet-washed
	hoverBg       = color.NRGBA{0xff, 0xff, 0xff, 0x12} // a row under the pointer
	searchBg      = lipgloss.Color("#1f1f1f")           // the search box
	searchFocusBg = lipgloss.Color("#2a2a2a")           // the search box while typing
	backdropDim   = color.NRGBA{0, 0, 0, 0xa0}          // over a header's blurred cover
	artVeil       = color.NRGBA{0x5b, 0x3f, 0xd0, 0x30} // over Smolify's art; a dark violet tile in a terminal
	danger        = lipgloss.Color("#f15e6c")
	white         = lipgloss.Color("#ffffff")
	subtle        = layout.TextStyle{Role: layout.TextSubtle}
	strong        = layout.TextStyle{Bold: true}
	caption       = layout.TextStyle{Role: layout.TextSubtle, Size: 11, Bold: true}
	secondary     = layout.TextStyle{Role: layout.TextSubtle, Size: 12} // a line about the line above
)

// Inter (rsms.me/inter, SIL Open Font License: fonts/OFL.txt), Smolify's
// type in a window.
var (
	//go:embed fonts/Inter-Regular.ttf
	interRegular []byte
	//go:embed fonts/Inter-Bold.ttf
	interBold []byte
)

//go:embed images/logo.png
var logoPNG []byte

// logo retains enough pixels for the loading screen and sidebar at Retina scale.
var logo = sync.OnceValue(func() image.Image {
	im, _, err := image.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		return nil
	}
	return shrink(im, 2*loadingLogoPoints)
})

// logoPoints is the logo's size in the top bar.
const logoPoints = 28
const loadingLogoPoints = 96

// WindowFonts are the font files a window needs for WindowTheme.
func WindowFonts() [][]byte { return [][]byte{interRegular, interBold} }

// WindowTheme is Smolify's look in a window: dark, the brand violet for
// focus and default buttons, a soft white for selected rows.
func WindowTheme() window.Theme {
	return window.Theme{
		Background: ink, Surface: lipgloss.Color("#282828"),
		Text: lipgloss.Color("#e9e9e9"), Subtle: lipgloss.Color("#a7a7a7"), Title: white,
		Border: lipgloss.Color("#2f2f2f"), Field: lipgloss.Color("#242424"),
		Accent: brand, Selection: color.NRGBA{0xff, 0xff, 0xff, 0x1a},
		Track:    lipgloss.Color("#4d4d4d"),
		Font:     "Inter",
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
