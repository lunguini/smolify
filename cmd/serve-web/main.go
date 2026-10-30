//go:build !js || !wasm

// serve-web serves the generated dist/web site with precompressed assets.
package main

import (
	"flag"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	dir := flag.String("dir", "dist/web", "static site directory")
	flag.Parse()
	root, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving %s at http://%s", root, *addr)
	log.Fatal(http.ListenAndServe(*addr, staticSite(root)))
}

func staticSite(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if strings.HasPrefix(name, "../") || name == ".." {
			http.NotFound(w, r)
			return
		}
		encoding := ""
		asset := name
		accepts := r.Header.Get("Accept-Encoding")
		for _, candidate := range []struct{ name, encoding string }{{".br", "br"}, {".gz", "gzip"}} {
			if acceptsEncoding(accepts, candidate.encoding) {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name+candidate.name))); err == nil {
					asset, encoding = name+candidate.name, candidate.encoding
					break
				}
			}
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(asset)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(name))
		if filepath.Ext(name) == ".wasm" {
			contentType = "application/wasm"
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Vary", "Accept-Encoding")
		if encoding != "" {
			w.Header().Set("Content-Encoding", encoding)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			return
		}
		if _, err := w.Write(data); err != nil {
			log.Printf("serve %q: %v", name, err)
		}
	})
}

func acceptsEncoding(header, encoding string) bool {
	for _, value := range strings.Split(header, ",") {
		parts := strings.Split(value, ";")
		name := strings.TrimSpace(parts[0])
		if name == encoding || name == "*" {
			for _, parameter := range parts[1:] {
				parameter = strings.TrimSpace(parameter)
				if parameter == "q=0" || parameter == "q=0.0" || parameter == "q=0.00" || parameter == "q=0.000" {
					return false
				}
			}
			return true
		}
	}
	return false
}
