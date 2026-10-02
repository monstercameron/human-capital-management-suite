package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// EnvChatTranslationModelConfigFile names the approval document that qualifies
// a model for the translation task (the same document shape as the persona and
// writing-style deployments, derived by NewChatlangDeployment). It is the only
// way translation is given a model outside the local development profile.
const EnvChatTranslationModelConfigFile = "HCMNEXT_CHAT_TRANSLATION_MODEL_CONFIG_FILE"

// EnvChatTranslationEngine selects the deterministic test engine in the local
// development profile ("fixture"): it translates nothing, it labels text with
// the target language, costs nothing and calls no one, so the whole flow can be
// driven without a model. It is refused in every other profile.
const EnvChatTranslationEngine = "HCMNEXT_CHAT_TRANSLATION_ENGINE"

// localChatlangModelDeploymentPath is read, when present, by the local
// development profile only, beside the persona and writing-style deployments.
const localChatlangModelDeploymentPath = ".artifacts/lanes/agent-dev/chat-translation-deployment.json"

type chatlangServeInput struct {
	Config  ServeConfig
	Runtime *agentRuntime
	Chat    composedChat
	Env     func(string) string
	Now     func() time.Time
	Tenants []string
}

// composeServedChatlang puts the translation worker together over the composed
// chat, its filters and an engine, and binds the engine to the governance so
// the administrator's settings take effect. It returns a nil runtime and a
// plain reason when no engine is provisioned, so translation is absent rather
// than broken. A document that exists but is invalid is an error to log, never
// a silent default. Workspaces stay off until an administrator turns
// translation on: this composes the ability, never the setting.
func composeServedChatlang(ctx context.Context, in chatlangServeInput) (*ChatlangRuntime, string, error) {
	if ctx == nil || in.Chat.renderings == nil || in.Chat.renderings.Languages == nil || in.Chat.store == nil || in.Chat.filters == nil {
		return nil, "chat, its reader selection or its content filters are not composed", nil
	}
	if in.Env == nil {
		in.Env = os.Getenv
	}
	local := in.Config.Profile == ServeProfileLocalDev
	now := personaServeClock(in.Now)
	var engine chatlang.Engine
	info := ChatlangEngineInfo{Ready: true, External: true}
	switch {
	case in.Env(EnvChatTranslationEngine) == "fixture":
		if !local {
			return nil, "the fixture translation engine is only available in the local development profile", nil
		}
		// External: the same governance applies as to a real engine, so a
		// channel barred from external engines is barred here too.
		engine, info.Name = &chatlang.FixtureEngine{}, "fixture (local development)"
	case in.Env(EnvChatTranslationEngine) == ChatlangEngineOpenAI:
		model, name, reason, err := chatlangGatewayEngine(ctx, in, local, now, true)
		if err != nil || reason != "" {
			return nil, reason, err
		}
		engine, info.Name = model, name
	default:
		model, name, reason, err := chatlangGatewayEngine(ctx, in, local, now, false)
		if err != nil || reason != "" {
			return nil, reason, err
		}
		engine, info.Name = model, name
	}
	runtime, err := NewChatlangRuntime(in.Chat.store, in.Chat.renderings.Languages, engine, ChatlangFilterScreen{Filters: in.Chat.filters}, now, in.Tenants)
	if err != nil {
		return nil, "", err
	}
	in.Chat.renderings.Languages.BindEngine(info)
	return runtime, "", nil
}

