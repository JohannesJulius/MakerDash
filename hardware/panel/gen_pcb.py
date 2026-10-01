#!/usr/bin/env python3
"""Erzeugt die Platine der MakerDash-Panel-Platine (KiCad 7) aus der Netzliste des Schaltplans.

Ablauf: python3 gen_schematic.py
        kicad-cli sch export netlist -o makerdash-panel.net makerdash-panel.kicad_sch
        python3 gen_pcb.py [--route freerouting.jar]
Ohne --route werden nur Bauteile platziert (Luftlinien); mit Freerouting wird geroutet,
danach Masseflächen gefüllt, DRC geprüft und Gerber für die Fertigung exportiert.
"""
import os
import re
import subprocess
import sys

import pcbnew

HERE = os.path.dirname(os.path.abspath(__file__))
NAME = "makerdash-panel"
FPDIR = "/usr/share/kicad/footprints"
MM = pcbnew.FromMM

# Platine 80 × 70 mm, Ursprung oben links bei (100, 100)
X0, Y0, W, H = 100.0, 100.0, 80.0, 70.0


def read_netlist(path):
    s = open(path, encoding="utf-8").read()
    comps = {}
    for m in re.finditer(r'\(comp \(ref "([^"]+)"\)\s*\(value "([^"]*)"\)\s*\(footprint "([^"]*)"\)', s):
        comps[m.group(1)] = (m.group(2), m.group(3))
    nets = {}
    for chunk in s.split("(net (code")[1:]:
        name = re.search(r'\(name "([^"]*)"', chunk).group(1)
        for ref, pin in re.findall(r'\(node \(ref "([^"]+)"\) \(pin "([^"]+)"\)', chunk):
            nets[(ref, pin)] = name.lstrip("/")
    return comps, nets


# Platzierung: Referenz -> (x, y, Drehung in Grad), Koordinaten relativ zur Platine
K_TOP = [(12 + i * 7.62, 6.0, 0) for i in range(8)]          # K1..K8 oben, Pin 1 (Signal) außen
K_BOT = [(12 + i * 7.62, 64.0, 180) for i in range(8)]       # K9..K16 unten, Pin 1 außen
PLACE = {
    "H1": (3.5, 3.5, 0), "H2": (76.5, 3.5, 0), "H3": (3.5, 66.5, 0), "H4": (76.5, 66.5, 0),
    "U1": (56.5, 31.0, 270),                                  # DIP-28 quer (Ursprung = Pin 1 oben rechts)
    "C5": (21.0, 26.0, 0), "C6": (41.0, 26.0, 0), "C4": (35.0, 43.5, 0),
    "J3": (4.0, 18.0, 0), "J4": (4.0, 32.0, 0),               # OLED / UPDI am linken Rand
    "JP1": (13.0, 50.0, 90), "JP2": (21.0, 50.0, 90),        # Adresse unter dem Chip links
    "R3": (44.0, 48.0, 0), "D1": (55.0, 48.0, 0),             # Status-LED
    "J1": (75.5, 21.0, 90), "J2": (75.5, 35.0, 90),           # Bus rein/raus am rechten Rand
    "R1": (60.0, 20.0, 0), "R2": (60.0, 24.0, 0), "JP3": (62.0, 28.5, 0),
    "U2": (66.0, 47.0, 0), "C1": (64.0, 40.5, 0), "C2": (63.0, 53.0, 0), "C3": (74.5, 47.0, 0),
    "J5": (76.0, 53.0, 0),
}
for i in range(8):
    PLACE["J%d" % (10 + i)] = K_TOP[i]
    PLACE["J%d" % (18 + i)] = K_BOT[i]

