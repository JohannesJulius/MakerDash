import sim
from sim import CLOCK, ADC, ENC, KEYQ, _Ev, Stop, render
ser = sim.usb_cdc.data
shots = []
def pc(line): ser.rx += (line + "\n").encode()
def press(): KEYQ.append(_Ev(True)); KEYQ.append(_Ev(False))
def turn(n): ENC["pos"] += n
def fader(pin, frac): ADC[pin] = int(400 + frac * (65535 - 800))
def shot(t): shots.append((t, render()))

# Splash läuft vor der Hauptschleife -> Zeitpunkte über sleep-Hook
def steps():
    yield 0.35; shot("1 Start-Animation")
    yield 0.6; shot("2 Start-Animation")
    yield 1.5
    pc("PING"); pc("APPS\tSpotify\tDiscord\tFirefox"); pc("OUTS\t0\tLautsprecher\tHeadset")
    pc("INS\t0\tUSB-Mikrofon"); pc("F3\tSpotify\t0"); pc("LABELS\tSystem\tMikrofon\tSpotify"); pc("CFG\tbrightness\t80\tsaver\t20\toverlay\t1")
    yield 0.3; shot("3 Startbild verbunden")
    fader("GP28", 0.72); yield 0.4; shot("4 Fader 1 (GP28) -> Overlay System")
    yield 2; shot("5 Overlay weg")
    fader("GP26", 0.3); yield 0.4; shot("6 Fader 3 (GP26) -> Spotify")
    yield 2
    press(); yield 0.1; press(); yield 0.1; turn(1); press(); yield 0.1
    fader("GP26", 0.35); yield 0.4; shot("7 gesperrt: Overlay 'ganz runter'")
    fader("GP26", 0.0); yield 0.5; shot("8 entsperrt")
    yield 2.2
    yield 20.5; shot("9 Schoner")
    yield 3; shot("10 Schoner, 3 s spaeter")
    press(); yield 0.2; shot("11 geweckt (Druck verschluckt)")
    press(); yield 0.1; turn(2); yield 0.1; shot("12 Menue")
    pc("UPDATING"); pc("PROGRESS\t60"); yield 0.2; shot("13 Update 60%")
    pc("REBOOT"); yield 1
    raise Stop

class Scen:
    def __init__(self): self.g = steps(); self.wait_until = 0
    def tick(self):
        if CLOCK[0] >= self.wait_until:
            self.wait_until = CLOCK[0] + next(self.g)

try:
    sim.run("../../firmware/code.py", Scen())
except sim.Reset:
    print("REBOOT ausgelöst -> microcontroller.reset()")
print([l for l in "".join(ser.tx).split("\n") if not l.startswith("F\t")][:10])
print("autoreload:", sim.sup.runtime.autoreload)
from PIL import Image, ImageDraw
W, H = 532, 286; cols = 3; rows = (len(shots) + 2) // 3
sheet = Image.new("L", (cols * W, rows * H), 90); d = ImageDraw.Draw(sheet)
for i, (t, im) in enumerate(shots):
    x, y = (i % cols) * W, (i // cols) * H; d.text((x + 4, y + 6), t, fill=255); sheet.paste(im, (x + 10, y + 24))
sheet.save("sheet_v2.png")
