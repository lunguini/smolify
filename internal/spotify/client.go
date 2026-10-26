// Package spotify implements search using Spotify's desktop search service.
// Requests use the embedded player's access and client tokens.
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BaseURL is the desktop query service.
const BaseURL = "https://api-partner.spotify.com/pathfinder/v2/query"

// tokenLife is how long a token is reused before asking again. Spotify's
// last an hour; a rejected one is replaced at once.
const tokenLife = 45 * time.Minute

// Client calls the desktop search service. Token supplies an access token (the player's);
// it is asked once per tokenLife and again when Spotify rejects one.
type Client struct {
	Token       func(context.Context) (string, error)
	ClientToken func(context.Context) (string, error)
	HTTP        *http.Client
	Base        string // complete endpoint; "": BaseURL
	Now         func() time.Time

	mu      sync.Mutex
	epoch   uint64
	token   string
	expires time.Time
	// until is when a rate limit ends: no request is sent before then.
	until time.Time
}

// Error is a Spotify service failure.
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
func (c *Client) accessToken(ctx context.Context, fresh bool, epoch uint64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	if epoch != c.epoch {
		c.mu.Unlock()
		return "", context.Canceled
	}
	if !fresh && c.token != "" && c.now().Before(c.expires) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.Token == nil {
		return "", errors.New("spotify: no token source")
	}
	t, err := c.Token(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	if ctx.Err() != nil {
		c.mu.Unlock()
		return "", ctx.Err()
	}
	if epoch != c.epoch {
		c.mu.Unlock()
		return "", context.Canceled
	}
	c.token, c.expires = t, c.now().Add(tokenLife)
	c.mu.Unlock()
	return t, nil
}

// post retries once when the access token is rejected.
func (c *Client) post(ctx context.Context, body []byte, out any) error {
	epoch := c.generation()
	err := c.postWith(ctx, false, body, out, epoch)
	var e Error
	if errors.As(err, &e) && e.Status == http.StatusUnauthorized {
		err = c.postWith(ctx, true, body, out, epoch)
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
func (c *Client) limit(retryAfter string, epoch ...uint64) {
	wait := defaultRetry
	if n, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && n >= 0 {
		wait = time.Duration(n) * time.Second
	}
	c.mu.Lock()
	if len(epoch) == 0 || epoch[0] == c.epoch {
		c.until = c.now().Add(wait)
	}
	c.mu.Unlock()
}

func (c *Client) postWith(ctx context.Context, fresh bool, body []byte, out any, epoch uint64) error {
	if err := c.limited(); err != nil {
		return err
	}
	token, err := c.accessToken(ctx, fresh, epoch)
	if err != nil {
		return err
	}
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	if c.ClientToken == nil {
		return errors.New("spotify: no client token source")
	}
	ct, err := c.ClientToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Token", ct)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("App-Platform", "Win32_x86_64")
	req.Header.Set("Spotify-App-Version", "1.2.52.442")
	req.Header.Set("User-Agent", "Spotify/124200290 OSX/0 (librespot)")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
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
	c.mu.Lock()
	stale := epoch != c.epoch
	c.mu.Unlock()
	if stale {
		return context.Canceled
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := Error{Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
		if e.Status == http.StatusTooManyRequests {
			c.limit(e.RetryAfter, epoch)
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

// Reset forgets account-specific authentication and rate limits. A token
// request already in flight cannot repopulate the cache after this call.
func (c *Client) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	c.token = ""
	c.expires = time.Time{}
	c.until = time.Time{}
}

// generation fixes the account identity for a whole request, including retries.
func (c *Client) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.epoch
}
