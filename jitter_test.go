package main

import "testing"

// simulate lässt eine Quelle mit srcRate Frames/s gegen eine Senke mit mixRate laufen
// und zählt Aussetzer (Senke bekommt nichts, obwohl die Quelle läuft).
func simulate(t *testing.T, srcRate float64, seconds int) (gaps int, maxFill int, finalFill int) {
	j := newJitterBuf()
	var acc float64
	pkt := make([]int16, 0, 2048)
	val := int16(1)
	out := make([]int32, 240*2) // Senke holt alle 5 ms 240 Frames
	started := false
	for tick := 0; tick < seconds*200; tick++ {
		// Quelle liefert in 10-ms-Paketen
		if tick%2 == 0 {
			acc += srcRate / 100
			n := int(acc)
			acc -= float64(n)
			pkt = pkt[:0]
			for i := 0; i < n; i++ {
				pkt = append(pkt, val, val)
			}
			j.push(pkt)
		}
		for i := range out {
			out[i] = 0
		}
		j.mixInto(out, 1)
		got := 0
		for i := 0; i < len(out); i += 2 {
			if out[i] != 0 {
				got++
			}
		}
		if got > 0 {
			started = true
		}
		if started && got < 240 {
			gaps++
		}
		if j.n > maxFill {
			maxFill = j.n
		}
	}
	return gaps, maxFill, j.n
}

// Gleicher Takt, aber große Pakete und unruhiger Abruf: Der Ausgleich darf nicht eingreifen.
func TestJitterNoCorrectionWithoutDrift(t *testing.T) {
	j := newJitterBuf()
	pkt := make([]int16, 960*2) // 20-ms-Pakete
	for i := range pkt {
		pkt[i] = 1
	}
	pulls := []int{240, 720, 96, 384, 480, 0, 960, 0} // im Mittel 360 Frames je 7,5 ms
	out := make([]int32, 960*2)
	pushed, pulled := 0, 0
	for tick := 0; tick < 200*60; tick++ {
		pulled += pulls[tick%len(pulls)]
		for pushed+960 <= pulled+960 { // Quelle hält Schritt
			j.push(pkt)
			pushed += 960
		}
		if n := pulls[tick%len(pulls)]; n > 0 {
			j.mixInto(out[:n*2], 1)
		}
	}
	if j.dropped+j.doubled > 60 || j.starves > 1 || j.resyncs > 0 {
		t.Fatalf("unnötige Korrekturen: ausgelassen %d, wiederholt %d, Leerläufe %d, Sprünge %d", j.dropped, j.doubled, j.starves, j.resyncs)
	}
}

func TestJitterResync(t *testing.T) {
	j := newJitterBuf()
	pkt := make([]int16, 480*2)
	out := make([]int32, 240*2)
	run := func(ticks int) {
		for i := 0; i < ticks; i++ {
			if i%2 == 0 {
				j.push(pkt)
			}
			j.mixInto(out, 1)
		}
	}
	// Anlauf mit 200 ms Rückstand: wird schon beim Start verworfen
	for i := 0; i < 20; i++ {
		j.push(pkt)
	}
	run(400)
	if j.n > mixRate*35/1000 || j.dropped > 0 {
		t.Fatalf("Anlauf: Füllstand %d, ausgelassen %d", j.n, j.dropped)
	}
	// Rückstand mitten im Betrieb (150 ms auf einmal): ein harter Sprung, dann wieder Ruhe
	for i := 0; i < 15; i++ {
		j.push(pkt)
	}
	run(600)
	if j.resyncs != 1 || j.n > mixRate*35/1000 {
		t.Fatalf("Rückstand nicht aufgeholt: %d Sprünge, Füllstand %d", j.resyncs, j.n)
	}
}

func TestJitterSameClock(t *testing.T) {
	gaps, maxFill, _ := simulate(t, 48000, 60)
	if gaps != 0 {
		t.Fatalf("Aussetzer bei gleichem Takt: %d", gaps)
	}
	if maxFill > 48000*60/1000 {
		t.Fatalf("Puffer wächst: %d Frames", maxFill)
	}
}

