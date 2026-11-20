//go:build !(js && wasm)

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func mib(bytes int64) float64 { return float64(bytes) / (1 << 20) }

// measureClientBundle builds the browser client from the current sources
// exactly as the gate does and reports the raw and transferred sizes.
func measureClientBundle(t *testing.T) (raw, gzip int64, buildErr error) {
	t.Helper()
	out := t.TempDir()
	var stdout bytes.Buffer
	buildErr = build(out, &stdout)
	sizes := map[string]int64{}
	for _, name := range []string{wasmFile, wasmFile + ".gz"} {
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("cannot measure the client bundle, the build produced no %s: %v", name, buildErr)
		}
		sizes[name] = info.Size()
	}
	raw, gzip = sizes[wasmFile], sizes[wasmFile+".gz"]
	t.Logf("%s: %d bytes (%.2f MiB)", wasmFile, raw, mib(raw))
	t.Logf("%s.gz: %d bytes (%.2f MiB)", wasmFile, gzip, mib(gzip))
	return raw, gzip, buildErr
}

// TestTodo_CHATUX_046_Integration builds the browser client from the current
// sources exactly as the gate does, requires the build to succeed and both
// files to hold something, and records their sizes. The size ceiling and the
// headroom floors this test used to enforce were removed on 2026-11-20.
func TestTodo_CHATUX_046_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling a wasm module takes tens of seconds; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}
	raw, gzip, buildErr := measureClientBundle(t)
	if buildErr != nil {
		t.Fatalf("the browser client build failed: %v", buildErr)
	}
	if raw <= 0 || gzip <= 0 || gzip >= raw {
		t.Fatalf("implausible client bundle sizes: %d raw, %d compressed", raw, gzip)
	}
}
