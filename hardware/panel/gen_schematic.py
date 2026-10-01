#!/usr/bin/env python3
"""Erzeugt den Schaltplan der MakerDash-Panel-Platine (KiCad 7).

Alle Verbindungen laufen über Netz-Labels bzw. Power-Symbole direkt an den Pins – dadurch bleibt
der Plan übersichtlich und das Skript einfach. Aufruf: python3 gen_schematic.py
Danach: kicad-cli sch erc makerdash-panel.kicad_sch
"""
import os
import re
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
SYMDIR = "/usr/share/kicad/symbols"
NAME = "makerdash-panel"
ROOT = str(uuid.uuid5(uuid.NAMESPACE_URL, "makerdash-panel-root"))
_n = [0]


def uid():
    _n[0] += 1
    return str(uuid.uuid5(uuid.NAMESPACE_URL, "makerdash-panel-%d" % _n[0]))


# ---------------------------------------------------------------- Bibliothek lesen

def _block(text, start):
    """Liefert den geklammerten Ausdruck ab Index start."""
    depth = 0
    for i in range(start, len(text)):
        c = text[i]
        if c == '"':
            # Zeichenkette überspringen
            j = i + 1
            while text[j] != '"':
                j += 2 if text[j] == "\\" else 1
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                return text[start:i + 1]
    raise ValueError("unvollständig")


def _raw_symbol(lib, name):
    text = open(os.path.join(SYMDIR, lib + ".kicad_sym"), encoding="utf-8").read()
    i = text.index('(symbol "%s"' % name)
    return _block(text, i)


def load_symbol(lib, name, alias=None, props=None):
    """Symboldefinition für lib_symbols, mit aufgelöstem "extends" und neuem Namen."""
    raw = _raw_symbol(lib, name)
    m = re.search(r'\(extends "([^"]+)"\)', raw[:300])
    own_props = dict(re.findall(r'\(property "([^"]+)" "([^"]*)"', raw))
    if m:
        base = m.group(1)
        body = _raw_symbol(lib, base)
        body = body.replace('(symbol "%s_' % base, '(symbol "%s_' % name)
        body = body.replace('(symbol "%s"' % base, '(symbol "%s"' % name, 1)
        for k, v in own_props.items():
            body = re.sub(r'(\(property "%s" )"[^"]*"' % re.escape(k), lambda mm: mm.group(1) + '"%s"' % v, body, count=1)
    else:
        body = raw
    new = alias or name
    body = body.replace('(symbol "%s_' % name, '(symbol "%s_' % new)
    body = body.replace('(symbol "%s"' % name, '(symbol "%s:%s"' % ("MakerDash" if alias else lib, new), 1)
    for k, v in (props or {}).items():
        body = re.sub(r'(\(property "%s" )"[^"]*"' % re.escape(k), lambda mm: mm.group(1) + '"%s"' % v, body, count=1)
    lib_id = ("MakerDash" if alias else lib) + ":" + new
    pins = []
    for pm in re.finditer(r'\(pin (\w+) \w+ \(at ([-\d.]+) ([-\d.]+) (\d+)\) \(length [\d.]+\)\s*(hide)?', body):
        rest = body[pm.end():pm.end() + 400]
        num = re.search(r'\(number "([^"]+)"', rest).group(1)
        pname = re.search(r'\(name "([^"]*)"', rest).group(1)
        pins.append(dict(num=num, name=pname, type=pm.group(1), x=float(pm.group(2)), y=float(pm.group(3)),
                         ang=int(pm.group(4)), hidden=bool(pm.group(5))))
    return dict(lib_id=lib_id, body=body, pins=pins)


# ---------------------------------------------------------------- Schaltplan

