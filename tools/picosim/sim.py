import sys, types, time as _rt
from PIL import Image, ImageFont, ImageDraw

# ---------- virtuelle Zeit & Szenario ----------
CLOCK = [0.0]
SCENARIO = None
class Stop(Exception): pass

tm = types.ModuleType("time")
tm.monotonic = lambda: CLOCK[0]
def _sleep(s):
    CLOCK[0] += max(s, 0.005)
    SCENARIO.tick()
tm.sleep = _sleep
sys.modules["time"] = tm

# ---------- board / busio ----------
board = types.ModuleType("board")
for n in ["GP%d" % i for i in range(29)]: setattr(board, n, n)
sys.modules["board"] = board
busio = types.ModuleType("busio"); busio.I2C = lambda scl, sda: ("i2c", scl, sda); sys.modules["busio"] = busio

# ---------- analog ----------
ADC = {}
analogio = types.ModuleType("analogio")
class AnalogIn:
    def __init__(self, pin): self.pin = pin; ADC.setdefault(pin, 30000)
    @property
    def value(self): return ADC[self.pin]
analogio.AnalogIn = AnalogIn; sys.modules["analogio"] = analogio

# ---------- rotaryio / keypad ----------
ENC = {"pos": 0}
rotaryio = types.ModuleType("rotaryio")
class IncrementalEncoder:
    def __init__(self, a, b, divisor=4): pass
    @property
    def position(self): return ENC["pos"]
rotaryio.IncrementalEncoder = IncrementalEncoder; sys.modules["rotaryio"] = rotaryio
KEYQ = []
keypad = types.ModuleType("keypad")
class _Ev:
    def __init__(self, p): self.pressed = p; self.released = not p
class _Events:
    def get(self): return KEYQ.pop(0) if KEYQ else None
class Keys:
    def __init__(self, pins, value_when_pressed, pull): self.events = _Events()
keypad.Keys = Keys; sys.modules["keypad"] = keypad

# ---------- usb_cdc ----------
class Ser:
    def __init__(self): self.rx = b""; self.tx = []; self.connected = True; self.timeout = None; self.write_timeout = None
    @property
    def in_waiting(self): return len(self.rx)
    def read(self, n): d, self.rx = self.rx[:n], self.rx[n:]; return d
    def write(self, b): self.tx.append(b.decode()); return len(b)
usb_cdc = types.ModuleType("usb_cdc"); usb_cdc.data = Ser(); sys.modules["usb_cdc"] = usb_cdc

# ---------- Fonts ----------
class BDF:
    def __init__(self, path):
        self.g = {}; cur = None; L = open(path).read().split("\n"); i = 0
        for l in L:
            if l.startswith("FONT_ASCENT"): self.ascent = int(l.split()[1])
            if l.startswith("FONT_DESCENT"): self.descent = int(l.split()[1])
        while i < len(L):
            l = L[i]
            if l.startswith("ENCODING"): cur = {"c": int(l.split()[1])}
            elif l.startswith("DWIDTH"): cur["adv"] = int(l.split()[1])
            elif l.startswith("BBX"): cur["bbx"] = list(map(int, l.split()[1:]))
            elif l == "BITMAP":
                rows = []; i += 1
                while L[i] != "ENDCHAR": rows.append(int(L[i], 16)); i += 1
                cur["rows"] = rows; self.g[cur["c"]] = cur
            i += 1
    def width(self, s): return sum(self.g[ord(c)]["adv"] for c in s if ord(c) in self.g)
    def draw(self, img, x, base, s, col):
        for ch in s:
            c = self.g.get(ord(ch))
            if not c: continue
            w, h, xo, yo = c["bbx"]; nb = (w + 7) // 8
            for r, v in enumerate(c["rows"]):
                for k in range(w):
                    if v >> (nb * 8 - 1 - k) & 1:
                        px, py = x + xo + k, base - yo - h + 1 + r
                        if 0 <= px < 128 and 0 <= py < 64: img.putpixel((px, py), col)
            x += c["adv"]
class TermFont:  # Näherung der 6x12-Terminalschrift
    ascent, descent = 9, 3
    pil = ImageFont.load_default()
    def width(self, s): return 6 * len(s)
    def draw(self, img, x, base, s, col):
        d = ImageDraw.Draw(img)
        for i, ch in enumerate(s): d.text((x + 6 * i, base - 9), ch, fill=col, font=self.pil)
terminalio = types.ModuleType("terminalio"); terminalio.FONT = TermFont(); sys.modules["terminalio"] = terminalio
abf = types.ModuleType("adafruit_bitmap_font"); bfm = types.ModuleType("adafruit_bitmap_font.bitmap_font")
bfm.load_font = lambda p: BDF(p.lstrip("/")); abf.bitmap_font = bfm
sys.modules["adafruit_bitmap_font"] = abf; sys.modules["adafruit_bitmap_font.bitmap_font"] = bfm

