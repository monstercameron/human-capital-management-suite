package application

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateClockKioskAssets_defaultDisabled(t *testing.T) {
	if err := ValidateClockKioskAssets(ClockKioskAssets{}); err != nil {
		t.Fatalf("empty kiosk assets: %v", err)
	}
}

func TestValidateClockKioskAssets_rejectsPartialPair(t *testing.T) {
	if err := ValidateClockKioskAssets(ClockKioskAssets{WASMPath: "clock.wasm"}); err == nil {
		t.Fatal("partial kiosk assets accepted")
	}
}

func TestValidateClockKioskAssets_acceptsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	wasm := filepath.Join(dir, "clock.wasm")
	shim := filepath.Join(dir, "wasm_exec.js")
	if err := os.WriteFile(wasm, []byte("wasm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("shim"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClockKioskAssets(ClockKioskAssets{WASMPath: wasm, WASMExecPath: shim}); err != nil {
		t.Fatalf("existing kiosk assets rejected: %v", err)
	}
}

func TestValidateClockKioskAssets_rejectsMissingAndUnboundedPaths(t *testing.T) {
	if err := ValidateClockKioskAssets(ClockKioskAssets{WASMPath: filepath.Join(t.TempDir(), "missing"), WASMExecPath: filepath.Join(t.TempDir(), "shim")}); err == nil {
		t.Fatal("missing kiosk assets accepted")
	}
	long := strings.Repeat("a", maxClockKioskPathBytes+1)
	if err := ValidateClockKioskAssets(ClockKioskAssets{WASMPath: long, WASMExecPath: long}); err == nil {
		t.Fatal("unbounded kiosk paths accepted")
	}
}
