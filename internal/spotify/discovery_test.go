package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discoveryClient(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		handler(w, r, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "token", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
}

func TestHomeUsesPersonalizedDesktopOperationAndNormalizesShelves(t *testing.T) {
	c := discoveryClient(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		if body["operationName"] != "home" {
			t.Errorf("operation = %v", body["operationName"])
		}
		vars := body["variables"].(map[string]any)
		if vars["sectionItemsLimit"] != float64(10) || vars["homeEndUserIntegration"] != "INTEGRATION_WEB_PLAYER" {
			t.Errorf("variables = %#v", vars)
		}
		_, _ = w.Write([]byte(`{
  "data": { "home": {
    "greeting": {"transformedLabel": "Good evening"},
    "sectionContainer": {"sections": {"items": [{
      "uri": "spotify:section:a",
      "data": {"title": {"text": "Made for you"}},
      "sectionItems": {"items": [
        {"content": {"__typename": "PlaylistResponseWrapper", "data": {"uri": "spotify:playlist:mix", "name": "Daily Mix 1", "images": {"items": [{"sources": [{"url": "cover", "width": 300}]}]}}}},
        {"content": {"__typename": "AlbumResponseWrapper", "data": {"uri": "spotify:album:a", "name": "Album", "coverArt": {"sources": [{"url": "album", "width": 300}]}, "artists": {"items": [{"uri": "spotify:artist:x", "profile": {"name": "Artist"}}]}}}}
      ]}
    }]}}
  }}
}`))
	})
	feed, err := c.Home(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if feed.Greeting != "Good evening" || len(feed.Sections) != 1 || len(feed.Sections[0].Items) != 2 {
		t.Fatalf("feed = %+v", feed)
	}
	if feed.Sections[0].Items[0].Kind != "playlist" || feed.Sections[0].Items[0].Image != "cover" {
		t.Fatalf("playlist = %+v", feed.Sections[0].Items[0])
	}
	if feed.Sections[0].Items[1].Kind != "album" || feed.Sections[0].Items[1].Artists[0].Name != "Artist" {
		t.Fatalf("album = %+v", feed.Sections[0].Items[1])
	}
}

func TestArtistAndAlbumNormalizeCatalogTracks(t *testing.T) {
	var calls int
	c := discoveryClient(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		calls++
		vars := body["variables"].(map[string]any)
		switch body["operationName"] {
		case "queryArtistOverview":
			if vars["uri"] != "spotify:artist:a" {
				t.Errorf("artist vars = %#v", vars)
			}
			_, _ = w.Write([]byte(`{"data":{"artistUnion":{"profile":{"name":"Artist"},"visuals":{"avatarImage":{"sources":[{"url":"artist-cover","width":300}]}},"discography":{"topTracks":{"items":[{"track":{"uri":"spotify:track:t","name":"Song","duration":{"totalMilliseconds":1200},"artists":{"items":[{"uri":"spotify:artist:a","profile":{"name":"Artist"}}]},"albumOfTrack":{"uri":"spotify:album:z","name":"Album"}}}]}}}}}`))
		case "getAlbum":
			if vars["uri"] != "spotify:album:z" || vars["limit"] != float64(50) {
				t.Errorf("album vars = %#v", vars)
			}
			_, _ = w.Write([]byte(`{"data":{"albumUnion":{"name":"Album","type":"ALBUM","coverArt":{"sources":[{"url":"album-cover","width":300}]},"artists":{"items":[{"uri":"spotify:artist:a","profile":{"name":"Artist"}}]},"tracksV2":{"totalCount":1,"items":[{"track":{"uri":"spotify:track:t","name":"Song","duration":{"totalMilliseconds":1200},"artists":{"items":[{"uri":"spotify:artist:a","profile":{"name":"Artist"}}]}}}]}}}}`))
		default:
			t.Errorf("operation = %v", body["operationName"])
		}
	})
	artist, err := c.Artist(context.Background(), "spotify:artist:a")
	if err != nil {
		t.Fatal(err)
	}
	if artist.Artist.Name != "Artist" || artist.Image != "artist-cover" || len(artist.TopTracks) != 1 || artist.TopTracks[0].URI != "spotify:track:t" {
		t.Fatalf("artist = %+v", artist)
	}
	album, err := c.Album(context.Background(), "spotify:album:z")
	if err != nil {
		t.Fatal(err)
	}
	if album.Album.Name != "Album" || album.Total != 1 || len(album.Tracks) != 1 || album.Tracks[0].Album.URI != "spotify:album:z" {
		t.Fatalf("album = %+v", album)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestAlbumFollowsAllPagesAndPreservesUnavailableSlots(t *testing.T) {
	var offsets []int
	c := discoveryClient(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		vars := body["variables"].(map[string]any)
		offset := int(vars["offset"].(float64))
		offsets = append(offsets, offset)
		n := 50
		if offset == 50 {
			n = 3
		}
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{"track": map[string]any{"uri": "spotify:track:x", "name": "Song"}}
		}
		if offset == 0 {
			items[1] = map[string]any{"track": nil}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"albumUnion": map[string]any{"name": "Long album", "tracksV2": map[string]any{"totalCount": 53, "items": items}}}})
	})
	album, err := c.Album(context.Background(), "spotify:album:long")
	if err != nil || len(album.Tracks) != 53 || album.Total != 53 || len(offsets) != 2 || offsets[1] != 50 {
		t.Fatalf("Paging total=%d tracks=%d offsets=%v err=%v", album.Total, len(album.Tracks), offsets, err)
	}
	if album.Tracks[1].URI != "" {
		t.Fatal("Unavailable item changed paging positions")
	}
}

func TestCatalogMissingOrInconsistentResponsesFail(t *testing.T) {
	for _, body := range []string{`{}`, `{"errors":[{"message":"Unavailable"}]}`, `{"data":{"albumUnion":{"name":"A","tracksV2":{"totalCount":2,"items":[]}}}}`} {
		c := discoveryClient(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { w.Write([]byte(body)) })
		if _, err := c.Album(context.Background(), "spotify:album:a"); err == nil {
			t.Fatal("Accepted incomplete album")
		}
	}
}
