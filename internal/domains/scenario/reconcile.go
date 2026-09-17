// Execution reconciliation: SCENARIO-007 reports what executing an
// approved Plan actually did, against the IntentCompilation that
// governed it.
//
// The report classifies every compiled intent as EXECUTED, DEFERRED,
// REJECTED, FAILED or UNKNOWN (no entry filed) and carries the actual
// outcome and the actual cost beside the planned value and cost, so
// outcome variance and cost variance are computed per intent. Cost
// variance compares actual against planned money for every filed entry
// regardless of status: a deferred intent that spent nothing still
// varies from its budget.
//
// The reconciliation is read-only: it binds digests and returns a new
// Reconciliation value. It never mutates the compilation, the report or
// the scenario assumptions frozen inside the compiled intents. The
// clock is the report's own ReportedAt instant, so reconciliation is
// pure: no database, no wall clock, no network.
package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ReconciliationRejectedCode is the stable machine-readable refusal code.
const ReconciliationRejectedCode = "SCENARIO_007_REJECTED"

// ErrReconciliationRejected is the sentinel for refused reconciliations.
// Match it with errors.Is rather than parsing the reason.
var ErrReconciliationRejected = errors.New("scenario: execution reconciliation rejected")

// ReconcileRejectedError names the offending field and state.
type ReconcileRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *ReconcileRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Is reports ErrReconciliationRejected without parsing the reason.
func (e *ReconcileRejectedError) Is(target error) bool {
	return target == ErrReconciliationRejected
}

// AsReconcileRejected unwraps a SCENARIO_007_REJECTED refusal.
func AsReconcileRejected(err error) (*ReconcileRejectedError, bool) {
	var rejected *ReconcileRejectedError
	if errors.As(err, &rejected) && rejected.Code == ReconciliationRejectedCode {
		return rejected, true
	}
	return nil, false
}

func reconcileRejected(field, state string) *ReconcileRejectedError {
	return &ReconcileRejectedError{Code: ReconciliationRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// ExecutionStatus is the closed vocabulary for what executing one
// compiled intent actually did.
type ExecutionStatus string

// The execution outcomes a completion report can file.
const (
	// ExecutionExecuted records an intent carried out with an actual
	// outcome and cost.
	ExecutionExecuted ExecutionStatus = "EXECUTED"
	// ExecutionDeferred records an intent postponed, not abandoned.
	ExecutionDeferred ExecutionStatus = "DEFERRED"
	// ExecutionRejected records an intent refused at execution time.
	ExecutionRejected ExecutionStatus = "REJECTED"
	// ExecutionFailed records an intent attempted but not completed.
	ExecutionFailed ExecutionStatus = "FAILED"
	// ExecutionUnknown marks a compiled intent the report says nothing
	// about. It is assigned by reconciliation, never filed.
	ExecutionUnknown ExecutionStatus = "UNKNOWN"
)

// Valid reports whether the status is declared.
func (s ExecutionStatus) Valid() bool {
	switch s {
	case ExecutionExecuted, ExecutionDeferred, ExecutionRejected, ExecutionFailed, ExecutionUnknown:
		return true
	default:
		return false
	}
}

// ExecutionEntry is one filed completion record: the actual outcome and
// the actual cost beside the planned cost. The planned outcome lives in
// the compiled intent's value. ActualValue is required for EXECUTED and
// optional otherwise; the free-text note is commentary and never
// enters the reconciliation digest.
type ExecutionEntry struct {
	Key         string
	Status      ExecutionStatus
	ActualValue TypedValue
	PlannedCost values.Decimal
	ActualCost  values.Decimal
	Note        string
}

// Validate implements validation.
func (e ExecutionEntry) Validate() error {
	if strings.TrimSpace(e.Key) == "" {
		return fmt.Errorf("%w: entry key is required", ErrInvalidScenario)
	}
	if !e.Status.Valid() || e.Status == ExecutionUnknown {
		return fmt.Errorf("%w: entry status %q cannot be filed", ErrInvalidScenario, e.Status)
	}
	if e.Status == ExecutionExecuted && e.ActualValue.Validate() != nil {
		return fmt.Errorf("%w: executed entry %q carries no actual outcome", ErrInvalidScenario, e.Key)
	}
	if err := e.PlannedCost.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q planned cost: %v", ErrInvalidScenario, e.Key, err)
	}
	if err := e.ActualCost.Validate(); err != nil {
		return fmt.Errorf("%w: entry %q actual cost: %v", ErrInvalidScenario, e.Key, err)
	}
	return nil
}

// ExecutionReport is the completion account for one compiled revision.
// RevisionDigest must equal the compilation's revision digest: the
// report reconciles against exactly the revision that governed
// execution.
type ExecutionReport struct {
	ScenarioID     string
	Revision       uint64
	RevisionDigest string
	ReportedAt     values.Instant
	Entries        []ExecutionEntry
}

// Validate implements validation.
func (r ExecutionReport) Validate() error {
	if strings.TrimSpace(r.ScenarioID) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: scenario id and non-zero revision are required", ErrInvalidScenario)
	}
	if strings.TrimSpace(r.RevisionDigest) == "" {
		return fmt.Errorf("%w: revision digest is required", ErrInvalidScenario)
	}
	if err := r.ReportedAt.Validate(); err != nil {
		return fmt.Errorf("%w: reported-at: %v", ErrInvalidScenario, err)
	}
	seen := make(map[string]struct{}, len(r.Entries))
	for _, entry := range r.Entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, dup := seen[entry.Key]; dup {
			return fmt.Errorf("%w: duplicate entry for key %q", ErrInvalidScenario, entry.Key)
		}
		seen[entry.Key] = struct{}{}
	}
	return nil
}

