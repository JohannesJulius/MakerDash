# Pico Dashboard – boot.py
# Wird von der App verwaltet. Läuft nur beim Einstecken bzw. nach einem Reset.

import supervisor
import usb_cdc
import usb_midi
import usb_hid

# Unter diesem Namen erscheint das Gerät in Windows
try:
    supervisor.set_usb_identification(manufacturer="DIY", product="Pico Dashboard")
except Exception:
    pass

# Zweite serielle Schnittstelle für die App, MIDI/Tastatur werden nicht gebraucht
usb_cdc.enable(console=True, data=True)
usb_midi.disable()
usb_hid.disable()

# Markierung, an der die Firmware erkennt, dass diese boot.py aktiv ist
print("DASHBOOT 2")
