# Smolify web build notes

## What is implemented

- `internal/app` uses shared model/catalog packages. The native player and file
  stores stay in native build files; browser settings and library snapshots use
  `localStorage` through host interfaces.
- Demo mode is the default. It has fictional sample playlists and tracks,
  locally generated abstract covers, simulated position and transport, shuffle,
  repeat, volume, likes, queue, and sample-data search.
- `?spotify` starts Authorization Code with PKCE when a client ID is configured.
  Access and refresh tokens plus the PKCE verifier/state are kept in
  `sessionStorage`; settings and library cache use `localStorage`.
- The Spotify adapter uses the documented Web API and the official Web
  Playback SDK. It handles token refresh and `Retry-After`, browsing,
  saved-track reads/writes, search, recent history, player state, transport,
  queue, and playback transfer. SDK state/errors are mapped to app events.
- `scripts/build-web.sh [go|tinygo]` copies the matching `wasm_exec.js` and
  creates `dist/web/` with raw, gzip, and Brotli assets. `go run ./cmd/serve-web`
  serves that directory locally.

No client ID, client secret, token, or personal account data is committed.
The two client ID/redirect URI meta fields in `web/index.html` are blank.
They can also be supplied through `window.SmolifyConfig.clientId` and
`window.SmolifyConfig.redirectUri` before the wasm startup script runs.

## Verification

Commands run in this worktree:

- `go test ./...` — passed.
- `go test -race ./...` — passed.
- `go vet ./...` — passed.
- `GOOS=js GOARCH=wasm go vet ./...` — passed.
- `GOOS=js GOARCH=wasm go build ./cmd/smolify-web` — passed, including with
  `GOWORK=off`.
- `tinygo build -target wasm ./cmd/smolify-web` — passed with TinyGo 0.41.1.
- `scripts/build-web.sh go` and `scripts/build-web.sh tinygo` — passed.
- `go run ./cmd/serve-web`; `curl -I` for the HTML and wasm with identity,
  gzip, and Brotli `Accept-Encoding` — all returned 200 with the expected
  `Content-Type` and `Content-Encoding` headers.

The browser UI was not visually or interactively exercised. No browser provider
was available through computer use, and the organization policy blocked opening
Brave. The wasm builds, unit tests, and local static-server headers were
verified; sign-in and playback with a real account still need a browser pass.

The wasm dependency graph has no `go-librespot`, `internal/player`,
`internal/spotify`, or `net/http` packages. It does include terminal packages
such as `os`, `os/user`, `os/signal`, and `os/exec` through Flatte's shared
root/rendering packages. Smolify's web code does not call the terminal file
selector or filesystem APIs, and both wasm toolchains build. If the boundary
must exclude every transitive terminal/filesystem symbol, Flatte will need a
build-tag split in its own repository.

## wasm sizes

Sizes from the build script on Go 1.26.1 and TinyGo 0.41.1:

| Toolchain | Raw | gzip | Brotli |
|---|---:|---:|---:|
| Go 1.26.1 | 8,306,629 bytes | 2,282,843 bytes | 1,710,018 bytes |
| TinyGo 0.41.1 | 6,182,042 bytes | 2,063,094 bytes | 1,536,910 bytes |

## Spotify setup and limits

For account mode, create/configure a Spotify developer app, put its client ID
in the page configuration, and register the exact redirect URI used by the
site. Add the account to the app's authorized users. The browser player
requires a Premium account and a browser that supports encrypted media.

Spotify's current Development Mode guide says the app owner needs Premium,
each developer may have one client ID, and an app may have five users. The
current migration guide also limits playlist-item reads to playlists the user
owns or collaborates on, caps search pages at ten per type, and removes the
artist top-tracks endpoint. Search paging follows the ten-item limit; artist
pages show metadata only. Song Radio is disabled in account mode because its
documented recommendations endpoint is marked deprecated. Browser audio
quality settings are not exposed by the Web Playback SDK.

References: [PKCE authorization](https://developer.spotify.com/documentation/web-api/tutorials/code-pkce-flow),
[refreshing tokens](https://developer.spotify.com/documentation/web-api/tutorials/refreshing-tokens),
[February 2026 migration guide](https://developer.spotify.com/documentation/web-api/tutorials/february-2026-migration-guide),
[Web Playback SDK](https://developer.spotify.com/documentation/web-playback-sdk/reference).
