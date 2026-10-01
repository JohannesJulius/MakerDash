# Bestellliste Panel-Platine v1.0

Alles bei **einem** Händler bestellbar, der den AVR64DD28 führt: **Mouser** (mouser.de) oder
**RS Components** (de.rs-online.com). Beide liefern in Deutschland meist in 1–2 Werktagen.
Reichelt ist bei Kleinteilen günstiger, führt den AVR64DD28 aber voraussichtlich nicht.

Mengen sind **für eine Platine plus Reserve**. Preise sind grobe Richtwerte und schwanken je nach Händler.

## Bauteile

| # | Teil | Hersteller-Teilenummer / Suchkriterien | Menge | ca. € |
|---|---|---|---|---|
| 1 | Mikrocontroller | **AVR64DD28-I/SP** (Microchip, DIP-28) – 1 Reserve | 2 | 4 |
| 2 | IC-Sockel 28-polig, schmal (7,62 mm) | z. B. Mill-Max **110-43-628-41-001000**, oder beliebiger „DIP-28 Sockel 0,3 Zoll“ | 1 | 1–3 |
| 3 | Spannungsregler 3,3 V | **MCP1700-3302E/TO** (Microchip, TO-92) | 2 | 1 |
| 4 | Keramik-Kondensator 1 µF | radial, **Rastermaß 5,08 mm**, ≥ 16 V, X7R | 3 | 1 |
| 5 | Keramik-Kondensator 100 nF | radial, **Rastermaß 5,08 mm**, ≥ 16 V, X7R | 5 | 1 |
| 6 | Elektrolyt-Kondensator 10 µF | radial, **Ø 5 mm, Rastermaß 2,0 mm**, ≥ 10 V | 2 | 0,5 |
| 7 | Widerstand 4,7 kΩ | Metallschicht 0207 (¼ W), 1 % | 4 | 0,5 |
| 8 | Widerstand 1 kΩ | Metallschicht 0207 (¼ W), 1 % | 2 | 0,3 |
| 9 | LED 3 mm grün | bedrahtet, 20 mA | 2 | 0,3 |
| 10 | Platinen-Buchse JST-XH 4-polig, stehend | **B4B-XH-A(LF)(SN)** (JST) | 2 | 0,5 |
| 11 | Stecker-Gehäuse JST-XH 4-polig | **XHP-4** (JST) | 3 | 0,3 |
| 12 | Crimpkontakte JST-XH | **SXH-001T-P0.6** (JST) | 15 | 1 |
| 13 | Stiftleiste 1×40, 2,54 mm, abbrechbar | gerade | 2 | 1 |
| 14 | Buchsenleiste 1×4, 2,54 mm | gerade (für das OLED) | 1 | 0,5 |
| 15 | Jumper / Kurzschlussbrücke 2,54 mm | | 3 | 0,3 |
| 16 | Abstandshalter M3 + Schrauben | z. B. 10 mm, Innen-/Außengewinde | 4 | 2 |

**Summe Bauteile: ca. 15–20 €** zzgl. Versand. Bei Mouser gibt es Gratisversand erst ab 50 €
Bestellwert. Darunter kostet der Versand etwa 20 €. Prüfe deshalb RS, oder bestell gleich Teile für
spätere Panels mit.

Hinweise:
- **Rastermaß 5,08 mm** bei den Keramik-Kondensatoren beachten: Die Platine hat 5-mm-Löcher, Teile mit 2,54 mm passen nicht ohne Biegen.
- Für die JST-Kontakte (12) brauchst du eine **Crimpzange für JST-XH**. Ohne Zange: fertig gecrimpte **JST-XH-4-Kabel** (z. B. Amazon, ca. 5 € für 10 Stück) statt Teile 11 und 12.
- Kanal-Stiftleisten (13): Es reicht, nur die benötigten Kanäle zu bestücken.

## Zum Programmieren (einmalig)

| Teil | Hinweis | ca. € |
|---|---|---|
| USB-Seriell-Adapter **mit 3,3-V-Pegel** | CP2102 oder CH340 (z. B. Amazon) – nach DxCore-Anleitung „SerialUPDI“ | 5 |
| Widerstand 470 Ω | für den UPDI-Adapter (gehört zur SerialUPDI-Schaltung) | – |

## Platine (erst bestellen, wenn die Firmware fertig ist)

`makerdash-panel-gerber.zip` hochladen:
- **JLCPCB / PCBWay** (China): 5 Stück ca. 2–5 € + Versand, ca. 1–2 Wochen
- **Aisler** (Deutschland): 3 Stück ca. 20–30 €, ca. 1 Woche

Einstellungen: 2 Lagen, 1,6 mm, HASL (bleifrei), keine Bestückung, Größe 80 × 70 mm.

## Vor der Bestellung abhaken

- [ ] Pinbelegung des AVR64DD28 gegen das Microchip-Datenblatt (DS40002315, Kapitel „Pinout“) prüfen – siehe Tabelle in `README.md`
- [ ] Pinbelegung deines OLED-Moduls: GND · VCC · SCL · SDA? (sonst Kabel statt Buchse verwenden)
- [ ] Passt die Platine (80 × 70 mm) ins Gehäuse hinter die Frontplatte?
- [ ] Firmware für Panel und Pico fertig
