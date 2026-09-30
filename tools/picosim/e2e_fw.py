import os, sys, time as realtime, builtins
import sim
PTY, CODE, FSROOT, DUR, LOG = sys.argv[1], sys.argv[2], sys.argv[3], float(sys.argv[4]), sys.argv[5]
_open = builtins.open
def myopen(p, *a, **k):
    if isinstance(p, str) and p.startswith("/") and not p.startswith("/home") and not p.startswith("/tmp") and not p.startswith("/dev"):
        return _open(os.path.join(FSROOT, p.lstrip("/")), *a, **k)
    return _open(p, *a, **k)
builtins.open = myopen
fd = os.open(PTY, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
log = _open(LOG, "w", buffering=1)
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
            if l and l != "PING": log.write("PC  > %r\n" % l)
        return d
    def write(self, b):
        s = b.decode().strip()
        if not s.startswith("PONG") or "PONG" not in getattr(self, "_seen", ""):
            log.write("PICO> %r\n" % s); self._seen = s
        os.write(fd, b); return len(b)
sim.usb_cdc.data = PtySer()
sim.tm.monotonic = realtime.monotonic
T0 = realtime.monotonic()
class Scen:
    def tick(self):
        realtime.sleep(0.004)
        if realtime.monotonic() - T0 > DUR: raise sim.Stop
sim.tm.sleep = lambda s: sim.SCENARIO.tick()
os.chdir(FSROOT) if os.path.isdir(os.path.join(FSROOT, "fonts")) else None
try:
    sim.run(CODE, Scen())
except sim.Reset:
    log.write("*** microcontroller.reset()\n")
