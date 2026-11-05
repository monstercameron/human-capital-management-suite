package agentmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	schemaflux "github.com/monstercameron/schemaflux"
)

// This file is the shared core of the single-shot typed chat operations
// (translation, tone rewrite): one SchemaFlux typed call over a pinned
// provider adapter, with the Go-derived output schema probed once and the
// provider's normalized result decoded by the operation.

// defaultedModelID returns the model identity to call: the configured one,
// or the operation's default when none is configured. A default is never a
// fallback: a call the named model cannot serve fails instead of being
// served by a different one.
func defaultedModelID(configured, def string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return def
}

// schemaProbe is a call-local SchemaFlux provider that records the schema
// SchemaFlux derives from the Go type. It never reaches a network.
type schemaProbe struct {
	schema  json.RawMessage
	model   string
	content string
	failed  error
}

func (*schemaProbe) Name() string                                      { return "openai" }
func (*schemaProbe) EstimateCost(schemaflux.CompletionRequest) float64 { return 0 }
func (*schemaProbe) RetryPolicy() (int, time.Duration)                 { return 0, 0 }
func (p *schemaProbe) Complete(_ context.Context, completion schemaflux.CompletionRequest) (schemaflux.CompletionResponse, error) {
	encoded, err := json.Marshal(completion.JSONSchema)
	if err != nil || len(completion.JSONSchema) == 0 {
		return schemaflux.CompletionResponse{}, p.failed
	}
	p.schema = encoded
	return schemaflux.CompletionResponse{Content: p.content, Model: p.model, Provider: "openai", FinishReason: "stop"}, nil
}

// deriveTypedSchema runs SchemaFlux once against a call-local probe to learn
// the Go-derived schema for T, so the schema pinned in a model profile and
// sent to the provider is exactly the one SchemaFlux would generate and
// validate against. probeValue stands in for provider content; it must decode
// into T.
func deriveTypedSchema[T any](model string, probeValue T, failed error) (json.RawMessage, error) {
	content, err := json.Marshal(probeValue)
	if err != nil {
		return nil, failed
	}
	probe := &schemaProbe{model: model, content: string(content), failed: failed}
	client := schemaflux.NewClient("").WithProviderInstance(probe).WithRetries(0)
	_, err = schemaflux.Generating[T]("probe").Strict().Model(model).RunResult(client.Context(context.Background()))
	if err != nil || len(probe.schema) == 0 {
		return nil, failed
	}
	return probe.schema, nil
}

// schemaCache memoizes one operation's Go-derived schema.
type schemaCache struct {
	once   sync.Once
	schema json.RawMessage
	err    error
}

func (c *schemaCache) get(derive func() (json.RawMessage, error)) (json.RawMessage, error) {
	c.once.Do(func() {
		schema, err := derive()
		if err != nil {
			c.err = err
			return
		}
		c.schema = schema
	})
	return append(json.RawMessage(nil), c.schema...), c.err
}

// typedSchemaDigest is the digest a model profile records as an operation's
// output schema.
func typedSchemaDigest(schema func() (json.RawMessage, error)) (string, error) {
	encoded, err := schema()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// typedSchemaOutput is the output constraint an operation's request carries.
func typedSchemaOutput(schema func() (json.RawMessage, error)) (OutputConstraint, error) {
	encoded, err := schema()
	if err != nil {
		return OutputConstraint{}, err
	}
	return OutputConstraint{Mode: OutputSchema, Schema: encoded}, nil
}

// typedChatOp runs one SchemaFlux typed operation over a pinned provider
// adapter: one provider call, no retries inside the library, no
// process-default client. Because the provider adapter is HCM's own,
// SchemaFlux never builds the OpenAI request itself, so its temperature rule
// (which only exempts the gpt-5 family) and its prompt-cache key (longer than
// the API's 64 characters) are never sent; the adapter's wire body carries
// neither.
type typedChatOp[T any] struct {
	adapter *SchemaFluxTypedAdapter[T]
	output  func() (OutputConstraint, error)
	decode  func(ModelResult) (T, error)
}

func newTypedChatOp[T any](provider ModelAdapter, selection ModelSelection, pricing *PricingSchedule, output func() (OutputConstraint, error), decode func(ModelResult) (T, error)) (*typedChatOp[T], error) {
	if strings.TrimSpace(selection.Identity.ModelID) == "" {
		return nil, ErrNotConfigured
	}
	adapter, err := NewSchemaFluxTypedAdapter[T](provider, selection, pricing)
	if err != nil {
		return nil, err
	}
	return &typedChatOp[T]{adapter: adapter, output: output, decode: decode}, nil
}

// run performs one call. The request's messages carry the instruction and
// the delimited text; run sets the typed output contract and refuses a
// request that brings tools. It returns the provider's normalized result
// (usage is priced on it, also when the call failed) beside the decoded value.
func (o *typedChatOp[T]) run(ctx context.Context, req ModelRequest) (T, ModelResult, error) {
	var zero T
	if o == nil || o.adapter == nil || ctx == nil {
		return zero, ModelResult{}, ErrNotConfigured
	}
	output, err := o.output()
	if err != nil {
		return zero, ModelResult{}, err
	}
	if len(req.Tools) != 0 {
		return zero, ModelResult{}, ErrInvalidModelRequest
	}
	req.Output = output
	req.RequiredFeatures = []ModelFeature{FeatureStructuredJSON}
	result, err := o.adapter.Invoke(ctx, req)
	if err != nil {
		return zero, result, err
	}
	value, err := o.decode(result)
	return value, result, err
}
