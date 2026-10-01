package application

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const maxClockKioskPathBytes = 4096

const (
	EnvClockKioskWASMPath   = "HCMNEXT_CLOCK_KIOSK_WASM_PATH"
	EnvClockKioskExecPath   = "HCMNEXT_CLOCK_KIOSK_EXEC_PATH"
	FieldClockKioskWASMPath = "clock-kiosk-wasm-path"
	FieldClockKioskExecPath = "clock-kiosk-exec-path"
)

// ClockKioskFields returns the optional standalone kiosk asset paths.
func ClockKioskFields() []bootstrap.Field {
	return []bootstrap.Field{
		{Name: FieldClockKioskWASMPath, Env: EnvClockKioskWASMPath, Usage: "local path to the compiled standalone clock kiosk WASM bundle"},
		{Name: FieldClockKioskExecPath, Env: EnvClockKioskExecPath, Usage: "local path to the matching wasm_exec.js shim"},
	}
}

// ValidateClockKioskAssets validates the optional standalone kiosk assets.
// Empty paths disable the route; a partial pair is always rejected.
func ValidateClockKioskAssets(assets ClockKioskAssets) error {
	wasm := strings.TrimSpace(assets.WASMPath)
	exec := strings.TrimSpace(assets.WASMExecPath)
	if wasm == "" && exec == "" {
		return nil
	}
	if wasm == "" || exec == "" {
		return errors.New("clock kiosk requires both WASM and matching Go shim paths")
	}
	for name, path := range map[string]string{"WASM": wasm, "Go shim": exec} {
		if len(path) > maxClockKioskPathBytes || strings.Contains(path, "://") || filepath.Clean(path) == "." {
			return fmt.Errorf("clock kiosk %s path is not a bounded local path", name)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("clock kiosk %s path: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("clock kiosk %s path is not a regular file", name)
		}
	}
	return nil
}
