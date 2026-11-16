//go:build windows

package main

// WASAPI-Zugriffe für den Mikro-Mix: Aufnahme von einem Gerät, Aufnahme eines einzelnen
// Programms (Process Loopback, ab Windows 10 2004) und Wiedergabe auf ein Gerät.
// Alles in einem festen Format (48 kHz, Stereo, 16 Bit) – Windows rechnet um.

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidIAudioClient        = mustGUID("{1CB9AD4C-DBFA-4C32-B178-C2F568A703B2}")
	iidIAudioRenderClient  = mustGUID("{F294ACFC-3146-4483-A7BF-ADDCA7C260E2}")
	iidIAudioCaptureClient = mustGUID("{C8ADBD64-E71E-48A0-A4DE-185C395CD317}")
	iidIUnknown            = mustGUID("{00000000-0000-0000-C000-000000000046}")
	iidIAgileObject        = mustGUID("{94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}")
	iidActivateHandler     = mustGUID("{41D949AB-9862-444A-80F6-C261334DA5EB}")

	mmdevapi                        = windows.NewLazySystemDLL("mmdevapi.dll")
	procActivateAudioInterfaceAsync = mmdevapi.NewProc("ActivateAudioInterfaceAsync")
	ntdll                           = windows.NewLazySystemDLL("ntdll.dll")
	procRtlGetVersion               = ntdll.NewProc("RtlGetVersion")
)

const (
	audclntShared             = 0
	streamLoopback            = 0x00020000
	streamEventCallback       = 0x00040000
	streamNoPersist           = 0x00080000
	streamAutoConvertPCM      = 0x80000000
	streamSrcDefaultQuality   = 0x08000000
	bufferFlagSilent          = 0x2
	eNoInterface              = 0x80004002
	audclntDeviceInvalidated  = 0x88890004
	hnsPerMs                  = 10000
	processLoopbackDevicePath = `VAD\Process_Loopback`
)

// waveFormatEx: WAVEFORMATEX ist 18 Bytes lang (gepackt); die Felder liegen in Go an denselben Stellen.
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

func newMixFormat() *waveFormatEx {
	f := heap[waveFormatEx]()
	*f = waveFormatEx{
		FormatTag: 1, Channels: mixChannels, SamplesPerSec: mixRate,
		AvgBytesPerSec: mixRate * mixChannels * 2, BlockAlign: mixChannels * 2, BitsPerSample: 16,
	}
	return f
}

// windowsBuild liefert die Build-Nummer (z. B. 19045 für Windows 10 22H2, 22631 für Windows 11).
func windowsBuild() uint32 {
	var info [284]byte // RTL_OSVERSIONINFOW
	*(*uint32)(unsafe.Pointer(&info[0])) = uint32(len(info))
	if r, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&info[0]))); r != 0 {
		return 0
	}
	return *(*uint32)(unsafe.Pointer(&info[12]))
}

// ---------------- Aufnahme ----------------

type captureStream struct {
	client comObj // IAudioClient
	cap    comObj // IAudioCaptureClient
	event  windows.Handle
	buf    *jitterBuf
	pid    uint32 // nur bei Programm-Aufnahme
}

func (c *captureStream) Close() {
	if c == nil {
		return
	}
	if c.client != 0 {
		c.client.call(11) // Stop
	}
	c.cap.Release()
	c.client.Release()
	if c.event != 0 {
		windows.CloseHandle(c.event)
	}
	c.cap, c.client, c.event = 0, 0, 0
}

// openDeviceCapture nimmt von einem Aufnahmegerät (Mikrofon) auf.
func openDeviceCapture(d device) (*captureStream, error) {
	cl, err := d.activate(&iidIAudioClient)
	if err != nil {
		return nil, err
	}
	c := &captureStream{client: cl, buf: newJitterBuf()}
	f := newMixFormat()
	hr := cl.call(3, audclntShared, streamAutoConvertPCM|streamSrcDefaultQuality|streamNoPersist,
		100*hnsPerMs, 0, ptr(f), 0)
	runtime.KeepAlive(f)
	if int32(hr) < 0 {
		c.Close()
		return nil, hresult("Mikrofon Initialize", hr)
	}
	return c.start()
}

func (c *captureStream) start() (*captureStream, error) {
	out := heap[comObj]()
	if hr := c.client.call(14, ptr(&iidIAudioCaptureClient), ptr(out)); int32(hr) < 0 {
		c.Close()
		return nil, hresult("GetService(Capture)", hr)
	}
	c.cap = *out
	if hr := c.client.call(10); int32(hr) < 0 {
		c.Close()
		return nil, hresult("Start", hr)
	}
	return c, nil
}

