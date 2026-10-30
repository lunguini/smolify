//go:build !js || !wasm

package webspotify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

type testSessionStore struct {
	token   Token
	pending PendingAuth
}

func (s *testSessionStore) LoadToken() (Token, error)         { return s.token, nil }
func (s *testSessionStore) SaveToken(token Token) error       { s.token = token; return nil }
func (s *testSessionStore) ClearToken() error                 { s.token = Token{}; return nil }
func (s *testSessionStore) SavePending(p PendingAuth) error   { s.pending = p; return nil }
func (s *testSessionStore) LoadPending() (PendingAuth, error) { return s.pending, nil }
func (s *testSessionStore) ClearPending() error               { s.pending = PendingAuth{}; return nil }

type fakeTransport struct {
	requests  []Request
	responses []Response
}

func (f *fakeTransport) Do(_ context.Context, request Request) (Response, error) {
	f.requests = append(f.requests, request)
	if len(f.responses) == 0 {
		return Response{}, errors.New("unexpected request")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestPKCEChallengeAndStateValidation(t *testing.T) {
	store := &testSessionStore{}
	transport := &fakeTransport{}
	auth := &Auth{
		ClientID: "client-id", RedirectURI: "https://example.test/",
		Transport: transport, Store: store, Random: strings.NewReader(strings.Repeat("a", 64)),
	}
	urlText, err := auth.Begin()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(urlText)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "accounts.spotify.com" || parsed.Path != "/authorize" {
		t.Fatalf("authorize URL = %s", urlText)
	}
	query := parsed.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") != PKCEChallenge(store.pending.Verifier) {
		t.Fatalf("PKCE parameters = %v", query)
	}
	if query.Get("state") != store.pending.State || len(store.pending.Verifier) < 43 {
		t.Fatalf("pending state = %+v", store.pending)
	}
	err = auth.ExchangeCode(context.Background(), "one-time-code", "wrong-state")
	if !errors.Is(err, ErrAuthState) || store.pending.State != "" {
		t.Fatalf("state mismatch err=%v pending=%+v", err, store.pending)
	}
}

func TestPKCECodeExchangeStoresToken(t *testing.T) {
	store := &testSessionStore{}
	transport := &fakeTransport{responses: []Response{{Status: 200, Body: []byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600,"scope":"streaming"}`)}}}
	auth := &Auth{
		ClientID: "client-id", RedirectURI: "https://example.test/",
		Transport: transport, Store: store, Random: strings.NewReader(strings.Repeat("b", 64)),
		Now: func() time.Time { return time.Unix(100, 0) },
	}
	if _, err := auth.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := auth.ExchangeCode(context.Background(), "code", store.pending.State); err != nil {
		t.Fatal(err)
	}
	if store.token.AccessToken != "access" || store.token.RefreshToken != "refresh" || !store.pendingIsEmpty() {
		t.Fatalf("stored auth = %+v pending=%+v", store.token, store.pending)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "POST" || !strings.Contains(string(transport.requests[0].Body), "code_verifier=") {
		t.Fatalf("token request = %+v", transport.requests)
	}
}

func TestTokenRefreshKeepsRotatingRefreshToken(t *testing.T) {
	store := &testSessionStore{token: Token{AccessToken: "old", RefreshToken: "refresh", Scope: "streaming", ExpiresAt: time.Unix(1, 0)}}
	transport := &fakeTransport{responses: []Response{{Status: 200, Body: []byte(`{"access_token":"new","token_type":"Bearer","expires_in":3600}`)}}}
	auth := &Auth{
		ClientID: "client-id", RedirectURI: "https://example.test/", Transport: transport, Store: store,
		Now: func() time.Time { return time.Unix(200, 0) },
	}
	got, err := auth.AccessToken(context.Background())
	if err != nil || got != "new" {
		t.Fatalf("AccessToken = %q, err=%v", got, err)
	}
	if store.token.RefreshToken != "refresh" || store.token.Scope != "streaming" {
		t.Fatalf("refresh fields lost: %+v", store.token)
	}
	var body url.Values
	body, err = url.ParseQuery(string(transport.requests[0].Body))
	if err != nil || body.Get("grant_type") != "refresh_token" || body.Get("refresh_token") != "refresh" {
		t.Fatalf("refresh body = %q, err=%v", transport.requests[0].Body, err)
	}
}

func (s *testSessionStore) pendingIsEmpty() bool { return s.pending == (PendingAuth{}) }

func TestTokenJSONIsSimple(t *testing.T) {
	data, err := json.Marshal(Token{AccessToken: "x", ExpiresAt: time.Unix(1, 0)})
	if err != nil || !strings.Contains(string(data), `"access_token":"x"`) {
		t.Fatalf("marshal token = %s, err=%v", data, err)
	}
	var _ io.Reader = strings.NewReader("")
}
