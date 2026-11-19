//go:build !js || !wasm

package app

import (
	"context"
	"image/color"
	"strings"
	"sync"
	"testing"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatest"
	"github.com/lunguini/flatte/theme"

	"github.com/lunguini/smolify/internal/player"
	"github.com/lunguini/smolify/internal/spicetify"
)

// fakeThemes is the Marketplace: two pages of themes and their schemes.
type fakeThemes struct {
	mu      sync.Mutex
	pages   [][]spicetify.Theme
	schemes map[string][]spicetify.Scheme
	err     error
	asked   map[string]int // Schemes calls by theme ID
	listed  []int          // pages asked for
}

func (f *fakeThemes) Themes(_ context.Context, page int) ([]spicetify.Theme, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = append(f.listed, page)
	if f.err != nil {
		return nil, false, f.err
	}
	if page > len(f.pages) {
		return nil, false, nil
	}
	return f.pages[page-1], page < len(f.pages), nil
}

func (f *fakeThemes) Schemes(_ context.Context, t spicetify.Theme) ([]spicetify.Scheme, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.asked == nil {
		f.asked = map[string]int{}
	}
	f.asked[t.ID]++
	return f.schemes[t.ID], nil
}

type memThemes struct {
	mu    sync.Mutex
	saved [][]SavedTheme
}

func (m *memThemes) SaveThemes(t []SavedTheme) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = append(m.saved, t)
	return nil
}

func (m *memThemes) last() []SavedTheme {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.saved) == 0 {
		return nil
	}
	return m.saved[len(m.saved)-1]
}

var (
	sleek = spicetify.Theme{ID: "spicetify/spicetify-themes/Sleek", Name: "Sleek", Description: "Sleek", Author: "harbassan", Stars: 6000}
	ziro  = spicetify.Theme{ID: "schnensch/ziro/Ziro", Name: "Ziro", Description: "A minimal theme", Author: "schnensch", Stars: 300}
	bloom = spicetify.Theme{ID: "nimsandu/spicetify-bloom/Bloom", Name: "Bloom", Description: "Bloom", Author: "nimsandu", Stars: 200}
)

func scheme(theme, name, button string) spicetify.Scheme {
	return spicetify.Scheme{Theme: theme, Name: name, Colors: map[string]color.NRGBA{"button": themeHex(button), "main": themeHex("#1e2030")}}
}

func themeHex(s string) color.NRGBA { return theme.Hex(s) }

func marketDriver(t *testing.T, saved []SavedTheme) (*flatest.Driver[State], *fakeThemes, *memThemes) {
	t.Helper()
	src := &fakeThemes{
		pages: [][]spicetify.Theme{{sleek, ziro}, {bloom}},
		schemes: map[string][]spicetify.Scheme{
			sleek.ID: {scheme("Sleek", "Coral", "#ff7b72"), scheme("Sleek", "Deep", "#82aaff")},
			ziro.ID:  {scheme("Ziro", "purple-dark", "#c39ef0")},
			bloom.ID: {scheme("Bloom", "dark", "#1db954")},
		},
	}
	store := &memThemes{}
	f := &fake{playlists: library}
	d := flatest.Start(WithMarketplace(New(f, nil, f, nil), src, store, saved), 100)
	d.State().themeDelay = 0
	d.Send(flatte.ResizeEvent{Width: 110, Height: 34})
	event(d, player.Ready{})
	return d, src, store
}

func press(d *flatest.Driver[State], k flatte.Key) { d.Send(flatte.KeyEvent{Key: k}); d.Settle() }

func frameAccent(d *flatest.Driver[State]) color.NRGBA {
	return View(d.State(), flatte.RenderContext{}).Theme.Dark.Accent
}

