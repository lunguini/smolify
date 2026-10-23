// Package spotify is the slice of Spotify's Web API that the embedded player
// does not serve: search and recently played. It signs requests
// with the player's access token; no developer app is needed.
package spotify

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
	"time"
)

// BaseURL is the Web API's root.
const BaseURL = "https://api.spotify.com/v1"

// tokenLife is how long a token is reused before asking again. Spotify's
// last an hour; a rejected one is replaced at once.
const tokenLife = 45 * time.Minute

// Client calls the Web API. Token supplies an access token (the player's);
// it is asked once per tokenLife and again when Spotify rejects one.
type Client struct {
	Token func(context.Context) (string, error)
	HTTP  *http.Client
	Base  string // "": BaseURL
	Now   func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
	// until is when a rate limit ends: no request is sent before then.
	until time.Time
}

// Error is a Web API failure.
type Error struct {
	Status     int
	RetryAfter string
	Message    string
}

func (e Error) Error() string {
	switch {
	case e.Status == http.StatusTooManyRequests && e.RetryAfter != "":
		return "Spotify is rate limiting; try again in " + e.RetryAfter + "s"
	case e.Message != "":
		return fmt.Sprintf("Spotify: %s (%d)", e.Message, e.Status)
	}
	return fmt.Sprintf("Spotify: HTTP %d", e.Status)
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// accessToken is the cached token, or a new one when it is stale or fresh
// is set.
func (c *Client) accessToken(ctx context.Context, fresh bool) (string, error) {
	c.mu.Lock()
	if !fresh && c.token != "" && c.now().Before(c.expires) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	if c.Token == nil {
		return "", errors.New("spotify: no token source")
	}
	t, err := c.Token(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token, c.expires = t, c.now().Add(tokenLife)
	c.mu.Unlock()
	return t, nil
}

// get fetches path with query into out, asking for a new token once if the
// cached one is rejected.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	err := c.getWith(ctx, false, path, query, out)
	var e Error
	if errors.As(err, &e) && e.Status == http.StatusUnauthorized {
		err = c.getWith(ctx, true, path, query, out)
	}
	return err
}

// defaultRetry is the wait after a 429 that names none.
const defaultRetry = 30 * time.Second

// limited is the error for a request inside a rate limit, if one holds.
func (c *Client) limited() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	left := c.until.Sub(c.now())
	if left <= 0 {
		return nil
	}
	return Error{Status: http.StatusTooManyRequests, RetryAfter: strconv.Itoa(int(left.Round(time.Second) / time.Second))}
}

// limit starts a rate limit of retryAfter seconds ("": defaultRetry).
func (c *Client) limit(retryAfter string) {
	wait := defaultRetry
	if n, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && n >= 0 {
		wait = time.Duration(n) * time.Second
	}
	c.mu.Lock()
	c.until = c.now().Add(wait)
	c.mu.Unlock()
}

func (c *Client) getWith(ctx context.Context, fresh bool, path string, query url.Values, out any) error {
	if err := c.limited(); err != nil {
		return err
	}
	token, err := c.accessToken(ctx, fresh)
	if err != nil {
		return err
	}
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	endpoint := base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := Error{Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
		if e.Status == http.StatusTooManyRequests {
			c.limit(e.RetryAfter)
		}
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &body) == nil {
			e.Message = body.Error.Message
		}
		return e
	}
	return json.Unmarshal(data, out)
}

// Results are what a search found.
type Results struct {
	Tracks    []Track
	Albums    []Album
	Artists   []Artist
	Playlists []Playlist
}

// Search looks q up across tracks, albums, artists, and playlists.
func (c *Client) Search(ctx context.Context, q string, limit int) (Results, error) {
	var out struct {
		Tracks    page[Track]    `json:"tracks"`
		Albums    page[Album]    `json:"albums"`
		Artists   page[Artist]   `json:"artists"`
		Playlists page[Playlist] `json:"playlists"`
	}
	query := url.Values{"q": {strings.TrimSpace(q)}, "type": {"track,album,artist,playlist"}, "limit": {strconv.Itoa(limit)}}
	if err := c.get(ctx, "/search", query, &out); err != nil {
		return Results{}, err
	}
	return Results{Tracks: out.Tracks.Items, Albums: out.Albums.Items, Artists: out.Artists.Items, Playlists: nonNil(out.Playlists.Items)}, nil
}

// nonNil drops the null entries Spotify returns for playlists it cannot
// show.
func nonNil(pls []Playlist) []Playlist {
	out := pls[:0]
	for _, p := range pls {
		if p.URI != "" {
			out = append(out, p)
		}
	}
	return out
}

// RecentlyPlayed is the user's last played tracks, newest first.
func (c *Client) RecentlyPlayed(ctx context.Context, limit int) ([]Played, error) {
	var out page[Played]
	if err := c.get(ctx, "/me/player/recently-played", url.Values{"limit": {strconv.Itoa(limit)}}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}
