package succession

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the read-only succession contract owned by
// this package. It grants no authority and performs no durable writes.
const ServingContractID = "hcmnext.conformance.succession/v1"

// ValidateServingContract exercises the pure role, readiness, slate, revision
// and disclosure path that a serving composition may expose. Keeping this
// check beside the semantic owner prevents a serving caller from bypassing
// the evidence and visibility rules by assembling fields independently.
func ValidateServingContract() error {
	when := values.NewInstant(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC))
	role, err := NewCriticalRole(CriticalRole{
		RoleID: "serving-critical-role", Revision: 1,
		PositionRef: "position:serving", JobRevisionRef: "job:serving",
		OwnerRef: "owner:serving", AuthorityRef: "authority:serving",
		EffectiveAt: when, KnownAt: when, EvidenceRefs: []string{"evidence:role"},
	})
	if err != nil {
		return fmt.Errorf("succession: serving critical role: %w", err)
	}

	for _, band := range []ReadinessBand{ReadinessReadyNow, ReadinessReadyLater, ReadinessNotReady, ReadinessUnknown} {
		assessment, assessmentErr := NewSuccessorReadinessRevision(SuccessorReadinessRevision{
			SuccessorID: "worker:serving", Revision: 1, Readiness: band,
			VacancyRisk: VacancyRiskUnknown, AssessedBy: "assessor:serving",
			AssessmentSource: "assessment:serving", EvidenceRefs: []string{"evidence:readiness"},
			EffectiveAt: when, KnownAt: when,
		})
		if assessmentErr != nil || !assessment.Readiness.Valid() {
			return fmt.Errorf("succession: serving readiness %q: %w", band, assessmentErr)
		}
	}

	readiness, err := NewSuccessorReadinessRevision(SuccessorReadinessRevision{
		SuccessorID: "worker:serving", Revision: 1, Readiness: ReadinessReadyNow,
		VacancyRisk: VacancyRiskMedium, AssessedBy: "assessor:serving",
		AssessmentSource: "assessment:serving", EvidenceRefs: []string{"evidence:readiness"},
		EffectiveAt: when, KnownAt: when,
	})
	if err != nil {
		return fmt.Errorf("succession: serving readiness: %w", err)
	}
	slate, err := NewSuccessionSlate(SuccessionSlate{
		SlateID: "slate:serving", Revision: 1,
		CriticalRoleID: role.RoleID, CriticalRoleRevision: role.Revision,
		CriticalRoleDigest: role.CanonicalDigest, PositionRef: role.PositionRef,
		JobRevisionRef: role.JobRevisionRef, NominatorID: "manager:serving",
		NominatorRole: "ROLE_MANAGER", NominatorAuthorizationRef: "authorization:serving",
		NominatorAuthorized: true, Visibility: DisclosureScoped,
		DeclaredScopes: []string{"talent.read"}, Candidates: []SuccessorReadinessRevision{readiness},
		EffectiveAt: when, KnownAt: when,
	})
	if err != nil {
		return fmt.Errorf("succession: serving slate: %w", err)
	}
	if _, err := slate.CandidatesFor("talent.read"); err != nil {
		return fmt.Errorf("succession: serving disclosed candidates: %w", err)
	}
	revised, err := slate.Revise([]SuccessorReadinessRevision{readiness}, "manager:serving-2", "authorization:serving-2")
	if err != nil || revised.ParentDigest != slate.CanonicalDigest {
		return fmt.Errorf("succession: serving slate revision: %w", err)
	}

	withheld := slate
	withheld.Visibility = DisclosureWithheld
	withheld.DeclaredScopes = nil
	withheld.CanonicalDigest = ""
	withheld, err = NewSuccessionSlate(withheld)
	if err != nil {
		return fmt.Errorf("succession: serving withheld slate: %w", err)
	}
	if _, err := withheld.CandidatesFor("talent.read"); !errors.Is(err, ErrSlateMembershipWithheld) {
		return fmt.Errorf("succession: serving withheld disclosure returned %v", err)
	}
	explanation, err := withheld.Explain()
	if err != nil || explanation.CandidateCount != 0 {
		return fmt.Errorf("succession: serving withheld explanation leaked membership: %+v (err=%v)", explanation, err)
	}

	unauthorized := slate
	unauthorized.NominatorAuthorized = false
	if err := unauthorized.Validate(); !errors.Is(err, ErrUnauthorizedNomination) {
		return fmt.Errorf("succession: serving unauthorized nomination returned %v", err)
	}
	return nil
}
