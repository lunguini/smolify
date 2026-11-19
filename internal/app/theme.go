package app

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	_ "image/png"
	"sync"

	"github.com/lunguini/flatte/flatui/layout"
	"github.com/lunguini/flatte/theme"
	"github.com/lunguini/flatte/window"

	"github.com/lunguini/smolify/internal/spicetify"
)

// Look is a Smolify theme: Flatte's design system, which a window, a
// browser and a terminal resolve layout's color roles in, and the one
// surface it has no role for, the library rail, a shade apart from the
// cards. Smolify draws in the dark palette in every appearance.
type Look struct {
	// ID is how Settings stores the look: "" for Smolify's own, else a
	// Spicetify scheme's "Theme/Scheme".
	ID   string
	Name string
	// Design is the design system; Light and Dark are the same palette.
	Design theme.Theme
	Rail   color.NRGBA
}

// Smolify's own colors: cards on a black window and the brand violet as
// the one accent.
var (
	ink         = color.NRGBA{0x00, 0x00, 0x00, 0xff} // the window behind the cards
	violet      = color.NRGBA{0x9d, 0x7b, 0xff, 0xff}
	cardBg      = color.NRGBA{0x12, 0x12, 0x12, 0xe6} // a little of the blur shows
	railBg      = color.NRGBA{0x18, 0x18, 0x18, 0xe6} // the library, a shade apart
	white       = color.NRGBA{0xff, 0xff, 0xff, 0xff} // on artwork, whatever the look
	artVeil     = color.NRGBA{0x5b, 0x3f, 0xd0, 0x30} // over Smolify's art; a dark violet tile in a terminal
	cardOpacity = cardBg.A
	subtle      = layout.TextStyle{Role: layout.TextSubtle}
	strong      = layout.TextStyle{Bold: true}
	caption     = layout.TextStyle{Role: layout.TextSubtle, Size: 11, Bold: true}
	secondary   = layout.TextStyle{Role: layout.TextSubtle, Size: 12} // a line about the line above
)

// SmolifyLook is Smolify's own look.
func SmolifyLook() Look {
	t := theme.Default().With(theme.Seeds{Accent: &theme.Pair{Light: violet, Dark: violet}})
	p := t.Dark
	// The ground (Canvas, and Chrome: the top and player bars on it) under
	// translucent cards (Content), menus (Raised) and fields.
	p.Canvas, p.Chrome, p.Content = ink, ink, cardBg
	p.Raised, p.Field = theme.Hex("#282828"), theme.Hex("#242424")
	p.Text, p.TextSecondary = theme.Hex("#e9e9e9"), theme.Hex("#a7a7a7")
	p.TextMuted, p.TextDisabled = theme.Hex("#8a8a8a"), theme.Hex("#535353")
	p.Hover = color.NRGBA{0xff, 0xff, 0xff, 0x12}
	p.SelectionActive = color.NRGBA{0xff, 0xff, 0xff, 0x1a} // a soft white for the selected row
	p.SelectionInactive = theme.Hex("#4d4d4d")              // also a slider's unfilled track
	p.Divider, p.ControlBorder = theme.Hex("#2a2a2a"), theme.Hex("#4a4a4a")
	p.Danger = theme.Hex("#f15e6c")
	t.Dark, t.Light = p, p
	t.Font = "Inter"
	return Look{Name: "Smolify", Design: t, Rail: railBg}
}

