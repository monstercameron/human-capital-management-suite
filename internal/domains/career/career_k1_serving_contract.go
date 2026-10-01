package career

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/skill"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServingContractID identifies the career contract composed by the shipped
// application cell. It links the domain model into serving composition without
// granting authority or creating durable state.
const ServingContractID = "hcmnext.conformance.career/v1"

// ValidateServingContract exercises the served career path: worker-authored
// preferences remain distinct from role revisions, readiness is computed
// against a pinned skill snapshot, development completion carries evidence,
// and assessments remain opinion/inference/recommendation rather than facts.
func ValidateServingContract() error {
	worker := careerServingRef(values.Kind("worker"), "00000000-0000-0000-0000-000000000401")
	role := careerServingRef(values.Kind("career_target_role"), "00000000-0000-0000-0000-000000000402")
	skillRef := careerServingRef(skill.KindSkill, "00000000-0000-0000-0000-000000000403")
	interval, err := careerServingInterval()
	if err != nil {
		return fmt.Errorf("career: serving interval: %w", err)
	}
	revision, err := values.NewSequenceRevision("career-serving", 1)
	if err != nil {
		return fmt.Errorf("career: serving revision: %w", err)
	}

	preference, err := NewCareerPreference(CareerPreferenceProfileRevision{
		PreferenceID:   careerServingRef(values.Kind("career_preference"), "00000000-0000-0000-0000-000000000404"),
		Revision:       revision,
		Worker:         worker,
		Mobility:       MobilityInternational,
		TargetRoleRefs: []values.EntityRef{role},
		Timeframe:      interval,
		Visibility:     VisibilityWorkerOnly,
	})
	if err != nil {
		return fmt.Errorf("career: serving preference: %w", err)
	}

	roleRevision, err := values.NewSequenceRevision("career-serving-role", 1)
	if err != nil {
		return fmt.Errorf("career: serving role revision: %w", err)
	}
	jobProfileRevision, err := values.NewSequenceRevision("job-profile-serving", 7)
	if err != nil {
		return fmt.Errorf("career: serving job-profile revision: %w", err)
	}
	target, err := NewTargetRoleProfile(TargetRoleProfileRevision{
		TargetRoleID:       role,
		Revision:           roleRevision,
		Worker:             worker,
		JobProfile:         careerServingRef(values.Kind("job_profile"), "00000000-0000-0000-0000-000000000405"),
		JobProfileRevision: jobProfileRevision,
		Requirements:       []RoleSkillRequirement{{SkillRef: skillRef, MinimumLevel: 3}},
		Visibility:         VisibilityWorkerOnly,
		Effective:          interval,
	})
	if err != nil {
		return fmt.Errorf("career: serving target role: %w", err)
	}

	definition, err := skill.NewSkillDefinition(skill.SkillDefinitionRevision{
		SkillRef:         skillRef,
		Revision:         revision,
		Name:             "serving-skill",
		ProficiencyScale: skill.DefaultProficiencyScale(),
	})
	if err != nil {
		return fmt.Errorf("career: serving skill definition: %w", err)
	}
	ontology, err := skill.NewSkillOntology(skill.SkillOntologyRevision{
		OntologyID: careerServingRef(values.Kind("skill_ontology"), "00000000-0000-0000-0000-000000000406"),
		Revision:   revision,
		Skills:     []skill.SkillDefinitionRevision{definition},
	})
	if err != nil {
		return fmt.Errorf("career: serving skill ontology: %w", err)
	}
	evidence, err := skill.NewWorkerSkillEvidence(skill.WorkerSkillEvidence{
		EvidenceID:   careerServingRef(values.Kind("skill_evidence"), "00000000-0000-0000-0000-000000000407"),
		Worker:       worker,
		SkillRef:     skillRef,
		Level:        4,
		EvidenceKind: skill.EvidenceCredential,
		EvidenceRef:  "career-serving-evidence",
		Verified:     true,
		Effective:    interval,
	})
	if err != nil {
		return fmt.Errorf("career: serving skill evidence: %w", err)
	}
	date, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		return fmt.Errorf("career: serving as-of date: %w", err)
	}
	readiness, err := ComputeReadinessGaps(context.Background(), skill.NewPinnedResolver(ontology, nil, skill.FakeSkillEvidenceReader{Evidence: []skill.WorkerSkillEvidence{evidence}}), target, date)
	if err != nil {
		return fmt.Errorf("career: serving readiness: %w", err)
	}
	if readiness.Status != ReadinessReady || len(readiness.Gaps) != 1 || readiness.Gaps[0].Gap != 0 {
		return fmt.Errorf("career: serving readiness was not ready: %+v", readiness)
	}

	objective, err := NewDevelopmentObjective(DevelopmentObjectiveProfileRevision{
		ObjectiveID:            careerServingRef(values.Kind("development_objective"), "00000000-0000-0000-0000-000000000408"),
		Revision:               revision,
		Worker:                 worker,
		TargetRole:             role,
		SkillRefs:              []values.EntityRef{skillRef},
		Description:            "Build serving capability",
		Owner:                  careerServingRef(values.Kind("career_owner"), "00000000-0000-0000-0000-000000000409"),
		State:                  ObjectiveComplete,
		CompletionEvidenceRefs: []string{"career-serving-completion"},
		Visibility:             VisibilityWorkerOnly,
		Effective:              interval,
	})
	if err != nil {
		return fmt.Errorf("career: serving objective: %w", err)
	}
	if len(objective.CompletionEvidenceRefs) == 0 || objective.CanonicalDigest == "" {
		return fmt.Errorf("career: serving objective lacks completion evidence or digest")
	}

	assessment := CareerAssessmentRevision{
		AssessmentID: careerServingRef(values.Kind("career_assessment"), "00000000-0000-0000-0000-000000000410"),
		Revision:     revision,
		Worker:       worker,
		TargetRole:   role,
		Source:       careerServingRef(values.Kind("source"), "00000000-0000-0000-0000-000000000411"),
		Epistemic:    EpistemicModelInference,
		Visibility:   VisibilityWorkerOnly,
		Summary:      "serving fit inference",
		Effective:    interval,
	}
	if err := assessment.Validate(); err != nil {
		return fmt.Errorf("career: serving assessment: %w", err)
	}
	if assessment.Epistemic == EpistemicFact || preference.Worker != assessment.Worker || preference.CanonicalDigest == "" {
		return fmt.Errorf("career: serving preference and assessment semantics collapsed")
	}
	if explanation := Explain(preference); explanation == "" || strings.Contains(explanation, string(preference.Mobility)) {
		return fmt.Errorf("career: serving explanation disclosed worker preference")
	}
	return nil
}

func careerServingRef(kind values.Kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: "acme", Kind: kind, Id: id}
}

func careerServingInterval() (values.EffectiveInterval, error) {
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	end, err := values.ParseLocalDate("2027-01-01")
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	return values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "gregorian", Version: "1"})
}
