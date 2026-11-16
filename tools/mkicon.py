"""Erzeugt App-Symbol, Tray-Symbole, Logo und das Seitenbild des Installers.
Aufruf: python3 tools/mkicon.py   (danach: rsrc -manifest app.manifest -ico icon.ico -o rsrc_windows_amd64.syso)
Farben wie in der Oberfläche: Graphit, Elfenbein für die Fader-Kappen, Bernstein für den Pegel."""
import os
import sys
from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BG, EDGE, SLOT, CAP, SIGNAL = (29, 31, 35), (59, 62, 69), (12, 13, 14), (236, 235, 230), (232, 162, 58)
LEVELS = (0.34, 0.72, 0.5)  # Stellung der drei Fader (0 = unten)


def logo(size, pad=0.0):
    s = 8  # Überabtastung
    n = size * s
    im = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    p = pad * n
    r = 0.2 * (n - 2 * p)
    d.rounded_rectangle((p, p, n - p, n - p), r, fill=BG, outline=EDGE, width=max(s, int(n * 0.012)))
    inner = n - 2 * p
    top, bot = p + inner * 0.2, p + inner * 0.8
    slot_w = inner * (0.05 if size >= 48 else 0.085)
    cap_w, cap_h = inner * (0.2 if size >= 48 else 0.24), inner * (0.105 if size >= 48 else 0.16)
    for i, lv in enumerate(LEVELS):
        x = p + inner * (0.26 + 0.24 * i)
        y = bot - (bot - top) * lv
        if size >= 24:
            d.rounded_rectangle((x - slot_w / 2, top, x + slot_w / 2, bot), slot_w / 2, fill=SLOT)
        d.rounded_rectangle((x - slot_w / 2, y, x + slot_w / 2, bot), slot_w / 2, fill=SIGNAL)
        d.rounded_rectangle((x - cap_w / 2, y - cap_h / 2, x + cap_w / 2, y + cap_h / 2), cap_h * 0.22, fill=CAP)
    return im.resize((size, size), Image.LANCZOS)


def main():
    for size in (16, 32, 48, 256):
        logo(size).save(os.path.join(ROOT, "assets", f"icon{size}.png"))
    logo(512).save(os.path.join(ROOT, "assets", "logo512.png"))
    sizes = [16, 24, 32, 48, 64, 128, 256]
    imgs = [logo(s) for s in sizes]
    imgs[-1].save(os.path.join(ROOT, "icon.ico"), sizes=[(s, s) for s in sizes], append_images=imgs[:-1])

    # Seitenbild des Installers (164 × 314)
    w, h = 164, 314
    side = Image.new("RGB", (w, h), (18, 19, 21))
    side.paste(logo(72), ((w - 72) // 2, 70), logo(72))
    d = ImageDraw.Draw(side)
    font = None
    for cand in sys.argv[1:] + ["C:/Windows/Fonts/bahnschrift.ttf", os.path.expanduser("~/.fonts/Barlow-SemiBold.ttf")]:
        if os.path.exists(cand):
            font = ImageFont.truetype(cand, 21)
            break
    if font:
        tw = d.textlength("MakerDash", font=font)
        d.text(((w - tw) / 2, 158), "MakerDash", fill=CAP, font=font)
    d.line((0, h - 1, w, h - 1), fill=EDGE)
    side.save(os.path.join(ROOT, "installer", "sidebar.bmp"))
    print("ok")


main()
