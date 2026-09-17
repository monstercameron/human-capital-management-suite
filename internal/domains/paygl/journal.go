package paygl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrJournalRejected is the PAYGL-004 refusal boundary. Debits must equal
// credits by currency, entity and ledger; no missing dimension can hide.
var ErrJournalRejected = errors.New("PAYGL_004_REJECTED")

// DebitCredit is the closed posting-side vocabulary.
type DebitCredit string

const (
	SideDebit  DebitCredit = "DEBIT"
	SideCredit DebitCredit = "CREDIT"
)

func (s DebitCredit) Valid() bool { return s == SideDebit || s == SideCredit }

// LedgerKind is the closed ledger vocabulary. Payroll posts to actuals.
type LedgerKind string

const (
	LedgerActual LedgerKind = "ACTUAL"
)

func (l LedgerKind) Valid() bool { return l == LedgerActual }

// JournalLine is one balanced posting line. Every line binds a governed
// labor dimension and the mapping split it came from.
type JournalLine struct {
	Ordinal   int
	Account   string
	Side      DebitCredit
	Amount    values.Decimal
	Currency  string
	Entity    string
	Ledger    LedgerKind
	Dimension labor.Dimension
	SourceRef string
}

// JournalRequest asks for an immutable journal linked to one source run.
type JournalRequest struct {
	JournalID         string
	SourceRunID       string
	SourceRunRevision uint64
	SourceRunDigest   string
	Lines             []JournalLine
}

// Journal is the immutable, content-addressed balanced journal.
type Journal struct {
	JournalID         string
	SourceRunID       string
	SourceRunRevision uint64
	SourceRunDigest   string
	Lines             []JournalLine
	TotalDebits       values.Decimal
	TotalCredits      values.Decimal
	Digest            string
}

