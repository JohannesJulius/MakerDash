//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParsePong(t *testing.T) {
	r, ok := parsePong("PONG\tDASH\t1")
	if !ok || r.Proto != 1 || r.Version != "1.0.0" || !r.BootOK {
		t.Fatalf("v1: %+v %v", r, ok)
	}
	r, ok = parsePong("PONG\tDASH\t2\t2.0.0\t0")
	if !ok || r.Proto != 2 || r.Version != "2.0.0" || r.BootOK {
		t.Fatalf("v2: %+v", r)
	}
	if _, ok := parsePong("hallo"); ok {
		t.Fatal("falsch erkannt")
	}
}

func TestVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"2.1.0", "2.0.0", true}, {"v2.0.1", "2.0.0", true}, {"2.0.0", "2.0.0", false}, {"1.9.9", "2.0.0", false}, {"2.0.0", "0.0.0-dev", true}, {"10.0.0", "9.9.9", true}}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("%s > %s = %v", c.a, c.b, got)
		}
	}
}

func TestFirmwareFiles(t *testing.T) {
	if bundledFirmware != "2.0.0" {
		t.Fatalf("bundled = %q", bundledFirmware)
	}
	files := firmwareFiles()
	if files[len(files)-1].rel != "code.py" {
		t.Fatal("code.py muss zuletzt kommen")
	}
	var names []string
	for _, f := range files {
		names = append(names, f.rel)
	}
	all := strings.Join(names, ",")
	for _, want := range []string{"boot.py", "settings.toml", "fonts/menu.bdf", "fonts/big.bdf", "lib/adafruit_displayio_ssd1306.py", "lib/adafruit_bitmap_font/bdf.py"} {
		if !strings.Contains(all, want) {
			t.Errorf("%s fehlt", want)
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "code.py"), []byte("alt"), 0o644)
	n, err := installFiles(dir, nil)
	if err != nil || n != len(files) {
		t.Fatalf("installFiles: %d %v", n, err)
	}
	n, _ = installFiles(dir, nil)
	if n != 0 {
		t.Fatalf("zweiter Durchlauf sollte nichts schreiben, schrieb %d", n)
	}
	toml, _ := os.ReadFile(filepath.Join(dir, "settings.toml"))
	if !strings.Contains(string(toml), `DASH_FADER1 = "GP28"`) || !strings.Contains(string(toml), `DASH_FADER3 = "GP26"`) {
		t.Fatalf("settings.toml:\n%s", toml)
	}
}

func TestHardwareValidation(t *testing.T) {
	h := defaultHardware()
	if err := validateHardware(h); err != nil {
		t.Fatal(err)
	}
	h.EncA = "GP4"
	if err := validateHardware(h); err == nil || !strings.Contains(err.Error(), "GP4") {
		t.Fatalf("Doppelbelegung nicht erkannt: %v", err)
	}
	h2 := sanitizeHardware(Hardware{Fader1: "GP3", Invert: "1x"})
	if h2.Fader1 != "GP28" || h2.Invert != "100" || h2.EncDivisor != 4 {
		t.Fatalf("sanitize: %+v", h2)
	}
}

func TestSanitize(t *testing.T) {
	if s := sanitize("Café Tür™\tX"); s != "Cafe TürX" {
		t.Fatalf("%q", s)
	}
}

// Updater gegen einen lokalen Fake-GitHub testen: Prüfsumme + Start des Installers
func TestUpdater(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "gestartet.txt")
	setup := []byte("FAKE-SETUP")
	sum := sha256.Sum256(setup)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	base := "http://" + ln.Addr().String()
	badSum := false
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.1.0","body":"- Neu","html_url":"x","assets":[{"name":"MakerDash-Setup.exe","browser_download_url":"%s/setup"},{"name":"SHA256SUMS.txt","browser_download_url":"%s/sums"}]}`, base, base)
	})
	mux.HandleFunc("/setup", func(w http.ResponseWriter, r *http.Request) { w.Write(setup) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		h := hex.EncodeToString(sum[:])
		if badSum {
			h = strings.Repeat("0", 64)
		}
		fmt.Fprintf(w, "%s  MakerDash-Setup.exe\n", h)
	})
	go http.Serve(ln, mux)
	os.Setenv("MAKERDASH_UPDATE_API", base)
	UpdateRepo = "test/repo"
	link = NewLinkDeferred()
	_ = marker

	updater.Check(false)
	s := updater.Snapshot()
	if s.State != "available" || s.Latest != "9.1.0" {
		t.Fatalf("Check: %+v", s)
	}
	badSum = true
	updater.Install()
	s = updater.Snapshot()
	if s.State != "error" || !strings.Contains(s.Error, "Prüfsumme") {
		t.Fatalf("falsche Prüfsumme nicht erkannt: %+v", s)
	}
	badSum = false
	updater.Install()
	time.Sleep(500 * time.Millisecond)
	s = updater.Snapshot()
	// Die Fake-Datei ist kein echtes Programm – ShellExecute scheitert, aber die Prüfung muss bestanden sein
	if s.State != "installing" && !strings.Contains(s.Error, "Installer konnte nicht gestartet werden") {
		t.Fatalf("Install: %+v", s)
	}
	t.Logf("Updater-Endzustand: %s %s", s.State, s.Error)
}

// Echter Ablauf mit dem richtigen Installer (nur wenn MAKERDASH_REAL_SETUP gesetzt ist)
func TestRealUpdate(t *testing.T) {
	path := os.Getenv("MAKERDASH_REAL_SETUP")
	if path == "" {
		t.Skip()
	}
	setup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(setup)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	base := "http://" + ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/real/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v2.0.0","body":"echt","assets":[{"name":"MakerDash-Setup.exe","browser_download_url":"%s/setup"},{"name":"SHA256SUMS.txt","browser_download_url":"%s/sums"}]}`, base, base)
	})
	mux.HandleFunc("/setup", func(w http.ResponseWriter, r *http.Request) { w.Write(setup) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  MakerDash-Setup.exe\n", hex.EncodeToString(sum[:]))
	})
	go http.Serve(ln, mux)
	os.Setenv("MAKERDASH_UPDATE_API", base)
	UpdateRepo = "real/repo"
	AppVersion = "1.1.0"
	link = NewLinkDeferred()
	updater = &Updater{st: UpdateState{State: "idle"}}
	updater.Check(false)
	if s := updater.Snapshot(); s.State != "available" {
		t.Fatalf("%+v", s)
	}
	updater.Install()
	s := updater.Snapshot()
	t.Logf("Zustand nach Install: %s %s", s.State, s.Error)
	if s.State != "installing" {
		t.Fatalf("%+v", s)
	}
	time.Sleep(20 * time.Second) // Installer arbeiten lassen
}
