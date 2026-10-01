package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaModelDeploymentPolicyAuthority checks the current durable route and
// actual configured deployment. The getter permits a deployment owner to
// reload signed configuration and withdraw provider eligibility immediately.
type PersonaModelDeploymentPolicyAuthority struct {
	Routes     PersonaModelRoutePolicyReader
	Deployment func(context.Context, values.TenantId) (PersonaModelDeployment, error)
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

type PersonaAgentModelRoutePolicyReader interface {
	CurrentPersonaModelRoutePolicyForAgent(context.Context, uuid.UUID, string, string, int64, int64, string, string, time.Time) (agentstore.PersonaModelRoutePolicy, error)
}

// currentPersonaAgentModelRoute selects the deployment for the admitted exact
// manifest. Legacy readers are accepted only when their returned payload still
// proves the same manifest, never as an ambiguous "latest" deployment.
func currentPersonaAgentModelRoute(ctx context.Context, reader PersonaModelRoutePolicyReader, tenant uuid.UUID, entity string, ref agentmanifest.Reference, agentDigest string, now time.Time) (agentstore.PersonaModelRoutePolicy, error) {
	if exact, ok := reader.(PersonaAgentModelRoutePolicyReader); ok {
		return exact.CurrentPersonaModelRoutePolicyForAgent(ctx, tenant, entity, ref.ID, int64(ref.Version), int64(ref.SchemaVersion), ref.Digest, agentDigest, now)
	}
	stored, err := reader.CurrentPersonaModelRoutePolicy(ctx, tenant, entity, ref.ID, int64(ref.Version), int64(ref.SchemaVersion), ref.Digest, now)
	if err != nil {
		return agentstore.PersonaModelRoutePolicy{}, err
	}
	var route PersonaRunModelRoute
	if err := json.Unmarshal(stored.RoutePayload, &route); err != nil || route.Route.Pin.AgentVersionDigest != agentDigest || route.Route.Task.AgentVersionDigest != agentDigest {
		return agentstore.PersonaModelRoutePolicy{}, ErrAgentModelPolicyUnavailable
	}
	return stored, nil
}

func (a PersonaModelDeploymentPolicyAuthority) CheckAgentPolicyDeployment(ctx context.Context, request agentrun.Request, current agentmodelpolicystore.Current) error {
	return a.checkDeployment(ctx, request, current, agentmanifest.Manifest{})
}

func (a PersonaModelDeploymentPolicyAuthority) CheckAgentPolicyDeploymentForManifest(ctx context.Context, request agentrun.Request, current agentmodelpolicystore.Current, manifest agentmanifest.Manifest) error {
	return a.checkDeployment(ctx, request, current, manifest)
}

func (a PersonaModelDeploymentPolicyAuthority) checkDeployment(ctx context.Context, request agentrun.Request, current agentmodelpolicystore.Current, manifest agentmanifest.Manifest) error {
	if ctx == nil || a.Routes == nil || a.Deployment == nil || a.TenantUUID == nil || a.Now == nil || request.Agent.Digest == "" || request.LegalEntity == "" {
		return ErrAgentModelPolicyUnavailable
	}
	tenant := values.TenantId(request.Source.TenantID)
	ref := current.Record.Reference
	stored, err := currentPersonaAgentModelRoute(ctx, a.Routes, a.TenantUUID(tenant), request.LegalEntity, ref, request.Agent.Digest, a.Now().UTC())
	if err != nil || stored.TenantID != a.TenantUUID(tenant) || stored.LegalEntityID != request.LegalEntity || stored.PolicyID != ref.ID || stored.PolicyVersion != int64(ref.Version) || stored.PolicySchemaVersion != int64(ref.SchemaVersion) || stored.PolicyDigest != ref.Digest || string(stored.PolicyPayload) != string(current.Record.Content) || (stored.EffectiveUntil != nil && request.Deadline.After(*stored.EffectiveUntil)) {
		return ErrAgentModelPolicyUnavailable
	}
	var route struct {
		PersonaRunModelRoute
		ModelDigest string `json:"model_digest"`
	}
	if err := decodeAgentPolicyDocument(stored.RoutePayload, &route); err != nil {
		return err
	}
	cfg, err := a.Deployment(ctx, tenant)
	if err != nil {
		return err
	}
	if _, _, err := cfg.validate(); err != nil {
		return err
	}
	var contract struct {
		Identity                    agentmodel.ModelIdentity  `json:"identity"`
		ProcessingRegion            string                    `json:"processing_region"`
		Purpose                     string                    `json:"purpose"`
		DataClasses                 []string                  `json:"data_classes"`
		SyntheticTenants            []string                  `json:"synthetic_tenants"`
		AllowedTenants              []string                  `json:"allowed_tenants"`
		Budget                      agentmanifest.Budget      `json:"budget"`
		OutputSchemaDigest          string                    `json:"output_schema_digest"`
		ToolSchemaDigest            string                    `json:"tool_schema_digest"`
		RetentionNS                 int64                     `json:"retention_ns"`
		RetentionMode               agentegress.RetentionMode `json:"retention_mode"`
		MaxLatencyNS                int64                     `json:"max_latency_ns"`
		TrainingUse                 agentmodel.ProcessingUse  `json:"training_use"`
		Logging                     agentmodel.ProcessingUse  `json:"logging"`
		InputMicrosPerMillion       int64                     `json:"input_micros_per_million"`
		CachedInputMicrosPerMillion int64                     `json:"cached_input_micros_per_million"`
		OutputMicrosPerMillion      int64                     `json:"output_micros_per_million"`
	}
	if err := json.Unmarshal(current.Record.Content, &contract); err != nil {
		return ErrAgentModelPolicyUnavailable
	}
	if (len(contract.SyntheticTenants) > 0 && !slices.Contains(contract.SyntheticTenants, string(tenant))) || (len(contract.AllowedTenants) > 0 && !slices.Contains(contract.AllowedTenants, string(tenant))) {
		return ErrAgentModelPolicyUnavailable
	}
	selection := route.Route.Pin.Primary
	if contract.ToolSchemaDigest == "" || route.Route.Pin.ToolSchemaDigest != contract.ToolSchemaDigest || route.Route.Task.ToolSchemaDigest != contract.ToolSchemaDigest {
		return ErrAgentModelPolicyUnavailable
	}
	if contract.MaxLatencyNS <= 0 || route.Route.Task.MaxLatency <= 0 || int64(route.Route.Task.MaxLatency) > contract.MaxLatencyNS {
		return ErrAgentModelPolicyUnavailable
	}
	if route.ModelDigest != selection.ProfileDigest || selection.Identity != contract.Identity || route.Route.Pin.AgentVersionDigest != request.Agent.Digest || route.Route.Task.AgentVersionDigest != request.Agent.Digest || route.Route.Task.Region != contract.ProcessingRegion || route.Purpose != contract.Purpose || route.Processing.Residency != contract.ProcessingRegion || route.Processing.TrainingUse != contract.TrainingUse || route.Processing.Logging != contract.Logging || route.Route.Pin.OutputSchemaDigest != contract.OutputSchemaDigest || route.Route.Task.OutputSchemaDigest != contract.OutputSchemaDigest || len(route.Route.Pin.Fallbacks) != 0 || request.Budget.MaxCostMicros > contract.Budget.MaxCostMicros || request.Budget.MaxInputTokens > contract.Budget.MaxInputTokens || request.Budget.MaxOutputTokens > contract.Budget.MaxOutputTokens {
		return ErrAgentModelPolicyUnavailable
	}
	var eligible bool
	for _, profile := range cfg.Profiles {
		if profile.ID == selection.ProfileID && profile.ProfileDigest == selection.ProfileDigest && profile.Identity == selection.Identity && agentmodel.ModelProfileDigest(profile) == selection.ProfileDigest && profile.Evaluation.Passed && profile.Evaluation.AgentVersionDigest == request.Agent.Digest && profile.Evaluation.SuiteDigest != "" && slices.Contains(profile.Regions, route.Route.Task.Region) && slices.Contains(profile.TaskProfileIDs, route.Route.Task.ID) && profile.SemanticsDigest == route.Route.Pin.SemanticsDigest && profile.OutputSchemaDigest == route.Route.Pin.OutputSchemaDigest && profile.ToolSchemaDigest == route.Route.Pin.ToolSchemaDigest && route.Route.Task.MaxLatency <= profile.MaxLatency && route.Route.Task.MaxCostMicros <= profile.MaxCostMicros {
			eligible = true
			if manifest.ID != "" {
				suiteMatches := false
				for _, evaluation := range manifest.EvaluationRefs {
					if evaluation.Digest == profile.Evaluation.SuiteDigest {
						suiteMatches = true
					}
				}
				if !suiteMatches || manifest.OutputSchema.Digest != profile.OutputSchemaDigest {
					eligible = false
				}
			}
			for _, class := range route.Route.Task.DataClasses {
				if !slices.Contains(profile.DataClasses, class) || !slices.Contains(contract.DataClasses, class) {
					eligible = false
				}
			}
		}
	}
	if !eligible {
		return ErrAgentModelPolicyUnavailable
	}
	var termsMatch, credentialMatch, pricingMatch bool
	for _, terms := range cfg.Terms {
		if contract.RetentionMode != "" && terms.Retention.Mode != contract.RetentionMode {
			continue
		}
		if terms.ModelProfile == selection.ProfileID && terms.ProviderID == selection.Identity.ProviderID && terms.ModelID == selection.Identity.ModelID && terms.ModelVersion == selection.Identity.Version && terms.Approved && terms.Encryption && terms.ContractRef != "" && terms.EgressGrantRef != "" && slices.Contains(terms.AllowedRegions, contract.ProcessingRegion) && int64(terms.Retention.MaxAge) == contract.RetentionNS && terms.TrainingUse == contract.TrainingUse && terms.Logging == contract.Logging && route.Processing.Retention == fmt.Sprintf("%s:%d", terms.Retention.Mode, int64(terms.Retention.MaxAge)) && route.Egress.ID == selection.ProfileID && route.Egress.Kind == agentegress.TargetModel && route.Egress.Retention == terms.Retention && slices.Contains(route.Egress.AllowedRegions, contract.ProcessingRegion) {
			termsMatch = true
			for _, class := range []string{string(route.ProfileClass), string(route.InvokerClass), string(route.ThreadClass)} {
				if !slices.Contains(contract.DataClasses, class) {
					termsMatch = false
				}
				classAllowed := false
				for _, allowed := range terms.AllowedClasses {
					if string(allowed) == class {
						classAllowed = true
					}
				}
				if !classAllowed {
					termsMatch = false
				}
			}
		}
	}
	for _, scope := range cfg.Credential.Scopes {
		if scope.TenantID == string(tenant) && scope.Region == contract.ProcessingRegion && scope.Purpose == contract.Purpose && scope.Destination == selection.ProfileID {
			credentialMatch = true
		}
	}
	for _, price := range cfg.Pricing.Entries {
		if price.Identity == selection.Identity && price.InputMicrosPerToken == 0 && price.OutputMicrosPerToken == 0 && price.InputMicrosPerMillionTokens == contract.InputMicrosPerMillion && price.CachedInputMicrosPerMillionTokens == contract.CachedInputMicrosPerMillion && price.OutputMicrosPerMillionTokens == contract.OutputMicrosPerMillion {
			pricingMatch = true
		}
	}
	if !termsMatch || !credentialMatch || !pricingMatch {
		return ErrAgentModelPolicyUnavailable
	}
	return nil
}
