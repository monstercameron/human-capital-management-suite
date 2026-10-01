package safety

import "fmt"

// ValidateServingContract checks the closed vocabularies a serving cell must
// preserve when it exposes safety evidence. It does not create or mutate a
// safety record.
func ValidateServingContract() error {
	for _, compartment := range []SafetyCompartment{
		CompartmentOperational, CompartmentMedical, CompartmentClaims, CompartmentRegulatory,
	} {
		if !compartment.Valid() {
			return fmt.Errorf("safety: serving contract compartment %q is not declared", compartment)
		}
	}
	for _, kind := range []IncidentKind{IncidentInjury, IncidentIllness, IncidentNearMiss, IncidentPropertyDamage} {
		if !kind.Valid() {
			return fmt.Errorf("safety: serving contract incident kind %q is not declared", kind)
		}
	}
	for _, kind := range []InjuryKind{InjuryPhysical, InjuryIllness, InjuryExposure} {
		if !kind.Valid() {
			return fmt.Errorf("safety: serving contract injury kind %q is not declared", kind)
		}
	}
	for _, class := range []ReportabilityClass{NotReportable, OSHARecordable, OSHAReportableFatality, OSHAReportableSevere} {
		if !class.Valid() {
			return fmt.Errorf("safety: serving contract reportability class %q is not declared", class)
		}
	}
	for _, status := range []ClaimStatus{ClaimDraft, ClaimSubmitted, ClaimAccepted, ClaimPaid} {
		if !status.Valid() {
			return fmt.Errorf("safety: serving contract claim status %q is not declared", status)
		}
	}
	for _, status := range []RestrictionStatus{RestrictionProposed, RestrictionActive, RestrictionCleared} {
		if !status.Valid() {
			return fmt.Errorf("safety: serving contract restriction status %q is not declared", status)
		}
	}
	for _, status := range []CorrectiveActionStatus{CorrectiveActionOpen, CorrectiveActionVerified, CorrectiveActionClosed} {
		if !status.Valid() {
			return fmt.Errorf("safety: serving contract corrective-action status %q is not declared", status)
		}
	}
	for _, status := range []FilingStatus{FilingSubmitted, FilingAccepted, FilingRejected, FilingAmended} {
		if !status.Valid() {
			return fmt.Errorf("safety: serving contract filing status %q is not declared", status)
		}
	}
	for _, status := range []PaymentStatus{PaymentObserved, PaymentSettled, PaymentReversed} {
		if !status.Valid() {
			return fmt.Errorf("safety: serving contract payment status %q is not declared", status)
		}
	}
	for _, status := range []ReconciliationStatus{ReconciliationOpen, ReconciliationClosed} {
		if !status.Valid() {
			return fmt.Errorf("safety: serving contract reconciliation status %q is not declared", status)
		}
	}
	return nil
}
