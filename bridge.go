//go:build windows

package main

// Brücke zwischen Go und der HTML-Oberfläche.

import (
	"encoding/json"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

var (
	stateDirty  atomic.Bool
	levelsDirt  atomic.Bool
	lastLevels  string
)

func markDirty()   { stateDirty.Store(true) }
func levelsDirty() { levelsDirt.Store(true) }

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func toast(kind, text string) {
	evalJS("window.__app&&window.__app.toast(" + jsString(kind) + "," + jsString(text) + ")")
}

// uiPump schickt Zustand (max. 10×/s) und Fader-Pegel (max. 30×/s) an die Oberfläche.
func uiPump() {
	t := time.NewTicker(33 * time.Millisecond)
	n := 0
	for range t.C {
		n++
		if !uiVisible() {
			continue
		}
		if n%3 == 0 && stateDirty.Swap(false) {
			if data, err := json.Marshal(buildState()); err == nil {
				evalJS("window.__app&&window.__app.state(" + string(data) + ")")
			}
		}
		if levelsDirt.Swap(false) {
			faderMu.Lock()
			v := faderValues
			faderMu.Unlock()
			b, _ := json.Marshal(v)
			if s := string(b); s != lastLevels {
				lastLevels = s
				evalJS("window.__app&&window.__app.levels(" + s + ")")
			}
		}
	}
}

// ---------------- Zustand für die Oberfläche ----------------

type uiItem struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Full      string `json:"full,omitempty"`
	Alias     string `json:"alias"`
	Hidden    bool   `json:"hidden"`
	Active    bool   `json:"active"` // läuft / angeschlossen
	IsDefault bool   `json:"isDefault,omitempty"`
}

type uiGroup struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Apps   []string `json:"apps"`
	Active bool     `json:"active"` // mindestens ein Programm spielt Ton ab
}

type uiState struct {
	App struct {
		Version string `json:"version"`
		Debug   bool   `json:"debug"`
	} `json:"app"`
	Conn struct {
		Connected bool   `json:"connected"`
		Port      string `json:"port"`
		Firmware  string `json:"firmware"`
		Proto     int    `json:"proto"`
	} `json:"conn"`
	Firmware  FirmwareState `json:"firmware"`
	Setup     SetupState    `json:"setup"`
	Drives    struct {
		CircuitPy string `json:"circuitpy"`
		Bootsel   string `json:"bootsel"`
		Board     string `json:"board"`
	} `json:"drives"`
	CPVersion string      `json:"cpVersion"`
	Faders    [3]int      `json:"faders"`
	F3        string      `json:"f3"`
	F3Key     string      `json:"f3Key"`
	Apps      []uiItem    `json:"apps"`
	Groups    []uiGroup   `json:"groups"`
	Mute      struct {
		Target string `json:"target"`
		Label  string `json:"label"`
		On     bool   `json:"on"`
	} `json:"mute"`
	Outputs   []uiItem    `json:"outputs"`
	Inputs    []uiItem    `json:"inputs"`
	Config    struct {
		Fader1       string   `json:"fader1"`
		Fader2       string   `json:"fader2"`
		Hardware     Hardware `json:"hardware"`
		Display      Display  `json:"display"`
		Autostart    bool     `json:"autostart"`
		AutoUpdate   bool     `json:"autoUpdate"`
		AutoFirmware bool     `json:"autoFirmware"`
		Port         string   `json:"port"`
		SetupDone    bool     `json:"setupDone"`
		UpdateRepo   string   `json:"updateRepo"`
	} `json:"config"`
	Update UpdateState `json:"update"`
	Pins   []string    `json:"pins"`
}

