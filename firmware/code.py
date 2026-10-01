# MakerDash – Firmware für Raspberry Pi Pico 2 (CircuitPython 9/10)
#
# Diese Datei wird von der App "MakerDash" installiert und aktualisiert.
# Einstellungen (Pins usw.) stehen in settings.toml und werden in der App geändert.

FW_VERSION = "2.2.0"
PROTO = 2
BOOT_MARKER = "DASHBOOT 2"

import os
import math
import time
import board
import busio
import displayio
import i2cdisplaybus
import terminalio
import rotaryio
import keypad
import usb_cdc
import supervisor
import microcontroller
import vectorio
from analogio import AnalogIn
import adafruit_displayio_ssd1306
from adafruit_display_text import bitmap_label
from adafruit_bitmap_font import bitmap_font


# ======================= Einstellungen (settings.toml) =======================

def cfg_str(name, standard):
    try:
        v = os.getenv(name)
    except Exception:
        v = None
    return standard if v is None else str(v)


def cfg_int(name, standard):
    try:
        return int(cfg_str(name, str(standard)))
    except ValueError:
        return standard


def cfg_pin(name, standard):
    return getattr(board, cfg_str(name, standard), getattr(board, standard))


PIN_FADER = (cfg_pin("DASH_FADER1", "GP28"),
             cfg_pin("DASH_FADER2", "GP27"),
             cfg_pin("DASH_FADER3", "GP26"))
FADER_INVERT = cfg_str("DASH_FADER_INVERT", "000")
PIN_ENC_A = cfg_pin("DASH_ENC_A", "GP10")
PIN_ENC_B = cfg_pin("DASH_ENC_B", "GP11")
PIN_ENC_SW = cfg_pin("DASH_ENC_SW", "GP12")
ENC_DIVISOR = cfg_int("DASH_ENC_DIVISOR", 4)
ENC_REVERSE = cfg_int("DASH_ENC_REVERSE", 0)
PIN_SDA = cfg_pin("DASH_SDA", "GP4")
PIN_SCL = cfg_pin("DASH_SCL", "GP5")
I2C_FREQ = cfg_int("DASH_I2C_FREQ", 400000)      # SSD1306 schafft 400 kHz (Fast Mode)
ANIMATION = cfg_int("DASH_ANIMATION", 1)

# Laufzeit-Einstellungen (werden von der App per CFG aktualisiert)
helligkeit = cfg_int("DASH_BRIGHTNESS", 100) / 100
schoner_sek = cfg_int("DASH_SAVER", 300)          # 0 = aus
overlay_an = cfg_int("DASH_OVERLAY", 1)

UNTEN_SCHWELLE = 15         # Fader 3 gilt als "unten" bei Wert <= 15 (von 1000)
BLINK_SEKUNDEN = 0.4
MENU_TIMEOUT = 15
PC_TIMEOUT = 8
OVERLAY_SEK = 1.5
LANG_SEK = 0.7             # so lange gedrückt halten = Stummschalten
AKTIV_SCHWELLE = 15         # so viel muss sich ein Fader bewegen, um als Bedienung zu zählen

# ======================= Hardware =======================

ser = usb_cdc.data
if ser is not None:
    ser.timeout = 0
    ser.write_timeout = 0.05


def senden(zeile):
    if ser is None or not ser.connected:
        return
    try:
        ser.write((zeile + "\n").encode("utf-8"))
    except Exception:
        pass


def boot_ok():
    try:
        with open("/boot_out.txt") as f:
            return BOOT_MARKER in f.read()
    except OSError:
        return False


BOOT_OK = boot_ok()


def boot_pruefen():
    """Nach einem Update ist die neue boot.py erst nach einem Reset aktiv.
    Einmal selbst neu starten; ein Merker im NVM verhindert eine Endlosschleife."""
    try:
        nvm = microcontroller.nvm
    except AttributeError:
        return
    if BOOT_OK:
        if nvm[0] != 0:
            nvm[0] = 0
        return
    try:
        with open("/boot.py") as f:
            neue_boot = BOOT_MARKER in f.read()
    except OSError:
        neue_boot = False
    if neue_boot and nvm[0] != 0xB2:
        nvm[0] = 0xB2
        time.sleep(0.5)
        microcontroller.reset()


