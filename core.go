//go:build windows

package main

// Kernlogik: Protokoll mit dem Pico, Menülisten, Fader -> Lautstärke.
//
//   PC -> Pico:  PING | SYNC | APPS\t<name>... | OUTS\t<idx>\t<name>... | INS\t<idx>\t<name>...
//                F3\t<name>[\t<sperren 0/1>] | LABELS\t<f1>\t<f2>\t<f3> | CFG\t<k>\t<v>...
//                UPDATING | PROGRESS\t<0-100> | UPDATE_ABORT | REBOOT
//   Pico -> PC:  PONG\tDASH\t<proto>\t<version>\t<boot ok> | F\t<1-3>\t<0-1000>
//                SETAPP\t<name> | SETOUT\t<name> | SETIN\t<name>

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type menuItem struct {
	Name string // Anzeige auf dem OLED
	Key  string // exe-Schlüssel bzw. Geräte-ID
}

var (
	audio *Audio
	link  *Link

	menuMu   sync.Mutex
	lastApps []menuItem
	lastOuts []menuItem
	lastIns  []menuItem
	lastSent string

	faderMu     sync.Mutex
	faderValues = [3]int{-1, -1, -1}
)

// Zeichen, die die Schrift auf dem Pico kann
var translit = map[rune]string{
	'á': "a", 'à': "a", 'â': "a", 'ã': "a", 'å': "a", 'é': "e", 'è': "e", 'ê': "e", 'ë': "e",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'ó': "o", 'ò': "o", 'ô': "o", 'õ': "o", 'ø': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ç': "c", 'ñ': "n", 'Á': "A", 'À': "A", 'É': "E", 'È': "E",
	'Í': "I", 'Ó': "O", 'Ú': "U", 'Ç': "C", 'Ñ': "N", '–': "-", '—': "-", '®': "", '™': "",
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 32 && r < 127:
			b.WriteRune(r)
		case strings.ContainsRune("äöüÄÖÜß°", r):
			b.WriteRune(r)
		default:
			if t, ok := translit[r]; ok {
				b.WriteString(t)
			}
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "?"
	}
	if r := []rune(out); len(r) > 24 {
		out = string(r[:24])
	}
	return out
}

func uniqueName(name string, used map[string]bool) string {
	n := name
	for i := 2; used[n]; i++ {
		n = fmt.Sprintf("%s %d", name, i)
	}
	used[n] = true
	return n
}

func deviceDisplayName(d DevInfo, all []DevInfo, c *Config) string {
	if ic := c.Devices[d.ID]; ic != nil && ic.Alias != "" {
		return ic.Alias
	}
	return deviceBaseName(d, all)
}

// deviceBaseName: Kurzname ("Lautsprecher"), bei Doppelungen der volle Name.
func deviceBaseName(d DevInfo, all []DevInfo) string {
	if d.Short == "" {
		return d.Friendly
	}
	for _, o := range all {
		if o.ID != d.ID && o.Short == d.Short {
			return d.Friendly
		}
	}
	return d.Short
}

func appDisplayName(a AppInfo, c *Config) string {
	if ic := c.Apps[a.Key]; ic != nil && ic.Alias != "" {
		return ic.Alias
	}
	return a.Name
}

type menus struct {
	apps   []menuItem
	outs   []menuItem
	curOut int
	ins    []menuItem
	curIn  int
	f3     string
}

func deviceMenu(devs []DevInfo, def string, c *Config) (items []menuItem, cur int) {
	used := map[string]bool{}
	cur = -1
	for _, d := range devs {
		if ic := c.Devices[d.ID]; ic != nil && ic.Hidden && d.ID != def {
			continue
		}
		if d.ID == def {
			cur = len(items)
		}
		items = append(items, menuItem{uniqueName(sanitize(deviceDisplayName(d, devs, c)), used), d.ID})
	}
	return
}

// fader3Name liefert den Anzeigenamen des Programms auf Fader 3.
func fader3Name(st AudioState, c *Config) string {
	if c.Fader3App == "" {
		return "-"
	}
	for _, a := range st.Apps {
		if a.Key == c.Fader3App {
			return sanitize(appDisplayName(a, c))
		}
	}
	if ic := c.Apps[c.Fader3App]; ic != nil {
		if ic.Alias != "" {
			return sanitize(ic.Alias)
		}
		if ic.Name != "" {
			return sanitize(ic.Name)
		}
	}
	return sanitize(strings.TrimSuffix(c.Fader3App, ".exe"))
}

func buildMenus(st AudioState, c *Config) (m menus) {
	used := map[string]bool{}
	for _, a := range st.Apps {
		if ic := c.Apps[a.Key]; ic != nil && ic.Hidden {
			continue
		}
		m.apps = append(m.apps, menuItem{uniqueName(sanitize(appDisplayName(a, c)), used), a.Key})
	}
	m.outs, m.curOut = deviceMenu(st.Outputs, st.DefaultOut, c)
	m.ins, m.curIn = deviceMenu(st.Inputs, st.DefaultIn, c)
	m.f3 = fader3Name(st, c)
	for _, a := range m.apps {
		if a.Key == c.Fader3App {
			m.f3 = a.Name // gleicher Name wie im Menü (auch bei Dubletten)
		}
	}
	return
}

