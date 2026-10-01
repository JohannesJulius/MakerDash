//go:build windows

package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)



func init() {
	// Fenster, Tray und WebView2 laufen alle im Haupt-Thread
	runtime.LockOSThread()
}

func setupLog() {
	p := filepath.Join(appDataDir(), "log.txt")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if st, err := os.Stat(p); err == nil && st.Size() > 2<<20 {
		os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(f))
	log.SetFlags(log.Ldate | log.Ltime)
}

func webviewDataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = appDataDir()
	}
	return filepath.Join(base, "MakerDash", "WebView2")
}

func main() {
	openUI := false
	for _, a := range os.Args[1:] {
		switch a {
		case "--einstellungen", "--settings", "--open":
			openUI = true
		case "--debug":
			debugLog = true
		}
	}
	migrateLegacy()
	setupLog()
	log.Printf("MakerDash %s startet", AppVersion)

	// Nur eine Instanz: eine zweite öffnet einfach das Fenster der ersten
	if _, err := windows.CreateMutex(nil, false, u16(`Local\MakerDashSingleInstance`)); err == windows.ERROR_ALREADY_EXISTS {
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16(msgClassName))), 0); h != 0 {
			postMessage(h, wmAppShow, 0, 0)
		}
		return
	}

	firstRun := loadConfig()
	if firstRun {
		withConfig(func(c *Config) {}, true)
	}
	windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)

	loadIcons()
	if err := createMsgWindow(); err != nil {
		messageBox(0, "MakerDash konnte nicht gestartet werden:\n"+err.Error(), "MakerDash", mbIconError)
		return
	}
	trayAdd()

	link = NewLinkDeferred()
	audio = NewAudio(func(st AudioState) {
		rememberSeen(st)
		reapplyOnNewApps(st)
		pushToPico(false)
		markDirty()
	})
	link.Start(handleLine, onConnect, onStatus)
	go uiPump()
	go firmware.watchDrives()
	go updater.loop()

	setupDone := false
	withConfig(func(c *Config) { setupDone = c.SetupDone }, false)
	if openUI || !setupDone {
		showMain("")
	}

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	trayRemove()
	log.Println("Beendet")
}