SILK = [  # (Text, x, y, Größe, Drehung)
    ("MakerDash Panel v1.0", 31.0, 55.5, 1.5, 0),
    ("BUS IN", 70.6, 21.0, 1.0, 90), ("BUS OUT", 70.6, 35.0, 1.0, 90),
    ("1 GND  2 5V", 71.0, 7.0, 0.8, 0), ("3 SDA  4 SCL", 71.0, 8.5, 0.8, 0),
    ("5V", 79.0, 54.3, 0.8, 90), ("PULLUP", 65.6, 29.8, 0.8, 90),
    ("OLED", 4.0, 14.0, 1.0, 0), ("G V C D", 7.0, 21.8, 0.8, 90),
    ("UPDI", 4.0, 29.0, 1.0, 0), ("U G +", 7.0, 34.5, 0.8, 90),
    ("ADR+1", 14.3, 46.0, 0.8, 0), ("ADR+2", 22.3, 46.0, 0.8, 0),
    ("S + -", 9.0, 8.5, 0.8, 90), ("S + -", 9.0, 61.5, 0.8, 90),
]


def load_fp(fpid):
    lib, name = fpid.split(":")
    fp = pcbnew.FootprintLoad(os.path.join(FPDIR, lib + ".pretty"), name)
    if fp is None:
        raise RuntimeError("Footprint fehlt: " + fpid)
    return fp


def add_text(board, text, x, y, size, rot, layer=pcbnew.F_SilkS):
    t = pcbnew.PCB_TEXT(board)
    t.SetText(text)
    t.SetPosition(pcbnew.VECTOR2I(MM(X0 + x), MM(Y0 + y)))
    t.SetLayer(layer)
    t.SetTextSize(pcbnew.VECTOR2I(MM(size), MM(size)))
    t.SetTextThickness(MM(size * 0.15))
    t.SetTextAngleDegrees(rot)
    board.Add(t)


def build():
    comps, pinnets = read_netlist(os.path.join(HERE, NAME + ".net"))
    board = pcbnew.BOARD()
    ds = board.GetDesignSettings()
    ds.SetCopperLayerCount(2)
    ds.m_MinResolvedSpokes = 1          # Massepads hängen zusätzlich an Leiterbahnen

    # Netzklassen: Versorgung breiter
    netinfo = {}
    for name in sorted(set(pinnets.values())):
        n = pcbnew.NETINFO_ITEM(board, name)
        board.Add(n)
        netinfo[name] = n
    ns = ds.m_NetSettings
    dflt = ns.m_DefaultNetClass
    dflt.SetTrackWidth(MM(0.3))
    dflt.SetClearance(MM(0.25))
    dflt.SetViaDiameter(MM(0.8))
    dflt.SetViaDrill(MM(0.4))
    pwr = pcbnew.NETCLASS("Power")
    pwr.SetTrackWidth(MM(0.6))
    pwr.SetClearance(MM(0.25))
    pwr.SetViaDiameter(MM(1.0))
    pwr.SetViaDrill(MM(0.5))
    ns.m_NetClasses["Power"] = pwr

    for ref, (value, fpid) in sorted(comps.items()):
        fp = load_fp(fpid)
        fp.SetReference(ref)
        fp.SetValue(value)
        x, y, rot = PLACE[ref]
        fp.SetPosition(pcbnew.VECTOR2I(MM(X0 + x), MM(Y0 + y)))
        fp.SetOrientationDegrees(rot)
        for pad in fp.Pads():
            net = pinnets.get((ref, pad.GetNumber()))
            if net:
                pad.SetNet(netinfo[net])
        # Werte nicht auf den Bestückungsdruck, Referenzen klein
        fp.Value().SetVisible(False)
        fp.Reference().SetTextSize(pcbnew.VECTOR2I(MM(0.9), MM(0.9)))
        fp.Reference().SetTextThickness(MM(0.14))
        board.Add(fp)

    # Kanal-Beschriftungen K1..K16 (über bzw. unter dem Signal-Pin)
    for i in range(16):
        x, y, _ = (K_TOP + K_BOT)[i]
        add_text(board, "K%d" % (i + 1), x, y - 3.0 if i < 8 else y + 3.0, 1.0, 0)
        board.FindFootprintByReference("J%d" % (10 + i)).Reference().SetVisible(False)
    for ref in ("H1", "H2", "H3", "H4", "J3", "J4", "JP1", "JP2", "JP3", "J5"):
        board.FindFootprintByReference(ref).Reference().SetVisible(False)
    for t in SILK:
        add_text(board, *t)

    # Platinenkontur mit abgerundeten Ecken
    r = 2.0
    def seg(x1, y1, x2, y2):
        s = pcbnew.PCB_SHAPE(board, pcbnew.SHAPE_T_SEGMENT)
        s.SetStart(pcbnew.VECTOR2I(MM(X0 + x1), MM(Y0 + y1)))
        s.SetEnd(pcbnew.VECTOR2I(MM(X0 + x2), MM(Y0 + y2)))
        s.SetLayer(pcbnew.Edge_Cuts)
        s.SetWidth(MM(0.1))
        board.Add(s)
    def arc(cx, cy, sx, sy):
        a = pcbnew.PCB_SHAPE(board, pcbnew.SHAPE_T_ARC)
        a.SetCenter(pcbnew.VECTOR2I(MM(X0 + cx), MM(Y0 + cy)))
        a.SetStart(pcbnew.VECTOR2I(MM(X0 + sx), MM(Y0 + sy)))
        a.SetArcAngleAndEnd(pcbnew.EDA_ANGLE(90, pcbnew.DEGREES_T))
        a.SetLayer(pcbnew.Edge_Cuts)
        a.SetWidth(MM(0.1))
        board.Add(a)
    seg(r, 0, W - r, 0); seg(W, r, W, H - r); seg(W - r, H, r, H); seg(0, H - r, 0, r)
    arc(W - r, r, W - r, 0); arc(W - r, H - r, W, H - r); arc(r, H - r, r, H); arc(r, r, 0, r)
    return board, netinfo


