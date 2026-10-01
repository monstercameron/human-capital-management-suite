package program

import (
	"fmt"
	"strings"
	"time"
)

// ServingContractID identifies the read-only Program contract composed by the
// shipped application cell. It does not grant authority or persist state.
const ServingContractID = "hcmnext.conformance.program/v1"

// ValidateServingContract exercises the complete Program path that a serving
// cell links: conformance-gated definition, exact revision resolution,
// population binding, and the governed enrollment lifecycle. All state is
// ephemeral, so composing this check cannot create a product-side effect.
func ValidateServingContract() error {
	catalog := NewCatalog()
	caller := Caller{ID: "serving-contract", Tenants: []string{"tenant-acme"}}
	if err := catalog.RecordConformance(SignedConformance{
		TodoID: "PROGRAM-CONF-001",
		Digest: "sha256:" + strings.Repeat("a", 64),
		Signer: "conformance-board",
	}); err != nil {
		return fmt.Errorf("program: serving conformance: %w", err)
	}

	definition, err := catalog.Define(caller, Definition{
		ID:         "serving-program",
		Name:       "Serving program",
		Type:       ProgramBenefit,
		Owner:      "total-rewards",
		Scope:      []string{"org:acme"},
		Funding:    FundingEmployer,
		Outcomes:   []string{"serving-outcome"},
		Extensions: map[string]string{"contract": ServingContractID},
	})
	if err != nil {
		return fmt.Errorf("program: serving definition: %w", err)
	}
	if !VerifyDefinition(definition) {
		return fmt.Errorf("program: serving definition did not reseal")
	}

	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	revision, err := catalog.AppendRevision(caller, Revision{
		ProgramID: definition.ID, Version: 1,
		Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
		From: from, To: to,
	})
	if err != nil {
		return fmt.Errorf("program: serving revision: %w", err)
	}
	resolved, err := catalog.ResolveRevision(ResolveContext{
		Tenant: "tenant-acme", Org: "org:acme", Jurisdiction: "US-CA",
		At: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || resolved.Digest != revision.Digest {
		return fmt.Errorf("program: serving revision resolution: got %q, err=%v", resolved.Digest, err)
	}

	binding, err := catalog.BindProgram(caller, Binding{
		ProgramID: definition.ID, RevisionDigest: revision.Digest,
		PopulationRef: "population:serving", EligibilityRef: "eligibility:serving",
		CycleRef: "cycle:serving", PopulationCount: 1, PopulationTotal: 1,
		AsOf:         time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		Completeness: CompletenessComplete, LateEntry: LateEntryDeny,
	})
	if err != nil {
		return fmt.Errorf("program: serving binding: %w", err)
	}
	if binding.Digest == "" {
		return fmt.Errorf("program: serving binding was not sealed")
	}

	enrollment, err := catalog.CreateEnrollment(caller, Enrollment{
		ID: "enrollment:serving", ProgramID: definition.ID, Participant: "worker:serving",
		Tenant: "tenant-acme", EligibilityRef: binding.EligibilityRef,
		Elections: []string{"standard"}, StartsAt: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		Reason: "serving-contract", Effects: []string{"effect:serving"},
	})
	if err != nil {
		return fmt.Errorf("program: serving enrollment: %w", err)
	}
	enrollment, err = catalog.ApplyTransition(caller, Transition{
		EnrollmentID: enrollment.ID, To: EnrollmentEnrolled,
		Reason: "serving-eligibility-confirmed", At: time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		ExpectedVersion: enrollment.Version,
	})
	if err != nil {
		return fmt.Errorf("program: serving enrollment transition: %w", err)
	}
	if enrollment.State != EnrollmentEnrolled {
		return fmt.Errorf("program: serving enrollment state is %q", enrollment.State)
	}
	enrollment, err = catalog.ApplyTransition(caller, Transition{
		EnrollmentID: enrollment.ID, To: EnrollmentWithdrawn,
		Reason: "serving-withdrawal", At: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		ExpectedVersion: enrollment.Version,
	})
	if err != nil {
		return fmt.Errorf("program: serving withdrawal: %w", err)
	}
	if enrollment.State != EnrollmentWithdrawn {
		return fmt.Errorf("program: serving withdrawal state is %q", enrollment.State)
	}
	return nil
}
