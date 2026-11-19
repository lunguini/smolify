package spicetify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Marketplace finds themes as Spicetify's Marketplace does: GitHub
// repositories with the spicetify-themes topic, by stars, less those on
// the Marketplace's blocklist and archived ones, each described by a
// manifest.json at its root. Only a theme's color schemes are used.
type Marketplace struct {
	Client *http.Client
	// API and Raw are api.github.com and raw.githubusercontent.com ("":
	// those), replaceable in tests.
	API, Raw string
}

// Theme is a Marketplace theme that has color schemes.
type Theme struct {
	// ID names it: "owner/repo/name".
	ID, Name, Description, Author string
	Stars                         int
	// SchemesURL is its color.ini.
	SchemesURL string
}

// ErrRateLimited is GitHub refusing more searches for now (about ten a
// minute without a sign-in).
var ErrRateLimited = errors.New("spicetify: GitHub is limiting requests; try again in a minute")

// perPage is how many repositories one search page holds (GitHub's most).
const perPage = 100

// maxBody bounds what one response may be read to.
const maxBody = 4 << 20

func (m Marketplace) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return http.DefaultClient
}

func (m Marketplace) api() string { return orDefault(m.API, "https://api.github.com") }
func (m Marketplace) raw() string { return orDefault(m.Raw, "https://raw.githubusercontent.com") }

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return strings.TrimSuffix(s, "/")
}

// get is url's body; a status other than 200 is an error.
func (m Marketplace) get(ctx context.Context, u string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json, */*")
	res, err := m.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if res.StatusCode != http.StatusOK {
		return b, res.StatusCode, fmt.Errorf("spicetify: %s: %s", u, res.Status)
	}
	return b, res.StatusCode, nil
}

type repo struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	Stars         int    `json:"stargazers_count"`
	Archived      bool   `json:"archived"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type manifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	UserCSS     string `json:"usercss"`
	Main        string `json:"main"`
	Schemes     string `json:"schemes"`
	Branch      string `json:"branch"`
	Authors     []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

// Themes is page (from 1) of the Marketplace's themes that have color
// schemes, and whether a later page has more.
func (m Marketplace) Themes(ctx context.Context, page int) ([]Theme, bool, error) {
	q := url.Values{"q": {"topic:spicetify-themes"}, "per_page": {strconv.Itoa(perPage)}, "sort": {"stars"}, "order": {"desc"}, "page": {strconv.Itoa(max(page, 1))}}
	b, status, err := m.get(ctx, m.api()+"/search/repositories?"+q.Encode())
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return nil, false, ErrRateLimited
	}
	if err != nil {
		return nil, false, err
	}
	var found struct {
		Total int    `json:"total_count"`
		Items []repo `json:"items"`
	}
	if err := json.Unmarshal(b, &found); err != nil {
		return nil, false, fmt.Errorf("spicetify: search: %w", err)
	}
	blocked := m.blocklist(ctx)
	repos := make([]repo, 0, len(found.Items))
	for _, r := range found.Items {
		if !r.Archived && !blocked[strings.ToLower(r.HTMLURL)] && strings.Count(r.FullName, "/") == 1 {
			repos = append(repos, r)
		}
	}
	// Manifests a few at a time (raw.githubusercontent.com throttles
	// bursts), kept in the search's order.
	themes := make([][]Theme, len(repos))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			themes[i] = m.repoThemes(ctx, r)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var out []Theme
	for _, ts := range themes {
		out = append(out, ts...)
	}
	return out, max(page, 1)*perPage < found.Total, nil
}

// blocklist is the Marketplace's blocked repository URLs, lower case; none
// when it cannot be read.
func (m Marketplace) blocklist(ctx context.Context) map[string]bool {
	b, _, err := m.get(ctx, m.raw()+"/spicetify/marketplace/main/resources/blacklist.json")
	if err != nil {
		return nil
	}
	var list struct {
		Repos []string `json:"repos"`
	}
	if json.Unmarshal(b, &list) != nil {
		return nil
	}
	out := make(map[string]bool, len(list.Repos))
	for _, u := range list.Repos {
		out[strings.ToLower(strings.TrimSuffix(u, "/"))] = true
	}
	return out
}

// repoThemes are r's manifest's themes that have color schemes; none when
// it has no readable manifest.
func (m Marketplace) repoThemes(ctx context.Context, r repo) []Theme {
	b, _, err := m.get(ctx, m.raw()+"/"+r.FullName+"/"+r.DefaultBranch+"/manifest.json")
	if err != nil {
		return nil
	}
	var list []manifest
	if strings.HasPrefix(strings.TrimSpace(string(b)), "[") {
		if json.Unmarshal(b, &list) != nil {
			return nil
		}
	} else {
		var one manifest
		if json.Unmarshal(b, &one) != nil {
			return nil
		}
		list = []manifest{one}
	}
	var out []Theme
	for _, mf := range list {
		name, desc := strings.TrimSpace(mf.Name), strings.TrimSpace(mf.Description)
		// A theme entry, as the Marketplace tells them apart, with colors.
		if name == "" || desc == "" || mf.UserCSS == "" || mf.Schemes == "" {
			continue
		}
		branch := r.DefaultBranch
		if mf.Branch != "" {
			branch = mf.Branch
		}
		schemes := mf.Schemes
		if !strings.HasPrefix(schemes, "http") {
			schemes = m.raw() + "/" + r.FullName + "/" + branch + "/" + strings.TrimPrefix(schemes, "/")
		}
		author := r.Owner.Login
		if len(mf.Authors) > 0 && strings.TrimSpace(mf.Authors[0].Name) != "" {
			author = strings.TrimSpace(mf.Authors[0].Name)
		}
		out = append(out, Theme{ID: r.FullName + "/" + name, Name: name, Description: desc, Author: author, Stars: r.Stars, SchemesURL: schemes})
	}
	return out
}

// Schemes are t's color schemes, from its color.ini.
func (m Marketplace) Schemes(ctx context.Context, t Theme) ([]Scheme, error) {
	b, _, err := m.get(ctx, t.SchemesURL)
	if err != nil {
		return nil, err
	}
	schemes, err := Parse(t.Name, strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	if len(schemes) == 0 {
		return nil, fmt.Errorf("spicetify: %s has no color schemes", t.Name)
	}
	return schemes, nil
}
