"""Wertet die Aufnahme vom Test-Kabel aus: Kommt der 440-Hz-Ton an, und zwar ohne Aussetzer?"""
import math
import struct
import sys
import wave

w = wave.open(sys.argv[1])
rate, ch = w.getframerate(), w.getnchannels()
raw = w.readframes(w.getnframes())
n = len(raw) // (2 * ch)
left = struct.unpack("<%dh" % (n * ch), raw[: n * ch * 2])[::ch]
win = rate // 100  # 10 ms
rms = []
for i in range(0, len(left) - win, win):
    s = left[i : i + win]
    rms.append(math.sqrt(sum(x * x for x in s) / win) / 32768)
loud = [i for i, r in enumerate(rms) if r > 0.05]
if not loud:
    print("FEHLER: kein Ton auf dem Kabel angekommen")
    sys.exit(1)
first, last = loud[0], loud[-1]
body = rms[first + 30 : last - 10]  # Einschwingen weglassen
gaps = sum(1 for r in body if r < 0.05)
# Frequenz über Nulldurchgänge im Kernbereich
seg = left[(first + 30) * win : (last - 10) * win]
zc = sum(1 for a, b in zip(seg, seg[1:]) if a < 0 <= b)
freq = zc * rate / max(1, len(seg))
# Knackser: Sprünge, die ein 440-Hz-Sinus mit diesem Pegel nicht haben kann
peak = max(abs(x) for x in seg) or 1
limit = peak * 2 * math.pi * 440 / rate * 2.5
clicks = sum(1 for a, b in zip(seg, seg[1:]) if abs(b - a) > limit)
dur = len(body) / 100
print(f"Ton von {first/100:.2f}s bis {last/100:.2f}s, ausgewertet {dur:.1f}s")
print(f"Pegel im Mittel {sum(body)/len(body):.3f}, Frequenz {freq:.1f} Hz, Aussetzer (10 ms): {gaps}, Knackser: {clicks}")
ok = gaps == 0 and abs(freq - 440) < 3 and clicks <= dur  # höchstens ~1 Taktkorrektur pro Sekunde
print("OK" if ok else "FEHLER")
sys.exit(0 if ok else 1)
