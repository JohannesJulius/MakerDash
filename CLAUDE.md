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
- `installer/setup.nsi` – NSIS-Installer (`/S /UPDATE` = stilles Update durch die App).
- Mikro-Mix: `wasapi.go` (Aufnahme/Wiedergabe, Process Loopback über `ActivateAudioInterfaceAsync`
  mit eigenem COM-Objekt), `mixer.go` (Thread, 5-ms-Takt), `jitter.go` (Puffer mit Taktausgleich,
  ohne Build-Tag – läuft auch mit `go test` unter Linux), `mix.go` (Kabel-/Mikrofonwahl, Menüs,
  Absturz-Schutz `guard-*`). Ziel ist VB-CABLE („CABLE Input“); Erkennung über den Gerätenamen.
- Bluetooth: `bluetooth.go` – Liste über `bluetoothapis.dll`, Verbinden/Trennen über
  `IKsControl` + `KSPROPSETID_BtAudio` (wie ToothTray), Akku über `cfgmgr32` (DEVPKEY am BTHENUM-Knoten).
- Symbole: `python3 tools/mkicon.py`, danach `rsrc -arch amd64 -manifest app.manifest -ico icon.ico
  -o rsrc_windows_amd64.syso` (github.com/akavel/rsrc).

## Release
Version in `VERSION` erhöhen, optional `release-notes/vX.Y.Z.md`, auf `main` pushen →
`.github/workflows/release.yml` baut App + Installer + `SHA256SUMS.txt` und legt das Release an.
Die installierten Apps finden es selbst (auch über Rechtsklick aufs Tray-Symbol installierbar).

## Bauen & Testen (Linux)
```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -X main.AppVersion=0.0.0-dev" -o dist/MakerDash.exe .
GOOS=windows go test -c -o dist/apptest.exe . && wine dist/apptest.exe -test.v   # Tests unter Wine
cd tools/uitest && python3 shots.py        # UI-Screenshots mit Playwright (Demo-Daten ohne Bridge)
cd tools/uitest && python3 drag.py         # Fader am Gerätebild ziehen, misst Abstand Maus/Kappe
cd tools/picosim && python3 test_v2.py     # Firmware in simulierter CircuitPython-Umgebung, rendert OLED
python3 tools/picosim/test_labels.py       # Textpositionen mit echten Adafruit-Bibliotheken (Blinka)
cd tools/picosim && python3 test_v23.py    # Menüs „Mikro-Mix“ und „Bluetooth“ (Firmware 2.3)
cd tools/picosim && python3 test_v24.py    # Mikro-Boost-Anzeige (SCALE, Firmware 2.4)
tools/mixtest/run.sh 20                    # Mixer unter Wine + PulseAudio: Sinus -> Test-Mikrofon -> Kabel, wertet Aussetzer aus
tools/picosim/e2e_mix.sh                   # simulierte Firmware bedient das Mix-Menü der echten .exe unter Wine
```
In diesem Container braucht Go `GOPROXY=direct GOSUMDB=off`; vor `apt-get install wine nsis socat xvfb
pulseaudio sox` erst `apt-get update`. Unter Wine heißt das Programm `wine` (kein `wine64`).
Wine kann WebView2, Audio-Sitzungen und das Umschalten des Standardgeräts (IPolicyConfig) NICHT
nachbilden – das muss auf echtem Windows geprüft werden. `tools/picosim/e2e_fw.py` verbindet die
simulierte Firmware über ein pty (socat) mit der echten .exe unter Wine.

## Stand (17.11.2026)
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
- 2.3.0 (16.11.2026): Mikro-Mix (Programm-Ton + Mikrofon auf VB-CABLE; `Config.Mix`, Fader-Ziel
  `mix`, Pico-Menü „Mikro-Mix“: PC sendet `MIXS`, Pico `SETMIX`), Bluetooth-Audiogeräte verbinden/
  trennen + Akku (PC sendet `BT`, Pico `BTCON`), Oberfläche neu gestaltet (Graphit, Bahnschrift,
  Bernstein nur für Pegel; neue Seiten „Mikro-Mix“ und „Bluetooth“), neues Symbol, `--selftest`.
  `pushToPico` schickt jetzt jede Zeile nur noch bei Änderung. Unter Wine geprüft: Mixer
  (Mikrofon -> Kabel, ohne Aussetzer), Menüs, Protokoll. **Auf echtem Windows NICHT geprüft:
  Programm-Aufnahme (Process Loopback), alles zu Bluetooth, VB-CABLE-Erkennung.** Wine kann beides
  nicht nachbilden. Bei Problemen zuerst `MakerDash.exe --selftest` -> `%APPDATA%\MakerDash\selftest.txt`.
- 2.4.0 (17.11.2026): Mikro-Boost – Fader-Ziel `boost` regelt `Config.Mix.MicVol` von 0 bis
  `Mix.BoostMax` (100–400 %, Standard 200; `gainOf`: bis 100 % quadratisch, darüber linear).
  Weicher Begrenzer in `clip16` (ab 75 % Vollaussteuerung). PC sendet `SCALE\t<max1>\t<max2>\t<max3>`,
  Firmware 2.4.0 zeigt damit die Prozent und eine 100-%-Markierung. Zuweisen von `boost` schaltet
  den Mikro-Mix ein. Mixer-Test mit Boost: `BOOST=300 tools/mixtest/run.sh 15`.
- 2.5.0 (17.11.2026): Software-Modus (`softmode.go`): ohne Dashboard füllt `softPump` alle 400 ms
  `faderValues` aus den echten Windows-Lautstärken (`Audio.Levels()`, gelesen in `refresh`), die
  Oberfläche zeigt Regler statt Pegel (Befehl `setFader {n, v}`, auch Ziehen am Gerätebild).
  `foregroundApp()` überspringt MakerDash selbst. Hinweis beim Abziehen nur, wenn kein
  Firmware-Update läuft (`FirmwareManager.Busy`). Unter Wine nur ohne WebView2 prüfbar.
- 2.5.1 (17.11.2026): Ziehen am Gerätebild im Software-Modus repariert – die App meldet gesendete
  Werte verzögert zurück (`levels`) und das Gerätebild wurde bei jedem `state` neu gezeichnet. Jetzt:
  `mergeLevels` behält eigene Werte 1,2 s, kein Neuzeichnen von `#devArt` während `drag`,
  Greif-Abstand zur Kappenmitte. Test: `python3 tools/uitest/drag.py` (bildet das Echo nach).
- `tools/picosim/sim.py` nutzt die echten Adafruit-Bibliotheken, wenn Blinka installiert ist
  (`pip install adafruit-blinka-displayio`) – dann sind die Bilder pixelgenau; `SIM_FAKE=1` erzwingt
  die alte Nachbildung. Wine ist per `apt-get install wine64` installierbar.
- Repository heißt jetzt `JohannesJulius/MakerDash` (alte Adresse wird weitergeleitet).
- Noch ungetestet auf echter Hardware: Programmliste (Audio-Sitzungen), Standardgerät umschalten,
  Einrichtung eines neuen Pico im BOOTSEL-Modus.
- Log beim Nutzer: `%APPDATA%\MakerDash\log.txt`; Diagnose-Start: `MakerDash.exe --open --debug`.
