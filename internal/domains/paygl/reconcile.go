package paygl

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrPostingReconciliationRejected is the PAYGL-006 refusal boundary. A
// provider's accepted claim is insufficient on its own; only a full
// comparison settles, and every deviation routes to repair.
var ErrPostingReconciliationRejected = errors.New("PAYGL_006_REJECTED")

// ERPLineState is the closed external posting-state vocabulary.
type ERPLineState string

const (
	ERPLineAccepted ERPLineState = "ACCEPTED"
	ERPLineRejected ERPLineState = "REJECTED"
	ERPLinePartial  ERPLineState = "PARTIAL"
	ERPLineUnknown  ERPLineState = "UNKNOWN"
)

// ERPLine is one externally observed posting line.
type ERPLine struct {
	JournalOrdinal int
	ExternalID     string
	Amount         values.Decimal
	Currency       string
	Dimensions     []string
	State          ERPLineState
}

// ERPObservation is the external system's view of one journal posting.
type ERPObservation struct {
	Source        string
	BatchID       string
	JournalDigest string
	Lines         []ERPLine
	Total         values.Decimal
	Currency      string
}

// ERPReader is the port through which ERP observations arrive. The domain
// never calls a provider directly; adapters implement this port.
type ERPReader interface {
	ReadObservation(journalDigest string) (ERPObservation, error)
}

// MemoryERPStore is a kernel-pure ERPReader for tests and local harnesses.
type MemoryERPStore struct {
	mu   sync.RWMutex
	held map[string]ERPObservation
}

// NewMemoryERPStore returns an empty observation store.
func NewMemoryERPStore() *MemoryERPStore {
	return &MemoryERPStore{held: map[string]ERPObservation{}}
}

// PutObservation holds one well-formed observation keyed by journal digest.
func (m *MemoryERPStore) PutObservation(obs ERPObservation) error {
	if strings.TrimSpace(obs.Source) == "" || strings.TrimSpace(obs.BatchID) == "" ||
		strings.TrimSpace(obs.JournalDigest) == "" || strings.TrimSpace(obs.Currency) == "" {
		return errors.Join(ErrPostingReconciliationRejected, errors.New("observation source, batch, journal digest and currency are required"))
	}
	if err := obs.Total.Validate(); err != nil {
		return errors.Join(ErrPostingReconciliationRejected, fmt.Errorf("observation total: %v", err))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.held[obs.JournalDigest] = obs
	return nil
}

// ReadObservation returns the held observation for one journal digest.
func (m *MemoryERPStore) ReadObservation(journalDigest string) (ERPObservation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	obs, ok := m.held[journalDigest]
	if !ok {
		return ERPObservation{}, errors.Join(ErrPostingReconciliationRejected, fmt.Errorf("no observation for %q", journalDigest))
	}
	return obs, nil
}

// PostingLineStatus is the per-line reconciliation outcome.
type PostingLineStatus string

const (
	PostingPosted   PostingLineStatus = "POSTED"
	PostingMismatch PostingLineStatus = "MISMATCH"
	PostingUnknown  PostingLineStatus = "UNKNOWN"
)

// PostingRepairScope names the span a posting repair must cover.
type PostingRepairScope string

const (
	PostingRepairLine     PostingRepairScope = "LINE"
	PostingRepairTotals   PostingRepairScope = "TOTALS"
	PostingRepairCoverage PostingRepairScope = "COVERAGE"
)

// PostingRepair is a scoped repair obligation for a posting gap.
type PostingRepair struct {
	Scope   PostingRepairScope
	Ordinal int
	Detail  string
}

// PostingAggregate is the aggregate posting outcome.
type PostingAggregate string

const (
	PostingSettled          PostingAggregate = "SETTLED"
	PostingRepairRequired   PostingAggregate = "REPAIR_REQUIRED"
	PostingUnknownAggregate PostingAggregate = "UNKNOWN"
)

// PostingLineResult pairs one journal line with its posting outcome.
type PostingLineResult struct {
	Ordinal    int
	ExternalID string
	Status     PostingLineStatus
	Detail     string
}

// PostingReconciliation is the immutable comparison of one journal against
// one external observation.
type PostingReconciliation struct {
	JournalDigest string
	BatchID       string
	Lines         []PostingLineResult
	Aggregate     PostingAggregate
	Repairs       []PostingRepair
	Digest        string
}

// ReconcilePosting compares journal, lines, totals, dimensions and external
// IDs. Full agreement settles; rejects, partials and mismatches require
// repair; pure unknowns stay unknown for re-observation.
func ReconcilePosting(journal Journal, obs ERPObservation) (PostingReconciliation, error) {
	fail := func(format string, args ...any) (PostingReconciliation, error) {
		return PostingReconciliation{}, errors.Join(ErrPostingReconciliationRejected, fmt.Errorf(format, args...))
	}
	if err := journal.Validate(); err != nil {
		return fail("journal: %v", err)
	}
	if strings.TrimSpace(obs.Source) == "" || strings.TrimSpace(obs.BatchID) == "" || strings.TrimSpace(obs.Currency) == "" {
		return fail("observation source, batch and currency are required")
	}
	if err := obs.Total.Validate(); err != nil {
		return fail("observation total: %v", err)
	}
	if obs.JournalDigest != "" && obs.JournalDigest != journal.Digest {
		return fail("observation binds another journal")
	}
	byOrdinal := map[int]ERPLine{}
	for _, line := range obs.Lines {
		byOrdinal[line.JournalOrdinal] = line
	}
	rec := PostingReconciliation{JournalDigest: journal.Digest, BatchID: obs.BatchID}
	currencyMatch := obs.Currency == journal.Currency() && journal.Currency() != ""
	for _, line := range journal.Lines {
		result := PostingLineResult{Ordinal: line.Ordinal, Status: PostingPosted}
		external, ok := byOrdinal[line.Ordinal]
		switch {
		case !ok:
			result.Status = PostingUnknown
			result.Detail = "no external line observed"
			rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairCoverage, Ordinal: line.Ordinal, Detail: result.Detail})
		case !currencyMatch:
			result.Status = PostingUnknown
			result.Detail = fmt.Sprintf("currency %q cannot be compared to journal", obs.Currency)
			rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case external.State == ERPLineRejected || external.State == ERPLinePartial:
			result.Status = PostingMismatch
			result.Detail = fmt.Sprintf("external state %s", external.State)
			rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case external.State == ERPLineUnknown:
			result.Status = PostingUnknown
			result.Detail = "external state unknown"
			rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case external.State != ERPLineAccepted:
			return fail("line %d: external state %q is not declared", line.Ordinal, external.State)
		case strings.TrimSpace(external.ExternalID) == "":
			result.Status = PostingMismatch
			result.Detail = "accepted without an external id"
			rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairLine, Ordinal: line.Ordinal, Detail: result.Detail})
		case !external.Amount.Equal(line.Amount):
			result.Status = PostingMismatch
			result.Detail = fmt.Sprintf("external amount %s, journal %s", external.Amount, line.Amount)
			rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairLine, Ordinal: line.Ordinal, Detail: result.Detail})
		default:
			found := false
			for _, id := range external.Dimensions {
				if id == line.Dimension.Value {
					found = true
					break
				}
			}
			if !found {
				result.Status = PostingMismatch
				result.Detail = fmt.Sprintf("dimension %q missing externally", line.Dimension.Value)
				rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairLine, Ordinal: line.Ordinal, Detail: result.Detail})
			} else {
				result.ExternalID = external.ExternalID
			}
		}
		rec.Lines = append(rec.Lines, result)
	}
	if len(obs.Lines) > len(journal.Lines) {
		rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairCoverage, Detail: "external lines without journal counterpart"})
	}
	totalsOK := currencyMatch && obs.Total.Equal(journal.TotalDebits) && len(obs.Lines) == len(journal.Lines)
	if !totalsOK {
		detail := fmt.Sprintf("external total %s %s, journal %s", obs.Total, obs.Currency, journal.TotalDebits)
		rec.Repairs = append(rec.Repairs, PostingRepair{Scope: PostingRepairTotals, Detail: detail})
	}
	rec.Aggregate = PostingSettled
	for _, result := range rec.Lines {
		switch result.Status {
		case PostingMismatch:
			rec.Aggregate = PostingRepairRequired
		case PostingUnknown:
			if rec.Aggregate == PostingSettled {
				rec.Aggregate = PostingUnknownAggregate
			}
		}
	}
	if !totalsOK {
		rec.Aggregate = PostingRepairRequired
	}
	rec.Digest = canonicalbytes.Digest(rec.body())
	return rec, nil
}

