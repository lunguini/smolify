//go:build !js || !wasm

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStaticSiteServesIndexAndMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := staticSite(root)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/", http.StatusOK},
		{"/index.html", http.StatusOK},
		{"/missing.html", http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestStaticSiteRejectsWindowsTraversal(t *testing.T) {
	// path.Clean only understands "/", so on Windows a backslash survives
	// and filepath.Join would treat it as a separator and escape root.
	name := `..\..\x`
	if runtime.GOOS != "windows" {
		t.Skip("backslash separates path elements only on Windows")
	}
	if filepath.IsLocal(name) {
		t.Fatalf("filepath.IsLocal(%q) = true, want false", name)
	}
	root := t.TempDir()
	rec := httptest.NewRecorder()
	staticSite(root).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusNotFound)
	}
}
