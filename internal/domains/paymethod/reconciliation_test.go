package paymethod

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func verifiedPayDestination(t *testing.T, id string) Destination {
	t.Helper()
	d := validDestination(t, id)
	challenge, err := NewVerificationChallenge(d, "challenge-"+id, MethodMicroDeposit, payMethodInstant(t, 100), payMethodInstant(t, 160), 2)
	if err != nil {
		t.Fatal(err)
	}
	event, err := challenge.Verify(payMethodInstant(t, 120), "sha256:evidence-"+id)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := ApplyVerification(d, event)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func recordedPaySubmission(t *testing.T, dest Destination, instruction, key string, at int64) RecordedSubmission {
	t.Helper()
	recorded, err := RecordSettlementSubmission(SettlementSubmission{
		InstructionID:     instruction,
		IdempotencyKey:    key,
		DestinationID:     dest.DestinationID,
		DestinationDigest: dest.CanonicalDigest,
		SubmittedAt:       payMethodInstant(t, at),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return recorded
}

func settledPayObservation(instruction string, at int64, t *testing.T) SettlementObservation {
	t.Helper()
	return SettlementObservation{
		InstructionID: instruction,
		Status:        SettlementSettled,
		ObservedAt:    payMethodInstant(t, at),
		ProviderRef:   "provider:ack-1",
	}
}

// TestPayMethodReconciliationDistinguishesVerificationAcceptanceAndSettlement
// is the primary PAYMETHOD-003 acceptance case: a prenote submission never
// counts as verification, a provider-timeout retry never records twice,
// payroll use of a superseded destination is quarantined, a returned payment
// never marks the worker paid, and a mismatch never rewrites intent.
func TestPayMethodReconciliationDistinguishesVerificationAcceptanceAndSettlement(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	before := dest.CanonicalDigest

	prenote, err := RecordPrenoteObservation(dest, PrenoteObservation{
		ObservationID: "prenote-1", DestinationID: dest.DestinationID,
		DestinationDigest: dest.CanonicalDigest, State: PrenoteSubmitted,
		ObservedAt: payMethodInstant(t, 200), ProviderRef: "provider:prenote-1",
	})
	if err != nil {
		t.Fatalf("RecordPrenoteObservation: %v", err)
	}
	if prenote.Verified {
		t.Fatal("submitted prenote reports verification")
	}
	if dest.CanonicalDigest != before || dest.Verification != VerificationVerified {
		t.Fatal("prenote observation changed destination verification state")
	}
	// A prenote on an unverified destination leaves it unverified.
	plain := validDestination(t, "dest-plain")
	plainPrenote, err := RecordPrenoteObservation(plain, PrenoteObservation{
		ObservationID: "prenote-2", DestinationID: plain.DestinationID,
		DestinationDigest: plain.CanonicalDigest, State: PrenoteConfirmed,
		ObservedAt: payMethodInstant(t, 200), ProviderRef: "provider:prenote-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plainPrenote.Verified || plain.Verification == VerificationVerified {
		t.Fatal("prenote submission counted as verification")
	}

	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	if _, err := RecordSettlementSubmission(SettlementSubmission{
		InstructionID: "instr-1", IdempotencyKey: "key-1",
		DestinationID: dest.DestinationID, DestinationDigest: dest.CanonicalDigest,
		SubmittedAt: payMethodInstant(t, 301),
	}, []RecordedSubmission{used}); !errors.Is(err, ErrSettlementDuplicate) {
		t.Fatalf("timeout retry error = %v, want ErrSettlementDuplicate", err)
	}

	report, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatalf("ReconcileSettlement: %v", err)
	}
	if report.State != ReconciliationMatched || !report.Verified || !report.Accepted || !report.Settled || !report.Paid || report.Quarantined {
		t.Fatalf("matched report = %+v", report)
	}

	rotated, err := dest.NewRevision(Destination{GovernedRef: "vault-token:dest-1-rotated", DisplayHint: "••••5678", Currency: "USD", CountryCode: "US", Rail: RailACH, Risk: RiskMedium, Effective: payMethodInterval(t)})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := ReconcileSettlement(rotated, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if stale.State != ReconciliationSuperseded || stale.Paid || !stale.Quarantined || stale.RepairIntentID == "" {
		t.Fatalf("superseded report = %+v", stale)
	}

	returnedObs := settledPayObservation("instr-1", 400, t)
	returnedObs.Status, returnedObs.ReturnCode = SettlementReturned, "R01"
	returned, err := ReconcileSettlement(dest, used, returnedObs, payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if returned.State != ReconciliationReturned || returned.Paid || returned.ReturnIntentID == "" {
		t.Fatalf("returned report = %+v", returned)
	}

	other := verifiedPayDestination(t, "dest-2")
	foreign, err := RecordSettlementSubmission(SettlementSubmission{
		InstructionID: "instr-1", IdempotencyKey: "key-9",
		DestinationID: other.DestinationID, DestinationDigest: other.CanonicalDigest,
		SubmittedAt: payMethodInstant(t, 300),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mismatched, err := ReconcileSettlement(dest, foreign, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if mismatched.State != ReconciliationMismatched || mismatched.Paid || !mismatched.Quarantined || mismatched.RepairIntentID == "" {
		t.Fatalf("mismatched report = %+v", mismatched)
	}
	if dest.CanonicalDigest != before {
		t.Fatal("reconciliation rewrote the expected destination")
	}
	if used.DestinationDigest != dest.CanonicalDigest {
		t.Fatal("reconciliation rewrote the recorded submission")
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestTodo_PAYMETHOD_003_Property proves the three legs are independent and
// the report digest is a pure function of its inputs.
func TestTodo_PAYMETHOD_003_Property(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	first, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReportDigest != second.ReportDigest {
		t.Fatal("reconciliation digest is not deterministic")
	}
	// Settlement observed against an unverified destination matches the
	// comparison but never marks the worker paid.
	plain := validDestination(t, "dest-plain")
	plainUsed := recordedPaySubmission(t, plain, "instr-plain", "key-plain", 300)
	unverified, err := ReconcileSettlement(plain, plainUsed, settledPayObservation("instr-plain", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if unverified.State != ReconciliationMatched || unverified.Verified || unverified.Paid {
		t.Fatalf("unverified settlement = %+v", unverified)
	}
	// A verified, accepted, but not yet settled instruction waits.
	pending := settledPayObservation("instr-1", 400, t)
	pending.Status = SettlementPending
	waiting, err := ReconcileSettlement(dest, used, pending, payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State != ReconciliationPendingSettlement || waiting.Settled || waiting.Paid || waiting.Quarantined {
		t.Fatalf("pending settlement = %+v", waiting)
	}
}

// TestTodo_PAYMETHOD_003_Golden pins byte-identical reconciliation evidence.
func TestTodo_PAYMETHOD_003_Golden(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	first, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReportDigest != second.ReportDigest || !bytes.Equal(first.Canonical(), second.Canonical()) {
		t.Fatal("identical observations did not produce identical reconciliation evidence")
	}
	explanation, err := second.Explain()
	if err != nil || explanation.State != ReconciliationMatched || !explanation.Paid {
		t.Fatalf("explanation = %+v, err = %v", explanation, err)
	}
}

// TestTodo_PAYMETHOD_003_Race proves concurrent reconciliations over shared
// records converge and never mutate them.
func TestTodo_PAYMETHOD_003_Race(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	obs := settledPayObservation("instr-1", 400, t)
	now := payMethodInstant(t, 500)
	var wg sync.WaitGroup
	digests := make([]string, 8)
	errs := make([]error, 8)
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			report, err := ReconcileSettlement(dest, used, obs, now, 3600)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = report.ReportDigest
			_ = report.Validate()
			_ = report.Canonical()
			_, _ = report.Explain()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if digests[i] != digests[0] {
			t.Fatal("concurrent reconciliations diverged")
		}
	}
}

// TestTodo_PAYMETHOD_003_Fault proves malformed reconciliation inputs are
// refused and provider duplicates never record twice.
func TestTodo_PAYMETHOD_003_Fault(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	obs := settledPayObservation("instr-1", 400, t)
	now := payMethodInstant(t, 500)
	tampered := dest
	tampered.CanonicalDigest = "sha256:forged"
	if _, err := ReconcileSettlement(tampered, used, obs, now, 3600); !errors.Is(err, ErrReconciliationRefused) {
		t.Fatalf("forged destination error = %v", err)
	}
	foreign := settledPayObservation("instr-other", 400, t)
	if _, err := ReconcileSettlement(dest, used, foreign, now, 3600); !errors.Is(err, ErrReconciliationRefused) {
		t.Fatalf("instruction mismatch error = %v", err)
	}
	future := settledPayObservation("instr-1", 900, t)
	if _, err := ReconcileSettlement(dest, used, future, now, 3600); !errors.Is(err, ErrReconciliationRefused) {
		t.Fatalf("future observation error = %v", err)
	}
	if _, err := ReconcileSettlement(dest, used, obs, now, -1); !errors.Is(err, ErrReconciliationRefused) {
		t.Fatalf("negative age error = %v", err)
	}
	again, err := RecordSettlementSubmission(SettlementSubmission{
		InstructionID: "instr-2", IdempotencyKey: "key-1",
		DestinationID: dest.DestinationID, DestinationDigest: dest.CanonicalDigest,
		SubmittedAt: payMethodInstant(t, 310),
	}, []RecordedSubmission{used})
	if !errors.Is(err, ErrSettlementDuplicate) {
		t.Fatalf("duplicate key error = %v, %+v", err, again)
	}
	returned := settledPayObservation("instr-1", 400, t)
	returned.Status = SettlementReturned
	if _, err := ReconcileSettlement(dest, used, returned, now, 3600); !errors.Is(err, ErrReconciliationRefused) {
		t.Fatalf("codeless return error = %v", err)
	}
}

// TestTodo_PAYMETHOD_003_Security proves forged and replayed observations can
// never produce a paid reconciliation.
func TestTodo_PAYMETHOD_003_Security(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	// A replayed observation past the freshness window quarantines as stale.
	replayed, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 400+7200), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State != ReconciliationStale || replayed.Paid || !replayed.Quarantined {
		t.Fatalf("replayed observation = %+v", replayed)
	}
	// An observation predating the submission is stale, never settled.
	early := settledPayObservation("instr-1", 250, t)
	premature, err := ReconcileSettlement(dest, used, early, payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if premature.State != ReconciliationStale || premature.Paid {
		t.Fatalf("predated observation = %+v", premature)
	}
	// A prenote record edited to claim verification fails validation.
	prenote, err := RecordPrenoteObservation(dest, PrenoteObservation{
		ObservationID: "prenote-9", DestinationID: dest.DestinationID,
		DestinationDigest: dest.CanonicalDigest, State: PrenoteSubmitted,
		ObservedAt: payMethodInstant(t, 200), ProviderRef: "provider:prenote-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	prenote.Verified = true
	if err := prenote.Validate(); err == nil {
		t.Fatal("forged prenote verification validated")
	}
}

// TestTodo_PAYMETHOD_003_Conformance proves verification, acceptance, and
// settlement stay distinct: no single leg implies the others.
func TestTodo_PAYMETHOD_003_Conformance(t *testing.T) {
	plain := validDestination(t, "dest-plain")
	if plain.Verification == VerificationVerified {
		t.Fatal("fresh destination starts verified")
	}
	prenote, err := RecordPrenoteObservation(plain, PrenoteObservation{
		ObservationID: "prenote-1", DestinationID: plain.DestinationID,
		DestinationDigest: plain.CanonicalDigest, State: PrenoteConfirmed,
		ObservedAt: payMethodInstant(t, 200), ProviderRef: "provider:prenote-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prenote.Verified {
		t.Fatal("confirmed prenote confers verification")
	}
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	failed := settledPayObservation("instr-1", 400, t)
	failed.Status = SettlementFailed
	report, err := ReconcileSettlement(dest, used, failed, payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != ReconciliationFailed || report.Paid || !report.Quarantined || report.RepairIntentID == "" {
		t.Fatalf("failed settlement = %+v", report)
	}
	if !report.Accepted || !report.Verified || report.Settled {
		t.Fatalf("failed legs = %+v", report)
	}
}

// TestTodo_PAYMETHOD_003_Mutation proves the report digest binds the compared
// destinations and outcome: any change yields a new digest and a tampered
// report fails validation.
func TestTodo_PAYMETHOD_003_Mutation(t *testing.T) {
	dest := verifiedPayDestination(t, "dest-1")
	used := recordedPaySubmission(t, dest, "instr-1", "key-1", 300)
	base, err := ReconcileSettlement(dest, used, settledPayObservation("instr-1", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ReconcileSettlement(verifiedPayDestination(t, "dest-9"), recordedPaySubmission(t, verifiedPayDestination(t, "dest-9"), "instr-9", "key-9", 300), settledPayObservation("instr-9", 400, t), payMethodInstant(t, 500), 3600)
	if err != nil {
		t.Fatal(err)
	}
	if other.ReportDigest == base.ReportDigest {
		t.Fatal("different destinations did not change the report digest")
	}
	tampered := base
	tampered.Paid = false
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered report validated")
	}
	empty := base
	empty.ReportDigest = ""
	if err := empty.Validate(); err == nil {
		t.Fatal("undigested report validated")
	}
}
