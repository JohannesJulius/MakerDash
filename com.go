//go:build windows

package main

// Minimale COM-Anbindung an die Windows Core Audio API (ohne cgo).

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

const (
	clsctxAll = 0x17

	eRender  = 0
	eCapture = 1

	eConsole        = 0
	eMultimedia     = 1
	eCommunications = 2

	deviceStateActive = 0x1
	stgmRead          = 0x0

	sFalse = 1
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}

var (
	clsidMMDeviceEnumerator  = mustGUID("{BCDE0395-E52F-467C-8E3D-C4579291692E}")
	iidIMMDeviceEnumerator   = mustGUID("{A95664D2-9614-4F35-A746-DE8DB63617E6}")
	iidIAudioEndpointVolume  = mustGUID("{5CDF2C82-841E-4546-9722-0CF74078229A}")
	iidIAudioSessionManager2 = mustGUID("{77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F}")
	iidIAudioSessionControl2 = mustGUID("{BFB7FF88-7239-4FC9-8FA2-07C950BE9C6D}")
	iidISimpleAudioVolume    = mustGUID("{87CE5498-68D6-44E5-9215-6DA47EF883D8}")
	clsidPolicyConfigClient  = mustGUID("{870AF99C-171D-4F9E-AF0D-E63DF40C2BC9}")
	iidIPolicyConfig         = mustGUID("{F8679F50-850A-41CF-9C72-430F290290C8}")
)

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

var (
	pkeyDeviceFriendlyName          = propertyKey{mustGUID("{A45C254E-DF1C-4EFD-8020-67D146A850E0}"), 14}
	pkeyDeviceDesc                  = propertyKey{mustGUID("{A45C254E-DF1C-4EFD-8020-67D146A850E0}"), 2}
	pkeyDeviceInterfaceFriendlyName = propertyKey{mustGUID("{026E516E-B814-414B-83CD-856D6FEF4822}"), 2}
)

// propVariant hat auf x64 24 Bytes.
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	val      [2]uint64
}

// comObj ist ein roher COM-Interface-Zeiger.
type comObj uintptr

func (o comObj) call(index int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(o))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	all := append([]uintptr{uintptr(o)}, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func (o comObj) Release() {
	if o != 0 {
		o.call(2)
	}
}

func (o comObj) QueryInterface(iid *windows.GUID) (comObj, error) {
	out := heap[comObj]()
	hr := o.call(0, ptr(iid), ptr(out))
	runtime.KeepAlive(out)
	if int32(hr) < 0 {
		return 0, hresult("QueryInterface", hr)
	}
	return *out, nil
}

func hresult(what string, hr uintptr) error {
	return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(hr))
}

func coCreate(clsid, iid *windows.GUID) (comObj, error) {
	out := heap[comObj]()
	hr, _, _ := procCoCreateInstance.Call(ptr(clsid), 0, clsctxAll, ptr(iid), ptr(out))
	runtime.KeepAlive(out)
	if int32(hr) < 0 {
		return 0, hresult("CoCreateInstance", hr)
	}
	return *out, nil
}

func f32(v float32) uintptr { return uintptr(math.Float32bits(v)) }

// heap legt einen Wert garantiert auf dem Heap an. Zeiger auf Stack-Variablen
// dürfen nicht als uintptr durch mehrere Funktionen gereicht werden, weil Go
// den Stack verschieben kann. Heap-Objekte bewegen sich nicht.
//
//go:noinline
func heap[T any]() *T { return new(T) }

