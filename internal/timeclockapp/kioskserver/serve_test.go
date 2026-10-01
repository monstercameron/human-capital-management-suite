package kioskserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTodo_TCLOCK_016_ShellAssetsAndCSP(t *testing.T) {
	h, err := New(Config{WASM: []byte("wasm"), WASMExec: []byte("exec")})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "http://kiosk.local/timeclock/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("shell status=%d csp=%q", w.Code, w.Header().Get("Content-Security-Policy"))
	}
	if !strings.Contains(w.Body.String(), "/timeclock/assets/timeclock.wasm") || strings.Contains(w.Body.String(), "/workspace") {
		t.Fatalf("unsafe shell: %s", w.Body.String())
	}
	for _, path := range []string{"/timeclock/assets/timeclock.wasm", "/timeclock/assets/wasm_exec.js", "/timeclock/assets/timeclock.css"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://kiosk.local"+path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("asset %s status=%d", path, w.Code)
		}
	}
}

func TestTodo_TCLOCK_016_DeviceAllowlistAndBodyBound(t *testing.T) {
	var gotPath string
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("X-Tenant") != "" {
			t.Error("forwarded unsigned tenant header")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h, err := New(Config{Upstream: upstream})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		method string
		want   int
	}{
		{"/timeclock/hcmnext.time.v1.ClockDeviceService/Heartbeat", http.MethodPost, http.StatusNoContent},
		{"/timeclock/hcmnext.time.v1.ClockDeviceService/CreateEnrollmentCode", http.MethodPost, http.StatusNotFound},
		{"/timeclock/hcmnext.time.v1.ClockDeviceService/Heartbeat/x", http.MethodPost, http.StatusNotFound},
		{"/timeclock/hcmnext.time.v1.ClockDeviceService/Heartbeat", http.MethodGet, http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, "http://kiosk.local"+tc.path, nil)
		req.Header.Set("X-Tenant", "forged")
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s %s status=%d want=%d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if gotPath != "/v1/time/clock-device/Heartbeat" {
		t.Fatalf("upstream path=%q", gotPath)
	}
	large := strings.NewReader(strings.Repeat("x", maxBody+1))
	r := httptest.NewRequest(http.MethodPost, "http://kiosk.local/timeclock/hcmnext.time.v1.ClockDeviceService/Heartbeat", large)
	r.ContentLength = maxBody + 1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body status=%d", w.Code)
	}
}

func TestTodo_TCLOCK_016_ConfigAndUpstreamProxy(t *testing.T) {
	if _, err := New(Config{Upstream: http.NotFoundHandler(), UpstreamURL: mustURL(t, "http://127.0.0.1")}); err == nil {
		t.Fatal("accepted two upstreams")
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "hello" {
			t.Errorf("body=%q", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer up.Close()
	u := mustURL(t, up.URL)
	h, err := New(Config{UpstreamURL: u})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://kiosk.local/timeclock/hcmnext.time.v1.ClockDeviceService/Heartbeat", strings.NewReader("hello"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("proxy status=%d", w.Code)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
