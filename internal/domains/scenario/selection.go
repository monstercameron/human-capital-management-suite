// Workforce-action selection, adverse-impact and pay-equity analysis
// (WF-CAP-009).
//
// A reduction in force or merit cycle ranks a decisional unit by declared
// criteria, then proves the ranking is lawful and fair: adverse-impact
// analysis reports four-fifths and age-band results with exact rational
// arithmetic, and pay-equity analysis reports gaps by cohort. Every output
// is privileged, explainable evidence: scores carry per-criterion
// breakdowns, ratios carry their counts, and nothing is rounded into a
// passing grade.
package scenario

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrSelectionInvalid refuses a malformed unit, criteria or request.
	ErrSelectionInvalid = errors.New("scenario: invalid selection analysis request")
	// ErrSelectionScores refuses a unit whose members lack a declared
	// criterion score.
	ErrSelectionScores = errors.New("scenario: member scores do not cover the declared criteria")
)

// AgeBandDimension is the reserved adverse-impact dimension derived from
// member ages: "under-40" and "40-plus".
const AgeBandDimension = "age_band"

// SelectionMember is one decisional-unit member: protected groups per
// declared dimension, an exact criterion score vector, exact pay, and the
// pay-equity cohort.
type SelectionMember struct {
	ID     string                    `json:"id"`
	Age    int                       `json:"age"`
	Groups map[string]string         `json:"groups"`
	Scores map[string]values.Decimal `json:"scores"`
	Pay    values.Decimal            `json:"pay"`
	Cohort string                    `json:"cohort"`
}

// SelectionCriterion is one declared ranking criterion with its weight.
// Weights are relative and never silently renormalized: the weighted sum
// is exactly what the declaration says.
type SelectionCriterion struct {
	Name   string         `json:"name"`
	Weight values.Decimal `json:"weight"`
}

// ScoreTerm explains one criterion's contribution to a ranked score.
type ScoreTerm struct {
	Criterion string         `json:"criterion"`
	Score     values.Decimal `json:"score"`
	Weight    values.Decimal `json:"weight"`
	Term      values.Decimal `json:"term"`
}

// RankedMember is a decisional-unit member with its exact weighted score,
// deterministic rank (ties break by member ID) and selection flag.
type RankedMember struct {
	Member    SelectionMember `json:"member"`
	Score     values.Decimal  `json:"score"`
	Breakdown []ScoreTerm     `json:"breakdown"`
	Rank      int             `json:"rank"`
	Selected  bool            `json:"selected"`
}

// ImpactRatio is one group's exact adverse-impact result: the selection
// counts, the exact selection rate, the exact ratio to the dimension's
// maximum rate, and the four-fifths flag.
type ImpactRatio struct {
	Dimension  string   `json:"dimension"`
	Group      string   `json:"group"`
	Selected   int      `json:"selected"`
	Total      int      `json:"total"`
	Rate       *big.Rat `json:"rate"`
	RatioToMax *big.Rat `json:"ratio_to_max"`
	Flagged    bool     `json:"flagged"`
}

// AdverseImpactReport carries one ratio row per group per dimension.
type AdverseImpactReport struct {
	Ratios []ImpactRatio `json:"ratios"`
}

// PayGap is one cohort's exact pay gap: the lower and upper group means
// and their exact ratio (always at most 1).
type PayGap struct {
	Cohort     string   `json:"cohort"`
	Dimension  string   `json:"dimension"`
	LowerGroup string   `json:"lower_group"`
	UpperGroup string   `json:"upper_group"`
	LowerMean  *big.Rat `json:"lower_mean"`
	UpperMean  *big.Rat `json:"upper_mean"`
	GapRat     *big.Rat `json:"gap_ratio"`
}

// SelectionAnalysis is the composed privileged evidence: ranking,
// adverse impact and pay equity over one decisional unit.
type SelectionAnalysis struct {
	Privileged bool                `json:"privileged"`
	Handling   string              `json:"handling"`
	Ranking    []RankedMember      `json:"ranking"`
	Impact     AdverseImpactReport `json:"impact"`
	Equity     []PayGap            `json:"equity"`
}

// RankDecisionalUnit scores every member by the declared criteria and
// selects the top selectCount. Scores are exact weighted sums; ranking is
// deterministic (score descending, member ID ascending).
func RankDecisionalUnit(unit []SelectionMember, criteria []SelectionCriterion, selectCount int) ([]RankedMember, error) {
	if len(unit) == 0 || len(criteria) == 0 {
		return nil, fmt.Errorf("%w: unit and criteria must be non-empty", ErrSelectionInvalid)
	}
	if selectCount < 0 || selectCount > len(unit) {
		return nil, fmt.Errorf("%w: select count %d for %d members", ErrSelectionInvalid, selectCount, len(unit))
	}
	scale, err := criteriaScale(criteria)
	if err != nil {
		return nil, err
	}
	ranked := make([]RankedMember, 0, len(unit))
	for _, member := range unit {
		if strings.TrimSpace(member.ID) == "" {
			return nil, fmt.Errorf("%w: member without an ID", ErrSelectionInvalid)
		}
		score, breakdown, err := weightedScore(member, criteria, scale)
		if err != nil {
			return nil, err
		}
		ranked = append(ranked, RankedMember{Member: member, Score: score, Breakdown: breakdown})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if c := ranked[i].Score.Cmp(ranked[j].Score); c != 0 {
			return c > 0
		}
		return ranked[i].Member.ID < ranked[j].Member.ID
	})
	for i := range ranked {
		ranked[i].Rank = i + 1
		ranked[i].Selected = i < selectCount
	}
	return ranked, nil
}

