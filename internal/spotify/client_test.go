package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// server answers the search requests in routes, recording the tokens used.
func server(t *testing.T, routes map[string]string, valid string) (*Client, *[]string, *int) {
	t.Helper()
	var tokens []string
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		tokens = append(tokens, tok)
		if tok != valid {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"status":401,"message":"The access token expired"}}`))
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	next := []string{"stale", valid, valid}
	c := &Client{Base: srv.URL, ClientToken: func(context.Context) (string, error) { return "client", nil }, HTTP: srv.Client(), Token: func(context.Context) (string, error) {
		tok := next[min(asked, len(next)-1)]
		asked++
		return tok, nil
	}}
	return c, &tokens, &asked
}

var searchBody = func() string {
	b, err := os.ReadFile("testdata/search.json")
	if err != nil {
		panic(err)
	}
	return string(b)
}()

func TestSearchRetriesARejectedToken(t *testing.T) {
	c, tokens, asked := server(t, map[string]string{"/": searchBody}, "good")
	r, err := c.Search(context.Background(), "  over ", 10)
	if err != nil {
		t.Fatal(err)
	}
	if *asked != 2 || strings.Join(*tokens, ",") != "stale,good" {
		t.Fatalf("asked %d, tokens %v", *asked, *tokens)
	}
	if len(r.Tracks) != 1 || r.Tracks[0].Name != "Overthink" || r.Tracks[0].Album.Name != "Thinking" || r.Tracks[0].Artists[0].Name != "Kuo" {
		t.Fatalf("tracks %+v", r.Tracks)
	}
	if len(r.Albums) != 1 || len(r.Artists) != 1 || len(r.Playlists) != 1 || r.Playlists[0].Owner.DisplayName != "Spotify" {
		t.Fatalf("results %+v", r)
	}
	// The good token is kept: a second search asks for none.
	if _, err := c.Search(context.Background(), "x", 10); err != nil || *asked != 2 {
		t.Fatalf("asked %d err %v", *asked, err)
	}
}

func TestTokenExpires(t *testing.T) {
	c, _, asked := server(t, map[string]string{"/": `{}`}, "good")
	c.Token = func(context.Context) (string, error) { *asked++; return "good", nil }
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }
	c.Search(context.Background(), "x", 5)
	now = now.Add(tokenLife + time.Second)
	c.Search(context.Background(), "x", 5)
	if *asked != 2 {
		t.Fatalf("asked %d, want a new token after %v", *asked, tokenLife)
	}
}

func TestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, ClientToken: func(context.Context) (string, error) { return "client", nil }, Token: func(context.Context) (string, error) { return "t", nil }}
	_, err := c.Search(context.Background(), "x", 1)
	var e Error
	if !errors.As(err, &e) || e.Status != 429 || err.Error() != "Spotify is rate limiting; try again in 7s" {
		t.Fatalf("err %v", err)
	}
	if (Error{Status: 403, Message: "Insufficient client scope"}).Error() != "Spotify: Insufficient client scope (403)" {
		t.Fatal((Error{Status: 403, Message: "Insufficient client scope"}).Error())
	}
	c = &Client{Base: srv.URL, ClientToken: func(context.Context) (string, error) { return "client", nil }, Token: func(context.Context) (string, error) { return "", errors.New("not signed in") }}
	if _, err := c.Search(context.Background(), "x", 1); err == nil || err.Error() != "not signed in" {
		t.Fatalf("token failure: %v", err)
	}
}

func TestCover(t *testing.T) {
	ims := []Image{{URL: "big", Width: 640}, {URL: "small", Width: 64}, {URL: "mid", Width: 300}}
	for px, want := range map[int]string{256: "mid", 64: "small", 1000: "big", 301: "big"} {
		if got := Cover(ims, px); got != want {
			t.Errorf("Cover(%d) = %q, want %q", px, got, want)
		}
	}
	if Cover(nil, 10) != "" {
		t.Error("no images")
	}
}

func TestRateLimitHoldsRequests(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := &Client{Base: srv.URL, ClientToken: func(context.Context) (string, error) { return "client", nil }, Now: func() time.Time { return now }, Token: func(context.Context) (string, error) { return "t", nil }}
	c.Search(context.Background(), "a", 1)
	now = now.Add(100 * time.Second)
	_, err := c.Search(context.Background(), "ab", 1)
	if hits != 1 || err == nil || err.Error() != "Spotify is rate limiting; try again in 20s" {
		t.Fatalf("hits %d, err %v: want the second search held for the 20s left", hits, err)
	}
	now = now.Add(21 * time.Second)
	c.Search(context.Background(), "abc", 1)
	if hits != 2 {
		t.Fatalf("hits %d after the limit ended", hits)
	}
}

func TestSearchRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Client-Token") != "client" || r.Header.Get("User-Agent") != "Spotify/124200290 OSX/0 (librespot)" || r.Header.Get("App-Platform") != "Win32_x86_64" || r.Header.Get("Spotify-App-Version") != "1.2.52.442" || r.Header.Get("Content-Type") != "application/json;charset=UTF-8" {
			t.Errorf("request %s headers %v", r.Method, r.Header)
		}
		var body struct {
			OperationName string
			Variables     struct {
				SearchTerm    string
				Limit, Offset int
			}
			Extensions struct {
				PersistedQuery struct {
					Version    int
					SHA256Hash string
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.OperationName != "searchDesktop" || body.Variables.SearchTerm != `Björk & "live"` || body.Variables.Limit != 30 || body.Variables.Offset != 0 || body.Extensions.PersistedQuery.Version != 1 || body.Extensions.PersistedQuery.SHA256Hash != "eef7cc54888d91bdd6802623477873caa3948ae173a0c34fd86827b267e94c03" {
			t.Errorf("body %+v", body)
		}
		w.Write([]byte(searchBody))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "access", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
	if _, err := c.Search(context.Background(), `  Björk & "live"  `, 30); err != nil {
		t.Fatal(err)
	}
}

func TestSearchInvalidResponses(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"data":null}`, `{"errors":[{"message":"PersistedQueryNotFound"}]}`, `{"data":{"searchV2":null},"errors":[{"message":"rejected"}]}`} {
		t.Run(body, func(t *testing.T) {
			c, _, _ := server(t, map[string]string{"/": body}, "good")
			if _, err := c.Search(context.Background(), "x", 5); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	c := &Client{Token: func(context.Context) (string, error) { t.Fatal("blank search requested token"); return "", nil }}
	if _, err := c.Search(context.Background(), "  ", 30); err != nil {
		t.Fatal(err)
	}
}

func TestSearchCancellation(t *testing.T) {
	c, _, _ := server(t, map[string]string{"/": searchBody}, "good")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Search(ctx, "over", 30); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search: %v", err)
	}
}

func TestSearchAuthFailures(t *testing.T) {
	t.Run("client token failure", func(t *testing.T) {
		c, _, _ := server(t, map[string]string{"/": searchBody}, "good")
		c.ClientToken = func(context.Context) (string, error) { return "", errors.New("client token unavailable") }
		if _, err := c.Search(context.Background(), "over", 30); err == nil || err.Error() != "client token unavailable" {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("missing client token source", func(t *testing.T) {
		c, _, _ := server(t, map[string]string{"/": searchBody}, "good")
		c.ClientToken = nil
		if _, err := c.Search(context.Background(), "over", 30); err == nil || err.Error() != "spotify: no client token source" {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("rejection retries only once", func(t *testing.T) {
		c, tokens, asked := server(t, map[string]string{"/": searchBody}, "never-valid")
		c.Token = func(context.Context) (string, error) { *asked++; return "rejected", nil }
		_, err := c.Search(context.Background(), "over", 30)
		var e Error
		if !errors.As(err, &e) || e.Status != 401 || *asked != 2 || len(*tokens) != 2 {
			t.Fatalf("err %v asked %d tokens %v", err, *asked, *tokens)
		}
	})
}

func TestResetClearsAccountTokenAndRejectsLateToken(t *testing.T) {
	c := &Client{}
	c.Token = func(context.Context) (string, error) { return "old", nil }
	if _, err := c.accessToken(context.Background(), false, c.generation()); err != nil {
		t.Fatal(err)
	}
	c.limit("100")
	c.Reset()
	c.Token = func(context.Context) (string, error) { return "new", nil }
	tok, err := c.accessToken(context.Background(), false, c.generation())
	if err != nil || tok != "new" || c.limited() != nil {
		t.Fatalf("token %q err %v limit %v", tok, err, c.limited())
	}
	started, release := make(chan struct{}), make(chan struct{})
	c.Token = func(context.Context) (string, error) { close(started); <-release; return "late-old", nil }
	done := make(chan error, 1)
	go func() { _, err := c.accessToken(context.Background(), true, c.generation()); done <- err }()
	<-started
	c.Reset()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("late token err %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		t.Fatalf("old token restored after reset: %q", c.token)
	}
}

func TestResetRejectsLateSearchResponse(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "old", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
	done := make(chan error, 1)
	go func() { _, err := c.Search(context.Background(), "old", 30); done <- err }()
	<-started
	c.Reset()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("late search err %v", err)
	}
	if err := c.limited(); err != nil {
		t.Fatalf("old response restored cooldown: %v", err)
	}
}

func TestAccessTokenRejectsResetBetweenRequestAndTokenAcquisition(t *testing.T) {
	c := &Client{Token: func(context.Context) (string, error) { t.Fatal("stale request called token source"); return "old", nil }}
	epoch := c.generation()
	c.Reset()
	if _, err := c.accessToken(context.Background(), false, epoch); !errors.Is(err, context.Canceled) {
		t.Fatalf("stale token acquisition: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		t.Fatalf("stale request cached token %q", c.token)
	}
}

func TestAccessTokenRejectsCancellationBeforeSourceAndCaching(t *testing.T) {
	t.Run("before source", func(t *testing.T) {
		c := &Client{Token: func(context.Context) (string, error) { t.Fatal("canceled request called source"); return "", nil }}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.accessToken(ctx, false, c.generation()); !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("before cache", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		c := &Client{Token: func(context.Context) (string, error) { cancel(); return "ignored-cancellation", nil }}
		if _, err := c.accessToken(ctx, false, c.generation()); !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
		if c.token != "" {
			t.Fatalf("canceled result cached %q", c.token)
		}
	})
}