class Sheet:
    def __init__(self):
        self.libs = {}
        self.items = []

    def sym(self, key, lib, name, alias=None, props=None):
        if key not in self.libs:
            self.libs[key] = load_symbol(lib, name, alias, props)
        return self.libs[key]

    def place(self, sym, ref, value, x, y, footprint, nets, hide_ref=False, extra=""):
        """nets: Pinnummer -> Netzname, "NC" (nicht verbunden) oder "PWR:+3V3"/"PWR:GND"."""
        pins = "".join('    (pin "%s" (uuid %s))\n' % (p["num"], uid()) for p in sym["pins"])
        self.items.append(
            '  (symbol (lib_id "%s") (at %.2f %.2f 0) (unit 1)\n'
            '    (in_bom yes) (on_board yes) (dnp no) (uuid %s)\n'
            '    (property "Reference" "%s" (at %.2f %.2f 0) (effects (font (size 1.27 1.27))%s))\n'
            '    (property "Value" "%s" (at %.2f %.2f 0) (effects (font (size 1.27 1.27))))\n'
            '    (property "Footprint" "%s" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '    (property "Datasheet" "~" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '%s%s'
            '    (instances (project "%s" (path "/%s" (reference "%s") (unit 1))))\n'
            '  )\n' % (sym["lib_id"], x, y, uid(), ref, x, y - 2.0 - self._top(sym), " hide" if hide_ref else "",
                       value, x, y + 2.0 + self._bottom(sym), footprint, x, y, x, y, extra, pins, NAME, ROOT, ref))
        for p in sym["pins"]:
            if p["hidden"] and not sym["lib_id"].startswith("power:"):
                continue
            px, py = x + p["x"], y - p["y"]
            net = nets.get(p["num"])
            if net is None:
                if sym["lib_id"].startswith("power:"):
                    continue
                raise ValueError("%s Pin %s (%s) ohne Netz" % (ref, p["num"], p["name"]))
            if net == "NC":
                self.items.append('  (no_connect (at %.2f %.2f) (uuid %s))\n' % (px, py, uid()))
            elif net.startswith("PWR:"):
                # kurzes Leitungsstück, bei benachbarten Pins abwechselnd lang, damit sich nichts überdeckt
                along = p["y"] if p["ang"] in (0, 180) else p["x"]
                stub = 2.54 if round(along / 2.54) % 2 == 0 else 7.62
                dx, dy = {0: (-1, 0), 180: (1, 0), 90: (0, 1), 270: (0, -1)}[p["ang"]]
                ex, ey = px + dx * stub, py + dy * stub
                self.wire(px, py, ex, ey)
                self.power(net[4:], ex, ey, p["ang"])
            else:
                self.label(net, px, py, (p["ang"] + 180) % 360)

    @staticmethod
    def _top(sym):
        return max([p["y"] for p in sym["pins"]] + [0])

    @staticmethod
    def _bottom(sym):
        return -min([p["y"] for p in sym["pins"]] + [0])

    def wire(self, x1, y1, x2, y2):
        self.items.append('  (wire (pts (xy %.2f %.2f) (xy %.2f %.2f)) (stroke (width 0) (type default)) (uuid %s))\n'
                          % (x1, y1, x2, y2, uid()))

    def label(self, net, x, y, ang):
        just = "left" if ang in (0, 90) else "right"
        self.items.append('  (label "%s" (at %.2f %.2f %d) (fields_autoplaced)\n'
                          '    (effects (font (size 1.27 1.27)) (justify %s)) (uuid %s))\n' % (net, x, y, ang, just, uid()))

    _pwr_n = [0]

    def power(self, net, x, y, pin_ang):
        """Power-Symbol am Pin: +5V/+3V3 zeigen nach oben, GND nach unten (je nach Pinrichtung gedreht)."""
        Sheet._pwr_n[0] += 1
        lib = {"GND": "GND", "+3V3": "+3V3", "+5V": "+5V"}[net]
        sym = self.sym("pwr_" + lib, "power", lib)
        # Drehung so, dass das Symbol vom Pin weg zeigt
        away = (pin_ang + 180) % 360          # Richtung vom Bauteil weg (Symbolkoordinaten)
        if net == "GND":
            rot = {270: 0, 90: 180, 0: 270, 180: 90}[away]
        else:
            rot = {90: 0, 270: 180, 180: 90, 0: 270}[away]
        ref = "#PWR%03d" % Sheet._pwr_n[0]
        pins = "".join('    (pin "%s" (uuid %s))\n' % (p["num"], uid()) for p in sym["pins"])
        self.items.append(
            '  (symbol (lib_id "%s") (at %.2f %.2f %d) (unit 1)\n'
            '    (in_bom yes) (on_board yes) (dnp no) (uuid %s)\n'
            '    (property "Reference" "%s" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '    (property "Value" "%s" (at %.2f %.2f 0) (effects (font (size 1.0 1.0))))\n'
            '    (property "Footprint" "" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '    (property "Datasheet" "" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '%s'
            '    (instances (project "%s" (path "/%s" (reference "%s") (unit 1))))\n'
            '  )\n' % (sym["lib_id"], x, y, rot, uid(), ref, x, y, net, x, y + (3.2 if net == "GND" else -3.2), x, y, x, y,
                       pins, NAME, ROOT, ref))

    def flag(self, net, x, y):
        """PWR_FLAG mit Label: markiert ein Netz als von außen versorgt."""
        sym = self.sym("flag", "power", "PWR_FLAG")
        Sheet._pwr_n[0] += 1
        ref = "#FLG%02d" % Sheet._pwr_n[0]
        self.items.append(
            '  (symbol (lib_id "%s") (at %.2f %.2f 0) (unit 1)\n'
            '    (in_bom yes) (on_board yes) (dnp no) (uuid %s)\n'
            '    (property "Reference" "%s" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '    (property "Value" "PWR_FLAG" (at %.2f %.2f 0) (effects (font (size 1.0 1.0))))\n'
            '    (property "Footprint" "" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '    (property "Datasheet" "~" (at %.2f %.2f 0) (effects (font (size 1.27 1.27)) hide))\n'
            '    (pin "1" (uuid %s))\n'
            '    (instances (project "%s" (path "/%s" (reference "%s") (unit 1))))\n'
            '  )\n' % (sym["lib_id"], x, y, uid(), ref, x, y, x, y - 3.2, x, y, x, y, uid(), NAME, ROOT, ref))
        self.power(net, x, y, 90 if net == "GND" else 270)

    def text(self, t, x, y, size=2.0, bold=False):
        t = t.replace('"', '\\"').replace("\n", "\\n")
        self.items.append('  (text "%s" (at %.2f %.2f 0)\n    (effects (font (size %.2f %.2f)%s) (justify left top)) (uuid %s))\n'
                          % (t, x, y, size, size, " bold" if bold else "", uid()))

    def rect(self, x1, y1, x2, y2):
        self.items.append('  (rectangle (start %.2f %.2f) (end %.2f %.2f) (stroke (width 0.2) (type dash)) (fill (type none)) (uuid %s))\n'
                          % (x1, y1, x2, y2, uid()))

    def write(self, path):
        libs = "".join("    " + l["body"].replace("\n", "\n    ") + "\n" for l in self.libs.values())
        with open(path, "w", encoding="utf-8") as f:
            f.write('(kicad_sch (version 20230121) (generator eeschema)\n\n  (uuid %s)\n\n  (paper "A3")\n\n' % ROOT)
            f.write('  (title_block\n    (title "MakerDash Panel-Platine")\n    (date "2026-10-01")\n    (rev "1.0")\n'
                    '    (company "MakerDash")\n    (comment 1 "16 Universal-Kanäle an I2C – AVR64DD28 im DIP-Sockel, 3,3 V")\n  )\n\n')
            f.write("  (lib_symbols\n%s  )\n\n" % libs)
            f.write("".join(self.items))
            f.write('\n  (sheet_instances\n    (path "/" (page "1"))\n  )\n)\n')


# ---------------------------------------------------------------- Bauteile & Netze

FP = {
    "R": "Resistor_THT:R_Axial_DIN0207_L6.3mm_D2.5mm_P7.62mm_Horizontal",
    "C": "Capacitor_THT:C_Disc_D5.0mm_W2.5mm_P5.00mm",
    "CP": "Capacitor_THT:CP_Radial_D5.0mm_P2.00mm",
    "LED": "LED_THT:LED_D3.0mm",
    "DIP28": "Package_DIP:DIP-28_W7.62mm_Socket",
    "TO92": "Package_TO_SOT_THT:TO-92_Inline_Wide",
    "XH4": "Connector_JST:JST_XH_B4B-XH-A_1x04_P2.50mm_Vertical",
    "H2": "Connector_PinHeader_2.54mm:PinHeader_1x02_P2.54mm_Vertical",
    "H3": "Connector_PinHeader_2.54mm:PinHeader_1x03_P2.54mm_Vertical",
    "S4": "Connector_PinSocket_2.54mm:PinSocket_1x04_P2.54mm_Vertical",
    "HOLE": "MountingHole:MountingHole_3.2mm_M3",
}

# Universal-Kanäle K1..K16: (Port-Pin, DIP-Pin, Analogeingang)
KANAELE = [  # Reihenfolge nach Lage der Pins auf der Platine (kurze Leiterbahnen)
    ("PD7", 13, "AIN7"), ("PD6", 12, "AIN6"), ("PD5", 11, "AIN5"), ("PD4", 10, "AIN4"),
    ("PD3", 9, "AIN3"), ("PD2", 8, "AIN2"), ("PD1", 7, "AIN1"), ("PC3", 5, "AIN31"),
    ("PC2", 4, "AIN30"), ("PC1", 3, "AIN29"), ("PC0", 2, "AIN28"), ("PA7", 1, "AIN27"),
    ("PA4", 26, "AIN24"), ("PA5", 27, "AIN25"), ("PA6", 28, "AIN26"), ("PA1", 23, "—"),
]


def build():
    s = Sheet()
    R = s.sym("R", "Device", "R")
    C = s.sym("C", "Device", "C")
    CP = s.sym("CP", "Device", "C_Polarized")
    LED = s.sym("LED", "Device", "LED")
    JMP = s.sym("JMP", "Jumper", "Jumper_2_Open")
    C2 = s.sym("C2", "Connector_Generic", "Conn_01x02")
    C3 = s.sym("C3", "Connector_Generic", "Conn_01x03")
    C4 = s.sym("C4", "Connector_Generic", "Conn_01x04")
    LDO = s.sym("LDO", "Regulator_Linear", "MCP1700x-330xxTO")
    HOLE = s.sym("HOLE", "Mechanical", "MountingHole")
    # AVR64DD28 ist pinkompatibel zum AVR DB28 aus der KiCad-Bibliothek (Versorgung, UPDI, Ports)
    MCU = s.sym("MCU", "MCU_Microchip_AVR_Dx", "AVR32DB28x-xSO", alias="AVR64DD28-I_SP",
                props={"Value": "AVR64DD28-I/SP", "Footprint": FP["DIP28"]})

    # --- Stromversorgung -----------------------------------------------------------
    s.text("Bus-Anschluss (zum Pico bzw. zum nächsten Panel)\nJST-XH: 1 GND · 2 +5V (VBUS) · 3 SDA · 4 SCL", 20, 22, 1.6, True)
    bus = {"1": "PWR:GND", "2": "PWR:+5V", "3": "SDA", "4": "SCL"}
    s.place(C4, "J1", "Bus IN", 35, 45, FP["XH4"], bus)
    s.place(C4, "J2", "Bus OUT", 35, 70, FP["XH4"], bus)
    s.flag("+5V", 25, 95)
    s.flag("GND", 45, 95)

    s.text("3,3-V-Regler (aus 5 V vom USB des Pico)", 85, 32, 1.6, True)
    s.place(LDO, "U2", "MCP1700-3302E/TO", 110, 52, FP["TO92"], {"1": "PWR:GND", "2": "PWR:+5V", "3": "PWR:+3V3"})
    s.place(C, "C1", "1µF", 90, 70, FP["C"], {"1": "PWR:+5V", "2": "PWR:GND"})
    s.place(C, "C2", "1µF", 125, 70, FP["C"], {"1": "PWR:+3V3", "2": "PWR:GND"})
    s.place(CP, "C3", "10µF", 138, 70, FP["CP"], {"1": "PWR:+3V3", "2": "PWR:GND"})
    s.place(C2, "J5", "5V AUX", 100, 95, FP["H2"], {"1": "PWR:+5V", "2": "PWR:GND"})
    s.text("J5: 5 V für spätere Erweiterungen\n(z. B. beleuchtete Taster über Transistor)", 108, 92, 1.27)

    # --- Mikrocontroller -------------------------------------------------------------
    s.text("Panel-Controller AVR64DD28 (DIP-28 im Sockel)\nI2C-Target an PA2/PA3, Adresse 0x30 + Jumper", 160, 22, 1.6, True)
    nets = {"14": "PWR:+3V3", "20": "PWR:+3V3", "6": "PWR:+3V3", "15": "PWR:GND", "21": "PWR:GND",
            "24": "SDA", "25": "SCL", "19": "UPDI", "18": "NC", "22": "LED", "16": "ADDR0", "17": "ADDR1"}
    for i, (port, pin, _) in enumerate(KANAELE):
        nets[str(pin)] = "K%d" % (i + 1)
    s.place(MCU, "U1", "AVR64DD28-I/SP", 200, 90, FP["DIP28"], nets)
    for i, (ref, x) in enumerate((("C4", 168), ("C5", 178), ("C6", 188))):
        s.place(C, ref, "100nF", x, 45, FP["C"], {"1": "PWR:+3V3", "2": "PWR:GND"})
    s.text("C4–C6 direkt an Pin 20 (VDD), 14 (AVDD), 6 (VDDIO2)", 160, 34, 1.27)

    # --- I2C, Display, Programmierung, Adresse -------------------------------------------
    s.text("I2C-Pull-ups (nur auf EINER Platine am Bus stecken)", 20, 115, 1.6, True)
    s.place(R, "R1", "4.7k", 35, 135, FP["R"], {"1": "I2C_PU", "2": "SDA"})
    s.place(R, "R2", "4.7k", 50, 135, FP["R"], {"1": "I2C_PU", "2": "SCL"})
    s.place(JMP, "JP3", "Pull-ups", 75, 128, FP["H2"], {"1": "PWR:+3V3", "2": "I2C_PU"})

    s.text("Display (SSD1306, 128×64, I2C 0x3C)\nBuchse: 1 GND · 2 VCC 3,3 V · 3 SCL · 4 SDA\nACHTUNG: bei manchen Modulen sind GND/VCC vertauscht!", 20, 150, 1.6, True)
    s.place(C4, "J3", "OLED", 35, 175, FP["S4"], {"1": "PWR:GND", "2": "PWR:+3V3", "3": "SCL", "4": "SDA"})

    s.text("UPDI (Programmieren)\n1 UPDI · 2 GND · 3 +3V3", 20, 200, 1.6, True)
    s.place(C3, "J4", "UPDI", 35, 222, FP["H3"], {"1": "UPDI", "2": "PWR:GND", "3": "PWR:+3V3"})

    s.text("Adresse: JP1 = +1, JP2 = +2\n(gesteckt = Pin an GND)", 120, 115, 1.6, True)
    s.place(JMP, "JP1", "ADDR0", 135, 135, FP["H2"], {"1": "ADDR0", "2": "PWR:GND"})
    s.place(JMP, "JP2", "ADDR1", 135, 150, FP["H2"], {"1": "ADDR1", "2": "PWR:GND"})

    s.text("Status-LED", 120, 165, 1.6, True)
    s.place(R, "R3", "1k", 125, 180, FP["R"], {"1": "LED", "2": "LED_A"})
    s.place(LED, "D1", "grün", 140, 190, FP["LED"], {"2": "LED_A", "1": "PWR:GND"})

    for i, (ref, x) in enumerate((("H1", 165), ("H2", 175), ("H3", 185), ("H4", 195))):
        s.place(HOLE, ref, "M3", x, 190, FP["HOLE"], {})

    # --- Universal-Kanäle ------------------------------------------------------------------
    s.text("Universal-Kanäle K1–K16: 1 Signal · 2 +3V3 · 3 GND\n"
           "Fader: Schleifer an 1, Enden an 2/3 · Taster: 1 und 3 · Drehgeber: A/B an zwei Kanälen, C an GND\n"
           "K1–K15 auch analog (12 Bit), K16 nur digital", 255, 22, 1.6, True)
    for i, (port, pin, ain) in enumerate(KANAELE):
        col, row = divmod(i, 8)
        x, y = 270 + col * 75, 50 + row * 28
        s.place(C3, "J%d" % (10 + i), "K%d" % (i + 1), x, y, FP["H3"],
                {"1": "K%d" % (i + 1), "2": "PWR:+3V3", "3": "PWR:GND"})
        s.text("%s · Pin %d · %s" % (port, pin, ain), x + 6, y - 1.5, 1.27)
    return s


if __name__ == "__main__":
    s = build()
    s.write(os.path.join(HERE, NAME + ".kicad_sch"))
    pro = os.path.join(HERE, NAME + ".kicad_pro")
    if not os.path.exists(pro):
        open(pro, "w").write('{"meta": {"filename": "%s.kicad_pro", "version": 1}}\n' % NAME)
    print("Schaltplan geschrieben")
