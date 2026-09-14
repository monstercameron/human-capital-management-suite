// Overall qualification status: QUAL-004 aggregates a validated
// per-requirement Evaluation into exactly one typed status with an
// explanation.
//
// QUALIFIED means every requirement satisfied; CONDITIONAL means
// nothing unsatisfied but something expiring or restricted;
// NOT_QUALIFIED means something missing or expired; UNKNOWN means
// there was nothing to judge. The explanation lists requirement refs
// by bucket and never discloses protected evidence content: refs only.
// A malformed evaluation rejects with QUAL_004_REJECTED naming the
// offending field and version, persisting nothing.
package qualification

import (
	"fmt"
	"sort"
)

// Overall is the only QUAL-004 outcome vocabulary.
type Overall string

// The overall outcomes.
const (
	OverallQualified    Overall = "QUALIFIED"
	OverallConditional  Overall = "CONDITIONAL"
	OverallNotQualified Overall = "NOT_QUALIFIED"
	OverallUnknown      Overall = "UNKNOWN"
)

// StatusRejectedCode is the stable machine-readable refusal code.
const StatusRejectedCode = "QUAL_004_REJECTED"

// StatusRejectedError names the offending field and version.
type StatusRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *StatusRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsStatusRejected unwraps a QUAL_004_REJECTED refusal.
func AsStatusRejected(err error) (*StatusRejectedError, bool) {
	if err == nil {
		return nil, false
	}
	if rejected, ok := err.(*StatusRejectedError); ok && rejected.Code == StatusRejectedCode {
		return rejected, true
	}
	return nil, false
}

// OverallStatus is the typed status with its explanation.
type OverallStatus struct {
	Overall    Overall
	Satisfied  []string
	Missing    []string
	Expiring   []string
	Restricted []string
}

// Summarize aggregates one validated evaluation. It is pure: no rows,
// events, outbox entries, human work or provider requests.
func Summarize(evaluation Evaluation) (OverallStatus, error) {
	if err := evaluation.Validate(); err != nil {
		return OverallStatus{}, &StatusRejectedError{Code: StatusRejectedCode, Field: "evaluation", State: "invalid", Version: schemaVersion}
	}
	status := OverallStatus{}
	if len(evaluation.Results) == 0 {
		status.Overall = OverallUnknown
		return status, nil
	}
	for _, result := range evaluation.Results {
		switch {
		case result.Restricted:
			status.Restricted = append(status.Restricted, result.Ref)
		case result.Status == StatusSatisfied:
			status.Satisfied = append(status.Satisfied, result.Ref)
		case result.Status == StatusExpiring:
			status.Expiring = append(status.Expiring, result.Ref)
		default:
			status.Missing = append(status.Missing, result.Ref)
		}
	}
	switch {
	case len(status.Missing) > 0:
		status.Overall = OverallNotQualified
	case len(status.Expiring) > 0 || len(status.Restricted) > 0:
		status.Overall = OverallConditional
	default:
		status.Overall = OverallQualified
	}
	sort.Strings(status.Satisfied)
	sort.Strings(status.Missing)
	sort.Strings(status.Expiring)
	sort.Strings(status.Restricted)
	return status, nil
}

// Explain renders the bounded human-readable account: refs by bucket,
// never evidence content.
func (s OverallStatus) Explain() string {
	return fmt.Sprintf("status %s: satisfied=%d missing=%d expiring=%d restricted=%d",
		s.Overall, len(s.Satisfied), len(s.Missing), len(s.Expiring), len(s.Restricted))
}
