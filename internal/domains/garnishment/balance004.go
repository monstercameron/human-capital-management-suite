// GARN-004: track garnishment balance, arrears and changes.
//
// ApplyLedgerEntry posts one payment, change, release or retro correction
// to an order balance as an immutable entry. Entries never edit history:
// a retro correction is a new entry linked to its target, and withholding
// can never exceed the order and rule cap. The ledger is kernel-pure.
package garnishment

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// BalanceVersion is the rejection version carried by BalanceRejection.
const BalanceVersion = "garnishment-balance/v1"

var (
	// ErrBalanceRejected is the GARN-004 sentinel. An over-cap posting,
	// an out-of-sequence entry or a mutation of history fails with this
	// error carrying the offending field, state and version.
	ErrBalanceRejected = errors.New("GARN_004_REJECTED")
)

// BalanceRejection is the stable GARN-004 failure shape.
type BalanceRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *BalanceRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrBalanceRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the GARN_004_REJECTED sentinel to errors.Is.
func (r *BalanceRejection) Unwrap() error { return ErrBalanceRejected }

func balanceReject(field, state, reason string) error {
	return &BalanceRejection{Field: field, State: state, Version: BalanceVersion, Reason: reason}
}

// LedgerEntryKind is the closed GARN-004 entry vocabulary.
type LedgerEntryKind string

const (
	LedgerWithholding LedgerEntryKind = "WITHHOLDING"
	LedgerRemittance  LedgerEntryKind = "REMITTANCE"
	LedgerRelease     LedgerEntryKind = "RELEASE"
	LedgerAmendment   LedgerEntryKind = "AMENDMENT"
	LedgerRetroCredit LedgerEntryKind = "RETRO_CREDIT"
	LedgerRetroDebit  LedgerEntryKind = "RETRO_DEBIT"
)

// Valid reports whether the entry kind is declared.
func (k LedgerEntryKind) Valid() bool {
	switch k {
	case LedgerWithholding, LedgerRemittance, LedgerRelease, LedgerAmendment, LedgerRetroCredit, LedgerRetroDebit:
		return true
	default:
		return false
	}
}

// LedgerEntry is one immutable balance posting. CorrectsSeq links a retro
// correction to its target entry; all other entries leave it zero.
type LedgerEntry struct {
	Tenant       string
	WorkerRef    string
	OrderRef     string
	Seq          uint64
	Kind         LedgerEntryKind
	Amount       values.Decimal
	EffectiveAt  time.Time
	CorrectsSeq  uint64
	AuthorityRef string
	Digest       string
}

// BalanceState is the explainable balance for one order.
type BalanceState struct {
	Tenant      string
	WorkerRef   string
	OrderRef    string
	OrderCap    values.Decimal
	Withheld    values.Decimal
	Remitted    values.Decimal
	Arrears     values.Decimal
	Entries     []LedgerEntry
	Explanation []string
	Digest      string
}

