package cba

import (
	"fmt"
	"time"
)

// ServingContractID identifies the read-only CBA contract composed by the
// shipped application cell. It grants no authority and creates no durable
// state.
const ServingContractID = "hcmnext.conformance.cba/v1"

// ValidateServingContract exercises the CBA applicability and agreement
// change paths that the serving composition links. The fixture is ephemeral:
// it proves revision lineage, representation, and typed change obligations
// without persisting a bargaining record or contacting an external provider.
func ValidateServingContract() error {
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	agreement := AgreementRevision{
		ID: "agreement:serving:revision:1", AgreementID: "agreement:serving", Revision: "1",
		Version: "serving-1", Representative: "representative:serving", Source: "source:serving",
		EffectiveFrom: at, KnownFrom: at, Precedence: 10,
	}
	unit := BargainingUnitRevision{
		ID: "unit:serving:revision:1", UnitID: "unit:serving", Revision: "1",
		AgreementID: agreement.AgreementID, Representative: agreement.Representative, Source: "unit-source:serving",
		EffectiveFrom: at, KnownFrom: at,
	}
	membership := MembershipRevision{
		ID: "membership:serving:revision:1", MembershipID: "membership:serving", Revision: "1",
		WorkerID: "worker:serving", UnitID: unit.UnitID, Source: "membership-source:serving",
		EffectiveFrom: at, KnownFrom: at,
	}
	applicability, err := ResolveApplicability(ApplicabilityRequest{
		WorkerID: "worker:serving", AgreementID: agreement.AgreementID,
		EffectiveAt: at, KnownAt: at,
		Agreements: []AgreementRevision{agreement}, Units: []BargainingUnitRevision{unit},
		Memberships: []MembershipRevision{membership},
	})
	if err != nil {
		return fmt.Errorf("cba: serving applicability: %w", err)
	}
	if applicability.Outcome != Applicable || applicability.AgreementRevision != agreement.Revision || applicability.UnitID != unit.UnitID || applicability.MembershipID != membership.MembershipID {
		return fmt.Errorf("cba: serving applicability was not fully evidenced: %+v", applicability)
	}
	if _, err := applicability.Digest(); err != nil {
		return fmt.Errorf("cba: serving applicability digest: %w", err)
	}

	prior, err := ComposeConstraints(CompositionRequest{
		Applicability: applicability, KnownAt: at, SeniorityServiceRevision: "seniority:serving:1",
		Constraints: []CBAConstraint{
			{ID: "constraint:wage", ClauseRef: "clause:wage", ReleaseRef: "release:serving:1", Kind: ConstraintWage, Source: SourceAgreement, Mandatory: true},
			{ID: "constraint:discipline", ClauseRef: "clause:discipline", ReleaseRef: "release:serving:1", Kind: ConstraintDiscipline, Source: SourceAgreement, Mandatory: true},
		},
		Policy: CompositionPolicy{
			Wage: CompareHigherMinimum, Schedule: CompareHigherMinimum, Leave: CompareHigherMinimum,
			Seniority: CompareHigherMinimum, Discipline: CompareHigherMinimum,
		},
		Provenance: []ProvenancePin{{Source: SourceAgreement, ReleaseRef: "release:serving:1", ClauseRefs: []string{"clause:wage", "clause:discipline"}}},
	})
	if err != nil {
		return fmt.Errorf("cba: serving composition: %w", err)
	}
	if prior.Outcome != CompositionAllowWithObligations || len(prior.Obligations) != 2 {
		return fmt.Errorf("cba: serving composition was not obligation-bearing: %+v", prior)
	}

	change, err := AnalyzeAgreementChange(AgreementChange{
		AgreementID: agreement.AgreementID, FromRevision: agreement.Revision, ToRevision: "2",
		ChangedClauses: []ClauseChange{
			{ClauseRef: "clause:wage", Kind: ConstraintWage, FromValue: "18.00", ToValue: "19.25"},
			{ClauseRef: "clause:discipline", Kind: ConstraintDiscipline, FromValue: "verbal", ToValue: "written"},
		},
		AffectedPopulation:   PopulationPin{ID: unit.UnitID, FrozenAt: at, Digest: "sha256:serving-population", MemberCount: 1},
		GrievanceDeadlineRef: "deadline:grievance:serving", ArbitrationDeadlineRef: "deadline:arbitration:serving",
		RepresentationRef: "representative:serving", CalendarRef: "calendar:serving", EffectiveAt: at.Add(24 * time.Hour), EvidenceDigest: "sha256:serving-change",
	}, prior)
	if err != nil {
		return fmt.Errorf("cba: serving agreement change: %w", err)
	}
	if err := change.Validate(); err != nil {
		return fmt.Errorf("cba: serving agreement impact: %w", err)
	}
	if change.PriorDigest != PriorDigestOf(prior) || len(change.Intents) != 7 || change.RepresentationRef == "" {
		return fmt.Errorf("cba: serving agreement impact lost history or obligations: %+v", change)
	}
	return nil
}
