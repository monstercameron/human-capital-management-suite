// PROGRAM-005: calculate and explain program outcomes. The core binds
// participant, snapshots, rules, funding, inputs and time, and returns a
// canonical value with status, obligations, unknowns and a safe
// explanation — but it carries no domain formula: every calculation runs
// through a registered formula looked up by id.
package program

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var (
	ErrInvalidOutcome = errors.New("program: invalid outcome input")
	ErrUnknownFormula = errors.New("program: unknown outcome formula")
)

// Outcome statuses share the cross-domain vocabulary.
const (
	OutcomeAchieved = "ACHIEVED"
	OutcomePartial  = "PARTIAL"
	OutcomePending  = "PENDING"
	OutcomeUnknown  = "UNKNOWN"
)

// OutcomeInput is everything an outcome binds.
type OutcomeInput struct {
	Participant    string
	ProgramID      string
	RevisionDigest string
	PopulationRef  string
	EligibilityRef string
	CycleRef       string
	FundingRef     string
	FormulaID      string
	FormulaVersion string
	Inputs         map[string]string
	UnknownInputs  []string
	At             time.Time
}

// OutcomeResult is the calculated, explained outcome.
type OutcomeResult struct {
	Value       string
	Status      string
	Obligations []string
	Unknowns    []string
	Explanation string
	Digest      string
}

// FormulaFunc is a domain-supplied calculation: canonical decimal out,
// error on uncomputable input. Registration keeps formulas out of the core.
type FormulaFunc func(inputs map[string]string) (string, error)

// RegisterFormula publishes a versioned domain formula by id.
func (c *Catalog) RegisterFormula(id string, fn FormulaFunc) {
	c.formulas[id] = fn
}

// isDecimalText reports whether s is a plain decimal literal.
func isDecimalText(s string) bool {
	if s == "" {
		return false
	}
	digits := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] >= '0' && s[i] <= '9':
			digits++
		case s[i] == '.' || s[i] == '+' || s[i] == '-':
			if (s[i] == '+' || s[i] == '-') && i != 0 {
				return false
			}
			if s[i] == '.' && strings.Count(s, ".") > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}

// CanonicalDecimal normalizes a decimal literal: sign, no leading zeros,
// no trailing fraction zeros, "0" for zero.
func CanonicalDecimal(s string) (string, error) {
	if !isDecimalText(s) {
		return "", fmt.Errorf("%w: %q is not a decimal", ErrInvalidOutcome, s)
	}
	neg := false
	t := s
	if strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-") {
		neg = strings.HasPrefix(t, "-")
		t = t[1:]
	}
	intPart, fracPart := t, ""
	if i := strings.IndexByte(t, '.'); i >= 0 {
		intPart, fracPart = t[:i], t[i+1:]
	}
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	fracPart = strings.TrimRight(fracPart, "0")
	out := intPart
	if fracPart != "" {
		out += "." + fracPart
	}
	if out == "0" {
		return "0", nil
	}
	if neg {
		out = "-" + out
	}
	return out, nil
}

func outcomeDigest(res OutcomeResult, in OutcomeInput) (string, error) {
	w := canonicalbytes.New("program-outcome", 1)
	w.String("participant", in.Participant)
	w.String("program_id", in.ProgramID)
	w.String("revision_digest", in.RevisionDigest)
	w.String("population_ref", in.PopulationRef)
	w.String("eligibility_ref", in.EligibilityRef)
	w.String("cycle_ref", in.CycleRef)
	w.String("funding_ref", in.FundingRef)
	w.String("formula_id", in.FormulaID)
	w.String("formula_version", in.FormulaVersion)
	keys := make([]string, 0, len(in.Inputs))
	for k := range in.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.String("input:"+k, in.Inputs[k])
	}
	w.SortedStrings("unknown_inputs", in.UnknownInputs)
	w.String("at", in.At.UTC().Format(time.RFC3339))
	w.String("value", res.Value)
	w.String("status", res.Status)
	w.SortedStrings("obligations", res.Obligations)
	w.SortedStrings("unknowns", res.Unknowns)
	w.String("explanation", res.Explanation)
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// Calculate binds the input, runs the registered formula, canonicalizes
// the value and seals the explained result.
func (c *Catalog) Calculate(in OutcomeInput) (OutcomeResult, error) {
	for _, ref := range []struct {
		name, value string
	}{
		{"participant", in.Participant},
		{"program_id", in.ProgramID},
		{"revision_digest", in.RevisionDigest},
		{"population_ref", in.PopulationRef},
		{"eligibility_ref", in.EligibilityRef},
		{"cycle_ref", in.CycleRef},
		{"funding_ref", in.FundingRef},
		{"formula_id", in.FormulaID},
		{"formula_version", in.FormulaVersion},
	} {
		if strings.TrimSpace(ref.value) == "" {
			return OutcomeResult{}, fmt.Errorf("%w: %s is required", ErrInvalidOutcome, ref.name)
		}
	}
	if in.At.IsZero() {
		return OutcomeResult{}, fmt.Errorf("%w: instant is required", ErrInvalidOutcome)
	}
	if !c.revDigests[in.RevisionDigest] {
		return OutcomeResult{}, fmt.Errorf("%w: revision digest is unknown", ErrInvalidOutcome)
	}
	fn, ok := c.formulas[in.FormulaID]
	if !ok {
		// Generic denial: the registry's contents stay undisclosed.
		return OutcomeResult{}, fmt.Errorf("%w: formula not available", ErrUnknownFormula)
	}
	raw, err := fn(in.Inputs)
	if err != nil {
		return OutcomeResult{}, fmt.Errorf("%w: %v", ErrInvalidOutcome, err)
	}
	value, err := CanonicalDecimal(raw)
	if err != nil {
		return OutcomeResult{}, err
	}
	res := OutcomeResult{
		Value:    value,
		Status:   OutcomeAchieved,
		Unknowns: append([]string(nil), in.UnknownInputs...),
	}
	if len(in.UnknownInputs) > 0 {
		res.Status = OutcomeUnknown
	}
	keys := make([]string, 0, len(in.Inputs))
	for k := range in.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+in.Inputs[k])
	}
	res.Explanation = fmt.Sprintf("participant %s under %s/%s with funding %s via %s@%s on inputs %s at %s",
		in.Participant, in.PopulationRef, in.CycleRef, in.FundingRef,
		in.FormulaID, in.FormulaVersion, strings.Join(pairs, ","),
		in.At.UTC().Format(time.RFC3339))
	digest, err := outcomeDigest(res, in)
	if err != nil {
		return OutcomeResult{}, err
	}
	res.Digest = digest
	return res, nil
}
