//go:build !js || !wasm

package app

import (
	"image/color"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/theme"

	"github.com/lunguini/smolify/internal/spicetify"
)

// Smolify's own look keeps the colors it had before it drew through the
// design system: cards on black, the brand violet, the soft white
// selection.
func TestSmolifyLookKeepsItsColors(t *testing.T) {
	l := SmolifyLook()
	p := l.Design.Dark
	if p.Canvas != ink || p.Chrome != ink || p.Content != cardBg || l.Rail != railBg {
		t.Errorf("surfaces: canvas %v chrome %v content %v rail %v", p.Canvas, p.Chrome, p.Content, l.Rail)
	}
	if p.Accent != violet || p.Focus != violet {
		t.Errorf("accent %v focus %v", p.Accent, p.Focus)
	}
	if p.SelectionActive != (color.NRGBA{0xff, 0xff, 0xff, 0x1a}) {
		t.Errorf("selection %v", p.SelectionActive)
	}
	if l.Design.Light != l.Design.Dark {
		t.Error("a light appearance would restyle Smolify")
	}
	readable(t, l)
}

// readable checks l's text and accent on the surfaces they sit on, as
// drawn: translucent cards and washes composited over what is under them.
// (theme.Check measures a translucent surface by its raw color.)
func readable(t *testing.T, l Look) {
	t.Helper()
	p := l.Design.Dark
	card := theme.Over(p.Content, p.Canvas)
	surfaces := map[string]color.NRGBA{
		"ground": p.Canvas, "card": card, "rail": theme.Over(l.Rail, p.Canvas), "menu": p.Raised, "field": p.Field,
		"selected row": theme.Over(p.SelectionActive, card), "hovered row": theme.Over(p.Hover, card),
	}
	for name, bg := range surfaces {
		for fgName, fg := range map[string]color.NRGBA{"text": p.Text, "secondary text": p.TextSecondary} {
			if r := theme.Contrast(fg, bg); r < 4.5 {
				t.Errorf("%s on %s %.2f:1", fgName, name, r)
			}
		}
	}
	for _, name := range []string{"ground", "card", "rail"} {
		if r := theme.Contrast(p.Accent, surfaces[name]); r < 4.5 {
			t.Errorf("accent on %s %.2f:1", name, r)
		}
	}
}

// A header's tint over Smolify's black ground is its cover's average
// darkened to 55%, as before looks.
func TestTintMixesTowardTheGround(t *testing.T) {
	l := SmolifyLook()
	got := l.colors().tint(color.NRGBA{200, 100, 50, 0xff})
	if want := (color.NRGBA{110, 55, 28, 0xff}); got != want {
		t.Fatalf("tint %v, want %v", got, want)
	}
	if l.colors().tint(nil) != nil {
		t.Fatal("no cover, a tint")
	}
	light := SpicetifyLook(spicetify.Scheme{Theme: "L", Name: "l", Colors: map[string]color.NRGBA{"player": {0xff, 0xff, 0xff, 0xff}}})
	if c := light.colors().tint(color.NRGBA{0, 0, 0, 0xff}).(color.NRGBA); c.R < 100 {
		t.Fatalf("a light ground's tint %v stays dark", c)
	}
}

