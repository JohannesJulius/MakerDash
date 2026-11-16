//go:build windows

package main

// Mikro-Mix: mischt das echte Mikrofon und den Ton ausgewählter Programme und gibt die Summe
// auf das virtuelle Kabel (VB-CABLE, "CABLE Input") aus. Discord, OBS & Co. wählen
// "CABLE Output" als Mikrofon und hören damit Stimme + Programm.
//
//   Mikrofon ──────────────┐
//                          ├─► Mixer ─► CABLE Input ═► CABLE Output (= Mikrofon in Discord/OBS)
//   Programm (Loopback) ───┘
//
// Ein eigener Thread besitzt alle WASAPI-Objekte. Er läuft im 5-ms-Takt, holt wartende
// Aufnahme-Pakete in Puffer (jitter.go) und füllt die Wiedergabe auf ~30 ms Vorrat auf.

import (
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winmm               = windows.NewLazySystemDLL("winmm.dll")
	procTimeBeginPeriod = winmm.NewProc("timeBeginPeriod")
	procTimeEndPeriod   = winmm.NewProc("timeEndPeriod")
)

const (
	mixTick      = 5 * time.Millisecond
	mixTargetPad = mixRate * 30 / 1000 // Vorrat in der Wiedergabe
	mixMaxChunk  = mixRate / 10
)

// mixWant ist der gewünschte Zustand (kommt aus Konfiguration + Geräteliste).
type mixWant struct {
	Enabled bool
	CableID string   // Wiedergabegerät "CABLE Input"; "" = nicht installiert
	MicID   string   // Aufnahmegerät; "" = keins
	Keys    []string // exe-Namen der Programme, die mit ins Mikrofon sollen
	AppGain float32
	MicGain float32
}

func (a mixWant) sameSetup(b mixWant) bool {
	return a.Enabled == b.Enabled && a.CableID == b.CableID && a.MicID == b.MicID &&
		strings.Join(a.Keys, "|") == strings.Join(b.Keys, "|")
}

// MixStatus beschreibt, was gerade läuft (für Oberfläche und Dashboard).
type MixStatus struct {
	State     string `json:"state"` // "off", "nocable", "starting", "running", "error"
	Error     string `json:"error,omitempty"`
	MicOK     bool   `json:"micOk"`
	Capturing int    `json:"capturing"` // Anzahl aufgenommener Programme (Prozessbäume)
	AppError  string `json:"appError,omitempty"`
}

type Mixer struct {
	mu       sync.Mutex
	want     mixWant
	pids     []uint32
	status   MixStatus
	peakMic  float32
	peakApp  float32
	stats    string
	wake     chan struct{}
	onChange func()
}

func NewMixer(onChange func()) *Mixer {
	m := &Mixer{wake: make(chan struct{}, 1), onChange: onChange, status: MixStatus{State: "off"}}
	go m.run()
	go m.watchProcs()
	return m
}

// Apply setzt den gewünschten Zustand.
func (m *Mixer) Apply(w mixWant) {
	m.mu.Lock()
	changed := !m.want.sameSetup(w)
	m.want = w
	m.mu.Unlock()
	if changed {
		m.poke()
	}
}

// SetGains ändert nur die Lautstärken (z. B. wenn ein Fader bewegt wird).
func (m *Mixer) SetGains(app, mic float32) {
	m.mu.Lock()
	m.want.AppGain, m.want.MicGain = app, mic
	m.mu.Unlock()
}

func (m *Mixer) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Mixer) Status() MixStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Peaks liefert die Spitzenpegel (0..1) seit dem letzten Aufruf.
func (m *Mixer) Peaks() (mic, app float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mic, app = m.peakMic, m.peakApp
	m.peakMic, m.peakApp = 0, 0
	return
}

// debugStats: Statistik der Puffer (für Tests und das Log).
func (m *Mixer) debugStats() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

func (m *Mixer) setStatus(s MixStatus) {
	m.mu.Lock()
	changed := m.status != s
	m.status = s
	m.mu.Unlock()
	if changed && m.onChange != nil {
		m.onChange()
	}
}

