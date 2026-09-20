package balance

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrTransferInvalid reports a transfer request that cannot settle:
	// unknown or already-reversed originals, value-changing reissues,
	// missing keys or timestamps, or a definition that forbids the legs.
	ErrTransferInvalid = errors.New("balance: invalid transfer request")

	// ErrTransferConflict reports a transfer whose original already carries
	// a reversal: the ledger, not this call, owns the settled reversal.
	ErrTransferConflict = errors.New("balance: transfer conflicts with ledger state")
)

// TransferRequest moves one posted entry between two accumulators of one
// definition. Original is the posted move in the source account; Reissue
// is the caller-built destination posting carrying the same kind and
// amount under its own transaction keys. RecordedAt/AuthorizedAt stamp
// the reversal leg; IdempotencyKey scopes both settlement receipts.
type TransferRequest struct {
	Definition       AccumulatorDefinition
	Original         BalanceEntry
	FromLedger       []BalanceEntry
	FromOpening      values.Decimal
	Reissue          BalanceEntry
	ToLedger         []BalanceEntry
	ToOpening        values.Decimal
	Rules            []BalanceRule
	ExpectedFromHead int64
	ExpectedToHead   int64
	RecordedAt       values.Instant
	AuthorizedAt     values.Instant
	IdempotencyKey   string
}

// TransferResult is one settled transfer: the correction-path reversal,
// the reissue, both posting plans, both settlement receipts from one
// shared business transaction, and both post-state authorized balances.
type TransferResult struct {
	Reversal     BalanceEntry
	ReversalPlan PostingPlan
	Reissue      BalanceEntry
	ReissuePlan  PostingPlan
	FromReceipt  PostingReceipt
	ToReceipt    PostingReceipt
	FromBalance  AuthorizedBalance
	ToBalance    AuthorizedBalance
	Digest       string
}