// Settings offers the browser when there is a Marketplace, and not
// without one.
func TestBrowseThemesRowNeedsAMarketplace(t *testing.T) {
	without := New(&fake{}, nil, nil, nil).State
	for _, id := range settingRows(without) {
		if id == setBrowseThemes {
			t.Fatal("Browse themes without a Marketplace")
		}
	}
	d, src, _ := marketDriver(t, nil)
	s := d.State()
	openSettings(s)
	for i, id := range settingRows(s) {
		if id == setBrowseThemes {
			s.settingCursor = i
		}
	}
	press(d, flatte.KeyEnter)
	if s.page != pageThemes || s.focus != paneThemes {
		t.Fatalf("Enter on Browse themes: page %v focus %v", s.page, s.focus)
	}
	if len(src.listed) != 1 || src.listed[0] != 1 {
		t.Fatalf("pages asked %v, want the first", src.listed)
	}
	if got := plain(d.Frame()); !strings.Contains(got, "Themes") || !strings.Contains(got, "Sleek") || !strings.Contains(got, "Ziro") {
		t.Fatalf("browser:\n%s", got)
	}
}

// Landing on a theme makes it the look: its colors fetched once, the
// first scheme chosen, saved and offered in Settings; ←/→ step its
// schemes; the first row goes back to Smolify's own look.
func TestBrowsingIsChoosing(t *testing.T) {
	d, src, store := marketDriver(t, nil)
	s := d.State()
	openThemes(s)
	d.Settle()
	if s.market.list.Cursor() != 0 || s.settings.Theme != "" {
		t.Fatalf("opened on row %d, theme %q", s.market.list.Cursor(), s.settings.Theme)
	}
	press(d, flatte.KeyDown) // Sleek
	if want := marketLookID(sleek, "Coral"); s.settings.Theme != want {
		t.Fatalf("theme %q, want %q", s.settings.Theme, want)
	}
	if got := frameAccent(d); got != theme.Hex("#ff7b72") {
		t.Fatalf("frame accent %v", got)
	}
	if saved := store.last(); len(saved) != 1 || saved[0].Theme.ID != sleek.ID || len(saved[0].Schemes) != 2 {
		t.Fatalf("saved %+v", saved)
	}
	if s.look().Name != "Sleek · Coral" || s.settingTheme.Selected() != lookIndex(s) {
		t.Fatalf("look %q; Settings choice %d of %d", s.look().Name, s.settingTheme.Selected(), lookIndex(s))
	}
	if !strings.Contains(plain(d.Frame()), "‹ Coral ›") {
		t.Fatalf("no scheme switcher on the chosen row:\n%s", plain(d.Frame()))
	}

	press(d, flatte.KeyRight)
	if s.settings.Theme != marketLookID(sleek, "Deep") || frameAccent(d) != theme.Hex("#82aaff") {
		t.Fatalf("→: theme %q", s.settings.Theme)
	}
	press(d, flatte.KeyRight) // wraps
	if s.settings.Theme != marketLookID(sleek, "Coral") {
		t.Fatalf("→ again: theme %q", s.settings.Theme)
	}

	press(d, flatte.KeyDown) // Ziro
	press(d, flatte.KeyUp)   // Sleek again: from this session's fetch
	if src.asked[sleek.ID] != 1 || src.asked[ziro.ID] != 1 {
		t.Fatalf("color.ini fetches %v", src.asked)
	}
	if saved := store.last(); len(saved) != 2 || saved[0].Theme.ID != sleek.ID || saved[1].Theme.ID != ziro.ID {
		t.Fatalf("saved newest first: %+v", saved)
	}

	press(d, flatte.KeyUp) // Smolify
	if s.settings.Theme != "" || frameAccent(d) != violet {
		t.Fatalf("first row: theme %q accent %v", s.settings.Theme, frameAccent(d))
	}
	if len(s.looks) != 1+3 { // Smolify, Sleek's two, Ziro's one
		t.Fatalf("looks %d", len(s.looks))
	}
}

