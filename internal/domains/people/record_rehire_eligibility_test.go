//go:build !(js && wasm)

package people

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev078RecordFixture(t *testing.T) RecordRehireEligibility {
	t.Helper()
	assignment, employment := wfcap002Keys(t)
	_ = assignment
	return RecordRehireEligibility{
		Tenant: wfcap002Tenant(), ProposalRevisionID: "proposal-9", ProposalDigest: "sha256:separation",
		ActorPrincipalID: "principal:hrbp", AuthorityDecision: "authority:9",
		WorkerID: "worker-1", EmploymentID: "employment-1",
		EmploymentKey: employment, Subject: wfcap002Subject(),
		Effective:        wfcap002OpenInterval(t, "2026-12-01"),
		ExpectedRevision: wfcap002Revision(t),
		Status:           RehireNotEligible, Reason: RehireReasonMisconduct,
		EffectiveAt: wfcap002Date(t, "2026-12-01"), Revision: 1,
	}
}

// TestTodo_REV_078_01_Record proves the people.record_rehire_eligibility
// capability: the first decision emits fenced CREATE writes for status,
// reason and effective date; a correction emits UPDATE writes against the
// current head with a higher revision; every invalid command is refused;
// Explain never leaks the reason.
func TestTodo_REV_078_01_Record(t *testing.T) {
	if RecordRehireEligibilityIntentType != "hcmnext.people.record_rehire_eligibility/v1" {
		t.Fatalf("intent type = %q", RecordRehireEligibilityIntentType)
	}
	for field, want := range map[string]string{
		RehireStatusField: "employment.rehire_status", RehireReasonField: "employment.rehire_reason",
		RehireEffectiveField: "employment.rehire_effective",
	} {
		if field != want {
			t.Fatalf("field path = %q, want %q", field, want)
		}
	}

	t.Run("GREEN_first_record_creates_three_fields", func(t *testing.T) {
		m := rev078RecordFixture(t)
		writes, err := m.PlannedWrites()
		if err != nil {
			t.Fatal(err)
		}
		want := []struct{ field, proposed string }{
			{RehireStatusField, "NOT_ELIGIBLE"},
			{RehireReasonField, "MISCONDUCT"},
			{RehireEffectiveField, "2026-12-01"},
		}
		if len(writes) != len(want) {
			t.Fatalf("writes = %+v, want 3 fields", writes)
		}
		for i, w := range want {
			got := writes[i]
			if got.FieldPath != w.field || got.ProposedCanonicalText != w.proposed || got.CurrentCanonicalText != "" {
				t.Fatalf("write %d = %+v, want CREATE %s=%q", i, got, w.field, w.proposed)
			}
			if got.Operation != intent.WriteOperationCreate || got.EffectiveInterval != m.Effective {
				t.Fatalf("write %d is not a fenced CREATE: %+v", i, got)
			}
			if got.ExpectedRevision != m.ExpectedRevision || got.SourceAuthorityDecision != m.AuthorityDecision {
				t.Fatalf("write %d carries no baseline or authority: %+v", i, got)
			}
			if got.Subject != m.Subject || !got.ResourceKey.Equal(m.EmploymentKey) {
				t.Fatalf("write %d targets %+v/%+v, want the separated employment", i, got.Subject, got.ResourceKey)
			}
		}
	})

	t.Run("GREEN_correction_updates_against_current_head", func(t *testing.T) {
		m := rev078RecordFixture(t)
		m.Revision = 2
		m.Status, m.Reason = RehireConditional, RehireReasonPolicy
		m.CurrentStatus, m.CurrentReason, m.CurrentEffective = "NOT_ELIGIBLE", "MISCONDUCT", "2026-12-01"
		writes, err := m.PlannedWrites()
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != 3 {
			t.Fatalf("writes = %+v, want 3 updates", writes)
		}
		if writes[0].Operation != intent.WriteOperationUpdate || writes[0].CurrentCanonicalText != "NOT_ELIGIBLE" || writes[0].ProposedCanonicalText != "CONDITIONAL" {
			t.Fatalf("status update = %+v", writes[0])
		}
		if writes[1].CurrentCanonicalText != "MISCONDUCT" || writes[1].ProposedCanonicalText != "POLICY_REVIEW" {
			t.Fatalf("reason update = %+v", writes[1])
		}
		if writes[2].CurrentCanonicalText != "2026-12-01" || writes[2].ProposedCanonicalText != "2026-12-01" {
			t.Fatalf("effective update = %+v", writes[2])
		}
	})

	t.Run("RED_rejects_invalid_commands", func(t *testing.T) {
		base := rev078RecordFixture(t)
		cases := map[string]func(*RecordRehireEligibility){
			"missing authority":       func(m *RecordRehireEligibility) { m.AuthorityDecision = "" },
			"non-worker subject":      func(m *RecordRehireEligibility) { m.Subject.Kind = "employment" },
			"subject not the worker":  func(m *RecordRehireEligibility) { m.Subject.SubjectID = "worker-2" },
			"status reason mismatch":  func(m *RecordRehireEligibility) { m.Reason = RehireReasonGoodStanding },
			"unknown reason":          func(m *RecordRehireEligibility) { m.Reason = "BECAUSE" },
			"zero revision":           func(m *RecordRehireEligibility) { m.Revision = 0 },
			"first record with head":  func(m *RecordRehireEligibility) { m.CurrentStatus = "ELIGIBLE" },
			"correction without head": func(m *RecordRehireEligibility) { m.Revision = 2 },
			"missing baseline":        func(m *RecordRehireEligibility) { m.ExpectedRevision = values.RevisionToken{} },
		}
		for name, mutate := range cases {
			m := base
			mutate(&m)
			if err := m.Validate(); err == nil {
				t.Fatalf("%s: invalid command validated", name)
			} else if !errors.Is(err, ErrRecordRehireEligibilityInvalid) && !errors.Is(err, ErrRecordRehireEligibilityNoHead) {
				t.Fatalf("%s: err = %v, want a typed record refusal", name, err)
			}
			if _, err := m.PlannedWrites(); err == nil {
				t.Fatalf("%s: invalid command planned writes", name)
			}
		}
		closed := base
		closed.Effective = wfcap002Interval(t, "2026-12-01", "2026-12-02")
		if _, err := closed.PlannedWrites(); err != nil {
			t.Fatalf("bounded fencing interval refused: %v", err)
		}
		instant, err := values.NewInstantInterval(values.NewInstant(time.Unix(0, 0).UTC()), values.NewInstant(time.Unix(1, 0).UTC()))
		if err != nil {
			t.Fatal(err)
		}
		stamped := base
		stamped.Effective = instant
		if err := stamped.Validate(); !errors.Is(err, ErrRecordRehireEligibilityInvalid) {
			t.Fatalf("instant interval err = %v, want a typed record refusal", err)
		}
	})

	t.Run("GREEN_explain_names_fields_not_reasons", func(t *testing.T) {
		m := rev078RecordFixture(t)
		explained := m.Explain()
		for _, field := range []string{"rehire_status", "rehire_reason", "rehire_effective", "worker-1", "employment-1"} {
			if !strings.Contains(explained, field) {
				t.Fatalf("explanation %q names no %s", explained, field)
			}
		}
		for _, secret := range []string{"MISCONDUCT", "NOT_ELIGIBLE", "2026-12-01"} {
			if strings.Contains(explained, secret) {
				t.Fatalf("explanation %q leaks %q", explained, secret)
			}
		}
		if explained == "" {
			t.Fatal("record has no explanation")
		}
	})
}
