//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type ItemCfg struct {
	Alias  string `json:"alias,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
	Name   string `json:"name,omitempty"` // zuletzt gesehener Name
	Kind   string `json:"kind,omitempty"` // Geräte: "out" oder "in"
}

// Group fasst mehrere Programme zusammen, die ein Fader gemeinsam regelt (wie Kanäle in Wave Link).
type Group struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Apps []string `json:"apps"` // exe-Schlüssel
}

// Hardware beschreibt die Verdrahtung des Dashboards (landet in settings.toml).
type Hardware struct {
	Fader1     string `json:"fader1"`
	Fader2     string `json:"fader2"`
	Fader3     string `json:"fader3"`
	Invert     string `json:"invert"` // z. B. "010"
	EncA       string `json:"encA"`
	EncB       string `json:"encB"`
	EncSW      string `json:"encSW"`
	EncDivisor int    `json:"encDivisor"`
	EncReverse bool   `json:"encReverse"`
	SDA        string `json:"sda"`
	SCL        string `json:"scl"`
}

// Display sind die Anzeige-Einstellungen am Dashboard.
type Display struct {
	Brightness int  `json:"brightness"` // 5..100
	Saver      int  `json:"saver"`      // Sekunden, 0 = aus
	Overlay    bool `json:"overlay"`
	Animation  bool `json:"animation"`
}

type Config struct {
	Version int `json:"version"`
	// Fader-Ziele: "master", "mic", "none", "focus" (aktives Fenster), "app:<exe>", "group:<id>".
	// Fader3App ist aus Kompatibilität ohne "app:" gespeichert (exe, "group:<id>" oder "focus").
	Fader1     string              `json:"fader1"`
	Fader2     string              `json:"fader2"`
	Fader3App  string              `json:"fader3App"`
	Groups     []*Group            `json:"groups,omitempty"`
	MuteTarget string              `json:"muteTarget,omitempty"` // Langer Druck auf den Drehgeber; "" = Mikrofon
	Apps       map[string]*ItemCfg `json:"apps"`
	Devices    map[string]*ItemCfg `json:"devices"`
	Port       string              `json:"port"`

	Hardware Hardware `json:"hardware"`
	Display  Display  `json:"display"`

	NoAutoUpdateCheck bool   `json:"noAutoUpdateCheck,omitempty"`
	NoAutoFirmware    bool   `json:"noAutoFirmware,omitempty"`
	UpdateRepo        string `json:"updateRepo,omitempty"` // überschreibt die eingebaute Quelle
	SetupDone         bool   `json:"setupDone,omitempty"`
}

const configVersion = 2

var (
	cfgMu sync.Mutex
	cfg   = defaultConfig()
)

func defaultHardware() Hardware {
	return Hardware{
		Fader1: "GP28", Fader2: "GP27", Fader3: "GP26", Invert: "000",
		EncA: "GP10", EncB: "GP11", EncSW: "GP12", EncDivisor: 4,
		SDA: "GP4", SCL: "GP5",
	}
}

func defaultDisplay() Display {
	return Display{Brightness: 100, Saver: 300, Overlay: true, Animation: true}
}

func defaultConfig() *Config {
	return &Config{
		Version:  configVersion,
		Fader1:   "master",
		Fader2:   "mic",
		Apps:     map[string]*ItemCfg{},
		Devices:  map[string]*ItemCfg{},
		Hardware: defaultHardware(),
		Display:  defaultDisplay(),
	}
}

func appDataDir() string {
	dir, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "MakerDash")
}

func configPath() string { return filepath.Join(appDataDir(), "config.json") }

func loadConfig() (firstRun bool) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return true
	}
	c := defaultConfig()
	if json.Unmarshal(data, c) != nil {
		return true
	}
	if c.Apps == nil {
		c.Apps = map[string]*ItemCfg{}
	}
	if c.Devices == nil {
		c.Devices = map[string]*ItemCfg{}
	}
	if c.Version < 2 {
		// Konfiguration aus Version 1: Hardware/Anzeige gab es noch nicht
		c.Hardware = defaultHardware()
		c.Display = defaultDisplay()
		c.Version = configVersion
	}
	c.Hardware = sanitizeHardware(c.Hardware)
	cfg = c
	return false
}

func saveConfigLocked() {
	p := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, p)
	}
}

func withConfig(fn func(c *Config), save bool) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	fn(cfg)
	if save {
		saveConfigLocked()
	}
}

// ---------------- Hardware / settings.toml ----------------

var adcPins = []string{"GP26", "GP27", "GP28"}

func allPins() []string {
	var p []string
	for i := 0; i <= 22; i++ {
		p = append(p, fmt.Sprintf("GP%d", i))
	}
	return append(p, "GP26", "GP27", "GP28")
}

func validPin(p string, list []string) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

func sanitizeHardware(h Hardware) Hardware {
	d := defaultHardware()
	pins := allPins()
	fix := func(v *string, def string, list []string) {
		if !validPin(*v, list) {
			*v = def
		}
	}
	fix(&h.Fader1, d.Fader1, adcPins)
	fix(&h.Fader2, d.Fader2, adcPins)
	fix(&h.Fader3, d.Fader3, adcPins)
	fix(&h.EncA, d.EncA, pins)
	fix(&h.EncB, d.EncB, pins)
	fix(&h.EncSW, d.EncSW, pins)
	fix(&h.SDA, d.SDA, pins)
	fix(&h.SCL, d.SCL, pins)
	if h.EncDivisor != 1 && h.EncDivisor != 2 && h.EncDivisor != 4 {
		h.EncDivisor = 4
	}
	inv := []byte("000")
	for i := 0; i < 3 && i < len(h.Invert); i++ {
		if h.Invert[i] == '1' {
			inv[i] = '1'
		}
	}
	h.Invert = string(inv)
	return h
}

// validateHardware prüft auf doppelt belegte Pins.
func validateHardware(h Hardware) error {
	used := map[string]string{}
	for name, p := range map[string]string{
		"Fader 1": h.Fader1, "Fader 2": h.Fader2, "Fader 3": h.Fader3,
		"Encoder A": h.EncA, "Encoder B": h.EncB, "Encoder-Taster": h.EncSW,
		"Display SDA": h.SDA, "Display SCL": h.SCL,
	} {
		if other, ok := used[p]; ok {
			a, b := other, name
			if a > b {
				a, b = b, a
			}
			return fmt.Errorf("%s ist doppelt belegt (%s und %s)", p, a, b)
		}
		used[p] = name
	}
	return nil
}

func settingsToml(h Hardware, d Display) string {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	lines := []string{
		"# MakerDash – wird von der App geschrieben. Bitte in der App ändern.",
		fmt.Sprintf("DASH_FADER1 = %q", h.Fader1),
		fmt.Sprintf("DASH_FADER2 = %q", h.Fader2),
		fmt.Sprintf("DASH_FADER3 = %q", h.Fader3),
		fmt.Sprintf("DASH_FADER_INVERT = %q", h.Invert),
		fmt.Sprintf("DASH_ENC_A = %q", h.EncA),
		fmt.Sprintf("DASH_ENC_B = %q", h.EncB),
		fmt.Sprintf("DASH_ENC_SW = %q", h.EncSW),
		fmt.Sprintf("DASH_ENC_DIVISOR = %d", h.EncDivisor),
		fmt.Sprintf("DASH_ENC_REVERSE = %d", b(h.EncReverse)),
		fmt.Sprintf("DASH_SDA = %q", h.SDA),
		fmt.Sprintf("DASH_SCL = %q", h.SCL),
		fmt.Sprintf("DASH_BRIGHTNESS = %d", d.Brightness),
		fmt.Sprintf("DASH_SAVER = %d", d.Saver),
		fmt.Sprintf("DASH_OVERLAY = %d", b(d.Overlay)),
		fmt.Sprintf("DASH_ANIMATION = %d", b(d.Animation)),
	}
	return strings.Join(lines, "\n") + "\n"
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
