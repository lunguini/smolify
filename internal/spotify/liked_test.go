package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestLikedTracksPagesPastMetadataCap(t *testing.T) {
	const total = 2057
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string `json:"operationName"`
			Variables     struct {
				Offset int `json:"offset"`
				Limit  int `json:"limit"`
			} `json:"variables"`
			Extensions struct {
				PersistedQuery struct {
					Hash string `json:"sha256Hash"`
				} `json:"persistedQuery"`
			} `json:"extensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.OperationName != "fetchLibraryTracks" || req.Extensions.PersistedQuery.Hash != fetchLibraryTracksHash || req.Variables.Limit > 50 {
			t.Errorf("request body: %+v", req)
		}
		requests++
		start := req.Variables.Offset
		end := min(start+req.Variables.Limit, total)
		items := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			if i == 1 { // Unavailable metadata still occupies its source slot.
				items = append(items, map[string]any{"track": nil})
				continue
			}
			items = append(items, map[string]any{"addedAt": map[string]any{"isoString": "2024-02-03T04:05:06Z"}, "track": map[string]any{
				"_uri": "spotify:track:" + strconv.Itoa(i),
				"data": map[string]any{"__typename": "Track", "name": "song", "duration": map[string]any{"totalMilliseconds": 1234}},
			}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"me": map[string]any{"library": map[string]any{"tracks": map[string]any{"totalCount": total, "items": items}}}}})
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "access", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
	var seen int
	for offset := 0; offset < total; {
		page, err := c.LikedTracks(context.Background(), offset, 50)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != total || page.Offset != offset || page.Limit != 50 {
			t.Fatalf("page metadata: %+v", page)
		}
		if len(page.Items) == 0 {
			t.Fatalf("empty page at offset %d", offset)
		}
		if offset == 0 && (page.Items[0].Track.URI != "spotify:track:0" || !page.Items[0].AddedAt.Equal(time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC))) {
			t.Fatalf("first item: %+v", page.Items[0])
		}
		if offset == 0 && (page.Items[1].Track.URI != "" || !page.Items[1].AddedAt.IsZero()) {
			t.Fatalf("placeholder slot: %+v", page.Items[1])
		}
		seen += len(page.Items)
		offset += len(page.Items)
	}
	if seen != total || requests != 42 {
		t.Fatalf("seen %d, requests %d", seen, requests)
	}
}

func TestLikedTracksRejectsInvalidPages(t *testing.T) {
	for name, body := range map[string]string{
		"missing total":      `{"data":{"me":{"library":{"tracks":{"items":[]}}}}}`,
		"empty before total": `{"data":{"me":{"library":{"tracks":{"totalCount":2,"items":[]}}}}}`,
		"negative total":     `{"data":{"me":{"library":{"tracks":{"totalCount":-1,"items":[]}}}}}`,
		"items past total":   `{"data":{"me":{"library":{"tracks":{"totalCount":0,"items":[{"track":null}]}}}}}`,
		"missing results":    `{}`,
		"graphql error":      `{"errors":[{"message":"query rejected"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			c := &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "access", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
			if _, err := c.LikedTracks(context.Background(), 0, 50); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}

func TestLikedTracksRejectsPageLargerThanRequestedLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"me":{"library":{"tracks":{"totalCount":2,"items":[{"track":null},{"track":null}]}}}}}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Token: func(context.Context) (string, error) { return "access", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
	if _, err := c.LikedTracks(context.Background(), 0, 1); err == nil {
		t.Fatal("accepted more rows than requested")
	}
}

func TestLikedTracksCancellationAndOffset(t *testing.T) {
	c := &Client{Token: func(context.Context) (string, error) { return "access", nil }, ClientToken: func(context.Context) (string, error) { return "client", nil }}
	if _, err := c.LikedTracks(context.Background(), -1, 20); err == nil {
		t.Fatal("accepted negative offset")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.LikedTracks(ctx, 0, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
}

func TestParseAddedAtObjectAndUnknown(t *testing.T) {
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(`{"isoString":"2025-01-02T03:04:05Z"}`), &raw); err != nil {
		t.Fatal(err)
	}
	if got := parseAddedAt(raw); got.IsZero() || got.Year() != 2025 {
		t.Fatalf("parsed timestamp %v", got)
	}
	if got := parseAddedAt(json.RawMessage(`"not a date"`)); !got.IsZero() {
		t.Fatalf("invalid timestamp %v", got)
	}
}
