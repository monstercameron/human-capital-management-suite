package application

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// agentModelKeyEnv is the environment variable SchemaFlux itself reads its
// provider key from. The composition never reads the key's value: it only
// asks whether one is configured.
const agentModelKeyEnv = "SCHEMAFLUX_API_KEY"

// ErrAgentModelUnavailable is the typed failure of a model step when no model
// provider is configured and the profile does not install the deterministic
// local-development provider.
var ErrAgentModelUnavailable = errors.New("application: no agent model provider is configured")

// agentModelKind names which provider the composition chose.
type agentModelKind = agentmodel.DefaultProviderKind

const (
	// agentModelFake is SchemaFlux's deterministic in-process provider,
	// installed only under the local-dev profile with no key configured.
	agentModelFake = agentmodel.DefaultProviderLocalDevFake
	// agentModelConfigured leaves SchemaFlux's default client alone: a real key
	// is configured and SchemaFlux builds its own provider.
	agentModelConfigured = agentmodel.DefaultProviderConfigured
	// agentModelUnavailable means model steps fail with ErrAgentModelUnavailable.
	agentModelUnavailable = agentmodel.DefaultProviderUnavailable
)

// agentModel is the outcome of provider selection.
type agentModel struct {
	Kind     agentModelKind
	Typed    *AgentTypedModelDispatchBinding
	Observer agentmodel.DefaultProviderObserver
}

// Available reports whether a model step can run.
func (m agentModel) Available() bool {
	if m.Typed != nil {
		return m.Typed.Available()
	}
	return m.Kind == agentModelFake || m.Kind == agentModelConfigured
}

// selectAgentModel picks the model provider. A configured key always wins and
// leaves SchemaFlux's default client untouched. Without one, only the
// local-dev profile installs the deterministic fake; every other profile
// installs nothing, so a production process can never answer from a stub.
func selectAgentModel(profile string, env func(string) string) agentModel {
	selection := agentmodel.SelectDefaultProvider(profile == ServeProfileLocalDev, env != nil && strings.TrimSpace(env(agentModelKeyEnv)) != "")
	return agentModel{Kind: selection.Kind, Observer: selection.Observer}
}

func initializeConfiguredAgentModel() error {
	if err := agentmodel.InitializeConfiguredDefaultProvider(); err != nil {
		return fmt.Errorf("application: initialize configured agent model: %w", err)
	}
	return nil
}
