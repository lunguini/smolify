//go:build js && wasm

package webspotify

import (
	"errors"
	"syscall/js"
)

// WebCryptoReader uses the browser's cryptographic random generator. It keeps
// PKCE verifier and state generation secure under both Go and TinyGo.
type WebCryptoReader struct{}

func (WebCryptoReader) Read(buffer []byte) (int, error) {
	crypto := js.Global().Get("crypto")
	if crypto.IsUndefined() || crypto.IsNull() {
		return 0, errors.New("browser cryptographic random source is unavailable")
	}
	array := js.Global().Get("Uint8Array").New(len(buffer))
	if _, err := safeGetRandomValues(crypto, array); err != nil {
		return 0, err
	}
	n := js.CopyBytesToGo(buffer, array)
	if n != len(buffer) {
		return n, errors.New("browser cryptographic random source returned short data")
	}
	return n, nil
}

func safeGetRandomValues(crypto, array js.Value) (value js.Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.New("browser cryptographic random source failed")
		}
	}()
	return crypto.Call("getRandomValues", array), nil
}
