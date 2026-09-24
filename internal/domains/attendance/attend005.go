// ATTEND-005: recalculate downstream time and payroll after resolution.
//
// Once an ATTEND-004 resolution reevaluates an interval, every downstream
// consumer — hours, overtime, balances and pay — must recalculate from the
// fresh findings. ConsumerInputs declares the dependency graph from finding
// kinds to consumers. RecalculateDownstream diffs prior and fresh minute
// totals per consumer into exactly one delta each; a RecalcLedger posts each
// receipt once; VerifyReconciliation proves every declared consumer was
// covered. The package is kernel-pure: no persistence, clocks or network.
package attendance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

var (
	// ErrRecalculationRejected reports a recalculation that cannot be
	// trusted: an unscoped request, a prior the fresh evidence does not
	// supersede, or a reconciliation that leaves a consumer uncovered.
	ErrRecalculationRejected = errors.New("ATTEND_005_REJECTED")
)

// DownstreamConsumer names one recalculation consumer.
type DownstreamConsumer string

// Downstream consumers.
const (
	ConsumerHours    DownstreamConsumer = "hours"
	ConsumerOvertime DownstreamConsumer = "overtime"
	ConsumerBalances DownstreamConsumer = "balances"
	ConsumerPay      DownstreamConsumer = "pay"
)

// ConsumerInputs declares the dependency graph: which finding kinds feed
// each downstream consumer. Pay tracks measured attendance minutes, while
// statutory meal/rest amounts are emitted as separate premium lines by
// RecalculateDownstreamWithPremiums. Overtime tracks overtime facts; balances
// track meal and rest facts; hours track schedule adherence.
func ConsumerInputs() map[DownstreamConsumer][]ExceptionKind {
	return map[DownstreamConsumer][]ExceptionKind{
		ConsumerHours:    {LateException, EarlyException, MissingException, UnscheduledException},
		ConsumerOvertime: {OvertimeException},
		ConsumerBalances: {MealException, BreakException},
		ConsumerPay:      {LateException, EarlyException, MissingException, UnscheduledException, OvertimeException},
	}
}

// ConsumerDelta is one consumer's recalculation: prior and fresh minute
// totals with their exact difference.
type ConsumerDelta struct {
	Consumer     DownstreamConsumer
	PriorMinutes int
	NewMinutes   int
	DeltaMinutes int
}

// Recalculation is the bounded answer for one resolved interval: one delta
// per declared consumer plus a stable receipt over the prior digest, the
// fresh digest and the deltas.
type Recalculation struct {
	WorkID       string
	PriorDigest  string
	NewDigest    string
	Deltas       []ConsumerDelta
	PremiumLines []PremiumLine
	Receipt      string
}

// RecalculateDownstreamWithPremiums adds explicitly governed meal/rest
// premiums to the pay recalculation while retaining the minute delta.
func RecalculateDownstreamWithPremiums(workID, jurisdiction string, prior, fresh Result, rule PremiumRule, workdayIDs map[string]string) (Recalculation, Outcome, error) {
	r, err := RecalculateDownstream(workID, prior, fresh)
	if err != nil {
		return Recalculation{}, Unknown, err
	}
	lines, outcome, err := CalculateBreakPremiums(fresh, jurisdiction, rule, workdayIDs)
	if err != nil || outcome == Unknown {
		return Recalculation{}, outcome, err
	}
	r.PremiumLines = lines
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d:%d:%d:%d", workID, prior.InputDigest, fresh.InputDigest, jurisdiction, rule.RuleRef.ID, rule.RuleRef.Version, rule.MealHours, rule.RestHours, rule.MaxMealPerWorkday, rule.MaxRestPerWorkday)
	for _, d := range r.Deltas {
		fmt.Fprintf(h, "\x00%s=%d:%d:%d", d.Consumer, d.PriorMinutes, d.NewMinutes, d.DeltaMinutes)
	}
	for _, p := range lines {
		fmt.Fprintf(h, "\x00premium=%s:%s:%d:%d", p.WorkdayID, p.Kind, p.Count, p.Hours)
	}
	r.Receipt = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return r, outcome, nil
}

func minutesByKind(res Result) map[ExceptionKind]int {
	totals := make(map[ExceptionKind]int)
	for _, e := range res.Exceptions {
		totals[e.Kind] += e.Minutes
	}
	return totals
}