func TestSpicetifyLookMapsSchemeKeys(t *testing.T) {
	hex := theme.Hex
	sc := spicetify.Scheme{Theme: "Sleek", Name: "Coral", Colors: map[string]color.NRGBA{
		"player": hex("#0b0b0d"), "main": hex("#18181a"), "sidebar": hex("#101012"), "card": hex("#252528"),
		"text": hex("#f5f5f5"), "subtext": hex("#b0b0b0"), "button": hex("#ff7b72"), "button-active": hex("#ff9a92"),
		"notification-error": hex("#e22134"),
	}}
	l := SpicetifyLook(sc)
	p := l.Design.Dark
	if l.ID != "Sleek/Coral" || l.Name != "Sleek · Coral" {
		t.Errorf("id %q name %q", l.ID, l.Name)
	}
	if p.Canvas != hex("#0b0b0d") || p.Chrome != hex("#0b0b0d") {
		t.Errorf("ground %v %v", p.Canvas, p.Chrome)
	}
	if want := (color.NRGBA{0x18, 0x18, 0x1a, cardOpacity}); p.Content != want {
		t.Errorf("content %v, want %v", p.Content, want)
	}
	if want := (color.NRGBA{0x10, 0x10, 0x12, cardOpacity}); l.Rail != want {
		t.Errorf("rail %v, want %v", l.Rail, want)
	}
	if p.Raised != hex("#252528") || p.Text != hex("#f5f5f5") || p.TextSecondary != hex("#b0b0b0") {
		t.Errorf("raised %v text %v subtext %v", p.Raised, p.Text, p.TextSecondary)
	}
	if p.Accent != hex("#ff7b72") || p.Focus != hex("#ff7b72") || p.AccentHover != hex("#ff9a92") {
		t.Errorf("accent %v focus %v hover %v", p.Accent, p.Focus, p.AccentHover)
	}
	if p.Danger != hex("#e22134") {
		t.Errorf("danger %v", p.Danger)
	}
	if c := l.colors(); c.accent != hex("#ff7b72") || c.playing.R != 0xff || c.playing.A != 0x1f {
		t.Errorf("derived accent %v playing %v", c.accent, c.playing)
	}
	if l.Design.Light != l.Design.Dark {
		t.Error("light and dark differ")
	}
}

// A scheme that leaves keys out keeps Smolify's for them.
func TestSpicetifyLookFallsBackPerKey(t *testing.T) {
	l := SpicetifyLook(spicetify.Scheme{Theme: "T", Name: "S", Colors: map[string]color.NRGBA{"button": theme.Hex("#1db954")}})
	own := SmolifyLook()
	p, o := l.Design.Dark, own.Design.Dark
	if p.Canvas != o.Canvas || p.Content != o.Content || l.Rail != own.Rail || p.Text != o.Text || p.Field != o.Field {
		t.Errorf("surfaces changed without their keys: %+v", p)
	}
	if p.Accent != theme.Hex("#1db954") {
		t.Errorf("accent %v", p.Accent)
	}
}

// Settings offers the looks the host found; choosing one saves its ID and
// restyles the next frame through Frame.Theme, live.
func TestChoosingALookRestylesTheFrame(t *testing.T) {
	coral := SpicetifyLook(spicetify.Scheme{Theme: "Sleek", Name: "Coral", Colors: map[string]color.NRGBA{"button": theme.Hex("#ff7b72")}})
	a := WithLooks(New(&fake{}, nil, nil, nil), []Look{coral})
	s := a.State
	if got := View(s, flatte.RenderContext{}).Theme; got == nil || got.Dark.Accent != violet {
		t.Fatalf("first frame's theme %v, want Smolify's", got)
	}
	if s.settingTheme.Selected() != 0 {
		t.Fatalf("theme choice %d", s.settingTheme.Selected())
	}
	rows := settingRows(s)
	for i, id := range rows {
		if id == setTheme {
			s.settingCursor = i
		}
	}
	changeSetting(s, setTheme, 1)
	if s.settings.Theme != "Sleek/Coral" || s.settingTheme.Selected() != 1 {
		t.Fatalf("after a step: theme %q choice %d", s.settings.Theme, s.settingTheme.Selected())
	}
	if got := View(s, flatte.RenderContext{}).Theme; got == nil || got.Dark.Accent != theme.Hex("#ff7b72") {
		t.Fatalf("frame theme after choosing Coral: %v", got)
	}
	chooseSetting(s, setTheme, 0)
	if s.settings.Theme != "" {
		t.Fatalf("back to Smolify's own: %q", s.settings.Theme)
	}
}

// A saved look that is no longer installed draws as Smolify's own, and
// keeps the saved choice for when it comes back.
func TestMissingLookFallsBackToSmolify(t *testing.T) {
	st := DefaultSettings()
	st.Theme = "Gone/Scheme"
	a := WithSettingsStore(New(&fake{}, nil, nil, nil), nil, st)
	s := a.State
	if s.look().ID != "" || s.settingTheme.Selected() != 0 {
		t.Fatalf("look %q choice %d", s.look().ID, s.settingTheme.Selected())
	}
	if s.settings.Theme != "Gone/Scheme" {
		t.Fatal("the saved choice was dropped")
	}
}