// Transfer posts a move between two accumulators as their aggregate
// heads. The reversal leg negates the original through the correction
// path: a contra-entry of the same amount and opposite kind carrying
// SupersedesDigest, permitted only when the definition carries the
// correction entry type. A full negation cannot be expressed as a
// correction delta because amounts stay positive, so the reversal is
// the correction-path entry for the negation rather than an
// ApplyRetroCorrection delta. The reissue leg mirrors the move into the
// destination through the atomic double-entry plan path. Both legs
// commit under one business transaction seeded with both openings, so
// either a failed leg refuses the whole transfer before any head moves:
// after both plans verify, commit cannot partially fail.
func Transfer(req TransferRequest) (TransferResult, error) {
	if err := req.Definition.Validate(); err != nil {
		return TransferResult{}, fmt.Errorf("%w: definition: %v", ErrTransferInvalid, err)
	}
	if err := req.Original.Validate(req.Definition); err != nil {
		return TransferResult{}, fmt.Errorf("%w: original: %v", ErrTransferInvalid, err)
	}
	if err := req.Reissue.Validate(req.Definition); err != nil {
		return TransferResult{}, fmt.Errorf("%w: reissue: %v", ErrTransferInvalid, err)
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return TransferResult{}, fmt.Errorf("%w: idempotency key is required", ErrTransferInvalid)
	}
	if !req.RecordedAt.IsSet() || !req.AuthorizedAt.IsSet() {
		return TransferResult{}, fmt.Errorf("%w: reversal timestamps are required", ErrTransferInvalid)
	}
	if req.Reissue.AccountID == req.Original.AccountID {
		return TransferResult{}, fmt.Errorf("%w: reissue must leave the source account", ErrTransferInvalid)
	}
	if req.Reissue.Kind != req.Original.Kind || !req.Reissue.Amount.Equal(req.Original.Amount) {
		return TransferResult{}, fmt.Errorf("%w: reissue must mirror the moved kind and amount", ErrTransferInvalid)
	}
	if req.Reissue.SourceTransactionID == req.Original.SourceTransactionID || req.Reissue.IdempotencyKey == req.Original.IdempotencyKey {
		return TransferResult{}, fmt.Errorf("%w: reissue must carry its own transaction keys", ErrTransferInvalid)
	}
	foundOriginal := false
	for _, e := range req.FromLedger {
		if e.Digest() == req.Original.Digest() && e.RecordedAt.Compare(req.Original.RecordedAt) == 0 {
			if foundOriginal {
				return TransferResult{}, fmt.Errorf("%w: original is not unique in the source ledger", ErrTransferInvalid)
			}
			foundOriginal = true
		}
		if e.SupersedesDigest == req.Original.Digest() {
			return TransferResult{}, fmt.Errorf("%w: the original already carries a reversal: %s", ErrTransferConflict, e.Digest())
		}
	}
	if !foundOriginal {
		return TransferResult{}, fmt.Errorf("%w: original is not an exact ledger member", ErrTransferInvalid)
	}
	if !contains(req.Definition.EntryTypes, AdjustmentEntryType) {
		return TransferResult{}, fmt.Errorf("%w: definition does not permit correction entries", ErrTransferInvalid)
	}
	reversal := req.Original.copy()
	if reversal.Kind == Debit {
		reversal.Kind = Credit
	} else {
		reversal.Kind = Debit
	}
	reversal.EntryType = AdjustmentEntryType
	reversal.SupersedesDigest = req.Original.Digest()
	reversal.SourceTransactionID = req.IdempotencyKey + "/reversal"
	reversal.IdempotencyKey = req.IdempotencyKey + "/reversal"
	reversal.RecordedAt = req.RecordedAt
	reversal.AuthorizedAt = req.AuthorizedAt
	if err := reversal.Validate(req.Definition); err != nil {
		return TransferResult{}, fmt.Errorf("%w: reversal: %v", ErrTransferInvalid, err)
	}
	known := req.RecordedAt
	if req.AuthorizedAt.After(known) {
		known = req.AuthorizedAt
	}
	fromPre, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: req.Original.AccountID, EffectiveAsOf: req.Original.EffectiveAt, KnownAt: known, Opening: req.FromOpening, Scale: reversal.Amount.Scale(), Rounding: reversal.Amount.Rounding()}, req.FromLedger)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: source balance: %v", ErrTransferInvalid, err)
	}
	fromPost, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: req.Original.AccountID, EffectiveAsOf: req.Original.EffectiveAt, KnownAt: known, Opening: req.FromOpening, Scale: reversal.Amount.Scale(), Rounding: reversal.Amount.Rounding()}, append(append([]BalanceEntry(nil), req.FromLedger...), reversal))
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: reversed balance: %v", ErrTransferInvalid, err)
	}
	reversalPlan, err := PlanPosting(PostingPlanRequest{Authorized: fromPre, Definition: req.Definition, ExpectedHead: req.ExpectedFromHead, Requested: reversal})
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: reversal plan: %v", ErrTransferInvalid, err)
	}
	toPre, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: req.Reissue.AccountID, EffectiveAsOf: req.Reissue.EffectiveAt, KnownAt: known, Opening: req.ToOpening, Scale: req.Reissue.Amount.Scale(), Rounding: req.Reissue.Amount.Rounding()}, req.ToLedger)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: destination balance: %v", ErrTransferInvalid, err)
	}
	reissuePlan, err := PlanPosting(PostingPlanRequest{Authorized: toPre, Definition: req.Definition, ExpectedHead: req.ExpectedToHead, Requested: req.Reissue, Rules: req.Rules})
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: reissue plan: %v", ErrTransferInvalid, err)
	}
	toPost, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: req.Reissue.AccountID, EffectiveAsOf: req.Reissue.EffectiveAt, KnownAt: known, Opening: req.ToOpening, Scale: req.Reissue.Amount.Scale(), Rounding: req.Reissue.Amount.Rounding()}, append(append([]BalanceEntry(nil), req.ToLedger...), req.Reissue))
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: reissued balance: %v", ErrTransferInvalid, err)
	}
	tx := NewBusinessTransaction()
	tx.SeedHead(req.Original.AccountID, reversalPlan.Opening.String())
	tx.SeedHead(req.Reissue.AccountID, reissuePlan.Opening.String())
	fromReceipt, err := tx.CommitPosting(reversalPlan, req.IdempotencyKey+"/reversal", nil)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: settle reversal: %v", ErrTransferInvalid, err)
	}
	toReceipt, err := tx.CommitPosting(reissuePlan, req.IdempotencyKey+"/reissue", nil)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: settle reissue: %v", ErrTransferInvalid, err)
	}
	result := TransferResult{Reversal: reversal, ReversalPlan: reversalPlan, Reissue: req.Reissue.copy(), ReissuePlan: reissuePlan, FromReceipt: fromReceipt, ToReceipt: toReceipt, FromBalance: fromPost, ToBalance: toPost}
	result.Digest, err = canonicalbytes.New("hcmnext.domains.balance.TransferResult", 1).String("reversal", reversal.Digest()).String("reissue", req.Reissue.Digest()).String("reversal_plan", reversalPlan.Digest).String("reissue_plan", reissuePlan.Digest).String("from_receipt", fromReceipt.Digest).String("to_receipt", toReceipt.Digest).Digest()
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: digest: %v", ErrTransferInvalid, err)
	}
	return result, nil
}