// RecalculateDownstream diffs the prior evaluation against the fresh
// reevaluation for every declared consumer. The fresh evidence must carry a
// digest different from the prior: an unchanged input is not a
// recalculation.
func RecalculateDownstream(workID string, prior, fresh Result) (Recalculation, error) {
	if workID == "" {
		return Recalculation{}, fmt.Errorf("%w: work scope is required", ErrRecalculationRejected)
	}
	if prior.InputDigest == "" || fresh.InputDigest == "" {
		return Recalculation{}, fmt.Errorf("%w: prior and fresh evaluations require evidence digests", ErrRecalculationRejected)
	}
	if prior.InputDigest == fresh.InputDigest {
		return Recalculation{}, fmt.Errorf("%w: fresh evidence does not supersede the prior", ErrRecalculationRejected)
	}
	if err := fresh.Validate(); err != nil {
		return Recalculation{}, fmt.Errorf("%w: fresh evaluation: %v", ErrRecalculationRejected, err)
	}
	graph := ConsumerInputs()
	consumers := []DownstreamConsumer{ConsumerHours, ConsumerOvertime, ConsumerBalances, ConsumerPay}
	priorTotals := minutesByKind(prior)
	freshTotals := minutesByKind(fresh)
	recalc := Recalculation{WorkID: workID, PriorDigest: prior.InputDigest, NewDigest: fresh.InputDigest}
	for _, consumer := range consumers {
		priorMinutes, freshMinutes := 0, 0
		for _, kind := range graph[consumer] {
			priorMinutes += priorTotals[kind]
			freshMinutes += freshTotals[kind]
		}
		recalc.Deltas = append(recalc.Deltas, ConsumerDelta{
			Consumer:     consumer,
			PriorMinutes: priorMinutes,
			NewMinutes:   freshMinutes,
			DeltaMinutes: freshMinutes - priorMinutes,
		})
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s", workID, prior.InputDigest, fresh.InputDigest)
	for _, d := range recalc.Deltas {
		fmt.Fprintf(h, "\x00%s=%d:%d:%d", d.Consumer, d.PriorMinutes, d.NewMinutes, d.DeltaMinutes)
	}
	sum := h.Sum(nil)
	recalc.Receipt = "sha256:" + hex.EncodeToString(sum)
	return recalc, nil
}

// RecalcLedger posts recalculation receipts exactly once. A repeated post of
// the same receipt returns the stored recalculation with already set.
type RecalcLedger struct {
	posted map[string]Recalculation
}

// NewRecalcLedger creates an empty posting ledger.
func NewRecalcLedger() *RecalcLedger {
	return &RecalcLedger{posted: make(map[string]Recalculation)}
}

// Post stores the recalculation, or returns the stored copy when the receipt
// was already posted.
func (l *RecalcLedger) Post(r Recalculation) (Recalculation, bool) {
	if existing, ok := l.posted[r.Receipt]; ok {
		return existing, true
	}
	l.posted[r.Receipt] = r
	return r, false
}

// Count reports the number of posted receipts.
func (l *RecalcLedger) Count() int { return len(l.posted) }

// VerifyReconciliation proves every declared consumer carries exactly one
// delta on the recalculation. A missing or duplicated consumer fails closed.
func VerifyReconciliation(r Recalculation) error {
	if r.Receipt == "" {
		return fmt.Errorf("%w: recalculation has no receipt", ErrRecalculationRejected)
	}
	graph := ConsumerInputs()
	seen := make(map[DownstreamConsumer]int)
	for _, d := range r.Deltas {
		seen[d.Consumer]++
	}
	consumers := make([]DownstreamConsumer, 0, len(graph))
	for consumer := range graph {
		consumers = append(consumers, consumer)
	}
	sort.Slice(consumers, func(i, j int) bool { return consumers[i] < consumers[j] })
	for _, consumer := range consumers {
		if seen[consumer] != 1 {
			return fmt.Errorf("%w: consumer %q has %d deltas", ErrRecalculationRejected, consumer, seen[consumer])
		}
	}
	if len(r.Deltas) != len(graph) {
		return fmt.Errorf("%w: %d deltas cover %d consumers", ErrRecalculationRejected, len(r.Deltas), len(graph))
	}
	return nil
}
