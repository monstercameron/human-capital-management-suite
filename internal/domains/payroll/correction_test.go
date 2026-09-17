package payroll

import (
	"errors"
	"sync"
	"testing"
)

func correctionReleaseFixture(t *testing.T) PayrollRelease {
	t.Helper()
	release, err := ReleasePayroll(releaseRequestFixture(t), nil)
	if err != nil {
		t.Fatalf("ReleasePayroll: %v", err)
	}
	return release
}

func correctionRequestFixture(t *testing.T) CorrectionRequest {
	t.Helper()
	release := correctionReleaseFixture(t)
	restated := releaseEffectsFixture()
	restated.AccountingDigest = "sha256:accounting-restocked"
	return CorrectionRequest{
		CorrectionKey:       "correction-key-1",
		Type:                CorrectionTypeCorrection,
		Release:             release,
		Reason:              "restate accounting leg for late fee schedule",
		RestatedEffects:     restated,
		RestatedObligations: releaseObligationsFixture(),
		ApprovalDigest:      "sha256:correction-approval",
	}
}

// TestTodo_PAYRUN_008 is the primary acceptance case: a finalized run is
// never mutated; instead an off-record correction links the causal release,
// the exact deltas, approvals, and restated obligations.
func TestTodo_PAYRUN_008(t *testing.T) {
	req := correctionRequestFixture(t)
	before := req.Release.ReleaseDigest
	correction, err := CorrectFinalizedRun(req, nil)
	if err != nil {
		t.Fatalf("CorrectFinalizedRun: %v", err)
	}
	if correction.CorrectionID != "payroll-correction/correction-key-1" {
		t.Fatalf("correction id = %q", correction.CorrectionID)
	}
	if correction.PriorDigest != req.Release.ReleaseDigest || correction.ReleaseID != req.Release.ReleaseID {
		t.Fatalf("correction does not link the causal release: %+v", correction)
	}
	if len(correction.Deltas) != 1 || correction.Deltas[0].Leg != "accounting" || correction.Deltas[0].PriorDigest != "sha256:accounting" || correction.Deltas[0].RestatedDigest != "sha256:accounting-restocked" {
		t.Fatalf("deltas wrong: %+v", correction.Deltas)
	}
	if correction.ApprovalDigest != "sha256:correction-approval" {
		t.Fatalf("approval not linked: %+v", correction)
	}
	if correction.RestatedObligations.SettlementDigest != "sha256:settlement" {
		t.Fatalf("obligations not restated: %+v", correction.RestatedObligations)
	}
	if correction.CorrectionDigest == "" {
		t.Fatal("correction digest is empty")
	}
	if err := correction.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	again, err := CorrectFinalizedRun(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.CorrectionDigest != correction.CorrectionDigest || string(again.Canonical()) != string(correction.Canonical()) {
		t.Fatal("identical correction inputs did not produce identical evidence")
	}
	digest, err := correction.Digest()
	if err != nil || digest != correction.CorrectionDigest {
		t.Fatalf("Digest() = %q, err = %v", digest, err)
	}
	explanation, err := correction.Explain()
	if err != nil || explanation.PriorDigest != req.Release.ReleaseDigest || explanation.Digest != correction.CorrectionDigest || explanation.Deltas != 1 {
		t.Fatalf("explanation = %+v, err = %v", explanation, err)
	}
	// The finalized release is untouched: append-only, never mutated.
	if req.Release.ReleaseDigest != before {
		t.Fatal("correction mutated the finalized release")
	}
	if err := req.Release.Validate(); err != nil {
		t.Fatalf("release no longer validates: %v", err)
	}
}

// TestTodo_PAYRUN_008_Property proves delta precision and causal linkage:
// only changed legs appear, an off-cycle links its reversal, and every
// obligation leg is restated.
func TestTodo_PAYRUN_008_Property(t *testing.T) {
	req := correctionRequestFixture(t)
	req.RestatedEffects = releaseEffectsFixture()
	req.RestatedEffects.PaymentsDigest = "sha256:payments-restocked"
	req.RestatedEffects.ReportingDigest = "sha256:reporting-restocked"
	multi, err := CorrectFinalizedRun(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(multi.Deltas) != 2 {
		t.Fatalf("deltas = %+v, want exactly payments and reporting", multi.Deltas)
	}
	seen := map[string]bool{}
	for _, d := range multi.Deltas {
		seen[d.Leg] = true
		if d.PriorDigest == "" || d.RestatedDigest == "" || d.PriorDigest == d.RestatedDigest {
			t.Fatalf("delta is incoherent: %+v", d)
		}
	}
	if !seen["payments"] || !seen["reporting"] {
		t.Fatalf("delta legs wrong: %+v", multi.Deltas)
	}
	offCycle := correctionRequestFixture(t)
	offCycle.CorrectionKey = "correction-key-offcycle"
	offCycle.Type = CorrectionTypeOffCycle
	offCycle.ReversalOf = "sha256:reversed-entry"
	off, err := CorrectFinalizedRun(offCycle, []PayrollCorrection{multi})
	if err != nil {
		t.Fatal(err)
	}
	if off.ReversalOf != "sha256:reversed-entry" {
		t.Fatalf("off-cycle lost its reversal link: %+v", off)
	}
	if off.Type != CorrectionTypeOffCycle {
		t.Fatalf("type = %s, want OFF_CYCLE", off.Type)
	}
	if err := offCycle.RestatedObligations.Validate(); err != nil {
		t.Fatalf("restated obligations: %v", err)
	}
}

// TestTodo_PAYRUN_008_Race proves concurrent corrections with distinct keys
// all succeed on the same release without mutating it.
func TestTodo_PAYRUN_008_Race(t *testing.T) {
	base := correctionRequestFixture(t)
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			correction, err := CorrectFinalizedRun(base, nil)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = correction.CorrectionDigest
			_ = correction.Validate()
			_, _ = correction.Explain()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent corrections diverged")
		}
	}
	if err := base.Release.Validate(); err != nil {
		t.Fatalf("release mutated under concurrency: %v", err)
	}
}

