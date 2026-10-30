#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TOOLCHAIN=${1:-go}
OUT="$ROOT/dist/web"

rm -rf "$OUT"
mkdir -p "$OUT" "$ROOT/web"
case "$TOOLCHAIN" in
  go)
    GOROOT=$(go env GOROOT)
    WASM_EXEC="$GOROOT/lib/wasm/wasm_exec.js"
    if [[ ! -f "$WASM_EXEC" ]]; then
      WASM_EXEC="$GOROOT/misc/wasm/wasm_exec.js"
    fi
    if [[ ! -f "$WASM_EXEC" ]]; then
      echo "could not find wasm_exec.js under $GOROOT" >&2
      exit 1
    fi
    cp "$WASM_EXEC" "$ROOT/web/wasm_exec.js"
    cp "$ROOT/web/wasm_exec.js" "$OUT/wasm_exec.js"
    cd "$ROOT"
    GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o "$OUT/smolify.wasm" ./cmd/smolify-web
    ;;
  tinygo)
    TINYGOROOT=$(tinygo env TINYGOROOT)
    WASM_EXEC="$TINYGOROOT/targets/wasm_exec.js"
    if [[ ! -f "$WASM_EXEC" ]]; then
      echo "could not find TinyGo wasm_exec.js at $WASM_EXEC" >&2
      exit 1
    fi
    cp "$WASM_EXEC" "$ROOT/web/wasm_exec.js"
    cp "$ROOT/web/wasm_exec.js" "$OUT/wasm_exec.js"
    cd "$ROOT"
    tinygo build -target wasm -o "$OUT/smolify.wasm" ./cmd/smolify-web
    ;;
  *)
    echo "usage: $0 [go|tinygo]" >&2
    exit 2
    ;;
esac

cp "$ROOT/web/index.html" "$OUT/index.html"
command -v brotli >/dev/null || { echo "brotli is required to build the compressed site" >&2; exit 1; }
while IFS= read -r -d '' file; do
  gzip -n -9 -c "$file" > "$file.gz"
  brotli --quality=11 --force "$file" --output="$file.br"
done < <(find "$OUT" -type f ! -name '*.gz' ! -name '*.br' -print0)

for asset in smolify.wasm smolify.wasm.gz smolify.wasm.br; do
  printf '%-20s %s bytes\n' "$asset" "$(wc -c < "$OUT/$asset" | tr -d ' ')"
done
