package balance

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func atomicTransferFixture(t *testing.T, amount int64, revision string) (TransferPlan, *BusinessTransaction) {
	t.Helper()
	def := postingPlanDefinition()
	def.ID = "source-accumulator-" + revision
	def.Version = revision
	destinationDefinition := postingPlanDefinition()
	destinationDefinition.ID = "destination-accumulator-" + revision
	destinationDefinition.Version = revision + "-destination"
	sourceAuth := postingAuthorizedBalance(t, "100.00")
	sourceAuth.AccountID = "source"
	sourceAuth.Digest = "source/" + revision
	destAuth := postingAuthorizedBalance(t, "5.00")
	destAuth.AccountID = "destination"
	destAuth.Digest = "destination/" + revision
	debit := postingEntry(t, def, Debit, fmt.Sprintf("%d.00", amount), "transfer/source")
	debit.AccountID, debit.SourceTransactionID, debit.IdempotencyKey = "source", "transfer", "transfer/source"
	credit := postingEntry(t, destinationDefinition, Credit, fmt.Sprintf("%d.00", amount), "transfer/destination")
	credit.AccountID, credit.SourceTransactionID, credit.IdempotencyKey = "destination", "transfer", "transfer/destination"
	plan, err := PlanTransfer(TransferPlanRequest{SourceAuthorized: sourceAuth, SourceDefinition: def, SourceExpectedHead: 0, Debit: debit, DestinationAuthorized: destAuth, DestinationDefinition: destinationDefinition, DestinationExpectedHead: 0, Credit: credit, IdempotencyKey: "transfer"})
	if err != nil {
		t.Fatal(err)
	}
	tx := NewBusinessTransaction()
	tx.SeedHead("source", plan.Source.Opening.String())
	tx.SeedHead("destination", plan.Destination.Opening.String())
	return plan, tx
}

func transferInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(v)
}

// transferFixture posts a 10-hour GRANT move to worker-1 and builds the
// matching reissue posting for worker-2.
func transferFixture(t *testing.T) (AccumulatorDefinition, BalanceEntry, []BalanceEntry, BalanceEntry) {
	t.Helper()
	d := validDefinition()
	at := transferInstant(t, "2026-06-01T00:00:00Z")
	recorded := transferInstant(t, "2026-06-02T00:00:00Z")
	original := validEntry()
	original.Kind = Credit
	original.EntryType = "GRANT"
	original.EffectiveAt, original.RecordedAt, original.AuthorizedAt = at, recorded, recorded
	original.Amount = values.MustDecimal("10.00", 2, values.RoundingExactRequired)
	reissue := original.copy()
	reissue.AccountID = "worker-2"
	reissue.Dimensions = map[string]string{"worker_id": "worker-2", "program": "pto"}
	reissue.SourceTransactionID = "transfer-1/reissue"
	reissue.IdempotencyKey = "transfer-1/reissue"
	return d, original, []BalanceEntry{original}, reissue
}

func transferRequest(t *testing.T, d AccumulatorDefinition, original BalanceEntry, ledger []BalanceEntry, reissue BalanceEntry) TransferRequest {
	t.Helper()
	zero := values.MustDecimal("0.00", 2, values.RoundingExactRequired)
	return TransferRequest{
		Definition:     d,
		Original:       original,
		FromLedger:     ledger,
		FromOpening:    zero,
		Reissue:        reissue,
		ToOpening:      zero,
		RecordedAt:     transferInstant(t, "2026-06-03T00:00:00Z"),
		AuthorizedAt:   transferInstant(t, "2026-06-03T00:00:00Z"),
		IdempotencyKey: "transfer-1",
	}
}

