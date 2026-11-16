//go:build windows

package main

// Bluetooth-Audiogeräte: gekoppelte Geräte auflisten, verbinden/trennen, Akkustand lesen.
//
//   - Liste der gekoppelten Geräte: bluetoothapis.dll (BluetoothFindFirstDevice …)
//   - Verbinden/Trennen: Windows hat dafür keine öffentliche Funktion. Der Bluetooth-Audiotreiber
//     kennt aber zwei Befehle (KSPROPSETID_BtAudio: ONESHOT_RECONNECT / ONESHOT_DISCONNECT) – dieselben,
//     die der "Verbinden"-Knopf in den Windows-Einstellungen benutzt. Sie werden an die Treiber-
//     Schnittstelle (IKsControl) hinter den Audio-Endpunkten des Geräts geschickt.
//   - Akkustand: Geräte-Eigenschaft {104EA319-6EE2-4701-BD47-8DDBF425BBE5} 2 am Freisprech-Knoten
//     (das ist die Zahl, die auch die Windows-Einstellungen anzeigen).

import (
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	bthapi              = windows.NewLazySystemDLL("bluetoothapis.dll")
	procBtFindFirst     = bthapi.NewProc("BluetoothFindFirstDevice")
	procBtFindNext      = bthapi.NewProc("BluetoothFindNextDevice")
	procBtFindClose     = bthapi.NewProc("BluetoothFindDeviceClose")
	procBtFindRadio     = bthapi.NewProc("BluetoothFindFirstRadio")
	procBtFindRadioDone = bthapi.NewProc("BluetoothFindRadioClose")

	cfgmgr           = windows.NewLazySystemDLL("cfgmgr32.dll")
	procCMListSize   = cfgmgr.NewProc("CM_Get_Device_ID_List_SizeW")
	procCMList       = cfgmgr.NewProc("CM_Get_Device_ID_ListW")
	procCMLocate     = cfgmgr.NewProc("CM_Locate_DevNodeW")
	procCMGetDevProp = cfgmgr.NewProc("CM_Get_DevNode_PropertyW")

	iidIDeviceTopology = mustGUID("{2A07407E-6497-4A18-9787-32F79BD0D98F}")
	iidIKsControl      = mustGUID("{28F54685-06FD-11D2-B27A-00A0C9223196}")
	ksPropSetBtAudio   = mustGUID("{7FA06C40-B8F6-4C7E-8556-E8C33A12E54D}")

	devpkeyBattery     = propertyKey{mustGUID("{104EA319-6EE2-4701-BD47-8DDBF425BBE5}"), 2}
	devpkeyContainerID = propertyKey{mustGUID("{8C7ED206-3F8A-4827-B3AB-AE9E1FAEFC6C}"), 2}
)

const (
	ksOneshotReconnect  = 0
	ksOneshotDisconnect = 1
	ksPropertyTypeGet   = 1

	deviceStateAll = 0xF

	cmFilterEnumerator = 0x1
	cmLocatePhantom    = 0x1
	devpropTypeByte    = 0x3
	devpropTypeGUID    = 0xD
)

type btSearchParams struct {
	Size                uint32
	ReturnAuthenticated int32
	ReturnRemembered    int32
	ReturnUnknown       int32
	ReturnConnected     int32
	IssueInquiry        int32
	TimeoutMultiplier   uint8
	Radio               windows.Handle
}

type btDeviceInfo struct {
	Size          uint32
	Address       uint64
	ClassOfDevice uint32
	Connected     int32
	Remembered    int32
	Authenticated int32
	LastSeen      [8]uint16
	LastUsed      [8]uint16
	Name          [248]uint16
}

type ksProperty struct {
	Set   windows.GUID
	ID    uint32
	Flags uint32
}

// BTDevice ist ein gekoppeltes Bluetooth-Audiogerät.
type BTDevice struct {
	ID        string `json:"id"` // Adresse, 12 Hex-Zeichen
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Battery   int    `json:"battery"` // 0..100, -1 = unbekannt
	CanSwitch bool   `json:"canSwitch"`
	Busy      string `json:"busy,omitempty"` // "connect" / "disconnect", solange ein Befehl läuft
}

type BTState struct {
	Available bool       `json:"available"` // Bluetooth-Adapter vorhanden und eingeschaltet
	Devices   []BTDevice `json:"devices"`
}

func (a BTState) equal(b BTState) bool {
	if a.Available != b.Available || len(a.Devices) != len(b.Devices) {
		return false
	}
	for i := range a.Devices {
		if a.Devices[i] != b.Devices[i] {
			return false
		}
	}
	return true
}

