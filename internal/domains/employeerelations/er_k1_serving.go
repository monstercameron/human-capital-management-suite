package employeerelations

import (
	"errors"
	"fmt"
)

// ServingContractID identifies the pure employee-relations contract composed
// by the shipped application service. It grants no authority and performs no
// durable writes.
const ServingContractID = "hcmnext.conformance.employee-relations/v1"

// ValidateServingContract exercises the complete employee-relations path that
// a serving composition exposes. The check is deterministic and read-only:
// it proves the domain is in the production dependency closure without
// creating a case or weakening any evidence, review, chronology, or privacy
// rule.
func ValidateServingContract() error {
	participants := []Participant{
		{Role: RoleSubject, Ref: "worker:subject"},
		{Role: RoleReporter, Ref: "worker:reporter"},
		{Role: RoleInvestigator, Ref: "worker:investigator"},
		{Role: RoleInterviewer, Ref: "worker:interviewer"},
		{Role: RoleInterviewee, Ref: "worker:subject"},
		{Role: RoleDecisionMaker, Ref: "worker:decision-maker"},
		{Role: RoleRepresentative, Ref: "worker:representative"},
		{Role: RoleReviewer, Ref: "worker:reviewer"},
		{Role: RoleWitness, Ref: "worker:witness"},
		{Role: RoleReviewer, Ref: "worker:manager"},
	}
	const caseRef = "case:serving"
	const compartmentRef = "compartment:employee-relations"

	allegation, err := NewAllegationRevision(AllegationRevision{
		ID: "allegation:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, ReporterRef: "worker:reporter", SubjectRef: "worker:subject",
		Summary: "serving allegation", Status: AllegationOpen, RetaliationSafeguard: true, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving allegation: %w", err)
	}
	investigation, err := NewInvestigationRevision(InvestigationRevision{
		ID: "investigation:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, AllegationRef: allegation.CanonicalDigest,
		InvestigatorRef: "worker:investigator", AuthorityRef: "authority:serving",
		Purpose: "establish facts", Scope: "reported conduct", Status: InvestigationActive, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving investigation: %w", err)
	}
	interview, err := NewInterviewRevision(InterviewRevision{
		ID: "interview:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, InvestigationRef: investigation.CanonicalDigest,
		InterviewerRef: "worker:interviewer", IntervieweeRef: "worker:subject",
		Statement: "sealed serving statement", Status: InterviewSealed, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving interview: %w", err)
	}
	finding, err := NewFindingRevision(FindingRevision{
		ID: "finding:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, InvestigationRef: investigation.CanonicalDigest,
		InvestigatorRef: "worker:investigator", SubjectRef: "worker:subject", ReporterRef: "worker:reporter",
		EvidenceStandard: EvidenceMoreLikelyThanNot, EvidenceRefs: []string{"evidence:serving"},
		Disposition: FindingSubstantiated, Rationale: "serving evidence supports the finding", Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving finding: %w", err)
	}
	discipline, err := NewDisciplineRevision(DisciplineRevision{
		ID: "discipline:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, FindingRef: finding.CanonicalDigest, SubjectRef: "worker:subject",
		Action: "serving review action", LegalReviewRef: "review:legal", RepresentationReviewRef: "review:representation",
		Status: DisciplineApproved, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving discipline: %w", err)
	}
	grievance, err := NewGrievanceRevision(GrievanceRevision{
		ID: "grievance:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, DecisionRef: discipline.CanonicalDigest, GrievantRef: "worker:subject",
		Grounds: "serving decision is contested", Status: GrievanceOpen, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving grievance: %w", err)
	}

	chronology, err := NewCaseChronology(caseRef, compartmentRef, participants)
	if err != nil {
		return fmt.Errorf("employeerelations: serving chronology: %w", err)
	}
	for _, entry := range [][3]string{
		{"allegation", allegation.ID, allegation.CanonicalDigest},
		{"investigation", investigation.ID, investigation.CanonicalDigest},
		{"finding", finding.ID, finding.CanonicalDigest},
		{"discipline", discipline.ID, discipline.CanonicalDigest},
		{"grievance", grievance.ID, grievance.CanonicalDigest},
	} {
		if err := chronology.AppendRecord(entry[0], entry[1], entry[2]); err != nil {
			return fmt.Errorf("employeerelations: serving chronology %s: %w", entry[0], err)
		}
	}
	appeal, err := NewAppealRevision(AppealRevision{
		ID: "appeal:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, ContestedDigest: finding.CanonicalDigest, ContestedKind: "finding",
		Grounds: "serving new evidence", DeadlineRef: "deadline:serving", RepresentationRef: "review:representation",
		HoldRefs: []string{"hold:serving"}, Status: AppealOpen, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving appeal: %w", err)
	}
	if err := chronology.AppendAppeal(appeal); err != nil {
		return fmt.Errorf("employeerelations: serving appeal chronology: %w", err)
	}
	settlement, err := NewSettlementRevision(SettlementRevision{
		ID: "settlement:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, AllegationDigest: allegation.CanonicalDigest, GrievanceDigest: grievance.CanonicalDigest,
		TermsRef: "terms:serving", AuthorityRef: "authority:settlement", HoldRefs: []string{"hold:serving"},
		HoldsChecked: true, RetaliationSafeguard: true, Status: SettlementApproved, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving settlement: %w", err)
	}
	if err := chronology.AppendSettlement(settlement); err != nil {
		return fmt.Errorf("employeerelations: serving settlement chronology: %w", err)
	}
	correction, err := NewCorrectionRevision(CorrectionRevision{
		ID: "correction:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		Participants: participants, CorrectsDigest: discipline.CanonicalDigest, CorrectsKind: "discipline",
		Correction: "serving corrected action", Reason: "serving correction reason", AuthorityRef: "authority:correction",
		Status: CorrectionIssued, Revision: 1,
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving correction: %w", err)
	}
	if err := chronology.AppendCorrection(correction); err != nil {
		return fmt.Errorf("employeerelations: serving correction chronology: %w", err)
	}

	if interview.CanonicalDigest == "" || len(chronology.Entries) != 8 {
		return fmt.Errorf("employeerelations: serving chronology is incomplete")
	}
	pkg := chronology.EvidencePackage()
	if len(pkg.Holds) != 1 || len(pkg.Deadlines) != 1 || pkg.Digest == "" {
		return fmt.Errorf("employeerelations: serving evidence package is incomplete")
	}
	signal, err := NewRetaliationSignal(RetaliationSignal{
		ID: "retaliation:serving", CaseRef: caseRef, CompartmentRef: compartmentRef,
		ReporterRef: "worker:reporter", SubjectRef: "worker:subject", ManagerRef: "worker:manager", DetailRef: "evidence:retaliation",
	})
	if err != nil {
		return fmt.Errorf("employeerelations: serving retaliation signal: %w", err)
	}
	if err := signal.AuthorizeViewer("worker:subject", RoleSubject, participants); !errors.Is(err, ErrRetaliationProtected) {
		return fmt.Errorf("employeerelations: serving retaliation disclosure was not refused")
	}
	if err := signal.AuthorizeViewer("worker:investigator", RoleInvestigator, participants); err != nil {
		return fmt.Errorf("employeerelations: serving investigator disclosure: %w", err)
	}
	return nil
}
