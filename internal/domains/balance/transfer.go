package balance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TransferPlan binds a debit and an equal credit to one pair of account and
// accumulator revisions. Its digest is the idempotency identity for the pair.
type TransferPlan struct {
	Source      PostingPlan
	Destination PostingPlan
	Amount      values.Decimal
	Digest      string
}

// TransferPlanRequest contains both independently authorized account states
// and the entries that form the conserved transfer.
type TransferPlanRequest struct {
	SourceAuthorized        AuthorizedBalance
	SourceDefinition        AccumulatorDefinition
	SourceExpectedHead      int64
	Debit                   BalanceEntry
	DestinationAuthorized   AuthorizedBalance
	DestinationDefinition   AccumulatorDefinition
	DestinationExpectedHead int64
	Credit                  BalanceEntry
	IdempotencyKey          string
}

// PlanTransfer builds a conserved cross-account debit/credit pair.
func PlanTransfer(req TransferPlanRequest) (TransferPlan, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" || req.Debit.AccountID == req.Credit.AccountID || req.Debit.Kind != Debit || req.Credit.Kind != Credit || !req.Debit.Amount.Equal(req.Credit.Amount) {
		return TransferPlan{}, fmt.Errorf("%w: transfer requires a source debit and equal destination credit", ErrTransferInvalid)
	}
	if req.SourceDefinition.Unit != req.DestinationDefinition.Unit || req.SourceDefinition.Currency != req.DestinationDefinition.Currency || req.Debit.Amount.Scale() != req.Credit.Amount.Scale() || req.Debit.Amount.Rounding() != req.Credit.Amount.Rounding() {
		return TransferPlan{}, fmt.Errorf("%w: transfer legs must use the same unit, currency, scale, and rounding", ErrTransferInvalid)
	}
	if req.Debit.SourceTransactionID != req.IdempotencyKey || req.Credit.SourceTransactionID != req.IdempotencyKey || req.Debit.IdempotencyKey != req.IdempotencyKey+"/source" || req.Credit.IdempotencyKey != req.IdempotencyKey+"/destination" {
		return TransferPlan{}, fmt.Errorf("%w: linked transaction keys do not match transfer", ErrTransferInvalid)
	}
	source, err := PlanPosting(PostingPlanRequest{Authorized: req.SourceAuthorized, Definition: req.SourceDefinition, ExpectedHead: req.SourceExpectedHead, Requested: req.Debit})
	if err != nil {
		return TransferPlan{}, fmt.Errorf("%w: source: %v", ErrTransferInvalid, err)
	}
	destination, err := PlanPosting(PostingPlanRequest{Authorized: req.DestinationAuthorized, Definition: req.DestinationDefinition, ExpectedHead: req.DestinationExpectedHead, Requested: req.Credit})
	if err != nil {
		return TransferPlan{}, fmt.Errorf("%w: destination: %v", ErrTransferInvalid, err)
	}
	if !source.Accepted.Equal(req.Debit.Amount) || !destination.Accepted.Equal(req.Credit.Amount) {
		return TransferPlan{}, fmt.Errorf("%w: transfer legs must be accepted in full", ErrTransferInvalid)
	}
	p := TransferPlan{Source: source, Destination: destination, Amount: req.Debit.Amount}
	p.Digest = linkedPairDigest(source, destination, p.Amount)
	return p, nil
}

// Verify detects any mutation to either leg or the transfer amount.
func (p TransferPlan) Verify() bool {
	if p.Digest == "" || !p.Source.Verify() || !p.Destination.Verify() || len(p.Source.Entries) != 1 || len(p.Destination.Entries) != 1 {
		return false
	}
	d, c := p.Source.Entries[0], p.Destination.Entries[0]
	if d.Kind != Debit || c.Kind != Credit || d.AccountID == c.AccountID || !d.Amount.Equal(c.Amount) || !d.Amount.Equal(p.Amount) {
		return false
	}
	return p.Digest == linkedPairDigest(p.Source, p.Destination, p.Amount)
}

func linkedPairDigest(source, destination PostingPlan, amount values.Decimal) string {
	sum := sha256.Sum256([]byte("balance-transfer\x00" + source.Digest + "\x00" + destination.Digest + "\x00" + amount.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

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
	reversal.SourceTransactionID = req.IdempotencyKey
	reversal.IdempotencyKey = req.IdempotencyKey + "/source"
	reversal.RecordedAt = req.RecordedAt
	reversal.AuthorizedAt = req.AuthorizedAt
	if err := reversal.Validate(req.Definition); err != nil {
		return TransferResult{}, fmt.Errorf("%w: reversal: %v", ErrTransferInvalid, err)
	}
	reissue := req.Reissue.copy()
	reissue.SourceTransactionID = req.IdempotencyKey
	reissue.IdempotencyKey = req.IdempotencyKey + "/destination"
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
	toPre, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: reissue.AccountID, EffectiveAsOf: reissue.EffectiveAt, KnownAt: known, Opening: req.ToOpening, Scale: reissue.Amount.Scale(), Rounding: reissue.Amount.Rounding()}, req.ToLedger)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: destination balance: %v", ErrTransferInvalid, err)
	}
	reissuePlan, err := PlanPosting(PostingPlanRequest{Authorized: toPre, Definition: req.Definition, ExpectedHead: req.ExpectedToHead, Requested: reissue, Rules: req.Rules})
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: reissue plan: %v", ErrTransferInvalid, err)
	}
	toPost, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: reissue.AccountID, EffectiveAsOf: reissue.EffectiveAt, KnownAt: known, Opening: req.ToOpening, Scale: reissue.Amount.Scale(), Rounding: reissue.Amount.Rounding()}, append(append([]BalanceEntry(nil), req.ToLedger...), reissue))
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: reissued balance: %v", ErrTransferInvalid, err)
	}
	tx := NewBusinessTransaction()
	tx.SeedHead(req.Original.AccountID, reversalPlan.Opening.String())
	tx.SeedHead(reissue.AccountID, reissuePlan.Opening.String())
	transferReceipt, err := tx.commitLinkedPair(reversalPlan, reissuePlan, reversal.Amount, req.IdempotencyKey)
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: settle linked transfer: %v", ErrTransferInvalid, err)
	}
	fromReceipt, toReceipt := transferReceipt.Source, transferReceipt.Destination
	result := TransferResult{Reversal: reversal, ReversalPlan: reversalPlan, Reissue: reissue, ReissuePlan: reissuePlan, FromReceipt: fromReceipt, ToReceipt: toReceipt, FromBalance: fromPost, ToBalance: toPost}
	result.Digest, err = canonicalbytes.New("hcmnext.domains.balance.TransferResult", 1).String("reversal", reversal.Digest()).String("reissue", reissue.Digest()).String("reversal_plan", reversalPlan.Digest).String("reissue_plan", reissuePlan.Digest).String("from_receipt", fromReceipt.Digest).String("to_receipt", toReceipt.Digest).Digest()
	if err != nil {
		return TransferResult{}, fmt.Errorf("%w: digest: %v", ErrTransferInvalid, err)
	}
	return result, nil
}
