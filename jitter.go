package main

// Puffer zwischen zwei Audio-Geräten mit leicht unterschiedlichem Takt (reines Go, ohne Windows-Aufrufe).
//
// Ein Mikrofon liefert z. B. 48 000,5 Abtastwerte pro Sekunde, das virtuelle Kabel nimmt 47 999,8 ab.
// Ohne Ausgleich läuft der Puffer irgendwann leer (Aussetzer) oder voll (wachsende Verzögerung).
// jitterBuf hält deshalb einen Vorrat um einen Sollwert und lässt bei Bedarf unhörbar einzelne
// Abtastwerte aus bzw. wiederholt sie.

const (
	mixRate     = 48000
	mixChannels = 2
	driftEvery  = 480 // höchstens ein Abtastwert Korrektur pro 10 ms (0,2 %)
)

type jitterBuf struct {
	buf    []int16 // Stereo, verschachtelt
	r, n   int     // Lese-Position und Füllstand in Frames
	frames int     // Kapazität in Frames

	target, low, high int
	primed            bool
	cnt               int
	peak              int32 // größter Betrag seit dem letzten takePeak (nach Verstärkung)

	avg              float64 // geglätteter Füllstand (Zeitkonstante ~1 s) – Grundlage für den Taktausgleich
	dropped, doubled int     // Statistik: ausgelassene / wiederholte Abtastwerte
	resyncs, starves int     // Statistik: harte Sprünge / Leerläufe
	total            int64   // Statistik: insgesamt angenommene Frames
}

func newJitterBuf() *jitterBuf {
	frames := mixRate / 4 // 250 ms
	return &jitterBuf{
		buf:    make([]int16, frames*mixChannels),
		frames: frames,
		target: mixRate * 20 / 1000,
		low:    mixRate * 14 / 1000,
		high:   mixRate * 32 / 1000,
	}
}

// push hängt Stereo-Abtastwerte an. Läuft der Puffer über, wird Altes verworfen.
func (j *jitterBuf) push(samples []int16) {
	n := len(samples) / mixChannels
	if n == 0 {
		return
	}
	j.total += int64(n)
	if n > j.frames {
		samples = samples[(n-j.frames)*mixChannels:]
		n = j.frames
	}
	if j.n+n > j.frames {
		// Überlauf: bis auf den Sollwert leeren, damit die Verzögerung nicht hängen bleibt
		drop := j.n + n - j.frames
		if keep := j.n - drop; keep > j.target {
			drop = j.n - j.target
		}
		j.r = (j.r + drop) % j.frames
		j.n -= drop
	}
	w := (j.r + j.n) % j.frames
	first := j.frames - w
	if first > n {
		first = n
	}
	copy(j.buf[w*mixChannels:], samples[:first*mixChannels])
	copy(j.buf, samples[first*mixChannels:n*mixChannels])
	j.n += n
}

// pushSilence hängt Stille an (Geräte melden stille Pakete ohne Daten).
func (j *jitterBuf) pushSilence(frames int) {
	const chunk = 480
	var zero [chunk * mixChannels]int16
	for frames > 0 {
		n := frames
		if n > chunk {
			n = chunk
		}
		j.push(zero[:n*mixChannels])
		frames -= n
	}
}

// mixInto addiert bis zu len(dst)/2 Frames mit der Verstärkung gain (1.0 = unverändert) auf dst.
// Solange der Vorrat nach einem Leerlauf noch nicht wieder aufgebaut ist, bleibt dst unverändert.
func (j *jitterBuf) mixInto(dst []int32, gain float32) {
	if !j.primed {
		if j.n < j.target {
			return
		}
		if j.n > j.high {
			// Anlauf mit zu viel Vorrat (manche Treiber liefern zuerst einen großen Block):
			// das Älteste verwerfen – es spielt ja noch nichts
			skip := j.n - j.target
			j.r = (j.r + skip) % j.frames
			j.n -= skip
		}
		j.primed = true
		j.cnt = 0
		j.avg = float64(j.n)
	}
	g := int32(gain * 256)
	frames := len(dst) / mixChannels
	// Füllstand in der Mitte dieses Abrufs glätten. Der Augenblickswert schwankt mit jedem
	// Paket um viele Millisekunden; erst der Mittelwert zeigt, ob die Takte auseinanderlaufen.
	mid := float64(j.n) - float64(frames)/2
	if mid < 0 {
		mid = 0
	}
	j.avg += (mid - j.avg) * float64(frames) / mixRate
	if j.avg > float64(mixRate*60/1000) && j.n > j.target+frames {
		// weit hinterher (z. B. nach einem Hänger): einmal hart aufholen statt sekundenlang nachzulaufen
		skip := j.n - j.target - frames
		j.r = (j.r + skip) % j.frames
		j.n -= skip
		j.avg = float64(j.target)
		j.resyncs++
	}
	for i := 0; i < frames; i++ {
		if j.n == 0 {
			j.primed = false // leer gelaufen: erst wieder Vorrat sammeln
			j.starves++
			return
		}
		p := j.r * mixChannels
		l := int32(j.buf[p]) * g >> 8
		r := int32(j.buf[p+1]) * g >> 8
		dst[i*2] += l
		dst[i*2+1] += r
		if l < 0 {
			l = -l
		}
		if r < 0 {
			r = -r
		}
		if l > j.peak {
			j.peak = l
		}
		if r > j.peak {
			j.peak = r
		}
		j.cnt++
		if j.cnt >= driftEvery {
			j.cnt = 0
			if j.avg > float64(j.high) && j.n > 1 {
				j.r = (j.r + 1) % j.frames // Quelle ist schneller: einen Abtastwert auslassen
				j.n--
				j.dropped++
			} else if j.avg < float64(j.low) {
				j.doubled++
				continue // Quelle ist langsamer: diesen Abtastwert noch einmal verwenden
			}
		}
		j.r = (j.r + 1) % j.frames
		j.n--
	}
}

// takePeak liefert den Spitzenpegel 0..1 seit dem letzten Aufruf.
func (j *jitterBuf) takePeak() float32 {
	p := float32(j.peak) / 32768
	j.peak = 0
	if p > 1 {
		p = 1
	}
	return p
}

func (j *jitterBuf) reset() {
	j.r, j.n, j.cnt, j.primed, j.peak, j.avg = 0, 0, 0, false, 0, 0
}

// clip16 begrenzt die Summe weich auf den Wertebereich von 16 Bit.
func clip16(src []int32, dst []int16) {
	for i, v := range src {
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		dst[i] = int16(v)
	}
}
