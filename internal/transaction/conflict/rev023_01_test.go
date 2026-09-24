package conflict_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

func TestTodo_REV_023_01(t *testing.T) {
	resource, err := values.NewResourceKey(values.TenantId("00000000-0000-0000-0000-000000000001"), values.Kind("assignment"), "worker", "9001")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewInstantInterval(
		values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("stream:assignment:9001", 17)
	if err != nil {
		t.Fatal(err)
	}
	original := conflict.WriteFootprint{
		Resource: resource, Field: conflict.FieldPath("employment.assignment.position"),
		Interval: interval, Operation: conflict.OperationUpdate,
		ExpectedRevision: revision,
		Authority:        conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local/v1"},
	}
	baseline, err := conflict.BaselineFromFootprint(original)
	if err != nil {
		t.Fatalf("convert footprint to baseline: %v", err)
	}
	got, err := baseline.Footprint()
	if err != nil {
		t.Fatalf("convert baseline to footprint: %v", err)
	}
	if string(got.Canonical()) != string(original.Canonical()) {
		t.Fatalf("round-trip footprint differs:\n got  %s\n want %s", got.Canonical(), original.Canonical())
	}

	pinned := conflict.WriteIntent{TenantID: "tenant-1", ID: "approved", ProposalID: "proposal-1", SnapshotDigest: "snapshot-1", Footprints: []conflict.WriteFootprint{original}}
	competing := conflict.WriteIntent{TenantID: "tenant-1", ID: "later", ProposalID: "proposal-2", SnapshotDigest: "snapshot-2", Footprints: []conflict.WriteFootprint{got}}
	preflight, err := conflict.ReevaluatePreflight(conflict.ReevaluateRequest{
		PolicyVersion: "conflict-preflight/v1", Pinned: []conflict.WriteIntent{pinned},
		Current: []conflict.WriteIntent{pinned, competing},
	})
	if err != nil {
		t.Fatalf("reevaluate approval-pinned footprint against current set: %v", err)
	}
	if preflight.Verdict != conflict.PreflightBlocked || len(preflight.Overlaps) != 1 {
		t.Fatalf("post-approval collision preflight = %+v, want BLOCKED with one overlap", preflight)
	}
}

func TestTodo_REV_023_01_Mutation(t *testing.T) {
	resource, err := values.NewResourceKey(values.TenantId("00000000-0000-0000-0000-000000000001"), values.Kind("assignment"), "worker", "9001")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewInstantInterval(
		values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("stream:assignment:9001", 17)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := conflict.BaselineFromFootprint(conflict.WriteFootprint{
		Resource: resource, Field: conflict.FieldPath("employment.assignment.position"),
		Interval: interval, Operation: conflict.OperationUpdate,
		ExpectedRevision: revision,
		Authority:        conflict.AuthorityScope{Domain: "PEOPLE", PolicyRef: "authority.local/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseline.ResourceCanonical = "resource:malformed"
	if _, err := baseline.Footprint(); !errors.Is(err, conflict.ErrInvalidFootprint) {
		t.Fatalf("tampered resource conversion error = %v, want invalid footprint", err)
	}
}