// read holt alle wartenden Pakete in den Puffer. Fehler bedeuten: Gerät weg, neu öffnen.
func (c *captureStream) read() error {
	data := heap[uintptr]()
	frames := heap[uint32]()
	flags := heap[uint32]()
	next := heap[uint32]()
	for i := 0; i < 64; i++ {
		if hr := c.cap.call(5, ptr(next)); int32(hr) < 0 {
			return hresult("GetNextPacketSize", hr)
		}
		if *next == 0 {
			return nil
		}
		hr := c.cap.call(3, ptr(data), ptr(frames), ptr(flags), 0, 0)
		if int32(hr) < 0 {
			return hresult("GetBuffer", hr)
		}
		n := int(*frames)
		if n > 0 {
			if *flags&bufferFlagSilent != 0 || *data == 0 {
				c.buf.pushSilence(n)
			} else {
				c.buf.push(unsafe.Slice((*int16)(unsafe.Pointer(*data)), n*mixChannels))
			}
		}
		c.cap.call(4, uintptr(*frames))
	}
	return nil
}

// ---------------- Wiedergabe ----------------

type renderStream struct {
	client comObj // IAudioClient
	ren    comObj // IAudioRenderClient
	frames uint32 // Puffergröße
}

func (r *renderStream) Close() {
	if r == nil {
		return
	}
	if r.client != 0 {
		r.client.call(11)
	}
	r.ren.Release()
	r.client.Release()
	r.ren, r.client = 0, 0
}

func openRender(d device) (*renderStream, error) {
	cl, err := d.activate(&iidIAudioClient)
	if err != nil {
		return nil, err
	}
	r := &renderStream{client: cl}
	f := newMixFormat()
	hr := cl.call(3, audclntShared, streamAutoConvertPCM|streamSrcDefaultQuality|streamNoPersist,
		200*hnsPerMs, 0, ptr(f), 0)
	runtime.KeepAlive(f)
	if int32(hr) < 0 {
		r.Close()
		return nil, hresult("Kabel Initialize", hr)
	}
	n := heap[uint32]()
	if hr := cl.call(4, ptr(n)); int32(hr) < 0 {
		r.Close()
		return nil, hresult("GetBufferSize", hr)
	}
	r.frames = *n
	out := heap[comObj]()
	if hr := cl.call(14, ptr(&iidIAudioRenderClient), ptr(out)); int32(hr) < 0 {
		r.Close()
		return nil, hresult("GetService(Render)", hr)
	}
	r.ren = *out
	if hr := cl.call(10); int32(hr) < 0 {
		r.Close()
		return nil, hresult("Start", hr)
	}
	return r, nil
}

// padding: Frames, die noch nicht abgespielt sind.
func (r *renderStream) padding() (int, error) {
	n := heap[uint32]()
	if hr := r.client.call(6, ptr(n)); int32(hr) < 0 {
		return 0, hresult("GetCurrentPadding", hr)
	}
	return int(*n), nil
}

func (r *renderStream) write(samples []int16) error {
	n := len(samples) / mixChannels
	if n == 0 {
		return nil
	}
	data := heap[uintptr]()
	if hr := r.ren.call(3, uintptr(n), ptr(data)); int32(hr) < 0 || *data == 0 {
		return hresult("Render GetBuffer", hr)
	}
	copy(unsafe.Slice((*int16)(unsafe.Pointer(*data)), n*mixChannels), samples)
	if hr := r.ren.call(4, uintptr(n), 0); int32(hr) < 0 {
		return hresult("Render ReleaseBuffer", hr)
	}
	return nil
}

// ---------------- Aufnahme eines einzelnen Programms ----------------

// activateHandler ist ein kleines COM-Objekt (IActivateAudioInterfaceCompletionHandler + IAgileObject),
// das Windows aufruft, sobald das Programm-Loopback bereit ist.
type activateHandler struct {
	vtbl *[4]uintptr
	ref  int32
	done windows.Handle
}

var (
	handlerVtbl  [4]uintptr
	handlerOnce  sync.Once
	handlerMu    sync.Mutex
	handlerAlive = map[uintptr]*activateHandler{} // hält die Objekte am Leben, solange Windows sie benutzt
)

