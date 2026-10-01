//go:build windows

package main

// Schlanke Win32-Anbindung (Fenster, Tray, Menüs) ohne GUI-Framework.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	dwmapi  = windows.NewLazySystemDLL("dwmapi.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")

	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pShowWindow               = user32.NewProc("ShowWindow")
	pDestroyWindow            = user32.NewProc("DestroyWindow")
	pUpdateWindow             = user32.NewProc("UpdateWindow")
	pGetClientRect            = user32.NewProc("GetClientRect")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pSendMessageW             = user32.NewProc("SendMessageW")
	pPostQuitMessage          = user32.NewProc("PostQuitMessage")
	pLoadCursorW              = user32.NewProc("LoadCursorW")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenuW              = user32.NewProc("AppendMenuW")
	pSetMenuDefaultItem       = user32.NewProc("SetMenuDefaultItem")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pMessageBoxW              = user32.NewProc("MessageBoxW")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pIsIconic                 = user32.NewProc("IsIconic")
	pSystemParametersInfoW    = user32.NewProc("SystemParametersInfoW")
	pFindWindowW              = user32.NewProc("FindWindowW")
	pGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	pDwmSetWindowAttribute    = dwmapi.NewProc("DwmSetWindowAttribute")
	pCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	pGetModuleHandleW         = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	pSetErrorMode             = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetErrorMode")
)

const (
	wmDestroy         = 0x0002
	wmSize            = 0x0005
	wmClose           = 0x0010
	wmGetMinMaxInfo   = 0x0024
	wmCommand         = 0x0111
	wmSetIcon         = 0x0080
	wmMove            = 0x0003
	wmLButtonUp       = 0x0202
	wmRButtonUp       = 0x0205
	wmLButtonDblClk   = 0x0203
	wmDpiChanged      = 0x02E0
	wmApp             = 0x8000
	wmActivate        = 0x0006
	ninBalloonClick   = 0x0405
	wmNull            = 0x0000

	swHide     = 0
	swShow     = 5
	swRestore  = 9
	swShowNorm = 1

	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000

	mfString    = 0x0000
	mfSeparator = 0x0800
	mfChecked   = 0x0008
	mfGrayed    = 0x0001

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmBottomAlign = 0x0020

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10
	niifInfo   = 0x1
	niifUser   = 0x4
	niifLarge  = 0x20

	mbOK          = 0x0
	mbYesNo       = 0x4
	mbIconInfo    = 0x40
	mbIconWarning = 0x30
	mbIconError   = 0x10
	idYes         = 6
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type minMaxInfo struct {
	reserved, maxSize, maxPosition, minTrackSize, maxTrackSize point
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     uintptr
}

func u16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func copyU16(dst []uint16, s string) {
	w, _ := windows.UTF16FromString(s)
	if len(w) > len(dst) {
		w = w[:len(dst)]
		w[len(w)-1] = 0
	}
	copy(dst, w)
}

func moduleHandle() uintptr {
	h, _, _ := pGetModuleHandleW.Call(0)
	return h
}

func postMessage(hwnd uintptr, m uint32, w, l uintptr) {
	pPostMessageW.Call(hwnd, uintptr(m), w, l)
}

func messageBox(owner uintptr, text, title string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), uintptr(flags))
	return int(r)
}

// iconFromPNG erzeugt ein HICON aus PNG-Daten (ab Windows Vista unterstützt).
func iconFromPNG(png []byte, size int) uintptr {
	if len(png) == 0 {
		return 0
	}
	h, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&png[0])), uintptr(len(png)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return h
}

func dpiFor(hwnd uintptr) int {
	if pGetDpiForWindow.Find() != nil {
		return 96
	}
	d, _, _ := pGetDpiForWindow.Call(hwnd)
	if d == 0 {
		return 96
	}
	return int(d)
}

func workArea() rect {
	var r rect
	const spiGetWorkArea = 0x0030
	pSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r
}

// darkTitleBar färbt die Titelleiste passend zum dunklen Design (Windows 10 20H1+/11).
func darkTitleBar(hwnd uintptr, color uint32) {
	if pDwmSetWindowAttribute.Find() != nil {
		return
	}
	on := int32(1)
	const dwmwaUseImmersiveDarkMode = 20
	const dwmwaCaptionColor = 35
	const dwmwaBorderColor = 34
	pDwmSetWindowAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&on)), 4)
	c := color
	pDwmSetWindowAttribute.Call(hwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&c)), 4)
	pDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&c)), 4)
}

func rgb(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }
