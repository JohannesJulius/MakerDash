import asyncio, sys
from playwright.async_api import async_playwright
URL = "file://" + __import__("os").path.abspath("../../ui/index.html") + ""
async def main():
    async with async_playwright() as p:
        b = await p.chromium.launch(executable_path="/opt/pw-browsers/chromium" if __import__("os").path.exists("/opt/pw-browsers/chromium") else None)
        pg = await b.new_page(viewport={"width": 1180, "height": 780}, device_scale_factor=1)
        errors = []
        pg.on("pageerror", lambda e: errors.append(str(e)))
        pg.on("console", lambda m: errors.append("console:" + m.text) if m.type == "error" else None)
        await pg.goto(URL); await pg.wait_for_timeout(600)
        for p_ in ["overview", "apps", "devices", "device", "updates", "settings"]:
            await pg.evaluate(f"window.__app.go('{p_}')"); await pg.wait_for_timeout(400)
            await pg.screenshot(path=f"{p_}.png")
        # Einrichtung: Zustand "BOOTSEL gefunden"
        await pg.evaluate("""()=>{const d=JSON.parse(JSON.stringify(window.__demo)); d.config.setupDone=false; d.conn.connected=false; d.drives.circuitpy=""; d.drives.bootsel="F:\\\\"; d.drives.board="RP2350"; window.__app.state(d); }""")
        await pg.evaluate("openWizard(1)"); await pg.wait_for_timeout(400); await pg.screenshot(path="wiz1.png")
        await pg.evaluate("""()=>{const d=JSON.parse(JSON.stringify(window.__demo)); d.config.setupDone=false; d.conn.connected=false; d.drives.circuitpy=""; d.setup={state:"download",progress:42,message:""}; window.__app.state(d); }""")
        await pg.wait_for_timeout(300); await pg.screenshot(path="wiz2.png")
        await pg.evaluate("wizStep=2;renderWizard()"); await pg.evaluate("window.__app.state(window.__demo)"); await pg.wait_for_timeout(500); await pg.screenshot(path="wiz3.png")
        await pg.evaluate("wizStep=0;renderWizard()"); await pg.wait_for_timeout(300); await pg.screenshot(path="wiz0.png")
        await pg.evaluate("closeWizard(); toast('ok','Dashboard aktualisiert auf Version 2.0.0'); toast('error','Das Laufwerk CIRCUITPY wurde nicht gefunden.')"); await pg.wait_for_timeout(500)
        await pg.evaluate("""()=>{const d=JSON.parse(JSON.stringify(window.__demo)); d.firmware={state:"updating",progress:55,message:"Dateien werden übertragen …",bundled:"2.0.0"}; window.__app.state(d); window.__app.go('device')}""")
        await pg.wait_for_timeout(400); await pg.screenshot(path="fwupd.png")
        print("errors:", errors)
        await b.close()
asyncio.run(main())