func handlerInit() {
	handlerVtbl[0] = windows.NewCallback(func(this uintptr, riid *windows.GUID, ppv *uintptr) uintptr {
		if ppv == nil {
			return 0x80004003 // E_POINTER
		}
		if *riid == iidIUnknown || *riid == iidIAgileObject || *riid == iidActivateHandler {
			handlerMu.Lock()
			if h := handlerAlive[this]; h != nil {
				h.ref++
			}
			handlerMu.Unlock()
			*ppv = this
			return 0
		}
		*ppv = 0
		return eNoInterface
	})
	handlerVtbl[1] = windows.NewCallback(func(this uintptr) uintptr {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		if h := handlerAlive[this]; h != nil {
			h.ref++
			return uintptr(h.ref)
		}
		return 1
	})
	handlerVtbl[2] = windows.NewCallback(func(this uintptr) uintptr {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		if h := handlerAlive[this]; h != nil {
			h.ref--
			if h.ref <= 0 {
				delete(handlerAlive, this)
				return 0
			}
			return uintptr(h.ref)
		}
		return 0
	})
	handlerVtbl[3] = windows.NewCallback(func(this uintptr, op uintptr) uintptr {
		handlerMu.Lock()
		h := handlerAlive[this]
		handlerMu.Unlock()
		if h != nil {
			windows.SetEvent(h.done)
		}
		return 0
	})
}

type loopbackParams struct {
	ActivationType uint32 // 1 = Process Loopback
	TargetPid      uint32
	Mode           uint32 // 0 = Prozess samt Kindprozessen
}

var errLoopbackUnsupported = errors.New("Programm-Aufnahme braucht Windows 10 (Version 2004) oder neuer")

// openProcessCapture nimmt den Ton eines Prozesses (samt Kindprozessen) auf.
func openProcessCapture(pid uint32) (*captureStream, error) {
	if b := windowsBuild(); b != 0 && b < 19041 {
		return nil, errLoopbackUnsupported
	}
	if err := procActivateAudioInterfaceAsync.Find(); err != nil {
		return nil, errLoopbackUnsupported
	}
	handlerOnce.Do(handlerInit)

	done, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(done)
	h := &activateHandler{vtbl: &handlerVtbl, ref: 1, done: done}
	this := uintptr(unsafe.Pointer(h))
	handlerMu.Lock()
	handlerAlive[this] = h
	handlerMu.Unlock()
	defer func() { // eigene Referenz abgeben
		handlerMu.Lock()
		if h.ref--; h.ref <= 0 {
			delete(handlerAlive, this)
		}
		handlerMu.Unlock()
	}()

	params := heap[loopbackParams]()
	*params = loopbackParams{ActivationType: 1, TargetPid: pid, Mode: 0}
	pv := heap[propVariant]()
	pv.vt = 65 // VT_BLOB
	pv.val[0] = uint64(unsafe.Sizeof(*params))
	pv.val[1] = uint64(uintptr(unsafe.Pointer(params)))
	path, _ := windows.UTF16PtrFromString(processLoopbackDevicePath)
	op := heap[comObj]()
	hr, _, _ := procActivateAudioInterfaceAsync.Call(ptr(path), ptr(&iidIAudioClient), ptr(pv), this, ptr(op))
	runtime.KeepAlive(path)
	if int32(hr) < 0 {
		return nil, hresult("ActivateAudioInterfaceAsync", hr)
	}
	defer (*op).Release()
	if ev, _ := windows.WaitForSingleObject(done, 4000); ev != windows.WAIT_OBJECT_0 {
		return nil, errors.New("Programm-Aufnahme: keine Antwort von Windows")
	}
	runtime.KeepAlive(params)
	runtime.KeepAlive(pv)

	res := heap[int32]()
	unk := heap[comObj]()
	if hr := (*op).call(3, ptr(res), ptr(unk)); int32(hr) < 0 {
		return nil, hresult("GetActivateResult", hr)
	}
	if *res < 0 || *unk == 0 {
		return nil, hresult("Programm-Aufnahme", uintptr(uint32(*res)))
	}
	cl, err := (*unk).QueryInterface(&iidIAudioClient)
	(*unk).Release()
	if err != nil {
		return nil, err
	}

	c := &captureStream{client: cl, buf: newJitterBuf(), pid: pid}
	f := newMixFormat()
	hr = cl.call(3, audclntShared, streamLoopback|streamEventCallback|streamAutoConvertPCM,
		200*hnsPerMs, 0, ptr(f), 0)
	runtime.KeepAlive(f)
	if int32(hr) < 0 {
		c.Close()
		return nil, hresult("Loopback Initialize", hr)
	}
	// Das Ereignis ist Pflicht, sobald EVENTCALLBACK gesetzt ist – abgefragt wird trotzdem im Takt des Mixers
	c.event, err = windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if hr := cl.call(13, uintptr(c.event)); int32(hr) < 0 {
		c.Close()
		return nil, hresult("SetEventHandle", hr)
	}
	return c.start()
}
