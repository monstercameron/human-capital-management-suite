// LEARN-007: reconcile external LMS state. Expected enrollment,
// completion and credential positions versus provider observations
// classify every gap as missing, extra, stale, conflicting or unknown and
// produce repair plans. A stale LMS completion or a mutable definition
// behind the recorded digest never becomes credential truth: the seal
// returns LEARN_007_REJECTED with zero effects.
package learning

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CodeRejected007 is the typed LEARN-007 denial code.
const CodeRejected007 = "LEARN_007_REJECTED"

var ErrInvalidLMSObservation = errors.New("learning: invalid LMS observation")

// LMS states and discrepancy kinds.
const (
	LMSEnrolled    = "ENROLLED"
	LMSCompleted   = "COMPLETED"
	LMSMissing     = "MISSING"
	LMSExtra       = "EXTRA"
	LMSStale       = "STALE"
	LMSConflicting = "CONFLICTING"
	LMSUnknown     = "UNKNOWN"
)

// LMSExpectation is the registry-owned expected position.
type LMSExpectation struct {
	LearnerID     string
	CourseID      string
	Version       uint64
	VersionDigest string
	ExpectedState string
}

// LMSObservation is one provider-reported position.
type LMSObservation struct {
	LearnerID     string
	CourseID      string
	Version       uint64
	VersionDigest string
	State         string
	Source        string
	At            time.Time
}

// LMSDiscrepancy is one classified gap.
type LMSDiscrepancy struct {
	LearnerID string
	Kind      string
	Detail    string
}

// LMSRepair is the repair plan for one discrepancy.
type LMSRepair struct {
	LearnerID string
	Action    string
	Owner     string
}

func lmsKey(learnerID, courseID string, version uint64) string {
	return fmt.Sprintf("%s\x00%s@%d", learnerID, courseID, version)
}

// RecordLMSObservation stores one provider observation for later
// reconciliation against expectations.
func (r *Registry) RecordLMSObservation(ob LMSObservation) error {
	if strings.TrimSpace(ob.LearnerID) == "" || strings.TrimSpace(ob.CourseID) == "" ||
		ob.Version == 0 || strings.TrimSpace(ob.Source) == "" || ob.At.IsZero() {
		return fmt.Errorf("%w: learner, course, version, source and instant are required", ErrInvalidLMSObservation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lmsObs = append(r.lmsObs, ob)
	return nil
}

// ReconcileLMS classifies expected positions against provider
// observations. Sourceless observations are refused, never classified.
func (r *Registry) ReconcileLMS(expected []LMSExpectation, observed []LMSObservation) ([]LMSDiscrepancy, []LMSRepair, error) {
	for _, ob := range observed {
		if strings.TrimSpace(ob.Source) == "" {
			return nil, nil, fmt.Errorf("%w: observation for %s has no source", ErrInvalidLMSObservation, ob.LearnerID)
		}
		if ob.At.IsZero() {
			return nil, nil, fmt.Errorf("%w: observation for %s has no instant", ErrInvalidLMSObservation, ob.LearnerID)
		}
	}
	latest := map[string]LMSObservation{}
	for _, ob := range observed {
		key := lmsKey(ob.LearnerID, ob.CourseID, ob.Version)
		cur, ok := latest[key]
		if !ok || ob.At.After(cur.At) {
			latest[key] = ob
		}
	}
	discrepancies := []LMSDiscrepancy{}
	for _, exp := range expected {
		ob, ok := latest[lmsKey(exp.LearnerID, exp.CourseID, exp.Version)]
		if !ok {
			discrepancies = append(discrepancies, LMSDiscrepancy{
				LearnerID: exp.LearnerID, Kind: LMSMissing,
				Detail: "expected position was never observed",
			})
			continue
		}
		switch {
		case ob.VersionDigest != exp.VersionDigest:
			discrepancies = append(discrepancies, LMSDiscrepancy{
				LearnerID: exp.LearnerID, Kind: LMSStale,
				Detail: "provider cites a version digest outside the recorded revision",
			})
		case ob.State != LMSEnrolled && ob.State != LMSCompleted:
			discrepancies = append(discrepancies, LMSDiscrepancy{
				LearnerID: exp.LearnerID, Kind: LMSUnknown,
				Detail: fmt.Sprintf("provider state %q is outside the shared vocabulary", ob.State),
			})
		case ob.State != exp.ExpectedState:
			discrepancies = append(discrepancies, LMSDiscrepancy{
				LearnerID: exp.LearnerID, Kind: LMSConflicting,
				Detail: fmt.Sprintf("state %s != %s", ob.State, exp.ExpectedState),
			})
		}
	}
	want := map[string]bool{}
	for _, exp := range expected {
		want[lmsKey(exp.LearnerID, exp.CourseID, exp.Version)] = true
	}
	for key, ob := range latest {
		if !want[key] {
			discrepancies = append(discrepancies, LMSDiscrepancy{
				LearnerID: ob.LearnerID, Kind: LMSExtra,
				Detail: "observed position was never expected",
			})
		}
	}
	sort.Slice(discrepancies, func(i, j int) bool {
		if discrepancies[i].LearnerID != discrepancies[j].LearnerID {
			return discrepancies[i].LearnerID < discrepancies[j].LearnerID
		}
		return discrepancies[i].Kind < discrepancies[j].Kind
	})
	repairs := make([]LMSRepair, 0, len(discrepancies))
	for _, d := range discrepancies {
		repairs = append(repairs, LMSRepair{
			LearnerID: d.LearnerID,
			Action:    "review-" + strings.ToLower(d.Kind),
			Owner:     "learning-operations",
		})
	}
	return discrepancies, repairs, nil
}

// SealLMSReconciliation seals a fully reconciled LMS position. Any
// discrepancy returns LEARN_007_REJECTED with zero effects.
func (r *Registry) SealLMSReconciliation(expected []LMSExpectation, observed []LMSObservation) (string, error) {
	discrepancies, _, err := r.ReconcileLMS(expected, observed)
	if err != nil {
		return "", err
	}
	if len(discrepancies) > 0 {
		first := discrepancies[0]
		return "", &Rejection{
			Code: CodeRejected007, Field: first.LearnerID,
			State: strings.ToLower(first.Kind), Version: "v1",
		}
	}
	w := canonicalbytes.New("learning-lms-reconciliation", 1)
	w.Int("expected", int64(len(expected)))
	w.Int("observed", int64(len(observed)))
	ids := make([]string, 0, len(expected))
	for _, exp := range expected {
		ids = append(ids, lmsKey(exp.LearnerID, exp.CourseID, exp.Version)+"="+exp.ExpectedState)
	}
	sort.Strings(ids)
	w.SortedStrings("positions", ids)
	raw, err := w.Bytes()
	if err != nil {
		return "", &Rejection{Code: CodeRejected007, Field: "digest", State: "unsealed", Version: "v1"}
	}
	return canonicalbytes.Digest(raw), nil
}

// SealLMSRecorded seals expectations against the recorded provider
// observations.
func (r *Registry) SealLMSRecorded(expected []LMSExpectation) (string, error) {
	r.mu.Lock()
	observed := append([]LMSObservation(nil), r.lmsObs...)
	r.mu.Unlock()
	return r.SealLMSReconciliation(expected, observed)
}