func TestJitterDrift(t *testing.T) {
	// ±500 ppm ist weit mehr, als echte Geräte auseinanderlaufen
	for _, rate := range []float64{48024, 47976} {
		gaps, maxFill, final := simulate(t, rate, 600)
		if gaps != 0 {
			t.Errorf("%.0f Hz: %d Aussetzer in 10 Minuten", rate, gaps)
		}
		if maxFill > 48000*80/1000 || final > 48000*60/1000 {
			t.Errorf("%.0f Hz: Verzögerung wächst (max %d, Ende %d Frames)", rate, maxFill, final)
		}
	}
}

func TestJitterRecoversAfterPause(t *testing.T) {
	j := newJitterBuf()
	out := make([]int32, 480*2)
	j.mixInto(out, 1) // leer: nichts passiert
	if j.primed {
		t.Fatal("ohne Daten darf nichts abgespielt werden")
	}
	one := make([]int16, 480*2)
	for i := range one {
		one[i] = 1000
	}
	j.push(one) // 10 ms – noch unter dem Sollwert
	j.mixInto(out, 1)
	if out[0] != 0 {
		t.Fatal("unter dem Sollwert muss gewartet werden")
	}
	j.push(one)
	j.mixInto(out, 0.5)
	if out[0] != 500 {
		t.Fatalf("Verstärkung 0,5 erwartet 500, bekommen %d", out[0])
	}
	if p := j.takePeak(); p < 0.014 || p > 0.016 {
		t.Fatalf("Spitzenpegel %.4f", p)
	}
	// Überlauf: weit mehr als die Kapazität – Füllstand bleibt begrenzt
	for i := 0; i < 100; i++ {
		j.push(one)
	}
	if j.n > j.frames {
		t.Fatalf("Überlauf nicht begrenzt: %d", j.n)
	}
}

func TestClip(t *testing.T) {
	dst := make([]int16, 3)
	clip16([]int32{40000, -40000, 123}, dst)
	if dst[0] != 32767 || dst[1] != -32768 || dst[2] != 123 {
		t.Fatalf("%v", dst)
	}
}

func TestRootsOf(t *testing.T) {
	type p struct {
		parent uint32
		exe    string
	}
	procs := map[uint32]p{
		4:   {0, "System"},
		100: {4, "explorer.exe"},
		200: {100, "chrome.exe"}, // Hauptprozess
		210: {200, "chrome.exe"}, // Audio-Dienst
		211: {200, "Chrome.exe"}, // Tab (andere Schreibweise)
		220: {210, "chrome.exe"},
		300: {100, "spotify.exe"},
		400: {100, "chrome.exe"}, // zweite, unabhängige Instanz
	}
	info := func(pid uint32) (uint32, string, bool) {
		x, ok := procs[pid]
		return x.parent, x.exe, ok
	}
	got := rootsOf([]uint32{220, 211, 210, 300, 400, 999}, info)
	want := []uint32{200, 300, 400, 999}
	if !samePids(got, want) {
		t.Fatalf("bekommen %v, erwartet %v", got, want)
	}
}

func TestCableNames(t *testing.T) {
	if !isCableName("CABLE Input (VB-Audio Virtual Cable)") || isCableName("Lautsprecher (Realtek(R) Audio)") {
		t.Fatal("Kabel-Erkennung")
	}
	if cableRank("CABLE Input (VB-Audio Virtual Cable)") != 0 || cableRank("CABLE-A Input (VB-Audio Cable A)") != 1 ||
		cableRank("Hi-Fi Cable Input (VB-Audio Hi-Fi Cable)") != 3 || cableRank("Mikrofon") != -1 {
		t.Fatal("Rangfolge")
	}
	if gainOf(0) != 0 || gainOf(100) != 1 || gainOf(50) != 0.25 || gainOf(500) != 1 {
		t.Fatal("gainOf")
	}
	if !macInPath(`{2}.\\?\bthenum#{0000110b}_vid#7&2a&0&38184c1d2f2a_c00000000#{6994}\snk`, "38184C1D2F2A") || macInPath("x", "") {
		t.Fatal("macInPath")
	}
}
