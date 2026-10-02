package application

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// NewChatlangDeployment derives the approval document for the translation task
// from an approved deployment: the same provider, model identity, pricing,
// credential, worker and signing identities, with exactly one profile qualified
// for ChatlangTaskProfileID, one destination for ChatlangPurpose, the matching
// clearance, and a credential scope for each workspace the base deployment
// already serves. It is the translation counterpart of NewChattoneDeployment.
//
// It does not run an evaluation and never invents evidence: the caller supplies
// the evaluation record (a passing suite digest the deployer is answerable for)
// and the document states it. The result is checked against the same rules the
// service applies at start-up.
func NewChatlangDeployment(base PersonaModelDeployment, evaluation agentmodel.ModelEvaluation, maxLatency time.Duration, maxCostMicros int64) (PersonaModelDeployment, error) {
	if len(base.Profiles) == 0 || len(base.Destinations) == 0 || len(base.Credential.Scopes) == 0 {
		return PersonaModelDeployment{}, fmt.Errorf("%w: the base deployment has no profile, destination or credential scope", errChatlangBinding)
	}
	if !evaluation.Passed || strings.TrimSpace(evaluation.AgentVersionDigest) == "" || strings.TrimSpace(evaluation.SuiteDigest) == "" {
		return PersonaModelDeployment{}, fmt.Errorf("%w: a passing evaluation with agent-version and suite digests is required", errChatlangBinding)
	}
	task := ChatlangTaskProfile()
	if maxLatency <= 0 || maxLatency > time.Minute || maxCostMicros <= 0 || maxCostMicros > task.MaxCostMicros {
		return PersonaModelDeployment{}, fmt.Errorf("%w: latency or cost is outside the task's bounds", errChatlangBinding)
	}
	template := base.Profiles[0]
	var baseTerms *agentegress.ProviderTerms
	for i := range base.Terms {
		if base.Terms[i].ModelProfile == template.ID {
			baseTerms = &base.Terms[i]
		}
	}
	if baseTerms == nil {
		return PersonaModelDeployment{}, fmt.Errorf("%w: the template profile has no provider terms", errChatlangBinding)
	}
	sum := sha256.Sum256([]byte(evaluation.AgentVersionDigest + "\x00" + evaluation.SuiteDigest + "\x00" + template.Identity.ModelID + "\x00" + template.Identity.Version))
	profile := agentmodel.ModelProfile{
		ID: "chat-translation-" + hex.EncodeToString(sum[:])[:12], Identity: template.Identity, Regions: slices.Clone(template.Regions),
		DataClasses: slices.Clone(task.DataClasses), TaskProfileIDs: []string{task.ID}, MaxLatency: maxLatency, MaxCostMicros: maxCostMicros, ExpectedCostMicros: maxCostMicros / 2,
		SemanticsDigest: task.SemanticsDigest, OutputSchemaDigest: chatlangOutputSchemaDigest, ToolSchemaDigest: chatlangToolSchemaDigest, Evaluation: evaluation,
	}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	terms := *baseTerms
	terms.ModelProfile = profile.ID
	terms.AllowedClasses = slices.Clone(classes)
	terms.AllowedRegions = slices.Clone(baseTerms.AllowedRegions)
	terms.SourceRules = []agentegress.ProviderSourceRule{
		{Class: chatlangSourceInstruction, Classes: slices.Clone(classes)},
		{Class: chatlangSourceText, Classes: slices.Clone(classes)},
	}
	region := ""
	for _, r := range profile.Regions {
		if slices.Contains(terms.AllowedRegions, r) {
			region = r
			break
		}
	}
	if region == "" {
		return PersonaModelDeployment{}, fmt.Errorf("%w: profile and terms share no processing region", errChatlangBinding)
	}
	out := base
	out.Profiles = []agentmodel.ModelProfile{profile}
	out.Terms = []agentegress.ProviderTerms{terms}
	destination := base.Destinations[0]
	destination.Name = profile.ID
	destination.Purposes = []string{ChatlangPurpose}
	destination.DataClasses = []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}
	out.Destinations = append(base.Destinations[:0:0], destination)
	out.Clearances = []trustdlp.Clearance{{Destination: profile.ID, Classes: slices.Clone(classes), Decision: trustdlp.Allow}}
	out.Credential.Scopes = nil
	seen := map[string]bool{}
	for _, scope := range base.Credential.Scopes {
		if seen[scope.TenantID] {
			continue
		}
		seen[scope.TenantID] = true
		out.Credential.Scopes = append(out.Credential.Scopes, OpenAIModelCredentialScope{TenantID: scope.TenantID, Region: region, Purpose: ChatlangPurpose, Destination: profile.ID})
	}
	// Policy sources and registrations belong to persona agents, not this task.
	out.PolicySources, out.PolicyRegistrations = nil, nil
	if _, _, err := out.validate(); err != nil {
		return PersonaModelDeployment{}, err
	}
	if _, err := chatlangQualify(out); err != nil {
		return PersonaModelDeployment{}, err
	}
	return out, nil
}
