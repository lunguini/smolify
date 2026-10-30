//go:build !js || !wasm

package webspotify

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPTransport is used by native tests and API client tooling.
type HTTPTransport struct{ Client *http.Client }

func (h HTTPTransport) Do(ctx context.Context, request Request) (Response, error) {
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, request.URL, bytes.NewReader(request.Body))
	if err != nil {
		return Response{}, err
	}
	for key, value := range request.Header {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Response{}, err
	}
	header := make(map[string]string)
	for _, key := range []string{"Retry-After", "Content-Type"} {
		if value := resp.Header.Get(key); value != "" {
			header[strings.ToLower(key)] = value
		}
	}
	return Response{Status: resp.StatusCode, Header: header, Body: body}, nil
}