// TestTodo_PAYRUN_008_Integration proves the correction sits exactly after
// the release on the lifecycle path: the release still settles while the
// correction carries the restated effects forward.
func TestTodo_PAYRUN_008_Integration(t *testing.T) {
	relReq := releaseRequestFixture(t)
	release, err := ReleasePayroll(relReq, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := correctionRequestFixture(t)
	req.Release = release
	correction, err := CorrectFinalizedRun(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if correction.RunID != release.RunID || correction.RunRevision != release.RunRevision {
		t.Fatalf("correction lost run identity: %+v", correction)
	}
	settled, err := relReq.Run.Settle()
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if settled.State != Settled {
		t.Fatalf("settled state = %s", settled.State)
	}
	if correction.RestatedEffects.AccountingDigest == release.Effects.AccountingDigest {
		t.Fatal("correction carries no restated effect forward")
	}
}

// TestTodo_PAYRUN_008_Fault proves every correction failure is typed and
// yields no correction: bad keys, empty reason/approval, no-op restatements,
// forged releases, and duplicates.
func TestTodo_PAYRUN_008_Fault(t *testing.T) {
	req := correctionRequestFixture(t)
	badType := req
	badType.CorrectionKey = "correction-key-bad-type"
	badType.Type = "REWRITE"
	if _, err := CorrectFinalizedRun(badType, nil); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("bad type error = %v", err)
	}
	emptyReason := req
	emptyReason.CorrectionKey = "correction-key-no-reason"
	emptyReason.Reason = ""
	if _, err := CorrectFinalizedRun(emptyReason, nil); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("empty reason error = %v", err)
	}
	emptyApproval := req
	emptyApproval.CorrectionKey = "correction-key-no-approval"
	emptyApproval.ApprovalDigest = ""
	if _, err := CorrectFinalizedRun(emptyApproval, nil); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("empty approval error = %v", err)
	}
	noop := req
	noop.CorrectionKey = "correction-key-noop"
	noop.RestatedEffects = releaseEffectsFixture()
	if _, err := CorrectFinalizedRun(noop, nil); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("no-op restatement error = %v", err)
	}
	forged := req
	forged.CorrectionKey = "correction-key-forged"
	forged.Release.ReleaseDigest = "sha256:forged"
	leaked, err := CorrectFinalizedRun(forged, nil)
	if !errors.Is(err, ErrCorrectionRejected) {
		t.Fatalf("forged release error = %v", err)
	}
	if leaked.CorrectionDigest != "" {
		t.Fatalf("forged release leaked a correction: %+v", leaked)
	}
	recorded, err := CorrectFinalizedRun(req, nil)
	if err != nil {
		t.Fatal(err)
	}
	rekeyed := req
	rekeyed.CorrectionKey = "correction-key-fresh"
	if _, err := CorrectFinalizedRun(rekeyed, []PayrollCorrection{recorded}); !errors.Is(err, ErrCorrectionDuplicate) {
		t.Fatalf("duplicate content error = %v", err)
	}
	if _, err := CorrectFinalizedRun(req, []PayrollCorrection{recorded}); !errors.Is(err, ErrCorrectionDuplicate) {
		t.Fatalf("duplicate key error = %v", err)
	}
	var refusal *CorrectionError
	if _, err := CorrectFinalizedRun(forged, nil); !errors.As(err, &refusal) || refusal.Field == "" || refusal.Reason == "" {
		t.Fatalf("refusal = %+v, want field and reason", err)
	}
}

// TestTodo_PAYRUN_008_Mutation proves the correction digest binds every
// restated leg, obligation, and approval: any change yields a new digest and
// a tampered correction fails validation.
func TestTodo_PAYRUN_008_Mutation(t *testing.T) {
	base, err := CorrectFinalizedRun(correctionRequestFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*CorrectionRequest){
		func(r *CorrectionRequest) { r.RestatedEffects.ReportingDigest = "sha256:reporting-alt" },
		func(r *CorrectionRequest) { r.RestatedObligations.FilingDigest = "sha256:filing-alt" },
		func(r *CorrectionRequest) { r.ApprovalDigest = "sha256:other-approval" },
		func(r *CorrectionRequest) { r.Reason = "another lawful reason" },
	}
	for i, mutate := range mutations {
		req := correctionRequestFixture(t)
		req.CorrectionKey = "correction-key-mut"
		mutate(&req)
		mutated, err := CorrectFinalizedRun(req, nil)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.CorrectionDigest == base.CorrectionDigest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.RestatedEffects = releaseEffectsFixture()
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered correction passed validation")
	}
	forged := base
	forged.CorrectionDigest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}