// AnalyzeAdverseImpact reports exact four-fifths results per group for
// each declared dimension over a ranked unit. The reserved age_band
// dimension derives "under-40"/"40-plus" from member ages. A group whose
// exact rate ratio to the dimension maximum is below 4/5 is flagged.
func AnalyzeAdverseImpact(ranked []RankedMember, dimensions []string) (AdverseImpactReport, error) {
	if len(ranked) == 0 || len(dimensions) == 0 {
		return AdverseImpactReport{}, fmt.Errorf("%w: ranking and dimensions must be non-empty", ErrSelectionInvalid)
	}
	var ratios []ImpactRatio
	for _, dimension := range dimensions {
		selected, total, err := dimensionCounts(ranked, dimension)
		if err != nil {
			return AdverseImpactReport{}, err
		}
		maxRate := new(big.Rat)
		for group := range total {
			rate := new(big.Rat).SetFrac64(int64(selected[group]), int64(total[group]))
			if rate.Cmp(maxRate) > 0 {
				maxRate = rate
			}
		}
		groups := make([]string, 0, len(total))
		for group := range total {
			groups = append(groups, group)
		}
		sort.Strings(groups)
		for _, group := range groups {
			rate := new(big.Rat).SetFrac64(int64(selected[group]), int64(total[group]))
			ratio := big.NewRat(1, 1)
			if maxRate.Sign() != 0 {
				ratio = new(big.Rat).Quo(rate, maxRate)
			}
			ratios = append(ratios, ImpactRatio{
				Dimension: dimension, Group: group,
				Selected: selected[group], Total: total[group],
				Rate: rate, RatioToMax: ratio,
				Flagged: ratio.Cmp(big.NewRat(4, 5)) < 0,
			})
		}
	}
	return AdverseImpactReport{Ratios: ratios}, nil
}

// AnalyzePayEquity reports, per cohort, the exact gap between the lowest
// and highest group mean pay along one dimension. Means are exact
// rationals over exact decimal pay; the gap is min/max, always at most 1.
func AnalyzePayEquity(ranked []RankedMember, dimension string) ([]PayGap, error) {
	if len(ranked) == 0 || strings.TrimSpace(dimension) == "" {
		return nil, fmt.Errorf("%w: ranking and dimension must be non-empty", ErrSelectionInvalid)
	}
	byCohort := map[string][]RankedMember{}
	for _, member := range ranked {
		cohort := member.Member.Cohort
		if strings.TrimSpace(cohort) == "" {
			return nil, fmt.Errorf("%w: member %s has no cohort", ErrSelectionInvalid, member.Member.ID)
		}
		byCohort[cohort] = append(byCohort[cohort], member)
	}
	cohorts := make([]string, 0, len(byCohort))
	for cohort := range byCohort {
		cohorts = append(cohorts, cohort)
	}
	sort.Strings(cohorts)
	gaps := make([]PayGap, 0, len(cohorts))
	for _, cohort := range cohorts {
		sums := map[string]*big.Rat{}
		counts := map[string]int64{}
		for _, member := range byCohort[cohort] {
			group, err := memberGroup(member.Member, dimension)
			if err != nil {
				return nil, err
			}
			pay, err := decimalRat(member.Member.Pay)
			if err != nil {
				return nil, fmt.Errorf("%w: pay: %v", ErrSelectionInvalid, err)
			}
			if sums[group] == nil {
				sums[group] = new(big.Rat)
			}
			sums[group].Add(sums[group], pay)
			counts[group]++
		}
		means := map[string]*big.Rat{}
		for group, sum := range sums {
			means[group] = new(big.Rat).Quo(sum, new(big.Rat).SetInt64(counts[group]))
		}
		lower, upper := "", ""
		for group, mean := range means {
			if lower == "" || mean.Cmp(means[lower]) < 0 {
				lower = group
			}
			if upper == "" || mean.Cmp(means[upper]) > 0 {
				upper = group
			}
		}
		gap := big.NewRat(1, 1)
		if means[upper].Sign() != 0 {
			gap = new(big.Rat).Quo(means[lower], means[upper])
		}
		gaps = append(gaps, PayGap{
			Cohort: cohort, Dimension: dimension,
			LowerGroup: lower, UpperGroup: upper,
			LowerMean: means[lower], UpperMean: means[upper], GapRat: gap,
		})
	}
	return gaps, nil
}

