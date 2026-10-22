package agentmodel

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	schemaflux "github.com/monstercameron/schemaflux"
	"github.com/monstercameron/schemaflux/schemafluxtest"
)

// DefaultProviderKind is the value-only result of selecting the process-wide
// provider used by legacy untyped agent steps.
type DefaultProviderKind string

const (
	DefaultProviderLocalDevFake DefaultProviderKind = "local-dev-deterministic-fake"
	DefaultProviderConfigured   DefaultProviderKind = "schemaflux-configured"
	DefaultProviderUnavailable  DefaultProviderKind = "unavailable"
)

// DefaultProviderSelection contains no SchemaFlux types or credential values.
type DefaultProviderSelection struct {
	Kind     DefaultProviderKind
	Observer DefaultProviderObserver
}

// DefaultProviderObserver exposes only aggregate call counts for the local
// development fake; it never exposes a SchemaFlux client or provider object.
type DefaultProviderObserver interface {
	CallCount() int
}

type defaultProviderObserver struct{ calls atomic.Int64 }

func (o *defaultProviderObserver) CallCount() int { return int(o.calls.Load()) }

// SelectDefaultProvider preserves the HCM profile policy while keeping
// SchemaFlux and its test provider behind the agentmodel package boundary.
// A configured key wins without changing the existing default client; only
// local development without a key installs the deterministic in-process fake.
func SelectDefaultProvider(localDevProfile bool, keyConfigured bool) DefaultProviderSelection {
	if keyConfigured {
		return DefaultProviderSelection{Kind: DefaultProviderConfigured}
	}
	if !localDevProfile {
		return DefaultProviderSelection{Kind: DefaultProviderUnavailable}
	}
	observer := &defaultProviderObserver{}
	fake := schemafluxtest.New().ReplyFunc(func(call int, req schemaflux.CompletionRequest) (string, error) {
		observer.calls.Add(1)
		return defaultFakeReply(call, req)
	})
	schemaflux.SetDefaultClient(schemaflux.NewClient("local-dev-fake-provider-not-a-key").WithProviderInstance(fake))
	return DefaultProviderSelection{Kind: DefaultProviderLocalDevFake, Observer: observer}
}

// InitializeConfiguredDefaultProvider delegates configured-provider setup to
// SchemaFlux, preserving its environment and process-wide default-client rules.
func InitializeConfiguredDefaultProvider() error {
	return schemaflux.InitWithEnv()
}

func defaultFakeReply(_ int, req schemaflux.CompletionRequest) (string, error) {
	prompt := req.UserPrompt
	if strings.Contains(prompt, "Extract only the declared fields") {
		return defaultFakeExtraction(prompt)
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
	encoded, err := json.Marshal(struct {
		Text      string   `json:"text"`
		Citations []string `json:"citations"`
	}{Text: text, Citations: []string{}})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func defaultFakeExtraction(prompt string) (string, error) {
	_, after, found := strings.Cut(prompt, "Schema: ")
	if !found {
		return "", fmt.Errorf("agent fake model: extraction prompt carries no schema")
	}
	line, _, _ := strings.Cut(after, "\n")
	var schema struct {
		ID      string `json:"ID"`
		Version string `json:"Version"`
		Fields  []struct {
			Name string `json:"Name"`
			Type string `json:"Type"`
		} `json:"Fields"`
	}
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