func rememberSeen(st AudioState) {
	withConfig(func(c *Config) {
		for _, a := range st.Apps {
			if ic := c.Apps[a.Key]; ic == nil {
				c.Apps[a.Key] = &ItemCfg{Name: a.Name}
			} else {
				ic.Name = a.Name
			}
		}
		mark := func(devs []DevInfo, kind string) {
			for _, d := range devs {
				if ic := c.Devices[d.ID]; ic == nil {
					c.Devices[d.ID] = &ItemCfg{Name: d.Friendly, Kind: kind}
				} else {
					ic.Name = d.Friendly
					ic.Kind = kind
				}
			}
		}
		mark(st.Outputs, "out")
		mark(st.Inputs, "in")
	}, true)
}

func listLine(cmd string, cur int, items []menuItem) string {
	var b strings.Builder
	b.WriteString(cmd)
	if cur != -2 {
		b.WriteString("\t" + strconv.Itoa(cur))
	}
	for _, it := range items {
		b.WriteString("\t" + it.Name)
	}
	return b.String()
}

var targetLabels = map[string]string{"master": "System", "mic": "Mikrofon", "none": "-"}

// pushToPico schickt Listen und Beschriftungen (nur bei Änderung oder force).
func pushToPico(force bool) {
	st := audio.State()
	var m menus
	var f1, f2 string
	withConfig(func(c *Config) {
		m = buildMenus(st, c)
		f1, f2 = targetLabels[c.Fader1], targetLabels[c.Fader2]
	}, false)
	lines := []string{
		listLine("APPS", -2, m.apps),
		listLine("OUTS", m.curOut, m.outs),
		listLine("INS", m.curIn, m.ins),
		"F3\t" + m.f3,
		"LABELS\t" + f1 + "\t" + f2 + "\t" + m.f3,
	}
	all := strings.Join(lines, "\n")
	menuMu.Lock()
	lastApps, lastOuts, lastIns = m.apps, m.outs, m.ins
	changed := all != lastSent
	lastSent = all
	menuMu.Unlock()
	if changed || force {
		for _, l := range lines {
			link.Send(l)
		}
	}
}

func sendDisplayCfg() {
	var d Display
	withConfig(func(c *Config) { d = c.Display }, false)
	ov := 0
	if d.Overlay {
		ov = 1
	}
	link.Send(fmt.Sprintf("CFG\tbrightness\t%d\tsaver\t%d\toverlay\t%d", d.Brightness, d.Saver, ov))
}

func faderTarget(n int) string {
	var t string
	withConfig(func(c *Config) {
		switch n {
		case 1:
			t = c.Fader1
		case 2:
			t = c.Fader2
		case 3:
			if c.Fader3App != "" {
				t = "app:" + c.Fader3App
			}
		}
	}, false)
	if t == "none" {
		return ""
	}
	return t
}

// setFader3App wird von der Oberfläche oder vom Dashboard-Menü aufgerufen.
func setFader3App(key string, fromUI bool) {
	withConfig(func(c *Config) { c.Fader3App = key }, true)
	if fromUI {
		var name string
		st := audio.State()
		withConfig(func(c *Config) { name = buildMenus(st, c).f3 }, false)
		link.Send("F3\t" + name + "\t1") // Fader 3 am Dashboard sperren
	}
	pushToPico(false)
	markDirty()
}

func lookup(list []menuItem, name string) string {
	menuMu.Lock()
	defer menuMu.Unlock()
	for _, it := range list {
		if it.Name == name {
			return it.Key
		}
	}
	return ""
}

func handleLine(line string) {
	f := strings.Split(line, "\t")
	switch f[0] {
	case "F":
		if len(f) < 3 {
			return
		}
		n, err1 := strconv.Atoi(f[1])
		v, err2 := strconv.Atoi(f[2])
		if err1 != nil || err2 != nil || n < 1 || n > 3 {
			return
		}
		faderMu.Lock()
		faderValues[n-1] = v
		faderMu.Unlock()
		levelsDirty()
		if t := faderTarget(n); t != "" {
			audio.SetVolume(t, float32(v)/1000)
		}
	case "SETAPP":
		if len(f) > 1 {
			if key := lookup(lastApps, f[1]); key != "" {
				setFader3App(key, false)
			}
		}
	case "SETOUT":
		if len(f) > 1 {
			if id := lookup(lastOuts, f[1]); id != "" {
				audio.SetDefaultDevice(id)
			}
		}
	case "SETIN":
		if len(f) > 1 {
			if id := lookup(lastIns, f[1]); id != "" {
				audio.SetDefaultDevice(id)
			}
		}
	}
}

func onConnect(r Remote) {
	faderMu.Lock()
	faderValues = [3]int{-1, -1, -1}
	faderMu.Unlock()
	pushToPico(true)
	if r.Proto >= 2 {
		sendDisplayCfg()
	}
	link.Send("SYNC")
	firmware.OnConnect(r)
	markDirty()
}

func onStatus(connected bool, port string) {
	if connected {
		setTrayTip("MakerDash – verbunden (" + port + ")")
	} else {
		setTrayTip("MakerDash – nicht verbunden")
		firmware.OnDisconnect()
	}
	markDirty()
}