func (r PostingReconciliation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paygl.PostingReconciliation", 1).
		String("journal_digest", r.JournalDigest).String("batch_id", r.BatchID).
		String("aggregate", string(r.Aggregate)).Count("lines", len(r.Lines))
	for _, line := range r.Lines {
		w.Int("ordinal", int64(line.Ordinal)).String("external_id", line.ExternalID).
			String("status", string(line.Status))
	}
	w.Count("repairs", len(r.Repairs))
	for _, repair := range r.Repairs {
		w.String("repair_scope", string(repair.Scope)).Int("repair_ordinal", int64(repair.Ordinal)).
			String("repair_detail", repair.Detail)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks the reconciliation against its journal: every posted
// line agrees, every other line has a repair, and the digest binds.
func (r PostingReconciliation) Validate(journal Journal) error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrPostingReconciliationRejected, fmt.Errorf(format, args...))
	}
	if r.JournalDigest != journal.Digest {
		return fail("reconciliation binds another journal")
	}
	if len(r.Lines) != len(journal.Lines) {
		return fail("line coverage differs from journal")
	}
	repaired := map[int]bool{}
	for _, repair := range r.Repairs {
		repaired[repair.Ordinal] = true
	}
	for i, result := range r.Lines {
		if result.Ordinal != journal.Lines[i].Ordinal {
			return fail("line %d is out of order", i)
		}
		if result.Status != PostingPosted && !repaired[result.Ordinal] {
			return fail("line %d has no repair", result.Ordinal)
		}
		if result.Status == PostingPosted && strings.TrimSpace(result.ExternalID) == "" {
			return fail("line %d posted without an external id", result.Ordinal)
		}
	}
	if r.Aggregate == PostingSettled && len(r.Repairs) != 0 {
		return fail("settled with repairs")
	}
	if r.Digest != canonicalbytes.Digest(r.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}