// Esc leaves the browser for where it was opened from, keeping the look,
// and does not quit.
func TestEscapeLeavesTheBrowser(t *testing.T) {
	d, _, _ := marketDriver(t, nil)
	s := d.State()
	openSettings(s)
	openThemes(s)
	d.Settle()
	press(d, flatte.KeyDown)
	press(d, flatte.KeyEscape)
	if s.page != pageSettings || s.settings.Theme != marketLookID(sleek, "Coral") {
		t.Fatalf("after Esc: page %v theme %q", s.page, s.settings.Theme)
	}
	if d.Quit() {
		t.Fatal("Esc in the browser quit Smolify")
	}
}

// Near the end of the list the next page loads.
func TestBrowserLoadsTheNextPage(t *testing.T) {
	d, src, _ := marketDriver(t, nil)
	s := d.State()
	openThemes(s)
	d.Settle()
	press(d, flatte.KeyDown)
	if len(src.listed) != 2 || src.listed[1] != 2 {
		t.Fatalf("pages asked %v", src.listed)
	}
	if s.market.list.Count() != 1+3 || s.market.more {
		t.Fatalf("rows %d more %v", s.market.list.Count(), s.market.more)
	}
}

func TestBrowserShowsWhyThemesDidNotLoad(t *testing.T) {
	d, src, _ := marketDriver(t, nil)
	src.err = spicetify.ErrRateLimited
	s := d.State()
	openThemes(s)
	d.Settle()
	if !strings.Contains(plain(d.Frame()), "GitHub is limiting requests") {
		t.Fatalf("no rate limit note:\n%s", plain(d.Frame()))
	}
	// Opening again tries again.
	src.err = nil
	travel(s, false)
	openThemes(s)
	d.Settle()
	if s.market.err != "" || len(s.market.themes) != 2 {
		t.Fatalf("retry: err %q themes %d", s.market.err, len(s.market.themes))
	}
}

// A theme chosen at an earlier launch draws without the network.
func TestSavedThemeDrawsOffline(t *testing.T) {
	saved := []SavedTheme{{Theme: ziro, Schemes: []spicetify.Scheme{scheme("Ziro", "purple-dark", "#c39ef0")}}}
	st := DefaultSettings()
	st.Theme = marketLookID(ziro, "purple-dark")
	f := &fake{}
	src := &fakeThemes{}
	a := WithSettingsStore(WithMarketplace(New(f, nil, f, nil), src, nil, saved), nil, st)
	s := a.State
	if s.look().ID != st.Theme || s.look().Design.Dark.Accent != theme.Hex("#c39ef0") {
		t.Fatalf("look %q", s.look().ID)
	}
	if len(src.listed) != 0 || len(src.asked) != 0 {
		t.Fatal("the network was asked")
	}
}

// Only the latest themes are kept.
func TestChosenThemesAreBounded(t *testing.T) {
	d, _, store := marketDriver(t, nil)
	s := d.State()
	for i := range keptThemes + 3 {
		th := spicetify.Theme{ID: "t/" + string(rune('a'+i)), Name: string(rune('A' + i))}
		chooseScheme(s, th, []spicetify.Scheme{scheme(th.Name, "s", "#ff0000")}, 0)
	}
	d.Settle()
	if n := len(store.last()); n != keptThemes {
		t.Fatalf("kept %d, want %d", n, keptThemes)
	}
}

func TestFileThemesRoundTrip(t *testing.T) {
	path := ThemesPath(t.TempDir())
	if got, err := LoadThemes(path); err != nil || got != nil {
		t.Fatalf("no file: %v, %v", got, err)
	}
	want := []SavedTheme{{Theme: sleek, Schemes: []spicetify.Scheme{scheme("Sleek", "Coral", "#ff7b72")}}}
	if err := (&FileThemes{Path: path}).SaveThemes(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadThemes(path)
	if err != nil || len(got) != 1 || got[0].Theme != sleek || got[0].Schemes[0].Colors["button"] != themeHex("#ff7b72") {
		t.Fatalf("loaded %+v, %v", got, err)
	}
}
