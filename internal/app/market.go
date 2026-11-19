package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lunguini/flatte"
	"github.com/lunguini/flatte/flatui"
	"github.com/lunguini/flatte/flatui/layout"

	"github.com/lunguini/smolify/internal/spicetify"
)

// The theme browser (Settings › Appearance › Browse themes) lists the
// Spicetify Marketplace's themes. Browsing is choosing: the theme the
// cursor rests on is fetched (its color.ini only) and becomes Smolify's
// look, kept for later launches; ←/→ step through its color schemes. Its
// first row is Smolify's own look, the way back.

// ThemeSource is the Spicetify Marketplace: its themes, a page at a time,
// and a theme's color schemes.
type ThemeSource interface {
	Themes(ctx context.Context, page int) ([]spicetify.Theme, bool, error)
	Schemes(ctx context.Context, t spicetify.Theme) ([]spicetify.Scheme, error)
}

// ThemeStore keeps the Marketplace themes chosen lately, newest first, so
// they draw at later launches without the network.
type ThemeStore interface {
	SaveThemes([]SavedTheme) error
}

// SavedTheme is a Marketplace theme kept with its color schemes.
type SavedTheme struct {
	Theme   spicetify.Theme    `json:"theme"`
	Schemes []spicetify.Scheme `json:"schemes"`
}

// keptThemes bounds how many chosen Marketplace themes are kept.
const keptThemes = 12

// looks are t's schemes as looks.
func (t SavedTheme) looks() []Look {
	out := make([]Look, 0, len(t.Schemes))
	for _, sc := range t.Schemes {
		l := SpicetifyLook(sc)
		l.ID, l.Name = marketLookID(t.Theme, sc.Name), t.Theme.Name+" · "+sc.Name
		out = append(out, l)
	}
	return out
}

// marketLookID is the Settings.Theme of scheme of Marketplace theme t.
func marketLookID(t spicetify.Theme, scheme string) string {
	return "market:" + t.ID + "#" + scheme
}

// marketState is the theme browser.
type marketState struct {
	source ThemeSource
	store  ThemeStore
	saved  []SavedTheme

	// list's row 0 is Smolify's own look; row i is themes[i-1].
	list    flatui.List
	themes  []spicetify.Theme
	pages   int // search pages loaded
	more    bool
	loading bool
	err     string
	// schemes are themes' color schemes fetched this session, by theme ID;
	// fetching is the theme the cursor rests on while its colors load.
	schemes  map[string][]spicetify.Scheme
	fetching string
}

// WithMarketplace offers the Spicetify Marketplace's themes in Settings,
// and the ones chosen lately (saved, newest first) as looks.
func WithMarketplace(app flatte.App[State], source ThemeSource, store ThemeStore, saved []SavedTheme) flatte.App[State] {
	s := app.State
	s.market.source, s.market.store, s.market.saved = source, store, saved
	rebuildLooks(s)
	return app
}

const idThemes = "themes"

// openThemes shows the theme browser, its cursor on the look in use, and
// loads the Marketplace's first page if it has none yet.
func openThemes(s *State) {
	if s.market.source == nil {
		return
	}
	if s.page != pageThemes {
		visit(s)
	}
	s.page, s.focus = pageThemes, paneThemes
	s.market.list.SetCount(1 + len(s.market.themes))
	s.market.list.Select(currentThemeRow(s))
	if s.market.pages == 0 && !s.market.loading {
		loadThemes(s)
	}
}

// currentThemeRow is the browser's row of the look in use (0 when it is
// not a listed Marketplace theme).
func currentThemeRow(s *State) int {
	id, ok := strings.CutPrefix(s.settings.Theme, "market:")
	if !ok {
		return 0
	}
	id, _, _ = strings.Cut(id, "#")
	for i, t := range s.market.themes {
		if t.ID == id {
			return i + 1
		}
	}
	return 0
}

// loadThemes fetches the Marketplace's next page.
func loadThemes(s *State) {
	src := s.market.source
	if src == nil || s.market.loading {
		return
	}
	page := s.market.pages + 1
	s.market.loading, s.market.err = true, ""
	flatte.Latest(s.fx, "themes", func(ctx context.Context) (themePage, error) {
		themes, more, err := src.Themes(ctx, page)
		return themePage{themes, more}, err
	}, func(s *State, p themePage, err error) {
		s.market.loading = false
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				s.market.err = themeError(err)
			}
			return
		}
		seen := make(map[string]bool, len(s.market.themes))
		for _, t := range s.market.themes {
			seen[t.ID] = true
		}
		for _, t := range p.themes {
			if !seen[t.ID] {
				s.market.themes = append(s.market.themes, t)
				seen[t.ID] = true
			}
		}
		s.market.pages, s.market.more = page, p.more
		at := s.market.list.Cursor()
		s.market.list.SetCount(1 + len(s.market.themes))
		s.market.list.Select(at)
		if at == 0 && page == 1 {
			s.market.list.Select(currentThemeRow(s))
		}
	})
}

