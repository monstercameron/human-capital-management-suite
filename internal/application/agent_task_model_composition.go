package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

const localAgentTaskModelDeploymentPath = ".artifacts/lanes/agent-dev/task-model-deployment.json"

// composeServedAgentTaskModel connects the private task runtime to the qualified
// SchemaFlux/OpenAI deployment. Persona-only profiles do not enable task starts.
func composeServedAgentTaskModel(ctx context.Context, cfg ServeConfig, runtime *agentRuntime, env func(string) string, now func() time.Time) error {
	if runtime == nil || runtime.TypedModels == nil {
		return nil
	}
	if cfg.Profile == ServeProfileLocalDev && cfg.AgentModelConfigFile == "" {
		path := filepath.FromSlash(localAgentTaskModelDeploymentPath)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.FromSlash(localPersonaModelSigningPath))
		if err != nil {
			return err
		}
		cfg.AgentModelConfigFile = path
		cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed = material.OutputSeed, material.WorkloadSeed
	}
	cfg, deployment, apiKey, configured, err := loadServedAgentModelConfiguration(ctx, cfg, env)
	if err != nil || !configured {
		return err
	}
	qualified := false
	for _, profile := range deployment.Profiles {
		if profile.Evaluation.Passed && profile.Evaluation.AgentVersionDigest == agentVersion {
			qualified = true
		}
	}
	if !qualified {
		return nil
	}
	if cfg.CellID == "" || deployment.Worker.Cell != cfg.CellID || runtime.Platform == nil || runtime.Budget == nil || runtime.Audit == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	now = personaServeClock(now)
	source, err := NewAgentTaskModelRequestSource(AgentTaskModelRequestSourceConfig{Platform: runtime.Platform, Deployment: deployment,
		Workload: deployment.Worker.Workload, LeaseTTL: time.Duration(deployment.Credential.LeaseTTLSeconds) * time.Second, Now: now, Audit: runtime.Audit})
	if err != nil {
		return err
	}
	model, _, err := ComposePersonaRuntimeTypedModel(deployment, apiKey, cfg.PersonaOutputSigningSeed, cfg.PersonaWorkloadSigningSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: runtime.Budget}, Routes: source, Sources: source, Resources: runtime.Resources, Now: now})
	if err != nil {
		return err
	}
	if err := source.BindCredentials(model.Leases); err != nil {
		return err
	}
	dispatcher, err := NewOpenAIPlatformTypedModelDispatcher(model.Gateway, source)
	if err != nil {
		return err
	}
	return BindAgentRuntimeTypedModel(runtime, dispatcher)
}
