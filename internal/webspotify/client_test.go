//go:build !js || !wasm

package webspotify

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testAPI(server string) *API {
	store := &testSessionStore{token: Token{AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresAt: time.Now().Add(time.Hour)}}
	auth := &Auth{ClientID: "test-client", RedirectURI: "https://example.test/", Transport: HTTPTransport{}, Store: store}
	api := NewAPI(auth, HTTPTransport{})
	api.BaseURL = server
	return api
}

func TestSearchUsesDocumentedEndpointAndHonors429(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/search" || r.Header.Get("Authorization") != "Bearer test-access" {
			t.Errorf("request %s auth=%q", r.URL, r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("type") != "track,album,artist,playlist" {
			t.Errorf("search params = %v", r.URL.Query())
		}
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tracks":{"items":[{"uri":"spotify:track:x","name":"Blue","duration_ms":123000,"artists":[{"uri":"spotify:artist:a","name":"A"}],"album":{"uri":"spotify:album:b","name":"B","images":[{"url":"https://img.test/b","width":320}]}}]},"albums":{"items":[{"uri":"spotify:album:b","name":"B","images":[{"url":"https://img.test/b","width":320}]}]},"artists":{"items":[{"uri":"spotify:artist:a","name":"A"}]},"playlists":{"items":[{"uri":"spotify:playlist:p","name":"P","owner":{"display_name":"Demo"},"images":[]}]}}`)
	}))
	defer server.Close()
	api := testAPI(server.URL)
	waited := time.Duration(0)
	api.Wait = func(_ context.Context, d time.Duration) error { waited = d; return nil }
	result, err := api.Search(context.Background(), "blue hour", 10)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || waited != 2*time.Second {
		t.Fatalf("calls=%d waited=%v", calls, waited)
	}
	if len(result.Tracks) != 1 || result.Tracks[0].Album.URI != "spotify:album:b" || len(result.Albums) != 1 || len(result.Artists) != 1 || len(result.Playlists) != 1 {
		t.Fatalf("search results = %+v", result)
	}
}

func TestPlaylistTracksUseCurrentItemsRoute(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/playlists/p/items" {
			t.Errorf("unexpected playlist path %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"total":1,"items":[{"item":{"type":"track","uri":"spotify:track:x","name":"Blue","duration_ms":90000,"track_number":1,"artists":[{"uri":"spotify:artist:a","name":"A"}],"album":{"uri":"spotify:album:b","name":"B","images":[{"url":"https://img.test/b","width":320}]}}}]}`)
	}))
	defer server.Close()
	listing, err := testAPI(server.URL).ContextTracks(context.Background(), "spotify:playlist:p")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !listing.Complete() || listing.Tracks[0].URI != "spotify:track:x" || listing.Tracks[0].Album != "B" {
		t.Fatalf("listing = %+v calls=%d", listing, calls)
	}
}

func TestLibraryWritesUseURIEndpointAndFortyItemBatches(t *testing.T) {
	var batches []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/me/library" {
			t.Errorf("unexpected library request %s %s", r.Method, r.URL.Path)
		}
		if r.Body != http.NoBody && r.ContentLength > 0 {
			t.Errorf("URI library writes should use query parameters, got body length %d", r.ContentLength)
		}
		uris, _ := url.QueryUnescape(r.URL.Query().Get("uris"))
		batches = append(batches, len(strings.Split(uris, ",")))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	uris := make([]string, 41)
	for i := range uris {
		uris[i] = fmt.Sprintf("spotify:track:%d", i)
	}
	if err := testAPI(server.URL).SetLiked(context.Background(), uris, true); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || batches[0] != 40 || batches[1] != 1 {
		t.Fatalf("URI batch sizes = %v", batches)
	}
}