def add_gnd_zones(board, netinfo):
    for layer in (pcbnew.F_Cu, pcbnew.B_Cu):
        z = pcbnew.ZONE(board)
        z.SetLayer(layer)
        z.SetNet(netinfo["GND"])
        z.SetLocalClearance(MM(0.3))
        z.SetMinThickness(MM(0.25))
        z.SetPadConnection(pcbnew.ZONE_CONNECTION_THERMAL)
        z.SetThermalReliefGap(MM(0.4))
        z.SetThermalReliefSpokeWidth(MM(0.5))
        o = z.Outline()
        o.NewOutline()
        for x, y in ((0.5, 0.5), (W - 0.5, 0.5), (W - 0.5, H - 0.5), (0.5, H - 0.5)):
            o.Append(MM(X0 + x), MM(Y0 + y))
        board.Add(z)
    pcbnew.ZONE_FILLER(board).Fill(board.Zones())


def set_power_class(pcb_path):
    """Versorgungsnetze in die Klasse "Power" (0,6 mm) – geht in KiCad 7 nur über die Projektdatei."""
    import json
    pro = pcb_path.replace(".kicad_pcb", ".kicad_pro")
    d = json.load(open(pro))
    d["net_settings"]["netclass_patterns"] = [{"netclass": "Power", "pattern": n} for n in ("GND", "+3V3", "+5V")]
    json.dump(d, open(pro, "w"), indent=2)


POWER_NETS = ("+3V3", "+5V", "GND")


def power_class_in_dsn(dsn):
    """Versorgungsnetze in der Freerouting-Datei in die Klasse "Power" (0,6 mm) verschieben.

    KiCad 7 löst die Netzklassen-Muster einer frisch erzeugten Platine nicht auf, deshalb direkt hier.
    """
    t = open(dsn, encoding="utf-8").read()
    m = re.search(r"\(class kicad_default (.*?)\n\s*\(circuit", t, re.S)
    names = m.group(1).split()
    rest = [n for n in names if n not in POWER_NETS]
    t = t[:m.start(1)] + " ".join(rest) + t[m.end(1):]
    t = re.sub(r"\(class Power[^\n(]*", "(class Power " + " ".join(POWER_NETS), t, count=1)
    open(dsn, "w", encoding="utf-8").write(t)


