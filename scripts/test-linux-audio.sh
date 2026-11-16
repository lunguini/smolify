#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
image_name=${SMOLIFY_AUDIO_SMOKE_IMAGE:-smolify-linux-audio-smoke}

docker build -f "$repo_root/scripts/linux-audio/Dockerfile" -t "$image_name" "$repo_root"
docker run --rm "$image_name"
