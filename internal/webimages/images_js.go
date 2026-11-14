//go:build js && wasm

// Package webimages fetches and decodes browser artwork through fetch.
package webimages

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"

	"github.com/lunguini/smolify/internal/webspotify"
)

type Images struct{ Transport webspotify.FetchTransport }

func (h Images) Fetch(ctx context.Context, url string) (image.Image, error) {
	response, err := h.Transport.Do(ctx, webspotify.Request{Method: "GET", URL: url})
	if err != nil {
		return nil, err
	}
	if response.Status < 200 || response.Status >= 300 {
		return nil, errors.New("cover request failed: HTTP " + strconv.Itoa(response.Status))
	}
	img, _, err := image.Decode(io.LimitReader(bytes.NewReader(response.Body), 8<<20))
	return img, err
}
