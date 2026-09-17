// PROGRAM-006: reconcile program participation and outcomes. Expected
// enrollment/outcome state versus external observations classifies every
// gap as missing, extra, stale or conflicting and produces domain-owned
// repair plans. Sealing a reconciliation with unresolved discrepancies —
// the seeded stale-participation defect — returns PROGRAM_006_REJECTED
// with zero effects.
package program

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CodeRejected006 is the typed PROGRAM-006 denial code.
const CodeRejected006 = "PROGRAM_006_REJECTED"

var ErrInvalidObservation = errors.New("program: invalid observation")

// Discrepancy kinds.
const (
	DiscrepancyMissing     = "MISSING"
	DiscrepancyExtra       = "EXTRA"
	DiscrepancyStale       = "STALE"
	DiscrepancyConflicting = "CONFLICTING"
)

// Rejection is the typed PROGRAM-006 denial.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version string
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", e.Code, e.Field, e.State, e.Version)
}

// AsRejection reports whether err is a *Rejection.
func AsRejection(err error) (*Rejection, bool) {
	var rej *Rejection
	if errors.As(err, &rej) {
		return rej, true
	}
	return nil, false
}

// Expectation is the program-owned expected state per enrollment.
type Expectation struct {
	EnrollmentID          string
	Participant           string
	ProgramID             string
	RevisionDigest        string
	ExpectedState         string
	ExpectedOutcomeDigest string
}

// Observation is one external effect/observation of an enrollment.
type Observation struct {
	EnrollmentID          string
	Source                string
	ObservedState         string
	ObservedOutcomeDigest string
	At                    time.Time
}

// Discrepancy is one classified gap.
type Discrepancy struct {
	EnrollmentID string
	Kind         string
	Detail       string
}

// RepairPlan is the domain-owned repair for one discrepancy.
type RepairPlan struct {
	EnrollmentID string
	Action       string
	Owner        string
}

// Reconcile classifies every expected enrollment against observations and
// returns a repair plan per discrepancy. Observations without a source
// are refused, never silently classified.
func (c *Catalog) Reconcile(expected []Expectation, observed []Observation) ([]Discrepancy, []RepairPlan, error) {
	for _, ob := range observed {
		if strings.TrimSpace(ob.Source) == "" {
			return nil, nil, fmt.Errorf("%w: observation for %s has no source", ErrInvalidObservation, ob.EnrollmentID)
		}
		if ob.At.IsZero() {
			return nil, nil, fmt.Errorf("%w: observation for %s has no instant", ErrInvalidObservation, ob.EnrollmentID)
		}
	}
	latest := map[string]Observation{}
	for _, ob := range observed {
		cur, ok := latest[ob.EnrollmentID]
		if !ok || ob.At.After(cur.At) {
			latest[ob.EnrollmentID] = ob
		}
	}
	discrepancies := []Discrepancy{}
	for _, exp := range expected {
		ob, ok := latest[exp.EnrollmentID]
		if !ok {
			discrepancies = append(discrepancies, Discrepancy{
				EnrollmentID: exp.EnrollmentID, Kind: DiscrepancyMissing,
				Detail: "expected enrollment was never observed",
			})
			continue
		}
		switch {
		case ob.ObservedState != exp.ExpectedState &&
			ob.ObservedOutcomeDigest != exp.ExpectedOutcomeDigest:
			discrepancies = append(discrepancies, Discrepancy{
				EnrollmentID: exp.EnrollmentID, Kind: DiscrepancyConflicting,
				Detail: fmt.Sprintf("state %s != %s and outcome digest differs",
					ob.ObservedState, exp.ExpectedState),
			})
		case ob.ObservedState != exp.ExpectedState:
			discrepancies = append(discrepancies, Discrepancy{
				EnrollmentID: exp.EnrollmentID, Kind: DiscrepancyStale,
				Detail: fmt.Sprintf("state %s != %s", ob.ObservedState, exp.ExpectedState),
			})
		case ob.ObservedOutcomeDigest != exp.ExpectedOutcomeDigest:
			discrepancies = append(discrepancies, Discrepancy{
				EnrollmentID: exp.EnrollmentID, Kind: DiscrepancyStale,
				Detail: "outcome digest differs from expected",
			})
		}
	}
	want := map[string]bool{}
	for _, exp := range expected {
		want[exp.EnrollmentID] = true
	}
	for id := range latest {
		if !want[id] {
			discrepancies = append(discrepancies, Discrepancy{
				EnrollmentID: id, Kind: DiscrepancyExtra,
				Detail: "observed enrollment was never expected",
			})
		}
	}
	sort.Slice(discrepancies, func(i, j int) bool {
		if discrepancies[i].EnrollmentID != discrepancies[j].EnrollmentID {
			return discrepancies[i].EnrollmentID < discrepancies[j].EnrollmentID
		}
		return discrepancies[i].Kind < discrepancies[j].Kind
	})
	repairs := make([]RepairPlan, 0, len(discrepancies))
	for _, d := range discrepancies {
		repairs = append(repairs, RepairPlan{
			EnrollmentID: d.EnrollmentID,
			Action:       "review-" + strings.ToLower(d.Kind),
			Owner:        "program-operations",
		})
	}
	return discrepancies, repairs, nil
}

// SealReconciliation seals a fully reconciled position. Any discrepancy —
// including a stale participation or outcome reported reconciled —
// returns PROGRAM_006_REJECTED and records zero effects.
func (c *Catalog) SealReconciliation(expected []Expectation, observed []Observation) (string, error) {
	discrepancies, _, err := c.Reconcile(expected, observed)
	if err != nil {
		return "", err
	}
	if len(discrepancies) > 0 {
		first := discrepancies[0]
		return "", &Rejection{
			Code: CodeRejected006, Field: first.EnrollmentID,
			State: strings.ToLower(first.Kind), Version: "v1",
		}
	}
	w := canonicalbytes.New("program-reconciliation", 1)
	w.Int("expected", int64(len(expected)))
	w.Int("observed", int64(len(observed)))
	ids := make([]string, 0, len(expected))
	for _, exp := range expected {
		ids = append(ids, exp.EnrollmentID+"="+exp.ExpectedOutcomeDigest)
	}
	sort.Strings(ids)
	w.SortedStrings("positions", ids)
	raw, err := w.Bytes()
	if err != nil {
		return "", &Rejection{Code: CodeRejected006, Field: "digest", State: "unsealed", Version: "v1"}
	}
	return canonicalbytes.Digest(raw), nil
}
