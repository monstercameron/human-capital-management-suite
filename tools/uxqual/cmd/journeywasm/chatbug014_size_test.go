//go:build !(js && wasm)

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_014 holds the client under its size ceilings without
// raising them: the ceilings keep their values, the build leaves the module's
// own packages un-inlined, and a real build of the current sources fits.
func TestTodo_CHATBUG_014(t *testing.T) {
	if maxJourneyWasmBytes != 60<<20 || maxJourneyWasmGzipBytes != 12<<20 {
		t.Fatalf("the size ceilings were changed: %d and %d", maxJourneyWasmBytes, maxJourneyWasmGzipBytes)
	}
	if !strings.HasSuffix(wasmGCFlags, "=-l") || strings.HasPrefix(wasmGCFlags, "all=") || !strings.Contains(wasmGCFlags, "human-capital-management-suite/...") {
		t.Fatalf("wasmGCFlags must turn inlining off for this module only, got %q", wasmGCFlags)
	}
	if testing.Short() {
		t.Skip("compiling a wasm module takes tens of seconds; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	// build refuses an over-budget bundle itself; the sizes are read back so a
	// failure names how far over, and a pass records the margin.
	buildErr := build(out, &stdout)
	for _, name := range []string{wasmFile, wasmFile + ".gz"} {
		if info, err := os.Stat(filepath.Join(out, name)); err == nil {
			t.Logf("%s: %d bytes", name, info.Size())
		}
	}
	if buildErr != nil {
		t.Fatalf("the client does not fit its size ceilings: %v", buildErr)
	}
	// The module must not carry the TLS stack a page can never run. The build
	// says so itself when the toolchain's net/http no longer takes the change;
	// the function names are read from the module because a stripped Go module
	// still holds them for its own tracebacks.
	if strings.Contains(stdout.String(), "building without the fetch-only net/http") {
		t.Fatalf("the build fell back to the full net/http: %s", stdout.String())
	}
	module, err := os.ReadFile(filepath.Join(out, wasmFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"crypto/tls.(*Conn).clientHandshake", "net/http.(*Transport).roundTrip", "crypto/x509.(*Certificate).Verify"} {
		if bytes.Contains(module, []byte(name)) {
			t.Fatalf("the client module contains %s: something reaches the dialling round trip again (an *http.Transport built by hand, or a net/http the fetch-only build no longer covers)", name)
		}
	}
	if !bytes.Contains(module, []byte("net/http.fetchOnlyTransport.RoundTrip")) {
		t.Fatal("the client module has no fetch-only round trip: its requests would have nowhere to go")
	}
}
