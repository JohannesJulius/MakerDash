# Pico Dashboard – Projektkontext für Claude

DIY-Mischpult von Johannes: Raspberry Pi Pico 2 (CircuitPython 10) mit 3 Fadern, Drehgeber mit Taster
und 0,96"-OLED (SSD1306) steuert Windows-Lautstärken. Sprache mit dem Nutzer: **Deutsch**.

## Aufbau
- `*.go` – Windows-App (Go, ohne cgo, Cross-Compile von Linux): Tray, eigenes Win32-Fenster mit
  WebView2 (`github.com/jchv/go-webview2/pkg/edge`), Core-Audio über eigene COM-Aufrufe (`com.go`),
  serielle Verbindung (`comport.go`, Polling statt Overlapped-I/O), Firmware-Verwaltung (`firmware.go`),
  Updater über GitHub Releases (`updater.go`).
- `ui/index.html` – komplette Oberfläche (dunkles Design), Brücke: `window.__app.state/levels/toast/go`,
  Befehle per `chrome.webview.postMessage(JSON)`, Verarbeitung in `bridge.go`.
- `firmware/` – Pico-Firmware (wird per `go:embed` in die App eingebettet und von ihr aufs CIRCUITPY-
  Laufwerk geschrieben). `FW_VERSION` in `firmware/code.py` erhöhen, wenn sich die Firmware ändert.
  Pins/Anzeige kommen aus `settings.toml`, das die App schreibt.
- Protokoll Pico <-> PC: Kommentar oben in `core.go`.
- `installer/setup.nsi` – NSIS-Installer (`/S /UPDATE` = stilles Update durch die App).

## Release
Version in `VERSION` erhöhen, optional `release-notes/vX.Y.Z.md`, auf `main` pushen →
`.github/workflows/release.yml` baut App + Installer + `SHA256SUMS.txt` und legt das Release an.
Die installierten Apps finden es selbst (auch über Rechtsklick aufs Tray-Symbol installierbar).

## Bauen & Testen (Linux)
```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X main.AppVersion=0.0.0-dev" -o dist/PicoDashboard.exe .
GOOS=windows go test -c -o dist/apptest.exe . && wine dist/apptest.exe -test.v   # Tests unter Wine
cd tools/uitest && python3 shots.py        # UI-Screenshots mit Playwright (Demo-Daten ohne Bridge)
cd tools/picosim && python3 test_v2.py     # Firmware in simulierter CircuitPython-Umgebung, rendert OLED
```
Wine kann WebView2, Audio-Sitzungen und das Umschalten des Standardgeräts (IPolicyConfig) NICHT
nachbilden – das muss auf echtem Windows geprüft werden. `tools/picosim/e2e_fw.py` verbindet die
simulierte Firmware über ein pty (socat) mit der echten .exe unter Wine.

## Stand (30.09.2026)
- 2.0.0: Fenster blieb auf echtem Windows schwarz (WebView2 in verstecktes Fenster eingebettet).
- 2.0.1: Fenster wird vor dem Einbetten angezeigt, `Show()` + `Resize()` danach, Log-Diagnose,
  Ausweichweg über Datei. **Auf echtem Windows noch nicht bestätigt.**
- Firmware 2.0.0 läuft bereits auf Johannes' Dashboard (automatisch von der App installiert).
- Noch ungetestet auf echter Hardware: Programmliste (Audio-Sitzungen), Standardgerät umschalten,
  Einrichtung eines neuen Pico im BOOTSEL-Modus.
- Log beim Nutzer: `%APPDATA%\PicoDashboard\log.txt`; Diagnose-Start: `PicoDashboard.exe --open --debug`.
