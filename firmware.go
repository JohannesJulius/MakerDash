//go:build windows

package main

// Verwaltung der Pico-Firmware: Version prüfen, automatisch aktualisieren,
// neuen Pico einrichten (CircuitPython aufspielen).

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed firmware
var firmwareFS embed.FS

const (
	circuitPythonVersion = "10.3.1"
	cpDownloadPage       = "https://circuitpython.org/board/raspberry_pi_pico2/"
)

func circuitPythonURL(board string) string {
	id := "raspberry_pi_pico2"
	if board == "RP2040" {
		id = "raspberry_pi_pico"
	}
	return fmt.Sprintf("https://downloads.circuitpython.org/bin/%s/en_US/adafruit-circuitpython-%s-en_US-%s.uf2",
		id, id, circuitPythonVersion)
}

var bundledFirmware = func() string {
	data, _ := firmwareFS.ReadFile("firmware/code.py")
	m := regexp.MustCompile(`FW_VERSION = "([^"]+)"`).FindSubmatch(data)
	if m == nil {
		return "?"
	}
	return string(m[1])
}()

type fwFile struct {
	rel  string
	data []byte
}

// firmwareFiles liefert alle Dateien in sinnvoller Reihenfolge (code.py zuletzt).
func firmwareFiles() []fwFile {
	var files []fwFile
	fs.WalkDir(firmwareFS, "firmware", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, _ := firmwareFS.ReadFile(p)
		files = append(files, fwFile{strings.TrimPrefix(p, "firmware/"), data})
		return nil
	})
	var h Hardware
	var disp Display
	withConfig(func(c *Config) { h, disp = c.Hardware, c.Display }, false)
	files = append(files, fwFile{"settings.toml", []byte(settingsToml(h, disp))})
	rank := func(rel string) int {
		switch {
		case strings.HasPrefix(rel, "lib/"):
			return 0
		case strings.HasPrefix(rel, "fonts/"):
			return 1
		case rel == "settings.toml":
			return 2
		case rel == "boot.py":
			return 3
		case rel == "code.py":
			return 5
		}
		return 4
	}
	sort.SliceStable(files, func(i, j int) bool {
		ri, rj := rank(files[i].rel), rank(files[j].rel)
		if ri != rj {
			return ri < rj
		}
		return files[i].rel < files[j].rel
	})
	return files
}

func writeIfChanged(root, rel string, data []byte) (bool, error) {
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	f.Sync()
	return true, f.Close()
}

// installFiles schreibt die Firmware auf das Laufwerk (nur geänderte Dateien).
func installFiles(root string, progress func(int)) (int, error) {
	files := firmwareFiles()
	changed := 0
	for i, f := range files {
		c, err := writeIfChanged(root, f.rel, f.data)
		if err != nil {
			return changed, fmt.Errorf("%s: %w", f.rel, err)
		}
		if c {
			changed++
		}
		if progress != nil {
			progress((i + 1) * 100 / len(files))
		}
	}
	return changed, nil
}

// ---------------- Manager ----------------

type FirmwareState struct {
	State    string `json:"state"` // unknown, ok, outdated, updating, restarting, error
	Progress int    `json:"progress"`
	Message  string `json:"message"`
	Bundled  string `json:"bundled"`
}

type SetupState struct {
	State    string `json:"state"` // idle, download, flash, waitDrive, install, restart, done, error
	Progress int    `json:"progress"`
	Message  string `json:"message"`
}

type FirmwareManager struct {
	mu        sync.Mutex
	st        FirmwareState
	setup     SetupState
	circuitpy []driveInfo
	bootsel   []driveInfo
	failed    bool // automatisches Update ist in dieser Sitzung fehlgeschlagen
	busy      bool
	waitVer   string
	waitSince time.Time
}

var firmware = &FirmwareManager{
	st:    FirmwareState{State: "unknown", Bundled: bundledFirmware},
	setup: SetupState{State: "idle"},
}

func (m *FirmwareManager) Snapshot() (FirmwareState, SetupState, []driveInfo, []driveInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.st, m.setup, append([]driveInfo(nil), m.circuitpy...), append([]driveInfo(nil), m.bootsel...)
}

func (m *FirmwareManager) set(fn func()) {
	m.mu.Lock()
	fn()
	m.mu.Unlock()
	markDirty()
}

// watchDrives prüft regelmäßig, welche Pico-Laufwerke angeschlossen sind.
func (m *FirmwareManager) watchDrives() {
	key := ""
	for {
		cp, bs := scanDrives()
		k := fmt.Sprint(cp, bs)
		if k != key {
			key = k
			m.set(func() { m.circuitpy, m.bootsel = cp, bs })
		}
		time.Sleep(2 * time.Second)
	}
}

