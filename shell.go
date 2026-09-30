//go:build windows

package main

// UI-Thread: verstecktes Nachrichtenfenster, Tray-Symbol und Hauptfenster mit WebView2.

import (
	_ "embed"
	"log"
	"os/exec"
	"sync"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

//go:embed ui/index.html
var indexHTML string

const (
	wmAppDispatch = wmApp + 1
	wmAppTray     = wmApp + 2
	wmAppShow     = wmApp + 3

	msgClassName  = "PicoDashboardMsgWnd"
	mainClassName = "PicoDashboardMainWnd"

	menuOpen      = 1
	menuUpdate    = 2
	menuAutostart = 3
	menuQuit      = 4
)

var bgColor = rgb(0x0B, 0x0D, 0x12)

var shell struct {
	msgHwnd        uintptr
	mainHwnd       uintptr
	chromium       *edge.Chromium
	pageReady      bool
	visible        bool
	taskbarCreated uint32
	iconSmall      uintptr
	iconBig        uintptr
	iconTray       uintptr
	nid            notifyIconData
	balloonPage    string

	qmu   sync.Mutex
	queue []func()
}

// runOnUI führt f im UI-Thread aus (von jedem Goroutine aufrufbar).
func runOnUI(f func()) {
	shell.qmu.Lock()
	shell.queue = append(shell.queue, f)
	shell.qmu.Unlock()
	postMessage(shell.msgHwnd, wmAppDispatch, 0, 0)
}

func runQueue() {
	shell.qmu.Lock()
	q := shell.queue
	shell.queue = nil
	shell.qmu.Unlock()
	for _, f := range q {
		f()
	}
}

// uiVisible meldet, ob das Fenster gerade angezeigt wird (thread-sicher genug für Drosselung).
func uiVisible() bool { return shell.visible && shell.pageReady }

// evalJS schickt JavaScript an die Oberfläche (von jedem Goroutine aufrufbar).
func evalJS(js string) {
	runOnUI(func() {
		if shell.chromium != nil && shell.pageReady {
			shell.chromium.Eval(js)
		}
	})
}

func loadIcons() {
	shell.iconSmall = iconFromPNG(iconPNG32, 16)
	shell.iconBig = iconFromPNG(iconPNG256, 48)
	sz, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
	if sz == 0 {
		sz = 16
	}
	src := iconPNG32
	if sz > 32 {
		src = iconPNG256
	}
	shell.iconTray = iconFromPNG(src, int(sz))
}

// ---------------- Nachrichtenfenster + Tray ----------------

func createMsgWindow() error {
	cb := windows.NewCallback(msgWndProc)
	wc := wndClassEx{
		lpfnWndProc:   cb,
		hInstance:     moduleHandle(),
		lpszClassName: u16(msgClassName),
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	// Normales (unsichtbares) Top-Level-Fenster, damit es "TaskbarCreated" empfängt
	h, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(msgClassName))),
		uintptr(unsafe.Pointer(u16("Pico Dashboard"))), 0, 0, 0, 0, 0, 0, 0, moduleHandle(), 0)
	if h == 0 {
		return err
	}
	shell.msgHwnd = h
	r, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
	shell.taskbarCreated = uint32(r)
	return nil
}

func trayAdd() {
	n := &shell.nid
	n.cbSize = uint32(unsafe.Sizeof(*n))
	n.hWnd = shell.msgHwnd
	n.uID = 1
	n.uFlags = nifMessage | nifIcon | nifTip
	n.uCallbackMessage = wmAppTray
	n.hIcon = shell.iconTray
	copyU16(n.szTip[:], "Pico Dashboard")
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(n)))
}

func trayRemove() {
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&shell.nid)))
}

// setTrayTip ändert den Tooltip (von jedem Goroutine aufrufbar).
func setTrayTip(tip string) {
	runOnUI(func() {
		n := &shell.nid
		n.uFlags = nifTip
		for i := range n.szTip {
			n.szTip[i] = 0
		}
		copyU16(n.szTip[:], tip)
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(n)))
	})
}

// notifyUser zeigt eine Windows-Benachrichtigung. page = Seite, die beim Klick geöffnet wird.
func notifyUser(title, text, page string) {
	runOnUI(func() {
		shell.balloonPage = page
		n := &shell.nid
		n.uFlags = nifInfo
		for i := range n.szInfo {
			n.szInfo[i] = 0
		}
		for i := range n.szInfoTitle {
			n.szInfoTitle[i] = 0
		}
		copyU16(n.szInfoTitle[:], title)
		copyU16(n.szInfo[:], text)
		n.dwInfoFlags = niifUser | niifLarge
		n.hBalloonIcon = shell.iconBig
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(n)))
	})
}

func trayMenu() {
	m, _, _ := pCreatePopupMenu.Call()
	add := func(id int, text string, flags uint32) {
		pAppendMenuW.Call(m, uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(u16(text))))
	}
	add(menuOpen, "Pico Dashboard öffnen", mfString)
	add(0, "", mfSeparator)
	add(menuUpdate, "Nach Updates suchen", mfString)
	var fl uint32 = mfString
	if autostartEnabled() {
		fl |= mfChecked
	}
	add(menuAutostart, "Mit Windows starten", fl)
	add(0, "", mfSeparator)
	add(menuQuit, "Beenden", mfString)
	pSetMenuDefaultItem.Call(m, menuOpen, 0)

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(shell.msgHwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmRightButton|tpmReturnCmd|tpmBottomAlign,
		uintptr(pt.x), uintptr(pt.y), 0, shell.msgHwnd, 0)
	postMessage(shell.msgHwnd, wmNull, 0, 0)
	pDestroyMenu.Call(m)

	switch cmd {
	case menuOpen:
		showMain("")
	case menuUpdate:
		showMain("updates")
		go updater.Check(true)
	case menuAutostart:
		setAutostart(!autostartEnabled())
		markDirty()
	case menuQuit:
		quitApp()
	}
}

