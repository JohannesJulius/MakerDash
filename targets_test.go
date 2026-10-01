//go:build windows

package main

import (
	"reflect"
	"strings"
	"testing"
)

func testConfig() *Config {
	c := defaultConfig()
	c.Groups = []*Group{{ID: "g1", Name: "Spiele", Apps: []string{"cs2.exe", "valorant.exe"}}}
	c.Apps["spotify.exe"] = &ItemCfg{Name: "Spotify"}
	return c
}

func TestResolveTarget(t *testing.T) {
	c := testConfig()
	c.Fader2 = "group:g1"
	c.Fader3App = "spotify.exe"
	cases := map[string][]string{
		"master":    {"master"},
		"none":      nil,
		"focus":     {"focus"},
		"app:x.exe": {"app:x.exe"},
		"group:g1":  {"app:cs2.exe", "app:valorant.exe"},
		"group:weg": nil,
		"fader2":    {"app:cs2.exe", "app:valorant.exe"},
		"fader3":    {"app:spotify.exe"},
		"fader1":    {"master"},
	}
	for in, want := range cases {
		if got := resolveTarget(c, in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, erwartet %v", in, got, want)
		}
	}
	c.Fader3App = "group:g1"
	if got := targetOf(c, 3); got != "group:g1" {
		t.Errorf("Fader 3 mit Gruppe: %s", got)
	}
	c.Fader3App = ""
	if got := resolveTarget(c, "fader3"); got != nil {
		t.Errorf("leerer Fader 3: %v", got)
	}
}

func TestValidTarget(t *testing.T) {
	c := testConfig()
	for _, ok := range []string{"master", "mic", "none", "focus", "group:g1", "app:a.exe"} {
		if !validTarget(c, ok, false) {
			t.Errorf("%s sollte gültig sein", ok)
		}
	}
	for _, bad := range []string{"", "group:g9", "app:", "fader1", "irgendwas"} {
		if validTarget(c, bad, false) {
			t.Errorf("%s sollte ungültig sein", bad)
		}
	}
	if !validTarget(c, "fader3", true) {
		t.Error("fader3 als Stumm-Ziel")
	}
}

func TestMenuMitGruppen(t *testing.T) {
	c := testConfig()
	c.Fader3App = "group:g1"
	st := AudioState{Apps: []AppInfo{{Key: "spiele.exe", Name: "Spiele"}, {Key: "spotify.exe", Name: "Spotify"}}} // sortiert wie von Audio
	m := buildMenus(st, c)
	var names, keys []string
	for _, it := range m.apps {
		names = append(names, it.Name)
		keys = append(keys, it.Key)
	}
	// Gruppen zuerst, gleichnamiges Programm bekommt eine Nummer, "Aktives Fenster" am Ende
	if want := []string{"Spiele", "Spiele 2", "Spotify", focusLabel}; !reflect.DeepEqual(names, want) {
		t.Errorf("Menü: %v", names)
	}
	if want := []string{"group:g1", "spiele.exe", "spotify.exe", "focus"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("Schlüssel: %v", keys)
	}
	if m.f3 != "Spiele" {
		t.Errorf("Fader-3-Name: %q", m.f3)
	}
}

func TestTargetLabel(t *testing.T) {
	c := testConfig()
	st := AudioState{}
	for in, want := range map[string]string{"master": "System", "mic": "Mikrofon", "none": "-", "focus": focusLabel,
		"group:g1": "Spiele", "app:spotify.exe": "Spotify", "app:neu.exe": "neu"} {
		if got := targetLabel(st, c, in); got != want {
			t.Errorf("%s: %q, erwartet %q", in, got, want)
		}
	}
}

func TestMuteState(t *testing.T) {
	st := AudioState{
		Apps:  []AppInfo{{Key: "cs2.exe"}},
		Muted: map[string]bool{"mic": true, "app:cs2.exe": true},
	}
	if !isMuted(st, []string{"mic"}) || isMuted(st, []string{"master"}) {
		t.Error("Endpunkte")
	}
	// Gruppe: nicht laufende Mitglieder zählen nicht
	if !isMuted(st, []string{"app:cs2.exe", "app:valorant.exe"}) {
		t.Error("Gruppe mit einem laufenden, stummen Programm")
	}
	if isMuted(st, []string{"app:valorant.exe"}) {
		t.Error("nichts läuft -> nicht stumm")
	}
	c := testConfig()
	if l := muteLine(st, c); l != "MUTE\t1\tMikrofon" {
		t.Errorf("Standard-Ziel: %q", l)
	}
	c.MuteTarget = "group:g1"
	if l := muteLine(st, c); !strings.HasPrefix(l, "MUTE\t1\tSpiele") {
		t.Errorf("Gruppe: %q", l)
	}
}

func TestAudioStateEqualMuted(t *testing.T) {
	a := AudioState{Muted: map[string]bool{"mic": true}}
	b := AudioState{Muted: map[string]bool{"master": true}}
	if a.equal(b) || !a.equal(AudioState{Muted: map[string]bool{"mic": true}}) {
		t.Error("Stumm-Zustand wird beim Vergleich nicht berücksichtigt")
	}
}