type themePage struct {
	themes []spicetify.Theme
	more   bool
}

func themeError(err error) string {
	if errors.Is(err, spicetify.ErrRateLimited) {
		return "GitHub is limiting requests. Try again in a minute."
	}
	return "Could not load themes: " + err.Error()
}

// themesKey moves through the browser: ↑/↓ (and Page Up/Down, Home/End)
// land on a theme, ←/→ step its color schemes, Esc goes back.
func themesKey(s *State, k flatte.KeyEvent) bool {
	l := &s.market.list
	switch k.Key {
	case flatte.KeyUp:
		l.MoveUp()
	case flatte.KeyDown:
		l.MoveDown()
	case flatte.KeyPageUp:
		l.Select(l.Cursor() - max(l.Height()-1, 1))
	case flatte.KeyPageDown:
		l.Select(l.Cursor() + max(l.Height()-1, 1))
	case flatte.KeyHome:
		l.Select(0)
	case flatte.KeyEnd:
		l.Select(l.Count() - 1)
	case flatte.KeyLeft, flatte.KeyRight:
		step := 1
		if k.Key == flatte.KeyLeft {
			step = -1
		}
		stepScheme(s, step)
		return true
	case flatte.KeyEnter:
		landOn(s, 0)
		return true
	case flatte.KeyEscape:
		travel(s, false)
		return true
	default:
		return false
	}
	landOn(s, s.themeDelay)
	return true
}

// landOn takes the row under the cursor once the cursor has rested there
// for delay, so passing over rows (keys held, a pointer swept across)
// changes nothing: then Smolify's own look, or a Marketplace theme once
// its color schemes are in. Near the end of the list it loads the next
// page.
func landOn(s *State, delay time.Duration) {
	l := s.market.list
	if l.Cursor() >= l.Count()-5 && s.market.more && !s.market.loading {
		loadThemes(s)
	}
	row := l.Cursor()
	flatte.Latest(s.fx, "theme", func(ctx context.Context) (struct{}, error) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return struct{}{}, ctx.Err()
			}
		}
		return struct{}{}, nil
	}, func(s *State, _ struct{}, err error) {
		if err == nil && s.page == pageThemes && s.market.list.Cursor() == row {
			takeRow(s, row)
		}
	})
}

// takeRow makes browser row the look: Smolify's own for row 0, else that
// theme's first scheme, its colors from this session's fetches or fetched
// now.
func takeRow(s *State, row int) {
	if row <= 0 {
		flatte.Cancel(s.fx, "theme-colors")
		s.market.fetching = ""
		chooseLook(s, "")
		return
	}
	i := row - 1
	if i >= len(s.market.themes) {
		return
	}
	t, src := s.market.themes[i], s.market.source
	if cached := s.market.schemes[t.ID]; cached != nil {
		flatte.Cancel(s.fx, "theme-colors")
		s.market.fetching = ""
		chooseScheme(s, t, cached, 0)
		return
	}
	s.market.fetching = t.ID
	flatte.Latest(s.fx, "theme-colors", func(ctx context.Context) ([]spicetify.Scheme, error) {
		return src.Schemes(ctx, t)
	}, func(s *State, schemes []spicetify.Scheme, err error) {
		if s.market.fetching == t.ID {
			s.market.fetching = ""
		}
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				s.market.err = fmt.Sprintf("Could not load %s's colors: %v", t.Name, err)
			}
			return
		}
		s.market.err = ""
		if s.market.schemes == nil {
			s.market.schemes = map[string][]spicetify.Scheme{}
		}
		s.market.schemes[t.ID] = schemes
		chooseScheme(s, t, schemes, 0)
	})
}

// stepScheme moves the theme in use, when it is the one under the cursor,
// step color schemes along (wrapping).
func stepScheme(s *State, step int) {
	i := s.market.list.Cursor() - 1
	if i < 0 || i >= len(s.market.themes) {
		return
	}
	t := s.market.themes[i]
	schemes := s.market.schemes[t.ID]
	if len(schemes) < 2 {
		return
	}
	now := 0
	for j, sc := range schemes {
		if marketLookID(t, sc.Name) == s.settings.Theme {
			now = j
		}
	}
	chooseScheme(s, t, schemes, wrap(now+step, len(schemes)))
}

