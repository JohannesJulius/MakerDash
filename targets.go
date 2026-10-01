//go:build windows

package main

// Fader-Ziele, Gruppen (mehrere Programme auf einem Fader, wie die Kanäle in Wave Link)
// und Stummschalten per langem Druck auf den Drehgeber.
//
// Ziel-Schreibweise: "master", "mic", "none", "focus" (Programm im aktiven Fenster),
// "app:<exe>", "group:<id>". Zum Stummschalten zusätzlich "fader1" … "fader3"
// (= das, was der Fader gerade regelt).

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const focusLabel = "Aktives Fenster"

func groupByID(c *Config, id string) *Group {
	for _, g := range c.Groups {
		if g.ID == id {
			return g
		}
	}
	return nil
}

func newGroupID(c *Config) string {
	for i := 1; ; i++ {
		id := "g" + strconv.Itoa(i)
		if groupByID(c, id) == nil {
			return id
		}
	}
}

// fader3Target übersetzt das gespeicherte Fader3App in die Ziel-Schreibweise.
func fader3Target(c *Config) string {
	k := c.Fader3App
	switch {
	case k == "":
		return "none"
	case k == "focus" || strings.HasPrefix(k, "group:"):
		return k
	}
	return "app:" + k
}

// targetOf liefert das Ziel eines Faders (1–3).
func targetOf(c *Config, n int) string {
	switch n {
	case 1:
		return c.Fader1
	case 2:
		return c.Fader2
	case 3:
		return fader3Target(c)
	}
	return "none"
}

// resolveTarget löst Gruppen und Fader-Verweise in einzelne Audio-Ziele auf.
func resolveTarget(c *Config, t string) []string {
	switch {
	case t == "" || t == "none":
		return nil
	case t == "fader1" || t == "fader2" || t == "fader3":
		return resolveTarget(c, targetOf(c, int(t[5]-'0')))
	case strings.HasPrefix(t, "group:"):
		g := groupByID(c, strings.TrimPrefix(t, "group:"))
		if g == nil {
			return nil
		}
		out := make([]string, 0, len(g.Apps))
		for _, k := range g.Apps {
			out = append(out, "app:"+k)
		}
		return out
	}
	return []string{t}
}

// validTarget prüft ein Ziel aus der Oberfläche.
func validTarget(c *Config, t string, allowFaderRef bool) bool {
	switch {
	case t == "master" || t == "mic" || t == "none" || t == "focus":
		return true
	case allowFaderRef && (t == "fader1" || t == "fader2" || t == "fader3"):
		return true
	case strings.HasPrefix(t, "group:"):
		return groupByID(c, strings.TrimPrefix(t, "group:")) != nil
	case strings.HasPrefix(t, "app:"):
		return len(t) > 4
	}
	return false
}

// appLabel: Anzeigename eines Programms, auch wenn es gerade nicht läuft.
func appLabel(st AudioState, c *Config, key string) string {
	for _, a := range st.Apps {
		if a.Key == key {
			return appDisplayName(a, c)
		}
	}
	if ic := c.Apps[key]; ic != nil {
		if ic.Alias != "" {
			return ic.Alias
		}
		if ic.Name != "" {
			return ic.Name
		}
	}
	return strings.TrimSuffix(key, ".exe")
}

// targetLabel: Name eines Ziels für das Display.
func targetLabel(st AudioState, c *Config, t string) string {
	switch {
	case t == "master":
		return "System"
	case t == "mic":
		return "Mikrofon"
	case t == "focus":
		return focusLabel
	case t == "fader1" || t == "fader2" || t == "fader3":
		return targetLabel(st, c, targetOf(c, int(t[5]-'0')))
	case strings.HasPrefix(t, "group:"):
		if g := groupByID(c, strings.TrimPrefix(t, "group:")); g != nil {
			return sanitize(g.Name)
		}
	case strings.HasPrefix(t, "app:"):
		return sanitize(appLabel(st, c, strings.TrimPrefix(t, "app:")))
	}
	return "-"
}

// ---------------- Lautstärke ----------------

func setTargetVolume(n int, v int) {
	var targets []string
	withConfig(func(c *Config) { targets = resolveTarget(c, targetOf(c, n)) }, false)
	for _, t := range targets {
		audio.SetVolume(t, float32(v)/1000)
	}
}

var (
	appsMu   sync.Mutex
	lastKeys = map[string]bool{}
)

// reapplyOnNewApps: Startet ein Programm, das ein Fader regelt (direkt oder über eine Gruppe),
// bekommt es sofort die Lautstärke des Faders – nicht erst, wenn der Fader bewegt wird.
func reapplyOnNewApps(st AudioState) {
	appsMu.Lock()
	neu := map[string]bool{}
	keys := map[string]bool{}
	for _, a := range st.Apps {
		keys[a.Key] = true
		if !lastKeys[a.Key] {
			neu["app:"+a.Key] = true
		}
	}
	lastKeys = keys
	appsMu.Unlock()
	if len(neu) == 0 {
		return
	}
	faderMu.Lock()
	vals := faderValues
	faderMu.Unlock()
	withConfig(func(c *Config) {
		for n := 1; n <= 3; n++ {
			if vals[n-1] < 0 {
				continue
			}
			for _, t := range resolveTarget(c, targetOf(c, n)) {
				if neu[t] {
					audio.SetVolume(t, float32(vals[n-1])/1000)
				}
			}
		}
	}, false)
}

// ---------------- Stummschalten ----------------

func muteTargetOf(c *Config) string {
	if c.MuteTarget == "" {
		return "mic"
	}
	return c.MuteTarget
}

// isMuted: true, wenn alle (laufenden) Teile des Ziels stumm sind.
func isMuted(st AudioState, targets []string) bool {
	running := map[string]bool{}
	for _, a := range st.Apps {
		running["app:"+a.Key] = true
	}
	any := false
	for _, t := range targets {
		if strings.HasPrefix(t, "app:") && !running[t] {
			continue
		}
		if t == "focus" {
			if k := foregroundApp(); k != "" {
				t = "app:" + k
			}
		}
		if !st.Muted[t] {
			return false
		}
		any = true
	}
	return any
}

// muteLine: Zustand fürs Display, z. B. "MUTE\t1\tMikrofon".
func muteLine(st AudioState, c *Config) string {
	t := muteTargetOf(c)
	on := 0
	if isMuted(st, resolveTarget(c, t)) {
		on = 1
	}
	return fmt.Sprintf("MUTE\t%d\t%s", on, targetLabel(st, c, t))
}

// toggleMute wird vom langen Tastendruck am Dashboard (oder aus der Oberfläche) ausgelöst.
func toggleMute() {
	st := audio.State()
	var targets []string
	withConfig(func(c *Config) { targets = resolveTarget(c, muteTargetOf(c)) }, false)
	if len(targets) == 0 {
		return
	}
	audio.SetMute(targets, !isMuted(st, targets))
}
