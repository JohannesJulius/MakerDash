//go:build windows

package main

import "testing"

func TestSoftValue(t *testing.T) {
	c := defaultConfig()
	c.Groups = []*Group{{ID: "g1", Name: "Musik", Apps: []string{"spotify.exe", "vlc.exe"}}}
	lv := map[string]float32{"master": 0.42, "mic": 1, "app:spotify.exe": 0.3, "app:vlc.exe": 0.75}
	cases := []struct {
		target string
		want   int
	}{
		{"master", 420}, {"mic", 1000}, {"app:spotify.exe", 300}, {"app:chrome.exe", -1},
		{"group:g1", 750}, {"group:gibtsnicht", -1}, {"none", -1}, {"mix", 700},
	}
	for _, tc := range cases {
		c.Fader1 = tc.target
		if got := softValue(c, 1, lv); got != tc.want {
			t.Errorf("%s: %d, erwartet %d", tc.target, got, tc.want)
		}
	}
	// Mikro-Boost: 150 % bei Obergrenze 300 % = Fader auf halber Höhe
	c.Fader2, c.Mix.MicVol, c.Mix.BoostMax = "boost", 150, 300
	if got := softValue(c, 2, lv); got != 500 {
		t.Errorf("boost: %d", got)
	}
	// Fader 3 speichert Programme ohne "app:"
	c.Fader3App = "vlc.exe"
	if got := softValue(c, 3, lv); got != 750 {
		t.Errorf("Fader 3: %d", got)
	}
	// Hin und zurück: was der Regler setzt, zeigt er danach auch an
	for _, v := range []int{0, 1, 333, 500, 999, 1000} {
		lv["master"] = float32(v) / 1000
		c.Fader1 = "master"
		if got := softValue(c, 1, lv); got != v {
			t.Errorf("Rundung %d -> %d", v, got)
		}
		c.Mix.MicVol = boostPercent(v, c.Mix.BoostMax)
		if got := softValue(c, 2, lv); got < v-2 || got > v+2 {
			t.Errorf("Boost-Rundung %d -> %d", v, got)
		}
	}
}