// chatlangGatewayEngine builds the governed model engine from a qualified
// deployment, as composeServedChattone does for writing styles.
func chatlangGatewayEngine(ctx context.Context, in chatlangServeInput, local bool, now func() time.Time, structured bool) (chatlang.Engine, string, string, error) {
	cfg := in.Config
	path := in.Env(EnvChatTranslationModelConfigFile)
	if path == "" && local {
		candidate := filepath.FromSlash(localChatlangModelDeploymentPath)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, "", "", fmt.Errorf("chat translation deployment cannot be read: %w", err)
		}
	}
	if path == "" {
		return nil, "", "no model is qualified for chat translation (set " + EnvChatTranslationModelConfigFile + ")", nil
	}
	if in.Runtime == nil || isNilPersonaOutputPort(in.Runtime.Audit) {
		return nil, "", "the agent platform's audit chain is not composed", nil
	}
	if in.Env("MODEL_API_KEY") == "" {
		return nil, "", "MODEL_API_KEY is not set", nil
	}
	cfg.AgentModelConfigFile = path
	if local {
		material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
		if err != nil {
			return nil, "", "", err
		}
		cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed = material.OutputSeed, material.WorkloadSeed
	}
	cfg, deployment, apiKey, configured, err := loadServedAgentModelConfiguration(ctx, cfg, in.Env)
	if err != nil {
		return nil, "", "", err
	}
	if !configured {
		return nil, "", "the chat translation deployment is not configured", nil
	}
	if local {
		deployment.Worker.Cell = cfg.CellID
	}
	if cfg.CellID == "" || deployment.Worker.Cell != cfg.CellID {
		return nil, "", "", fmt.Errorf("%w: chat translation worker cell does not match the serving cell", ErrPersonaModelConfiguration)
	}
	// A dedicated ledger: translation volume is far above an agent's, and an
	// agent's ceilings must not be spent by it. What a workspace may spend is
	// enforced from the persisted usage lines, not from this ledger.
	ledger, err := agentbudget.New(ChatlangBudgetPolicy())
	if err != nil {
		return nil, "", "", err
	}
	evidence := &ChatlangModelEvidence{Audit: in.Runtime.Audit, Now: now}
	var resources AgentModelResourceAdmission
	if in.Runtime.Resources != nil {
		resources = personaRunModelResources{runtime: in.Runtime.Resources}
	}
	if structured {
		// The "openai" engine: the same deployment document, key, leases, egress and
		// budget, with the typed operation beneath. A deployment qualified for the
		// text route is not eligible for it.
		gateway, leases, err := chatlangOpenAIModel(deployment, apiKey, PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Resources: resources, Now: now})
		if err != nil {
			return nil, "", "", err
		}
		binding, err := NewChatlangStructuredBinding(deployment, leases, ledger, now, ChatlangModelFromEnv(in.Env))
		if err != nil {
			return nil, "", err.Error(), nil
		}
		return ChatlangStructuredEngine{Gateway: gateway, Binding: binding}, "OpenAI " + binding.Profile.Identity.ModelID + " through SchemaFlux", "", nil
	}
	model, _, err := ComposePersonaRuntimeTypedModel(deployment, apiKey, cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Resources: resources, Now: now})
	if err != nil {
		return nil, "", "", err
	}
	binding, err := NewChatlangDeploymentBinding(deployment, model.Leases, ledger, now)
	if err != nil {
		// A deployment that does not qualify a model for this task is the
		// ordinary "not provisioned" state, stated plainly.
		return nil, "", err.Error(), nil
	}
	return ChatlangGatewayEngine{Gateway: model.Gateway, Binding: binding}, "governed model route", "", nil
}

// chatlangBackgroundWorkload runs the translation jobs of the served tenants
// until shutdown.
func chatlangBackgroundWorkload(runtime *ChatlangRuntime) *bootstrap.Workload {
	if runtime == nil || len(runtime.Tenants) == 0 {
		return nil
	}
	return &bootstrap.Workload{Name: "chat-translation", Run: func(ctx context.Context) error {
		_ = runtime.Run(ctx)
		return nil
	}}
}

// logChatlangComposition states, once at start-up, whether translation is
// composed and, if not, why.
func logChatlangComposition(logger interface {
	Info(string, ...any)
	Error(string, ...any)
}, runtime *ChatlangRuntime, reason string, err error) {
	if isNilPersonaOutputPort(logger) {
		return
	}
	switch {
	case err != nil:
		logger.Error("hcmnext.chat_translation_unavailable", "error", err.Error())
	case runtime == nil:
		logger.Info("hcmnext.chat_translation_unavailable", "reason", reason)
	default:
		logger.Info("hcmnext.chat_translation_ready")
	}
}
