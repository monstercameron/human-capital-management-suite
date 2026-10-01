package clockpartner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	timeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
)

func TestTodo_TCLOCK_018_OpenAPIFromGeneratedDescriptors(t *testing.T) {
	document, err := timeclock.OpenAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	text := string(document)
	for _, value := range []string{"hcmnext.time.v1.ClockDeviceService", "hcmnext.time.v1.WorkerClockService", "hcmnext.time.v1.MissingPunchService", "/v1/time/clock-device/SubmitPunches", "/v1/time/self/in", "/v1/time/missing-punch/SubmitCorrection", "ConnectError", "x-hcmnext-max-request-bytes"} {
		if !strings.Contains(text, value) {
			t.Fatalf("generated contract is missing %q", value)
		}
	}
	if !strings.Contains(text, "DevicePunch") || !strings.Contains(text, "MISSING_PUNCH_DECISION_APPROVED") {
		t.Fatal("generated descriptor schemas or enum values are missing")
	}
}

func TestTodo_TCLOCK_018_OpenAPIServedIntegration(t *testing.T) {
	handler, err := timeclock.OpenAPIHandler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	document, err := FetchClockOpenAPI(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(document) == 0 {
		t.Fatal("served contract is empty")
	}
	if _, err := FetchClockOpenAPI(context.Background(), server.URL+timeclock.OpenAPIPath); err != nil {
		t.Fatalf("explicit served contract path: %v", err)
	}
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	if _, err := FetchClockOpenAPI(context.Background(), bad.URL); !strings.Contains(err.Error(), "contract endpoint returned HTTP 404") {
		t.Fatalf("missing served endpoint error = %v", err)
	}
}
