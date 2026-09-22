package simassign

// Reversal is one promotion effect kind's undo contract: its reversibility
// class, the compensating action that undoes or repairs it, how that action
// is applied, and the post-commit observation that watches it.
//
// The table below ([PromotionReversals]) is the one reversibility
// declaration WF-REV-003 sources proposals, plans and cancellation from:
// the assignment/compensation simulators project it onto
// intent.PlannedEffect (and the plan's outbox, compensation and observation
// bindings), and workflow cancellation derives its verdict from the same
// values (a SUPERSEDING_REVISION compensation reverts, any other declared
// compensation compensates). No simulator repeats these strings: the shared
// effect constructors resolve them from the effect kind, so a new effect
// kind cannot be simulated without declaring how it is undone.
type Reversal struct {
	// Kind is the effect kind this contract undoes.
	Kind EffectKind
	// Reversibility is the effect's undo class.
	Reversibility Reversibility
	// CompensationRef is the compensating action. A ref ending in
	// ".supersede" undoes the effect by writing a superseding revision in
	// the effect's own store; every other ref is a distinct counter-action
	// (a release, a credit). Workflow cancellation reads the same mark.
	CompensationRef string
	// CompensationStrategy is how that action is applied.
	CompensationStrategy string
	// ObservationRef is the post-commit observation that watches the effect.
	ObservationRef string
}

// promotionReversals is the single declaration. Its values are the exact
// contracts the simulators previously repeated per effect; they moved here
// verbatim so proposals, plans and cancellation can never disagree about
// what undoes a promotion effect.
var promotionReversals = []Reversal{
	{
		Kind:                 EffectAssignmentRevision,
		Reversibility:        Reversible,
		CompensationRef:      "people.assignment.supersede",
		CompensationStrategy: "SUPERSEDING_REVISION",
		ObservationRef:       "observe.people.assignment_effective",
	},
	{
		Kind:                 EffectManagerRelationship,
		Reversibility:        Reversible,
		CompensationRef:      "org.manager_relationship.supersede",
		CompensationStrategy: "SUPERSEDING_REVISION",
		ObservationRef:       "observe.org.manager_chain_current",
	},
	{
		Kind:                 EffectPositionOccupancy,
		Reversibility:        Compensatable,
		CompensationRef:      "position.reservation.release",
		CompensationStrategy: "RELEASE_RESERVATION",
		ObservationRef:       "observe.position.occupancy_consumed",
	},
	{
		// EffectCompensationRevision and EffectBudgetReservation are
		// declared by internal/domains/promotion/simcomp, which builds on
		// this package; their kinds are restated here as EffectKind values
		// (not imports, which would cycle) and pinned by simcomp's own
		// reversal-conformance test, so the two spellings cannot drift.
		Kind:                 EffectKind("rewards.compensation.revision"),
		Reversibility:        Reversible,
		CompensationRef:      "rewards.compensation.supersede",
		CompensationStrategy: "SUPERSEDING_REVISION",
		ObservationRef:       "observe.rewards.compensation_effective",
	},
	{
		Kind:                 EffectKind("budget.compensation_pool.reservation"),
		Reversibility:        Compensatable,
		CompensationRef:      "hcmnext.rewards.release_compensation_budget/v1",
		CompensationStrategy: "RELEASE_RESERVATION",
		ObservationRef:       "observe.budget.reservation_confirmed",
	},
}

// PromotionReversals returns the single reversibility declaration in kind
// order. The caller receives a copy: the declaration itself is immutable.
func PromotionReversals() []Reversal {
	out := make([]Reversal, len(promotionReversals))
	copy(out, promotionReversals)
	return out
}

// ReversalFor resolves one effect kind to its undo contract. False means the
// kind declares no reversal and therefore cannot be simulated, proposed,
// planned or cancelled: the shared effect constructors refuse it rather
// than inventing one.
func ReversalFor(kind EffectKind) (Reversal, bool) {
	for _, rev := range promotionReversals {
		if rev.Kind == kind {
			return rev, true
		}
	}
	return Reversal{}, false
}