# ---------- displayio ----------
dio = types.ModuleType("displayio")
class Group(list):
    hidden = False
dio.Group = Group
class Bitmap:
    def __init__(self, w, h, n): self.w, self.h = w, h; self.d = {}
    def __setitem__(self, k, v): self.d[k] = v
    def __getitem__(self, k): return self.d.get(k, 0)
class Palette(list):
    def __init__(self, n): super().__init__([0] * n); self.transp = set()
    def make_transparent(self, i): self.transp.add(i)
class TileGrid:
    def __init__(self, bmp, pixel_shader, x=0, y=0): self.bmp, self.pal, self.x, self.y = bmp, pixel_shader, x, y; self.hidden = False
dio.Bitmap, dio.Palette, dio.TileGrid = Bitmap, Palette, TileGrid
dio.release_displays = lambda: None
sys.modules["displayio"] = dio
i2cdb = types.ModuleType("i2cdisplaybus"); i2cdb.I2CDisplayBus = lambda i2c, device_address: None; sys.modules["i2cdisplaybus"] = i2cdb
DISPLAY = {}
ssd = types.ModuleType("adafruit_displayio_ssd1306")
class SSD1306:
    def __init__(self, bus, width, height): self.root_group = None; self.brightness = 1.0; self.awake = True; DISPLAY["d"] = self
    def sleep(self): self.awake = False
    def wake(self): self.awake = True
ssd.SSD1306 = SSD1306; sys.modules["adafruit_displayio_ssd1306"] = ssd

# ---------- vectorio / supervisor / microcontroller / os ----------
vio = types.ModuleType("vectorio")
class Rectangle:
    def __init__(self, pixel_shader, width, height, x=0, y=0): self.pal, self.width, self.height, self.x, self.y = pixel_shader, width, height, x, y; self.hidden = False
vio.Rectangle = Rectangle; sys.modules["vectorio"] = vio
sup = types.ModuleType("supervisor"); sup.runtime = types.SimpleNamespace(autoreload=True); sys.modules["supervisor"] = sup
mc = types.ModuleType("microcontroller")
class Reset(Exception): pass
def _reset(): raise Reset()
mc.reset = _reset; sys.modules["microcontroller"] = mc
import os as _os
ENV = {}
_real_getenv = _os.getenv
_os.getenv = lambda k, d=None: ENV.get(k, d) if k.startswith("DASH_") else _real_getenv(k, d)

# ---------- Label ----------
adt = types.ModuleType("adafruit_display_text"); lab = types.ModuleType("adafruit_display_text.label")
class Label:
    def __init__(self, font, text="", x=0, y=0, scale=1, color=0xFFFFFF, anchor_point=None, anchored_position=None):
        self.font, self.text, self.color, self.hidden = font, text, color, False
        self.anchor_point, self.anchored_position = anchor_point or (0, 0.5), anchored_position or (x, y)
    @property
    def bounding_box(self):
        return (0, 0, self.font.width(self.text), self.font.ascent + self.font.descent)
lab.Label = Label; adt.label = lab
sys.modules["adafruit_display_text"] = adt; sys.modules["adafruit_display_text.label"] = lab

def render(scale=4):
    img = Image.new("L", (128, 64), 0)
    def walk(g):
        if getattr(g, "hidden", False): return
        for e in g:
            if isinstance(e, list): walk(e); continue
            if e.hidden: continue
            if isinstance(e, TileGrid):
                for yy in range(e.bmp.h):
                    for xx in range(e.bmp.w):
                        v = e.bmp[xx, yy]
                        if v in e.pal.transp: continue
                        if e.pal[v] and 0 <= e.x+xx < 128 and 0 <= e.y+yy < 64: img.putpixel((e.x+xx, e.y+yy), 255)
            elif isinstance(e, Rectangle):
                for yy in range(e.height):
                    for xx in range(e.width):
                        if 0 <= e.x+xx < 128 and 0 <= e.y+yy < 64: img.putpixel((e.x+xx, e.y+yy), 255)
            elif isinstance(e, Label) and e.text:
                w = e.font.width(e.text); h = e.font.ascent + e.font.descent
                ax, ay = e.anchor_point; px, py = e.anchored_position
                x0 = int(round(px - ax * w)); top = int(round(py - ay * h))
                e.font.draw(img, x0, top + e.font.ascent - 1, e.text, 255 if e.color else 0)
    walk(DISPLAY["d"].root_group)
    d = DISPLAY["d"]
    if not d.awake: img = Image.new("L", (128, 64), 0)
    elif d.brightness < 0.5: img = img.point(lambda p: p * 0.25)
    return img.resize((128 * scale, 64 * scale), Image.NEAREST)

def run(code_path, scenario):
    global SCENARIO
    SCENARIO = scenario
    src = open(code_path).read()
    try:
        exec(compile(src, code_path, "exec"), {"__name__": "__main__"})
    except Stop:
        pass
