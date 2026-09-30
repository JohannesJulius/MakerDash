# Prüft mit den echten Adafruit-Bibliotheken (pip install adafruit-blinka-displayio), dass die Texte
# der Firmware mit bitmap_label so sitzen wie mit label.Label. Erlaubt: 1–2 Pixel bei Unterlängen
# und beim Menüpfeil ">" (label.Label zentriert jeden Text auf seine eigenen Pixel, die Firmware
# hält die Grundlinie fest – Pfeil und Text einer Zeile liegen so auf einer Linie).
import os, sys, re
os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
sys.path.insert(0, "firmware/lib")
import displayio, terminalio
from adafruit_bitmap_font import bitmap_font
from adafruit_display_text import label, bitmap_label
FONT_MENU = bitmap_font.load_font("firmware/fonts/menu.bdf"); FONT_GROSS = bitmap_font.load_font("firmware/fonts/big.bdf")
WEISS = 0xFFFFFF
src = open("firmware/code.py", encoding="utf-8").read()
block = src[src.index("KORREKTUR = "):src.index("# --- Startbild ---")]
exec(block)
def raster(g, ox=0, oy=0, out=None):
    out = set() if out is None else out
    ox += g.x; oy += g.y
    for e in g:
        if isinstance(e, displayio.Group): raster(e, ox, oy, out); continue
        b = e.bitmap; pal = e.pixel_shader
        for yy in range(b.height):
            for xx in range(b.width):
                v = b[xx, yy]
                if v and not pal.is_transparent(v): out.add((ox + e.x + xx, oy + e.y + yy))
    return out
def px(l):
    g = displayio.Group(); g.append(l); r = raster(g); g.remove(l); return r
F = {"FONT_MENU": FONT_MENU, "FONT_GROSS": FONT_GROSS, "terminalio.FONT": terminalio.FONT}
calls = re.findall(r'text\((FONT_MENU|FONT_GROSS|terminalio\.FONT), "([^"]*)", \(([\d.]+), ([\d.]+)\), \((\d+), (\d+)\)\)', src)
samples = ["Zurück", "Fader 3", "Mikrofon", "Spotify", "Lautsprecher", "71 %", "GANZ RUNTER!", "(kein Ton aktiv)", ">", "* Discord", "Äpfel gy"]
bad = 0; n = 0
for f, t0, ax, ay, x, y in calls:
    for t in ([t0] if t0 else []) + samples:
        a = px(label.Label(F[f], text=t, color=WEISS, anchor_point=(float(ax), float(ay)), anchored_position=(int(x), int(y))))
        b = px(text(F[f], t, (float(ax), float(ay)), (int(x), int(y))))
        n += 1
        if a != b:
            dy = min(p[1] for p in b) - min(p[1] for p in a); dx = min(p[0] for p in b) - min(p[0] for p in a)
            same_shape = {(p[0]-dx, p[1]-dy) for p in b} == a
            if not (same_shape and dx == 0 and 0 < dy <= 2 and re.search("[gjpqy()>]", t)):
                print("DIFF", f, (ax, ay), (x, y), repr(t), "dx", dx, "dy", dy); bad += 1
print(n, "Vergleiche,", bad, "Abweichungen")
def chk(name, mk, f, t, anc, pos):
    l = mk(); a = px(label.Label(f, text=t, color=WEISS, anchor_point=anc, anchored_position=pos))
    print(name, "ok" if px(l) == a else "ABWEICHUNG")
def m1():
    l = text(FONT_GROSS, "", (0.5, 0.5), (64, 30)); schrift_setzen(l, FONT_MENU, "GANZ RUNTER!"); return l
def m2():
    l = text(FONT_GROSS, "", (0.5, 0.5), (64, 30)); schrift_setzen(l, FONT_MENU, "X"); schrift_setzen(l, FONT_GROSS, "42 %"); return l
def m3():
    l = text(FONT_GROSS, "", (0, 0.5), (0, 27)); gross = "SEHR LANGER PROGRAMMNAME XX"; schrift_setzen(l, FONT_GROSS, gross); schrift_setzen(l, FONT_MENU, gross); return l
def m4():
    l = text(FONT_GROSS, "Maker", (0, 0.5), (128, 20)); platzieren(l, 50, 20); return l
chk("gross->klein", m1, FONT_MENU, "GANZ RUNTER!", (.5, .5), (64, 30))
chk("klein->gross", m2, FONT_GROSS, "42 %", (.5, .5), (64, 30))
chk("langer Name", m3, FONT_MENU, "SEHR LANGER PROGRAMMNAME XX", (0, .5), (0, 27))
chk("platzieren", m4, FONT_GROSS, "Maker", (0, .5), (50, 20))