// TestTodo_REV_039_03 proves a posted move transfers between accumulators:
// the reversal leg negates the original through the correction path, the
// reissue leg mirrors the move into the destination, and both legs settle
// atomically with deterministic receipts.
func TestTodo_REV_039_03(t *testing.T) {
	plan, tx := atomicTransferFixture(t, 10, "2026.1")
	if !plan.Verify() || plan.Source.DefinitionVersion != "2026.1" || plan.Destination.DefinitionVersion != "2026.1-destination" || !plan.Source.Accepted.Equal(plan.Destination.Accepted) {
		t.Fatalf("invalid linked transfer plan: %+v", plan)
	}
	receipt, err := tx.CommitTransfer(plan, "transfer")
	if err != nil {
		t.Fatalf("CommitTransfer: %v", err)
	}
	if receipt.Digest != plan.Digest || receipt.Source.EndingHead != "90.00" || receipt.Destination.EndingHead != "15.00" {
		t.Fatalf("receipt=%+v", receipt)
	}
	sh, _ := tx.Head("source")
	dh, _ := tx.Head("destination")
	if sh != "90.00" || dh != "15.00" {
		t.Fatalf("heads=%s/%s", sh, dh)
	}

	d, original, ledger, reissue := transferFixture(t)
	first, err := Transfer(transferRequest(t, d, original, ledger, reissue))
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if first.Reversal.Kind != Debit || first.Reversal.Amount.String() != "10.00" {
		t.Fatalf("reversal = %+v, want a Debit contra-entry for 10.00", first.Reversal)
	}
	if first.Reversal.SupersedesDigest != original.Digest() || first.Reversal.EntryType != AdjustmentEntryType {
		t.Fatalf("reversal = %+v, want a correction-path entry superseding the original", first.Reversal)
	}
	if first.Reissue.AccountID != "worker-2" || first.Reissue.Kind != Credit || first.Reissue.Amount.String() != "10.00" {
		t.Fatalf("reissue = %+v, want the mirrored move in worker-2", first.Reissue)
	}
	if first.FromBalance.Ending.String() != "0.00" || first.ToBalance.Ending.String() != "10.00" {
		t.Fatalf("balances = %s/%s, want 0.00/10.00", first.FromBalance.Ending, first.ToBalance.Ending)
	}
	if first.FromReceipt.Entries != 1 || first.ToReceipt.Entries != 1 || first.Digest == "" {
		t.Fatalf("result = %+v, want one entry per receipt and a digest", first)
	}
	if first.Reversal.SourceTransactionID != "transfer-1" || first.Reissue.SourceTransactionID != "transfer-1" || first.Reversal.IdempotencyKey != "transfer-1/source" || first.Reissue.IdempotencyKey != "transfer-1/destination" {
		t.Fatalf("legacy Transfer did not bind both legs to the transfer key: %+v / %+v", first.Reversal, first.Reissue)
	}
	second, err := Transfer(transferRequest(t, d, original, ledger, reissue))
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("transfer is not deterministic")
	}
	// The correction/reissue API has a distinct opposite-direction case when
	// the original movement was a debit; it still settles through the same
	// atomic paired commit core.
	legacyDef, debitOriginal, _, debitReissue := transferFixture(t)
	debitOriginal.Kind = Debit
	debitOriginal.SourceTransactionID = "legacy-debit"
	debitOriginal.IdempotencyKey = "legacy-debit"
	debitReissue = debitOriginal.copy()
	debitReissue.AccountID = "worker-2"
	debitReissue.Dimensions = map[string]string{"worker_id": "worker-2", "program": "pto"}
	debitReissue.SourceTransactionID = "legacy-debit/reissue"
	debitReissue.IdempotencyKey = "legacy-debit/reissue"
	legacyReq := transferRequest(t, legacyDef, debitOriginal, []BalanceEntry{debitOriginal}, debitReissue)
	legacyReq.IdempotencyKey = "legacy-debit-transfer"
	legacyReq.FromOpening = values.MustDecimal("20.00", 2, values.RoundingExactRequired)
	legacyReq.ToOpening = values.MustDecimal("20.00", 2, values.RoundingExactRequired)
	legacy, err := Transfer(legacyReq)
	if err != nil {
		t.Fatalf("legacy debit Transfer: %v", err)
	}
	if legacy.Reversal.Kind != Credit || legacy.Reissue.Kind != Debit || legacy.FromReceipt.Entries != 1 || legacy.ToReceipt.Entries != 1 {
		t.Fatalf("legacy debit transfer = %+v", legacy)
	}
}

// TestTodo_REV_039_03_Property proves transfer guards: an unknown original,
// an already-reversed move, and a value-changing reissue are all refused.
func TestTodo_REV_039_03_Property(t *testing.T) {
	// Vary amounts and definition revisions while checking exact conservation.
	rng := rand.New(rand.NewSource(3903))
	for i := 0; i < 100; i++ {
		amount := int64(rng.Intn(90) + 1)
		revision := fmt.Sprintf("rev-%d-%d", i, rng.Intn(10000))
		plan, tx := atomicTransferFixture(t, amount, revision)
		debit, credit := plan.Source.Entries[0], plan.Destination.Entries[0]
		if debit.Kind != Debit || credit.Kind != Credit || !debit.Amount.Equal(credit.Amount) || !debit.Amount.Equal(plan.Amount) {
			t.Fatalf("case %d amount %d is not conserved", i, amount)
		}
		if _, err := tx.CommitTransfer(plan, "transfer"); err != nil {
			t.Fatalf("case %d amount %d: %v", i, amount, err)
		}
	}
	d, original, ledger, reissue := transferFixture(t)
	ghost := original.copy()
	ghost.SourceTransactionID = "ghost"
	ghost.IdempotencyKey = "ghost"
	if _, err := Transfer(transferRequest(t, d, ghost, ledger, reissue)); err == nil {
		t.Fatal("Transfer accepted an original that is not a ledger member")
	}
	first, err := Transfer(transferRequest(t, d, original, ledger, reissue))
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	ledger = append(append([]BalanceEntry(nil), ledger...), first.Reversal)
	if _, err := Transfer(transferRequest(t, d, original, ledger, reissue)); err == nil {
		t.Fatal("Transfer reversed an already-reversed move")
	}
	changed := reissue.copy()
	changed.Amount = values.MustDecimal("9.00", 2, values.RoundingExactRequired)
	if _, err := Transfer(transferRequest(t, d, original, []BalanceEntry{original}, changed)); err == nil {
		t.Fatal("Transfer accepted a reissue that changes the moved value")
	}
}

