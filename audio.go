//go:build windows

package main

import (
	"fmt"
	"os"
	"log"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

type AppInfo struct {
	Key  string // z. B. "spotify.exe"
	Name string // z. B. "Spotify"
}

type DevInfo struct {
	ID       string
	Short    string // z. B. "Lautsprecher"
	Friendly string // z. B. "Lautsprecher (Realtek(R) Audio)"
}

type AudioState struct {
	Apps       []AppInfo
	Outputs    []DevInfo
	DefaultOut string
	Inputs     []DevInfo
	DefaultIn  string
	Muted      map[string]bool // "master", "mic", "app:<exe>" – nur Einträge, die stumm sind
}

func sameDevs(a, b []DevInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a AudioState) equal(b AudioState) bool {
	if a.DefaultOut != b.DefaultOut || a.DefaultIn != b.DefaultIn || len(a.Apps) != len(b.Apps) ||
		len(a.Muted) != len(b.Muted) {
		return false
	}
	for k := range a.Muted {
		if !b.Muted[k] {
			return false
		}
	}
	for i := range a.Apps {
		if a.Apps[i] != b.Apps[i] {
			return false
		}
	}
	return sameDevs(a.Outputs, b.Outputs) && sameDevs(a.Inputs, b.Inputs)
}

// Audio kapselt alle COM-Zugriffe in einem eigenen Thread.
type Audio struct {
	mu       sync.Mutex
	state    AudioState
	pending  map[string]float32 // "master", "mic", "app:<key>"
	cmds     chan func()
	wake     chan struct{}
	onChange func(AudioState)

	// nur im Audio-Thread benutzt
	enum     deviceEnumerator
	sessions map[string][]session
	procs    map[uint32]AppInfo
	master   comObj
	mic      comObj
}

func NewAudio(onChange func(AudioState)) *Audio {
	a := &Audio{
		pending:  map[string]float32{},
		cmds:     make(chan func(), 32),
		wake:     make(chan struct{}, 1),
		onChange: onChange,
		sessions: map[string][]session{},
		procs:    map[uint32]AppInfo{},
	}
	go a.run()
	return a
}

func (a *Audio) State() AudioState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// SetVolume merkt sich einen Zielwert; der Audio-Thread setzt nur den neuesten.
func (a *Audio) SetVolume(target string, v float32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	a.mu.Lock()
	a.pending[target] = v
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// SetMute schaltet Ziele stumm bzw. wieder ein ("master", "mic", "app:<exe>", "focus").
func (a *Audio) SetMute(targets []string, on bool) {
	a.cmds <- func() {
		for _, t := range targets {
			a.setMute(t, on)
		}
		a.refresh()
	}
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

func (a *Audio) setMute(target string, on bool) {
	switch {
	case target == "master":
		if a.master != 0 {
			a.master.call(14, boolArg(on), 0) // IAudioEndpointVolume::SetMute
		}
	case target == "mic":
		if a.mic != 0 {
			a.mic.call(14, boolArg(on), 0)
		}
	case target == "focus":
		if k := foregroundApp(); k != "" {
			a.setMute("app:"+k, on)
		}
	case strings.HasPrefix(target, "app:"):
		for _, s := range a.sessions[strings.TrimPrefix(target, "app:")] {
			s.vol.call(5, boolArg(on), 0) // ISimpleAudioVolume::SetMute
		}
	}
}

// SetDefaultDevice macht ein Gerät (Ausgabe oder Mikrofon) zum Standard.
func (a *Audio) SetDefaultDevice(id string) {
	a.cmds <- func() {
		if err := setDefaultDevice(id); err != nil {
			log.Println("Standardgerät:", err)
		}
		a.refresh()
	}
}

func (a *Audio) RefreshNow() {
	select {
	case a.cmds <- a.refresh:
	default:
	}
}

func (a *Audio) run() {
	runtime.LockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		log.Println("CoInitializeEx:", err)
	}
	var err error
	for {
		a.enum, err = newDeviceEnumerator()
		if err == nil {
			break
		}
		log.Println("DeviceEnumerator:", err)
		time.Sleep(3 * time.Second)
	}
	a.refresh()
	tick := time.NewTicker(1500 * time.Millisecond)
	for {
		select {
		case <-tick.C:
			a.refresh()
		case f := <-a.cmds:
			f()
		case <-a.wake:
			a.applyPending()
		}
	}
}

func (a *Audio) applyPending() {
	a.mu.Lock()
	p := a.pending
	a.pending = map[string]float32{}
	a.mu.Unlock()
	for target, v := range p {
		switch {
		case target == "master":
			if a.master != 0 {
				a.master.call(7, f32(v), 0)
			}
		case target == "mic":
			if a.mic != 0 {
				a.mic.call(7, f32(v), 0)
			}
		case target == "focus":
			if k := foregroundApp(); k != "" {
				for _, s := range a.sessions[k] {
					s.SetVolume(v)
				}
			}
		case strings.HasPrefix(target, "app:"):
			for _, s := range a.sessions[strings.TrimPrefix(target, "app:")] {
				s.SetVolume(v)
			}
		}
	}
}

func endpointMuted(ev comObj) bool {
	if ev == 0 {
		return false
	}
	var m int32
	ev.call(15, ptr(&m)) // IAudioEndpointVolume::GetMute
	return m != 0
}

// foregroundApp liefert den exe-Schlüssel des Programms im Vordergrund ("" wenn unbekannt).
func foregroundApp() string {
	h, _, _ := pGetForegroundWindow.Call()
	if h == 0 {
		return ""
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	return processInfo(pid).Key
}

func (a *Audio) refresh() {
	// Endpunkt-Lautstärken für Master und Mikro neu holen
	a.master.Release()
	a.master = 0
	a.mic.Release()
	a.mic = 0
	defOut, defIn := "", ""
	if d, err := a.enum.Default(eRender, eMultimedia); err == nil {
		defOut = d.ID()
		a.master, _ = d.activate(&iidIAudioEndpointVolume)
		d.Release()
	}
	if d, err := a.enum.Default(eCapture, eConsole); err == nil {
		defIn = d.ID()
		a.mic, _ = d.activate(&iidIAudioEndpointVolume)
		d.Release()
	}
	var ins []DevInfo
	if caps, err := a.enum.Devices(eCapture); err == nil {
		for _, d := range caps {
			ins = append(ins, DevInfo{
				ID:       d.ID(),
				Short:    d.property(pkeyDeviceDesc),
				Friendly: d.property(pkeyDeviceFriendlyName),
			})
			d.Release()
		}
	}
	sort.Slice(ins, func(i, j int) bool { return ins[i].Friendly < ins[j].Friendly })

	// Alte Sessions freigeben
	for _, list := range a.sessions {
		for _, s := range list {
			s.vol.Release()
		}
	}
	a.sessions = map[string][]session{}

	var outs []DevInfo
	devs, err := a.enum.Devices(eRender)
	if err != nil {
		log.Println(err)
	}
	seenPIDs := map[uint32]bool{}
	for _, d := range devs {
		outs = append(outs, DevInfo{
			ID:       d.ID(),
			Short:    d.property(pkeyDeviceDesc),
			Friendly: d.property(pkeyDeviceFriendlyName),
		})
		sess, err := deviceSessions(d)
		d.Release()
		if err != nil {
			debugf("Sessions: %v", err)
			continue
		}
		for _, s := range sess {
			debugf("Session pid=%d system=%v", s.pid, s.system)
			if s.system || s.pid == 0 {
				s.vol.Release()
				continue
			}
			info, ok := a.procs[s.pid]
			if !ok {
				info = processInfo(s.pid)
				debugf("Prozess %d -> %q %q", s.pid, info.Key, info.Name)
				a.procs[s.pid] = info
			}
			seenPIDs[s.pid] = true
			if info.Key == "" {
				s.vol.Release()
				continue
			}
			a.sessions[info.Key] = append(a.sessions[info.Key], s)
		}
	}
	for pid := range a.procs {
		if !seenPIDs[pid] {
			delete(a.procs, pid)
		}
	}

	appMap := map[string]AppInfo{}
	for _, info := range a.procs {
		if info.Key != "" {
			appMap[info.Key] = info
		}
	}
	apps := make([]AppInfo, 0, len(appMap))
	for _, info := range appMap {
		apps = append(apps, info)
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	sort.Slice(outs, func(i, j int) bool { return outs[i].Friendly < outs[j].Friendly })

	muted := map[string]bool{}
	if endpointMuted(a.master) {
		muted["master"] = true
	}
	if endpointMuted(a.mic) {
		muted["mic"] = true
	}
	for key, list := range a.sessions {
		if len(list) > 0 {
			var m int32
			list[0].vol.call(6, ptr(&m)) // ISimpleAudioVolume::GetMute
			if m != 0 {
				muted["app:"+key] = true
			}
		}
	}
	st := AudioState{Apps: apps, Outputs: outs, DefaultOut: defOut, Inputs: ins, DefaultIn: defIn, Muted: muted}
	a.mu.Lock()
	changed := !a.state.equal(st)
	a.state = st
	a.mu.Unlock()
	if changed && a.onChange != nil {
		a.onChange(st)
	}
}

// processInfo liefert Schlüssel (exe-Name) und lesbaren Namen eines Prozesses.
var debugLog = os.Getenv("MAKERDASH_DEBUG") != ""
var debugSeen = map[string]bool{}

// debugf schreibt jede Meldung nur einmal ins Log (nur mit MAKERDASH_DEBUG oder --debug).
func debugf(format string, args ...any) {
	if !debugLog {
		return
	}
	m := fmt.Sprintf(format, args...)
	if debugSeen[m] {
		return
	}
	debugSeen[m] = true
	log.Println(m)
}

func processInfo(pid uint32) AppInfo {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		debugf("OpenProcess %d: %v", pid, err)
		return AppInfo{}
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		debugf("QueryFullProcessImageName %d: %v", pid, err)
		return AppInfo{}
	}
	path := windows.UTF16ToString(buf[:size])
	base := filepath.Base(path)
	key := strings.ToLower(base)
	name := fileDescription(path)
	if name == "" || len([]rune(name)) > 22 {
		name = strings.TrimSuffix(base, filepath.Ext(base))
		r := []rune(name)
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		name = string(r)
	}
	return AppInfo{Key: key, Name: name}
}

func fileDescription(path string) string {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return ""
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return ""
	}
	var tr *[2]uint16
	var n uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&data[0]), `\VarFileInfo\Translation`, unsafe.Pointer(&tr), &n); err != nil || n < 4 {
		return ""
	}
	sub := `\StringFileInfo\` + hex4(tr[0]) + hex4(tr[1]) + `\FileDescription`
	var p *uint16
	if err := windows.VerQueryValue(unsafe.Pointer(&data[0]), sub, unsafe.Pointer(&p), &n); err != nil || n == 0 {
		return ""
	}
	return strings.TrimSpace(windows.UTF16PtrToString(p))
}

func hex4(v uint16) string {
	const h = "0123456789abcdef"
	return string([]byte{h[v>>12&0xF], h[v>>8&0xF], h[v>>4&0xF], h[v&0xF]})
}
