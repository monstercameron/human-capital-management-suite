//go:build !js || !wasm

// Package main rejects native execution because the kiosk requires the
// browser APIs available only with GOOS=js GOARCH=wasm.
package main

import (
	"fmt"
	"io"
	"os"
)

const nativeUnsupportedMessage = "timeclockwasm is browser-only; build it with GOOS=js GOARCH=wasm go build ./tools/uxqual/cmd/timeclockwasm"

func main() {
	os.Exit(nativeUnsupported(os.Stderr))
}

func nativeUnsupported(stderr io.Writer) int {
	_, _ = fmt.Fprintln(stderr, nativeUnsupportedMessage)
	return 2
}