boot_pruefen()


class Fader:
    """Liest einen Fader geglättet aus und liefert Werte 0–1000."""

    def __init__(self, pin, invert):
        self.adc = AnalogIn(pin)
        self.invert = invert
        self.gefiltert = self._roh()
        self.wert = self._skaliert(self.gefiltert)
        self.aktiv_wert = self.wert

    def _roh(self):
        s = 0
        for _ in range(8):
            s += self.adc.value
        return s / 8

    def _skaliert(self, roh):
        x = (roh - 400) / (65535 - 800)
        x = max(0.0, min(1.0, x))
        if self.invert:
            x = 1.0 - x
        return int(x * 1000 + 0.5)

    def lesen(self):
        """Neuer Wert bei Änderung, sonst None."""
        self.gefiltert += (self._roh() - self.gefiltert) * 0.3
        neu = self._skaliert(self.gefiltert)
        if abs(neu - self.wert) >= 4 or (neu != self.wert and neu in (0, 1000)):
            self.wert = neu
            return neu
        return None

    def bewegt(self):
        """True, wenn der Fader deutlich bewegt wurde (für Anzeige/Schoner)."""
        if abs(self.wert - self.aktiv_wert) >= AKTIV_SCHWELLE or (
                self.wert != self.aktiv_wert and self.wert in (0, 1000)):
            self.aktiv_wert = self.wert
            return True
        return False


fader = [Fader(PIN_FADER[i], FADER_INVERT[i:i + 1] == "1") for i in range(3)]

if ENC_REVERSE:
    PIN_ENC_A, PIN_ENC_B = PIN_ENC_B, PIN_ENC_A
encoder = rotaryio.IncrementalEncoder(PIN_ENC_A, PIN_ENC_B, divisor=ENC_DIVISOR)
letzte_pos = encoder.position
taster = keypad.Keys((PIN_ENC_SW,), value_when_pressed=False, pull=True)

# ======================= Display =======================
displayio.release_displays()
i2c = busio.I2C(PIN_SCL, PIN_SDA, frequency=I2C_FREQ)
bus = i2cdisplaybus.I2CDisplayBus(i2c, device_address=0x3C)
display = adafruit_displayio_ssd1306.SSD1306(bus, width=128, height=64)

WEISS = 0xFFFFFF
SCHWARZ = 0x000000
FONT_MENU = bitmap_font.load_font("/fonts/menu.bdf")
FONT_GROSS = bitmap_font.load_font("/fonts/big.bdf")
# BDF-Schriften laden Zeichen sonst erst bei Bedarf – jedes neue Zeichen durchsucht dann die
# ganze Datei. Das machte das Menü träge; deshalb nach der Start-Animation alles vorladen.
ZEICHEN = "".join(chr(c) for c in range(32, 127)) + "äöüÄÖÜß°"

PAL_WEISS = displayio.Palette(1)
PAL_WEISS[0] = WEISS


def helligkeit_setzen(wert):
    try:
        display.brightness = max(0.02, min(1.0, wert))
    except Exception:
        pass


helligkeit_setzen(helligkeit)


def rechteck(w, h, x, y):
    return vectorio.Rectangle(pixel_shader=PAL_WEISS, width=w, height=h, x=x, y=y)


def icon(muster, x, y):
    bmp = displayio.Bitmap(len(muster[0]), len(muster), 2)
    for yy, zeile in enumerate(muster):
        for xx, c in enumerate(zeile):
            bmp[xx, yy] = 1 if c == "1" else 0
    pal = displayio.Palette(2)
    pal[0] = SCHWARZ
    pal[1] = WEISS
    pal.make_transparent(0)
    return displayio.TileGrid(bmp, pixel_shader=pal, x=x, y=y)


ICON_LAUTSPRECHER = ("000001000", "000011000", "011111010", "011111001",
                     "011111001", "011111010", "000011000", "000001000")


