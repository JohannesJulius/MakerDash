# MakerDash – Projektkontext für Claude

DIY-Mischpult „MakerDash“ (bis 2.0.x „Pico Dashboard“) von Johannes: Raspberry Pi Pico 2 (CircuitPython 10) mit 3 Fadern, Drehgeber mit Taster
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
- `hardware/zero-panel/` – Panel mit RP2040-Zero (Anleitung/Verdrahtung). Firmware-Rollen in
  `code.py` (`DASH_ROLE` auto): local (USB + Display), bridge (USB ohne Display, UART GP0/GP1 zum
  Panel, beantwortet PING selbst, PONG mit 6. Feld Panel-Version), panel (ohne USB, UART statt
  usb_cdc). Die frühere AVR-Platine (hardware/panel) wurde verworfen, steht nur noch in der Git-Historie.
- `installer/setup.nsi` – NSIS-Installer (`/S /UPDATE` = stilles Update durch die App).

## Release
Version in `VERSION` erhöhen, optional `release-notes/vX.Y.Z.md`, auf `main` pushen →
`.github/workflows/release.yml` baut App + Installer + `SHA256SUMS.txt` und legt das Release an.
Die installierten Apps finden es selbst (auch über Rechtsklick aufs Tray-Symbol installierbar).

## Bauen & Testen (Linux)
```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X main.AppVersion=0.0.0-dev" -o dist/MakerDash.exe .
GOOS=windows go test -c -o dist/apptest.exe . && wine dist/apptest.exe -test.v   # Tests unter Wine
cd tools/uitest && python3 shots.py        # UI-Screenshots mit Playwright (Demo-Daten ohne Bridge)
cd tools/picosim && python3 test_v2.py     # Firmware in simulierter CircuitPython-Umgebung, rendert OLED
python3 tools/picosim/test_labels.py       # Textpositionen mit echten Adafruit-Bibliotheken (Blinka)
cd tools/picosim && python3 test_rollen.py # Brücke und Panel (RP2040-Zero) getrennt simuliert
```
Wine kann WebView2, Audio-Sitzungen und das Umschalten des Standardgeräts (IPolicyConfig) NICHT
nachbilden – das muss auf echtem Windows geprüft werden. `tools/picosim/e2e_fw.py` verbindet die
simulierte Firmware über ein pty (socat) mit der echten .exe unter Wine.

## Stand (30.09.2026)
- 2.0.0: Fenster blieb auf echtem Windows schwarz (WebView2 in verstecktes Fenster eingebettet).
- 2.0.1: Fenster wird vor dem Einbetten angezeigt, `Show()` + `Resize()` danach, Log-Diagnose,
  Ausweichweg über Datei. **Auf echtem Windows bestätigt: App läuft (30.09.2026).**
- Firmware 2.0.0 läuft bereits auf Johannes' Dashboard (automatisch von der App installiert).
- 2.1.0: Umbenennung in MakerDash (`legacy.go` übernimmt %APPDATA%-Ordner und Autostart, der
  Installer entfernt die alte Installation; Release enthält zusätzlich `PicoDashboard-Setup.exe`,
  damit 2.0.x-Apps das Update finden). Firmware 2.1.0: wanderndes Logo als Bildschirmschoner,
  schnelleres Menü (Glyphen vorladen, `bitmap_label`, I2C 400 kHz). Geräte-Grafik in der App nach
  Johannes' Frontplatten-Zeichnung (hochkant). Update von 2.0.1 auf 2.1.0 hat bei Johannes geklappt.
- 2.1.1: `bitmap_label` (adafruit_display_text 5.0.5) setzt BDF-Texte 6–9 px zu tief (nimmt die
  Schrifthöhe als Oberlänge, zentriert auf textabhängigen Kasten). Firmware nutzt Anker y = 0 plus
  Tabelle `KORREKTUR`; Schriftwechsel/Verschieben nur über `schrift_setzen`/`platzieren`.
  Prüfung mit echten Bibliotheken: `python3 tools/picosim/test_labels.py`
  (braucht `pip install adafruit-blinka-displayio`; `sim.py` bildet diesen Fehler NICHT ab).
- 2.2.0: Gruppen (`Config.Groups`, Ziel `group:<id>`), alle Fader frei belegbar, Ziel `focus`
  (Vordergrundfenster), Stummschalten per langem Druck (Pico sendet `MUTE`, PC meldet
  `MUTE\t0/1\t<name>`, Ziel `Config.MuteTarget`). Logik in `targets.go`, Tests in
  `targets_test.go`. Kurzer Druck wirkt jetzt beim Loslassen. **Nicht auf echter Hardware geprüft.**
- `tools/picosim/sim.py` nutzt die echten Adafruit-Bibliotheken, wenn Blinka installiert ist
  (`pip install adafruit-blinka-displayio`) – dann sind die Bilder pixelgenau; `SIM_FAKE=1` erzwingt
  die alte Nachbildung. Wine ist per `apt-get install wine64` installierbar.
- 2.3.0: Panel mit RP2040-Zero (Zero zeichnet Display, Pico = Brücke). **Nur simuliert, nicht auf
  Hardware geprüft.** Einrichtung des Zero per BOOTSEL nutzt das CircuitPython-Build
  `waveshare_rp2040_zero` (URL nicht geprüft – Download-Server war gesperrt).
- Repository heißt jetzt `JohannesJulius/MakerDash` (alte Adresse wird weitergeleitet).
- Noch ungetestet auf echter Hardware: Programmliste (Audio-Sitzungen), Standardgerät umschalten,
  Einrichtung eines neuen Pico im BOOTSEL-Modus.
- Log beim Nutzer: `%APPDATA%\MakerDash\log.txt`; Diagnose-Start: `MakerDash.exe --open --debug`.
