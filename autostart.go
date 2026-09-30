//go:build windows

package main

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runName = "PicoDashboard"

func exePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runName)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(v), strings.ToLower(exePath()))
}

func setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue(runName, `"`+exePath()+`"`)
	}
	err = k.DeleteValue(runName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}