// ComposeSelectionAnalysis ranks, analyzes impact and equity, and marks
// the result privileged evidence with its handling caveat.
func ComposeSelectionAnalysis(unit []SelectionMember, criteria []SelectionCriterion, selectCount int, dimensions []string, equityDimension string) (SelectionAnalysis, error) {
	ranked, err := RankDecisionalUnit(unit, criteria, selectCount)
	if err != nil {
		return SelectionAnalysis{}, err
	}
	impact, err := AnalyzeAdverseImpact(ranked, dimensions)
	if err != nil {
		return SelectionAnalysis{}, err
	}
	equity, err := AnalyzePayEquity(ranked, equityDimension)
	if err != nil {
		return SelectionAnalysis{}, err
	}
	return SelectionAnalysis{
		Privileged: true,
		Handling:   "Privileged workforce-action evidence: restrict to counsel and the decisional process. Ratios are exact; a flagged group needs review, not an automatic stop.",
		Ranking:    ranked,
		Impact:     impact,
		Equity:     equity,
	}, nil
}

// criteriaScale validates the declaration and returns the working scale:
// every weight and every score must share one scale so no term rounds.
func criteriaScale(criteria []SelectionCriterion) (int32, error) {
	scale := criteria[0].Weight.Scale()
	for _, criterion := range criteria {
		if strings.TrimSpace(criterion.Name) == "" {
			return 0, fmt.Errorf("%w: criterion without a name", ErrSelectionInvalid)
		}
		if err := criterion.Weight.Validate(); err != nil || criterion.Weight.Sign() <= 0 {
			return 0, fmt.Errorf("%w: weight for %q must be a positive decimal", ErrSelectionInvalid, criterion.Name)
		}
		if criterion.Weight.Scale() != scale {
			return 0, fmt.Errorf("%w: weights must share one scale", ErrSelectionInvalid)
		}
	}
	return scale, nil
}

// weightedScore computes one member's exact weighted sum with a
// per-criterion breakdown.
func weightedScore(member SelectionMember, criteria []SelectionCriterion, scale int32) (values.Decimal, []ScoreTerm, error) {
	total, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, nil, err
	}
	breakdown := make([]ScoreTerm, 0, len(criteria))
	for _, criterion := range criteria {
		score, ok := member.Scores[criterion.Name]
		if !ok {
			return values.Decimal{}, nil, fmt.Errorf("%w: member %s lacks %q", ErrSelectionScores, member.ID, criterion.Name)
		}
		if err := score.Validate(); err != nil || score.Scale() != scale {
			return values.Decimal{}, nil, fmt.Errorf("%w: member %s score %q must share the criteria scale", ErrSelectionInvalid, member.ID, criterion.Name)
		}
		term, err := score.Mul(criterion.Weight, scale, values.RoundingExactRequired)
		if err != nil {
			return values.Decimal{}, nil, fmt.Errorf("%w: member %s term %q: %v", ErrSelectionInvalid, member.ID, criterion.Name, err)
		}
		total, err = total.Add(term)
		if err != nil {
			return values.Decimal{}, nil, err
		}
		breakdown = append(breakdown, ScoreTerm{Criterion: criterion.Name, Score: score, Weight: criterion.Weight, Term: term})
	}
	return total, breakdown, nil
}

// dimensionCounts tallies selected and total members per group along one
// dimension, deriving the reserved age band from ages.
func dimensionCounts(ranked []RankedMember, dimension string) (map[string]int, map[string]int, error) {
	selected, total := map[string]int{}, map[string]int{}
	for _, member := range ranked {
		group, err := memberGroup(member.Member, dimension)
		if err != nil {
			return nil, nil, err
		}
		total[group]++
		if member.Selected {
			selected[group]++
		}
	}
	return selected, total, nil
}

// memberGroup resolves one member's group along a dimension.
func memberGroup(member SelectionMember, dimension string) (string, error) {
	if dimension == AgeBandDimension {
		if member.Age < 0 {
			return "", fmt.Errorf("%w: member %s has a negative age", ErrSelectionInvalid, member.ID)
		}
		if member.Age >= 40 {
			return "40-plus", nil
		}
		return "under-40", nil
	}
	group, ok := member.Groups[dimension]
	if !ok || strings.TrimSpace(group) == "" {
		return "", fmt.Errorf("%w: member %s lacks dimension %q", ErrSelectionInvalid, member.ID, dimension)
	}
	return group, nil
}

// decimalRat converts an exact decimal to its exact rational value.
func decimalRat(d values.Decimal) (*big.Rat, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d.Scale())), nil)
	rat := new(big.Rat).SetFrac(d.Unscaled(), denominator)
	if d.Sign() < 0 {
		rat.Neg(rat)
	}
	return rat, nil
}
