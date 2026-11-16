# Startet PulseAudio neu mit Test-Geräten (wird von run.sh und e2e_mix.sh eingebunden):
#   "CABLE Input (VB-Audio Virtual Cable)" als virtuelles Kabel, "Mikrofon (Test USB)" als Mikrofon,
#   gespeist aus der Senke "micfeed".
pulseaudio --kill 2>/dev/null || true
for i in 1 2 3 4 5 6 7 8 9 10; do pactl info >/dev/null 2>&1 || break; sleep 0.3; done
for i in 1 2 3 4 5; do
  pulseaudio -D --exit-idle-time=-1 --disallow-exit 2>/dev/null || true
  sleep 1
  pactl info >/dev/null 2>&1 && break
done
pactl info >/dev/null 2>&1 || { echo "PulseAudio startet nicht" >&2; exit 1; }
pactl load-module module-null-sink sink_name=cable rate=44100 sink_properties='device.description="CABLE\ Input\ (VB-Audio\ Virtual\ Cable)"' >/dev/null
pactl load-module module-null-sink sink_name=speakers sink_properties='device.description="Lautsprecher\ (Test)"' >/dev/null
pactl load-module module-null-sink sink_name=micfeed sink_properties='device.description="MicFeed"' >/dev/null
pactl load-module module-remap-source source_name=mic master=micfeed.monitor source_properties='device.description="Mikrofon\ (Test\ USB)"' >/dev/null
pactl set-default-sink speakers
pactl set-default-source mic