type btEndpoint struct {
	isBT      bool
	ksID      string // Treiber-Schnittstelle hinter dem Endpunkt
	container windows.GUID
	hasCont   bool
	name      string
}

type btPending struct {
	connect bool
	until   time.Time
}

type Bluetooth struct {
	mu       sync.Mutex
	state    BTState
	cmds     chan func()
	onChange func(BTState)

	// nur im Bluetooth-Thread benutzt
	enum       deviceEnumerator
	endpoints  map[string]*btEndpoint  // Endpunkt-ID -> Ergebnis der Untersuchung
	targets    map[string][]string     // Adresse -> Treiber-Schnittstellen (für Verbinden/Trennen)
	containers map[string]windows.GUID // Adresse -> Container-ID
	pending    map[string]btPending
}

func NewBluetooth(onChange func(BTState)) *Bluetooth {
	b := &Bluetooth{
		cmds:       make(chan func(), 8),
		onChange:   onChange,
		endpoints:  map[string]*btEndpoint{},
		targets:    map[string][]string{},
		containers: map[string]windows.GUID{},
		pending:    map[string]btPending{},
	}
	go b.run()
	return b
}

func (b *Bluetooth) State() BTState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return BTState{Available: b.state.Available, Devices: append([]BTDevice{}, b.state.Devices...)}
}

// SetConnected verbindet (true) oder trennt (false) ein Gerät.
func (b *Bluetooth) SetConnected(id string, on bool) {
	select {
	case b.cmds <- func() {
		guardBegin("bluetooth")
		b.setConnected(id, on)
		guardEnd("bluetooth")
	}:
	default:
	}
}

func (b *Bluetooth) run() {
	runtime.LockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		log.Println("Bluetooth CoInitializeEx:", err)
	}
	defer func() {
		if r := recover(); r != nil {
			log.Println("Bluetooth-Überwachung beendet:", r)
		}
	}()
	guardBegin("bluetooth")
	b.refresh()
	guardEnd("bluetooth")
	for {
		wait := 4 * time.Second
		if len(b.pending) > 0 {
			wait = time.Second
		}
		select {
		case f := <-b.cmds:
			f()
		case <-time.After(wait):
		}
		b.refresh()
	}
}

func btAdapterPresent() bool {
	if procBtFindRadio.Find() != nil {
		return false
	}
	size := heap[uint32]()
	*size = 4
	radio := heap[windows.Handle]()
	h, _, _ := procBtFindRadio.Call(ptr(size), ptr(radio))
	if h == 0 {
		return false
	}
	if *radio != 0 {
		windows.CloseHandle(*radio)
	}
	procBtFindRadioDone.Call(h)
	return true
}

func btPaired() []btDeviceInfo {
	if procBtFindFirst.Find() != nil {
		return nil
	}
	sp := heap[btSearchParams]()
	*sp = btSearchParams{ReturnAuthenticated: 1, ReturnRemembered: 1, ReturnConnected: 1}
	sp.Size = uint32(unsafe.Sizeof(*sp))
	info := heap[btDeviceInfo]()
	info.Size = uint32(unsafe.Sizeof(*info))
	h, _, _ := procBtFindFirst.Call(ptr(sp), ptr(info))
	if h == 0 {
		return nil
	}
	defer procBtFindClose.Call(h)
	var out []btDeviceInfo
	for i := 0; i < 64; i++ {
		out = append(out, *info)
		*info = btDeviceInfo{}
		info.Size = uint32(unsafe.Sizeof(*info))
		if r, _, _ := procBtFindNext.Call(h, ptr(info)); r == 0 {
			break
		}
	}
	return out
}

func btMac(addr uint64) string { return fmt.Sprintf("%012X", addr&0xFFFFFFFFFFFF) }

// inspect untersucht einen Audio-Endpunkt: Hängt er an einem Bluetooth-Treiber?
func (b *Bluetooth) inspect(d device) *btEndpoint {
	ep := &btEndpoint{name: d.property(pkeyDeviceFriendlyName)}
	ep.container, ep.hasCont = d.propertyGUID(pkeyDeviceContainerID)
	topo, err := d.activate(&iidIDeviceTopology)
	if err != nil {
		return ep
	}
	defer topo.Release()
	n := heap[uint32]()
	if hr := topo.call(3, ptr(n)); int32(hr) < 0 {
		return ep
	}
	for i := uint32(0); i < *n && i < 16; i++ {
		pc := heap[comObj]()
		if hr := topo.call(4, uintptr(i), ptr(pc)); int32(hr) < 0 || *pc == 0 {
			continue
		}
		conn := *pc
		ps := heap[*uint16]()
		hr := conn.call(10, ptr(ps)) // IConnector::GetDeviceIdConnectedTo
		conn.Release()
		if int32(hr) < 0 || *ps == nil {
			continue
		}
		id := windows.UTF16PtrToString(*ps)
		windows.CoTaskMemFree(unsafe.Pointer(*ps))
		if strings.HasPrefix(strings.ToLower(id), `{2}.\\?\bth`) {
			ep.isBT, ep.ksID = true, id
			break
		}
	}
	return ep
}

