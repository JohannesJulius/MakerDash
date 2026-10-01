#!/bin/sh
# Erzeugt Schaltplan, Platine (geroutet), Gerber-Zip und PDF neu.
# Braucht: KiCad 7 (kicad-cli + Python-Modul pcbnew), Java, xvfb-run und freerouting.jar (v1.9).
#   ./build.sh /pfad/zu/freerouting.jar
set -e
cd "$(dirname "$0")"
JAR="${1:?Pfad zu freerouting.jar angeben}"
python3 gen_schematic.py
kicad-cli sch export netlist -o makerdash-panel.net makerdash-panel.kicad_sch
kicad-cli sch export pdf -o makerdash-panel-schaltplan.pdf makerdash-panel.kicad_sch
python3 gen_pcb.py --route "$JAR"
python3 - <<'PY'
import pcbnew
b = pcbnew.LoadBoard("makerdash-panel.kicad_pcb")
pcbnew.WriteDRCReport(b, "drc.rpt", pcbnew.EDA_UNITS_MILLIMETRES, True)
PY
grep "Found" drc.rpt
if grep -A1 "^\[" drc.rpt | grep -q "Severity: error"; then echo "DRC-Fehler, siehe drc.rpt"; exit 1; fi
rm -rf gerber && mkdir gerber
kicad-cli pcb export gerbers -l F.Cu,B.Cu,F.SilkS,B.SilkS,F.Mask,B.Mask,Edge.Cuts -o gerber/ makerdash-panel.kicad_pcb
kicad-cli pcb export drill --format excellon --excellon-separate-th -o gerber/ makerdash-panel.kicad_pcb
rm -f makerdash-panel-gerber.zip && (cd gerber && zip -q ../makerdash-panel-gerber.zip *)
rm -rf gerber logs makerdash-panel.net makerdash-panel.kicad_prl drc.rpt
echo "Fertig: makerdash-panel-gerber.zip"
