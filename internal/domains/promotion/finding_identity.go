package promotion

import "sort"

// FindingIdentity is a Finding's stable, typed identity: what makes two
// findings the same underlying observation.
//
// Code is part of that identity, deliberately. Code is not a display string:
// preflight.go's own doc comment calls it out as "the stable identity that
// rules, tests and UIs key on", it is a small closed vocabulary of typed
// constants (CodeProposedAmountInvalid, CodeNotARaise, and so on) assigned by
// the rule that raised the finding, and it is what already lets a caller
// distinguish "the amount is invalid" from "the amount is not a raise" even
// when both describe the same Field at the same Severity -- checkCompensation
// (rules.go) can raise both CodeProposedAmountInvalid and CodeNotARaise for
// one proposal, on Field "proposed.base" at SeverityBlocking, because a
// present-but-zero proposed amount is simultaneously an invalid amount and,
// compared against a positive current amount, not a raise. Two genuinely
// different business problems. An identity of (Field, Severity) alone cannot
// tell them apart and silently drops one -- that is data loss, not
// deduplication, and it is not limited to this one pair: Field "target.grade"
// + SeverityBlocking carries both CodeTargetGradeRequired and CodeSameGrade,
// and Field "proposed.base.currency" + SeverityBlocking carries both
// CodeCurrencyRequired and CodeCurrencyChangeNotV0. Nothing enforces that such
// pairs stay mutually exclusive as rules are added, so identity has to be
// precise now rather than coarse and lucky.
//
// Message is excluded. Unlike Code, Message is operator-facing prose with no
// closed vocabulary and no promise of stability -- REFACTOR's "never on
// rendered strings" is about exactly this field -- so two occurrences of the
// same Code/Field/Severity that happen to be worded differently (or whose
// wording was corrupted in transit) are still recognized as one observation.
//
// This also settles RED's "swapped internal code and message" case on its own
// terms rather than by refusing to trust Code at all. Two readings are
// possible for two occurrences that are supposed to be "one observation
// appearing twice":
//
//   - The rule that fired is the same both times (same Code), and only the
//     Message text differs or was scrambled. That is exactly a duplicate under
//     this identity -- Message is excluded -- and [DeduplicateFindings]
//     collapses it, picking the canonical Message deterministically rather
//     than trusting whichever copy arrived first.
//   - The Code itself genuinely differs between the two occurrences. Under a
//     Code-inclusive identity that means two different rules fired, which are
//     two different findings by construction, not a pairing this package can
//     safely "fix" by guessing which Message belongs with which Code -- that
//     guess would be exactly the rendered-string matching REFACTOR forbids,
//     applied one level up. If two occurrences with different Codes really
//     are meant to be the same observation, that is a bug in whatever emitted
//     inconsistent Codes for it, and the correct response is to surface both
//     findings honestly rather than collapse them on an assumption, so the
//     inconsistency stays visible instead of being silently smoothed over.
//
// Owner stays outside FindingIdentity even though GREEN names it alongside
// identity: two findings sharing (Code, Field, Severity) from different
// owners are independently sourced corroboration of one observation, not two
// observations, so they still collapse to one identity and
// [DeduplicateFindings] records every contributing owner on the single
// survivor instead of leaving the reader to notice two rows describe the same
// thing.
type FindingIdentity struct {
	Code     string
	Field    string
	Severity Severity
}

// Identity returns f's stable semantic identity.
func (f Finding) Identity() FindingIdentity {
	return FindingIdentity{Code: f.Code, Field: f.Field, Severity: f.Severity}
}

// DeduplicateFindings collapses findings sharing a [FindingIdentity] into one
// canonical finding per identity and returns the result in a deterministic
// order.
//
// Three properties hold regardless of input order or how many times a
// duplicate was appended:
//
//  1. Exact duplicates collapse. Two findings with the same identity
//     (same Code, Field and Severity) and the same Owner are one observation
//     reported more than once; the survivor carries that Owner and no
//     corroboration.
//  2. Independently sourced corroboration stays attributable. Two findings
//     with the same identity but distinct, non-empty Owners are one
//     observation confirmed by more than one source; the survivor's Owner is
//     the lexicographically first contributing owner and CorroboratedBy lists
//     every other distinct owner, sorted. Nothing is silently dropped.
//  3. The canonical Message is chosen by content, not by arrival order:
//     within an identity group (Code is already fixed, since it is part of
//     the identity) the lexicographically smallest Message wins. This is what
//     makes a same-Code duplicate whose Message was reworded or corrupted
//     resolve to the same one explanation no matter which copy happened to be
//     appended first.
//
// Findings whose Code genuinely differs are never merged, even when they
// share Field and Severity: see [FindingIdentity] for why that distinction is
// load-bearing rather than incidental.
//
// A nil input returns nil; a non-nil, empty input returns a non-nil, empty
// slice, matching the nil-preserving convention [simcontract.Assemble]'s own
// clone helper uses for every other section.
func DeduplicateFindings(findings []Finding) []Finding {
	if findings == nil {
		return nil
	}

	groups := make(map[FindingIdentity][]Finding, len(findings))
	order := make([]FindingIdentity, 0, len(findings))
	for _, f := range findings {
		id := f.Identity()
		if _, seen := groups[id]; !seen {
			order = append(order, id)
		}
		groups[id] = append(groups[id], f)
	}

	out := make([]Finding, 0, len(order))
	for _, id := range order {
		out = append(out, canonicalizeFindingGroup(id, groups[id]))
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		if out[i].Severity != out[j].Severity {
			return out[i].Severity < out[j].Severity
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// canonicalizeFindingGroup picks the one Finding that represents an identity
// group, deterministically, and folds in every distinct owner that
// corroborated it. Code, Field and Severity are already fixed by id -- every
// member of group shares them by construction -- so the only content left to
// canonicalize is Message.
func canonicalizeFindingGroup(id FindingIdentity, group []Finding) Finding {
	explanations := append([]Finding(nil), group...)
	sort.SliceStable(explanations, func(i, j int) bool {
		return explanations[i].Message < explanations[j].Message
	})
	canonical := explanations[0]
	canonical.Code = id.Code
	canonical.Field = id.Field
	canonical.Severity = id.Severity

	owners := make(map[string]struct{}, len(group))
	for _, f := range group {
		if f.Owner != "" {
			owners[f.Owner] = struct{}{}
		}
	}
	if len(owners) == 0 {
		canonical.Owner = ""
		canonical.CorroboratedBy = nil
		return canonical
	}
	distinct := make([]string, 0, len(owners))
	for o := range owners {
		distinct = append(distinct, o)
	}
	sort.Strings(distinct)

	canonical.Owner = distinct[0]
	if len(distinct) > 1 {
		canonical.CorroboratedBy = append([]string(nil), distinct[1:]...)
	} else {
		canonical.CorroboratedBy = nil
	}
	return canonical
}
