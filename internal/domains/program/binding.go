// PROGRAM-003: bind program population, eligibility and cycle. A binding
// cites exact snapshot refs and the revision digest it was computed
// against; mismatched snapshot/time/version or silent partial population
// cannot enroll. Completeness and late-entry policy stay explicit.
package program

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

var ErrInvalidBinding = errors.New("program: invalid program binding")

// Completeness declares whether the bound population is whole.
type Completeness string

const (
	CompletenessComplete Completeness = "COMPLETE"
	CompletenessPartial  Completeness = "PARTIAL"
)

// LateEntryPolicy declares how late entrants are handled.
type LateEntryPolicy string

const (
	LateEntryDeny              LateEntryPolicy = "DENY"
	LateEntryAllowWithApproval LateEntryPolicy = "ALLOW_WITH_APPROVAL"
)

// Binding is one immutable population/eligibility/cycle binding.
// PopulationCount/PopulationTotal expose how whole the bound population
// is; a shortfall against the total is a partial population and must say
// so instead of silently enrolling as complete.
type Binding struct {
	ProgramID       string
	RevisionDigest  string
	PopulationRef   string
	EligibilityRef  string
	CycleRef        string
	PopulationCount int64
	PopulationTotal int64
	AsOf            time.Time
	Completeness    Completeness
	LateEntry       LateEntryPolicy
	Digest          string
}

// partial reports whether the counts declare a shortfall.
func (b Binding) partial() bool {
	return b.PopulationTotal > 0 && b.PopulationCount < b.PopulationTotal
}

// ValidateBinding checks a binding's shape without resolving it.
func ValidateBinding(b Binding) error {
	if strings.TrimSpace(b.ProgramID) == "" || strings.TrimSpace(b.RevisionDigest) == "" {
		return fmt.Errorf("%w: program and revision digest are required", ErrInvalidBinding)
	}
	for _, ref := range []struct {
		name, value string
	}{
		{"population_ref", b.PopulationRef},
		{"eligibility_ref", b.EligibilityRef},
		{"cycle_ref", b.CycleRef},
	} {
		if strings.TrimSpace(ref.value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidBinding, ref.name)
		}
	}
	if b.AsOf.IsZero() {
		return fmt.Errorf("%w: as-of instant is required", ErrInvalidBinding)
	}
	if b.Completeness != CompletenessComplete && b.Completeness != CompletenessPartial {
		return fmt.Errorf("%w: unknown completeness %q", ErrInvalidBinding, b.Completeness)
	}
	if b.LateEntry != LateEntryDeny && b.LateEntry != LateEntryAllowWithApproval {
		return fmt.Errorf("%w: unknown late-entry policy %q", ErrInvalidBinding, b.LateEntry)
	}
	return nil
}

func bindingDigest(b Binding) (string, error) {
	w := canonicalbytes.New("program-binding", 1)
	w.String("program_id", b.ProgramID)
	w.String("revision_digest", b.RevisionDigest)
	w.String("population_ref", b.PopulationRef)
	w.String("eligibility_ref", b.EligibilityRef)
	w.String("cycle_ref", b.CycleRef)
	w.Int("population_count", b.PopulationCount)
	w.Int("population_total", b.PopulationTotal)
	w.String("as_of", b.AsOf.UTC().Format(time.RFC3339))
	w.String("completeness", string(b.Completeness))
	w.String("late_entry", string(b.LateEntry))
	raw, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(raw), nil
}

// BindProgram resolves the binding against the catalog: the revision
// digest must match a recorded revision of the program, AsOf must fall
// inside that revision's interval, and a partial population must carry an
// explicit late-entry policy instead of silently enrolling.
func (c *Catalog) BindProgram(caller Caller, b Binding) (Binding, error) {
	if err := ValidateBinding(b); err != nil {
		return Binding{}, err
	}
	if err := c.authorize(caller, "tenant-acme"); err != nil {
		return Binding{}, err
	}
	revs, err := c.store.List(b.ProgramID)
	if err != nil {
		return Binding{}, fmt.Errorf("%w: %v", ErrRevisionStore, err)
	}
	var rev *Revision
	for i := range revs {
		if revs[i].Digest == b.RevisionDigest {
			rev = &revs[i]
		}
	}
	if rev == nil {
		return Binding{}, fmt.Errorf("%w: revision digest is unknown", ErrInvalidBinding)
	}
	if b.AsOf.Before(rev.From) || !b.AsOf.Before(rev.To) {
		return Binding{}, fmt.Errorf("%w: as-of instant is outside the revision interval", ErrInvalidBinding)
	}
	if b.partial() && b.Completeness != CompletenessPartial {
		return Binding{}, fmt.Errorf("%w: partial population bound as complete", ErrInvalidBinding)
	}
	if b.partial() && b.LateEntry == LateEntryDeny {
		return Binding{}, fmt.Errorf("%w: partial population needs an explicit late-entry policy", ErrInvalidBinding)
	}
	b.Digest = ""
	digest, err := bindingDigest(b)
	if err != nil {
		return Binding{}, err
	}
	b.Digest = digest
	c.bindings = append(c.bindings, b)
	c.journal = append(c.journal, JournalEntry{
		Op: "bind", Ref: b.ProgramID,
		Detail: fmt.Sprintf("population=%s completeness=%s late_entry=%s",
			b.PopulationRef, b.Completeness, b.LateEntry),
	})
	return b, nil
}
