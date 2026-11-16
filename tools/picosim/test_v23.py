"""Firmware 2.3: Menüs "Mikro-Mix" und "Bluetooth" in der simulierten CircuitPython-Umgebung."""
import sim
from sim import CLOCK, ENC, KEYQ, _Ev, Stop, render
ser = sim.usb_cdc.data
shots = []
def pc(line): ser.rx += (line + "\n").encode()
def press(): KEYQ.append(_Ev(True)); KEYQ.append(_Ev(False))
def turn(n): ENC["pos"] += n
def shot(t): shots.append((t, render()))
def sent(): return [l for l in "".join(ser.tx).split("\n") if l and not l.startswith(("F\t", "PONG"))]

def steps():
    yield 2.5
    pc("PING"); pc("APPS\tMusik\tSpotify\tDiscord\tAktives Fenster"); pc("OUTS\t0\tLautsprecher\tKopfhörer (WH-1000XM4)")
    pc("INS\t0\tUSB-Mikrofon"); pc("F3\tSpotify\t0"); pc("LABELS\tSystem\tMikrofon\tSpotify"); pc("MUTE\t0\tMikrofon")
    pc("CFG\tbrightness\t100\tsaver\t0\toverlay\t1")
    pc("MIXS\t1\t-1\tMusik\tSpotify\tDiscord")
    pc("BT\tWH-1000XM4\t1\t80\tJBL Flip 6\t0\t-1\tSoundcore Liberty 4 NC\t1\t100")
    yield 0.3; shot("1 Startbild ohne Mix")
    press(); yield 0.1; turn(3); yield 0.1; shot("2 Hauptmenü: Mikro-Mix")
    press(); yield 0.1; shot("3 Mikro-Mix: Aus ist aktiv")
    turn(2); yield 0.1; shot("4 Spotify markiert")
    press(); yield 0.2; shot("5 Einblendung Mikro-Mix / Spotify")
    assert sent()[-1] == "SETMIX\tSpotify", sent()
    pc("MIXS\t1\t1\tMusik\tSpotify\tDiscord")
    yield 2.0; shot("6 Startbild mit MIX-Marke")
    # Bluetooth
    press(); yield 0.1; turn(4); yield 0.1; shot("7 Hauptmenü: Bluetooth")
    press(); yield 0.1; shot("8 Bluetooth-Liste mit Akku")
    turn(1); yield 0.1; press(); yield 0.1; shot("9 JBL verbinden -> ...")
    assert sent()[-1] == "BTCON\tJBL Flip 6\t1", sent()
    press(); yield 0.1                     # während "beschäftigt" passiert nichts
    assert sent()[-1] == "BTCON\tJBL Flip 6\t1" and sent().count("BTCON\tJBL Flip 6\t1") == 1, sent()
    pc("BT\tWH-1000XM4\t1\t80\tJBL Flip 6\t1\t-1\tSoundcore Liberty 4 NC\t1\t100")
    yield 0.2; shot("10 JBL verbunden (ohne Akkuwert)")
    turn(1); yield 0.1; shot("11 langer Name wird gekürzt")
    turn(-2); yield 0.1; press(); yield 0.1
    assert sent()[-1] == "BTCON\tWH-1000XM4\t0", sent()
    pc("BT\tWH-1000XM4\t0\t-1\tJBL Flip 6\t1\t-1\tSoundcore Liberty 4 NC\t1\t100")
    yield 0.2; shot("12 WH-1000XM4 getrennt")
    turn(-5); yield 0.1; press(); yield 0.1; shot("13 Zurück -> Hauptmenü auf Bluetooth")
    turn(-1); yield 0.1; press(); yield 0.1; turn(-5); yield 0.1; turn(1); yield 0.1; press(); yield 0.3
    assert sent()[-1] == "SETMIX\t", sent()
    shot("14 Mikro-Mix aus"); pc("MIXS\t1\t-1\tMusik\tSpotify\tDiscord")
    yield 2.0; shot("15 Startbild ohne Marke")
    # Kabel fehlt / keine Geräte
    pc("MIXS\t0\t-1\tSpotify"); pc("BT")
    press(); yield 0.1; turn(3); press(); yield 0.1; shot("16 Kabel fehlt")
    turn(-5); press(); yield 0.1; turn(1); press(); yield 0.1; shot("17 keine Bluetooth-Geräte")
    raise Stop

class Scen:
    def __init__(self): self.g = steps(); self.wait_until = 0
    def tick(self):
        if CLOCK[0] >= self.wait_until:
            self.wait_until = CLOCK[0] + next(self.g)

sim.run("../../firmware/code.py", Scen())
print("gesendet:", sent())
from PIL import Image, ImageDraw
W, H = 532, 286; cols = 3; rows = (len(shots) + 2) // 3
sheet = Image.new("L", (cols * W, rows * H), 90); d = ImageDraw.Draw(sheet)
for i, (t, im) in enumerate(shots):
    x, y = (i % cols) * W, (i // cols) * H; d.text((x + 4, y + 6), t, fill=255); sheet.paste(im, (x + 10, y + 24))
sheet.save("sheet_v23.png")
print("ok ->", "sheet_v23.png", "(echte Bibliotheken)" if sim.REAL else "(Nachbildung)")
