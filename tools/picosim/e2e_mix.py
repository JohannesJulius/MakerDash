"""Ende-zu-Ende: simulierte Firmware 2.3 <-> echte MakerDash.exe unter Wine (über ein pty).
Bedient am "Dashboard" das Menü Mikro-Mix und prüft, was der PC antwortet.
Aufruf über tools/picosim/e2e_mix.sh."""
import os, sys, time as realtime, builtins
import sim
from sim import ENC, KEYQ, _Ev
PTY, CODE, FSROOT, LOG = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
_open = builtins.open
def myopen(p, *a, **k):
    if isinstance(p, str) and p.startswith("/") and not p.startswith(("/home", "/tmp", "/dev", "/usr", "/root")):
        return _open(os.path.join(FSROOT, p.lstrip("/")), *a, **k)
    return _open(p, *a, **k)
builtins.open = myopen
fd = os.open(PTY, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
log = _open(LOG, "w", buffering=1)
RX, TX = [], []
class PtySer:
    connected = True; timeout = 0; write_timeout = 0
    def __init__(self): self.buf = b""
    def _pump(self):
        try:
            while True:
                d = os.read(fd, 4096)
                if not d: break
                self.buf += d
        except BlockingIOError: pass
    @property
    def in_waiting(self): self._pump(); return len(self.buf)
    def read(self, n):
        d, self.buf = self.buf[:n], self.buf[n:]
        for l in d.decode(errors="replace").splitlines():
            if l and l != "PING": log.write("PC  > %r\n" % l); RX.append(l)
        return d
    def write(self, b):
        s = b.decode().strip("\r\n")
        if not s.startswith(("PONG", "F\t")): log.write("PICO> %r\n" % s); TX.append(s)
        os.write(fd, b); return len(b)
sim.usb_cdc.data = PtySer()
sim.tm.monotonic = realtime.monotonic
def press(): KEYQ.append(_Ev(True)); KEYQ.append(_Ev(False))
def turn(n): ENC["pos"] += n
def last(prefix):
    for l in reversed(RX):
        if l.startswith(prefix): return l
    return ""
def wait_for(cond, what, sec=25):
    t = realtime.monotonic()
    while not cond():
        if realtime.monotonic() - t > sec: raise AssertionError("Zeitüberschreitung: " + what + " | zuletzt: " + repr(RX[-6:]))
        yield 0.1
def steps():
    yield from wait_for(lambda: last("MIXS") and last("BT") and last("APPS"), "Listen vom PC")
    assert last("MIXS") == "MIXS\t1\t-1\tMusik", last("MIXS")       # Kabel da, Mix aus, Gruppe als Quelle
    assert last("BT") == "BT", last("BT")
    yield 0.5
    press(); yield 0.2; turn(3); yield 0.2; press(); yield 0.2       # Hauptmenü -> Mikro-Mix
    turn(1); yield 0.2; press(); yield 0.2                           # "Musik" wählen
    assert TX[-1] == "SETMIX\tMusik", TX
    yield from wait_for(lambda: last("MIXS") == "MIXS\t1\t0\tMusik", "PC meldet Mix aktiv")
    yield 4                                                          # Mixer läuft (Mikrofon -> Kabel)
    press(); yield 0.2; turn(3); yield 0.2; press(); yield 0.2; turn(-1); yield 0.2; press(); yield 0.2   # "Aus"
    assert TX[-1] == "SETMIX\t", TX
    yield from wait_for(lambda: last("MIXS") == "MIXS\t1\t-1\tMusik", "PC meldet Mix aus")
    log.write("OK\n"); print("E2E OK")
    raise sim.Stop
class Scen:
    def __init__(self): self.g = steps(); self.until = 0
    def tick(self):
        realtime.sleep(0.004)
        if realtime.monotonic() >= self.until: self.until = realtime.monotonic() + next(self.g)
sim.tm.sleep = lambda s: sim.SCENARIO.tick()
sim.run(CODE, Scen())