func quitApp() {
	trayRemove()
	pPostQuitMessage.Call(0)
}

func msgWndProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmAppDispatch:
		runQueue()
		return 0
	case wmAppShow:
		showMain("")
		return 0
	case wmAppTray:
		switch uint32(l) {
		case wmLButtonUp:
			showMain("")
		case wmRButtonUp:
			trayMenu()
		case ninBalloonClick:
			showMain(shell.balloonPage)
		}
		return 0
	}
	if shell.taskbarCreated != 0 && uint32(m) == shell.taskbarCreated {
		trayAdd() // Explorer wurde neu gestartet
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, w, l)
	return r
}

// ---------------- Hauptfenster ----------------

func mainWndProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmSize:
		if shell.chromium != nil {
			shell.chromium.Resize()
		}
		return 0
	case wmMove:
		if shell.chromium != nil {
			shell.chromium.NotifyParentWindowPositionChanged()
		}
	case wmClose:
		pShowWindow.Call(hwnd, swHide)
		shell.visible = false
		return 0
	case wmGetMinMaxInfo:
		mm := (*minMaxInfo)(unsafe.Pointer(l))
		d := int32(dpiFor(hwnd))
		mm.minTrackSize = point{940 * d / 96, 620 * d / 96}
		return 0
	case wmDpiChanged:
		r := (*rect)(unsafe.Pointer(l))
		pSetWindowPos.Call(hwnd, 0, uintptr(r.left), uintptr(r.top),
			uintptr(r.right-r.left), uintptr(r.bottom-r.top), 0x0014)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, w, l)
	return r
}

func createMain() bool {
	brush, _, _ := pCreateSolidBrush.Call(uintptr(bgColor))
	cur, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc := wndClassEx{
		lpfnWndProc:   windows.NewCallback(mainWndProc),
		hInstance:     moduleHandle(),
		hIcon:         shell.iconBig,
		hIconSm:       shell.iconSmall,
		hCursor:       cur,
		hbrBackground: brush,
		lpszClassName: u16(mainClassName),
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	h, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(mainClassName))),
		uintptr(unsafe.Pointer(u16("Pico Dashboard"))), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 1180, 780, 0, 0, moduleHandle(), 0)
	if h == 0 {
		log.Println("CreateWindow:", err)
		return false
	}
	shell.mainHwnd = h
	darkTitleBar(h, bgColor)

	// Größe passend zur Bildschirmskalierung, zentriert
	d := int32(dpiFor(h))
	wa := workArea()
	ww, wh := 1180*d/96, 780*d/96
	if ww > wa.right-wa.left {
		ww = wa.right - wa.left
	}
	if wh > wa.bottom-wa.top {
		wh = wa.bottom - wa.top
	}
	x := wa.left + (wa.right-wa.left-ww)/2
	y := wa.top + (wa.bottom-wa.top-wh)/2
	pSetWindowPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0x0014)

	c := edge.NewChromium()
	c.DataPath = webviewDataDir()
	c.MessageCallback = handleUIMessage
	if !c.Embed(h) {
		return false
	}
	shell.chromium = c
	if ctl := c.GetController(); ctl != nil {
		if c2 := ctl.GetICoreWebView2Controller2(); c2 != nil {
			c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{A: 255, R: 0x0B, G: 0x0D, B: 0x12})
		}
	}
	if s, err := c.GetSettings(); err == nil {
		s.PutAreDefaultContextMenusEnabled(debugLog)
		s.PutAreDevToolsEnabled(debugLog)
		s.PutIsStatusBarEnabled(false)
		s.PutIsZoomControlEnabled(false)
		s.PutAreBrowserAcceleratorKeysEnabled(debugLog)
		s.PutIsPinchZoomEnabled(false)
		s.PutIsSwipeNavigationEnabled(false)
	}
	c.Resize()
	c.NavigateToString(indexHTML)
	return true
}

// showMain öffnet das Hauptfenster (optional auf einer bestimmten Seite).
func showMain(page string) {
	if shell.mainHwnd == 0 {
		if !createMain() {
			if shell.mainHwnd != 0 {
				pDestroyWindow.Call(shell.mainHwnd)
				shell.mainHwnd = 0
			}
			webviewMissing()
			return
		}
	}
	if iconic, _, _ := pIsIconic.Call(shell.mainHwnd); iconic != 0 {
		pShowWindow.Call(shell.mainHwnd, swRestore)
	} else {
		pShowWindow.Call(shell.mainHwnd, swShow)
	}
	pSetForegroundWindow.Call(shell.mainHwnd)
	shell.visible = true
	if page != "" && shell.pageReady {
		shell.chromium.Eval(`window.__app&&window.__app.go(` + jsString(page) + `)`)
	} else if page != "" {
		pendingPage = page
	}
	markDirty()
}

var pendingPage string

func webviewMissing() {
	const url = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
	if messageBox(0, "Für die Oberfläche von Pico Dashboard wird die Microsoft WebView2 Runtime benötigt "+
		"(bei Windows 11 normalerweise vorinstalliert).\n\nJetzt herunterladen?", "Pico Dashboard",
		mbYesNo|mbIconWarning) == idYes {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}
