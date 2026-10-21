# Smolify Development Guide

Smolify is a lightweight Spotify client — terminal by default, a native
window with `-gui` — built on Flatte (`github.com/lunguini/flatte`, branch
`gui`) as an outside module, with playback by an embedded go-librespot.
It is GPL-3.0 (go-librespot is). It needs Spotify Premium.

## Internal docs

Plans, status, and findings live in `.docs/` (git-crypt encrypted, opaque
names). Read `.docs/index.md` first. If `.docs` files look binary, stop and
ask for `git-crypt unlock`. Do not copy `.docs` content into public files or
commit messages.

## Build

Needs cgo and Homebrew's audio codecs:

```bash
brew install libogg libvorbis flac mpg123
go test ./...
go run ./cmd/smolify          # terminal
go run ./cmd/smolify -gui     # window (macOS)
```

The player stores its device ID and credentials in the user config
directory (`smolify/state.json`, mode 0600). Never commit credentials.

## Shape

- `cmd/` — process setup only.
- `internal/player` — the embedded player; no UI types.
- `internal/app` — Flatte state, handlers, and pure views, with tests and
  goldens.
- Flatte conventions apply: one state struct, pure View, no messages, async
  results as named updates, layout trees for frames.

Commits follow Conventional Commits; no co-author trailers.
