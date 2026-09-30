//go:build windows

package main

// Einfache serielle Schnittstelle direkt über die Windows-API.
// Lesen per kurzem Polling (keine überlappende E/A) – funktioniert mit
// jedem Treiber, auch virtuellen Ports.

import (
	"errors"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procSetCommState      = kernel32.NewProc("SetCommState")
	procGetCommState      = kernel32.NewProc("GetCommState")
	procSetCommTimeouts   = kernel32.NewProc("SetCommTimeouts")
	procSetupComm         = kernel32.NewProc("SetupComm")
	procPurgeComm         = kernel32.NewProc("PurgeComm")
	procEscapeCommFunc    = kernel32.NewProc("EscapeCommFunction")
)

type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32
	wReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   byte
	Parity     byte
	StopBits   byte
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EofChar    byte
	EvtChar    byte
	wReserved1 uint16
}

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

type comPort struct {
	h      windows.Handle
	mu     sync.Mutex // schützt Schreiben
	closed bool
	cmu    sync.Mutex
}

func openCOM(name string) (*comPort, error) {
	name = strings.TrimSpace(name)
	path, err := windows.UTF16PtrFromString(`\\.\` + name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	procSetupComm.Call(uintptr(h), 4096, 4096)

	d := heap[dcb]()
	d.DCBlength = uint32(unsafe.Sizeof(dcb{}))
	procGetCommState.Call(uintptr(h), ptr(d))
	d.BaudRate = 115200
	d.ByteSize = 8
	d.Parity = 0   // keine
	d.StopBits = 0 // 1 Stoppbit
	// fBinary=1, fDtrControl=ENABLE (Bits 4-5 = 01), fRtsControl=ENABLE (Bits 12-13 = 01)
	d.Flags = 1 | (1 << 4) | (1 << 12)
	if r, _, e := procSetCommState.Call(uintptr(h), ptr(d)); r == 0 {
		windows.CloseHandle(h)
		return nil, errors.New("SetCommState: " + e.Error())
	}

	t := heap[commTimeouts]()
	t.ReadIntervalTimeout = 0xFFFFFFFF // Lesen kehrt sofort zurück
	t.WriteTotalTimeoutConstant = 1000
	procSetCommTimeouts.Call(uintptr(h), ptr(t))

	const setDTR = 5
	procEscapeCommFunc.Call(uintptr(h), setDTR)
	const purgeRxClear, purgeTxClear = 0x8, 0x4
	procPurgeComm.Call(uintptr(h), purgeRxClear|purgeTxClear)
	return &comPort{h: h}, nil
}

// Read liefert sofort, was gerade da ist (evtl. 0 Bytes).
func (c *comPort) Read(buf []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(c.h, buf, &n, nil)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (c *comPort) Write(b []byte) (int, error) {
	if c.isClosed() {
		return 0, errors.New("Port geschlossen")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var n uint32
	err := windows.WriteFile(c.h, b, &n, nil)
	if err == nil && int(n) != len(b) {
		err = errors.New("Schreiben unvollständig")
	}
	return int(n), err
}

// Close markiert den Port als geschlossen; das Handle schließt der Lese-Goroutine.
func (c *comPort) Close() error {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	c.closed = true
	return nil
}

func (c *comPort) release() {
	c.mu.Lock() // kein Schreiben mehr in Arbeit
	defer c.mu.Unlock()
	windows.CloseHandle(c.h)
}

func (c *comPort) isClosed() bool {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return c.closed
}
