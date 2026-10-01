package timeclock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failingApp struct {
	*parityApp
	err error
}

func (a *failingApp) SubmitPunches(_ context.Context, _ *trust.Principal, _ clockservice.BatchRequest) (clockservice.BatchResponse, error) {
	return clockservice.BatchResponse{}, a.err
}

func TestHTTPErrorProjectionUsesCanonicalJSONAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
		grpc string
	}{
		{name: "permission denied", err: status.Error(codes.PermissionDenied, "device is not allowed"), code: http.StatusForbidden, grpc: "permission_denied"},
		{name: "invalid argument", err: status.Error(codes.InvalidArgument, "bad request"), code: http.StatusBadRequest, grpc: "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &failingApp{parityApp: &parityApp{}, err: tc.err}
			server := httptest.NewServer(withPrincipal(New(app).HTTPHandler(), t))
			defer server.Close()
			resp, err := server.Client().Post(server.URL+"/v1/time/clock-device/SubmitPunches", "application/json", strings.NewReader(`{"device_id":"dev","punches":[{"device_sequence":1,"event_type":"PUNCH_EVENT_TYPE_CLOCK_IN","worker":{"punch_token":"token"},"device_occurred_at":"1970-01-01T00:00:01Z"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.code {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			if !strings.Contains(string(body), `"code":"`+tc.grpc+`"`) || !strings.Contains(string(body), `"message":"`) {
				t.Fatalf("body=%s want code/message JSON", body)
			}
			if strings.Contains(string(body), `"Code"`) || strings.Contains(string(body), `"Message"`) {
				t.Fatalf("non-canonical keys in body=%s", body)
			}
		})
	}
}

func TestHTTPBoundaryUsesCanonicalPrefixAndJSONErrors(t *testing.T) {
	h := New(&parityApp{}).HTTPHandler()
	for _, tc := range []struct {
		name, method, path string
		status             int
		code               string
	}{
		{name: "legacy prefix", method: http.MethodPost, path: "/hcmnext.time.v1.ClockDeviceService/Heartbeat", status: http.StatusNotFound, code: "not_found"},
		{name: "bare method", method: http.MethodPost, path: "/Heartbeat", status: http.StatusNotFound, code: "not_found"},
		{name: "method", method: http.MethodGet, path: "/v1/time/clock-device/Heartbeat", status: http.StatusMethodNotAllowed, code: "unimplemented"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHTTPBodyLimitIsOneMiB(t *testing.T) {
	h := New(&parityApp{}).HTTPHandler()
	r := httptest.NewRequest(http.MethodPost, "/v1/time/clock-device/Heartbeat", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"resource_exhausted"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func withPrincipal(h http.Handler, t *testing.T) http.Handler {
	t.Helper()
	p := testPrincipal(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(trust.WithPrincipal(r.Context(), p)))
	})
}
