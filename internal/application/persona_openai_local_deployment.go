package application

import (
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

const (
	LocalPersonaOpenAIModelID      = "gpt-5-mini"
	LocalPersonaOpenAIModelVersion = "gpt-5-mini-2025-08-07"
	// Global is an unconstrained provider processing envelope, not a residency guarantee.
	LocalPersonaOpenAIRegion        = "global"
	LocalPersonaOpenAIPurpose       = "persona.reply"
	LocalPersonaOpenAIMaxCostMicros = int64(10000)
	LocalPersonaOpenAIMaxLatency    = 2 * time.Minute
	LocalPersonaOpenAIRetention     = 30 * 24 * time.Hour
)

// NewLocalPersonaOpenAIModelDeployment composes the user's local OpenAI
// authorization with measured model profiles. The caller must obtain profiles
// from the evaluator; this function never manufactures passing evidence.
// Serve selects this helper only for local-dev with a configured MODEL_API_KEY.
// Synthetic demo PUBLIC/INTERNAL data is the complete permitted envelope.
func NewLocalPersonaOpenAIModelDeployment(tenant string, profiles []agentmodel.ModelProfile, material LocalPersonaModelSigningMaterial) (PersonaModelDeployment, error) {
	if !localPersonaOpenAIDemoTenant(tenant) || len(profiles) == 0 {
		return PersonaModelDeployment{}, modelConfigurationError("local OpenAI defaults require measured profiles and a known seeded demo tenant")
	}
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	cfg := PersonaModelDeployment{
		Version:     1,
		Credential:  PersonaModelCredentialDeployment{ID: "local-openai-model", Version: "v1", LeaseTTLSeconds: 60},
		Worker:      PersonaModelWorkerDeployment{ID: "local-persona-worker", Workload: "local-persona-model", Issuer: "local-persona-issuer", KeyID: "local-persona-workload-v1", Cell: "local-dev", IdentityTTLSeconds: 60, RunLeaseTTLSeconds: 60},
		OutputKeyID: "local-persona-output-v1",
	}
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if profile.Identity != identity || !slices.Equal(profile.Regions, []string{LocalPersonaOpenAIRegion}) || profile.MaxCostMicros <= 0 || profile.MaxCostMicros > LocalPersonaOpenAIMaxCostMicros || profile.MaxLatency <= 0 || profile.MaxLatency > LocalPersonaOpenAIMaxLatency || len(profile.DataClasses) == 0 {
			return PersonaModelDeployment{}, modelConfigurationError("local profile exceeds the pinned synthetic-demo processing envelope")
		}
		for _, class := range profile.DataClasses {
			if class != string(trustdlp.ClassPublic) && class != string(trustdlp.ClassInternal) {
				return PersonaModelDeployment{}, modelConfigurationError("local OpenAI permits only synthetic PUBLIC and INTERNAL data")
			}
		}
		if _, duplicate := seen[profile.ID]; duplicate {
			return PersonaModelDeployment{}, modelConfigurationError("local model profile ID is duplicated")
		}
		seen[profile.ID] = struct{}{}
		profile.Regions = slices.Clone(profile.Regions)
		profile.DataClasses = slices.Clone(profile.DataClasses)
		profile.TaskProfileIDs = slices.Clone(profile.TaskProfileIDs)
		cfg.Profiles = append(cfg.Profiles, profile)
		cfg.Terms = append(cfg.Terms, LocalPersonaOpenAIProcessingTerms(profile.ID))
		cfg.Destinations = append(cfg.Destinations, outbound.Destination{Name: profile.ID, TrustBundleRef: "openai-api:https:system-root-cas", Purposes: []string{LocalPersonaOpenAIPurpose}, DataClasses: []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}})
		cfg.Clearances = append(cfg.Clearances, trustdlp.Clearance{Destination: profile.ID, Classes: slices.Clone(classes), Decision: trustdlp.Allow})
		for _, demoTenant := range []string{"harborcare-demo", "ironridge-demo"} {
			cfg.Credential.Scopes = append(cfg.Credential.Scopes, OpenAIModelCredentialScope{TenantID: demoTenant, Region: LocalPersonaOpenAIRegion, Purpose: LocalPersonaOpenAIPurpose, Destination: profile.ID})
		}
	}
	pricing, pricingPublicKey, err := NewLocalPersonaOpenAISignedPricing(material)
	if err != nil {
		return PersonaModelDeployment{}, err
	}
	cfg.Pricing, cfg.PricingPublicKey = *pricing, pricingPublicKey
	if _, _, err := cfg.validate(); err != nil {
		return PersonaModelDeployment{}, err
	}
	return cfg, nil
}

func localPersonaOpenAIDemoTenant(tenant string) bool {
	return tenant == "harborcare-demo" || tenant == "ironridge-demo"
}

// NewLocalPersonaOpenAISignedPricing is also used by the candidate evaluator;
// pricing does not confer model evaluation or production routing approval.
func NewLocalPersonaOpenAISignedPricing(material LocalPersonaModelSigningMaterial) (*agentmodel.PricingSchedule, string, error) {
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion}
	cfg := PersonaModelDeployment{Pricing: agentmodel.PricingSchedule{Version: "openai-standard-2026-09-30", Authority: "local-dev:user-authorized-openai:2026-09-30", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerMillionTokens: 250000, OutputMicrosPerMillionTokens: 2000000, CachedInputMicrosPerMillionTokens: 25000}}}}
	if err := SignLocalPersonaModelPricing(&cfg, material); err != nil {
		return nil, "", err
	}
	pricing, err := agentmodel.NewPricingSchedule(cfg.Pricing)
	return pricing, cfg.PricingPublicKey, err
}

// LocalPersonaOpenAIProcessingTerms declares normal API processing terms.
// The evaluator narrows SourceRules to synthetic-fixture and independently
// verifies every fixture source before a candidate dispatch. These terms
// provide no agent-version evaluation or production router eligibility.
func LocalPersonaOpenAIProcessingTerms(profileID string) agentegress.ProviderTerms {
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	return agentegress.ProviderTerms{
		ModelProfile: profileID, ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, ModelVersion: LocalPersonaOpenAIModelVersion,
		// References name API documentation and the user's local provider
		// choice. They do not assert a separately signed commercial contract.
		ContractRef: "https://developers.openai.com/api/docs/guides/your-data", EgressGrantRef: "local-dev:user-authorized-openai:synthetic-demo", Approved: true, Encryption: true,
		AllowedRegions: []string{LocalPersonaOpenAIRegion}, AllowedClasses: slices.Clone(classes),
		Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionBounded, MaxAge: LocalPersonaOpenAIRetention}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseAllowed,
		SourceRules: []agentegress.ProviderSourceRule{{Class: "persona-profile", Classes: slices.Clone(classes)}, {Class: "persona-invoking-post", Classes: slices.Clone(classes)}, {Class: "persona-thread-context", Classes: slices.Clone(classes)}, {Class: "persona-untrusted-tool-result", Classes: slices.Clone(classes)}, {Class: "persona-model-tool-proposal", Classes: slices.Clone(classes)},
			// Documents an agent's instructions reference go out under the same
			// classes as a document found by the policy search tool.
			{Class: "persona-untrusted-reference-document", Classes: slices.Clone(classes)}, {Class: "persona-reference-document", Classes: slices.Clone(classes)}},
	}
}
