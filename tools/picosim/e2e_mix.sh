#!/bin/bash
# Ende-zu-Ende-Test des Mikro-Mix: simulierte Firmware <-> MakerDash.exe unter Wine.
# Braucht: wine, socat, xvfb-run, pulseaudio, python3 (+ adafruit-blinka-displayio oder SIM_FAKE=1).
set -e
cd "$(dirname "$0")/../.."
ROOT=$PWD
export WINEDEBUG=-all XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR:-/tmp/makerdash-xdg}
mkdir -p "$XDG_RUNTIME_DIR" dist && chmod 700 "$XDG_RUNTIME_DIR"
WORK=$(mktemp -d)
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X main.AppVersion=0.0.0-dev" -o dist/MakerDash.exe .

. tools/mixtest/pulse.sh

wineserver -k 2>/dev/null || true
APPDATA_DIR="$HOME/.wine/drive_c/users/$USER/AppData/Roaming/MakerDash"
mkdir -p "$APPDATA_DIR"; rm -f "$APPDATA_DIR/log.txt" "$APPDATA_DIR"/guard-*
cat > "$APPDATA_DIR/config.json" <<JSON
{"version":2,"fader1":"master","fader2":"mic","fader3App":"","port":"COM7","setupDone":true,"noAutoUpdateCheck":true,"noAutoFirmware":true,
 "groups":[{"id":"g1","name":"Musik","apps":["spotify.exe"]}],"apps":{},"devices":{},"mix":{"appVol":70,"micVol":100}}
JSON
socat -d -d pty,raw,echo=0,link="$WORK/pc" pty,raw,echo=0,link="$WORK/pico" 2>"$WORK/socat.log" &
SOCAT=$!
sleep 1
ln -sf "$WORK/pc" "$HOME/.wine/dosdevices/com7"
xvfb-run -a wine dist/MakerDash.exe >/dev/null 2>&1 &
cd tools/picosim
RC=0
timeout 120 python3 e2e_mix.py "$WORK/pico" "$ROOT/firmware/code.py" "$ROOT/firmware" "$WORK/e2e.log" || RC=$?
wineserver -k 2>/dev/null || true
kill $SOCAT 2>/dev/null || true
echo "--- Protokoll"; cat "$WORK/e2e.log"
echo "--- log.txt"; grep -v "^$" "$APPDATA_DIR/log.txt" | tail -15
exit $RC
