// Package productui explains policy outcomes and
// obligation states. Explanations describe what an
// already-projected value means; they never evaluate
// policy, re-decide validity, or grant authority. Each
// tokenized obligation state carries reviewed copy, and
// untokenized states — unspecified, out-of-range — fail
// closed with no explanation. The policy outcome
// explanation pairs with the simulation panel's outcome
// under its no-authority notice.
package productui

import (
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

// ObligationExplanation is the safe, reviewable projection used beside an
// obligation status. Unknown enum values intentionally remain unexplained;
// technical identifiers and provider details belong behind an authorized
// disclosure owned by the workflow service.
type ObligationExplanation struct {
	Text      string
	Explained bool
}

// ResolveObligationExplanation resolves the compact explanation model for a
// status chip without exposing raw enum names or treating copy as authority.
func ResolveObligationExplanation(locale LocaleContext, state intentsv1.ObligationState) ObligationExplanation {
	text, ok := ExplainObligation(locale, state)
	return ObligationExplanation{Text: text, Explained: ok}
}

// ExplainObligation explains one obligation state through
// reviewed copy. It reports false for states the
// lifecycle surface cannot tokenize.
func ExplainObligation(locale LocaleContext, state intentsv1.ObligationState) (string, bool) {
	token, valid := obligationToken(state)
	if !valid {
		return "", false
	}
	return locale.Text("status.obligation.explain." + token.key), true
}

// ExplainPolicyOutcome explains one simulated policy
// outcome through reviewed copy.
func ExplainPolicyOutcome(locale LocaleContext, allowed bool) string {
	if allowed {
		return locale.Text("view_as.explain.allowed")
	}
	return locale.Text("view_as.explain.denied")
}