// watchProcs sucht einmal pro Sekunde die Prozesse der gewählten Programme. So beginnt die
// Aufnahme, sobald ein Programm startet – nicht erst, wenn es schon Ton abspielt.
func (m *Mixer) watchProcs() {
	for {
		m.mu.Lock()
		w := m.want
		m.mu.Unlock()
		var pids []uint32
		if w.Enabled && len(w.Keys) > 0 {
			pids = processRoots(w.Keys)
		}
		m.mu.Lock()
		changed := !samePids(m.pids, pids)
		m.pids = pids
		m.mu.Unlock()
		if changed {
			m.poke()
		}
		time.Sleep(time.Second)
	}
}

// processRoots liefert die Hauptprozesse aller laufenden Programme mit einem der exe-Namen.
func processRoots(keys []string) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	want := map[string]bool{}
	for _, k := range keys {
		want[strings.ToLower(k)] = true
	}
	type proc struct {
		parent uint32
		exe    string
	}
	procs := map[uint32]proc{}
	var match []uint32
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		exe := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		procs[e.ProcessID] = proc{e.ParentProcessID, exe}
		if want[exe] {
			match = append(match, e.ProcessID)
		}
	}
	sort.Slice(match, func(i, j int) bool { return match[i] < match[j] })
	return rootsOf(match, func(pid uint32) (uint32, string, bool) {
		p, ok := procs[pid]
		return p.parent, p.exe, ok
	})
}

