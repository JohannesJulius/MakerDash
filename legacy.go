//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Bis Version 2.0.x hieß die App "Pico Dashboard". Einstellungen und Autostart
// werden beim ersten Start unter dem neuen Namen übernommen.
const legacyName = "PicoDashboard"

func migrateLegacy() {
	if dir, err := os.UserConfigDir(); err == nil {
		old, neu := filepath.Join(dir, legacyName), appDataDir()
		if _, err := os.Stat(neu); os.IsNotExist(err) {
			if _, err := os.Stat(old); err == nil {
				os.Rename(old, neu)
			}
		}
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		os.RemoveAll(filepath.Join(base, legacyName)) // WebView2-Daten, Download-Cache
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	v, _, err := k.GetStringValue(legacyName)
	if err != nil {
		return
	}
	k.DeleteValue(legacyName)
	if v != "" && !strings.Contains(strings.ToLower(v), "uninstall") {
		k.SetStringValue(runName, `"`+exePath()+`"`)
	}
}
