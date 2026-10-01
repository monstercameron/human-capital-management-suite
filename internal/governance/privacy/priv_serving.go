package privacy

import (
	"fmt"
	"time"
)

var servingStateLawStates = [...]StateCode{StateCA, StateCO, StateVA, StateTX}

// ValidateServingContract checks the state-law, processor and FTI contracts
// that a serving composition must carry. The checks stay with privacy
// governance so a production cell cannot accidentally omit a newly closed
// status or lose the live FTI envelope-key policy.
func ValidateServingContract() error {
	if err := validateStateLawServingContract(); err != nil {
		return fmt.Errorf("privacy: state-law serving contract: %w", err)
	}
	for _, status := range []ItemStatus{
		ItemResolved, ItemPendingRetry, ItemEscalated, ItemException,
	} {
		if !status.valid() {
			return fmt.Errorf("privacy: serving contract status %q is not declared", status)
		}
	}
	for _, outcome := range []AckOutcome{AckFulfilled, AckRefused, AckUnknownCopy} {
		if !outcome.valid() {
			return fmt.Errorf("privacy: serving contract acknowledgement %q is not declared", outcome)
		}
	}
	for _, action := range []ProcessorAction{ProcessorActionDelete, ProcessorActionReturn} {
		if !action.valid() {
			return fmt.Errorf("privacy: serving contract action %q is not declared", action)
		}
	}
	if _, err := requiredFTIKeyScope(); err != nil {
		return fmt.Errorf("privacy: serving contract FTI key scope: %w", err)
	}
	return nil
}

// ValidateStateLawRoster verifies the roster that a serving composition is
// about to use. Every gated state must resolve from the same signed,
// primary-source-reviewed roster at the release's evaluation date; callers
// cannot silently fall back to another state's answer or a global toggle.
func ValidateStateLawRoster(roster Roster, asOf time.Time) error {
	if asOf.IsZero() {
		return fmt.Errorf("%w: state-law release date is required", ErrStateLawRefused)
	}
	for _, state := range servingStateLawStates {
		if _, err := roster.Resolve(state, asOf); err != nil {
			return fmt.Errorf("%w: serving roster cannot resolve %s: %v", ErrStateLawRefused, state, err)
		}
	}
	return nil
}

func validateStateLawServingContract() error {
	seen := make(map[StateCode]struct{}, len(servingStateLawStates))
	for _, state := range servingStateLawStates {
		if !ValidStateLaw(state) {
			return fmt.Errorf("state %q is not declared", state)
		}
		if _, duplicate := seen[state]; duplicate {
			return fmt.Errorf("state %q is declared more than once", state)
		}
		seen[state] = struct{}{}
	}
	return nil
}
