package pseudonym

import (
	"errors"
	"fmt"
	"strings"
)

// ScreenVerdict is the closed ANON-008 outcome vocabulary.
type ScreenVerdict string

const (
	VerdictAllow          ScreenVerdict = "ALLOW"
	VerdictSuppress       ScreenVerdict = "SUPPRESS"
	VerdictGeneralize     ScreenVerdict = "GENERALIZE"
	VerdictReviewRequired ScreenVerdict = "REVIEW_REQUIRED"
)

func (v ScreenVerdict) Valid() bool {
	switch v {
	case VerdictAllow, VerdictSuppress, VerdictGeneralize, VerdictReviewRequired:
		return true
	default:
		return false
	}
}

var (
	// ErrScreenRejected identifies output that cannot release as requested.
	ErrScreenRejected = errors.New("pseudonym: output screen rejected")
	// ErrScreenPolicy identifies an invalid screening policy.
	ErrScreenPolicy = errors.New("pseudonym: invalid screening policy")
)

// ReidentificationPolicy is the versioned ANON-008 guardrail: minimum cell
// sizes, export ceilings, the review queue and the quasi-identifier
// vocabulary that triggers generalization.
type ReidentificationPolicy struct {
	MinCellSize      int
	MaxExportRows    int
	ReviewQueue      string
	QuasiIdentifiers []string
}

func (p ReidentificationPolicy) Validate() error {
	if p.MinCellSize < 2 || p.MaxExportRows <= 0 || strings.TrimSpace(p.ReviewQueue) == "" {
		return fmt.Errorf("%w: cell floor, export ceiling and review queue are required", ErrScreenPolicy)
	}
	return nil
}

// OutputRecord is the screened analytics/search output: counts and column
// names only, never row content.
type OutputRecord struct {
	QueryRef   string
	FilterSig  string
	RowCount   int
	CellSizes  []int
	Columns    []string
	Embedding  bool
	JoinedWith string
	Exported   bool
}

// ScreenResult is the ANON-008 verdict with reasons, the review queue when
// human judgment is required, and the columns to drop for generalization.
type ScreenResult struct {
	Verdict        ScreenVerdict
	Reasons        []string
	Queue          string
	DroppedColumns []string
}

func filterOverlap(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	// Differencing probe: one filter signature extends the other, so the two
	// outputs together can isolate a single subject.
	return strings.HasPrefix(a, b+"&") || strings.HasPrefix(b, a+"&") ||
		strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func quasiPresent(policy ReidentificationPolicy, columns []string) []string {
	lowered := map[string]bool{}
	for _, c := range columns {
		lowered[strings.ToLower(c)] = true
	}
	var found []string
	for _, q := range policy.QuasiIdentifiers {
		if lowered[strings.ToLower(q)] {
			found = append(found, q)
		}
	}
	return found
}

// ScreenOutput tests one analytics/search output against re-identification.
// Small cohorts suppress; differencing probes, cross-dataset joins and large
// embedding exports require review; quasi-identifier exports generalize.
// Unsafe results never release: there is no allow-with-warning path.
func ScreenOutput(policy ReidentificationPolicy, record OutputRecord, history []OutputRecord) (ScreenResult, error) {
	if err := policy.Validate(); err != nil {
		return ScreenResult{}, err
	}
	if strings.TrimSpace(record.QueryRef) == "" {
		return ScreenResult{}, fmt.Errorf("%w: query reference is required", ErrScreenRejected)
	}
	if record.RowCount < 0 {
		return ScreenResult{}, fmt.Errorf("%w: row count is invalid", ErrScreenRejected)
	}
	small := record.RowCount < policy.MinCellSize
	for _, cell := range record.CellSizes {
		if cell < policy.MinCellSize {
			small = true
			break
		}
	}
	if small {
		return ScreenResult{
			Verdict: VerdictSuppress,
			Reasons: []string{fmt.Sprintf("cohort below minimum cell size %d", policy.MinCellSize)},
		}, nil
	}
	for _, prior := range history {
		if filterOverlap(prior.FilterSig, record.FilterSig) &&
			prior.RowCount >= policy.MinCellSize && record.RowCount >= policy.MinCellSize {
			return ScreenResult{
				Verdict: VerdictReviewRequired,
				Queue:   policy.ReviewQueue,
				Reasons: []string{"repeated narrowing filters may difference a single subject"},
			}, nil
		}
	}
	if record.JoinedWith != "" {
		return ScreenResult{
			Verdict: VerdictReviewRequired,
			Queue:   policy.ReviewQueue,
			Reasons: []string{"cross-dataset join requires linkage review"},
		}, nil
	}
	if quasi := quasiPresent(policy, record.Columns); len(quasi) > 0 && record.Exported {
		return ScreenResult{
			Verdict:        VerdictGeneralize,
			Reasons:        []string{"export carries quasi-identifiers; release requires generalization"},
			DroppedColumns: quasi,
		}, nil
	}
	if record.Embedding && record.Exported && record.RowCount > policy.MaxExportRows {
		return ScreenResult{
			Verdict: VerdictReviewRequired,
			Queue:   policy.ReviewQueue,
			Reasons: []string{"bulk embedding export requires review"},
		}, nil
	}
	return ScreenResult{Verdict: VerdictAllow, Reasons: []string{"output satisfies the re-identification policy"}}, nil
}
