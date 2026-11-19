package spicetify

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const sleek = `; Sleek's schemes
[Coral]
text               = FFFFFF
subtext            = F0F0F0
main               = 18181A   ; the main view
sidebar            = #101012
button             = ff7b72

# a second scheme, keys in another case
[Deep Blue]
Text = c8d3f5
MAIN = 1E2030
selected-row = 3b4261
`

func TestParseReadsEverySchemeInOrder(t *testing.T) {
	got, err := Parse("Sleek", strings.NewReader(sleek))
	if err != nil {
		t.Fatal(err)
	}
	want := []Scheme{
		{Theme: "Sleek", Name: "Coral", Colors: map[string]color.NRGBA{
			"text": {0xff, 0xff, 0xff, 0xff}, "subtext": {0xf0, 0xf0, 0xf0, 0xff},
			"main": {0x18, 0x18, 0x1a, 0xff}, "sidebar": {0x10, 0x10, 0x12, 0xff}, "button": {0xff, 0x7b, 0x72, 0xff},
		}},
		{Theme: "Sleek", Name: "Deep Blue", Colors: map[string]color.NRGBA{
			"text": {0xc8, 0xd3, 0xf5, 0xff}, "main": {0x1e, 0x20, 0x30, 0xff}, "selected-row": {0x3b, 0x42, 0x61, 0xff},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse:\n got %+v\nwant %+v", got, want)
	}
}

// Values Smolify cannot read (xrdb references, short or bad hex) are left
// out, so the scheme falls back for that key instead of failing whole.
func TestParseSkipsUnreadableValues(t *testing.T) {
	got, err := Parse("X", strings.NewReader("[A]\ntext = ${xrdb:foreground}\nmain = fff\nbutton = zzzzzz\nshadow = 000000\nstray line\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Colors) != 1 || got[0].Colors["shadow"] != (color.NRGBA{0, 0, 0, 0xff}) {
		t.Fatalf("got %+v, want only shadow", got)
	}
}

func TestParseRejectsKeysOutsideAScheme(t *testing.T) {
	if _, err := Parse("X", strings.NewReader("text = ffffff\n[A]\nmain = 000000\n")); err == nil {
		t.Fatal("a key before any [scheme] parsed")
	}
}

func TestSchemeID(t *testing.T) {
	if got := (Scheme{Theme: "Sleek", Name: "Coral"}).ID(); got != "Sleek/Coral" {
		t.Fatal(got)
	}
}

func TestLoadFindsInstalledThemesAndTheCurrentOne(t *testing.T) {
	dir := fstest.MapFS{
		"config-xpui.ini":         {Data: []byte("[Setting]\nspotify_path = /x\ncurrent_theme = Sleek\ncolor_scheme = Deep Blue\n\n[Patch]\n")},
		"Themes/Sleek/color.ini":  {Data: []byte(sleek)},
		"Themes/Sleek/user.css":   {Data: []byte("body{}")},
		"Themes/Ziro/color.ini":   {Data: []byte("[purple-dark]\nmain = 1b1b1b\n")},
		"Themes/Empty/user.css":   {Data: []byte("")}, // no color.ini: not a color theme
		"Themes/Broken/color.ini": {Data: []byte("main = 000000\n")},
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range got.Schemes {
		ids = append(ids, s.ID())
	}
	if want := []string{"Sleek/Coral", "Sleek/Deep Blue", "Ziro/purple-dark"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("schemes %q, want %q", ids, want)
	}
	if got.Current != "Sleek/Deep Blue" {
		t.Fatalf("current %q", got.Current)
	}
}

// Spicetify's current scheme defaults to the theme's first when
// color_scheme is empty.
func TestLoadCurrentDefaultsToTheThemesFirstScheme(t *testing.T) {
	dir := fstest.MapFS{
		"config-xpui.ini":        {Data: []byte("[Setting]\ncurrent_theme = Sleek\ncolor_scheme =\n")},
		"Themes/Sleek/color.ini": {Data: []byte(sleek)},
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "Sleek/Coral" {
		t.Fatalf("current %q", got.Current)
	}
}

func TestLoadWithoutSpicetify(t *testing.T) {
	got, err := Load(fstest.MapFS{})
	if err != nil || len(got.Schemes) != 0 || got.Current != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestConfigDirs(t *testing.T) {
	env := map[string]string{"XDG_CONFIG_HOME": "/xdg", "APPDATA": `C:\Users\a\AppData\Roaming`}
	getenv := func(k string) string { return env[k] }
	if got, want := ConfigDirs("darwin", getenv, "/home/a"), []string{"/xdg/spicetify", "/home/a/.config/spicetify", "/home/a/.spicetify"}; !reflect.DeepEqual(got, want) {
		t.Errorf("darwin %q", got)
	}
	if got := ConfigDirs("windows", getenv, `C:\Users\a`); len(got) == 0 || !strings.HasSuffix(got[0], "spicetify") || !strings.HasPrefix(got[0], `C:\Users\a\AppData\Roaming`) {
		t.Errorf("windows %q", got)
	}
	env["SPICETIFY_CONFIG"] = "/custom"
	if got := ConfigDirs("linux", getenv, "/home/a"); got[0] != "/custom" {
		t.Errorf("SPICETIFY_CONFIG not first: %q", got)
	}
}
