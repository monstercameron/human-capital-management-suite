// FTIME-005: allocate approved timecard minutes exactly across authorized
// project/work-order lines, with source references that stop one minute
// from ever being charged twice, and corrections that produce deltas
// rather than rewriting a prior allocation.
package timecard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrTimeAllocationRejected is the FTIME-005 seeded-defect sentinel.
var ErrTimeAllocationRejected = errors.New("FTIME_005_REJECTED")

// AllocationSource identifies one immutable piece of approved-time
// evidence -- a paired punch, a duration line or an earlier allocation --
// that backs a share of allocated minutes. A source may appear in at most
// one line across a whole allocation request: that uniqueness check is
// what stops one minute being charged twice.
type AllocationSource struct {
	Kind string // "PUNCH" | "LINE" | "CORRECTION"
	ID   string
}

func (s AllocationSource) key() string { return s.Kind + "\x00" + s.ID }

func (s AllocationSource) Validate() error {
	if strings.TrimSpace(s.Kind) == "" || strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: allocation source requires kind and id", ErrTimeAllocationRejected)
	}
	return nil
}

// AllocationLine assigns exact minutes to one authorized labor dimension
// (typically a project or work order, modeled as a labor.Dimension) with
// the source evidence that backs it. It never carries a pay rate: only the
// dimension identity and the minutes travel with an allocation.
type AllocationLine struct {
	Dimension labor.Dimension
	Minutes   values.Decimal // exact whole minutes at the source's scale
	Sources   []AllocationSource
}

// TimeAllocationRequest is the complete pinned allocation question: one
// approved timecard's total minutes split exactly across authorized lines
// under one governed labor rule.
type TimeAllocationRequest struct {
	TimecardID       string
	ApprovedRevision uint64
	ApprovedMinutes  values.Decimal
	Rule             labor.LaborRule
	Lines            []AllocationLine
}

// TimeAllocation is the immutable, exactly-conserved result: shares plus
// zero residual equal the approved minutes, by construction.
type TimeAllocation struct {
	TimecardID          string
	ApprovedRevision    uint64
	RuleID, RuleVersion string
	TotalMinutes        values.Decimal
	Lines               []AllocationLine
}

// AllocateApprovedTime splits an approved timecard's minutes exactly across
// authorized dimensions. Declaring a zero residual is deliberate: approved
// time must allocate exactly, with nothing left uncosted.
func AllocateApprovedTime(req TimeAllocationRequest) (TimeAllocation, error) {
	if strings.TrimSpace(req.TimecardID) == "" {
		return TimeAllocation{}, fmt.Errorf("%w: timecard id is required", ErrTimeAllocationRejected)
	}
	if err := req.ApprovedMinutes.Validate(); err != nil {
		return TimeAllocation{}, fmt.Errorf("%w: approved minutes: %v", ErrTimeAllocationRejected, err)
	}
	if req.ApprovedMinutes.Sign() < 0 {
		return TimeAllocation{}, fmt.Errorf("%w: approved minutes cannot be negative", ErrTimeAllocationRejected)
	}
	if len(req.Lines) == 0 {
		return TimeAllocation{}, fmt.Errorf("%w: at least one allocation line is required", ErrTimeAllocationRejected)
	}
	seenSources := make(map[string]struct{})
	shares := make([]labor.TimeShare, 0, len(req.Lines))
	for i, line := range req.Lines {
		if len(line.Sources) == 0 {
			return TimeAllocation{}, fmt.Errorf("%w: line %d requires source evidence", ErrTimeAllocationRejected, i)
		}
		for _, src := range line.Sources {
			if err := src.Validate(); err != nil {
				return TimeAllocation{}, err
			}
			key := src.key()
			if _, dup := seenSources[key]; dup {
				return TimeAllocation{}, fmt.Errorf("%w: source %s is charged in more than one line", ErrTimeAllocationRejected, key)
			}
			seenSources[key] = struct{}{}
		}
		shares = append(shares, labor.TimeShare{Dimension: line.Dimension, Amount: line.Minutes})
	}
	zero, err := values.NewDecimal("0", req.ApprovedMinutes.Scale(), req.ApprovedMinutes.Rounding())
	if err != nil {
		return TimeAllocation{}, fmt.Errorf("%w: %v", ErrTimeAllocationRejected, err)
	}
	alloc, err := labor.AllocateWorkedTime(labor.WorkAllocationRequest{
		SourceID: req.TimecardID, SourceLabel: "timecard/" + req.TimecardID,
		Rule: req.Rule, RuleVersion: req.Rule.Version,
		SourceAmount: req.ApprovedMinutes, Shares: shares, DeclaredResidual: zero,
	})
	if err != nil {
		return TimeAllocation{}, fmt.Errorf("%w: %v", ErrTimeAllocationRejected, err)
	}
	return TimeAllocation{
		TimecardID: req.TimecardID, ApprovedRevision: req.ApprovedRevision,
		RuleID: alloc.RuleID, RuleVersion: alloc.RuleVersion, TotalMinutes: alloc.SourceAmount,
		Lines: append([]AllocationLine(nil), req.Lines...),
	}, nil
}

