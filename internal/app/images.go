package app

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // Spotify's covers
	_ "image/png"
	"io"
	"net/http"
	"time"
)

// HTTPImages fetches cover art over HTTPS and decodes it.
type HTTPImages struct{ Client *http.Client }

// maxImageBytes bounds a cover download.
const maxImageBytes = 8 << 20

func (h HTTPImages) Fetch(ctx context.Context, url string) (image.Image, error) {
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover: %s", resp.Status)
	}
	im, _, err := image.Decode(io.LimitReader(resp.Body, maxImageBytes))
	return im, err
}
