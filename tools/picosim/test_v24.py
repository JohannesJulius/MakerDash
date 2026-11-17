"""Firmware 2.4: Mikro-Boost – Fader zeigt bis zum eingestellten Maximum (SCALE), Markierung bei 100 %."""
import sim
from sim import CLOCK, Stop, render
ser = sim.usb_cdc.data
shots = []
def pc(line): ser.rx += (line + "\n").encode()
def shot(t): shots.append((t, render()))
PINS = ("GP28", "GP27", "GP26")
def setf(i, v):
    for k in list(sim.ADC):
        if str(k).endswith(PINS[i]) or str(k) == PINS[i]:
            sim.ADC[k] = int(400 + v * (65535 - 800))
def steps():
    yield 2.5
    pc("PING"); pc("APPS\tSpotify"); pc("OUTS\t0\tLautsprecher"); pc("INS\t0\tUSB-Mikrofon"); pc("F3\tSpotify\t0")
    pc("LABELS\tSystem\tMikro-Boost\tSpotify"); pc("CFG\tbrightness\t100\tsaver\t0\toverlay\t1")
    pc("SCALE\t100\t300\t100")
    yield 0.5
    for v, t in ((0.5, "Fader 2 halb -> 150 %"), (1.0, "Fader 2 oben -> 300 %"), (1/3, "Fader 2 auf 1/3 -> 100 %")):
        setf(1, v); yield 0.4; shot(t)
    setf(0, 0.8); yield 0.4; shot("Fader 1 normal (ohne Markierung)")
    pc("SCALE\t100\t200\t100"); setf(1, 0.75); yield 0.4; shot("Bereich 200 % -> 150 %")
    pc("SCALE\tx\t1\t5000"); yield 0.2
    raise Stop
class Scen:
    def __init__(self): self.g = steps(); self.wait_until = 0
    def tick(self):
        if CLOCK[0] >= self.wait_until: self.wait_until = CLOCK[0] + next(self.g)
sim.run("../../firmware/code.py", Scen())
from PIL import Image, ImageDraw
W, H = 532, 286
sheet = Image.new("L", (3 * W, 2 * H), 90); d = ImageDraw.Draw(sheet)
for i, (t, im) in enumerate(shots):
    x, y = (i % 3) * W, (i // 3) * H; d.text((x + 4, y + 6), t, fill=255); sheet.paste(im, (x + 10, y + 24))
sheet.save("sheet_v24.png"); print("ok", len(shots))