# bitmap_label zeichnet ein Bitmap pro Text statt einer Kachel pro Buchstabe – deutlich schneller.
# Bei BDF-Schriften hält es aber die Schrifthöhe für die Oberlänge und zentriert auf einen vom Text
# abhängigen Kasten: Texte rutschten 6–9 Pixel nach unten. Mit Anker oben (y = 0) liegt die Schrift
# dagegen immer gleich; diese Tabelle rechnet darauf um, sodass alles wie mit label.Label sitzt.
# Werte je Schrift: (Höhe der Großbuchstaben, Abstand Ankerpunkt -> Oberkante Großbuchstaben)
KORREKTUR = {id(FONT_MENU): (9, 8), id(FONT_GROSS): (13, 9)}
_anker_y = {}


def _y_korrigiert(font, ay, y):
    k = KORREKTUR.get(id(font))
    if k is None:
        return y
    return y - int(ay * k[0]) - k[1]


def text(font, t, anker, pos, farbe=WEISS):
    ax, ay = anker
    bdf = id(font) in KORREKTUR
    lbl = bitmap_label.Label(font, text=t, color=farbe, anchor_point=(ax, 0) if bdf else anker,
                             anchored_position=(pos[0], _y_korrigiert(font, ay, pos[1])))
    _anker_y[id(lbl)] = (ay, pos[1])
    return lbl


def platzieren(lbl, x, y):
    """Wie anchored_position = (x, y), aber mit der Korrektur für BDF-Schriften."""
    ay = _anker_y[id(lbl)][0]
    _anker_y[id(lbl)] = (ay, y)
    lbl.anchored_position = (x, _y_korrigiert(lbl.font, ay, y))


def schrift_setzen(lbl, font, t):
    """Schrift und Text ändern; die Höhe wird für die neue Schrift neu berechnet."""
    if lbl.font is not font:
        lbl.font = font
        ay, y = _anker_y[id(lbl)]
        lbl.anchored_position = (lbl.anchored_position[0], _y_korrigiert(font, ay, y))
    if lbl.text != t:
        lbl.text = t


# --- Startbild ---
start = displayio.Group()
lbl_titel = text(terminalio.FONT, "Fader 3", (0, 0), (0, 0))
lbl_pc = text(terminalio.FONT, "", (1, 0), (127, 0))
# Stumm-Anzeige oben rechts: schwarze Schrift auf weißem Feld
mute_bg = rechteck(1, 11, 0, 0)
mute_bg.hidden = True
lbl_mute = text(terminalio.FONT, "", (1, 0), (125, 0), farbe=SCHWARZ)
lbl_name = text(FONT_GROSS, "", (0, 0.5), (0, 27))
ico_ausgabe = icon(ICON_LAUTSPRECHER, 0, 51)
lbl_ausgabe = text(FONT_MENU, "", (0, 0.5), (12, 55))
lbl_hinweis = text(terminalio.FONT, "", (0.5, 0.5), (64, 55))
for teil in (lbl_titel, lbl_pc, mute_bg, lbl_mute, lbl_name, rechteck(128, 1, 0, 44),
             ico_ausgabe, lbl_ausgabe, lbl_hinweis):
    start.append(teil)

# --- Menü (4 Zeilen, markierte Zeile invertiert) ---
ZEILEN = 4
ZEILENHOEHE = 16
menue = displayio.Group()
balken = rechteck(128, ZEILENHOEHE, 0, 0)
menue.append(balken)
menue_labels = []
menue_pfeile = []
for i in range(ZEILEN):
    mitte = i * ZEILENHOEHE + ZEILENHOEHE // 2
    l = text(FONT_MENU, "", (0, 0.5), (3, mitte))
    p = text(FONT_MENU, "", (1, 0.5), (126, mitte))
    menue.append(l)
    menue.append(p)
    menue_labels.append(l)
    menue_pfeile.append(p)

# --- Lautstärke-Anzeige ---
overlay = displayio.Group()
ov_name = text(FONT_MENU, "", (0.5, 0.5), (64, 8))
ov_wert = text(FONT_GROSS, "", (0.5, 0.5), (64, 30))
ov_rahmen = displayio.Group()
for r in (rechteck(120, 1, 4, 48), rechteck(120, 1, 4, 57), rechteck(1, 10, 4, 48),
          rechteck(1, 10, 123, 48)):
    ov_rahmen.append(r)
