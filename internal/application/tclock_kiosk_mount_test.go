package application

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_TCLOCK016_KioskMountOwnsOnlyStandaloneRoutes(t *testing.T) {
	dir := t.TempDir()
	assets := ClockKioskAssets{WASMPath: filepath.Join(dir, "clock.wasm"), WASMExecPath: filepath.Join(dir, "wasm_exec.js")}
	if err := os.WriteFile(assets.WASMPath, []byte("\x00asm\x01\x00\x00\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assets.WASMExecPath, []byte("/* matching shim */"), 0600); err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	handler, err := overlayClockKiosk(next, assets)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/timeclock/", 200}, {"/timeclock/assets/timeclock.wasm", 200}, {"/timeclock/admin", 404}, {"/workspace/app/myself", 202}} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d", response.Code, tc.status)
			}
			if tc.path == "/timeclock/" && !strings.Contains(response.Body.String(), "timeclock.wasm") {
				t.Fatal("kiosk page did not load its own bundle")
			}
		})
	}
	if _, err := overlayClockKiosk(next, ClockKioskAssets{WASMPath: assets.WASMPath}); err == nil {
		t.Fatal("partial assets silently mounted")
	}
	if got, err := overlayClockKiosk(next, ClockKioskAssets{}); err != nil || got == nil {
		t.Fatalf("disabled kiosk damaged HTTP surface: %v", err)
	}
}