// IntentDelta is the reconciled account for one compiled intent: its
// execution status, whether the actual outcome or cost varied from
// plan, and the signed actual-minus-planned cost (empty when the report
// filed nothing for the key).
type IntentDelta struct {
	Key             string
	Status          ExecutionStatus
	OutcomeVariance bool
	CostVariance    bool
	CostDelta       string
}

func (d IntentDelta) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.scenario.IntentDelta", schemaVersion).
		String("key", d.Key).String("status", string(d.Status)).
		Bool("outcome_variance", d.OutcomeVariance).Bool("cost_variance", d.CostVariance).
		String("cost_delta", d.CostDelta)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Reconciliation is the read-only account of executing one compiled
// Plan: one ordered delta per compiled intent plus the status counts.
type Reconciliation struct {
	ScenarioID        string
	Revision          uint64
	RevisionDigest    string
	CompilationDigest string
	Deltas            []IntentDelta
	ExecutedCount     int
	DeferredCount     int
	RejectedCount     int
	FailedCount       int
	UnknownCount      int
	CanonicalDigest   string
}

// Validate implements validation.
func (r Reconciliation) Validate() error {
	if strings.TrimSpace(r.ScenarioID) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: scenario id and non-zero revision are required", ErrInvalidScenario)
	}
	if strings.TrimSpace(r.RevisionDigest) == "" || strings.TrimSpace(r.CompilationDigest) == "" {
		return fmt.Errorf("%w: revision and compilation digests are required", ErrInvalidScenario)
	}
	if len(r.Deltas) == 0 {
		return fmt.Errorf("%w: at least one delta is required", ErrInvalidScenario)
	}
	previous := ""
	counts := map[ExecutionStatus]int{}
	for _, delta := range r.Deltas {
		if strings.TrimSpace(delta.Key) == "" || !delta.Status.Valid() {
			return fmt.Errorf("%w: delta carries no key or declared status", ErrInvalidScenario)
		}
		if delta.Key < previous {
			return fmt.Errorf("%w: deltas are not ordered by key", ErrInvalidScenario)
		}
		previous = delta.Key
		counts[delta.Status]++
	}
	if counts[ExecutionExecuted] != r.ExecutedCount || counts[ExecutionDeferred] != r.DeferredCount ||
		counts[ExecutionRejected] != r.RejectedCount || counts[ExecutionFailed] != r.FailedCount ||
		counts[ExecutionUnknown] != r.UnknownCount {
		return fmt.Errorf("%w: status counts do not match the deltas", ErrInvalidScenario)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidScenario)
	}
	return nil
}

func (r Reconciliation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.scenario.Reconciliation", schemaVersion).
		String("scenario_id", r.ScenarioID).Int("revision", int64(r.Revision)).
		String("revision_digest", r.RevisionDigest).String("compilation_digest", r.CompilationDigest).
		Count("deltas", len(r.Deltas))
	deltas := append([]IntentDelta(nil), r.Deltas...)
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Key < deltas[j].Key })
	for _, delta := range deltas {
		body := delta.body()
		if body == nil {
			return nil
		}
		w.Field("delta", body)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r Reconciliation) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Explain renders the bounded human-readable account: identity, status
