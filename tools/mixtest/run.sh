#!/bin/bash
# Prüft den Mikro-Mix unter Wine mit PulseAudio als Audiotreiber:
#   Sinuston -> "Mikrofon (Test USB)" -> MakerDash-Mixer -> "CABLE Input" -> Aufnahme -> Auswertung
# Braucht: wine, pulseaudio, sox, python3.   Aufruf: tools/mixtest/run.sh [Sekunden]
set -e
cd "$(dirname "$0")/../.."
SECS=${1:-12}
export WINEDEBUG=-all
export XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/tmp/makerdash-xdg}
mkdir -p "$XDG_RUNTIME_DIR" dist && chmod 700 "$XDG_RUNTIME_DIR"
OUT=$(mktemp -d)

. tools/mixtest/pulse.sh

GOOS=windows GOARCH=amd64 go test -c -o dist/apptest.exe .
sox -n -r 44100 -c 2 -b 16 "$OUT/sine.wav" synth $((SECS + 6)) sine 440 gain -6
paplay --device=micfeed "$OUT/sine.wav" &
PLAY=$!
parecord --device=cable.monitor --rate=48000 --channels=2 --format=s16le --file-format=wav "$OUT/rec.wav" &
REC=$!
sleep 1
wineserver -k 2>/dev/null || true
MAKERDASH_MIXTEST=$SECS wine dist/apptest.exe -test.run 'TestMixerPassthrough' -test.v 2>&1 | grep -v -E '^ALSA|rundll32'
kill $REC $PLAY 2>/dev/null || true
wait 2>/dev/null || true
python3 tools/mixtest/analyse.py "$OUT/rec.wav"