ov_fuell = rechteck(1, 6, 6, 50)
for teil in (ov_name, ov_wert, ov_rahmen, ov_fuell):
    overlay.append(teil)

# --- Update-Bildschirm ---
update_bild = displayio.Group()
upd_titel = text(FONT_MENU, "Update", (0.5, 0.5), (64, 12))
upd_text = text(terminalio.FONT, "Wird installiert ...", (0.5, 0.5), (64, 30))
upd_fuell = rechteck(1, 6, 6, 50)
for teil in (upd_titel, upd_text, rechteck(120, 1, 4, 48), rechteck(120, 1, 4, 57),
             rechteck(1, 10, 4, 48), rechteck(1, 10, 123, 48), upd_fuell):
    update_bild.append(teil)


# --- Bildschirmschoner: MakerDash-Logo wandert langsam über das Display ---
schoner = displayio.Group()
sch_knoepfe = []
for i in range(3):
    schoner.append(rechteck(1, 15, 2 + i * 6, 1))
    k = rechteck(5, 3, i * 6, 7)
    schoner.append(k)
    sch_knoepfe.append(k)
schoner.append(text(FONT_MENU, "MakerDash", (0, 0.5), (19, 8)))
SCH_W, SCH_H = 19 + 73, 17
sch_x, sch_y = 10.0, 20.0
sch_dx, sch_dy = 9.0, 5.0             # Pixel pro Sekunde
sch_zeit = 0.0


def schoner_schritt(t):
    """Bewegt das Logo (ca. 20 Bilder/s) und lässt die Mini-Fader wippen."""
    global sch_x, sch_y, sch_dx, sch_dy, sch_zeit
    dt = t - sch_zeit
    if dt < 0.05:
        return
    sch_zeit = t
    dt = min(dt, 0.2)
    sch_x += sch_dx * dt
    sch_y += sch_dy * dt
    if sch_x < 0 or sch_x > 128 - SCH_W:
        sch_dx = -sch_dx
        sch_x = max(0.0, min(128.0 - SCH_W, sch_x))
    if sch_y < 0 or sch_y > 64 - SCH_H:
        sch_dy = -sch_dy
        sch_y = max(0.0, min(64.0 - SCH_H, sch_y))
    schoner.x = int(sch_x)
    schoner.y = int(sch_y)
    for i, k in enumerate(sch_knoepfe):
        k.y = int(7 + 6 * math.sin(t * 1.6 + i * 2.1))


def start_animation():
    """Kurzes Logo beim Einschalten: drei Fader schieben sich in Position."""
    g = displayio.Group()
    spuren_x = (12, 24, 36)
    knoepfe = []
    for x in spuren_x:
        g.append(rechteck(1, 48, x, 8))
        k = rechteck(9, 5, x - 4, 51)
        g.append(k)
        knoepfe.append(k)
    t_gross = text(FONT_GROSS, "Maker", (0, 0.5), (128, 20))
    t_klein = text(FONT_GROSS, "Dash", (0, 0.5), (128, 42))
    t_ver = text(terminalio.FONT, "v" + FW_VERSION, (1, 1), (127, 64))
    t_ver.hidden = True
    for t in (t_gross, t_klein, t_ver):
        g.append(t)
    display.root_group = g
    ziele = (0.25, 0.7, 0.45)
    t0 = time.monotonic()
    dauer = 1.6
    while True:
        t = time.monotonic() - t0
        if t > dauer:
            break
        for i, k in enumerate(knoepfe):
            p = max(0.0, min(1.0, (t - i * 0.12) / 0.7))
            p = 1 - (1 - p) ** 3                      # sanft abbremsen
            k.y = int(51 - p * ziele[i] * 43)
        p = max(0.0, min(1.0, (t - 0.35) / 0.6))
        p = 1 - (1 - p) ** 3
        x = int(128 - p * (128 - 50))
        platzieren(t_gross, x, 20)
        p2 = max(0.0, min(1.0, (t - 0.5) / 0.6))
        p2 = 1 - (1 - p2) ** 3
        platzieren(t_klein, int(128 - p2 * (128 - 50)), 42)
        t_ver.hidden = t < 1.0
        time.sleep(0.015)
    return time.monotonic()


