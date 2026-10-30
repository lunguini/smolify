package webspotify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Scopes is the documented access needed for browsing and Connect playback.
var Scopes = []string{
	"playlist-read-private",
	"playlist-read-collaborative",
	"user-read-private",
	"user-library-read",
	"user-library-modify",
	"user-read-recently-played",
	"user-read-playback-state",
	"user-modify-playback-state",
	"streaming",
}

var ErrAuthState = errors.New("spotify auth: state check failed")
var ErrNoToken = errors.New("spotify auth: no refreshable token")

// Token is kept by a host session store; browser hosts should use
// sessionStorage rather than persistent storage.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type PendingAuth struct {
	Verifier string `json:"verifier"`
	State    string `json:"state"`
}

// SessionStore keeps only short-lived OAuth state and tokens.
type SessionStore interface {
	LoadToken() (Token, error)
	SaveToken(Token) error
	ClearToken() error
	SavePending(PendingAuth) error
	LoadPending() (PendingAuth, error)
	ClearPending() error
}

// Auth runs Authorization Code with PKCE and refreshes tokens.
type Auth struct {
	ClientID    string
	RedirectURI string
	Transport   Transport
	Store       SessionStore
	Now         func() time.Time
	Random      io.Reader

	mu sync.Mutex
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Auth) random() io.Reader {
	if a.Random != nil {
		return a.Random
	}
	return rand.Reader
}

// PKCEChallenge returns the RFC 7636 S256 challenge for verifier.
func PKCEChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func randomURLSafe(r io.Reader, n int) (string, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Begin creates PKCE state, stores the verifier, and returns Spotify's
// authorization URL.
func (a *Auth) Begin() (string, error) {
	if a.ClientID == "" || a.RedirectURI == "" || a.Store == nil {
		return "", errors.New("spotify auth: client id, redirect URI, and session store are required")
	}
	verifier, err := randomURLSafe(a.random(), 32)
	if err != nil {
		return "", err
	}
	state, err := randomURLSafe(a.random(), 32)
	if err != nil {
		return "", err
	}
	if err := a.Store.SavePending(PendingAuth{Verifier: verifier, State: state}); err != nil {
		return "", err
	}
	q := url.Values{
		"client_id":             {a.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {a.RedirectURI},
		"code_challenge_method": {"S256"},
		"code_challenge":        {PKCEChallenge(verifier)},
		"state":                 {state},
		"scope":                 {strings.Join(Scopes, " ")},
	}
	return AuthorizeURL + "?" + q.Encode(), nil
}

// ExchangeCode validates the OAuth state and exchanges a one-time code.
func (a *Auth) ExchangeCode(ctx context.Context, code, state string) error {
	if a.Store == nil || a.Transport == nil || code == "" || state == "" {
		return ErrAuthState
	}
	pending, err := a.Store.LoadPending()
	if err != nil || pending.State == "" || subtle.ConstantTimeCompare([]byte(state), []byte(pending.State)) != 1 {
		_ = a.Store.ClearPending()
		return ErrAuthState
	}
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {a.RedirectURI},
		"client_id":     {a.ClientID},
		"code_verifier": {pending.Verifier},
	}
	token, err := a.tokenRequest(ctx, values)
	if err != nil {
		return err
	}
	if err := a.Store.SaveToken(token); err != nil {
		return err
	}
	return a.Store.ClearPending()
}

// AccessToken returns a current access token, refreshing it when it is about
// to expire.
func (a *Auth) AccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Store == nil {
		return "", ErrNoToken
	}
	token, err := a.Store.LoadToken()
	if err != nil {
		return "", err
	}
	if token.AccessToken != "" && token.ExpiresAt.After(a.now().Add(30*time.Second)) {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		return "", ErrNoToken
	}
	return a.refreshLocked(ctx, token)
}

// Refresh forces a refresh after an API 401, even when the token has not
// reached its recorded expiry.
func (a *Auth) Refresh(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Store == nil {
		return "", ErrNoToken
	}
	token, err := a.Store.LoadToken()
	if err != nil || token.RefreshToken == "" {
		return "", ErrNoToken
	}
	return a.refreshLocked(ctx, token)
}

func (a *Auth) refreshLocked(ctx context.Context, previous Token) (string, error) {
	token, err := a.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {previous.RefreshToken},
		"client_id":     {a.ClientID},
	})
	if err != nil {
		return "", err
	}
	if token.RefreshToken == "" {
		token.RefreshToken = previous.RefreshToken
	}
	if token.Scope == "" {
		token.Scope = previous.Scope
	}
	if err := a.Store.SaveToken(token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (a *Auth) tokenRequest(ctx context.Context, values url.Values) (Token, error) {
	if a.Transport == nil {
		return Token{}, errors.New("spotify auth: missing transport")
	}
	resp, err := a.Transport.Do(ctx, Request{
		Method: "POST", URL: TokenURL,
		Header: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
		Body:   []byte(values.Encode()),
	})
	if err != nil {
		return Token{}, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return Token{}, fmt.Errorf("spotify auth: token endpoint HTTP %d: %s", resp.Status, responseMessage(resp.Body))
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return Token{}, fmt.Errorf("spotify auth: decode token: %w", err)
	}
	if raw.AccessToken == "" {
		return Token{}, errors.New("spotify auth: empty access token")
	}
	return Token{
		AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken,
		TokenType: raw.TokenType, Scope: raw.Scope,
		ExpiresAt: a.now().Add(time.Duration(raw.ExpiresIn) * time.Second),
	}, nil
}

func responseMessage(body []byte) string {
	var value struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &value) == nil {
		return strings.TrimSpace(value.Error + " " + value.Description)
	}
	return "token request rejected"
}