type btGroup struct {
	active bool
	ksIDs  []string
	names  []string
}

// scanEndpoints sammelt alle Bluetooth-Audio-Endpunkte, gruppiert nach Gerät (Container-ID).
func (b *Bluetooth) scanEndpoints() (byContainer map[windows.GUID]*btGroup, loose []*btGroup) {
	byContainer = map[windows.GUID]*btGroup{}
	if b.enum.comObj == 0 {
		e, err := newDeviceEnumerator()
		if err != nil {
			return
		}
		b.enum = e
	}
	seen := map[string]bool{}
	for _, flow := range []int{eRender, eCapture} {
		devs, err := b.enum.DevicesWithState(flow, deviceStateAll)
		if err != nil {
			continue
		}
		for _, d := range devs {
			id := d.ID()
			state := d.State()
			ep := b.endpoints[id]
			if ep == nil && id != "" {
				ep = b.inspect(d)
				b.endpoints[id] = ep
				if ep.isBT {
					debugf("Bluetooth-Endpunkt %q -> %s", ep.name, ep.ksID)
				}
			}
			d.Release()
			if ep == nil || !ep.isBT {
				continue
			}
			seen[id] = true
			var g *btGroup
			if ep.hasCont {
				g = byContainer[ep.container]
				if g == nil {
					g = &btGroup{}
					byContainer[ep.container] = g
				}
			} else {
				g = &btGroup{}
				loose = append(loose, g)
			}
			g.active = g.active || state == deviceStateActive
			g.ksIDs = append(g.ksIDs, ep.ksID)
			g.names = append(g.names, ep.name)
		}
	}
	for id, ep := range b.endpoints {
		if ep.isBT && !seen[id] {
			delete(b.endpoints, id) // Gerät wurde entkoppelt
		}
	}
	return
}

// btNodes liest für alle Bluetooth-Geräteknoten Container-ID und Akkustand.
type btNode struct {
	id        string // Instanz-ID in Großbuchstaben
	container windows.GUID
	hasCont   bool
	battery   int
}

func btNodes() []btNode {
	if procCMListSize.Find() != nil || procCMGetDevProp.Find() != nil {
		return nil
	}
	filter, _ := windows.UTF16PtrFromString("BTHENUM")
	size := heap[uint32]()
	if r, _, _ := procCMListSize.Call(ptr(size), ptr(filter), cmFilterEnumerator); r != 0 || *size < 2 {
		return nil
	}
	buf := make([]uint16, *size+64)
	if r, _, _ := procCMList.Call(ptr(filter), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), cmFilterEnumerator); r != 0 {
		return nil
	}
	runtime.KeepAlive(filter)
	var out []btNode
	for start := 0; start < len(buf) && buf[start] != 0; {
		end := start
		for end < len(buf) && buf[end] != 0 {
			end++
		}
		id := windows.UTF16ToString(buf[start:end])
		node := btNode{id: strings.ToUpper(id), battery: -1}
		inst := heap[uint32]()
		if r, _, _ := procCMLocate.Call(ptr(inst), uintptr(unsafe.Pointer(&buf[start])), cmLocatePhantom); r == 0 {
			var g windows.GUID
			if devNodeProp(*inst, devpkeyContainerID, devpropTypeGUID, unsafe.Pointer(&g), 16) {
				node.container, node.hasCont = g, true
			}
			var level uint8
			if devNodeProp(*inst, devpkeyBattery, devpropTypeByte, unsafe.Pointer(&level), 1) && level <= 100 {
				node.battery = int(level)
			}
		}
		out = append(out, node)
		start = end + 1
	}
	return out
}

func devNodeProp(inst uint32, key propertyKey, wantType uint32, dst unsafe.Pointer, n uint32) bool {
	k := heap[propertyKey]()
	*k = key
	typ := heap[uint32]()
	size := heap[uint32]()
	buf := heap[[16]byte]()
	*size = uint32(len(buf))
	r, _, _ := procCMGetDevProp.Call(uintptr(inst), ptr(k), ptr(typ), ptr(buf), ptr(size), 0)
	if r != 0 || *typ != wantType || *size < n {
		return false
	}
	copy(unsafe.Slice((*byte)(dst), n), buf[:n])
	return true
}

