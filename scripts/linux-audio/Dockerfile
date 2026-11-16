FROM golang:1.26.1-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        libasound2-dev \
        libflac-dev \
        libmpg123-dev \
        libogg-dev \
        libvorbis-dev \
        pkg-config \
        pulseaudio \
        pulseaudio-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY assets ./assets
COPY LICENSE README.md ./

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build ./... \
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/smolify-linux ./cmd/smolify \
    && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go test -tags audiointegration -c -o /usr/local/bin/player-audio-smoke ./internal/player

RUN useradd --create-home --uid 10001 smoke \
    && install -d -o smoke -g smoke /tmp/smolify-runtime
COPY scripts/linux-audio/entrypoint.sh /usr/local/bin/smolify-audio-smoke
RUN chmod 0755 /usr/local/bin/smolify-audio-smoke

ENV HOME=/home/smoke \
    XDG_RUNTIME_DIR=/tmp/smolify-runtime \
    PULSE_SERVER=unix:/tmp/smolify-runtime/pulse/native \
    SMOLIFY_AUDIO_INTEGRATION=1
USER smoke
ENTRYPOINT ["/usr/local/bin/smolify-audio-smoke"]