func buildState() uiState {
	var s uiState
	s.App.Version = AppVersion
	s.App.Debug = debugLog
	connected, port, rem := link.Connected()
	s.Conn.Connected, s.Conn.Port = connected, port
	if connected {
		s.Conn.Firmware, s.Conn.Proto = rem.Version, rem.Proto
	}
	fw, setup, cp, bs := firmware.Snapshot()
	s.Firmware, s.Setup = fw, setup
	if len(cp) > 0 {
		s.Drives.CircuitPy = cp[0].Root
	}
	if len(bs) > 0 {
		s.Drives.Bootsel, s.Drives.Board = bs[0].Root, bs[0].Board
	}
	s.CPVersion = circuitPythonVersion
	faderMu.Lock()
	s.Faders = faderValues
	faderMu.Unlock()
	s.Update = updater.Snapshot()
	s.Pins = allPins()

	st := audio.State()
	running := map[string]bool{}
	for _, a := range st.Apps {
		running[a.Key] = true
	}
	present := map[string]bool{}
	for _, d := range append(append([]DevInfo{}, st.Outputs...), st.Inputs...) {
		present[d.ID] = true
	}
	withConfig(func(c *Config) {
		s.F3 = targetLabel(st, c, fader3Target(c))
		s.Mute.Target = muteTargetOf(c)
		s.Mute.Label = targetLabel(st, c, s.Mute.Target)
		s.Mute.On = isMuted(st, resolveTarget(c, s.Mute.Target))
		s.Groups = []uiGroup{}
		for _, g := range c.Groups {
			ug := uiGroup{ID: g.ID, Name: g.Name, Apps: append([]string{}, g.Apps...)}
			for _, k := range g.Apps {
				ug.Active = ug.Active || running[k]
			}
			s.Groups = append(s.Groups, ug)
		}
		s.F3Key = c.Fader3App
		s.Config.Fader1, s.Config.Fader2 = c.Fader1, c.Fader2
		s.Config.Hardware, s.Config.Display = c.Hardware, c.Display
		s.Config.AutoUpdate = !c.NoAutoUpdateCheck
		s.Config.AutoFirmware = !c.NoAutoFirmware
		s.Config.Port = c.Port
		s.Config.SetupDone = c.SetupDone
		s.Config.UpdateRepo = c.UpdateRepo

		for _, k := range sortedKeys(c.Apps) {
			ic := c.Apps[k]
			name := ic.Name
			if name == "" {
				name = strings.TrimSuffix(k, ".exe")
			}
			s.Apps = append(s.Apps, uiItem{Key: k, Name: name, Alias: ic.Alias, Hidden: ic.Hidden, Active: running[k]})
		}
		dev := func(list []DevInfo, def string, kind string) []uiItem {
			var out []uiItem
			seen := map[string]bool{}
			for _, d := range list {
				ic := c.Devices[d.ID]
				it := uiItem{Key: d.ID, Name: deviceBaseName(d, list), Full: d.Friendly, Active: true, IsDefault: d.ID == def}
				if ic != nil {
					it.Alias, it.Hidden = ic.Alias, ic.Hidden
				}
				out = append(out, it)
				seen[d.ID] = true
			}
			for _, k := range sortedKeys(c.Devices) {
				ic := c.Devices[k]
				kk := ic.Kind
				if kk == "" {
					kk = "out"
				}
				if seen[k] || kk != kind {
					continue
				}
				out = append(out, uiItem{Key: k, Name: ic.Name, Full: ic.Name, Alias: ic.Alias, Hidden: ic.Hidden})
			}
			return out
		}
		s.Outputs = dev(st.Outputs, st.DefaultOut, "out")
		s.Inputs = dev(st.Inputs, st.DefaultIn, "in")
	}, false)
	s.Config.Autostart = autostartEnabled()
	sort.SliceStable(s.Apps, func(i, j int) bool {
		if s.Apps[i].Active != s.Apps[j].Active {
			return s.Apps[i].Active
		}
		return strings.ToLower(s.Apps[i].Name) < strings.ToLower(s.Apps[j].Name)
	})
	return s
}

// ---------------- Befehle aus der Oberfläche ----------------

type uiMsg struct {
	Type     string          `json:"type"`
	N        int             `json:"n"`
	Target   string          `json:"target"`
	Key      string          `json:"key"`
	Kind     string          `json:"kind"`
	Hidden   *bool           `json:"hidden"`
	Alias    *string         `json:"alias"`
	On       bool            `json:"on"`
	Value    string          `json:"value"`
	Hardware *Hardware       `json:"hardware"`
	Display  *Display        `json:"display"`
	Apps     *[]string       `json:"apps"`
	Raw      json.RawMessage `json:"raw"`
}

