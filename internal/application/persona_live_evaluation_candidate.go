package application

import (
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// NewLocalPersonaOpenAICandidate derives the unqualified evaluation pin from
// the exact immutable local Policy Helper manifest. It carries no passing
// evaluation, production publication or ordinary-chat routing permission.
func NewLocalPersonaOpenAICandidate(manifest agentmanifest.Manifest) (agentmodel.ModelProfile, PersonaRunModelRoute, error) {
	policy, _ := LocalPersonaOpenAIModelPolicyReference()
	starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	digest, err := manifest.Digest()
	if err != nil || !ok || manifest.ModelPolicy != policy || manifest.OutputSchema.ID != PersonaChatReplySchema || manifest.OutputSchema.Digest != PersonaChatReplySchemaDigest || !slices.Equal(manifest.ToolCeiling, personaStarterToolCeiling(starter)) || manifest.Budget.MaxCostMicros > uint64(LocalPersonaOpenAIMaxCostMicros) {
		return agentmodel.ModelProfile{}, PersonaRunModelRoute{}, agenteval.ErrPersonaEvaluation
	}
	taskID := "local.persona.policy_helper.reply"
	profile := agentmodel.ModelProfile{ID: "local-openai-policy-helper-" + strings.TrimPrefix(digest, "sha256:")[:12], Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion}, Regions: []string{LocalPersonaOpenAIRegion}, DataClasses: []string{"INTERNAL", "PUBLIC"}, TaskProfileIDs: []string{taskID}, MaxLatency: LocalPersonaOpenAIMaxLatency, MaxCostMicros: int64(manifest.Budget.MaxCostMicros), ExpectedCostMicros: int64(manifest.Budget.MaxCostMicros), SemanticsDigest: policy.Digest, OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: LocalPersonaOpenAIToolSchemaDigest()}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	route := PersonaRunModelRoute{Route: agentmodel.RouteRequest{TraceID: "candidate-evaluation", Pin: agentmodel.ModelPin{AgentVersionDigest: digest, TaskProfileID: taskID, Primary: selection, SemanticsDigest: profile.SemanticsDigest, OutputSchemaDigest: profile.OutputSchemaDigest, ToolSchemaDigest: profile.ToolSchemaDigest}, Task: agentmodel.TaskProfile{ID: taskID, AgentVersionDigest: digest, Region: LocalPersonaOpenAIRegion, DataClasses: profile.DataClasses, MaxLatency: profile.MaxLatency, MaxCostMicros: profile.MaxCostMicros, SemanticsDigest: profile.SemanticsDigest, OutputSchemaDigest: profile.OutputSchemaDigest, ToolSchemaDigest: profile.ToolSchemaDigest}, BudgetRemainingMicros: profile.MaxCostMicros}, Purpose: LocalPersonaOpenAIPurpose, Processing: agentmodel.ProcessingPolicy{Residency: LocalPersonaOpenAIRegion, Retention: fmt.Sprintf("%s:%d", agentegress.RetentionBounded, int64(LocalPersonaOpenAIRetention)), TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseAllowed}, Egress: agentegress.Profile{ID: profile.ID, Kind: agentegress.TargetModel, AllowedRegions: []string{LocalPersonaOpenAIRegion}, AllowedClasses: classes, Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionBounded, MaxAge: LocalPersonaOpenAIRetention}}, TaskPolicy: agentegress.TaskPolicy{AllowedRegions: []string{LocalPersonaOpenAIRegion}, AllowedResultClasses: classes, MaxExternalRetention: LocalPersonaOpenAIRetention, ResultRetention: LocalPersonaOpenAIRetention}, ProfileClass: trustdlp.ClassInternal, InvokerClass: trustdlp.ClassInternal, ThreadClass: trustdlp.ClassInternal}
	return profile, route, nil
}