// NewJournal validates balance per currency, entity and ledger, then seals
// the journal with a content digest.
func NewJournal(req JournalRequest) (Journal, error) {
	fail := func(format string, args ...any) (Journal, error) {
		return Journal{}, errors.Join(ErrJournalRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(req.JournalID) == "" {
		return fail("journal id is required")
	}
	if strings.TrimSpace(req.SourceRunID) == "" || req.SourceRunRevision == 0 || strings.TrimSpace(req.SourceRunDigest) == "" {
		return fail("source run id, revision and digest are required")
	}
	if len(req.Lines) == 0 {
		return fail("at least one line is required")
	}
	lines := append([]JournalLine(nil), req.Lines...)
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j].Ordinal < lines[j-1].Ordinal; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
	scale, rounding := lines[0].Amount.Scale(), lines[0].Amount.Rounding()
	seen := map[int]struct{}{}
	groups := map[string][2]values.Decimal{}
	for _, line := range lines {
		if line.Ordinal <= 0 {
			return fail("line ordinal must be positive")
		}
		if _, ok := seen[line.Ordinal]; ok {
			return fail("duplicate line ordinal %d", line.Ordinal)
		}
		seen[line.Ordinal] = struct{}{}
		if strings.TrimSpace(line.Account) == "" {
			return fail("line %d: account is required", line.Ordinal)
		}
		if !line.Side.Valid() {
			return fail("line %d: side %q is not declared", line.Ordinal, line.Side)
		}
		if err := line.Amount.Validate(); err != nil {
			return fail("line %d amount: %v", line.Ordinal, err)
		}
		if line.Amount.Scale() != scale || line.Amount.Rounding() != rounding {
			return fail("line %d precision differs", line.Ordinal)
		}
		if line.Amount.Sign() < 0 {
			return fail("line %d amount cannot be negative", line.Ordinal)
		}
		if strings.TrimSpace(line.Currency) == "" || strings.TrimSpace(line.Entity) == "" {
			return fail("line %d: currency and entity are required", line.Ordinal)
		}
		if !line.Ledger.Valid() {
			return fail("line %d: ledger %q is not declared", line.Ordinal, line.Ledger)
		}
		if err := line.Dimension.Validate(); err != nil {
			return fail("line %d: %v", line.Ordinal, err)
		}
		if strings.TrimSpace(line.SourceRef) == "" {
			return fail("line %d: source ref is required", line.Ordinal)
		}
		key := line.Currency + "\x00" + line.Entity + "\x00" + string(line.Ledger)
		pair := groups[key]
		var err error
		if line.Side == SideDebit {
			pair[0], err = addOrZero(pair[0], line.Amount, scale, rounding)
		} else {
			pair[1], err = addOrZero(pair[1], line.Amount, scale, rounding)
		}
		if err != nil {
			return fail("line %d: %v", line.Ordinal, err)
		}
		groups[key] = pair
	}
	for key, pair := range groups {
		if !pair[0].Equal(pair[1]) {
			return fail("group %q: debits %s do not equal credits %s", key, pair[0], pair[1])
		}
	}
	debits, err := values.NewDecimal("0", scale, rounding)
	if err != nil {
		return fail("zero: %v", err)
	}
	credits := debits
	for _, line := range lines {
		if line.Side == SideDebit {
			debits, err = debits.Add(line.Amount)
		} else {
			credits, err = credits.Add(line.Amount)
		}
		if err != nil {
			return fail("totals: %v", err)
		}
	}
	out := Journal{
		JournalID: req.JournalID, SourceRunID: req.SourceRunID,
		SourceRunRevision: req.SourceRunRevision, SourceRunDigest: req.SourceRunDigest,
		Lines: lines, TotalDebits: debits, TotalCredits: credits,
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func addOrZero(total, amount values.Decimal, scale int32, rounding values.RoundingMode) (values.Decimal, error) {
	if err := total.Validate(); err != nil {
		total, err = values.NewDecimal("0", scale, rounding)
		if err != nil {
			return values.Decimal{}, err
		}
	}
	return total.Add(amount)
}

func (j Journal) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paygl.Journal", 1).
		String("journal_id", j.JournalID).String("source_run_id", j.SourceRunID).
		Int("source_run_revision", int64(j.SourceRunRevision)).String("source_run_digest", j.SourceRunDigest).
		Value("total_debits", j.TotalDebits).Value("total_credits", j.TotalCredits).
		Count("lines", len(j.Lines))
	for _, line := range j.Lines {
		w.Int("ordinal", int64(line.Ordinal)).String("account", line.Account).
			String("side", string(line.Side)).Value("amount", line.Amount).
			String("currency", line.Currency).String("entity", line.Entity).
			String("ledger", string(line.Ledger)).Value("dimension", line.Dimension).
			String("source_ref", line.SourceRef)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Currency returns the journal currency when every line agrees, else "".
func (j Journal) Currency() string {
	currency := ""
	for _, line := range j.Lines {
		if currency == "" {
			currency = line.Currency
		} else if line.Currency != currency {
			return ""
		}
	}
	return currency
}

// Validate rechecks group balance, totals and the digest binding.
func (j Journal) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrJournalRejected, fmt.Errorf(format, args...))
	}
	if len(j.Lines) == 0 {
		return fail("at least one line is required")
	}
	groups := map[string][2]values.Decimal{}
	seen := map[int]struct{}{}
	var debits, credits values.Decimal
	first := true
	for _, line := range j.Lines {
		if line.Ordinal <= 0 {
			return fail("line ordinal must be positive")
		}
		if _, ok := seen[line.Ordinal]; ok {
			return fail("duplicate line ordinal %d", line.Ordinal)
		}
		seen[line.Ordinal] = struct{}{}
		if !line.Side.Valid() || strings.TrimSpace(line.Account) == "" {
			return fail("line %d is incomplete", line.Ordinal)
		}
		if err := line.Dimension.Validate(); err != nil {
			return fail("line %d: %v", line.Ordinal, err)
		}
		key := line.Currency + "\x00" + line.Entity + "\x00" + string(line.Ledger)
		pair := groups[key]
		var err error
		if line.Side == SideDebit {
			pair[0], err = addOrZero(pair[0], line.Amount, line.Amount.Scale(), line.Amount.Rounding())
		} else {
			pair[1], err = addOrZero(pair[1], line.Amount, line.Amount.Scale(), line.Amount.Rounding())
		}
		if err != nil {
			return fail("line %d: %v", line.Ordinal, err)
		}
		groups[key] = pair
		if first {
			zero, err := values.NewDecimal("0", line.Amount.Scale(), line.Amount.Rounding())
			if err != nil {
				return fail("zero: %v", err)
			}
			debits, credits = zero, zero
			first = false
		}
		if line.Side == SideDebit {
			debits, err = debits.Add(line.Amount)
		} else {
			credits, err = credits.Add(line.Amount)
		}
		if err != nil {
			return fail("line %d: %v", line.Ordinal, err)
		}
	}
	for key, pair := range groups {
		if !pair[0].Equal(pair[1]) {
			return fail("group %q is unbalanced", key)
		}
	}
	if !debits.Equal(j.TotalDebits) || !credits.Equal(j.TotalCredits) {
		return fail("totals mismatch")
	}
	if j.Digest != canonicalbytes.Digest(j.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}