func (m *FirmwareManager) findCircuitPy(wait time.Duration) string {
	deadline := time.Now().Add(wait)
	for {
		cp, _ := scanDrives()
		if len(cp) > 0 {
			return cp[0].Root
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(700 * time.Millisecond)
	}
}

func autoFirmwareEnabled() bool {
	on := true
	withConfig(func(c *Config) { on = !c.NoAutoFirmware }, false)
	return on
}

func (m *FirmwareManager) OnConnect(r Remote) {
	m.mu.Lock()
	outdated := r.Proto < 2 || r.Version != bundledFirmware
	waiting := m.waitVer != ""
	m.mu.Unlock()

	switch {
	case outdated:
		m.set(func() {
			if m.st.State != "updating" {
				m.st = FirmwareState{State: "outdated", Bundled: bundledFirmware,
					Message: fmt.Sprintf("Auf dem Dashboard läuft Version %s, verfügbar ist %s.", r.Version, bundledFirmware)}
			}
		})
		m.mu.Lock()
		auto := !m.failed && !m.busy && autoFirmwareEnabled()
		m.mu.Unlock()
		if auto {
			go m.Update()
		}
	case !r.BootOK:
		m.set(func() {
			m.st = FirmwareState{State: "error", Bundled: bundledFirmware,
				Message: "Die Firmware ist installiert, aber noch nicht vollständig aktiv. Bitte das Dashboard einmal aus- und wieder einstecken."}
		})
	default:
		m.set(func() {
			m.st = FirmwareState{State: "ok", Bundled: bundledFirmware}
			m.waitVer = ""
			if m.setup.State == "restart" {
				m.setup = SetupState{State: "done"}
			}
		})
		if waiting {
			toast("ok", "Dashboard aktualisiert auf Version "+bundledFirmware)
			if !uiVisible() {
				notifyUser("Dashboard aktualisiert", "Die Firmware wurde auf Version "+bundledFirmware+" aktualisiert.", "")
			}
		}
		go m.syncSettings()
	}
}

func (m *FirmwareManager) OnDisconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st.State == "ok" || m.st.State == "outdated" {
		m.st.State = "unknown"
	}
}

func (m *FirmwareManager) fail(msg string) {
	log.Println("Firmware:", msg)
	m.set(func() {
		m.st = FirmwareState{State: "error", Message: msg, Bundled: bundledFirmware}
		m.failed = true
		m.busy = false
		m.waitVer = ""
	})
	toast("error", msg)
}

// Update installiert die mitgelieferte Firmware auf dem angeschlossenen Dashboard.
func (m *FirmwareManager) Update() {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return
	}
	m.busy = true
	m.mu.Unlock()
	m.set(func() { m.st = FirmwareState{State: "updating", Bundled: bundledFirmware, Message: "Wird vorbereitet …"} })

	connected, _, rem := link.Connected()
	v2 := connected && rem.Proto >= 2
	if v2 {
		link.Send("UPDATING")
		time.Sleep(300 * time.Millisecond)
	}
	root := m.findCircuitPy(6 * time.Second)
	if root == "" {
		if v2 {
			link.Send("UPDATE_ABORT")
		}
		m.fail("Das Laufwerk CIRCUITPY wurde nicht gefunden. Ist das Dashboard per USB verbunden?")
		return
	}
	last := -1
	_, err := installFiles(root, func(p int) {
		if p/5 == last/5 {
			return
		}
		last = p
		m.set(func() { m.st.Progress = p; m.st.Message = "Dateien werden übertragen …" })
		if v2 {
			link.Send(fmt.Sprintf("PROGRESS\t%d", p))
		}
	})
	if err != nil {
		if v2 {
			link.Send("UPDATE_ABORT")
		}
		m.fail("Update fehlgeschlagen: " + err.Error())
		return
	}
	m.set(func() {
		m.st = FirmwareState{State: "restarting", Progress: 100, Bundled: bundledFirmware, Message: "Dashboard startet neu …"}
		m.waitVer = bundledFirmware
		m.waitSince = time.Now()
		m.busy = false
	})
	if v2 {
		link.Send("REBOOT")
	}
	// Alte Firmware (v1) startet durch das automatische Neuladen selbst neu.
	go func() {
		time.Sleep(60 * time.Second)
		m.mu.Lock()
		stuck := m.st.State == "restarting"
		m.mu.Unlock()
		if stuck {
			m.fail("Das Dashboard hat sich nach dem Update nicht zurückgemeldet. Bitte einmal aus- und wieder einstecken.")
		}
	}()
}

