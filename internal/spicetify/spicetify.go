// Package spicetify reads the color schemes of themes installed for
// Spicetify (spicetify.app), the Spotify client customizer, so Smolify can
// draw in them. A Spicetify theme is a folder under Themes/ in Spicetify's
// config directory; its color.ini holds one or more named schemes of
// colors ("text", "main", "button", …). Its user.css restyles Spotify's
// own web UI and has no meaning here.
package spicetify

import (
	"bufio"
	"errors"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Scheme is one color scheme of a theme: its keys, lower case, and their
// colors. Keys Smolify could not read are absent.
type Scheme struct {
	Theme, Name string
	Colors      map[string]color.NRGBA
}

// ID names s among all installed schemes: "Theme/Scheme".
func (s Scheme) ID() string { return s.Theme + "/" + s.Name }

// Parse reads a theme's color.ini: a [section] per scheme, then key =
// value lines, where a value is a hex color with or without its #. Lines
// starting with ; or # are comments, and so is anything after a ; on a
// line. A value that is not a 6-digit hex color (an xrdb reference, say)
// is skipped, so that key falls back rather than the scheme failing.
func Parse(theme string, r io.Reader) ([]Scheme, error) {
	var out []Scheme
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			out = append(out, Scheme{Theme: theme, Name: strings.TrimSpace(line[1 : len(line)-1]), Colors: map[string]color.NRGBA{}})
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("spicetify: %s color.ini line %d: a color before any [scheme]", theme, n)
		}
		if c, ok := hex(strings.TrimSpace(value)); ok {
			out[len(out)-1].Colors[strings.ToLower(strings.TrimSpace(key))] = c
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("spicetify: %s color.ini: %w", theme, err)
	}
	return out, nil
}

// hex is v ("1db954" or "#1db954") as an opaque color.
func hex(v string) (color.NRGBA, bool) {
	v = strings.TrimPrefix(v, "#")
	if len(v) != 6 {
		return color.NRGBA{}, false
	}
	n, err := strconv.ParseUint(v, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 0xff}, true
}

// Installed is what a Spicetify config directory holds: every scheme of
// every theme, by theme name then in file order, and the ID of the one
// Spicetify is set to ("" when unset or not installed).
type Installed struct {
	Schemes []Scheme
	Current string
}

// Load reads dir, a Spicetify config directory: Themes/*/color.ini and
// config-xpui.ini. A theme whose color.ini cannot be read or parsed is
// skipped; an empty or missing directory is no themes, not an error.
func Load(dir fs.FS) (Installed, error) {
	var got Installed
	themes, err := fs.ReadDir(dir, "Themes")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return got, err
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name() < themes[j].Name() })
	for _, t := range themes {
		if !t.IsDir() {
			continue
		}
		f, err := dir.Open(path.Join("Themes", t.Name(), "color.ini"))
		if err != nil {
			continue
		}
		schemes, err := Parse(t.Name(), f)
		f.Close()
		if err != nil {
			continue
		}
		got.Schemes = append(got.Schemes, schemes...)
	}
	theme, scheme := currentSetting(dir)
	for _, s := range got.Schemes {
		if s.Theme == theme && (s.Name == scheme || scheme == "") {
			got.Current = s.ID()
			break
		}
	}
	return got, nil
}

// currentSetting is config-xpui.ini's current_theme and color_scheme.
func currentSetting(dir fs.FS) (theme, scheme string) {
	f, err := dir.Open("config-xpui.ini")
	if err != nil {
		return "", ""
	}
	defer f.Close()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section != "[Setting]" {
			continue
		}
		switch strings.TrimSpace(key) {
		case "current_theme":
			theme = strings.TrimSpace(value)
		case "color_scheme":
			scheme = strings.TrimSpace(value)
		}
	}
	return theme, scheme
}

// ConfigDirs are the directories Spicetify may keep its config in on goos,
// most likely first: $SPICETIFY_CONFIG, then %APPDATA%\spicetify on
// Windows, or $XDG_CONFIG_HOME/spicetify, ~/.config/spicetify and the
// older ~/.spicetify elsewhere.
func ConfigDirs(goos string, getenv func(string) string, home string) []string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	join := func(parts ...string) string { return strings.Join(parts, sep) }
	var dirs []string
	if d := getenv("SPICETIFY_CONFIG"); d != "" {
		dirs = append(dirs, d)
	}
	if goos == "windows" {
		if d := getenv("APPDATA"); d != "" {
			dirs = append(dirs, join(d, "spicetify"))
		}
		if home != "" {
			dirs = append(dirs, join(home, ".spicetify"))
		}
		return dirs
	}
	if d := getenv("XDG_CONFIG_HOME"); d != "" {
		dirs = append(dirs, join(d, "spicetify"))
	}
	if home != "" {
		dirs = append(dirs, join(home, ".config", "spicetify"), join(home, ".spicetify"))
	}
	return dirs
}
