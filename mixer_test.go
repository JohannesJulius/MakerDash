//go:build windows

package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func testAudioState(t *testing.T) AudioState {
	runtime.LockOSThread()
	windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)
	enum, err := newDeviceEnumerator()
	if err != nil {
		t.Skip("kein Audio:", err)
	}
	var st AudioState
	for flow, dst := range map[int]*[]DevInfo{eRender: &st.Outputs, eCapture: &st.Inputs} {
		devs, _ := enum.Devices(flow)
		for _, d := range devs {
			*dst = append(*dst, DevInfo{ID: d.ID(), Short: d.property(pkeyDeviceDesc), Friendly: d.property(pkeyDeviceFriendlyName)})
			d.Release()
		}
	}
	return st
}

func TestFindCableAndMic(t *testing.T) {
	st := AudioState{
		Outputs: []DevInfo{
			{ID: "o1", Friendly: "Lautsprecher (Realtek(R) Audio)"},
			{ID: "o2", Friendly: "CABLE-A Input (VB-Audio Cable A)"},
			{ID: "o3", Friendly: "CABLE Input (VB-Audio Virtual Cable)"},
		},
		Inputs: []DevInfo{
			{ID: "i1", Friendly: "CABLE Output (VB-Audio Virtual Cable)"},
			{ID: "i2", Friendly: "Mikrofon (USB Audio)"},
			{ID: "i3", Friendly: "Headset (WH-1000XM4 Hands-Free)"},
			{ID: "i4", Friendly: "CABLE-A Output (VB-Audio Cable A)"},
		},
		DefaultIn: "i1",
	}
	play, rec, ok := findCable(st)
	if !ok || play.ID != "o3" || rec.ID != "i1" {
		t.Fatalf("Kabel: %v %v %v", play, rec, ok)
	}
	// Standard ist das Kabel selbst -> erstes echtes Mikrofon
	if m, ok := pickMic(st, ""); !ok || m.ID != "i2" {
		t.Fatalf("Mikrofon automatisch: %v", m)
	}
	if m, _ := pickMic(st, "i3"); m.ID != "i3" {
		t.Fatalf("Mikrofon eingestellt: %v", m)
	}
	if m, _ := pickMic(st, "i1"); m.ID != "i2" {
		t.Fatalf("Kabel darf nie Mikrofon sein: %v", m)
	}
	st.DefaultIn = "i3"
	if m, _ := pickMic(st, "weg"); m.ID != "i3" {
		t.Fatalf("Standard-Mikrofon: %v", m)
	}
	if _, _, ok := findCable(AudioState{Outputs: st.Outputs[:1]}); ok {
		t.Fatal("Kabel ohne Kabel gefunden")
	}
}

func TestMixMenu(t *testing.T) {
	c := defaultConfig()
	c.Groups = []*Group{{ID: "g1", Name: "Musik", Apps: []string{"spotify.exe", "vlc.exe"}}}
	st := AudioState{Apps: []AppInfo{{Key: "spotify.exe", Name: "Spotify"}, {Key: "chrome.exe", Name: "Google Chrome"}}}
	m := buildMenus(st, c)
	if len(m.mix) != 3 || m.curMix != -1 || m.mix[0].Key != "group:g1" {
		t.Fatalf("Menü: %+v cur=%d", m.mix, m.curMix)
	}
	if l := mixLine(m.mix, m.curMix, st, c); l != "MIXS\t0\t-1\tMusik\tSpotify\tGoogle Chrome" {
		t.Fatalf("Zeile: %q", l)
	}
	c.Mix.Enabled, c.Mix.Source = true, "chrome.exe"
	m = buildMenus(st, c)
	if m.curMix != 2 {
		t.Fatalf("aktiver Eintrag: %d", m.curMix)
	}
	if k := mixSourceKeys(c); len(k) != 1 || k[0] != "chrome.exe" {
		t.Fatalf("Schlüssel: %v", k)
	}
	// gewähltes Programm läuft nicht: trotzdem im Menü
	c.Mix.Source = "vlc.exe"
	c.Apps["vlc.exe"] = &ItemCfg{Name: "VLC media player"}
	m = buildMenus(st, c)
	if len(m.mix) != 4 || m.curMix != 3 || m.mix[3].Name != "VLC media player" {
		t.Fatalf("nicht laufend: %+v cur=%d", m.mix, m.curMix)
	}
	c.Mix.Source = "group:g1"
	if k := mixSourceKeys(c); strings.Join(k, ",") != "spotify.exe,vlc.exe" {
		t.Fatalf("Gruppe: %v", k)
	}
	c.Mix.Enabled = false
	if l := mixLine(m.mix, 0, st, c); !strings.HasPrefix(l, "MIXS\t0\t-1\t") {
		t.Fatalf("ausgeschaltet: %q", l)
	}
	// Fader-Ziel "mix"
	if !validTarget(c, "mix", false) || validTarget(c, "mix", true) || targetLabel(st, c, "mix") != "Mikro-Mix" {
		t.Fatal("Ziel mix")
	}
	c.Fader3App = "mix"
	if fader3Target(c) != "mix" {
		t.Fatal("Fader 3 mix")
	}
}

