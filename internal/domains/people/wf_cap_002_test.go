//go:build !(js && wasm)

package people

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wfcap002Calendar() values.CalendarRef {
	return values.CalendarRef{Ref: "people.assignment.test", Version: "1"}
}

func wfcap002Date(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func wfcap002Interval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewLocalDateInterval(wfcap002Date(t, start), wfcap002Date(t, end), wfcap002Calendar())
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func wfcap002OpenInterval(t *testing.T, start string) values.EffectiveInterval {
	t.Helper()
	iv, err := values.NewOpenLocalDateInterval(wfcap002Date(t, start), wfcap002Calendar())
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func wfcap002Tenant() values.TenantId {
	return values.TenantId("22222222-2222-4222-8222-222222222222")
}

func wfcap002Keys(t *testing.T) (values.ResourceKey, values.ResourceKey) {
	t.Helper()
	tenant := wfcap002Tenant()
	assignment, err := values.NewResourceKey(tenant, values.Kind("assignment"), "worker-1", "assignment-1")
	if err != nil {
		t.Fatal(err)
	}
	employment, err := values.NewResourceKey(tenant, values.Kind("employment"), "worker-1", "employment-1")
	if err != nil {
		t.Fatal(err)
	}
	return assignment, employment
}

func wfcap002Revision(t *testing.T) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision("people.assignment/worker-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func wfcap002Subject() intent.SubjectReference {
	return intent.SubjectReference{Kind: "worker", SubjectID: "worker-1", AuthorityDomain: "people"}
}

func wfcap002RevisionFixture(t *testing.T) AssignmentRevision {
	t.Helper()
	assignment, _ := wfcap002Keys(t)
	return AssignmentRevision{
		Tenant: wfcap002Tenant(), ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal",
		ActorPrincipalID: "principal:hrbp", AuthorityDecision: "authority:1",
		WorkerID: "worker-1", AssignmentID: "assignment-1",
		ResourceKey: assignment, Subject: wfcap002Subject(),
		Effective: wfcap002Interval(t, "2026-11-01", "2027-11-01"), ExpectedRevision: wfcap002Revision(t),
		CurrentOrgUnit: "org-a", TargetOrgUnit: "org-b",
		CurrentLocation: "loc-a", TargetLocation: "loc-b",
		CurrentCostCenter: "cc-a", TargetCostCenter: "cc-b",
		CurrentJobCode: "ENG-3", TargetJobCode: "ENG-4",
		CurrentFTE: "1.00", TargetFTE: "0.80",
	}
}

func wfcap002EndFixture(t *testing.T) EndEmployment {
	t.Helper()
	assignment, employment := wfcap002Keys(t)
	return EndEmployment{
		Tenant: wfcap002Tenant(), ProposalRevisionID: "proposal-9", ProposalDigest: "sha256:separation",
		ActorPrincipalID: "principal:hrbp", AuthorityDecision: "authority:9",
		WorkerID: "worker-1", EmploymentID: "employment-1", AssignmentID: "assignment-1",
		EmploymentKey: employment, AssignmentKey: assignment, Subject: wfcap002Subject(),
		Effective: wfcap002OpenInterval(t, "2026-12-01"), ExpectedRevision: wfcap002Revision(t),
		CurrentStatus: "ACTIVE", CurrentPositionID: "position-7",
	}
}

// TestTodo_WF_CAP_002 proves people.revise_assignment revises the closed
// assignment field set as one effective-dated fenced revision, that
// people.end_employment ends employment and vacates the occupied position,
// and that the promotion mutation delegates its write set to the revision.
func TestTodo_WF_CAP_002(t *testing.T) {
	t.Run("GREEN_revise_emits_fenced_writes_in_stable_order", func(t *testing.T) {
		m := wfcap002RevisionFixture(t)
		writes, err := m.PlannedWrites()
		if err != nil {
			t.Fatal(err)
		}
		want := []struct {
			field    string
			current  string
			proposed string
		}{
			{"assignment.org_unit", "org-a", "org-b"},
			{"assignment.location", "loc-a", "loc-b"},
			{"assignment.cost_center", "cc-a", "cc-b"},
			{"assignment.job_code", "ENG-3", "ENG-4"},
			{"assignment.fte", "1.00", "0.80"},
		}
		if len(writes) != len(want) {
			t.Fatalf("writes = %+v, want %d fields", writes, len(want))
		}
		for i, w := range want {
			got := writes[i]
			if got.FieldPath != w.field || got.CurrentCanonicalText != w.current || got.ProposedCanonicalText != w.proposed {
				t.Fatalf("write %d = %+v, want %s %q->%q", i, got, w.field, w.current, w.proposed)
			}
			if got.Operation != intent.WriteOperationUpdate {
				t.Fatalf("write %d operation = %q, want UPDATE", i, got.Operation)
			}
			if got.EffectiveInterval != m.Effective {
				t.Fatalf("write %d is not fenced to the revision interval", i)
			}
			if got.ExpectedRevision != m.ExpectedRevision {
				t.Fatalf("write %d carries no baseline revision", i)
			}
			if got.SourceAuthorityDecision != m.AuthorityDecision {
				t.Fatalf("write %d carries no authority decision", i)
			}
		}
	})

	t.Run("GREEN_revise_partial_changes_only", func(t *testing.T) {
		m := wfcap002RevisionFixture(t)
		m.TargetOrgUnit, m.TargetLocation, m.TargetCostCenter, m.TargetFTE = "", "", "", ""
		writes, err := m.PlannedWrites()
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != 1 || writes[0].FieldPath != "assignment.job_code" {
			t.Fatalf("writes = %+v, want only the job change", writes)
		}
	})

	t.Run("GREEN_end_ends_employment_and_vacates_position", func(t *testing.T) {
		m := wfcap002EndFixture(t)
		writes, err := m.PlannedWrites()
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != 2 {
			t.Fatalf("writes = %+v, want the status end and the position vacate", writes)
		}
		end := writes[0]
		if end.FieldPath != "employment.status" || end.CurrentCanonicalText != "ACTIVE" || end.ProposedCanonicalText != "ENDED" {
			t.Fatalf("end write = %+v, want employment.status ACTIVE->ENDED", end)
		}
		if end.Operation != intent.WriteOperationUpdate || end.EffectiveInterval != m.Effective {
			t.Fatalf("end write is not a fenced UPDATE: %+v", end)
		}
		vacate := writes[1]
		if vacate.FieldPath != "assignment.position_id" || vacate.CurrentCanonicalText != "position-7" {
			t.Fatalf("vacate write = %+v, want the occupied position removed", vacate)
		}
		if vacate.Operation != intent.WriteOperationDelete || vacate.EffectiveInterval != m.Effective {
			t.Fatalf("vacate write is not a fenced DELETE: %+v", vacate)
		}
		if vacate.ProposedCanonicalText != "" {
			t.Fatalf("a DELETE carries no proposed text, got %q", vacate.ProposedCanonicalText)
		}
	})

	t.Run("GREEN_promotion_delegates_to_the_revision", func(t *testing.T) {
		promotion := promotionMutationFixture(t)
		promotion.Effective = wfcap002Interval(t, "2026-11-01", "2027-11-01")
		writes, err := promotion.PlannedWrites()
		if err != nil {
			t.Fatal(err)
		}
		revision := promotion.assignmentRevision()
		changes, err := revision.ChangedFields()
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != len(changes) {
			t.Fatalf("promotion writes %d fields but the revision projects %d", len(writes), len(changes))
		}
		for i := range writes {
			if writes[i].FieldPath != changes[i].FieldPath ||
				writes[i].CurrentCanonicalText != changes[i].CurrentCanonicalText ||
				writes[i].ProposedCanonicalText != changes[i].ProposedCanonicalText {
				t.Fatalf("promotion write %d = %+v, revision projects %+v", i, writes[i], changes[i])
			}
		}
	})

	t.Run("RED_rejects_invalid_mutations", func(t *testing.T) {
		revision := wfcap002RevisionFixture(t)
		revision.TargetOrgUnit, revision.TargetLocation, revision.TargetCostCenter, revision.TargetJobCode, revision.TargetFTE = "org-a", "loc-a", "cc-a", "ENG-3", "1.00"
		if err := revision.Validate(); !errors.Is(err, ErrAssignmentRevisionNoChange) {
			t.Fatalf("unchanged revision Validate = %v, want ErrAssignmentRevisionNoChange", err)
		}
		revision = wfcap002RevisionFixture(t)
		revision.Subject.Kind = "employment"
		if err := revision.Validate(); err == nil {
			t.Fatal("revision accepted a non-worker subject")
		}
		revision = wfcap002RevisionFixture(t)
		promotion := promotionMutationFixture(t)
		revision.Effective = promotion.Effective
		if err := revision.Validate(); err == nil {
			t.Fatal("revision accepted a non-LOCAL_DATE interval")
		}
		end := wfcap002EndFixture(t)
		end.CurrentStatus = "ENDED"
		if err := end.Validate(); !errors.Is(err, ErrEmploymentAlreadyEnded) {
			t.Fatalf("re-ended employment Validate = %v, want ErrEmploymentAlreadyEnded", err)
		}
		end = wfcap002EndFixture(t)
		end.ExpectedRevision = values.RevisionToken{}
		if err := end.Validate(); err == nil {
			t.Fatal("end accepted a missing baseline revision")
		}
	})

	t.Run("GREEN_explain_names_fields_not_values", func(t *testing.T) {
		revision := wfcap002RevisionFixture(t)
		explained := revision.Explain()
		for _, field := range []string{"org_unit", "location", "cost_center", "job_code", "fte"} {
			if !strings.Contains(explained, field) {
				t.Fatalf("explanation %q names no %s", explained, field)
			}
		}
		for _, secret := range []string{"org-b", "loc-b", "cc-b", "ENG-4", "0.80"} {
			if strings.Contains(explained, secret) {
				t.Fatalf("explanation %q leaks proposed value %q", explained, secret)
			}
		}
		end := wfcap002EndFixture(t)
		if got := end.Explain(); got == "" {
			t.Fatal("end employment has no explanation")
		}
	})
}

// TestTodo_WF_CAP_002_Property proves assignment revisions never overlap in
// effective time: the chain accepts only abutting or disjoint intervals, and
// every refusal names a revision it would overlap.
func TestTodo_WF_CAP_002_Property(t *testing.T) {
	cal := wfcap002Calendar()
	for seed := uint64(1); seed <= 64; seed++ {
		rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		var chain AssignmentRevisionChain
		accepted := 0
		attempts := 64 + rng.IntN(64)
		for i := 0; i < attempts; i++ {
			startDay := 1 + rng.IntN(28)
			start, err := values.ParseLocalDate(sprintfDay(startDay))
			if err != nil {
				t.Fatal(err)
			}
			var next values.EffectiveInterval
			if rng.IntN(4) == 0 {
				next, err = values.NewOpenLocalDateInterval(start, cal)
			} else {
				length := rng.IntN(45)
				end, err := values.ParseLocalDate(sprintfDay(startDay + length))
				if err != nil {
					t.Fatal(err)
				}
				if end.Compare(start) <= 0 {
					continue
				}
				next, err = values.NewLocalDateInterval(start, end, cal)
			}
			if err != nil {
				t.Fatal(err)
			}
			grown, err := chain.Append(next)
			if err != nil {
				if !errors.Is(err, ErrAssignmentRevisionOverlaps) {
					t.Fatalf("seed %d: Append = %v, want ErrAssignmentRevisionOverlaps", seed, err)
				}
				overlapped := false
				for _, have := range chain {
					overlap, oerr := have.Overlaps(next)
					if oerr != nil {
						t.Fatal(oerr)
					}
					overlapped = overlapped || overlap
				}
				if !overlapped {
					t.Fatalf("seed %d: refused %+v but it overlaps nothing accepted", seed, next)
				}
				continue
			}
			chain, accepted = grown, accepted+1
		}
		if accepted == 0 {
			t.Fatalf("seed %d: accepted nothing", seed)
		}
		for i := range chain {
			for j := i + 1; j < len(chain); j++ {
				overlap, err := chain[i].Overlaps(chain[j])
				if err != nil {
					t.Fatal(err)
				}
				if overlap {
					t.Fatalf("seed %d: accepted revisions %d and %d overlap", seed, i, j)
				}
			}
		}
	}
}

func sprintfDay(day int) string {
	month, d := 1+((day-1)/28), 1+((day-1)%28)
	return fmt.Sprintf("2026-%02d-%02d", month, d)
}