// SpicetifyLook is a Spicetify color scheme as a look. Its keys map onto
// Smolify's surfaces as they sit on Spotify: the player bar's color is
// the ground under it, main the cards, sidebar the library rail, card the
// menus, button the accent. Keys a scheme leaves out keep Smolify's.
func SpicetifyLook(sc spicetify.Scheme) Look {
	l := SmolifyLook()
	l.ID, l.Name = sc.ID(), sc.Theme+" · "+sc.Name
	key := func(k string) (color.NRGBA, bool) { c, ok := sc.Colors[k]; return c, ok }
	t := l.Design
	if c, ok := key("button"); ok {
		t = t.With(theme.Seeds{Accent: &theme.Pair{Light: c, Dark: c}})
	}
	p := t.Dark
	if c, ok := key("player"); ok {
		p.Canvas, p.Chrome = c, c
	}
	if c, ok := key("main"); ok {
		p.Content = translucent(c)
	}
	if c, ok := key("sidebar"); ok {
		l.Rail = translucent(c)
	}
	if c, ok := key("card"); ok {
		p.Raised = c
	}
	if c, ok := key("text"); ok {
		p.Text = c
	}
	main := theme.Over(p.Content, p.Canvas) // a card as it shows
	if c, ok := key("subtext"); ok {
		p.TextSecondary, p.TextMuted = c, blend(c, main, 0.25)
	}
	if _, ok := key("main"); ok {
		p.Divider = blend(main, p.TextSecondary, 0.12)
		p.ControlBorder = blend(main, p.TextSecondary, 0.35)
		p.Field = blend(main, p.Text, 0.08)
	}
	if c, ok := key("main-elevated"); ok {
		p.Field = c
	}
	if c, ok := key("button-active"); ok {
		p.AccentHover = c
	}
	if c, ok := key("button-disabled"); ok {
		p.TextDisabled = c
	}
	if c, ok := key("selected-row"); ok {
		// Spicetify draws selected-row as a strong wash (Spotify uses it at
		// 30% and up), not the faint one Smolify's white is.
		p.SelectionActive = color.NRGBA{c.R, c.G, c.B, 0x4d}
		p.SelectionInactive = blend(main, c, 0.3)
		p.Hover = color.NRGBA{c.R, c.G, c.B, 0x26}
	}
	if c, ok := key("highlight"); ok {
		p.Hover = c
	}
	if c, ok := key("notification"); ok {
		p.Info, p.InfoTint = c, blend(main, c, 0.16)
	}
	if c, ok := key("notification-error"); ok {
		p.Danger, p.DangerTint = c, blend(main, c, 0.16)
	}
	if c, ok := key("shadow"); ok {
		p.Shadow = color.NRGBA{c.R, c.G, c.B, 0x80}
	}
	t.Dark, t.Light = p, p
	l.Design = t
	return l
}

// translucent is c as a card: as see-through as Smolify's own.
func translucent(c color.NRGBA) color.NRGBA { c.A = cardOpacity; return c }

// blend is a and b mixed in sRGB, t of the way from a to b, opaque.
func blend(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 0xff}
}

// colors are the colors Smolify picks itself in a look, for what layout
// has no role for: frames, washes, sliders, a terminal's outlines.
type colors struct {
	accent, border, hover, rail, tile, search, searchFocus color.NRGBA
	playing, playingEdge, termSelect, dim, rule, ground    color.NRGBA
}

func (l *Look) colors() colors {
	p := &l.Design.Dark
	card := theme.Over(p.Content, p.Canvas)
	return colors{
		accent: p.Accent, border: p.ControlBorder, hover: p.Hover, rail: l.Rail,
		tile: p.Raised, search: p.Field, searchFocus: theme.Over(p.Hover, p.Field),
		playing:     color.NRGBA{p.Accent.R, p.Accent.G, p.Accent.B, 0x1f}, // the playing row, accent-washed
		playingEdge: blend(card, p.Accent, 0.44),                           // the playing row's outline in a terminal
		termSelect:  blend(card, p.Accent, 0.3),                            // the selected row in a terminal
		dim:         color.NRGBA{p.Canvas.R, p.Canvas.G, p.Canvas.B, 0xa0}, // over a header's blurred cover
		rule:        p.Divider,
		ground:      p.Canvas,
	}
}

// tint is a header's color from its cover's average color: mixed toward
// the ground so the look's text reads on it.
func (c colors) tint(average color.Color) color.Color {
	if average == nil {
		return nil
	}
	return blend(color.NRGBAModel.Convert(average).(color.NRGBA), c.ground, 0.45)
}

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

// WindowTheme is Smolify's type in a window. Its colors are left to the
// look in effect (Frame.Theme), so a look chosen in Settings restyles the
// window live.
func WindowTheme() window.Theme {
	return window.Theme{Font: "Inter", FontSize: 13, TitleSize: 15, LineSpacing: 1.35, Radius: 6}
}

// coverAverage is a cover's average color, for its header's tint.
func coverAverage(im image.Image) color.Color {
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
	return color.NRGBA{uint8(r / count), uint8(g / count), uint8(bl / count), 0xff}
}