func TestBoostTarget(t *testing.T) {
	c := defaultConfig()
	st := AudioState{}
	if !validTarget(c, "boost", false) || validTarget(c, "boost", true) || targetLabel(st, c, "boost") != "Mikro-Boost" {
		t.Fatal("Ziel boost")
	}
	if usesBoost(c) || faderScale(c, 2) != 100 {
		t.Fatal("Standard ohne Boost")
	}
	c.Mix.Enabled = false
	c.Fader2 = "boost"
	boostAssigned(c, c.Fader2)
	if !c.Mix.Enabled || !usesBoost(c) || faderScale(c, 2) != 200 || faderScale(c, 1) != 100 {
		t.Fatalf("Fader 2 Boost: enabled=%v scale=%d", c.Mix.Enabled, faderScale(c, 2))
	}
	c.Mix.BoostMax = 300
	c.Fader2 = "mic"
	c.Fader3App = "boost"
	if fader3Target(c) != "boost" || faderScale(c, 3) != 300 || faderScale(c, 2) != 100 {
		t.Fatal("Fader 3 Boost")
	}
	// alte Konfiguration ohne boostMax
	c.Mix.BoostMax = 0
	if faderScale(c, 3) != 200 {
		t.Fatal("Standard-Obergrenze")
	}
}

// Läuft nur mit MAKERDASH_MIXTEST=<Sekunden> (siehe tools/mixtest/run.sh): schleift das
// Test-Mikrofon über den echten Mixer auf das Test-Kabel.
func TestMixerPassthrough(t *testing.T) {
	secs := 0
	for _, ch := range os.Getenv("MAKERDASH_MIXTEST") {
		secs = secs*10 + int(ch-'0')
	}
	if secs == 0 {
		t.Skip("MAKERDASH_MIXTEST nicht gesetzt")
	}
	st := testAudioState(t)
	play, _, ok := findCable(st)
	if !ok {
		t.Skip("kein virtuelles Kabel")
	}
	var mic DevInfo
	for _, d := range st.Inputs {
		if strings.HasPrefix(d.Friendly, "Mikrofon") {
			mic = d
		}
	}
	if mic.ID == "" {
		t.Skip("kein Test-Mikrofon")
	}
	micGain := float32(1)
	if pct, err := strconv.Atoi(os.Getenv("MAKERDASH_MIXBOOST")); err == nil {
		micGain = gainOf(pct) // z. B. 300 = Mikro-Boost 300 %
	}
	m := NewMixer(nil)
	m.Apply(mixWant{Enabled: true, CableID: play.ID, MicID: mic.ID, Keys: []string{"gibtsnicht.exe"}, AppGain: 1, MicGain: micGain})
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	var peak float32
	running := false
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		s := m.Status()
		if s.State == "running" && s.MicOK {
			running = true
		}
		if p, _ := m.Peaks(); p > peak {
			peak = p
		}
	}
	s := m.Status()
	t.Logf("Status %+v, Spitzenpegel Mikrofon %.2f, Puffer: %s", s, peak, m.debugStats())
	if !running || s.State != "running" {
		t.Fatalf("Mixer läuft nicht: %+v", s)
	}
	if peak < 0.05 {
		t.Fatalf("kein Signal vom Mikrofon (%.3f)", peak)
	}
	// ausschalten muss sauber aufräumen
	m.Apply(mixWant{})
	time.Sleep(300 * time.Millisecond)
	if s := m.Status(); s.State != "off" {
		t.Fatalf("nach dem Ausschalten: %+v", s)
	}
}
