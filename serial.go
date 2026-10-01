//go:build windows

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial/enumerator"
)

// USB-Hersteller-IDs: Raspberry Pi (Pico) und Adafruit (CircuitPython)
var picoVIDs = map[string]bool{"2E8A": true, "239A": true}

// Remote beschreibt die Firmware am anderen Ende (aus der PONG-Antwort).
type Remote struct {
	Proto   int
	Version string
	BootOK  bool
	Bridge  bool   // Pico ohne Display, leitet an ein Panel weiter (ab Firmware 2.3)
	Panel   string // Firmware-Version des angeschlossenen Panels ("" = keins)
}

func parsePong(s string) (Remote, bool) {
	f := strings.Split(s, "\t")
	if len(f) < 2 || f[0] != "PONG" || f[1] != "DASH" {
		return Remote{}, false
	}
	r := Remote{Proto: 1, Version: "1.0.0", BootOK: true}
	if len(f) > 2 {
		if p, err := strconv.Atoi(f[2]); err == nil {
			r.Proto = p
		}
	}
	if len(f) > 3 {
		r.Version = f[3]
	}
	if len(f) > 4 {
		r.BootOK = f[4] == "1"
	}
	if len(f) > 5 {
		r.Bridge = true
		if f[5] != "-" {
			r.Panel = f[5]
		}
	}
	return r, true
}

type Link struct {
	mu        sync.Mutex
	port      *comPort
	portName  string
	remote    Remote
	onLine    func(string)
	onConnect func(Remote)
	onStatus  func(connected bool, port string)
	logged    map[string]bool
}

func NewLinkDeferred() *Link { return &Link{logged: map[string]bool{}} }

func (l *Link) Start(onLine func(string), onConnect func(Remote), onStatus func(bool, string)) {
	l.onLine, l.onConnect, l.onStatus = onLine, onConnect, onStatus
	go l.run()
}

func (l *Link) Connected() (bool, string, Remote) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.port != nil, l.portName, l.remote
}

func (l *Link) Send(line string) {
	l.mu.Lock()
	p := l.port
	l.mu.Unlock()
	if p == nil {
		return
	}
	if _, err := p.Write([]byte(line + "\n")); err != nil {
		log.Println("Senden:", err)
		p.Close()
	}
}

func candidatePorts() []string {
	var manual string
	withConfig(func(c *Config) { manual = strings.TrimSpace(c.Port) }, false)
	if manual != "" {
		return []string{manual}
	}
	list, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil
	}
	var res []string
	// Datenschnittstelle hat meist die höhere COM-Nummer -> rückwärts probieren
	for i := len(list) - 1; i >= 0; i-- {
		p := list[i]
		if p.IsUSB && picoVIDs[strings.ToUpper(p.VID)] {
			res = append(res, p.Name)
		}
	}
	return res
}

func (l *Link) run() {
	for {
		for _, name := range candidatePorts() {
			c, rem, err := handshake(name)
			if err != nil {
				if !l.logged[name] {
					log.Printf("%s: %v", name, err)
					l.logged[name] = true
				}
				continue
			}
			l.logged = map[string]bool{}
			l.mu.Lock()
			l.port = c.port
			l.portName = name
			l.remote = rem
			l.mu.Unlock()
			log.Printf("Verbunden: %s (Firmware %s, Protokoll %d, boot %v)", name, rem.Version, rem.Proto, rem.BootOK)
			l.onStatus(true, name)
			l.onConnect(rem)
			l.serve(c)
			l.mu.Lock()
			l.port = nil
			l.portName = ""
			l.mu.Unlock()
			c.port.Close()
			log.Println("Getrennt:", name)
			l.onStatus(false, "")
			break
		}
		time.Sleep(1500 * time.Millisecond)
	}
}

// conn liest in einem eigenen Goroutine Zeilen vom Port.
type conn struct {
	port  *comPort
	lines chan string
}

func newConn(p *comPort) *conn {
	c := &conn{port: p, lines: make(chan string, 256)}
	go func() {
		defer close(c.lines)
		defer p.release()
		buf := make([]byte, 1024)
		var line []byte
		for !p.isClosed() {
			n, err := p.Read(buf)
			if err != nil {
				return
			}
			if n == 0 {
				time.Sleep(8 * time.Millisecond)
				continue
			}
			for _, b := range buf[:n] {
				if b == '\n' {
					s := strings.TrimRight(string(line), "\r")
					line = line[:0]
					if s != "" {
						c.lines <- s
					}
				} else if len(line) < 8192 {
					line = append(line, b)
				}
			}
		}
	}()
	return c
}

func handshake(name string) (*conn, Remote, error) {
	p, err := openCOM(name)
	if err != nil {
		return nil, Remote{}, err
	}
	c := newConn(p)
	if _, err := p.Write([]byte("\nPING\n")); err != nil {
		p.Close()
		return nil, Remote{}, err
	}
	timeout := time.After(1500 * time.Millisecond)
	var got []string
	for {
		select {
		case s, ok := <-c.lines:
			if !ok {
				p.Close()
				return nil, Remote{}, fmt.Errorf("Port geschlossen (empfangen: %q)", got)
			}
			if r, ok := parsePong(s); ok {
				return c, r, nil
			}
			if len(got) < 5 {
				got = append(got, s)
			}
		case <-timeout:
			p.Close()
			return nil, Remote{}, fmt.Errorf("keine Antwort vom Dashboard (empfangen: %q)", got)
		}
	}
}

func (l *Link) serve(c *conn) {
	ping := time.NewTicker(3 * time.Second)
	defer ping.Stop()
	lastRx := time.Now()
	for {
		select {
		case s, ok := <-c.lines:
			if !ok {
				return
			}
			lastRx = time.Now()
			if r, ok := parsePong(s); ok {
				// Firmware wurde ohne USB-Neuverbindung neu gestartet (z. B. nach einem Update)
				l.mu.Lock()
				changed := r != l.remote
				l.remote = r
				l.mu.Unlock()
				if changed {
					log.Printf("Firmware gewechselt: %s (Protokoll %d, boot %v)", r.Version, r.Proto, r.BootOK)
					l.onConnect(r)
				}
				continue
			}
			l.onLine(s)
		case <-ping.C:
			if time.Since(lastRx) > 10*time.Second {
				log.Println("Keine Antwort mehr vom Dashboard")
				return
			}
			if _, err := c.port.Write([]byte("PING\n")); err != nil {
				return
			}
		}
	}
}
