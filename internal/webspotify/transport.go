// Package webspotify implements Spotify's documented OAuth and Web API
// flows without depending on the desktop player's private services.
package webspotify

import "context"

const (
	DefaultAPIBase = "https://api.spotify.com/v1"
	AuthorizeURL   = "https://accounts.spotify.com/authorize"
	TokenURL       = "https://accounts.spotify.com/api/token"
)

// Request and Response are the small transport boundary shared by native
// tests and the browser fetch implementation.
type Request struct {
	Method string
	URL    string
	Header map[string]string
	Body   []byte
}

type Response struct {
	Status int
	Header map[string]string
	Body   []byte
}

// Transport sends a request using the current host's HTTP implementation.
type Transport interface {
	Do(context.Context, Request) (Response, error)
}

// FetchTransport is available in the js/wasm build and calls window.fetch.
type FetchTransport struct{}