func handleUIMessage(raw string) {
	var m uiMsg
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return // z. B. Echo einer eigenen Nachricht
	}
	switch m.Type {
	case "log":
		log.Println("Oberfläche:", m.Value)
		return
	case "ready":
		if !shell.pageReady {
			log.Println("Oberfläche bereit")
		}
		shell.pageReady = true
		if pendingPage != "" {
			shell.chromium.Eval(`window.__app&&window.__app.go(` + jsString(pendingPage) + `)`)
			pendingPage = ""
		}
		lastLevels = ""
		markDirty()
		levelsDirty()
		return
	}
	// alles Weitere außerhalb des UI-Threads, damit die Oberfläche flüssig bleibt
	go handleCommand(m)
}

func handleCommand(m uiMsg) {
	defer markDirty()
	switch m.Type {
	case "setFaderTarget":
		ok := false
		withConfig(func(c *Config) {
			if !validTarget(c, m.Target, false) {
				return
			}
			ok = true
			if m.N == 1 {
				c.Fader1 = m.Target
			} else if m.N == 2 {
				c.Fader2 = m.Target
			}
		}, true)
		if !ok {
			return
		}
		pushToPico(false)
		link.Send("SYNC")
	case "setMuteTarget":
		withConfig(func(c *Config) {
			if validTarget(c, m.Target, true) && m.Target != "none" {
				c.MuteTarget = m.Target
			}
		}, true)
		pushToPico(false)
	case "toggleMute":
		toggleMute()
	case "addGroup":
		var id string
		withConfig(func(c *Config) {
			id = newGroupID(c)
			name := cleanName(m.Value)
			if name == "" {
				name = "Gruppe " + strings.TrimPrefix(id, "g")
			}
			c.Groups = append(c.Groups, &Group{ID: id, Name: name, Apps: []string{}})
		}, true)
		pushToPico(false)
	case "setGroup":
		withConfig(func(c *Config) {
			g := groupByID(c, m.Key)
			if g == nil {
				return
			}
			if n := cleanName(m.Value); n != "" {
				g.Name = n
			}
			if m.Apps != nil {
				g.Apps = []string{}
				seen := map[string]bool{}
				for _, k := range *m.Apps {
					k = strings.ToLower(strings.TrimSpace(k))
					if k != "" && !seen[k] {
						seen[k] = true
						g.Apps = append(g.Apps, k)
					}
				}
			}
		}, true)
		pushToPico(false)
		// Mitglieder sofort auf die Stellung der Fader bringen, die diese Gruppe regeln
		faderMu.Lock()
		vals := faderValues
		faderMu.Unlock()
		for n := 1; n <= 3; n++ {
			var t string
			withConfig(func(c *Config) { t = targetOf(c, n) }, false)
			if t == "group:"+m.Key && vals[n-1] >= 0 {
				setTargetVolume(n, vals[n-1])
			}
		}
	case "deleteGroup":
		withConfig(func(c *Config) {
			for i, g := range c.Groups {
				if g.ID == m.Key {
					c.Groups = append(c.Groups[:i], c.Groups[i+1:]...)
					break
				}
			}
			ref := "group:" + m.Key
			if c.Fader1 == ref {
				c.Fader1 = "none"
			}
			if c.Fader2 == ref {
				c.Fader2 = "none"
			}
			if c.Fader3App == ref {
				c.Fader3App = ""
			}
			if c.MuteTarget == ref {
				c.MuteTarget = ""
			}
		}, true)
		pushToPico(false)
	case "setFader3App":
		setFader3App(m.Key, true)
	case "setItem":
		withConfig(func(c *Config) {
			var ic *ItemCfg
			if m.Kind == "app" {
				ic = c.Apps[m.Key]
			} else {
				ic = c.Devices[m.Key]
			}
			if ic == nil {
				return
			}
			if m.Hidden != nil {
				ic.Hidden = *m.Hidden
			}
			if m.Alias != nil {
				a := strings.TrimSpace(*m.Alias)
				if r := []rune(a); len(r) > 24 {
					a = string(r[:24])
				}
				ic.Alias = a
			}
		}, true)
		pushToPico(false)
	case "forgetItem":
		withConfig(func(c *Config) {
			if m.Kind == "app" {
				delete(c.Apps, m.Key)
			} else {
				delete(c.Devices, m.Key)
			}
		}, true)
	case "setDefaultDevice":
		audio.SetDefaultDevice(m.Key)
	case "setHardware":
		if m.Hardware == nil {
			return
		}
		h := sanitizeHardware(*m.Hardware)
		if err := validateHardware(h); err != nil {
			toast("error", err.Error())
			return
		}
		withConfig(func(c *Config) { c.Hardware = h }, true)
		if ok, _, _ := link.Connected(); ok || len(firstDrive()) > 0 {
			if err := firmware.ApplyHardware(); err != nil {
				toast("error", "Gespeichert, aber nicht übertragen: "+err.Error())
			} else {
				toast("ok", "Belegung übertragen – das Dashboard startet neu.")
			}
		} else {
			toast("info", "Gespeichert. Wird übertragen, sobald das Dashboard verbunden ist.")
		}
	case "setDisplay":
		if m.Display == nil {
			return
		}
		d := *m.Display
		if d.Brightness < 5 {
			d.Brightness = 5
		}
		if d.Brightness > 100 {
			d.Brightness = 100
		}
		var animChanged bool
		withConfig(func(c *Config) {
			animChanged = c.Display.Animation != d.Animation
			c.Display = d
		}, true)
		sendDisplayCfg()
		if animChanged {
			go firmware.ApplyHardware()
		}
	case "setAutostart":
		if err := setAutostart(m.On); err != nil {
			toast("error", "Autostart konnte nicht geändert werden")
		}
	case "setAutoUpdate":
		withConfig(func(c *Config) { c.NoAutoUpdateCheck = !m.On }, true)
	case "setAutoFirmware":
		withConfig(func(c *Config) { c.NoAutoFirmware = !m.On }, true)
	case "setPort":
		p := strings.ToUpper(strings.TrimSpace(m.Value))
		withConfig(func(c *Config) { c.Port = p }, true)
	case "setUpdateRepo":
		r := strings.Trim(strings.TrimSpace(m.Value), "/")
		r = strings.TrimPrefix(r, "https://github.com/")
		withConfig(func(c *Config) { c.UpdateRepo = r }, true)
		go updater.Check(true)
	case "checkUpdate":
		updater.Check(true)
	case "installUpdate":
		updater.Install()
	case "installFirmware":
		firmware.mu.Lock()
		firmware.failed = false
		firmware.mu.Unlock()
		firmware.Update()
	case "setupCircuitPython":
		firmware.SetupCircuitPython()
	case "setupFirmware":
		firmware.SetupFirmware()
	case "setupReset":
		firmware.set(func() { firmware.setup = SetupState{State: "idle"} })
	case "setupDone":
		withConfig(func(c *Config) { c.SetupDone = true }, true)
	case "openLogs":
		exec.Command("explorer", appDataDir()).Start()
	case "openUrl":
		u := m.Value
		if strings.HasPrefix(u, "https://github.com/") || strings.HasPrefix(u, "https://circuitpython.org/") ||
			strings.HasPrefix(u, "https://go.microsoft.com/") {
			exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
		}
	case "quit":
		runOnUI(quitApp)
	default:
		log.Println("Unbekannter UI-Befehl:", m.Type)
	}
}

// cleanName kürzt Namen aus der Oberfläche auf die Länge, die das Display zeigen kann.
func cleanName(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 24 {
		s = string(r[:24])
	}
	return s
}

func firstDrive() string {
	_, _, cp, _ := firmware.Snapshot()
	if len(cp) > 0 {
		return cp[0].Root
	}
	return ""
}
