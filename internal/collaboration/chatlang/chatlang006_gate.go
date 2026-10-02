package chatlang

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CHATLANG-006: the quality gate. Before a language pair is offered, and again
// whenever the engine or the glossary changes, a labelled set of
// workplace-critical messages is translated through the engine in the same way
// the product translates (protected, then checked and restored) and what must
// survive is checked: names, numbers, dates and quantities must come back
// exactly, a negated sentence must not come back as its positive, and the engine
// must vouch for the meaning. A pair that does not reach the pass mark is
// offered with the "may be inaccurate" mark or not at all.
//
// The set below is the part of the labelled set that is known without a
// bilingual reviewer: cases whose correct outcome is checkable by machine.
// Reviewer-scored quality on 300 messages per pair is the research of
// CHATLANG-001 and needs a live run.

// GateCategory is the kind of risk a case guards.
type GateCategory string

const (
	GateNegation GateCategory = "negation"
	GateDeadline GateCategory = "deadline"
	GateQuantity GateCategory = "quantity"
	GateSafety   GateCategory = "safety"
	GateLegal    GateCategory = "legal"
	GateName     GateCategory = "name"
	GateAcronym  GateCategory = "slang"
)

// GateCase is one labelled message. Keep lists text that must be in the
// translation exactly as written. Opposite names the case that says the reverse
// (a negation and its positive): the two must not translate to the same text.
type GateCase struct {
	ID       string
	Category GateCategory
	Source   string
	Text     string
	Keep     []string
	Opposite string
}

// GateCases returns the labelled set, in a fixed order.
func GateCases() []GateCase {
	return []GateCase{
		{ID: "neg-approve", Category: GateNegation, Source: "en", Text: "Do not approve the payment until the review is finished.", Opposite: "pos-approve"},
		{ID: "pos-approve", Category: GateNegation, Source: "en", Text: "Approve the payment as soon as the review is finished.", Opposite: "neg-approve"},
		{ID: "neg-enter", Category: GateNegation, Source: "en", Text: "Nobody may enter the server room without a badge.", Opposite: "pos-enter"},
		{ID: "pos-enter", Category: GateNegation, Source: "en", Text: "Everybody may enter the server room without a badge.", Opposite: "neg-enter"},
		{ID: "deadline", Category: GateDeadline, Source: "en", Text: "The timesheets are due by 17:00 on 2026-10-30 at the latest.", Keep: []string{"17:00", "2026-10-30"}},
		{ID: "quantity", Category: GateQuantity, Source: "en", Text: "Order 250 units of part 4471 and no more than 3 pallets.", Keep: []string{"250", "4471", "3"}},
		{ID: "money", Category: GateQuantity, Source: "en", Text: "The limit is $12,500 per quarter, not $15,000.", Keep: []string{"$12,500", "$15,000"}},
		{ID: "safety", Category: GateSafety, Source: "en", Text: "Evacuate through the north exit if the alarm sounds; the lift is out of service.", Keep: []string{}},
		{ID: "legal", Category: GateLegal, Source: "en", Text: "This notice is a final written warning under section 4.2 of the handbook.", Keep: []string{"4.2"}},
		{ID: "name", Category: GateName, Source: "en", Text: "Please send the signed form to Walt Brennan in Dallas.", Keep: []string{"Walt Brennan", "Dallas"}},
		{ID: "slang", Category: GateAcronym, Source: "en", Text: "FYI the PTO request is approved, no worries.", Keep: []string{"PTO"}},
		{ID: "de-deadline", Category: GateDeadline, Source: "de", Text: "Bitte senden Sie die Unterlagen bis spätestens 2026-11-15 um 09:30 ein.", Keep: []string{"2026-11-15", "09:30"}},
		{ID: "de-neg", Category: GateNegation, Source: "de", Text: "Die Zahlung darf nicht freigegeben werden, bevor die Prüfung abgeschlossen ist.", Opposite: "de-pos"},
		{ID: "de-pos", Category: GateNegation, Source: "de", Text: "Die Zahlung darf freigegeben werden, sobald die Prüfung abgeschlossen ist.", Opposite: "de-neg"},
	}
}

// GatePair is one direction the product offers.
type GatePair struct{ Source, Target string }