def schriften_vorladen():
    for f in (FONT_MENU, FONT_GROSS):
        try:
            f.load_glyphs(ZEICHEN)
        except Exception:
            pass


if ANIMATION:
    ende = start_animation()
    schriften_vorladen()                       # läuft, während das Logo noch steht
    rest = 0.5 - (time.monotonic() - ende)
    if rest > 0:
        time.sleep(rest)
else:
    schriften_vorladen()

# ======================= Zustand =======================
apps = []
ausgaben = []
ausgabe_idx = -1
mikros = []
mikro_idx = -1
f3_name = "-"
namen = ["Fader 1", "Fader 2", "Fader 3"]
gesperrt = False
letzte_rx = -100.0
modus = "START"         # START, HAUPT, PROG, AUSGABE, MIKRO
cursor = 0
scroll = 0
jetzt = time.monotonic()
letzte_eingabe = jetzt
letzte_aktivitaet = jetzt
overlay_bis = 0.0
schlaeft = False
gedimmt = False
update_laeuft = False
rx_puffer = b""
mute_an = False
mute_name = ""
mute_bekannt = False        # erst nach der ersten Meldung vom PC Änderungen einblenden
taste_seit = None           # Zeitpunkt, seit dem der Taster gedrückt ist
taste_lang = False


def pc_da():
    return ser is not None and ser.connected and time.monotonic() - letzte_rx < PC_TIMEOUT


def eintraege(m=None):
    """Liste aus (Text, Pfeil, Wert)."""
    modus_ = m or modus
    if modus_ == "HAUPT":
        return [("Zurück", False, None), ("Fader 3", True, None),
                ("Ausgabe", True, None), ("Mikrofon", True, None)]
    liste = [("Zurück", False, None)]
    if not pc_da():
        return liste + [("(kein PC)", False, None)]
    if modus_ == "PROG":
        if not apps:
            liste.append(("(kein Ton aktiv)", False, None))
        for n in apps:
            liste.append((("* " if n == f3_name else "  ") + n, False, n))
    elif modus_ == "AUSGABE":
        for i, n in enumerate(ausgaben):
            liste.append((("* " if i == ausgabe_idx else "  ") + n, False, n))
    elif modus_ == "MIKRO":
        if not mikros:
            liste.append(("(kein Mikrofon)", False, None))
        for i, n in enumerate(mikros):
            liste.append((("* " if i == mikro_idx else "  ") + n, False, n))
    return liste


def menue_zeichnen():
    global scroll, cursor
    liste = eintraege()
    cursor = max(0, min(cursor, len(liste) - 1))
    if cursor < scroll:
        scroll = cursor
    elif cursor >= scroll + ZEILEN:
        scroll = cursor - ZEILEN + 1
    scroll = max(0, min(scroll, max(0, len(liste) - ZEILEN)))
    for zeile in range(ZEILEN):
        idx = scroll + zeile
        t, pfeil, _ = liste[idx] if idx < len(liste) else ("", False, None)
        farbe = SCHWARZ if idx == cursor else WEISS
        # nur Geändertes anfassen – jede Änderung kostet Rechenzeit und Übertragung
        lbl, pf = menue_labels[zeile], menue_pfeile[zeile]
        if lbl.text != t:
            lbl.text = t
        if lbl.color != farbe:
            lbl.color = farbe
        p = ">" if pfeil else ""
        if pf.text != p:
            pf.text = p
        if pf.color != farbe:
            pf.color = farbe
    y = (cursor - scroll) * ZEILENHOEHE
    if balken.y != y:
        balken.y = y


def gross_setzen(lbl, t):
    schrift_setzen(lbl, FONT_GROSS, t)
    if lbl.bounding_box[2] > 124:
        schrift_setzen(lbl, FONT_MENU, t)


