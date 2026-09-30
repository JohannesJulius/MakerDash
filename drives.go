//go:build windows

package main

// Findet das CIRCUITPY-Laufwerk des Pico und Picos im BOOTSEL-Modus.

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type driveInfo struct {
	Root  string // z. B. "E:\"
	Label string
	Board string // bei BOOTSEL: "RP2350" oder "RP2040"
}

func volumeLabel(root string) string {
	var name [261]uint16
	p, _ := windows.UTF16PtrFromString(root)
	err := windows.GetVolumeInformation(p, &name[0], uint32(len(name)), nil, nil, nil, nil, 0)
	if err != nil {
		return ""
	}
	return windows.UTF16ToString(name[:])
}

// scanDrives liefert CIRCUITPY-Laufwerke und BOOTSEL-Laufwerke.
func scanDrives() (circuitpy []driveInfo, bootsel []driveInfo) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		t := windows.GetDriveType(u16(root))
		if t != windows.DRIVE_REMOVABLE && t != windows.DRIVE_FIXED {
			continue
		}
		label := strings.ToUpper(volumeLabel(root))
		switch {
		case label == "CIRCUITPY":
			circuitpy = append(circuitpy, driveInfo{Root: root, Label: label})
		case label == "RP2350" || label == "RPI-RP2":
			board := "RP2040"
			if data, err := os.ReadFile(filepath.Join(root, "INFO_UF2.TXT")); err == nil {
				if strings.Contains(string(data), "RP2350") {
					board = "RP2350"
				}
			} else if label == "RP2350" {
				board = "RP2350"
			}
			bootsel = append(bootsel, driveInfo{Root: root, Label: label, Board: board})
		}
	}
	return
}

func init() {
	// Keine Windows-Fehlerdialoge bei leeren Kartenlesern o. ä.
	const semFailCriticalErrors = 0x0001
	pSetErrorMode.Call(semFailCriticalErrors)
	_ = unsafe.Sizeof(0)
}