func (b *Bluetooth) refresh() {
	st := BTState{Available: btAdapterPresent(), Devices: []BTDevice{}}
	paired := btPaired()
	groups, loose := b.scanEndpoints()
	nodes := btNodes()
	targets := map[string][]string{}
	now := time.Now()

	for _, p := range paired {
		mac := btMac(p.Address)
		name := strings.TrimSpace(windows.UTF16ToString(p.Name[:]))
		if name == "" {
			continue
		}
		dev := BTDevice{ID: mac, Name: name, Battery: -1}

		// Container-ID und Akkustand aus den Geräteknoten
		var cont windows.GUID
		hasCont := false
		for _, n := range nodes {
			if !strings.Contains(n.id, mac) {
				continue
			}
			if n.hasCont && !hasCont {
				cont, hasCont = n.container, true
			}
			if n.battery >= 0 {
				dev.Battery = n.battery
			}
		}

		// Audio-Endpunkte dieses Geräts
		var g *btGroup
		if hasCont {
			g = groups[cont]
		}
		if g == nil {
			for _, cand := range groups {
				for _, ks := range cand.ksIDs {
					if macInPath(ks, mac) {
						g = cand
					}
				}
			}
		}
		if g == nil {
			for _, cand := range loose {
				for i, ks := range cand.ksIDs {
					if macInPath(ks, mac) || strings.Contains(strings.ToLower(cand.names[i]), strings.ToLower(name)) {
						g = cand
					}
				}
			}
		}
		isAudio := (p.ClassOfDevice>>8)&0x1F == 0x04
		if g == nil && !isAudio {
			continue // Maus, Tastatur, Handy … – hier nicht von Interesse
		}
		if g != nil {
			dev.CanSwitch = true
			dev.Connected = g.active
			targets[mac] = g.ksIDs
		} else {
			dev.Connected = p.Connected != 0
		}
		if !dev.Connected {
			dev.Battery = -1 // der gespeicherte Wert wäre veraltet
		}
		if pd, ok := b.pending[mac]; ok {
			if pd.connect == dev.Connected || now.After(pd.until) {
				delete(b.pending, mac)
			} else if pd.connect {
				dev.Busy = "connect"
			} else {
				dev.Busy = "disconnect"
			}
		}
		st.Devices = append(st.Devices, dev)
	}
	sort.SliceStable(st.Devices, func(i, j int) bool {
		if st.Devices[i].Connected != st.Devices[j].Connected {
			return st.Devices[i].Connected
		}
		return strings.ToLower(st.Devices[i].Name) < strings.ToLower(st.Devices[j].Name)
	})
	b.targets = targets

	b.mu.Lock()
	changed := !b.state.equal(st)
	b.state = st
	b.mu.Unlock()
	if changed && b.onChange != nil {
		b.onChange(st)
	}
}

func (b *Bluetooth) setConnected(mac string, on bool) {
	ids := b.targets[mac]
	if len(ids) == 0 || b.enum.comObj == 0 {
		log.Printf("Bluetooth: keine Audio-Schnittstelle für %s gefunden", mac)
		return
	}
	prop := heap[ksProperty]()
	*prop = ksProperty{Set: ksPropSetBtAudio, ID: ksOneshotReconnect, Flags: ksPropertyTypeGet}
	if !on {
		prop.ID = ksOneshotDisconnect
	}
	sent := 0
	done := map[string]bool{}
	for _, id := range ids {
		if done[id] {
			continue
		}
		done[id] = true
		d, err := b.enum.Device(id)
		if err != nil {
			debugf("Bluetooth GetDevice: %v", err)
			continue
		}
		ks, err := d.activate(&iidIKsControl)
		d.Release()
		if err != nil {
			debugf("Bluetooth IKsControl: %v", err)
			continue
		}
		ret := heap[uint32]()
		hr := ks.call(3, ptr(prop), unsafe.Sizeof(*prop), 0, 0, ptr(ret))
		ks.Release()
		if int32(hr) < 0 {
			debugf("Bluetooth KsProperty: 0x%08X", uint32(hr))
			continue
		}
		sent++
	}
	runtime.KeepAlive(prop)
	if sent == 0 {
		log.Printf("Bluetooth: Befehl für %s wurde von keinem Treiber angenommen", mac)
		return
	}
	b.pending[mac] = btPending{connect: on, until: time.Now().Add(15 * time.Second)}
}