func (m *Mixer) run() {
	runtime.LockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		log.Println("Mikro-Mix CoInitializeEx:", err)
	}
	var enum deviceEnumerator
	var out *renderStream
	var outID string
	var mic *captureStream
	var micID string
	apps := map[uint32]*captureStream{}
	retryApp := map[uint32]time.Time{}
	var retryOut, retryMic time.Time
	var appErr string
	logged := map[string]bool{}
	logOnce := func(msg string) {
		if !logged[msg] {
			logged[msg] = true
			log.Println("Mikro-Mix:", msg)
		}
	}
	sum := make([]int32, mixMaxChunk*mixChannels)
	pcm := make([]int16, mixMaxChunk*mixChannels)
	active := false
	var guardUntil time.Time // Absturz-Markierung "mix" bleibt bis hierhin liegen
	var nextStats time.Time
	var written int64

	closeCaptures := func() {
		mic.Close()
		mic, micID = nil, ""
		for pid, c := range apps {
			c.Close()
			delete(apps, pid)
		}
	}
	closeAll := func() {
		closeCaptures()
		out.Close()
		out, outID = nil, ""
		if active {
			procTimeEndPeriod.Call(1)
			active = false
			log.Println("Mikro-Mix: angehalten")
		}
		guardEnd("mix")
		guardUntil = time.Time{}
	}
	idle := func(d time.Duration) {
		select {
		case <-m.wake:
		case <-time.After(d):
		}
	}

	for {
		m.mu.Lock()
		w := m.want
		pids := m.pids
		m.mu.Unlock()

		if !w.Enabled || w.CableID == "" {
			closeAll()
			if w.Enabled {
				m.setStatus(MixStatus{State: "nocable"})
			} else {
				m.setStatus(MixStatus{State: "off"})
			}
			idle(time.Hour)
			continue
		}
		if enum.comObj == 0 {
			e, err := newDeviceEnumerator()
			if err != nil {
				m.setStatus(MixStatus{State: "error", Error: err.Error()})
				idle(3 * time.Second)
				continue
			}
			enum = e
		}
		now := time.Now()

		// Wiedergabe aufs Kabel
		if out != nil && outID != w.CableID {
			out.Close()
			out = nil
		}
		if out == nil {
			if now.Before(retryOut) {
				idle(300 * time.Millisecond)
				continue
			}
			if !active {
				guardBegin("mix")
				guardUntil = now.Add(3 * time.Second)
			}
			d, err := enum.Device(w.CableID)
			if err == nil {
				out, err = openRender(d)
				d.Release()
			}
			if err != nil {
				logOnce(err.Error())
				closeCaptures()
				m.setStatus(MixStatus{State: "error", Error: "Das virtuelle Kabel lässt sich nicht öffnen."})
				retryOut = now.Add(2 * time.Second)
				continue
			}
			outID = w.CableID
			if !active {
				procTimeBeginPeriod.Call(1)
				active = true
				log.Println("Mikro-Mix: läuft")
			}
		}

		// Mikrofon
		if mic != nil && micID != w.MicID {
			mic.Close()
			mic = nil
		}
		if mic == nil && w.MicID != "" && now.After(retryMic) {
			d, err := enum.Device(w.MicID)
			if err == nil {
				mic, err = openDeviceCapture(d)
				d.Release()
			}
			if err != nil {
				logOnce(err.Error())
				retryMic = now.Add(2 * time.Second)
			} else {
				micID = w.MicID
			}
		}

		// Programme
		wanted := map[uint32]bool{}
		for _, pid := range pids {
			wanted[pid] = true
			if apps[pid] != nil || now.Before(retryApp[pid]) {
				continue
			}
			guardBegin("loopback")
			c, err := openProcessCapture(pid)
			guardEnd("loopback")
			if err != nil {
				logOnce(err.Error())
				appErr = err.Error()
				retryApp[pid] = now.Add(5 * time.Second)
				continue
			}
			appErr = ""
			apps[pid] = c
		}
		for pid, c := range apps {
			if !wanted[pid] {
				c.Close()
				delete(apps, pid)
			}
		}
		for pid := range retryApp {
			if !wanted[pid] {
				delete(retryApp, pid)
			}
		}
		if len(pids) == 0 {
			appErr = ""
		}

		// Aufnehmen
		if mic != nil {
			if err := mic.read(); err != nil {
				logOnce(err.Error())
				mic.Close()
				mic = nil
				retryMic = now.Add(time.Second)
			}
		}
		for pid, c := range apps {
			if err := c.read(); err != nil {
				logOnce(err.Error())
				c.Close()
				delete(apps, pid)
				retryApp[pid] = now.Add(2 * time.Second)
			}
		}

		// Mischen und ausgeben
		pad, err := out.padding()
		if err == nil {
			n := mixTargetPad - pad
			if free := int(out.frames) - pad; n > free {
				n = free
			}
			if n > mixMaxChunk {
				n = mixMaxChunk
			}
			if n > 0 {
				s := sum[:n*mixChannels]
				for i := range s {
					s[i] = 0
				}
				var pm, pa float32
				if mic != nil {
					mic.buf.mixInto(s, w.MicGain)
					pm = mic.buf.takePeak()
				}
				for _, c := range apps {
					c.buf.mixInto(s, w.AppGain)
					if p := c.buf.takePeak(); p > pa {
						pa = p
					}
				}
				clip16(s, pcm[:n*mixChannels])
				err = out.write(pcm[:n*mixChannels])
				written += int64(n)
				m.mu.Lock()
				if pm > m.peakMic {
					m.peakMic = pm
				}
				if pa > m.peakApp {
					m.peakApp = pa
				}
				m.mu.Unlock()
			}
		}
		if err != nil {
			logOnce(err.Error())
			out.Close()
			out = nil
			retryOut = now.Add(time.Second)
			m.setStatus(MixStatus{State: "starting"})
			continue
		}

		if now.After(nextStats) {
			nextStats = now.Add(time.Second)
			var sb strings.Builder
			if mic != nil {
				j := mic.buf
				fmt.Fprintf(&sb, "Ausgabe %d Frames, Mikrofon %d Frames, %d ms (-%d +%d, Leerlauf %d, Sprung %d)", written, j.total, j.n*1000/mixRate, j.dropped, j.doubled, j.starves, j.resyncs)
			}
			for pid, c := range apps {
				j := c.buf
				fmt.Fprintf(&sb, " | PID %d: %d ms (-%d +%d, Leerlauf %d, Sprung %d)", pid, j.n*1000/mixRate, j.dropped, j.doubled, j.starves, j.resyncs)
			}
			m.mu.Lock()
			m.stats = sb.String()
			m.mu.Unlock()
		}
		if !guardUntil.IsZero() && now.After(guardUntil) {
			guardEnd("mix")
			guardUntil = time.Time{}
		}
		m.setStatus(MixStatus{State: "running", MicOK: mic != nil, Capturing: len(apps), AppError: appErr})
		time.Sleep(mixTick)
	}
}
