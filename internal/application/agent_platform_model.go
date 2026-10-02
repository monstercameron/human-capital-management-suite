package application

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/schemaflux"
	"github.com/monstercameron/schemaflux/schemafluxtest"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
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
type agentModelKind string

const (
	// agentModelFake is SchemaFlux's deterministic in-process provider,
	// installed only under the local-dev profile with no key configured.
	agentModelFake agentModelKind = "local-dev-deterministic-fake"
	// agentModelConfigured leaves SchemaFlux's default client alone: a real key
	// is configured and SchemaFlux builds its own provider.
	agentModelConfigured agentModelKind = "schemaflux-configured"
	// agentModelUnavailable means model steps fail with ErrAgentModelUnavailable.
	agentModelUnavailable agentModelKind = "unavailable"
)

// agentModel is the outcome of provider selection.
type agentModel struct {
	Kind  agentModelKind
	Typed *AgentTypedModelDispatchBinding
	// Fake is set only for agentModelFake; tests read its call count.
	Fake *schemafluxtest.Provider
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
	if env != nil && strings.TrimSpace(env(agentModelKeyEnv)) != "" {
		return agentModel{Kind: agentModelConfigured}
	}
	if profile != ServeProfileLocalDev {
		return agentModel{Kind: agentModelUnavailable}
	}
	fake := schemafluxtest.New().ReplyFunc(agentFakeReply)
	// The key below is a placeholder: the provider answers in process and
	// nothing is sent anywhere.
	schemaflux.SetDefaultClient(schemaflux.NewClient("local-dev-fake-provider-not-a-key").WithProviderInstance(fake))
	return agentModel{Kind: agentModelFake, Fake: fake}
}

func initializeConfiguredAgentModel() error {
	if err := schemaflux.InitWithEnv(); err != nil {
		return fmt.Errorf("application: initialize configured agent model: %w", err)
	}
	return nil
}

// agentFakeReply answers a model call deterministically from the request. It
// replies with the ModelOutput shape for a summary step and with the
// quarantine wire shape for an extraction, so both model entry points of the
// platform work under local development.
func agentFakeReply(_ int, req schemaflux.CompletionRequest) (string, error) {
	prompt := req.UserPrompt
	if strings.Contains(prompt, "Extract only the declared fields") {
		return agentFakeExtraction(prompt)
	}
	goal := ""
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "Goal: ") {
			goal = strings.TrimSpace(strings.TrimPrefix(line, "Goal: "))
			break
		}
	}
	text := goal
	if exact, ok := strings.CutPrefix(text, "Reply with exactly:"); ok {
		text = strings.TrimSpace(exact)
	}
	if text == "" {
		text = "No answer was produced."
	}
	encoded, err := json.Marshal(agentsystem.ModelOutput{Text: text, Citations: []string{}})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// agentFakeExtraction returns one zero value per declared field, with the
// schema identity echoed back, so the quarantined extractor's typed contract
// is met without inventing content.
func agentFakeExtraction(prompt string) (string, error) {
	_, after, found := strings.Cut(prompt, "Schema: ")
	if !found {
		return "", fmt.Errorf("agent fake model: extraction prompt carries no schema")
	}
	line, _, _ := strings.Cut(after, "\n")
	var schema agentsecurity.ExtractionSchema
	if err := json.Unmarshal([]byte(line), &schema); err != nil {
		return "", fmt.Errorf("agent fake model: extraction schema: %w", err)
	}
	type wireValue struct {
		Name      string `json:"name"`
		ValueJSON string `json:"value_json"`
		Location  string `json:"location"`
	}
	wire := struct {
		SchemaID      string      `json:"schema_id"`
		SchemaVersion string      `json:"schema_version"`
		Values        []wireValue `json:"values"`
	}{SchemaID: schema.ID, SchemaVersion: schema.Version, Values: []wireValue{}}
	for _, field := range schema.Fields {
		zero := `""`
		switch strings.ToLower(field.Type) {
		case "number", "integer", "int":
			zero = `0`
		case "boolean", "bool":
			zero = `false`
		}
		wire.Values = append(wire.Values, wireValue{Name: field.Name, ValueJSON: zero, Location: "content:1"})
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
