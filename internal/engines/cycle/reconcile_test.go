package cycle

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func reconciliationFixture(t *testing.T) ReconciliationRequest {
	t.Helper()
	revision, err := NewRevision(validCycle(), "rev-1", "v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	open, err := NewOpenCycle(revision)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	dimensions := []DimensionEvidence{
		{Dimension: DimensionMembers, Complete: true, EvidenceRef: "ledger:members/1", ManifestDigest: digest('a'), Detail: "all members settled"},
		{Dimension: DimensionCalculations, Complete: true, EvidenceRef: "ledger:calc/1", ManifestDigest: digest('b'), Detail: "all calculations final"},
		{Dimension: DimensionApprovals, Complete: true, EvidenceRef: "ledger:approve/1", ManifestDigest: digest('c'), Detail: "quorum reached"},
		{Dimension: DimensionEffects, Complete: true, EvidenceRef: "ledger:effects/1", ManifestDigest: digest('d'), Detail: "all effects applied"},
		{Dimension: DimensionObservations, Complete: true, EvidenceRef: "ledger:observe/1", ManifestDigest: digest('e'), Detail: "observations collected"},
	}
	return ReconciliationRequest{Cycle: open, At: time.Date(2026, 2, 2, 12, 0, 0, 0, time.UTC), Dimensions: dimensions}
}

func TestTodo_CYCLE_010(t *testing.T) {
	request := reconciliationFixture(t)
	result, err := ReconcileCompletion(request)
	if err != nil {
		t.Fatalf("reconcile completion: %v", err)
	}
	if result.Verdict != VerdictComplete || len(result.Repairs) != 0 || result.Digest == "" {
		t.Fatalf("complete cycle did not reconcile clean: %+v", result)
	}
	if len(result.Dimensions) != 5 {
		t.Fatalf("result must report each dimension, got %d", len(result.Dimensions))
	}
	seen := map[CompletionDimension]bool{}
	for _, dimension := range result.Dimensions {
		if !dimension.Complete || dimension.EvidenceRef == "" || dimension.ManifestDigest == "" {
			t.Fatalf("dimension lost evidence: %+v", dimension)
		}
		seen[dimension.Dimension] = true
	}
	for _, want := range []CompletionDimension{DimensionMembers, DimensionCalculations, DimensionApprovals, DimensionEffects, DimensionObservations} {
		if !seen[want] {
			t.Fatalf("dimension %s missing from result", want)
		}
	}
	if err := result.Verify(); err != nil {
		t.Fatalf("sealed result Verify: %v", err)
	}
}

func TestTodo_CYCLE_010_Property(t *testing.T) {
	all := []CompletionDimension{DimensionMembers, DimensionCalculations, DimensionApprovals, DimensionEffects, DimensionObservations}
	wantObligation := map[CompletionDimension]RepairObligation{
		DimensionMembers:      ObligationReconcileMembership,
		DimensionCalculations: ObligationRecalculate,
		DimensionApprovals:    ObligationReapprove,
		DimensionEffects:      ObligationReplayEffects,
		DimensionObservations: ObligationCollectObservations,
	}
	// Every non-empty incomplete subset must refuse the close and name
	// exactly the repair obligations for its incomplete dimensions.
	for mask := 1; mask < (1 << len(all)); mask++ {
		request := reconciliationFixture(t)
		var incomplete []CompletionDimension
		for i, dimension := range all {
			if mask&(1<<i) != 0 {
				request.Dimensions[i].Complete = false
				incomplete = append(incomplete, dimension)
			}
		}
		result, err := ReconcileCompletion(request)
		if !errors.Is(err, ErrCompletionIncomplete) {
			t.Fatalf("mask %05b: error = %v, want incomplete close refused", mask, err)
		}
		if result.Verdict != VerdictIncomplete {
			t.Fatalf("mask %05b: verdict = %s, want INCOMPLETE", mask, result.Verdict)
		}
		if len(result.Repairs) != len(incomplete) {
			t.Fatalf("mask %05b: %d repairs for %d incomplete dimensions", mask, len(result.Repairs), len(incomplete))
		}
		for i, dimension := range incomplete {
			repair := result.Repairs[i]
			if repair.Dimension != dimension || repair.Obligation != wantObligation[dimension] || repair.EvidenceRef == "" {
				t.Fatalf("mask %05b: repair %d = %+v", mask, i, repair)
			}
		}
	}
	// The fully complete case must close without obligations.
	request := reconciliationFixture(t)
	result, err := ReconcileCompletion(request)
	if err != nil || result.Verdict != VerdictComplete || len(result.Repairs) != 0 {
		t.Fatalf("complete: result=%+v err=%v", result, err)
	}
}

func TestTodo_CYCLE_010_Golden(t *testing.T) {
	result, err := ReconcileCompletion(reconciliationFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Digest, "sha256:25118356410f6554aa696d27d2fa55e1ba972aef0ca8cf34597232c2d683bd1b"; got != want {
		t.Fatalf("digest=%q, want %q", got, want)
	}
}

func TestTodo_CYCLE_010_Mutation(t *testing.T) {
	request := reconciliationFixture(t)
	result, err := ReconcileCompletion(request)
	if err != nil {
		t.Fatal(err)
	}
	before := result.Digest
	request.Dimensions[0].Complete = false
	request.Dimensions = append(request.Dimensions, DimensionEvidence{Dimension: DimensionMembers, Complete: true})
	if result.Digest != before || len(result.Dimensions) != 5 || !result.Dimensions[0].Complete {
		t.Fatal("sealed result changed after request mutation")
	}
	if err := result.Verify(); err != nil {
		t.Fatalf("unchanged result Verify=%v", err)
	}
	result.Dimensions[1].Complete = false
	if err := result.Verify(); !errors.Is(err, ErrCompletionInvalid) {
		t.Fatalf("mutated sealed result Verify=%v", err)
	}
}
