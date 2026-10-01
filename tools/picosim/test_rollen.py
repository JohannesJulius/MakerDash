"""Testet die Rollen "bridge" (Pico ohne Display) und "panel" (RP2040-Zero ohne USB).

Jede Rolle läuft in einem eigenen Prozess (die Simulation ersetzt Module global).
Aufruf: python3 test_rollen.py      -> beide Tests, Bilder in sheet_panel.png
"""
import subprocess
import sys

if len(sys.argv) == 1:
    ok = True
    for rolle in ("bridge", "panel"):
        r = subprocess.run([sys.executable, __file__, rolle], capture_output=True, text=True)
        print(r.stdout.strip())
        if r.returncode != 0:
            print(r.stderr[-2000:])
            ok = False
    sys.exit(0 if ok else 1)

import sim
from sim import CLOCK, ADC, KEYQ, _Ev, Stop, render

ROLLE = sys.argv[1]
usb = sim.usb_cdc.data
shots = []


def pc(z):
    usb.rx += (z + "\n").encode()


def panel_in(z):
    sim.LINK["uart"].rx += (z + "\n").encode()


def gesendet(liste):
    return [l for l in "".join(liste).split("\n") if l]


if ROLLE == "bridge":
    sim.HW["display"] = False                 # Pico ohne eigenes Display -> Brücke
    sim.HW["usb"] = True

    def steps():
        yield 0.1
        pc("PING")
        yield 0.05
        pong = gesendet(usb.tx)
        assert pong and pong[-1].startswith("PONG\tDASH\t2\t") and pong[-1].endswith("\t-"), pong   # noch kein Panel
        uart = sim.LINK["uart"]
        assert "HELLO" in gesendet(uart.tx), gesendet(uart.tx)
        panel_in("PANEL\t2.3.0")
        yield 0.05
        assert gesendet(usb.tx)[-1] == "PANEL\t2.3.0", gesendet(usb.tx)   # App erfährt vom neuen Panel
        pc("PING")
        pc("APPS\tSpotify\tDiscord")
        pc("CFG\tbrightness\t80")
        yield 0.05
        assert gesendet(usb.tx)[-1].endswith("\t2.3.0"), gesendet(usb.tx)   # PONG mit Panel-Version
        an_panel = gesendet(uart.tx)
        for z in ("PING", "APPS\tSpotify\tDiscord", "CFG\tbrightness\t80"):
            assert z in an_panel, (z, an_panel)
        panel_in("PONG\tDASH\t2\t2.3.0\t1")       # Antwort des Panels auf PING: wird verschluckt
        panel_in("F\t1\t500")
        panel_in("MUTE")
        yield 0.05
        an_pc = gesendet(usb.tx)
        assert an_pc[-2:] == ["F\t1\t500", "MUTE"], an_pc
        assert sum(1 for z in an_pc if z.startswith("PONG")) == 2, an_pc
        print("bridge: OK –", len(an_pc), "Zeilen an den PC,", len(gesendet(uart.tx)), "ans Panel")
        raise Stop

else:  # panel
    sim.HW["display"] = True
    sim.HW["usb"] = False                     # nur über 5 V versorgt -> Panel

    def steps():
        yield 4.5                               # 1,5 s auf USB warten + Start-Animation
        uart = sim.LINK["uart"]
        assert "PANEL\t2.3.0" in gesendet(uart.tx), gesendet(uart.tx)  # meldet sich selbst
        panel_in("HELLO")
        panel_in("PING")
        panel_in("APPS\tSpiele\tSpotify\tAktives Fenster")
        panel_in("OUTS\t0\tLautsprecher\tHeadset")
        panel_in("F3\tSpotify\t0")
        panel_in("LABELS\tSystem\tMikrofon\tSpotify")
        panel_in("MUTE\t0\tMikrofon")
        yield 0.3
        shots.append(("Panel: Startbild", render()))
        tx = gesendet(uart.tx)
        assert tx.count("PANEL\t2.3.0") == 2 and any(z.startswith("PONG") for z in tx), tx
        ADC["GP28"] = int(400 + 0.6 * (65535 - 800))
        yield 0.4
        shots.append(("Panel: Fader 1", render()))
        assert any(z.startswith("F\t1\t") for z in gesendet(uart.tx)), gesendet(uart.tx)
        assert sim.usb_cdc.data.tx == [], sim.usb_cdc.data.tx       # nichts über USB
        print("panel: OK –", len(gesendet(uart.tx)), "Zeilen an die Brücke")
        raise Stop


class Scen:
    def __init__(self):
        self.g = steps()
        self.wait_until = 0

    def tick(self):
        if CLOCK[0] >= self.wait_until:
            self.wait_until = CLOCK[0] + next(self.g)


sim.run("../../firmware/code.py", Scen())
if shots:
    from PIL import Image, ImageDraw
    W, H = 532, 286
    sheet = Image.new("L", (len(shots) * W, H), 90)
    d = ImageDraw.Draw(sheet)
    for i, (t, im) in enumerate(shots):
        d.text((i * W + 4, 6), t, fill=255)
        sheet.paste(im, (i * W + 10, 24))
    sheet.save("sheet_panel.png")
