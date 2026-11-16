#!/bin/sh
set -eu

mkdir -p "$XDG_RUNTIME_DIR/pulse"
chmod 700 "$XDG_RUNTIME_DIR"
pulseaudio \
	--daemonize=no \
	--exit-idle-time=-1 \
	--disable-shm=yes \
	--load="module-native-protocol-unix socket=$XDG_RUNTIME_DIR/pulse/native auth-anonymous=1" \
	--load="module-null-sink sink_name=smolify_test" &
pulse_pid=$!
cleanup() {
	kill "$pulse_pid" 2>/dev/null || true
	wait "$pulse_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

attempt=0
while [ "$attempt" -lt 50 ]; do
	if pactl info >/dev/null 2>&1; then
		break
	fi
	if ! kill -0 "$pulse_pid" 2>/dev/null; then
		wait "$pulse_pid" || true
		echo "PulseAudio exited before creating its socket" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 0.1
done

if ! pactl info >/dev/null 2>&1; then
	echo "PulseAudio did not become ready" >&2
	exit 1
fi
pactl set-default-sink smolify_test
pactl list short sinks
/usr/local/bin/player-audio-smoke -test.run '^TestPulseAudioSilenceStream$' -test.v
