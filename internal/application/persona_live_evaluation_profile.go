package application

import (
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// QualifyPersonaCandidateModelProfile derives the production approval fields
// from one sealed measured report. Evaluation metadata changes the profile
// digest, so the report binds the unevaluated candidate digest and the returned
// profile receives its own canonical digest after qualification.
func QualifyPersonaCandidateModelProfile(report agenteval.PersonaEvaluationReport, candidate agentmodel.ModelProfile, agentVersionDigest string, suite agenteval.PersonaSuite) (agentmodel.ModelProfile, error) {
	evidence, err := report.Evidence()
	if err != nil || !evidence.Passed || candidate.Evaluation.Passed || candidate.ProfileDigest == "" ||
		candidate.ProfileDigest != agentmodel.ModelProfileDigest(candidate) || evidence.Target.ModelDigest != "sha256:"+candidate.ProfileDigest ||
		!personaRequestDigest(agentVersionDigest) || evidence.SuiteID != suite.ID || evidence.SuiteDigest != agenteval.PersonaSuiteDigest(suite) ||
		len(evidence.Cases) != len(suite.Cases) || len(evidence.Cases) < 8 || evidence.CostMicros > suite.MaxCostMicros {
		return agentmodel.ModelProfile{}, agenteval.ErrPersonaEvaluation
	}
	for i, result := range evidence.Cases {
		if !result.Passed || result.CaseID != suite.Cases[i].ID || !personaRequestDigest(result.EvidenceDigest) {
			return agentmodel.ModelProfile{}, agenteval.ErrPersonaEvaluation
		}
	}
	qualified := candidate
	qualified.Regions = slices.Clone(candidate.Regions)
	qualified.DataClasses = slices.Clone(candidate.DataClasses)
	qualified.TaskProfileIDs = slices.Clone(candidate.TaskProfileIDs)
	qualified.Evaluation = agentmodel.ModelEvaluation{AgentVersionDigest: agentVersionDigest, SuiteDigest: evidence.SuiteDigest, Passed: true}
	qualified.ProfileDigest = agentmodel.ModelProfileDigest(qualified)
	return qualified, nil
}
