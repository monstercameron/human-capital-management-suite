package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// EnvChatWritingStyleModelConfigFile names the approval document that
// qualifies a model for the writing-style task (the same document shape as the
// persona and private-task deployments). It is the only way the writing-style
// service is given a model outside the local development profile.
const EnvChatWritingStyleModelConfigFile = "HCMNEXT_CHAT_WRITING_STYLE_MODEL_CONFIG_FILE"

// localChattoneModelDeploymentPath is read, when present, by the local
// development profile only, beside the persona and private-task deployments.
const localChattoneModelDeploymentPath = ".artifacts/lanes/agent-dev/chat-writing-style-deployment.json"

type chattoneServeInput struct {
	Config  ServeConfig
	Runtime *agentRuntime
	Chat    composedChat
	Facts   ChatAuthorityFacts
	Env     func(string) string
	Now     func() time.Time
	// DailyOperations is the per-person daily ceiling on model calls (rewrites);
	// zero selects chattoneDailyOperations.
	DailyOperations int
}

// composeServedChattone puts the writing-style service together over the
// composed chat, its content filters and reader selection, and the governed
// model gateway. It returns a nil service and a plain reason when a qualified
// model is not provisioned, so the controls are absent rather than broken. A
// document that exists but is invalid is an error to log, never a silent
// default.
func composeServedChattone(ctx context.Context, in chattoneServeInput) (*ChattoneService, string, error) {
	if ctx == nil || in.Chat.service == nil || in.Chat.renderings == nil || in.Chat.filters == nil {
		return nil, "chat, its reader selection or its content filters are not composed", nil
	}
	if in.Env == nil {
		in.Env = os.Getenv
	}
	cfg := in.Config
	path := in.Env(EnvChatWritingStyleModelConfigFile)
	local := cfg.Profile == ServeProfileLocalDev
	if path == "" && local {
		candidate := filepath.FromSlash(localChattoneModelDeploymentPath)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("chat writing style deployment cannot be read: %w", err)
		}
	}
	if path == "" {
		return nil, "no model is qualified for chat writing styles (set " + EnvChatWritingStyleModelConfigFile + ")", nil
	}
	if in.Runtime == nil || in.Runtime.Budget == nil || isNilPersonaOutputPort(in.Runtime.Audit) {
		return nil, "the agent platform's budget ledger and audit chain are not composed", nil
	}
	if in.Env("MODEL_API_KEY") == "" {
		return nil, "MODEL_API_KEY is not set", nil
	}
	cfg.AgentModelConfigFile = path
	if local {
		material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
		if err != nil {
			return nil, "", err
		}
		cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed = material.OutputSeed, material.WorkloadSeed
	}
	cfg, deployment, apiKey, configured, err := loadServedAgentModelConfiguration(ctx, cfg, in.Env)
	if err != nil {
		return nil, "", err
	}
	if !configured {
		return nil, "the chat writing style deployment is not configured", nil
	}
	if local {
		// As for the persona deployment, the local profile serves its own cell.
		deployment.Worker.Cell = cfg.CellID
	}
	if cfg.CellID == "" || deployment.Worker.Cell != cfg.CellID {
		return nil, "", fmt.Errorf("%w: chat writing style worker cell does not match the serving cell", ErrPersonaModelConfiguration)
	}
	now := personaServeClock(in.Now)
	evidence := &ChattoneModelEvidence{Audit: in.Runtime.Audit, Now: now}
	var resourcesPort AgentModelResourceAdmission
	if in.Runtime.Resources != nil {
		resourcesPort = personaRunModelResources{runtime: in.Runtime.Resources}
	}
	model, _, err := ComposePersonaRuntimeTypedModel(deployment, apiKey, cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: in.Runtime.Budget}, Routes: evidence, Sources: evidence, Resources: resourcesPort, Now: now})
	if err != nil {
		return nil, "", err
	}
	binding, err := NewChattoneDeploymentBinding(deployment, model.Leases, in.Runtime.Budget, now)
	if err != nil {
		// A deployment that does not qualify a model for this task is the
		// ordinary "not provisioned" state, stated plainly.
		return nil, err.Error(), nil
	}
	cfgService := ChattoneServiceConfig{
		Model:           ChattoneGatewayModel{Gateway: model.Gateway, Binding: binding},
		Policy:          ChattoneContentPolicy{Filters: in.Chat.filters, Identity: chatFilterIdentity{facts: in.Facts, now: now}, Conversations: in.Chat.service},
		Conversations:   ChattoneChatSource{Chat: in.Chat.service, Renderings: in.Chat.renderings, Status: in.Chat.status, Now: now},
		Authority:       ChattoneChatAuthority{Chat: &PersonaChatSurface{Chat: in.Chat.service, Now: now}},
		Administration:  ChattoneRoleAdministration{Facts: in.Facts, Now: now},
		DailyOperations: in.DailyOperations,
		Now:             now,
	}
	// With the chat store composed, the choice a workspace's administrator makes
	// and every person's daily count live in it, so both survive a restart.
	if in.Chat.store != nil {
		limit := in.DailyOperations
		if limit <= 0 {
			limit = chattoneDailyOperations
		}
		cfgService.Settings = in.Chat.store
		cfgService.Ledger = ChattoneDurableLedger{Store: in.Chat.store, Limit: limit}
	}
	service, err := NewChattoneService(cfgService)
	if err != nil {
		return nil, "", err
	}
	// The development tenant is the only workspace the composition root turns
	// on. Every other workspace opts in through its administrator.
	if local && slices.Contains(cfg.ServedTenants(), localAgentDemoTenant) {
		if err := ChattoneEnableTenant(service.Rewrite.Registry, localAgentDemoTenant); err != nil {
			return nil, "", err
		}
	}
	return service, "", nil
}

// logChattoneComposition states, once at start-up, whether the writing-style
// controls are served and, if not, why.
func logChattoneComposition(logger interface {
	Info(string, ...any)
	Error(string, ...any)
}, service *ChattoneService, reason string, err error) {
	if isNilPersonaOutputPort(logger) {
		return
	}
	switch {
	case err != nil:
		logger.Error("hcmnext.chat_writing_style_unavailable", "error", err.Error())
	case service == nil:
		logger.Info("hcmnext.chat_writing_style_unavailable", "reason", reason)
	default:
		logger.Info("hcmnext.chat_writing_style_ready")
	}
}
