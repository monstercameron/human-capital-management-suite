package clockpartner

import (
	"errors"
	"testing"
)

func TestTodo_TCLOCK_018_Conformance_OpenAPI(t *testing.T) {
	document := []byte("paths: {}\nx-hcmnext-service: hcmnext.time.v1.ClockDeviceService\nCreateEnrollmentCode: {}\nEnrollDevice: {}\nRevokeDevice: {}\nSyncRoster: {}\nIdentifyWorker: {}\nSubmitPunches: {}\nHeartbeat: {}\nrequestBody: {}\nexample: {}")
	if err := CheckOpenAPI(document); err != nil {
		t.Fatal(err)
	}
	if err := CheckOpenAPI([]byte("openapi: 3.1.0")); !errors.Is(err, ErrConformance) {
		t.Fatalf("missing contract accepted: %v", err)
	}
}