def start_zeichnen():
    if lbl_name.text != f3_name:
        gross_setzen(lbl_name, f3_name)
    da = pc_da()
    lbl_pc.text = "" if da else "kein PC"
    m = ""
    if da and mute_an:
        m = mute_name + " aus"
        if len(m) > 13:
            m = "STUMM"
    if lbl_mute.text != m:
        lbl_mute.text = m
        if m:
            w = lbl_mute.bounding_box[2]
            mute_bg.x = 125 - w - 2
            mute_bg.width = w + 4
    mute_bg.hidden = not m
    aus = ausgaben[ausgabe_idx] if 0 <= ausgabe_idx < len(ausgaben) else "-"
    if lbl_ausgabe.text != aus:
        lbl_ausgabe.text = aus


def bild_zeigen():
    """Zeigt das zum Zustand passende Bild."""
    if schlaeft and not update_laeuft:
        display.root_group = schoner
    elif update_laeuft:
        display.root_group = update_bild
    elif time.monotonic() < overlay_bis:
        display.root_group = overlay
    elif modus == "START":
        start_zeichnen()
        display.root_group = start
    else:
        menue_zeichnen()
        display.root_group = menue


def gehe_zu(neuer_modus, neuer_cursor=0):
    global modus, cursor, scroll, overlay_bis
    modus = neuer_modus
    cursor = neuer_cursor
    scroll = 0
    overlay_bis = 0
    bild_zeigen()


def cursor_auf(ziel_modus, name, standard=1):
    for i, (_, _, wert) in enumerate(eintraege(ziel_modus)):
        if wert is not None and wert == name:
            return i
    return standard


