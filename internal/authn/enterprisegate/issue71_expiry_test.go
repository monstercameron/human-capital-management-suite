package enterprisegate

import (
	"testing"
	"time"
)

// Issue 71: fixtures dated in the past must not silently decide the real-clock
// path. Expired evidence still blocks approval, and a record whose evidence is
// valid relative to the clock in use is approved at that clock.
func TestIssue71_ExpiredEvidenceStillBlocks(t *testing.T) {
	record := completeRecord()
	valid, err := EvaluateAt(record, gateNow)
	if err != nil || valid.Status != GateApproved {
		t.Fatalf("at the fixture clock: %+v %v", valid, err)
	}
	expired, err := EvaluateAt(record, gateNow.Add(31*24*time.Hour))
	if err != nil || expired.Status == GateApproved {
		t.Fatalf("expired evidence was approved: %+v %v", expired, err)
	}
	// The real clock decides Evaluate: the stale fixture is blocked, a record
	// that expires 30 days after now is approved, whatever today's date is.
	if stale, err := Evaluate(record); err != nil || stale.Status == GateApproved {
		t.Fatalf("stale fixture approved by the real clock: %+v %v", stale, err)
	}
	live := completeRecord()
	for i := range live.Evidence {
		live.Evidence[i].ExpiresAt = time.Now().UTC().Add(30 * 24 * time.Hour)
	}
	if got, err := Evaluate(live); err != nil || got.Status != GateApproved {
		t.Fatalf("fresh fixture on the real clock: %+v %v", got, err)
	}
}
