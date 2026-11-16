//go:build windows

package main

// Selbsttest: "MakerDash.exe --selftest" prüft die Windows-Schnittstellen für Mikro-Mix und
// Bluetooth der Reihe nach und schreibt das Ergebnis nach %APPDATA%\MakerDash\selftest.txt.
// Jeder Schritt wird sofort gespeichert – bricht der Test ab, zeigt die Datei, wo.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func runSelftest() {
	path := filepath.Join(appDataDir(), "selftest.txt")
	os.MkdirAll(filepath.Dir(path), 0o755)
	var b strings.Builder
	p := func(format string, args ...any) {
		fmt.Fprintf(&b, format+"\r\n", args...)
		os.WriteFile(path, []byte(b.String()), 0o644)
	}
	windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	p("MakerDash %s – Selbsttest %s", AppVersion, time.Now().Format("02.01.2006 15:04:05"))
	p("Windows-Build: %d", windowsBuild())

	enum, err := newDeviceEnumerator()
	if err != nil {
		p("Audio: %v", err)
		return
	}
	var st AudioState
	list := func(flow int) (out []DevInfo) {
		devs, _ := enum.Devices(flow)
		for _, d := range devs {
			out = append(out, DevInfo{ID: d.ID(), Short: d.property(pkeyDeviceDesc), Friendly: d.property(pkeyDeviceFriendlyName)})
			d.Release()
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Friendly < out[j].Friendly })
		return
	}
	st.Outputs, st.Inputs = list(eRender), list(eCapture)
	if d, err := enum.Default(eRender, eMultimedia); err == nil {
		st.DefaultOut = d.ID()
		d.Release()
	}
	if d, err := enum.Default(eCapture, eConsole); err == nil {
		st.DefaultIn = d.ID()
		d.Release()
	}
	mark := func(id, def string) string {
		if id == def {
			return " (Standard)"
		}
		return ""
	}
	p("")
	p("Ausgabegeräte:")
	for _, d := range st.Outputs {
		p("  %s%s", d.Friendly, mark(d.ID, st.DefaultOut))
	}
	p("Aufnahmegeräte:")
	for _, d := range st.Inputs {
		p("  %s%s", d.Friendly, mark(d.ID, st.DefaultIn))
	}

	p("")
	p("Mikro-Mix")
	play, rec, cable := findCable(st)
	if cable {
		p("  Virtuelles Kabel: %s -> %s", play.Friendly, rec.Friendly)
	} else {
		p("  Virtuelles Kabel: nicht gefunden (VB-CABLE installieren)")
	}
	mic, micOK := pickMic(st, "")
	if micOK {
		p("  Mikrofon: %s", mic.Friendly)
		p("  Mikrofon öffnen …")
		if d, err := enum.Device(mic.ID); err != nil {
			p("    Fehler: %v", err)
		} else {
			c, err := openDeviceCapture(d)
			d.Release()
			if err != nil {
				p("    Fehler: %v", err)
			} else {
				peak := float32(0)
				sum := make([]int32, 4800*2)
				for i := 0; i < 60; i++ {
					time.Sleep(5 * time.Millisecond)
					if err := c.read(); err != nil {
						p("    Lesefehler: %v", err)
						break
					}
					c.buf.mixInto(sum[:480], 1)
					if x := c.buf.takePeak(); x > peak {
						peak = x
					}
				}
				c.Close()
				p("    ok, Spitzenpegel %.0f %%", peak*100)
			}
		}
	} else {
		p("  Mikrofon: keins gefunden")
	}
	p("  Programm-Aufnahme (eigener Prozess %d) …", os.Getpid())
	if c, err := openProcessCapture(uint32(os.Getpid())); err != nil {
		p("    Fehler: %v", err)
	} else {
		var rerr error
		for i := 0; i < 40 && rerr == nil; i++ {
			time.Sleep(5 * time.Millisecond)
			rerr = c.read()
		}
		c.Close()
		if rerr != nil {
			p("    Lesefehler: %v", rerr)
		} else {
			p("    ok")
		}
	}
	if cable {
		p("  Kabel öffnen …")
		if d, err := enum.Device(play.ID); err != nil {
			p("    Fehler: %v", err)
		} else {
			r, err := openRender(d)
			d.Release()
			if err != nil {
				p("    Fehler: %v", err)
			} else {
				silence := make([]int16, 480*2)
				var werr error
				for i := 0; i < 20 && werr == nil; i++ {
					if pad, err := r.padding(); err != nil {
						werr = err
					} else if pad < mixTargetPad {
						werr = r.write(silence)
					}
					time.Sleep(5 * time.Millisecond)
				}
				r.Close()
				if werr != nil {
					p("    Schreibfehler: %v", werr)
				} else {
					p("    ok (Puffer %d Frames)", r.frames)
				}
			}
		}
	}

	p("")
	p("Bluetooth")
	p("  Adapter: %v", btAdapterPresent())
	paired := btPaired()
	p("  Gekoppelte Geräte: %d", len(paired))
	for _, d := range paired {
		p("    %s  %s  Klasse 0x%06X  verbunden=%v", btMac(d.Address), windows.UTF16ToString(d.Name[:]), d.ClassOfDevice, d.Connected != 0)
	}
	p("  Geräteknoten lesen …")
	nodes := btNodes()
	p("    %d Knoten", len(nodes))
	for _, n := range nodes {
		if n.battery >= 0 {
			p("    Akku %d %%: %s", n.battery, n.id)
		}
	}
	p("  Audio-Endpunkte untersuchen …")
	bl := &Bluetooth{
		endpoints: map[string]*btEndpoint{}, targets: map[string][]string{},
		containers: map[string]windows.GUID{}, pending: map[string]btPending{},
	}
	bl.refresh()
	for id, ep := range bl.endpoints {
		if ep.isBT {
			p("    %s -> %s", ep.name, ep.ksID)
		}
		_ = id
	}
	p("  Ergebnis:")
	for _, d := range bl.state.Devices {
		p("    %s  verbunden=%v  Akku=%d  schaltbar=%v  (%d Schnittstellen)", d.Name, d.Connected, d.Battery, d.CanSwitch, len(bl.targets[d.ID]))
	}
	p("")
	p("Fertig.")
}
