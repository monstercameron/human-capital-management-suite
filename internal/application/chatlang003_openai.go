package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// The "openai" translation engine (owner decision 2026-10-02: language, speech
// and writing style may use OpenAI through SchemaFlux). It is the same
// chatlang.Engine port as the governed text route and the fixture, behind the
// same gateway, the same MODEL_API_KEY, the same lease, egress, budget and
// pricing checks; what differs is the answer. Language detection and
// translation are one SchemaFlux typed operation (agentmodel.ChatlangTranslation:
// the language found, the text, the model's verdict on the meaning), so a
// message whose language is not recorded is detected in the call that
// translates it and a text already in the target language comes back unchanged.
//
// Everything before the call stays where it was: the producer protects
// mentions, links, code, numbers and glossary terms, the authority checks the
// workspace and channel settings (including "Never use an outside service")
// and the monthly limit before the engine is asked, and a failure leaves the
// message as written.
const (
	// ChatlangEngineOpenAI is the value of EnvChatTranslationEngine that selects
	// this engine.
	ChatlangEngineOpenAI = "openai"
	// EnvChatTranslationModel names the model the engine asks for. The model is
	// only a name: the deployment's qualified profile must name the same one,
	// and a call is never served by another model.
	EnvChatTranslationModel = "HCMNEXT_CHAT_TRANSLATION_MODEL"
	// EnvChatTranslationLiveSmoke gates the owner's one live translation: with
	// it set to "1", TestChatlangLiveSmoke makes a single real call. No other
	// test reads it, and no test sets it.
	EnvChatTranslationLiveSmoke = "HCMNEXT_LIVE_CHAT_TRANSLATION"
)

// ChatlangModelFromEnv is the configured translation model, or the default.
func ChatlangModelFromEnv(env func(string) string) string {
	if env == nil {
		return agentmodel.ChatlangDefaultModel
	}
	return agentmodel.ChatlangModelID(env(EnvChatTranslationModel))
}

// NewChatlangStructuredDeployment derives the approval document for the
// structured route, as NewChatlangDeployment does for the text route: one
// profile qualified for ChatlangTaskProfileID whose output schema is the
// SchemaFlux-derived schema of agentmodel.ChatlangTranslation.
func NewChatlangStructuredDeployment(base PersonaModelDeployment, evaluation agentmodel.ModelEvaluation, maxLatency time.Duration, maxCostMicros int64) (PersonaModelDeployment, error) {
	digest, err := agentmodel.ChatlangTranslationSchemaDigest()
	if err != nil {
		return PersonaModelDeployment{}, fmt.Errorf("%w: %v", errChatlangBinding, err)
	}
	return newChatlangDeployment(base, evaluation, maxLatency, maxCostMicros, digest)
}

// NewChatlangStructuredBinding selects the one profile a deployment qualified
// for structured translation and refuses one whose model is not the configured
// one: the named model is the model called.
func NewChatlangStructuredBinding(dep PersonaModelDeployment, leases *ModelLeaseSource, ledger *agentbudget.Ledger, now func() time.Time, model string) (*ChatlangDeploymentBinding, error) {
	if leases == nil || ledger == nil || now == nil {
		return nil, fmt.Errorf("%w: leases, budget ledger and clock are required", errChatlangBinding)
	}
	digest, err := agentmodel.ChatlangTranslationSchemaDigest()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errChatlangBinding, err)
	}
	q, err := chatlangQualifyFor(dep, digest)
	if err != nil {
		return nil, err
	}
	if want := agentmodel.ChatlangModelID(model); q.profile.Identity.ModelID != want {
		return nil, fmt.Errorf("%w: the qualified profile names model %q but %q is configured; no other model is substituted", errChatlangBinding, q.profile.Identity.ModelID, want)
	}
	return &ChatlangDeploymentBinding{Profile: q.profile, Terms: q.terms, Leases: leases, Workload: dep.Worker.Workload, Ledger: ledger, Region: q.region, Limits: q.limits, Now: now, Structured: true}, nil
}

// ChatlangStructuredEngine is the "openai" engine: the typed translation
// operation reached through the governed model gateway.
type ChatlangStructuredEngine struct {
	Gateway ChatlangGateway
	Binding ChatlangGatewayBinding
}

// Ready reports whether a qualified model is bound.
func (e ChatlangStructuredEngine) Ready() bool {
	return ChatlangGatewayEngine{Gateway: e.Gateway, Binding: e.Binding}.Ready()
}

