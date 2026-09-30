//go:build windows

package main

import _ "embed"

//go:embed assets/icon32.png
var iconPNG32 []byte

//go:embed assets/icon256.png
var iconPNG256 []byte