// counts and per-key variance flags — never assumption values.
func (r Reconciliation) Explain() string {
	flags := make([]string, 0, len(r.Deltas))
	for _, delta := range r.Deltas {
		flag := delta.Key + "=" + string(delta.Status)
		if delta.OutcomeVariance {
			flag += "+outcome"
		}
		if delta.CostVariance {
			flag += "+cost"
		}
		flags = append(flags, flag)
	}
	return fmt.Sprintf("reconciliation scenario=%s revision=%d executed=%d deferred=%d rejected=%d failed=%d unknown=%d deltas=%s",
		r.ScenarioID, r.Revision, r.ExecutedCount, r.DeferredCount, r.RejectedCount, r.FailedCount, r.UnknownCount,
		strings.Join(flags, ","))
}

// ReconcileExecution reports the completion report against the
// compilation that governed execution. Every compiled intent receives
// exactly one ordered delta; compiled keys the report omits reconcile
// as UNKNOWN. Entries for keys the compilation never governed, the
// wrong scenario or revision, or a moved revision digest are rejected
// with SCENARIO_007_REJECTED. Inputs are only read.
func ReconcileExecution(compiled IntentCompilation, report ExecutionReport) (Reconciliation, error) {
	if err := compiled.Validate(); err != nil {
		return Reconciliation{}, err
	}
	seen := make(map[string]struct{}, len(report.Entries))
	for _, entry := range report.Entries {
		if _, dup := seen[entry.Key]; dup {
			return Reconciliation{}, reconcileRejected("entry", "duplicate-key")
		}
		seen[entry.Key] = struct{}{}
	}
	if err := report.Validate(); err != nil {
		return Reconciliation{}, err
	}
	if report.ScenarioID != compiled.ScenarioID {
		return Reconciliation{}, reconcileRejected("scenario-id", "mismatch")
	}
	if report.Revision != compiled.Revision {
		return Reconciliation{}, reconcileRejected("revision", "mismatch")
	}
	if report.RevisionDigest != compiled.RevisionDigest {
		return Reconciliation{}, reconcileRejected("revision-digest", "mismatch")
	}
	governed := make(map[string]CompiledIntent, len(compiled.Intents))
	for _, intent := range compiled.Intents {
		governed[intent.Key] = intent
	}
	entries := make(map[string]ExecutionEntry, len(report.Entries))
	for _, entry := range report.Entries {
		if _, ok := governed[entry.Key]; !ok {
			return Reconciliation{}, reconcileRejected("entry", "ungoverned-key")
		}
		entries[entry.Key] = entry
	}
	keys := make([]string, 0, len(governed))
	for key := range governed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	reconciled := Reconciliation{
		ScenarioID: compiled.ScenarioID, Revision: compiled.Revision,
		RevisionDigest: compiled.RevisionDigest, CompilationDigest: compiled.CanonicalDigest,
		Deltas: make([]IntentDelta, 0, len(keys)),
	}
	for _, key := range keys {
		intent := governed[key]
		entry, filed := entries[key]
		delta := IntentDelta{Key: key, Status: ExecutionUnknown}
		if filed {
			delta.Status = entry.Status
			if entry.Status == ExecutionExecuted {
				delta.OutcomeVariance = !bytes.Equal(intent.Value.Canonical(), entry.ActualValue.Canonical())
			}
			delta.CostVariance = entry.ActualCost.Cmp(entry.PlannedCost) != 0
			costDelta, err := entry.ActualCost.Sub(entry.PlannedCost)
			if err != nil {
				return Reconciliation{}, reconcileRejected("cost", "incomparable")
			}
			delta.CostDelta = costDelta.String()
		}
		reconciled.Deltas = append(reconciled.Deltas, delta)
		switch delta.Status {
		case ExecutionExecuted:
			reconciled.ExecutedCount++
		case ExecutionDeferred:
			reconciled.DeferredCount++
		case ExecutionRejected:
			reconciled.RejectedCount++
		case ExecutionFailed:
			reconciled.FailedCount++
		case ExecutionUnknown:
			reconciled.UnknownCount++
		}
	}
	reconciled.CanonicalDigest = reconciled.computedDigest()
	if err := reconciled.Validate(); err != nil {
		return Reconciliation{}, err
	}
	return reconciled, nil
}
