package application

import (
	"fmt"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	modelopenai "github.com/monstercameron/human-capital-management-suite/internal/agentmodel/openai"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// OpenAIAgentModelGatewayConfig contains approved deployment policy and live
// provider dependencies. APIKey is consumed only by the private provider adapter;
// it is never put in a request, route record, lease handle or model prompt.
type OpenAIAgentModelGatewayConfig struct {
	APIKey        string
	BaseURL       string
	HTTPClient    *http.Client
	Profiles      []agentmodel.ModelProfile
	Terms         []agentegress.ProviderTerms
	Pricing       *agentmodel.PricingSchedule
	Budget        agentmodel.Budget
	Routes        agentmodel.RouteRecorder
	Egress        *agentegress.Evaluator
	Leases        *lease.Manager
	Sources       agentegress.SourceClassificationVerifier
	Resources     AgentModelResourceAdmission
	LeaseBindings *ModelLeaseSource
}

// NewOpenAIAgentModelGateway composes the real OpenAI Responses adapter behind
// SchemaFlux's typed generator, immutable routing, approved egress terms,
// single-use credential leases and authoritative pricing. It supplies no model,
// evaluation, data class, processing or budget defaults.
func NewOpenAIAgentModelGateway(cfg OpenAIAgentModelGatewayConfig) (*AgentModelGateway, error) {
	return newOpenAIAgentModelGateway(cfg, true)
}

// NewOpenAITypedAgentModelGateway is used only beneath agentmodel.Generate[T],
// whose Go-derived schema is carried in the owned request. It applies the same
// routing, credential, source, egress, resource and cost gates as persona calls.
func NewOpenAITypedAgentModelGateway(cfg OpenAIAgentModelGatewayConfig) (*AgentModelGateway, error) {
	return newOpenAIAgentModelGateway(cfg, false)
}

// NewOpenAISchemaFluxTypedAgentModelGateway serves native runs whose admitted
// output contract is an owned Go business type, without an outer Generate call.
// Supply only profiles qualified for that exact type's output schema.
func NewOpenAISchemaFluxTypedAgentModelGateway[T any](cfg OpenAIAgentModelGatewayConfig) (*AgentModelGateway, error) {
	return newOpenAIAgentModelGatewayWithAdapter(cfg, func(provider agentmodel.ModelAdapter, selection agentmodel.ModelSelection, pricing *agentmodel.PricingSchedule) (agentmodel.ModelAdapter, error) {
		return agentmodel.NewSchemaFluxTypedAdapter[T](provider, selection, pricing)
	})
}

func newOpenAIAgentModelGateway(cfg OpenAIAgentModelGatewayConfig, personaEnvelope bool) (*AgentModelGateway, error) {
	return newOpenAIAgentModelGatewayWithAdapter(cfg, func(provider agentmodel.ModelAdapter, selection agentmodel.ModelSelection, pricing *agentmodel.PricingSchedule) (agentmodel.ModelAdapter, error) {
		if personaEnvelope {
			return agentmodel.NewSchemaFluxAdapter(provider, selection, pricing)
		}
		return agentmodel.NewPricedAdapter(provider, selection, pricing)
	})
}

func newOpenAIAgentModelGatewayWithAdapter(cfg OpenAIAgentModelGatewayConfig, buildAdapter func(agentmodel.ModelAdapter, agentmodel.ModelSelection, *agentmodel.PricingSchedule) (agentmodel.ModelAdapter, error)) (*AgentModelGateway, error) {
	if cfg.Pricing == nil || cfg.Budget == nil || cfg.Leases == nil || cfg.LeaseBindings == nil || cfg.Routes == nil || cfg.Egress == nil || cfg.Sources == nil || len(cfg.Profiles) == 0 || len(cfg.Terms) != len(cfg.Profiles) {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	router, err := agentmodel.NewRouter(cfg.Profiles, cfg.Routes)
	if err != nil {
		return nil, fmt.Errorf("application: OpenAI model catalog: %w", err)
	}
	adapters := make(map[agentmodel.ModelSelection]agentmodel.ModelAdapter, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		if profile.Identity.ProviderID != "openai" {
			return nil, ErrAgentModelGatewayNotConfigured
		}
		selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
		matched := false
		for _, term := range cfg.Terms {
			if term.ModelProfile == profile.ID && term.ProviderID == profile.Identity.ProviderID && term.ModelID == profile.Identity.ModelID && term.ModelVersion == profile.Identity.Version {
				matched = true
			}
		}
		if !matched {
			return nil, ErrAgentModelGatewayNotConfigured
		}
		provider, err := modelopenai.New(modelopenai.Config{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, HTTPClient: cfg.HTTPClient, ModelProfile: profile.ID, Identity: profile.Identity, PreflightInputTokens: true})
		if err != nil {
			return nil, fmt.Errorf("application: OpenAI provider configuration: %w", err)
		}
		adapter, err := buildAdapter(provider, selection, cfg.Pricing)
		if err != nil {
			return nil, fmt.Errorf("application: OpenAI typed generation: %w", err)
		}
		adapters[selection] = adapter
	}
	dispatcher, err := agentegress.NewProviderDispatcher(cfg.Egress, cfg.Leases, cfg.Sources, cfg.Terms)
	if err != nil {
		return nil, fmt.Errorf("application: OpenAI egress policy: %w", err)
	}
	return NewAgentModelGateway(AgentModelGatewayConfig{Router: router, Egress: dispatcher, Adapters: adapters, Pricing: cfg.Pricing, Budget: cfg.Budget, Resources: cfg.Resources, LeaseBindings: cfg.LeaseBindings})
}