// TestTodo_REV_039_03_Race proves concurrent transfers over disjoint moves
// settle independently without sharing state.
func TestTodo_REV_039_03_Race(t *testing.T) {
	plan, tx := atomicTransferFixture(t, 10, "race")
	const concurrent = 24
	var commits sync.WaitGroup
	got := make([]TransferReceipt, concurrent)
	commitErrs := make([]error, concurrent)
	for i := range got {
		commits.Add(1)
		go func(i int) { defer commits.Done(); got[i], commitErrs[i] = tx.CommitTransfer(plan, "transfer") }(i)
	}
	commits.Wait()
	for i := range got {
		if commitErrs[i] != nil || got[i].Digest != plan.Digest {
			t.Fatalf("worker %d receipt=%+v err=%v", i, got[i], commitErrs[i])
		}
	}
	sh, _ := tx.Head("source")
	dh, _ := tx.Head("destination")
	if sh != "90.00" || dh != "15.00" {
		t.Fatalf("raced heads=%s/%s", sh, dh)
	}
	// Race a linked transfer against a correction-style source posting from the
	// same opening head. The lock and compare-and-set allow exactly one winner.
	interleaved, interTx := atomicTransferFixture(t, 10, "interleaving")
	def := postingPlanDefinition()
	def.ID, def.Version = interleaved.Source.DefinitionID, interleaved.Source.DefinitionVersion
	authorized := postingAuthorizedBalance(t, "100.00")
	authorized.AccountID, authorized.Digest = "source", "source/correction"
	correction := postingEntry(t, def, Credit, "1.00", "correction")
	correction.AccountID = "source"
	correctionPlan, err := PlanPosting(PostingPlanRequest{Authorized: authorized, Definition: def, ExpectedHead: 0, Requested: correction})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var racers sync.WaitGroup
	racers.Add(2)
	var transferErr, correctionErr error
	go func() {
		defer racers.Done()
		<-start
		_, transferErr = interTx.CommitTransfer(interleaved, "interleaving")
	}()
	go func() {
		defer racers.Done()
		<-start
		_, correctionErr = interTx.CommitPosting(correctionPlan, "correction", nil)
	}()
	close(start)
	racers.Wait()
	if (transferErr == nil) == (correctionErr == nil) {
		t.Fatalf("want exactly one winner: transfer=%v correction=%v", transferErr, correctionErr)
	}
	sh, _ = interTx.Head("source")
	dh, _ = interTx.Head("destination")
	if transferErr == nil && (sh != "90.00" || dh != "15.00") {
		t.Fatalf("transfer winner heads=%s/%s", sh, dh)
	}
	if correctionErr == nil && (sh != "101.00" || dh != "5.00") {
		t.Fatalf("correction winner heads=%s/%s", sh, dh)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, original, ledger, reissue := transferFixture(t)
			key := "transfer-race"
			reissue.SourceTransactionID = key
			reissue.IdempotencyKey = key
			req := transferRequest(t, d, original, ledger, reissue)
			req.IdempotencyKey = key
			_, errs[i] = Transfer(req)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
}

func TestTodo_REV_039_03_Mutation(t *testing.T) {
	plan, tx := atomicTransferFixture(t, 10, "mutation")
	// The public plan contract is directional: a source credit and destination
	// debit cannot be presented as an ordinary transfer.
	reversed := TransferPlanRequest{
		SourceAuthorized: postingAuthorizedBalance(t, "100.00"), SourceDefinition: postingPlanDefinition(), SourceExpectedHead: 0,
		Debit: plan.Source.Entries[0].copy(), DestinationAuthorized: postingAuthorizedBalance(t, "5.00"), DestinationDefinition: postingPlanDefinition(), DestinationExpectedHead: 0,
		Credit: plan.Destination.Entries[0].copy(), IdempotencyKey: "transfer",
	}
	reversed.SourceAuthorized.AccountID, reversed.DestinationAuthorized.AccountID = "source", "destination"
	reversed.SourceDefinition.ID, reversed.SourceDefinition.Version = plan.Source.DefinitionID, plan.Source.DefinitionVersion
	reversed.DestinationDefinition.ID, reversed.DestinationDefinition.Version = plan.Destination.DefinitionID, plan.Destination.DefinitionVersion
	reversed.Debit.Kind, reversed.Credit.Kind = Credit, Debit
	if _, err := PlanTransfer(reversed); err == nil {
		t.Fatal("public TransferPlan accepted source credit/destination debit")
	}

	if _, err := tx.CommitTransfer(plan, "transfer"); err != nil {
		t.Fatal(err)
	}
	mutated := plan
	mutated.Destination = plan.Source
	if mutated.Verify() {
		t.Fatal("mutated plan verified")
	}
	if _, err := tx.CommitTransfer(mutated, "forged"); err == nil {
		t.Fatal("mutated transfer committed")
	}
	sh, _ := tx.Head("source")
	dh, _ := tx.Head("destination")
	if sh != "90.00" || dh != "15.00" {
		t.Fatalf("mutation changed heads=%s/%s", sh, dh)
	}
	changed, _ := atomicTransferFixture(t, 11, "mutation")
	if changed.Digest == plan.Digest {
		t.Fatal("changed amount kept transfer digest")
	}
	if _, err := tx.CommitTransfer(changed, "transfer"); err == nil {
		t.Fatal("idempotency key accepted different plan")
	}
	if _, err := tx.CommitTransfer(plan, "unrelated-key"); err == nil {
		t.Fatal("transfer accepted a key unrelated to its sealed entries")
	}
	// Posting and transfer commits share one idempotency namespace in both
	// directions, including when the posting targets a third account.
	postingTx := NewBusinessTransaction()
	postingTx.SeedHead("other", "20.00")
	def := postingPlanDefinition()
	otherAuth := postingAuthorizedBalance(t, "20.00")
	otherAuth.AccountID = "other"
	other := postingEntry(t, def, Debit, "1.00", "collision")
	other.AccountID = "other"
	otherPlan, err := PlanPosting(PostingPlanRequest{Authorized: otherAuth, Definition: def, ExpectedHead: 0, Requested: other})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := postingTx.CommitPosting(otherPlan, "transfer", nil); err != nil {
		t.Fatal(err)
	}
	postingTx.SeedHead("source", plan.Source.Opening.String())
	postingTx.SeedHead("destination", plan.Destination.Opening.String())
	if _, err := postingTx.CommitTransfer(plan, "transfer"); err == nil {
		t.Fatal("transfer reused a posting idempotency key")
	}
	if h, _ := postingTx.Head("source"); h != "100.00" {
		t.Fatalf("posting/transfer collision changed source head: %s", h)
	}
	if h, _ := postingTx.Head("destination"); h != "5.00" {
		t.Fatalf("posting/transfer collision changed destination head: %s", h)
	}

	if _, err := tx.CommitPosting(otherPlan, "transfer", nil); err == nil {
		t.Fatal("posting reused a transfer idempotency key")
	}
	for _, legKey := range []string{"transfer/source", "transfer/destination"} {
		if _, err := tx.CommitPosting(otherPlan, legKey, nil); err == nil {
			t.Fatalf("posting reused reserved transfer leg key %q", legKey)
		}
	}
	replay, err := tx.CommitTransfer(plan, "transfer")
	if err != nil || replay.Digest != plan.Digest {
		t.Fatalf("transfer replay receipt=%+v err=%v", replay, err)
	}
	// A posting that claims either derived leg key first prevents the pair
	// from starting, and neither transfer head advances.
	legCollisionPlan, legCollisionTx := atomicTransferFixture(t, 10, "leg-collision")
	legCollisionTx.SeedHead("other", "20.00")
	otherAuth.AccountID, otherAuth.Digest = "other", "other/leg-collision"
	otherPlan, err = PlanPosting(PostingPlanRequest{Authorized: otherAuth, Definition: def, ExpectedHead: 0, Requested: other})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legCollisionTx.CommitPosting(otherPlan, "transfer/source", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := legCollisionTx.CommitTransfer(legCollisionPlan, "transfer"); err == nil {
		t.Fatal("transfer started after its source leg key was used")
	}
	if h, _ := legCollisionTx.Head("source"); h != "100.00" {
		t.Fatalf("derived-key collision changed source head: %s", h)
	}
	if h, _ := legCollisionTx.Head("destination"); h != "5.00" {
		t.Fatalf("derived-key collision changed destination head: %s", h)
	}
}
