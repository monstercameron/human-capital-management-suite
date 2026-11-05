// Ordinary absence commit, cancel and team coverage (WF-CAP-004).
//
// absence.commit posts an ordinary (non-protected) absence and its balance
// debit; absence.cancel restores the debit exactly; absence.team_coverage
// reports absence overlap at the caller's permitted granularity. Commit and
// cancel post through the caller's balance EntryStore and record in the
// caller's OrdinaryLedger: both are idempotent on their keys, so a retry
// never double-debits or double-restores.
package availability

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrOrdinaryAbsenceInvalid refuses a malformed commit or cancel.
	ErrOrdinaryAbsenceInvalid = errors.New("availability: invalid ordinary absence request")
	// ErrOrdinaryAbsenceConflicted refuses to commit a window whose
	// simulation reports conflicts: unknown hours must never become a
	// debit.
	ErrOrdinaryAbsenceConflicted = errors.New("availability: absence window has unresolved conflicts")
	// ErrOrdinaryAbsenceUnknown refuses to cancel an absence the ledger
	// never committed.
	ErrOrdinaryAbsenceUnknown = errors.New("availability: unknown ordinary absence")
	// ErrOrdinaryCoverageDenied refuses an undeclared coverage granularity.
	ErrOrdinaryCoverageDenied = errors.New("availability: coverage granularity is not declared")
)

// OrdinaryState is the lifecycle of one committed ordinary absence.
type OrdinaryState string

const (
	// OrdinaryCommitted says the absence and its debit are posted.
	OrdinaryCommitted OrdinaryState = "COMMITTED"
	// OrdinaryCancelled says the debit was restored by a cancel.
	OrdinaryCancelled OrdinaryState = "CANCELLED"
)

// OrdinaryAbsence is one committed ordinary absence: the simulated window
// it was committed from, the exact debit posted, and its lifecycle state.
type OrdinaryAbsence struct {
	ID           string
	Worker       values.EntityRef
	Account      string
	WindowDigest string
	Dates        []values.LocalDate
	Hours        float64
	Debit        values.Decimal
	DebitDigest  string
	State        OrdinaryState
	CommittedAt  time.Time
	CancelledAt  time.Time
}

// OrdinaryCommitRequest carries everything absence.commit needs. Result
// must be a verified, conflict-free simulation; Key is the idempotency
// key shared by the absence record and its debit entry.
type OrdinaryCommitRequest struct {
	Worker  values.EntityRef
	Account string
	Result  AbsenceWindowResult
	Key     string
	At      time.Time
}

// OrdinaryLedger is the committed-absence record: an append-only,
// mutex-guarded map from absence ID with idempotency-key replay.
type OrdinaryLedger struct {
	mu       sync.Mutex
	records  map[string]*OrdinaryAbsence
	byKey    map[string]string
	cancelBy map[string]string
}

// NewOrdinaryLedger starts an empty committed-absence record.
func NewOrdinaryLedger() *OrdinaryLedger {
	return &OrdinaryLedger{records: make(map[string]*OrdinaryAbsence), byKey: make(map[string]string), cancelBy: make(map[string]string)}
}

// Committed returns every absence still in COMMITTED state.
func (l *OrdinaryLedger) Committed() []OrdinaryAbsence {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []OrdinaryAbsence
	for _, record := range l.records {
		if record.State == OrdinaryCommitted {
			out = append(out, *record)
		}
	}
	return out
}