// Translate implements chatlang.Engine.
func (e ChatlangStructuredEngine) Translate(ctx context.Context, r chatlang.Request) (chatlang.Response, error) {
	if !e.Ready() || strings.TrimSpace(r.Tenant) == "" {
		return chatlang.Response{}, chatlang.ErrUnavailable
	}
	req, err := e.Binding.Bind(ctx, r.Tenant)
	if err != nil {
		if errors.Is(err, agentbudget.ErrPaused) {
			return chatlang.Response{}, chatlang.ErrBudget
		}
		return chatlang.Response{}, err
	}
	if req.TenantID != r.Tenant || req.Dispatch.Outbound.Tenant != r.Tenant || req.Route.Task.ID != ChatlangTaskProfileID || req.Route.Pin.TaskProfileID != ChatlangTaskProfileID || req.Dispatch.Model.Output.Mode != agentmodel.OutputSchema {
		return chatlang.Response{}, chatlang.ErrUnavailable
	}
	instruction, data := chatlang.StructuredPrompt(r)
	req.Dispatch.Model.TaskProfile = ChatlangTaskProfileID
	req.Dispatch.Model.Messages = []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: instruction}, {Role: agentmodel.RoleUser, Content: data}}
	req.Dispatch.Model.Tools = nil
	req.Dispatch.Model.RequiredFeatures = []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}
	provenance := []string{"chat-translation:" + req.Route.TraceID}
	req.Dispatch.Outbound.DeclaredFields = []string{"model.message.0", "model.message.1"}
	req.Dispatch.Outbound.Fields = []agentegress.Field{
		{Name: "model.message.0", Value: instruction, Class: trustdlp.ClassInternal, Taint: []string{string(agentsecurity.TaintCanonical)}, Provenance: provenance},
		{Name: "model.message.1", Value: data, Class: trustdlp.ClassInternal, Taint: []string{string(agentsecurity.TaintHuman)}, Provenance: provenance},
	}
	req.Dispatch.FieldSources = map[string]string{"model.message.0": chatlangSourceInstruction, "model.message.1": chatlangSourceText}
	result, err := e.Gateway.Dispatch(context.WithValue(ctx, chatlangEvidenceKey{}, req), req)
	if err != nil {
		if errors.Is(err, agentbudget.ErrPaused) {
			return chatlang.Response{}, chatlang.ErrBudget
		}
		return chatlang.Response{}, err
	}
	model := result.Dispatch.Model
	translation, err := agentmodel.DecodeChatlangTranslation(model)
	if err != nil {
		return chatlang.Response{}, chatlang.ErrUnavailable
	}
	detected := chatrender.Language(translation.SourceLanguage)
	if !chatrender.Supported(detected) {
		// A language the product does not offer is not one it can name; it is
		// treated as having none of its own.
		detected = "und"
	}
	return chatlang.Response{Text: strings.TrimSpace(translation.Text), Provider: model.Provider.ProviderID, Model: model.Provider.ModelID,
		InputTokens: model.Usage.InputTokens, OutputTokens: model.Usage.OutputTokens, CostMicros: model.Usage.CostMicros,
		DetectedSource: detected, MeaningChecked: true, MeaningPreserved: translation.MeaningPreserved, InstructionDigest: chatlang.StructuredInstructionDigest()}, nil
}

var _ chatlang.Engine = ChatlangStructuredEngine{}

// chatlangOpenAIModel builds the gateway the structured engine dispatches
// through: the same authority, leases, egress policy, pricing and budget as the
// persona and text routes (composePersonaRuntimeModel), with SchemaFlux's typed
// operation for agentmodel.ChatlangTranslation beneath the priced provider. The
// provider adapter is HCM's own, so SchemaFlux never builds the OpenAI request
// itself and its temperature rule (gpt-5 prefix only) and over-long prompt
// cache key are never sent.
func chatlangOpenAIModel(cfg PersonaModelDeployment, apiKey string, deps PersonaModelDeploymentDependencies) (*AgentModelGateway, *ModelLeaseSource, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, nil, modelConfigurationError("MODEL_API_KEY is required")
	}
	if isNilPersonaOutputPort(deps.Budget) || isNilPersonaOutputPort(deps.Routes) || isNilPersonaOutputPort(deps.Sources) || deps.Now == nil {
		return nil, nil, modelConfigurationError("durable budget, route recorder, source classifier and clock are required")
	}
	pricing, egress, err := cfg.validate()
	if err != nil {
		return nil, nil, err
	}
	authority, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: cfg.Credential.ID, Version: cfg.Credential.Version, Workload: cfg.Worker.Workload, Scopes: cfg.Credential.Scopes, MaxTTL: time.Duration(cfg.Credential.LeaseTTLSeconds) * time.Second, Now: deps.Now})
	if err != nil {
		return nil, nil, err
	}
	manager, err := lease.NewManager(authority, deps.Now)
	if err != nil {
		return nil, nil, err
	}
	leases, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": authority}, Leases: manager, MaxTTL: time.Duration(cfg.Credential.LeaseTTLSeconds) * time.Second})
	if err != nil {
		return nil, nil, err
	}
	gateway, err := NewOpenAISchemaFluxTypedAgentModelGateway[agentmodel.ChatlangTranslation](OpenAIAgentModelGatewayConfig{APIKey: apiKey, BaseURL: cfg.BaseURL, Profiles: cfg.Profiles, Terms: cfg.Terms, Pricing: pricing, Budget: deps.Budget, Routes: deps.Routes, Egress: egress, Leases: manager, Sources: deps.Sources, Resources: deps.Resources, LeaseBindings: leases})
	if err != nil {
		return nil, nil, err
	}
	return gateway, leases, nil
}

// Detects reports that the engine finds the language itself.
func (ChatlangStructuredEngine) Detects() bool { return true }