func entryDigest(e LedgerEntry) string {
	w := canonicalbytes.New("hcmnext.domains.garnishment.LedgerEntry", 1).
		String("tenant", e.Tenant).
		String("worker_ref", e.WorkerRef).
		String("order_ref", e.OrderRef).
		Int("seq", int64(e.Seq)).
		String("kind", string(e.Kind)).
		Value("amount", e.Amount).
		String("effective_at", e.EffectiveAt.UTC().Format(time.RFC3339)).
		Int("corrects", int64(e.CorrectsSeq)).
		String("authority", e.AuthorityRef)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func zero2() values.Decimal {
	return values.MustDecimal("0.00", 2, values.RoundingHalfUp)
}

// OpenBalance starts the ledger for one order under its cap.
func OpenBalance(tenant, workerRef, orderRef string, cap values.Decimal) (BalanceState, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(workerRef) == "" || strings.TrimSpace(orderRef) == "" {
		return BalanceState{}, balanceReject("balance.scope", "MISSING", "tenant, worker and order refs are required")
	}
	if err := cap.Validate(); err != nil || cap.Sign() < 0 {
		return BalanceState{}, balanceReject("balance.cap", "INVALID", "order cap must be a valid non-negative decimal")
	}
	return BalanceState{
		Tenant: tenant, WorkerRef: workerRef, OrderRef: orderRef, OrderCap: cap,
		Withheld: zero2(), Remitted: zero2(), Arrears: cap,
	}, nil
}

// ApplyLedgerEntry posts one immutable entry and returns the successor
// state. The prior state is never mutated.
func ApplyLedgerEntry(state BalanceState, e LedgerEntry) (BalanceState, error) {
	if !e.Kind.Valid() {
		return BalanceState{}, balanceReject("balance.kind", "UNDECLARED", fmt.Sprintf("entry kind %q is not declared", e.Kind))
	}
	if e.Tenant != state.Tenant || e.WorkerRef != state.WorkerRef || e.OrderRef != state.OrderRef {
		return BalanceState{}, balanceReject("balance.scope", "MISMATCH", "entry must stay in the ledger scope")
	}
	if e.Seq != uint64(len(state.Entries)+1) {
		return BalanceState{}, balanceReject("balance.seq", "OUT_OF_SEQUENCE", fmt.Sprintf("entry seq %d breaks the chain at %d", e.Seq, len(state.Entries)+1))
	}
	if err := e.Amount.Validate(); err != nil || e.Amount.Sign() < 0 {
		return BalanceState{}, balanceReject("balance.amount", "INVALID", "entry amount must be a valid non-negative decimal")
	}
	if e.EffectiveAt.IsZero() {
		return BalanceState{}, balanceReject("balance.effective_at", "MISSING", "effective time is required")
	}
	if strings.TrimSpace(e.AuthorityRef) == "" {
		return BalanceState{}, balanceReject("balance.authority_ref", "MISSING", "authority ref is required")
	}
	if (e.Kind == LedgerRetroCredit || e.Kind == LedgerRetroDebit) && e.CorrectsSeq == 0 {
		return BalanceState{}, balanceReject("balance.corrects_seq", "MISSING", "retro correction must link its target entry")
	}
	if e.CorrectsSeq > uint64(len(state.Entries)) {
		return BalanceState{}, balanceReject("balance.corrects_seq", "DANGLING", "retro correction links an unknown entry")
	}
	next := state
	next.Entries = append(append([]LedgerEntry(nil), state.Entries...), LedgerEntry{})
	entry := e
	entry.Digest = entryDigest(e)
	if entry.Digest == "" {
		return BalanceState{}, balanceReject("balance.digest", "UNENCODABLE", "entry is not digestible")
	}
	add := func(base values.Decimal, delta values.Decimal) (values.Decimal, error) {
		out, err := base.Add(delta)
		if err != nil {
			return values.Decimal{}, balanceReject("balance.total", "INEXACT", fmt.Sprintf("total is not computable: %v", err))
		}
		return out, nil
	}
	sub := func(base values.Decimal, delta values.Decimal) (values.Decimal, error) {
		out, err := base.Sub(delta)
		if err != nil {
			return values.Decimal{}, balanceReject("balance.total", "INEXACT", fmt.Sprintf("total is not computable: %v", err))
		}
		return out, nil
	}
	var err error
	switch e.Kind {
	case LedgerWithholding, LedgerRetroDebit:
		next.Withheld, err = add(next.Withheld, e.Amount)
		if err != nil {
			return BalanceState{}, err
		}
		if next.Withheld.Cmp(next.OrderCap) > 0 {
			return BalanceState{}, balanceReject("balance.withheld", "OVER_CAP", "withholding cannot exceed the order cap")
		}
	case LedgerRetroCredit:
		next.Withheld, err = sub(next.Withheld, e.Amount)
		if err != nil {
			return BalanceState{}, err
		}
		if next.Withheld.Sign() < 0 {
			return BalanceState{}, balanceReject("balance.withheld", "NEGATIVE", "retro credit cannot drive withholding negative")
		}
	case LedgerRemittance:
		next.Remitted, err = add(next.Remitted, e.Amount)
		if err != nil {
			return BalanceState{}, err
		}
		if next.Remitted.Cmp(next.Withheld) > 0 {
			return BalanceState{}, balanceReject("balance.remitted", "OVER_WITHHELD", "remittance cannot exceed withholding")
		}
	case LedgerRelease, LedgerAmendment:
		if !e.Amount.IsZero() {
			return BalanceState{}, balanceReject("balance.amount", "NONZERO_LIFECYCLE", "release and amendment entries carry no amount")
		}
	}
	next.Arrears, err = next.OrderCap.Sub(next.Withheld)
	if err != nil {
		return BalanceState{}, balanceReject("balance.arrears", "INEXACT", fmt.Sprintf("arrears are not computable: %v", err))
	}
	next.Entries[len(next.Entries)-1] = entry
	next.Explanation = append(append([]string(nil), state.Explanation...),
		fmt.Sprintf("seq %d %s %s", e.Seq, e.Kind, e.Amount.String()))
	w := canonicalbytes.New("hcmnext.domains.garnishment.BalanceState", 1).
		String("tenant", next.Tenant).
		String("worker_ref", next.WorkerRef).
		String("order_ref", next.OrderRef).
		Value("withheld", next.Withheld).
		Value("remitted", next.Remitted).
		Value("arrears", next.Arrears).
		Int("entries", int64(len(next.Entries)))
	digest, derr := w.Digest()
	if derr != nil {
		return BalanceState{}, balanceReject("balance.digest", "UNENCODABLE", "state is not digestible")
	}
	next.Digest = digest
	return next, nil
}