func ptr[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

// ---------------- IMMDeviceEnumerator ----------------

type deviceEnumerator struct{ comObj }

func newDeviceEnumerator() (deviceEnumerator, error) {
	o, err := coCreate(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
	return deviceEnumerator{o}, err
}

// Liste aktiver Geräte (flow = eRender / eCapture)
func (e deviceEnumerator) Devices(flow int) ([]device, error) {
	pc := heap[comObj]()
	hr := e.call(3, uintptr(flow), deviceStateActive, ptr(pc))
	if int32(hr) < 0 {
		return nil, hresult("EnumAudioEndpoints", hr)
	}
	coll := *pc
	defer coll.Release()
	n := heap[uint32]()
	coll.call(3, ptr(n))
	res := make([]device, 0, *n)
	for i := uint32(0); i < *n; i++ {
		d := heap[comObj]()
		if hr := coll.call(4, uintptr(i), ptr(d)); int32(hr) >= 0 && *d != 0 {
			res = append(res, device{*d})
		}
	}
	return res, nil
}

func (e deviceEnumerator) Default(flow, role int) (device, error) {
	d := heap[comObj]()
	hr := e.call(4, uintptr(flow), uintptr(role), ptr(d))
	if int32(hr) < 0 {
		return device{}, hresult("GetDefaultAudioEndpoint", hr)
	}
	return device{*d}, nil
}

// ---------------- IMMDevice ----------------

type device struct{ comObj }

func (d device) ID() string {
	pp := heap[*uint16]()
	if hr := d.call(5, ptr(pp)); int32(hr) < 0 || *pp == nil {
		return ""
	}
	s := windows.UTF16PtrToString(*pp)
	windows.CoTaskMemFree(unsafe.Pointer(*pp))
	return s
}

func (d device) activate(iid *windows.GUID) (comObj, error) {
	out := heap[comObj]()
	hr := d.call(3, ptr(iid), clsctxAll, 0, ptr(out))
	if int32(hr) < 0 {
		return 0, hresult("Activate", hr)
	}
	return *out, nil
}

func (d device) property(key propertyKey) string {
	ps := heap[comObj]()
	if hr := d.call(4, stgmRead, ptr(ps)); int32(hr) < 0 || *ps == 0 {
		return ""
	}
	store := *ps
	defer store.Release()
	k := heap[propertyKey]()
	*k = key
	pv := heap[propVariant]()
	if hr := store.call(5, ptr(k), ptr(pv)); int32(hr) < 0 {
		return ""
	}
	defer procPropVariantClear.Call(ptr(pv))
	const vtLPWSTR = 31
	if pv.vt != vtLPWSTR || pv.val[0] == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(uintptr(pv.val[0]))))
}

// ---------------- IAudioEndpointVolume ----------------

func setEndpointVolume(d device, v float32) error {
	ev, err := d.activate(&iidIAudioEndpointVolume)
	if err != nil {
		return err
	}
	defer ev.Release()
	if hr := ev.call(7, f32(v), 0); int32(hr) < 0 {
		return hresult("SetMasterVolumeLevelScalar", hr)
	}
	return nil
}

// ---------------- Sessions ----------------

type session struct {
	pid    uint32
	system bool
	vol    comObj // ISimpleAudioVolume
}

func deviceSessions(d device) ([]session, error) {
	mgr, err := d.activate(&iidIAudioSessionManager2)
	if err != nil {
		return nil, err
	}
	defer mgr.Release()
	pe := heap[comObj]()
	if hr := mgr.call(5, ptr(pe)); int32(hr) < 0 {
		return nil, hresult("GetSessionEnumerator", hr)
	}
	en := *pe
	defer en.Release()
	n := heap[int32]()
	en.call(3, ptr(n))
	var res []session
	for i := int32(0); i < *n; i++ {
		pctl := heap[comObj]()
		if hr := en.call(4, uintptr(i), ptr(pctl)); int32(hr) < 0 || *pctl == 0 {
			continue
		}
		ctl := *pctl
		state := heap[uint32]()
		ctl.call(3, ptr(state))
		const audioSessionStateExpired = 2
		if *state == audioSessionStateExpired {
			ctl.Release()
			continue
		}
		ctl2, err := ctl.QueryInterface(&iidIAudioSessionControl2)
		if err != nil {
			ctl.Release()
			continue
		}
		ppid := heap[uint32]()
		ctl2.call(14, ptr(ppid))
		pid := *ppid
		isSys := ctl2.call(15) == 0 // S_OK = Systemklänge
		vol, err := ctl.QueryInterface(&iidISimpleAudioVolume)
		ctl2.Release()
		ctl.Release()
		if err != nil {
			continue
		}
		res = append(res, session{pid: pid, system: isSys, vol: vol})
	}
	return res, nil
}

func (s session) SetVolume(v float32) {
	s.vol.call(3, f32(v), 0)
}

// ---------------- IPolicyConfig (Standardgerät setzen) ----------------

func setDefaultDevice(id string) error {
	pc, err := coCreate(&clsidPolicyConfigClient, &iidIPolicyConfig)
	if err != nil {
		return err
	}
	defer pc.Release()
	p, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	for _, role := range []int{eConsole, eMultimedia, eCommunications} {
		hr := pc.call(13, ptr(p), uintptr(role))
		runtime.KeepAlive(p)
		if int32(hr) < 0 {
			return hresult("SetDefaultEndpoint", hr)
		}
	}
	return nil
}
