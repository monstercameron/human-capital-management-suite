package journey

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_REV_009_03_JourneyDurableManifest(t *testing.T) {
	detail := workspace.JourneyDetail{DurableRecords: []workspace.JourneyRecordFamily{
		{Family: "workflow_timer", Section: "NODE", State: "LOADED", Count: 2},
		{Family: "workflow_lease", Section: "INSTANCE", State: "NOT_RECORDED"},
		{Family: "workflow_checkpoint", Section: "INSTANCE", State: "REDACTED", Reason: "NO_INSTANCE_SCOPE"},
		{Family: "outbox", Section: "CONNECTOR", State: "INTEGRITY_FAILED", Reason: "OUTBOX_DIGEST_MISMATCH"},
		{Family: "effect_reconciliation_job", Section: "OBSERVATION", State: "UNAVAILABLE", Reason: "NO_RECONCILIATION_STORE"},
	}}
	shown := toDetail(detail, true).GetDurableRecords()
	if len(shown) != len(detail.DurableRecords) {
		t.Fatalf("durable manifest entries = %d, want %d", len(shown), len(detail.DurableRecords))
	}
	for i, want := range detail.DurableRecords {
		got := shown[i]
		if got.GetFamily() != want.Family || got.GetSection() != want.Section || got.GetState() != want.State ||
			got.GetCount() != want.Count || got.GetReason() != want.Reason {
			t.Errorf("durable manifest[%d] = %+v, want %+v", i, got, want)
		}
	}
	if hidden := toDetail(detail, false).GetDurableRecords(); len(hidden) != 0 {
		t.Fatalf("unauthorized diagnostics exposed %d durable manifest entries", len(hidden))
	}
}
