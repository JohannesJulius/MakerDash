//go:build windows

package main

// Verbindet Mikro-Mix und Bluetooth mit Konfiguration, Oberfläche und Dashboard.

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	mixer *Mixer
	bt    *Bluetooth

	lastMix []menuItem
	lastBT  []menuItem

	noticeMu sync.Mutex
	notices  []string // Hinweise, die die Oberfläche beim nächsten Öffnen zeigt
)

// ---------------- Absturz-Schutz ----------------
//
// Mikro-Mix und Bluetooth sprechen direkt mit Windows-Treibern. Stürzt die App dabei ab,
// bleibt eine Markierung liegen; beim nächsten Start wird die Funktion dann ausgelassen,
// statt die App in eine Absturz-Schleife zu schicken.

func guardPath(name string) string { return filepath.Join(appDataDir(), "guard-"+name) }

func guardBegin(name string) { os.WriteFile(guardPath(name), []byte("1"), 0o644) }
func guardEnd(name string)   { os.Remove(guardPath(name)) }

// guardCrashed meldet (einmalig), ob der letzte Versuch nicht sauber zu Ende kam.
func guardCrashed(name string) bool {
	if _, err := os.Stat(guardPath(name)); err != nil {
		return false
	}
	os.Remove(guardPath(name))
	return true
}

func addNotice(text string) {
	log.Println("Hinweis:", text)
	noticeMu.Lock()
	notices = append(notices, text)
	noticeMu.Unlock()
}

func takeNotices() []string {
	noticeMu.Lock()
	defer noticeMu.Unlock()
	n := notices
	notices = nil
	return n
}

// checkGuards läuft beim Start, bevor Mixer und Bluetooth angelegt werden.
func checkGuards() (bluetoothOK bool) {
	if guardCrashed("mix") {
		withConfig(func(c *Config) { c.Mix.Enabled = false }, true)
		addNotice("Der Mikro-Mix wurde ausgeschaltet, weil MakerDash beim letzten Start dabei abgestürzt ist.")
	}
	if guardCrashed("loopback") {
		withConfig(func(c *Config) { c.Mix.Source = "" }, true)
		addNotice("Die Programm-Aufnahme für den Mikro-Mix wurde zurückgesetzt, weil MakerDash dabei abgestürzt ist.")
	}
	if guardCrashed("bluetooth") {
		addNotice("Bluetooth ist für diesen Start ausgeschaltet, weil MakerDash dabei abgestürzt ist.")
		return false
	}
	return true
}

// ---------------- Mikro-Mix ----------------

// findCable sucht die beiden Enden von VB-CABLE: "CABLE Input" (Wiedergabe) und "CABLE Output" (Aufnahme).
func findCable(st AudioState) (play, rec DevInfo, ok bool) {
	best := -1
	for _, d := range st.Outputs {
		if r := cableRank(d.Friendly); r >= 0 && (best < 0 || r < best) {
			best, play = r, d
		}
	}
	if best < 0 {
		return
	}
	for _, d := range st.Inputs {
		if cableRank(d.Friendly) != best {
			continue
		}
		if rec.ID == "" || strings.Contains(strings.ToLower(d.Friendly), "cable output") {
			rec = d
		}
	}
	return play, rec, true
}

// pickMic wählt das echte Mikrofon: das eingestellte, sonst das Standard-Mikrofon, sonst das erste.
// Das Kabel selbst kommt nie in Frage (sonst gäbe es eine Schleife).
func pickMic(st AudioState, want string) (DevInfo, bool) {
	var def, first *DevInfo
	for i := range st.Inputs {
		d := &st.Inputs[i]
		if isCableName(d.Friendly) {
			continue
		}
		if d.ID == want {
			return *d, true
		}
		if d.ID == st.DefaultIn {
			def = d
		}
		if first == nil {
			first = d
		}
	}
	if def != nil {
		return *def, true
	}
	if first != nil {
		return *first, true
	}
	return DevInfo{}, false
}