def route(board, jar):
    dsn = os.path.join(HERE, NAME + ".dsn")
    ses = os.path.join(HERE, NAME + ".ses")
    if not pcbnew.ExportSpecctraDSN(board, dsn):
        raise RuntimeError("DSN-Export fehlgeschlagen")
    power_class_in_dsn(dsn)
    subprocess.run(["xvfb-run", "-a", "java", "-jar", jar, "-de", dsn, "-do", ses, "-mp", "30"],
                   check=True, timeout=900)
    import_ses(board, ses)
    for f in (dsn, ses):
        os.remove(f)


def import_ses(board, path):
    """Liest Leiterbahnen und Vias aus der Freerouting-Sitzung (SES).

    KiCad 7 kann SES nur im geöffneten Editor importieren; das Format ist aber einfach:
    Koordinaten in (resolution um N)-Einheiten, Y-Achse gegenüber KiCad gespiegelt.
    """
    s = open(path, encoding="utf-8").read()
    res = re.search(r"\(resolution um (\d+)\)", s)
    scale = 1000.0 / int(res.group(1))           # SES-Einheit -> nm
    nets = board.GetNetsByName()
    layers = {"F.Cu": pcbnew.F_Cu, "B.Cu": pcbnew.B_Cu}
    routes = s[s.index("(network_out"):]
    n_tracks = n_vias = 0
    for chunk in re.split(r"\(net ", routes)[1:]:
        name = re.match(r'"?([^"\s)]+)"?', chunk).group(1)
        net = nets[name]
        for lay, width, coords in re.findall(r"\(path (\S+) (\d+)((?:\s+-?\d+)+)", chunk):
            v = [int(c) for c in coords.split()]
            pts = [(v[i] * scale, -v[i + 1] * scale) for i in range(0, len(v), 2)]
            for (x1, y1), (x2, y2) in zip(pts, pts[1:]):
                t = pcbnew.PCB_TRACK(board)
                t.SetStart(pcbnew.VECTOR2I(int(x1), int(y1)))
                t.SetEnd(pcbnew.VECTOR2I(int(x2), int(y2)))
                t.SetWidth(int(int(width) * scale))
                t.SetLayer(layers[lay])
                t.SetNet(net)
                board.Add(t)
                n_tracks += 1
        for padstack, x, y in re.findall(r'\(via "?([^"\s]+)"? (-?\d+) (-?\d+)', chunk):
            m = re.search(r"(\d+):(\d+)_um", padstack)
            via = pcbnew.PCB_VIA(board)
            via.SetPosition(pcbnew.VECTOR2I(int(int(x) * scale), int(-int(y) * scale)))
            via.SetWidth(MM(int(m.group(1)) / 1000) if m else MM(0.8))
            via.SetDrill(MM(int(m.group(2)) / 1000) if m else MM(0.4))
            via.SetNet(net)
            board.Add(via)
            n_vias += 1
    print("Importiert: %d Leiterbahnstücke, %d Vias" % (n_tracks, n_vias))


if __name__ == "__main__":
    board, netinfo = build()
    path = os.path.join(HERE, NAME + ".kicad_pcb")
    if "--route" in sys.argv:
        pcbnew.SaveBoard(path, board)
        set_power_class(path)
        board = pcbnew.LoadBoard(path)
        netinfo = {n.GetNetname(): n for n in board.GetNetsByName().values()}
        route(board, sys.argv[sys.argv.index("--route") + 1])
        add_gnd_zones(board, netinfo)
    pcbnew.SaveBoard(path, board)
    print("Platine geschrieben:", path)
