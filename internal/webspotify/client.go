package webspotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPError is a documented Web API response failure.
type HTTPError struct {
	Status     int
	RetryAfter string
	Message    string
}

func (e HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Spotify Web API: %s (HTTP %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("Spotify Web API: HTTP %d", e.Status)
}

func (e HTTPError) HTTPStatus() int         { return e.Status }
func (e HTTPError) RetryAfterValue() string { return e.RetryAfter }

// API is an OAuth-authorized Spotify Web API client.
type API struct {
	Auth          *Auth
	Transport     Transport
	BaseURL       string
	Now           func() time.Time
	Wait          func(context.Context, time.Duration) error
	Max429Retries int
}

func NewAPI(auth *Auth, transport Transport) *API {
	return &API{Auth: auth, Transport: transport, BaseURL: DefaultAPIBase, Max429Retries: 3}
}

func (c *API) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *API) wait(ctx context.Context, d time.Duration) error {
	if c.Wait != nil {
		return c.Wait(ctx, d)
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *API) request(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	if c.Auth == nil || c.Transport == nil {
		return nil, errors.New("spotify Web API: missing auth or transport")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultAPIBase
	}
	endpoint := base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	token, err := c.Auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	refreshed := false
	rateRetries := 0
	max429 := c.Max429Retries
	if max429 <= 0 {
		max429 = 3
	}
	for {
		header := map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json"}
		if body != nil {
			header["Content-Type"] = "application/json"
		}
		resp, err := c.Transport.Do(ctx, Request{Method: method, URL: endpoint, Header: header, Body: data})
		if err != nil {
			return nil, err
		}
		if resp.Status == 401 && !refreshed {
			token, err = c.Auth.Refresh(ctx)
			if err != nil {
				return nil, err
			}
			refreshed = true
			continue
		}
		if resp.Status == 429 && rateRetries < max429 {
			rateRetries++
			if err := c.wait(ctx, retryDuration(resp.Header["retry-after"], c.now())); err != nil {
				return nil, err
			}
			continue
		}
		if resp.Status < 200 || resp.Status >= 300 {
			return nil, HTTPError{Status: resp.Status, RetryAfter: resp.Header["retry-after"], Message: apiErrorMessage(resp.Body)}
		}
		return resp.Body, nil
	}
}

func retryDuration(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := time.Parse(time.RFC1123, value); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
		return 0
	}
	return 30 * time.Second
}

func apiErrorMessage(body []byte) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	value := raw["error"]
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(value, &detail) == nil {
		return detail.Message
	}
	return ""
}
