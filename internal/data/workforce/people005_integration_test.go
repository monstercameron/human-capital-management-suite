package workforce_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PEOPLE_005_Integration exercises the governed explanation over a
// row actually committed to PostgreSQL and read through the app-role,
// tenant-scoped workforce adapter.
func TestTodo_PEOPLE_005_Integration(t *testing.T) {
	t.Parallel()
	db, tenant, row := seedWorker(t, "people-005-explanation")
	worker := values.EntityRef{Tenant: factsTenant, Kind: people.KindWorker, Id: row.WorkerID.String()}
	facts := workforce.NewFacts(appBeginner(t, db), tenantMap(tenant))
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("effective date: %v", err)
	}
	known, err := values.NewKnownAt(values.NewInstant(fixedInstant))
	if err != nil {
		t.Fatalf("known-at: %v", err)
	}
	fields := []people.FieldID{people.FieldLegalName, people.FieldGrade}
	got, err := people.ExplainWorkerState(context.Background(), facts, people.ExplainWorkerStateRequest{
		Tenant: factsTenant, Worker: worker,
		AsOf:          people.AsOf{EffectiveOn: effective, KnownAt: known},
		Fields:        fields,
		Authorization: fixtures.AllowAll("people-005-integration/1", "integration-test", fields),
	})
	if err != nil {
		t.Fatalf("ExplainWorkerState over PostgreSQL: %v", err)
	}
	if got.Disclosure != people.DisclosureFull || got.Presence != people.SubjectPresent {
		t.Fatalf("disclosure/presence = %s/%s, want FULL/PRESENT", got.Disclosure, got.Presence)
	}
	if value, ok := got.Value(people.FieldLegalName); !ok || value != row.LegalName {
		t.Fatalf("explained legal name = %q (present=%v), want %q", value, ok, row.LegalName)
	}
	grade, ok := got.Value(people.FieldGrade)
	if !ok || grade != row.Grade {
		t.Fatalf("explained grade = %q (present=%v), want %q", grade, ok, row.Grade)
	}
	for _, field := range got.Fields {
		if field.Access != people.AccessAuthorized || !field.Revision.IsSpecified() || field.Provenance.EvidenceRef != workforce.EvidenceRef(row) {
			t.Errorf("field %s lacks persisted source evidence: %+v", field.Field, field)
		}
	}
	if !got.Effects.IsZero() || got.Receipt.ExecutionState != evidence.ExecutionStateNotPlanned {
		t.Fatalf("read integration emitted effects: %v; receipt=%+v", got.Effects.NonZero(), got.Receipt)
	}
}
