package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// server answers the Web API paths in routes, recording the tokens used.
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
	c := &Client{Base: srv.URL, HTTP: srv.Client(), Token: func(context.Context) (string, error) {
		tok := next[min(asked, len(next)-1)]
		asked++
		return tok, nil
	}}
	return c, &tokens, &asked
}

const searchBody = `{
 "tracks": {"items": [{"uri": "spotify:track:1", "name": "Overthink", "duration_ms": 200000,
   "artists": [{"uri": "spotify:artist:k", "name": "Kuo"}],
   "album": {"uri": "spotify:album:t", "name": "Thinking", "images": [{"url": "big", "width": 640}, {"url": "small", "width": 64}, {"url": "mid", "width": 300}]}}]},
 "albums": {"items": [{"uri": "spotify:album:t", "name": "Thinking"}]},
 "artists": {"items": [{"uri": "spotify:artist:k", "name": "Kuo"}]},
 "playlists": {"items": [null, {"uri": "spotify:playlist:p", "name": "Mix", "owner": {"display_name": "Spotify"}}]}
}`

func TestSearchRetriesARejectedToken(t *testing.T) {
	c, tokens, asked := server(t, map[string]string{"/search": searchBody}, "good")
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
	c, _, asked := server(t, map[string]string{"/search": `{}`}, "good")
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
	c := &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "t", nil }}
	_, err := c.Search(context.Background(), "x", 1)
	var e Error
	if !errors.As(err, &e) || e.Status != 429 || err.Error() != "Spotify is rate limiting; try again in 7s" {
		t.Fatalf("err %v", err)
	}
	if (Error{Status: 403, Message: "Insufficient client scope"}).Error() != "Spotify: Insufficient client scope (403)" {
		t.Fatal((Error{Status: 403, Message: "Insufficient client scope"}).Error())
	}
	c = &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "", errors.New("not signed in") }}
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
	c := &Client{Base: srv.URL, Now: func() time.Time { return now }, Token: func(context.Context) (string, error) { return "t", nil }}
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
