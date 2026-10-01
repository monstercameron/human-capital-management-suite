package application

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/timeclockapp/kioskserver"
)

// ClockKioskAssets names the compiled kiosk bundle and its matching Go shim.
// Empty paths leave the standalone kiosk route unavailable until assets are configured.
type ClockKioskAssets struct {
	WASMPath     string
	WASMExecPath string
}

func overlayClockKiosk(next http.Handler, assets ClockKioskAssets) (http.Handler, error) {
	if assets.WASMPath == "" && assets.WASMExecPath == "" {
		return next, nil
	}
	if assets.WASMPath == "" || assets.WASMExecPath == "" {
		return nil, fmt.Errorf("clock kiosk requires both WASM and matching Go shim")
	}
	wasm, err := os.ReadFile(filepath.Clean(assets.WASMPath))
	if err != nil {
		return nil, fmt.Errorf("read clock kiosk bundle: %w", err)
	}
	shim, err := os.ReadFile(filepath.Clean(assets.WASMExecPath))
	if err != nil {
		return nil, fmt.Errorf("read clock kiosk Go shim: %w", err)
	}
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" || len(shim) == 0 {
		return nil, fmt.Errorf("clock kiosk assets are invalid")
	}
	kiosk, err := kioskserver.New(kioskserver.Config{Upstream: next, WASM: wasm, WASMExec: shim})
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/timeclock" || strings.HasPrefix(r.URL.Path, "/timeclock/") {
			kiosk.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
