//go:build !js || !wasm

package webspotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/lunguini/smolify/internal/model"
)

func TestPlaybackControlsUseDocumentedPlayerRoutes(t *testing.T) {
	type call struct {
		Method string
		Path   string
		Query  url.Values
		Body   map[string]any
	}
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query()}
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			if err := json.Unmarshal(body, &item.Body); err != nil {
				t.Errorf("decode request body %q: %v", body, err)
			}
		}
		calls = append(calls, item)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	api := testAPI(server.URL)
	ctx := context.Background()
	if err := api.TransferPlayback(ctx, "device", true); err != nil {
		t.Fatal(err)
	}
	if err := api.StartPlayback(ctx, "device", "spotify:playlist:list", "spotify:track:pick", nil); err != nil {
		t.Fatal(err)
	}
	if err := api.Pause(ctx, "device"); err != nil {
		t.Fatal(err)
	}
	if err := api.Resume(ctx, "device"); err != nil {
		t.Fatal(err)
	}
	if err := api.Next(ctx, "device"); err != nil {
		t.Fatal(err)
	}
	if err := api.Previous(ctx, "device"); err != nil {
		t.Fatal(err)
	}
	if err := api.Seek(ctx, "device", 1250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := api.SetVolume(ctx, "device", 63); err != nil {
		t.Fatal(err)
	}
	if err := api.SetShuffle(ctx, "device", true); err != nil {
		t.Fatal(err)
	}
	if err := api.SetRepeat(ctx, "device", model.Repeat{Track: true}); err != nil {
		t.Fatal(err)
	}
	if err := api.AddToQueue(ctx, "device", "spotify:track:queued"); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(calls))
	for i, got := range calls {
		paths[i] = got.Method + " " + got.Path
	}
	want := []string{
		"PUT /me/player", "PUT /me/player/play", "PUT /me/player/pause", "PUT /me/player/play",
		"POST /me/player/next", "POST /me/player/previous", "PUT /me/player/seek",
		"PUT /me/player/volume", "PUT /me/player/shuffle", "PUT /me/player/repeat", "POST /me/player/queue",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("player routes = %v, want %v", paths, want)
	}
	if calls[1].Query.Get("device_id") != "device" || calls[1].Body["context_uri"] != "spotify:playlist:list" {
		t.Fatalf("start request = %+v", calls[1])
	}
	offset, ok := calls[1].Body["offset"].(map[string]any)
	if !ok || offset["uri"] != "spotify:track:pick" {
		t.Fatalf("start offset = %#v", calls[1].Body["offset"])
	}
	if calls[6].Query.Get("position_ms") != "1250" || calls[7].Query.Get("volume_percent") != "63" || calls[8].Query.Get("state") != "true" || calls[9].Query.Get("state") != "track" || calls[10].Query.Get("uri") != "spotify:track:queued" {
		t.Fatalf("player query params = %+v", calls)
	}
}

func TestLikedSongsPlaybackUsesTrackURIs(t *testing.T) {
	var got callBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	api := testAPI(server.URL)
	err := api.StartPlayback(context.Background(), "device", model.LikedSongs("user"), "spotify:track:b", []string{"spotify:track:a", "spotify:track:b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.URIs) != 2 || got.URIs[1] != "spotify:track:b" || got.ContextURI != "" {
		t.Fatalf("saved-library playback body = %+v", got)
	}
	if got.Offset["uri"] != "spotify:track:b" {
		t.Fatalf("saved-library playback offset = %+v", got.Offset)
	}
}

func TestLikedSongsPlaybackFetchesQueueWhenUnknown(t *testing.T) {
	var got callBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/me/tracks" {
			items := `{"track":{"uri":"spotify:track:a"}},{"track":{"uri":"spotify:track:b"}}`
			if r.URL.Query().Get("offset") != "0" {
				items = `{"track":{"uri":"spotify:track:c"}}`
			}
			_, _ = io.WriteString(w, `{"items":[`+items+`],"total":3}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	api := testAPI(server.URL)
	if err := api.StartPlayback(context.Background(), "device", model.LikedSongs("user"), "spotify:track:c", nil); err != nil {
		t.Fatal(err)
	}
	if len(got.URIs) != 3 || got.Offset["uri"] != "spotify:track:c" {
		t.Fatalf("saved-library playback body = %+v", got)
	}
}

type callBody struct {
	URIs       []string          `json:"uris"`
	ContextURI string            `json:"context_uri"`
	Offset     map[string]string `json:"offset"`
}
