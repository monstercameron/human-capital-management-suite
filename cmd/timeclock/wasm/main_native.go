//go:build !js || !wasm

// Package main is intentionally empty for native builds. The kiosk is a
// browser-only command and is compiled with GOOS=js GOARCH=wasm.
package main

func main() {}
