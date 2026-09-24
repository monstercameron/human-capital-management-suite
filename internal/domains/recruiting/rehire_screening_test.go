package recruiting

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_REV_078_01_Conformance(t *testing.T) {
	date, err := values.NewLocalDate(2026, time.March, 1)
	if err != nil {
		t.Fatal(err)
	}
	record := people.RehireEligibility{
		Worker:     values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"},
		Employment: values.EntityRef{Tenant: "tenant-a", Kind: "employment", Id: "00000000-0000-4000-8000-000000000002"},
		Status:     people.RehireNotEligible, Reason: people.RehireReasonMisconduct,
		EffectiveAt: date, RecordedAt: date, Revision: 1, AuthorityRef: "authority:exit-decision-1",
	}
	before := append([]byte(nil), record.Canonical()...)
	for _, role := range []RehireViewerRole{RehireViewerHR, RehireViewerRecruiter} {
		view, err := ReadRehireScreening(record, role)
		if err != nil || view.Status != record.Status || view.Reason != record.Reason {
			t.Fatalf("%s view = %+v, err=%v", role, view, err)
		}
	}
	candidate, err := ReadRehireScreening(record, RehireViewerCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != record.Status || candidate.Reason != "" {
		t.Fatalf("candidate view exposed raw reason or hid status: %+v", candidate)
	}
	if string(before) != string(record.Canonical()) {
		t.Fatal("read-only screening changed the people-owned eligibility record")
	}
	if _, err := ReadRehireScreening(record, "UNKNOWN"); err == nil {
		t.Fatal("unknown role received a rehire screening view")
	}
}
