"""Zieht Fader 2 am Gerätebild und misst den Abstand zwischen Maus und Fader-Kappe.
Die echte App meldet jeden Wert mit Verzögerung zurück (levels) und schickt zwischendurch neuen
Zustand (state) – beides wird hier nachgebildet."""
import asyncio, sys, pathlib
from playwright.async_api import async_playwright
HTML = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else pathlib.Path(__file__).parent / "../../ui/index.html").resolve().as_uri()
ECHO = """()=>{
  const d=JSON.parse(JSON.stringify(window.__demo)); d.conn.connected=false; d.firmware.state="unknown";
  d.drives={bootsel:false,circuitpy:false}; d.faders=[640,300,500]; d.f3Key="spotify.exe";
  window.__soft=true; window.__app.state(d); window.__app.levels(d.faders); window.__app.go('overview');
  const vals=[...d.faders];
  window.__sent=[];
  const orig=console.log;
  console.log=(...a)=>{ if(a[0]==="→" && a[1]==="setFader"){ const m=a[2]; window.__sent.push(m.v);
     setTimeout(()=>{ vals[m.n-1]=m.v; window.__app.levels([...vals]); }, 70); } else orig(...a); };
  window.__stateTimer=setInterval(()=>{ const s=JSON.parse(JSON.stringify(d)); s.faders=[...vals]; window.__app.state(s); }, 100);
}"""
async def main():
    async with async_playwright() as p:
        b = await p.chromium.launch(); pg = await b.new_page(viewport={"width": 1180, "height": 780})
        await pg.goto(HTML); await pg.wait_for_timeout(400); await pg.evaluate(ECHO); await pg.wait_for_timeout(300)
        async def knob_center():
            return await pg.evaluate("""()=>{const k=document.getElementById('kn1').getBoundingClientRect(); return (k.top+k.bottom)/2}""")
        async def mid(): 
            box = await pg.locator("#devArt svg").bounding_box(); return box
        box = await mid()
        x = box["x"] + box["width"] * (141.8 + 2) / 288
        start = await knob_center()
        # Kappe 4 px unter ihrer Mitte greifen (wie man es mit der Maus macht)
        y0 = start + 4
        await pg.mouse.move(x, y0); await pg.mouse.down()
        worst = 0; first = None
        for k in range(1, 23):  # 88 px nach oben – innerhalb des Fader-Wegs
            y = y0 - k * 4
            await pg.mouse.move(x, y); await pg.wait_for_timeout(16)
            off = (await knob_center()) - (y - 4)   # Kappe soll dort bleiben, wo sie gegriffen wurde
            if first is None: first = off
            worst = max(worst, abs(off))
        before = await knob_center()
        await pg.mouse.up(); await pg.wait_for_timeout(500)
        after = await knob_center()
        alive = await pg.evaluate("document.getElementById('kn1').isConnected")
        print(f"größter Versatz beim Ziehen: {worst:.1f} px, Sprung beim Loslassen: {after-before:+.1f} px, letzte Werte: {(await pg.evaluate('window.__sent'))[-3:]}")
        # Klick in die Schiene (weit weg von der Kappe): Kappe springt dorthin und bleibt da
        target = box["y"] + box["height"] * 330 / 511
        await pg.mouse.click(x, target); await pg.wait_for_timeout(600)
        jump = (await knob_center()) - target
        # Schieberegler in der Liste: Wert bleibt nach dem Loslassen stehen
        await pg.fill("#fr0", "250"); await pg.wait_for_timeout(600)
        slider = await pg.evaluate("[+document.getElementById('fr0').value, document.getElementById('fp0').textContent]")
        print(f"Klick in die Schiene: Kappe {jump:+.1f} px neben der Maus; Liste: {slider}; Kappe noch im Bild: {alive}")
        ok = worst <= 3 and abs(after - before) <= 3 and abs(jump) <= 3 and slider[0] == 250 and alive
        print("OK" if ok else "FEHLER"); await b.close(); sys.exit(0 if ok else 1)
asyncio.run(main())
