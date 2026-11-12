package operationobservation

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The verdict strings are persisted and compared on the wire, so each one is
// pinned rather than trusted to its constant name.
func TestVerdictValuesArePinned(t *testing.T) {
	cases := map[ObservationVerdict]string{
		ObservationApplied:    "APPLIED",
		ObservationNotApplied: "NOT_APPLIED",
		ObservationConflict:   "CONFLICT",
		ObservationUnknown:    "UNKNOWN",
	}
	for verdict, want := range cases {
		if string(verdict) != want {
			t.Errorf("verdict %q, want %q", verdict, want)
		}
	}
}

func TestObservationCarriesEveryFieldUnchanged(t *testing.T) {
	obsID, opID := uuid.New(), uuid.New()
	at := time.Date(2026, 11, 11, 10, 30, 0, 0, time.UTC)
	obs := Observation{
		TenantID: "tenant-1", ObservationID: obsID, OperationID: opID,
		ExternalResourceKey: "resource-7", Verdict: ObservationConflict,
		ExternalObjectRef: "ref-9", ExternalVersion: "v3", ObservedDigest: "sha256:ff",
		ObservedAt: at, Authority: "provider",
	}
	if obs.ObservationID != obsID || obs.OperationID != opID || obs.Verdict != ObservationConflict {
		t.Fatalf("identity or verdict lost: %+v", obs)
	}
	if !obs.ObservedAt.Equal(at) || obs.Authority != "provider" || obs.ExternalVersion != "v3" {
		t.Fatalf("time or authority lost: %+v", obs)
	}
	if (Observation{}).ObservationID != uuid.Nil {
		t.Fatal("zero observation has a non-nil id")
	}
}
