package spicetify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeGitHub serves a topic search, the Marketplace blocklist, manifests and
// color.ini files, as api.github.com and raw.githubusercontent.com do.
func fakeGitHub(t *testing.T, searchStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var searches atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		searches.Add(1)
		if searchStatus != http.StatusOK {
			w.WriteHeader(searchStatus)
			return
		}
		if q := r.URL.Query().Get("q"); q != "topic:spicetify-themes" {
			t.Errorf("search q %q", q)
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `{"total_count": 102, "items": [{"full_name": "late/theme", "html_url": "https://github.com/late/theme", "default_branch": "main", "stargazers_count": 1}]}`)
			return
		}
		fmt.Fprint(w, `{"total_count": 102, "items": [
			{"full_name": "spicetify/spicetify-themes", "html_url": "https://github.com/spicetify/spicetify-themes", "default_branch": "master", "stargazers_count": 5000, "owner": {"login": "spicetify"}},
			{"full_name": "bad/blocked", "html_url": "https://github.com/bad/blocked", "default_branch": "main", "stargazers_count": 900},
			{"full_name": "old/archived", "html_url": "https://github.com/old/archived", "default_branch": "main", "stargazers_count": 800, "archived": true},
			{"full_name": "solo/one", "html_url": "https://github.com/solo/one", "default_branch": "main", "stargazers_count": 300, "owner": {"login": "solo"}},
			{"full_name": "css/only", "html_url": "https://github.com/css/only", "default_branch": "main", "stargazers_count": 200},
			{"full_name": "broken/manifest", "html_url": "https://github.com/broken/manifest", "default_branch": "main", "stargazers_count": 100}
		]}`)
	})
	mux.HandleFunc("/spicetify/marketplace/main/resources/blacklist.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"repos": ["// a comment", "https://github.com/bad/blocked"]}`)
	})
	mux.HandleFunc("/spicetify/spicetify-themes/master/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"name": "Sleek", "description": "Sleek", "usercss": "Sleek/user.css", "schemes": "Sleek/color.ini", "preview": "Sleek/p.png", "authors": [{"name": "harbassan"}]},
			{"name": "NoColors", "description": "No schemes", "usercss": "NoColors/user.css"},
			{"name": "Extension", "description": "not a theme", "main": "ext.js"}
		]`)
	})
	mux.HandleFunc("/solo/one/main/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"name": "One", "description": "A single theme", "usercss": "user.css", "schemes": "https://example.test/one/color.ini", "branch": "dev"}`)
	})
	mux.HandleFunc("/css/only/main/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"name": "CSS", "description": "css only", "usercss": "user.css"}`)
	})
	mux.HandleFunc("/broken/manifest/main/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{not json`)
	})
	mux.HandleFunc("/spicetify/spicetify-themes/master/Sleek/color.ini", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "[Coral]\ntext = FFFFFF\nbutton = ff7b72\n[Deep]\nmain = 1e2030\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &searches
}

func market(srv *httptest.Server) Marketplace {
	return Marketplace{Client: srv.Client(), API: srv.URL, Raw: srv.URL}
}

func TestMarketplaceListsThemesWithColorSchemes(t *testing.T) {
	srv, _ := fakeGitHub(t, http.StatusOK)
	got, more, err := market(srv).Themes(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !more {
		t.Error("102 results, 100 per page: there is a second page")
	}
	var ids []string
	for _, th := range got {
		ids = append(ids, th.ID)
	}
	// By stars; blocked, archived, CSS-only, scheme-less and broken entries
	// are left out.
	if want := "spicetify/spicetify-themes/Sleek solo/one/One"; strings.Join(ids, " ") != want {
		t.Fatalf("themes %q, want %q", ids, want)
	}
	sleek := got[0]
	if sleek.Name != "Sleek" || sleek.Author != "harbassan" || sleek.Stars != 5000 ||
		sleek.SchemesURL != srv.URL+"/spicetify/spicetify-themes/master/Sleek/color.ini" {
		t.Errorf("sleek %+v", sleek)
	}
	one := got[1]
	if one.SchemesURL != "https://example.test/one/color.ini" || one.Author != "solo" || one.Description != "A single theme" {
		t.Errorf("one %+v", one)
	}
}

func TestMarketplaceSecondPage(t *testing.T) {
	srv, _ := fakeGitHub(t, http.StatusOK)
	got, more, err := market(srv).Themes(context.Background(), 2)
	if err != nil || more || len(got) != 0 { // late/theme has no manifest
		t.Fatalf("page 2: %+v more=%v err=%v", got, more, err)
	}
}

func TestMarketplaceRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		srv, _ := fakeGitHub(t, status)
		if _, _, err := market(srv).Themes(context.Background(), 1); !errors.Is(err, ErrRateLimited) {
			t.Errorf("status %d: err %v, want ErrRateLimited", status, err)
		}
	}
}

func TestMarketplaceSchemes(t *testing.T) {
	srv, _ := fakeGitHub(t, http.StatusOK)
	m := market(srv)
	themes, _, err := m.Themes(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	schemes, err := m.Schemes(context.Background(), themes[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(schemes) != 2 || schemes[0].Name != "Coral" || schemes[1].Name != "Deep" || schemes[0].Theme != "Sleek" {
		t.Fatalf("schemes %+v", schemes)
	}
	if _, err := m.Schemes(context.Background(), Theme{Name: "Gone", SchemesURL: srv.URL + "/nope/color.ini"}); err == nil {
		t.Fatal("a missing color.ini read")
	}
}