// CommitOrdinaryAbsence verifies the simulated window, posts the hours
// debit to the worker's balance account, and records the absence. A
// window with conflicts is refused. Repeating a key replays the original
// record without posting again.
func CommitOrdinaryAbsence(ledger *OrdinaryLedger, store *balance.EntryStore, def balance.AccumulatorDefinition, req OrdinaryCommitRequest) (OrdinaryAbsence, error) {
	if ledger == nil || store == nil {
		return OrdinaryAbsence{}, fmt.Errorf("%w: ledger and balance store are required", ErrOrdinaryAbsenceInvalid)
	}
	if strings.TrimSpace(req.Account) == "" || strings.TrimSpace(req.Key) == "" || req.At.IsZero() {
		return OrdinaryAbsence{}, fmt.Errorf("%w: account, key and commit time are required", ErrOrdinaryAbsenceInvalid)
	}
	if err := req.Worker.Validate(); err != nil {
		return OrdinaryAbsence{}, fmt.Errorf("%w: worker: %v", ErrOrdinaryAbsenceInvalid, err)
	}
	if !req.Result.Verify() {
		return OrdinaryAbsence{}, fmt.Errorf("%w: window digest does not verify", ErrOrdinaryAbsenceInvalid)
	}
	if len(req.Result.Conflicts) != 0 {
		return OrdinaryAbsence{}, fmt.Errorf("%w: %d conflicts", ErrOrdinaryAbsenceConflicted, len(req.Result.Conflicts))
	}
	if req.Result.Hours <= 0 {
		return OrdinaryAbsence{}, fmt.Errorf("%w: window consumes no hours", ErrOrdinaryAbsenceInvalid)
	}
	amount, err := absenceHoursDecimal(req.Result.Hours)
	if err != nil {
		return OrdinaryAbsence{}, fmt.Errorf("%w: hours: %v", ErrOrdinaryAbsenceInvalid, err)
	}

	ledger.mu.Lock()
	if id, ok := ledger.byKey[req.Key]; ok {
		record := *ledger.records[id]
		ledger.mu.Unlock()
		return record, nil
	}
	ledger.mu.Unlock()

	entry := balance.BalanceEntry{
		AccountID: req.Account, DefinitionID: def.ID, DefinitionVersion: def.Version,
		Unit: def.Unit, Currency: def.Currency, Subject: def.Subject, Period: string(def.Period),
		Dimensions: map[string]string{"worker_id": req.Worker.Id},
		Kind:       balance.Debit, Amount: amount, EntryType: "ABSENCE_DEBIT",
		SourceTransactionID: req.Key, IdempotencyKey: req.Key + "/debit",
	}
	receipt, err := store.Post(balance.PostRequest{Entry: entry, ExpectedHead: store.Head(req.Account)}, def)
	if err != nil {
		return OrdinaryAbsence{}, fmt.Errorf("%w: debit: %v", ErrOrdinaryAbsenceInvalid, err)
	}
	dates := make([]values.LocalDate, 0, len(req.Result.Days))
	for _, day := range req.Result.Days {
		dates = append(dates, day.Date)
	}
	record := OrdinaryAbsence{
		ID: "ordinary:" + req.Key, Worker: req.Worker, Account: req.Account,
		WindowDigest: req.Result.Digest, Dates: dates, Hours: req.Result.Hours,
		Debit: amount, DebitDigest: receipt.Digest, State: OrdinaryCommitted, CommittedAt: req.At.UTC(),
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.records[record.ID] = &record
	ledger.byKey[req.Key] = record.ID
	kept := record
	return kept, nil
}

// CancelOrdinaryAbsence restores a committed absence's exact debit with a
// credit and marks the absence CANCELLED. Repeating a cancel key replays
// the cancellation without posting again, as does cancelling an already
// cancelled absence.
func CancelOrdinaryAbsence(ledger *OrdinaryLedger, store *balance.EntryStore, def balance.AccumulatorDefinition, absenceID, key string, at time.Time) (OrdinaryAbsence, error) {
	if ledger == nil || store == nil {
		return OrdinaryAbsence{}, fmt.Errorf("%w: ledger and balance store are required", ErrOrdinaryAbsenceInvalid)
	}
	if strings.TrimSpace(absenceID) == "" || strings.TrimSpace(key) == "" || at.IsZero() {
		return OrdinaryAbsence{}, fmt.Errorf("%w: absence, key and cancel time are required", ErrOrdinaryAbsenceInvalid)
	}
	ledger.mu.Lock()
	if id, ok := ledger.cancelBy[key]; ok {
		record := *ledger.records[id]
		ledger.mu.Unlock()
		return record, nil
	}
	record, ok := ledger.records[absenceID]
	if !ok {
		ledger.mu.Unlock()
		return OrdinaryAbsence{}, fmt.Errorf("%w: %s", ErrOrdinaryAbsenceUnknown, absenceID)
	}
	if record.State == OrdinaryCancelled {
		kept := *record
		ledger.cancelBy[key] = record.ID
		ledger.mu.Unlock()
		return kept, nil
	}
	debit := record.Debit
	account := record.Account
	worker := record.Worker
	ledger.mu.Unlock()

	entry := balance.BalanceEntry{
		AccountID: account, DefinitionID: def.ID, DefinitionVersion: def.Version,
		Unit: def.Unit, Currency: def.Currency, Subject: def.Subject, Period: string(def.Period),
		Dimensions: map[string]string{"worker_id": worker.Id},
		Kind:       balance.Credit, Amount: debit, EntryType: "ABSENCE_RESTORE",
		SourceTransactionID: key, IdempotencyKey: key + "/credit",
	}
	if _, err := store.Post(balance.PostRequest{Entry: entry, ExpectedHead: store.Head(account)}, def); err != nil {
		return OrdinaryAbsence{}, fmt.Errorf("%w: restore: %v", ErrOrdinaryAbsenceInvalid, err)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	record.State = OrdinaryCancelled
	record.CancelledAt = at.UTC()
	ledger.cancelBy[key] = record.ID
	return *record, nil
}

// CoverageGranularity is the caller's permitted team-coverage detail.
type CoverageGranularity string

const (
	// CoverageCounts reports per-day absent counts with no identities.
	CoverageCounts CoverageGranularity = "COUNTS"
	// CoverageIdentified additionally names the absent workers per day.
	CoverageIdentified CoverageGranularity = "IDENTIFIED"
)

// TeamDayCoverage is one day's absence overlap. Workers is populated only
// under CoverageIdentified; under CoverageCounts it is always empty.
type TeamDayCoverage struct {
	Date        values.LocalDate
	AbsentCount int
	Workers     []string
}

// TeamCoverage reports, for each requested day, how many of the given
// committed absences cover it. Counts never carry identities: naming
// workers requires CoverageIdentified.
func TeamCoverage(absences []OrdinaryAbsence, dates []values.LocalDate, granularity CoverageGranularity) ([]TeamDayCoverage, error) {
	if granularity != CoverageCounts && granularity != CoverageIdentified {
		return nil, fmt.Errorf("%w: %q", ErrOrdinaryCoverageDenied, granularity)
	}
	out := make([]TeamDayCoverage, 0, len(dates))
	for _, date := range dates {
		day := TeamDayCoverage{Date: date}
		for _, absence := range absences {
			if absence.State != OrdinaryCommitted {
				continue
			}
			for _, covered := range absence.Dates {
				if covered == date {
					day.AbsentCount++
					if granularity == CoverageIdentified {
						day.Workers = append(day.Workers, absence.Worker.Id)
					}
					break
				}
			}
		}
		out = append(out, day)
	}
	return out, nil
}

// absenceHoursDecimal converts simulated hours to the exact decimal debit.
// The nine-decimal text is the same rendering the window digest pins, and
// EXACT_REQUIRED refuses any conversion that would round it.
func absenceHoursDecimal(hours float64) (values.Decimal, error) {
	return values.NewDecimal(strconv.FormatFloat(hours, 'f', 9, 64), 9, values.RoundingExactRequired)
}
