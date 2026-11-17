//go:build windows

package main

// Software-Modus: Ist kein Dashboard angeschlossen, regelt man die drei Kanäle mit Reglern in der
// App. Die Regler zeigen dabei, was in Windows tatsächlich eingestellt ist (auch wenn man die
// Lautstärke woanders ändert). Steckt man das Dashboard ein, übernehmen wieder die Fader.

import (
	"strings"
	"sync"
	"time"
)

var (
	softMu      sync.Mutex
	softTouched [3]time.Time // zuletzt in der App bewegt – solange nicht von Windows überschreiben
	softWasConn bool
	softSeen    bool // erster Verbindungsstatus schon gemeldet
	softToastAt time.Time
)

const softHold = 1500 * time.Millisecond

func dashboardConnected() bool {
	ok, _, _ := link.Connected()
	return ok
}

// softSetFader: Regler in der App bewegt (nur ohne Dashboard).
func softSetFader(n, v int) {
	if n < 1 || n > 3 || dashboardConnected() {
		return
	}
	v = clampInt(v, 0, 1000)
	softMu.Lock()
	softTouched[n-1] = time.Now()
	softMu.Unlock()
	faderMu.Lock()
	faderValues[n-1] = v
	faderMu.Unlock()
	levelsDirty()
	setTargetVolume(n, v)
}

// softValue rechnet die aktuelle Lautstärke eines Ziels in eine Fader-Stellung (0–1000) um;
// -1 = nichts zu regeln (nicht belegt, Programm läuft nicht).
func softValue(c *Config, n int, lv map[string]float32) int {
	t := targetOf(c, n)
	pos := func(key string) int {
		if v, ok := lv[key]; ok {
			return clampInt(int(v*1000+0.5), 0, 1000)
		}
		return -1
	}
	switch {
	case t == "master" || t == "mic":
		return pos(t)
	case t == "focus":
		if k := foregroundApp(); k != "" {
			return pos("app:" + k)
		}
		return -1
	case t == "mix":
		return clampInt(c.Mix.AppVol*10, 0, 1000)
	case t == "boost":
		return clampInt((c.Mix.MicVol*1000+boostMaxOf(c.Mix.BoostMax)/2)/boostMaxOf(c.Mix.BoostMax), 0, 1000)
	case strings.HasPrefix(t, "app:"):
		return pos(t)
	case strings.HasPrefix(t, "group:"):
		best := -1
		for _, m := range resolveTarget(c, t) {
			if p := pos(m); p > best {
				best = p
			}
		}
		return best
	}
	return -1
}

// softPump hält im Software-Modus die Regler der App auf dem Stand von Windows.
func softPump() {
	for {
		time.Sleep(400 * time.Millisecond)
		foregroundApp() // merkt sich das letzte fremde Programm im Vordergrund (Ziel "Aktives Fenster")
		if dashboardConnected() {
			continue
		}
		lv := audio.Levels()
		var vals [3]int
		withConfig(func(c *Config) {
			for n := 1; n <= 3; n++ {
				vals[n-1] = softValue(c, n, lv)
			}
		}, false)
		now := time.Now()
		softMu.Lock()
		touched := softTouched
		softMu.Unlock()
		changed := false
		faderMu.Lock()
		if dashboardConnected() { // inzwischen eingesteckt: Werte kommen vom Dashboard
			faderMu.Unlock()
			continue
		}
		for i := range vals {
			if now.Sub(touched[i]) < softHold {
				continue
			}
			if faderValues[i] != vals[i] {
				faderValues[i] = vals[i]
				changed = true
			}
		}
		faderMu.Unlock()
		if changed {
			levelsDirty()
		}
	}
}

// softStatus meldet den Wechsel zwischen Dashboard und Software-Modus – aber nicht beim Start
// und nicht, während ein Firmware-Update das Dashboard absichtlich neu startet.
func softStatus(connected bool) {
	softMu.Lock()
	first := !softSeen
	was := softWasConn
	softSeen, softWasConn = true, connected
	softMu.Unlock()
	if first || connected == was {
		return
	}
	if connected {
		softMu.Lock()
		announced := !softToastAt.IsZero() // nur wenn vorher der Software-Modus angesagt wurde
		softToastAt = time.Time{}
		softMu.Unlock()
		if announced {
			toast("ok", "Dashboard wieder verbunden – die Fader übernehmen.")
		}
		return
	}
	go func() {
		time.Sleep(4 * time.Second)
		if dashboardConnected() || firmware.Busy() {
			return
		}
		softMu.Lock()
		softToastAt = time.Now()
		softMu.Unlock()
		toast("info", "Dashboard getrennt. Du regelst die Lautstärken jetzt in der App (Übersicht).")
	}()
}
