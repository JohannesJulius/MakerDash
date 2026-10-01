# MakerDash-Panel mit RP2040-Zero

Das Panel ist ein eigenständiges Bedienteil: Ein **RP2040-Zero** liest Fader, Drehgeber und Taster
und zeichnet das Display selbst (Menü, Lautstärke-Anzeige, Bildschirmschoner). Der **Pico** hängt per
USB am PC und ist nur noch die Brücke zum Panel: **4 Adern, 2 GPIOs**.

```
PC ──USB── Pico 2 ──4 Adern (GND · 5V · TX · RX)── RP2040-Zero ──┬── Fader 1–3
            (Brücke)                                  (Panel)       ├── Drehgeber + Taster
                                                                    └── OLED-Display
```

Beide Boards laufen mit **derselben Firmware**, sie erkennt ihre Rolle selbst:

| Board | USB zum PC | Display dran | Rolle |
|---|---|---|---|
| Pico | ja | ja | klassisches Dashboard (wie bisher) |
| Pico | ja | **nein** | **Brücke** zum Panel |
| RP2040-Zero | **nein** (nur 5 V) | ja | **Panel** |
| RP2040-Zero | ja | ja | funktioniert auch allein als Dashboard (z. B. zum Testen) |

## Bestellliste

| Teil | Hinweis | ca. € |
|---|---|---|
| **Waveshare RP2040-Zero** | Amazon / AliExpress; Variante mit oder ohne angelötete Stiftleisten | 4–6 |
| Dupont-Kabel Buchse–Buchse, 4 Stück (oder 4-poliges Kabel) | Verbindung Pico ↔ Zero; meist schon vorhanden | 1–3 |
| Stiftleisten 2,54 mm | nur falls der Zero ohne Leisten kommt | 1 |

Fader, Drehgeber und Display hast du schon. Sie wandern vom Pico an den Zero.

## Verdrahtung

**Pico ↔ Zero (4 Adern, TX und RX überkreuzt):**

| Pico 2 | Zero |
|---|---|
| VBUS (Pin 40) | 5V |
| GND (z. B. Pin 38) | GND |
| GP0 (Pin 1) – TX | GP1 – RX |
| GP1 (Pin 2) – RX | GP0 – TX |

**Bedienelemente am Zero:** Es gelten dieselben GP-Nummern wie bisher am Pico.

| Bauteil | Zero-Pin |
|---|---|
| Fader 1 / 2 / 3 – Schleifer | GP28 / GP27 / GP26 |
| Fader – Enden | 3V3 und GND |
| Drehgeber A / B | GP10 / GP11 |
| Drehgeber C (Mitte) | GND |
| Drehgeber-Taster | GP12 und GND |
| OLED VCC / GND | 3V3 / GND |
| OLED SDA / SCL | GP4 / GP5 |

Am Pico bleibt **kein Display** angeschlossen, sonst startet er als klassisches Dashboard.

## Einrichtung

1. **Zuerst den Pico aktualisieren:** Mit der jetzigen Verdrahtung MakerDash 2.3 installieren. Die
   App spielt Firmware 2.3.0 automatisch auf den Pico.
2. **Zero einrichten:** Den Pico abziehen. Am Zero die Taste **BOOT** gedrückt halten und ihn per
   USB-C an den PC stecken. Die App erkennt ihn und installiert CircuitPython und die
   MakerDash-Firmware.
3. **Umbauen:** Bedienelemente und Display vom Pico an den Zero umstecken, Pico und Zero mit den
   4 Adern verbinden.
4. **Pico wieder per USB anstecken.** Er startet als Brücke, der Zero als Panel. In der App steht
   unter **Übersicht → Panel** „RP2040-Zero · Firmware 2.3.0“.

**Firmware-Updates:** Den Pico aktualisiert die App wie bisher. Ist die Panel-Firmware älter, zeigt
die App einen Hinweis. Dann den Zero einmal direkt per USB-C an den PC stecken (Pico vorher abziehen),
die App aktualisiert ihn automatisch.

## Gut zu wissen

- Die Seite **Dashboard → Verdrahtung** in der App ändert die Pins des Boards, das gerade per USB
  angeschlossen ist. Für den Zero gelten die Standard-Pins aus der Tabelle oben. Willst du sie
  ändern, steckst du den Zero direkt an.
- Getestet ist das bisher nur im Simulator (Brücke und Panel getrennt). Auf echter Hardware noch
  nicht.