// mixSourceKeys löst die Quelle in exe-Namen auf.
func mixSourceKeys(c *Config) []string {
	s := c.Mix.Source
	switch {
	case s == "":
		return nil
	case strings.HasPrefix(s, "group:"):
		if g := groupByID(c, strings.TrimPrefix(s, "group:")); g != nil {
			return append([]string{}, g.Apps...)
		}
		return nil
	}
	return []string{s}
}

func mixSourceLabel(st AudioState, c *Config) string {
	s := c.Mix.Source
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "group:"):
		return targetLabel(st, c, s)
	}
	return targetLabel(st, c, "app:"+s)
}

// updateMix gleicht den Mixer mit Konfiguration und Geräteliste ab.
func updateMix() {
	if mixer == nil {
		return
	}
	st := audio.State()
	var w mixWant
	var micWant string
	withConfig(func(c *Config) {
		w.Enabled = c.Mix.Enabled
		w.Keys = mixSourceKeys(c)
		w.AppGain, w.MicGain = gainOf(c.Mix.AppVol), gainOf(c.Mix.MicVol)
		micWant = c.Mix.MicID
	}, false)
	if play, _, ok := findCable(st); ok {
		w.CableID = play.ID
	}
	if m, ok := pickMic(st, micWant); ok {
		w.MicID = m.ID
	}
	mixer.Apply(w)
}

// setMixSource wählt das Programm, das mit ins Mikrofon soll ("" = keins). Aufgerufen von
// der Oberfläche und vom Dashboard-Menü; schaltet den Mikro-Mix bei Bedarf ein.
func setMixSource(key string) {
	withConfig(func(c *Config) {
		c.Mix.Source = key
		if key != "" {
			c.Mix.Enabled = true
		}
	}, true)
	updateMix()
	pushToPico(false)
	markDirty()
}

// mixMenu: Einträge für das Dashboard-Menü "Mikro-Mix" (Gruppen, dann Programme).
func mixMenu(apps []menuItem, st AudioState, c *Config) (items []menuItem, cur int) {
	cur = -1
	for _, a := range apps {
		if a.Key == "focus" {
			continue
		}
		if a.Key == c.Mix.Source {
			cur = len(items)
		}
		items = append(items, a)
	}
	if c.Mix.Source != "" && cur < 0 {
		// gewähltes Programm läuft gerade nicht – trotzdem zeigen
		used := map[string]bool{}
		for _, a := range items {
			used[a.Name] = true
		}
		cur = len(items)
		items = append(items, menuItem{uniqueName(mixSourceLabel(st, c), used), c.Mix.Source})
	}
	return
}

// mixLine: "MIXS\t<bereit 0/1>\t<aktiver Eintrag oder -1>\t<name>…"
func mixLine(items []menuItem, cur int, st AudioState, c *Config) string {
	ready := 0
	if _, _, ok := findCable(st); ok {
		ready = 1
	}
	if !c.Mix.Enabled {
		cur = -1
	}
	var b strings.Builder
	b.WriteString("MIXS\t" + strconv.Itoa(ready) + "\t" + strconv.Itoa(cur))
	for _, it := range items {
		b.WriteString("\t" + it.Name)
	}
	return b.String()
}

// ---------------- Bluetooth ----------------

// btMenu: Einträge und Zeile fürs Dashboard: "BT\t<name>\t<0 getrennt/1 verbunden/2 beschäftigt>\t<Akku oder -1>…"
func btMenu() (items []menuItem, line string) {
	var b strings.Builder
	b.WriteString("BT")
	if bt == nil {
		return nil, b.String()
	}
	used := map[string]bool{}
	for _, d := range bt.State().Devices {
		if !d.CanSwitch {
			continue
		}
		name := uniqueName(sanitize(d.Name), used)
		items = append(items, menuItem{name, d.ID})
		state := 0
		if d.Busy != "" {
			state = 2
		} else if d.Connected {
			state = 1
		}
		b.WriteString("\t" + name + "\t" + strconv.Itoa(state) + "\t" + strconv.Itoa(d.Battery))
	}
	return items, b.String()
}

func startBluetooth() {
	if bt != nil {
		return
	}
	bt = NewBluetooth(func(BTState) {
		pushToPico(false)
		markDirty()
	})
}