// GateDecision is what a pair's result allows.
type GateDecision string

const (
	// GateOffer: the pair is offered as it is.
	GateOffer GateDecision = "offer"
	// GateOfferMarked: the pair is offered with the "may be inaccurate" mark.
	GateOfferMarked GateDecision = "offer_marked"
	// GateWithhold: the pair is not offered.
	GateWithhold GateDecision = "withhold"
)

// Pass marks: the share of a pair's cases that must pass to be offered as it is,
// and to be offered at all.
const (
	GateOfferMark       = 0.9
	GateOfferMarkedMark = 0.7
)

// GateResult is the outcome for one pair.
type GateResult struct {
	Pair     GatePair
	Cases    int
	Passed   int
	Failures []string
	Decision GateDecision
}

// Share is the share of cases that passed.
func (r GateResult) Share() float64 {
	if r.Cases == 0 {
		return 0
	}
	return float64(r.Passed) / float64(r.Cases)
}

// GateDecisionFor is the decision a pass share earns.
func GateDecisionFor(cases, passed int) GateDecision {
	if cases == 0 {
		return GateWithhold
	}
	share := float64(passed) / float64(cases)
	switch {
	case share >= GateOfferMark:
		return GateOffer
	case share >= GateOfferMarkedMark:
		return GateOfferMarked
	}
	return GateWithhold
}

// RunGate translates the cases of each pair (those whose source is the pair's
// source) through the engine, as the producer does, and checks them. The
// engine's failure on a case is a failed case, not an error: a pair whose engine
// cannot answer is withheld. A cancelled context ends the run with its error.
func RunGate(ctx context.Context, engine Engine, glossary Glossary, pairs []GatePair) ([]GateResult, error) {
	if engine == nil {
		return nil, ErrUnavailable
	}
	all := GateCases()
	byID := map[string]GateCase{}
	for _, c := range all {
		byID[c.ID] = c
	}
	results := make([]GateResult, 0, len(pairs))
	for _, pair := range pairs {
		result := GateResult{Pair: pair}
		outputs := map[string]string{}
		for _, c := range all {
			if c.Source != pair.Source {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result.Cases++
			output, failure := gateTranslate(ctx, engine, glossary, pair, c)
			outputs[c.ID] = output
			if failure != "" {
				result.Failures = append(result.Failures, c.ID+": "+failure)
				continue
			}
			result.Passed++
		}
		// A negation must not come back as its positive.
		for _, c := range all {
			if c.Source != pair.Source || c.Opposite == "" {
				continue
			}
			other, ok := byID[c.Opposite]
			if !ok || other.Source != pair.Source || c.ID > other.ID {
				continue
			}
			if outputs[c.ID] != "" && outputs[c.ID] == outputs[other.ID] {
				result.Failures = append(result.Failures, c.ID+": translates to the same text as its opposite "+other.ID)
				// The pair counts against the case that was passing.
				if result.Passed > 0 {
					result.Passed--
				}
			}
		}
		sort.Strings(result.Failures)
		result.Decision = GateDecisionFor(result.Cases, result.Passed)
		results = append(results, result)
	}
	return results, nil
}

func gateTranslate(ctx context.Context, engine Engine, glossary Glossary, pair GatePair, c GateCase) (string, string) {
	protected := Protect(c.Text, glossary, pair.Target)
	response, err := engine.Translate(ctx, Request{Tenant: "quality-gate", Message: c.ID, Revision: 1, Source: pair.Source, Target: pair.Target, Text: protected.Text()})
	if err != nil {
		return "", "the engine did not answer"
	}
	if response.MeaningChecked && !response.MeaningPreserved {
		return "", "the engine does not vouch for the meaning"
	}
	if failures := protected.Verify(response.Text); len(failures) != 0 {
		return "", "a protected item did not come back: " + strings.Join(failures, "; ")
	}
	restored, failures, err := protected.Restore(response.Text)
	if err != nil || len(failures) != 0 {
		return "", "the protected items could not be restored"
	}
	for _, keep := range c.Keep {
		if !strings.Contains(restored, keep) {
			return restored, fmt.Sprintf("%q is missing from the translation", keep)
		}
	}
	if strings.TrimSpace(restored) == "" {
		return "", "the translation is empty"
	}
	return restored, ""
}
