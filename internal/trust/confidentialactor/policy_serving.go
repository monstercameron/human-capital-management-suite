package confidentialactor

import "fmt"

// ServingContractID identifies the disclosure contract linked by the shipped
// application cell. The check is deliberately pure: it validates the
// semantic owner's explicit, closed vocabulary without granting authority or
// retaining intake state.
const ServingContractID = "hcmnext.confidential-actor/v1"

// ValidateServingContract keeps confidential-actor policy on the served
// dependency path. Every mode is exercised with an explicit declaration, so
// a composed cell cannot silently fall back to a mode or disclosure policy.
func ValidateServingContract() error {
	for _, mode := range []Mode{ModeKnown, ModePseudonymous, ModeAnonymous, ModeEscrowed} {
		intake := Intake{
			Mode: mode,
			Abuse: AbuseControls{
				RateLimitPerHour: 1,
				ReportChannel:    "safety://serving-contract",
				BlockOnRisk:      true,
			},
			Revelation: RevelationPolicy{
				Allowed:          mode != ModeAnonymous,
				Trigger:          "serving-review",
				ApproverRole:     "confidentiality-reviewer",
				RequiresEvidence: true,
			},
			Downstream: DisclosureLimit{
				Recipients: []string{"serving-case-team"},
				Fields:     []string{"case_status"},
				Expiry:     "2027-01-01T00:00:00Z",
				Notify:     true,
			},
		}
		if mode != ModeAnonymous {
			intake.IdentityEvidence = []IdentityEvidence{{Kind: "serving-proof", Ref: "evidence:serving"}}
		}
		if err := intake.Validate(); err != nil {
			return fmt.Errorf("confidentialactor: serving mode %q: %w", mode, err)
		}
	}
	return nil
}