def overlay_zeigen(i):
    global overlay_bis
    if not overlay_an or update_laeuft:
        return
    ov_name.text = namen[i]
    if i == 2 and gesperrt:
        schrift_setzen(ov_wert, FONT_MENU, "ganz runter!")
    else:
        schrift_setzen(ov_wert, FONT_GROSS, "%d %%" % ((fader[i].wert + 5) // 10))
    ov_fuell.width = max(1, fader[i].wert * 116 // 1000)
    ov_rahmen.hidden = False
    ov_fuell.hidden = False
    overlay_bis = time.monotonic() + OVERLAY_SEK
    if display.root_group is not overlay:
        display.root_group = overlay


def overlay_text(name, wert):
    """Einblendung ohne Balken, z. B. "Mikrofon / stumm"."""
    global overlay_bis
    if not overlay_an or update_laeuft:
        return
    ov_name.text = name
    schrift_setzen(ov_wert, FONT_GROSS, wert)
    ov_rahmen.hidden = True
    ov_fuell.hidden = True
    overlay_bis = time.monotonic() + OVERLAY_SEK
    if display.root_group is not overlay:
        display.root_group = overlay


def aufwachen():
    """Bildschirmschoner beenden. Gibt True zurück, wenn der Bildschirm aus war."""
    global schlaeft, gedimmt, letzte_aktivitaet
    letzte_aktivitaet = time.monotonic()
    war_aus = schlaeft
    if schlaeft:
        schlaeft = False
        bild_zeigen()
    if gedimmt:
        helligkeit_setzen(helligkeit)
        gedimmt = False
    return war_aus


def taster_gedrueckt():
    global f3_name, gesperrt, ausgabe_idx, mikro_idx
    if modus == "START":
        gehe_zu("HAUPT", 1)
        return
    if modus == "HAUPT":
        if cursor == 0:
            gehe_zu("START")
        elif cursor == 1:
            gehe_zu("PROG", cursor_auf("PROG", f3_name))
        elif cursor == 2:
            aktiv = ausgaben[ausgabe_idx] if 0 <= ausgabe_idx < len(ausgaben) else None
            gehe_zu("AUSGABE", cursor_auf("AUSGABE", aktiv))
        else:
            aktiv = mikros[mikro_idx] if 0 <= mikro_idx < len(mikros) else None
            gehe_zu("MIKRO", cursor_auf("MIKRO", aktiv))
        return
    _, _, wert = eintraege()[cursor]
    if cursor == 0:
        gehe_zu("HAUPT", {"PROG": 1, "AUSGABE": 2, "MIKRO": 3}[modus])
    elif wert is None:
        pass
    elif modus == "PROG":
        if wert != f3_name:
            f3_name = wert
            namen[2] = wert
            gesperrt = True
        senden("SETAPP\t" + wert)
        gehe_zu("START")
    elif modus == "AUSGABE":
        if wert in ausgaben:
            ausgabe_idx = ausgaben.index(wert)
        senden("SETOUT\t" + wert)
        gehe_zu("START")
    elif modus == "MIKRO":
        if wert in mikros:
            mikro_idx = mikros.index(wert)
        senden("SETIN\t" + wert)
        gehe_zu("START")


def fader_senden(i, wert):
    senden("F\t%d\t%d" % (i + 1, wert))


def cfg_anwenden(teile):
    global helligkeit, schoner_sek, overlay_an
    for k in range(0, len(teile) - 1, 2):
        schluessel, wert = teile[k], teile[k + 1]
        try:
            if schluessel == "brightness":
                helligkeit = int(wert) / 100
                if not gedimmt:
                    helligkeit_setzen(helligkeit)
            elif schluessel == "saver":
                schoner_sek = int(wert)
            elif schluessel == "overlay":
                overlay_an = int(wert)
        except ValueError:
            pass


def zeile_verarbeiten(zeile):
    global apps, ausgaben, ausgabe_idx, mikros, mikro_idx, f3_name, letzte_rx
    global gesperrt, namen, update_laeuft, mute_an, mute_name, mute_bekannt
    letzte_rx = time.monotonic()
    teile = zeile.split("\t")
    befehl = teile[0]
    if befehl == "PING":
        senden("PONG\tDASH\t%d\t%s\t%d" % (PROTO, FW_VERSION, 1 if BOOT_OK else 0))
        return
    if befehl == "SYNC":
        for i in range(3):
            if i == 2 and gesperrt:
                continue
            fader_senden(i, fader[i].wert)
        return
    if befehl == "APPS":
        apps = teile[1:]
    elif befehl in ("OUTS", "INS"):
        try:
            idx = int(teile[1])
        except (IndexError, ValueError):
            idx = -1
        if befehl == "OUTS":
            ausgabe_idx, ausgaben = idx, teile[2:]
        else:
            mikro_idx, mikros = idx, teile[2:]
    elif befehl == "F3" and len(teile) > 1:
        if len(teile) > 2 and teile[2] == "1" and teile[1] != f3_name:
            gesperrt = True
        f3_name = teile[1]
        if len(namen) == 3:
            namen[2] = f3_name
    elif befehl == "MUTE" and len(teile) > 2:
        neu = teile[1] == "1"
        umgeschaltet = mute_bekannt and neu != mute_an
        mute_an, mute_name, mute_bekannt = neu, teile[2], True
        if umgeschaltet and not schlaeft:
            overlay_text(mute_name, "stumm" if neu else "an")
            return
    elif befehl == "LABELS" and len(teile) >= 4:
        namen = teile[1:4]
        return
    elif befehl == "CFG":
        cfg_anwenden(teile[1:])
        return
    elif befehl == "UPDATING":
        update_laeuft = True
        try:
            supervisor.runtime.autoreload = False
        except Exception:
            pass
        aufwachen()
        upd_fuell.width = 1
        bild_zeigen()
        return
    elif befehl == "PROGRESS" and len(teile) > 1:
        try:
            upd_fuell.width = max(1, min(116, int(teile[1]) * 116 // 100))
        except ValueError:
            pass
        return
    elif befehl == "UPDATE_ABORT":
        update_laeuft = False
        try:
            supervisor.runtime.autoreload = True
        except Exception:
            pass
        bild_zeigen()
        return
    elif befehl == "REBOOT":
        upd_text.text = "Neustart ..."
        time.sleep(0.3)
        microcontroller.reset()
    else:
        return
    if update_laeuft:
        return
    if display.root_group is start:
        start_zeichnen()
    elif display.root_group is menue:
        menue_zeichnen()


def empfangen():
    global rx_puffer
    if ser is None:
        return
    n = ser.in_waiting
    if not n:
        return
    rx_puffer += ser.read(n)
    while b"\n" in rx_puffer:
        roh, rx_puffer = rx_puffer.split(b"\n", 1)
        try:
            zeile = roh.decode("utf-8").strip()
        except UnicodeError:
            continue
        if zeile:
            zeile_verarbeiten(zeile)
    if len(rx_puffer) > 8192:
        rx_puffer = b""


gehe_zu("START")
war_pc_da = False

# ======================= Hauptschleife =======================
while True:
    jetzt = time.monotonic()
    empfangen()

    # Fader
    for i in range(3):
        v = fader[i].lesen()
        if fader[i].bewegt():
            aufwachen()
            overlay_zeigen(i)
        if i == 2 and gesperrt:
            if fader[2].wert <= UNTEN_SCHWELLE:
                gesperrt = False
                fader_senden(2, fader[2].wert)
                if display.root_group is overlay:
                    overlay_zeigen(2)
            continue
        if v is not None:
            fader_senden(i, v)

    # Encoder drehen
    pos = encoder.position
    if pos != letzte_pos:
        schritte = pos - letzte_pos
        letzte_pos = pos
        letzte_eingabe = jetzt
        if not aufwachen() and not update_laeuft:
            if overlay_bis:
                overlay_bis = 0
                bild_zeigen()
            elif modus != "START":
                neu = max(0, min(len(eintraege()) - 1, cursor + schritte))
                if neu != cursor:
                    cursor = neu
                    menue_zeichnen()

    # Taster: kurz = auswählen (beim Loslassen), lang = stummschalten
    ereignis = taster.events.get()
    if ereignis:
        letzte_eingabe = jetzt
        if ereignis.pressed:
            if aufwachen() or update_laeuft:
                taste_seit = None              # Druck hat nur aufgeweckt
            else:
                taste_seit = jetzt
                taste_lang = False
        elif taste_seit is not None:
            if not taste_lang:
                if overlay_bis:
                    overlay_bis = 0
                taster_gedrueckt()
            taste_seit = None
    if taste_seit is not None and not taste_lang and jetzt - taste_seit >= LANG_SEK:
        taste_lang = True
        letzte_aktivitaet = jetzt
        senden("MUTE")

    # Lautstärke-Anzeige ausblenden
    if overlay_bis and jetzt >= overlay_bis:
        overlay_bis = 0
        bild_zeigen()

    # Menü-Timeout
    if modus != "START" and jetzt - letzte_eingabe > MENU_TIMEOUT:
        gehe_zu("START")

    # Verbindungsanzeige
    da = pc_da()
    if da != war_pc_da:
        war_pc_da = da
        if not update_laeuft and not overlay_bis:
            bild_zeigen()

    # Hinweis blinken lassen (ersetzt die Ausgabezeile)
    if display.root_group is start:
        if gesperrt:
            an = int(jetzt / BLINK_SEKUNDEN) % 2 == 0
            soll = "Regler ganz runter!" if an else ""
            if lbl_hinweis.text != soll:
                lbl_hinweis.text = soll
            ico_ausgabe.hidden = True
            lbl_ausgabe.hidden = True
        elif lbl_hinweis.text or ico_ausgabe.hidden:
            lbl_hinweis.text = ""
            ico_ausgabe.hidden = False
            lbl_ausgabe.hidden = False

    # Bildschirmschoner: gedimmtes, wanderndes Logo (schont das OLED, sieht aber nicht "aus" aus)
    if schoner_sek > 0 and not update_laeuft:
        if schlaeft:
            schoner_schritt(jetzt)
        elif jetzt - letzte_aktivitaet > schoner_sek:
            schlaeft = True
            overlay_bis = 0
            helligkeit_setzen(max(0.02, helligkeit * 0.4))
            gedimmt = True
            schoner_schritt(jetzt)
            bild_zeigen()
    elif schlaeft and not update_laeuft:
        aufwachen()                            # Schoner wurde in der App ausgeschaltet

    time.sleep(0.005)
