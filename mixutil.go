package main

// Kleine Helfer für Mikro-Mix und Bluetooth ohne Windows-Aufrufe (damit sie überall testbar sind).

import (
	"sort"
	"strings"
)

// rootsOf sucht zu jeder PID den obersten Vorfahren mit gleichem Programmnamen und liefert
// die eindeutigen Ergebnisse sortiert. info liefert Elternprozess und exe-Namen einer PID.
func rootsOf(pids []uint32, info func(pid uint32) (parent uint32, exe string, ok bool)) []uint32 {
	seen := map[uint32]bool{}
	var out []uint32
	for _, pid := range pids {
		cur := pid
		for depth := 0; depth < 32; depth++ {
			parent, exe, ok := info(cur)
			if !ok || parent == 0 || parent == cur {
				break
			}
			_, pexe, pok := info(parent)
			if !pok || !strings.EqualFold(pexe, exe) {
				break
			}
			cur = parent
		}
		if !seen[cur] {
			seen[cur] = true
			out = append(out, cur)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func samePids(a, b []uint32) bool {
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

// isCableName erkennt die Geräte von VB-CABLE am Namen
// ("CABLE Input (VB-Audio Virtual Cable)" bzw. "CABLE Output (VB-Audio Virtual Cable)").
func isCableName(friendly string) bool {
	n := strings.ToLower(friendly)
	return strings.Contains(n, "vb-audio") && strings.Contains(n, "cable")
}

// cableRank: kleiner = besser. Das normale VB-CABLE gewinnt vor Cable A/B und Hi-Fi Cable.
func cableRank(friendly string) int {
	n := strings.ToLower(friendly)
	switch {
	case !isCableName(n):
		return -1
	case strings.Contains(n, "virtual cable") && !strings.Contains(n, "hi-fi"):
		return 0
	case strings.Contains(n, "cable a"):
		return 1
	case strings.Contains(n, "cable b"):
		return 2
	}
	return 3
}

// gainOf rechnet Prozent (0–100) in einen Faktor um; leicht gekrümmt, damit sich der Regler
// im unteren Bereich feiner anfühlt (50 % ≈ −12 dB).
func gainOf(percent int) float32 {
	if percent <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	p := float32(percent) / 100
	return p * p
}

// macInPath prüft, ob eine Bluetooth-Adresse (12 Hex-Zeichen) in einem Gerätepfad vorkommt.
func macInPath(path, mac string) bool {
	return mac != "" && strings.Contains(strings.ToLower(path), strings.ToLower(mac))
}