// chooseScheme makes scheme n of Marketplace theme t the look: kept first
// among the saved themes, and saved.
func chooseScheme(s *State, t spicetify.Theme, schemes []spicetify.Scheme, n int) {
	kept := []SavedTheme{{Theme: t, Schemes: schemes}}
	for _, saved := range s.market.saved {
		if saved.Theme.ID != t.ID && len(kept) < keptThemes {
			kept = append(kept, saved)
		}
	}
	s.market.saved = kept
	rebuildLooks(s)
	if store := s.market.store; store != nil {
		flatte.Go(s.fx, "save-themes", func(context.Context) (struct{}, error) {
			return struct{}{}, store.SaveThemes(kept)
		}, func(s *State, _ struct{}, err error) {
			if err != nil {
				fail(s, "Could not save the theme: "+err.Error())
			}
		})
	}
	chooseLook(s, marketLookID(t, schemes[n].Name))
}

// chooseLook makes look id the setting (saved by applySettings).
func chooseLook(s *State, id string) {
	if s.settings.Theme == id {
		return
	}
	st := s.settings
	st.Theme = id
	applySettings(s, st)
}

// themeHovered lands on the browser row under the pointer, taken once the
// pointer has rested on it for themeHoverDelay (sweeping across rows
// changes nothing), as the keyboard's cursor would.
func themeHovered(s *State, target string) bool {
	i, ok := rowIndex(idThemes, target)
	if !ok || s.page != pageThemes {
		return false
	}
	if i != s.market.list.Cursor() {
		s.market.list.Select(i)
		landOn(s, s.themeHoverDelay)
	}
	return true
}

// themeClicked handles a click in the browser: on a row, landing on it at
// once.
func themeClicked(s *State, target string) bool {
	i, ok := rowIndex(idThemes, target)
	if !ok || s.page != pageThemes {
		return false
	}
	s.focus = paneThemes
	s.market.list.Select(i)
	landOn(s, 0)
	return true
}

// themesView is the theme browser.
func themesView(s *State, g bool, width int) layout.Node {
	pal := s.look().colors()
	rows := s.market.list.Rows(idThemes, func(i int, selected bool) layout.Node {
		name, about, side := "Smolify", "Smolify's own look", ""
		current := s.settings.Theme == ""
		if i > 0 && i-1 < len(s.market.themes) {
			t := s.market.themes[i-1]
			name = t.Name
			about = t.Description
			if t.Author != "" && !strings.EqualFold(t.Author, t.Description) {
				about += " · " + t.Author
			}
			if t.Description == t.Name {
				about = "by " + t.Author
			}
			about += fmt.Sprintf(" · ★ %d", t.Stars)
			current = strings.HasPrefix(s.settings.Theme, marketLookID(t, ""))
			switch schemes := s.market.schemes[t.ID]; {
			case s.market.fetching == t.ID:
				side = "Loading…"
			case current && len(schemes) > 1:
				_, scheme, _ := strings.Cut(s.settings.Theme, "#")
				side = "‹ " + scheme + " ›"
			case len(schemes) > 1:
				side = fmt.Sprintf("%d color schemes", len(schemes))
			}
		}
		style := layout.TextStyle{}
		if current {
			style.Color = layout.ColorAccent
		}
		row := layout.Row{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Fixed(railRowLines), Gap: 2, PadLeft: 1, PadRight: 1}, Align: layout.AlignCenter, Children: []layout.Node{
			twoLines(layout.NodeBase{W: layout.Grow(1), Tooltip: name}, name, style, about, secondary),
		}}
		if side != "" {
			row.Children = append(row.Children, layout.Label{Text: side, Style: secondary})
		}
		if g {
			row.H = layout.Points(48)
			row.PadTop, row.PadBottom = 1, 1
		}
		return row
	})
	rows.ID, rows.W, rows.H = idThemes, layout.Grow(1), layout.Grow(1)
	withHover(&rows, s.focus == paneThemes, g, pal)
	if !g {
		roomyRows(&rows, pal)
	}
	note := "From the Spicetify Marketplace. The theme you land on is the one Smolify uses; ←/→ change its colors."
	switch {
	case s.market.err != "":
		note = s.market.err
	case s.market.loading && len(s.market.themes) == 0:
		note = "Loading themes…"
	}
	noteStyle := secondary
	if s.market.err != "" {
		noteStyle = layout.TextStyle{Color: layout.ColorDanger, Size: secondary.Size}
	}
	return layout.Col{NodeBase: layout.NodeBase{W: layout.Grow(1), H: layout.Grow(1), Pad: pick(g, 2, 1), Gap: 1}, Children: []layout.Node{
		layout.Label{Text: "Themes", Style: layout.TextStyle{Bold: true, Size: 30, Color: layout.ColorPrimary}},
		layout.Label{NodeBase: layout.NodeBase{W: layout.Grow(1)}, Text: note, Wrap: true, Style: noteStyle},
		layout.Col{NodeBase: layout.NodeBase{W: settingsWidth(g, width), H: layout.Grow(1)}, Children: []layout.Node{rows}},
	}}
}