// syncSettings schreibt settings.toml, falls sie nicht zur App passt.
func (m *FirmwareManager) syncSettings() {
	root := m.findCircuitPy(3 * time.Second)
	if root == "" {
		return
	}
	var h Hardware
	var d Display
	withConfig(func(c *Config) { h, d = c.Hardware, c.Display }, false)
	if changed, err := writeIfChanged(root, "settings.toml", []byte(settingsToml(h, d))); err != nil {
		log.Println("settings.toml:", err)
	} else if changed {
		log.Println("settings.toml aktualisiert")
	}
}

// ApplyHardware schreibt die Pin-Belegung aufs Dashboard (das startet dabei neu).
func (m *FirmwareManager) ApplyHardware() error {
	root := m.findCircuitPy(3 * time.Second)
	if root == "" {
		return errors.New("Das Laufwerk CIRCUITPY wurde nicht gefunden")
	}
	var h Hardware
	var d Display
	withConfig(func(c *Config) { h, d = c.Hardware, c.Display }, false)
	_, err := writeIfChanged(root, "settings.toml", []byte(settingsToml(h, d)))
	return err
}

// ---------------- Neuen Pico einrichten ----------------

func (m *FirmwareManager) setupSet(state string, progress int, msg string) {
	m.set(func() { m.setup = SetupState{State: state, Progress: progress, Message: msg} })
}

func (m *FirmwareManager) setupFail(msg string) {
	log.Println("Einrichtung:", msg)
	m.set(func() { m.setup = SetupState{State: "error", Message: msg}; m.busy = false })
}

// SetupCircuitPython spielt CircuitPython auf einen Pico im BOOTSEL-Modus
// und installiert danach die Dashboard-Firmware.
func (m *FirmwareManager) SetupCircuitPython() {
	m.mu.Lock()
	if m.busy || len(m.bootsel) == 0 {
		m.mu.Unlock()
		return
	}
	m.busy = true
	target := m.bootsel[0]
	m.mu.Unlock()

	cacheDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "PicoDashboard", "cache")
	os.MkdirAll(cacheDir, 0o755)
	url := circuitPythonURL(target.Board)
	file := filepath.Join(cacheDir, path.Base(url))
	if st, err := os.Stat(file); err != nil || st.Size() < 100_000 {
		m.setupSet("download", 0, "CircuitPython "+circuitPythonVersion+" wird heruntergeladen …")
		err := download(url, file, func(p int) { m.set(func() { m.setup.Progress = p }) })
		if err != nil {
			m.setupFail("Download fehlgeschlagen: " + err.Error())
			return
		}
	}
	m.setupSet("flash", 0, "CircuitPython wird aufgespielt …")
	data, err := os.ReadFile(file)
	if err != nil {
		m.setupFail(err.Error())
		return
	}
	// Der Pico startet nach dem Schreiben sofort neu; ein Fehler ganz am Ende ist normal.
	if err := os.WriteFile(filepath.Join(target.Root, path.Base(url)), data, 0o644); err != nil {
		log.Println("UF2 schreiben (evtl. normal):", err)
	}
	m.setupSet("waitDrive", 0, "Warte auf den Neustart des Pico …")
	root := m.findCircuitPy(90 * time.Second)
	if root == "" {
		m.setupFail("Der Pico hat sich nach dem Aufspielen nicht als CIRCUITPY gemeldet. Bitte neu einstecken und erneut versuchen.")
		return
	}
	time.Sleep(2 * time.Second) // Dateisystem wird beim ersten Start angelegt
	m.installToDrive(root)
}

// SetupFirmware installiert die Firmware auf ein CIRCUITPY-Laufwerk ohne Dashboard-Software.
func (m *FirmwareManager) SetupFirmware() {
	m.mu.Lock()
	if m.busy || len(m.circuitpy) == 0 {
		m.mu.Unlock()
		return
	}
	m.busy = true
	root := m.circuitpy[0].Root
	m.mu.Unlock()
	m.installToDrive(root)
}

func (m *FirmwareManager) installToDrive(root string) {
	m.setupSet("install", 0, "Dashboard-Software wird installiert …")
	_, err := installFiles(root, func(p int) { m.set(func() { m.setup.Progress = p }) })
	if err != nil {
		m.setupFail("Installation fehlgeschlagen: " + err.Error())
		return
	}
	m.set(func() {
		m.setup = SetupState{State: "restart", Progress: 100, Message: "Dashboard startet …"}
		m.waitVer = bundledFirmware
		m.busy = false
	})
	go func() {
		time.Sleep(60 * time.Second)
		m.mu.Lock()
		stuck := m.setup.State == "restart"
		m.mu.Unlock()
		if stuck {
			m.setupFail("Das Dashboard meldet sich nicht. Bitte das USB-Kabel einmal ab- und wieder anstecken.")
		}
	}()
}