func dimKey(d labor.Dimension) string { return d.Kind.String() + "\x00" + d.Value + "\x00" + d.Version }

// digest is a deterministic, order-independent fingerprint of one
// allocation, used to prove RACE-safe determinism and idempotent replays.
func (a TimeAllocation) digest() string {
	lines := append([]AllocationLine(nil), a.Lines...)
	sort.Slice(lines, func(i, j int) bool { return dimKey(lines[i].Dimension) < dimKey(lines[j].Dimension) })
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|%s|%s|%s|", a.TimecardID, a.ApprovedRevision, a.RuleID, a.RuleVersion, a.TotalMinutes.String())
	for _, l := range lines {
		fmt.Fprintf(&b, "%s=%s;", dimKey(l.Dimension), l.Minutes.String())
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Digest exposes the deterministic allocation fingerprint.
func (a TimeAllocation) Digest() string { return a.digest() }

// AllocationDeltaLine is one signed correction to a previously allocated
// dimension: positive adds minutes, negative removes them. It is evidence
// for a downstream ledger to post, never applied by mutating the prior
// allocation.
type AllocationDeltaLine struct {
	Dimension    labor.Dimension
	DeltaMinutes values.Decimal
}

// AllocationCorrection is the exact difference between two conserved
// allocations for the same timecard at successive approved revisions.
type AllocationCorrection struct {
	TimecardID               string
	FromRevision, ToRevision uint64
	Deltas                   []AllocationDeltaLine
	Next                     TimeAllocation
}

// CorrectAllocation re-derives the allocation from a fresh request and
// returns both the new allocation and the exact per-dimension delta
// against the previous one. The delta is computed independently from the
// two conserved allocations; it is never trusted from a caller-declared
// adjustment.
func CorrectAllocation(prev TimeAllocation, next TimeAllocationRequest) (AllocationCorrection, error) {
	if prev.TimecardID != next.TimecardID {
		return AllocationCorrection{}, fmt.Errorf("%w: correction must target the same timecard", ErrTimeAllocationRejected)
	}
	if next.ApprovedRevision <= prev.ApprovedRevision {
		return AllocationCorrection{}, fmt.Errorf("%w: correction must advance the approved revision", ErrTimeAllocationRejected)
	}
	nextAlloc, err := AllocateApprovedTime(next)
	if err != nil {
		return AllocationCorrection{}, err
	}
	scale, rounding := nextAlloc.TotalMinutes.Scale(), nextAlloc.TotalMinutes.Rounding()
	prevByDim := map[string]values.Decimal{}
	for _, l := range prev.Lines {
		prevByDim[dimKey(l.Dimension)] = l.Minutes
	}
	nextByDim := map[string]values.Decimal{}
	for _, l := range nextAlloc.Lines {
		nextByDim[dimKey(l.Dimension)] = l.Minutes
	}
	dims := map[string]labor.Dimension{}
	for _, l := range prev.Lines {
		dims[dimKey(l.Dimension)] = l.Dimension
	}
	for _, l := range nextAlloc.Lines {
		dims[dimKey(l.Dimension)] = l.Dimension
	}
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	deltas := make([]AllocationDeltaLine, 0, len(keys))
	for _, k := range keys {
		before, ok := prevByDim[k]
		if !ok {
			before, err = values.NewDecimal("0", scale, rounding)
			if err != nil {
				return AllocationCorrection{}, err
			}
		}
		after, ok := nextByDim[k]
		if !ok {
			after, err = values.NewDecimal("0", scale, rounding)
			if err != nil {
				return AllocationCorrection{}, err
			}
		}
		diff, err := after.Sub(before)
		if err != nil {
			return AllocationCorrection{}, err
		}
		if diff.IsZero() {
			continue
		}
		deltas = append(deltas, AllocationDeltaLine{Dimension: dims[k], DeltaMinutes: diff})
	}
	return AllocationCorrection{TimecardID: prev.TimecardID, FromRevision: prev.ApprovedRevision, ToRevision: nextAlloc.ApprovedRevision, Deltas: deltas, Next: nextAlloc}, nil
}
