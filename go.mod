module makerdash

go 1.24.7

replace go.bug.st/serial => github.com/bugst/go-serial v1.6.4

replace golang.org/x/sys => github.com/golang/sys v0.37.0

require (
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	go.bug.st/serial v0.0.0-00010101000000-000000000000
	golang.org/x/sys v0.37.0
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
)
