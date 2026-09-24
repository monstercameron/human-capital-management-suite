package admin

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

func TestTodo_REV_009_03(t *testing.T) {
	want := []inspect.RecordFamily{
		{Family: inspect.FamilyTimer, Section: inspect.SectionNode, State: inspect.RecordLoaded, Count: 2},
		{Family: inspect.FamilyLease, Section: inspect.SectionInstance, State: inspect.RecordNotRecorded},
		{Family: inspect.FamilyCheckpoint, Section: inspect.SectionInstance, State: inspect.RecordIntegrityFailed, Reason: "CHECKPOINT_DIGEST_MISMATCH"},
		{Family: inspect.FamilyOutbox, Section: inspect.SectionConnector, State: inspect.RecordRedacted, Reason: "NO_CONNECTOR_SCOPE"},
		{Family: inspect.FamilyReconciliation, Section: inspect.SectionObservation, State: inspect.RecordUnavailable, Reason: "NO_RECONCILIATION_STORE"},
	}
	got := toDurableRecordProfiles(want)
	if len(got) != len(want) {
		t.Fatalf("manifest family count = %d, want %d", len(got), len(want))
	}
	for i, record := range got {
		if record.GetFamily() != want[i].Family || record.GetSection() != string(want[i].Section) ||
			record.GetState() != string(want[i].State) || record.GetCount() != int32(want[i].Count) || record.GetReason() != want[i].Reason {
			t.Errorf("manifest[%d] = %+v, want %+v", i, record, want[i])
		}
	}
}
