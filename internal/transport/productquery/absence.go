package productquery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ALIGN-020: repository absence is semantic, not empty. A missing candidate
// value is never interpreted as an empty business value, and an
// authorization decision is never confused with a source gap. MapAbsence is
// the total, closed mapping from a repository cell plus its field ruling to
// the response field: every (cell state, ruling effect) pair maps to exactly
// one outcome, so no absence kind collapses into another and no invalid
// combination passes silently.

// ErrAbsenceInvalid is returned when a cell and ruling cannot be mapped:
// a non-present cell carrying a value, or a ruling outside the closed
// effect set.
var ErrAbsenceInvalid = errors.New("productquery: cell state and ruling do not map to a response field")

// MapAbsence maps one repository cell under one field ruling to its
// response field. It returns included=false (with no error) only when the
// ruling denies or withholds the field: the field is then absent from the
// response uniformly, whether the source had a value or not. Every other
// pair yields exactly one state, and a non-present cell carrying a value is
// refused rather than laundered into a response.
func MapAbsence(cell Cell, effect authz.Effect) (Field, bool, error) {
	switch effect {
	case authz.EffectAllow:
		state := cell.State
		if state == "" {
			state = ValueUnknown
		}
		mapped := Cell{State: state, Value: cell.Value}
		if err := mapped.validate(); err != nil {
			return Field{}, false, fmt.Errorf("%w: %v", ErrAbsenceInvalid, err)
		}
		return Field{Disposition: effect, State: state, Value: cell.Value}, true, nil
	case authz.EffectRedacted:
		return Field{Disposition: effect, State: ValueRedacted}, true, nil
	case authz.EffectDenied, authz.EffectWithheld:
		return Field{}, false, nil
	default:
		return Field{}, false, fmt.Errorf("%w: unknown ruling effect %d", ErrAbsenceInvalid, uint8(effect))
	}
}

// AbsenceSummary counts the semantic absence states across one envelope.
// The counts distinguish "no value" (ABSENT, UNKNOWN, UNAVAILABLE) from "a
// value the caller may not see" (REDACTED) and from disclosed values
// (PRESENT), so operators and auditors can tell source gaps from
// authorization outcomes without reading any value.
type AbsenceSummary struct {
	Tenant   string         `json:"tenant"`
	Counts   map[string]int `json:"counts"`
	Subjects int            `json:"subjects"`
	Digest   string         `json:"digest"`
}

// SummarizeAbsence validates env and counts its response field states. A
// tampered or cross-tenant envelope is refused before anything is counted.
func SummarizeAbsence(env Envelope) (AbsenceSummary, error) {
	if err := env.Validate(); err != nil {
		return AbsenceSummary{}, err
	}
	counts := map[string]int{}
	for _, row := range env.Rows {
		for _, field := range row.Fields {
			counts[string(field.State)]++
		}
	}
	summary := AbsenceSummary{Tenant: env.Tenant.String(), Counts: counts, Subjects: len(env.Rows)}
	// encoding/json marshals maps with sorted keys, so this digest is
	// deterministic over the counted states.
	b, err := json.Marshal(struct {
		Tenant   string         `json:"tenant"`
		Counts   map[string]int `json:"counts"`
		Subjects int            `json:"subjects"`
	}{summary.Tenant, summary.Counts, summary.Subjects})
	if err != nil {
		return AbsenceSummary{}, fmt.Errorf("productquery: absence summary: %w", err)
	}
	sum := sha256.Sum256(b)
	summary.Digest = hex.EncodeToString(sum[:])
	return summary, nil
}

// ExplainAbsence returns a value-free description of the absence mapping.
func ExplainAbsence() string {
	return "repository cells map to exactly one response state (present, absent, unknown, unavailable, redacted); denied and withheld rulings omit uniformly; non-present cells never carry values"
}
