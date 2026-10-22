# Smolify

A small, fast Spotify client for the terminal and the desktop, built with
[Flatte](https://github.com/lunguini/flatte) and
[go-librespot](https://github.com/devgianlu/go-librespot).

Work in progress. Requires Spotify Premium. Not affiliated with Spotify.

## Build

```bash
brew install libogg libvorbis flac mpg123   # audio codecs (cgo)
go run ./cmd/smolify                        # in the terminal
go run ./cmd/smolify -gui                   # in a window (macOS)
go run ./cmd/smolify -sign-out              # forget the Spotify sign-in
go run ./cmd/smolify -debug-log smolify.log # log player events and memory
```

## License

GPL-3.0 — see [LICENSE](LICENSE).
