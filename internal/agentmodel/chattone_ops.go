package agentmodel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	schemaflux "github.com/monstercameron/schemaflux"
)

// ChattoneDefaultModel is the model the chat tone operations ask for when a
// deployment names none. It is a default, never a fallback: a deployment that
// names another model gets that model, and a call that cannot be served by the
// named model fails instead of being served by a different one.
const ChattoneDefaultModel = "gpt-6-luna"

// ErrChattoneRewrite means the provider's answer to a tone rewrite is not the
// operation's typed result. Callers treat it as "no rewrite": the original text
// stands.
var ErrChattoneRewrite = errors.New("agentmodel: tone rewrite result is invalid")

// ChattoneRewrite is the one structured result of a tone rewrite, used by the
// writing-style controls and by "Reword" alike (only the instruction differs).
// The model returns the rewritten text and its own verdict on whether the
// rewrite kept the meaning. The verdict is a second opinion beside HCM's local
// checks, never a replacement for them; a false or missing verdict means the
// original stands.
type ChattoneRewrite struct {
	Text             string `json:"text" schemaflux:"required"`
	MeaningPreserved bool   `json:"meaning_preserved"`
}

// Usable reports whether the rewrite may be offered at all: it has text and
// the model vouched for the meaning.
func (r ChattoneRewrite) Usable() bool {
	return strings.TrimSpace(r.Text) != "" && r.MeaningPreserved
}

// ChattoneModelID returns the model identity to call: the configured one, or
// ChattoneDefaultModel when none is configured.
func ChattoneModelID(configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return ChattoneDefaultModel
}

// chattoneSchemaProbe is a call-local SchemaFlux provider that records the
// schema SchemaFlux derives from the Go type. It never reaches a network.
type chattoneSchemaProbe struct{ schema json.RawMessage }

func (*chattoneSchemaProbe) Name() string                                      { return "openai" }
func (*chattoneSchemaProbe) EstimateCost(schemaflux.CompletionRequest) float64 { return 0 }
func (*chattoneSchemaProbe) RetryPolicy() (int, time.Duration)                 { return 0, 0 }
func (p *chattoneSchemaProbe) Complete(_ context.Context, completion schemaflux.CompletionRequest) (schemaflux.CompletionResponse, error) {
	encoded, err := json.Marshal(completion.JSONSchema)
	if err != nil || len(completion.JSONSchema) == 0 {
		return schemaflux.CompletionResponse{}, ErrChattoneRewrite
	}
	p.schema = encoded
	return schemaflux.CompletionResponse{Content: `{"text":"probe","meaning_preserved":true}`, Model: ChattoneDefaultModel, Provider: "openai", FinishReason: "stop"}, nil
}

var (
	chattoneSchemaOnce sync.Once
	chattoneSchema     json.RawMessage
	chattoneSchemaErr  error
)

// ChattoneRewriteSchema is SchemaFlux's own Go-derived schema for
// ChattoneRewrite, so the schema pinned in a model profile and sent to the
// provider is exactly the one SchemaFlux would generate and validate against.
func ChattoneRewriteSchema() (json.RawMessage, error) {
	chattoneSchemaOnce.Do(func() {
		probe := &chattoneSchemaProbe{}
		client := schemaflux.NewClient("").WithProviderInstance(probe).WithRetries(0)
		_, err := schemaflux.Generating[ChattoneRewrite]("probe").Strict().Model(ChattoneDefaultModel).RunResult(client.Context(context.Background()))
		if err != nil || len(probe.schema) == 0 {
			chattoneSchemaErr = ErrChattoneRewrite
			return
		}
		chattoneSchema = probe.schema
	})
	return append(json.RawMessage(nil), chattoneSchema...), chattoneSchemaErr
}

// ChattoneRewriteSchemaDigest is the digest a model profile records as its
// output schema for the tone rewrite task.
func ChattoneRewriteSchemaDigest() (string, error) {
	schema, err := ChattoneRewriteSchema()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(schema)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ChattoneRewriteOutput is the output constraint a tone rewrite request carries.
func ChattoneRewriteOutput() (OutputConstraint, error) {
	schema, err := ChattoneRewriteSchema()
	if err != nil {
		return OutputConstraint{}, err
	}
	return OutputConstraint{Mode: OutputSchema, Schema: schema}, nil
}

// DecodeChattoneRewrite reads the typed result out of a normalized provider
// result. It refuses a failure or refusal, unknown or missing fields, trailing
// bytes and empty text, and never echoes provider bytes in its error.
func DecodeChattoneRewrite(result ModelResult) (ChattoneRewrite, error) {
	if result.Failure != nil || result.Refusal != nil || result.Finish != FinishComplete || len(result.ToolProposals) != 0 || len(result.RequestedActions) != 0 {
		return ChattoneRewrite{}, ErrChattoneRewrite
	}
	var shape struct {
		Text             *string `json:"text"`
		MeaningPreserved *bool   `json:"meaning_preserved"`
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Structured))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&shape) != nil || shape.Text == nil || shape.MeaningPreserved == nil || strings.TrimSpace(*shape.Text) == "" {
		return ChattoneRewrite{}, ErrChattoneRewrite
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ChattoneRewrite{}, ErrChattoneRewrite
	}
	return ChattoneRewrite{Text: *shape.Text, MeaningPreserved: *shape.MeaningPreserved}, nil
}

// ChattoneRewriteOp is the tone rewrite as one SchemaFlux typed operation over
// a pinned provider adapter: one provider call, no retries inside the library,
// no process-default client. Because the provider adapter is HCM's own, SchemaFlux
// never builds the OpenAI request itself, so its temperature rule (which only
// exempts the gpt-5 family) and its prompt-cache key (longer than the API's 64
// characters) are never sent; the adapter's wire body carries neither.
type ChattoneRewriteOp struct {
	adapter *SchemaFluxTypedAdapter[ChattoneRewrite]
}

// NewChattoneRewriteOp binds the operation to a provider, an approved model
// selection and its pricing. The selection must name a model; there is no
// substitute.
func NewChattoneRewriteOp(provider ModelAdapter, selection ModelSelection, pricing *PricingSchedule) (*ChattoneRewriteOp, error) {
	if strings.TrimSpace(selection.Identity.ModelID) == "" {
		return nil, ErrNotConfigured
	}
	adapter, err := NewSchemaFluxTypedAdapter[ChattoneRewrite](provider, selection, pricing)
	if err != nil {
		return nil, err
	}
	return &ChattoneRewriteOp{adapter: adapter}, nil
}

// Run performs one rewrite. The request's messages carry the instruction and
// the delimited text; Run sets the typed output contract and refuses a request
// that brings tools. It returns the provider's normalized result (usage is
// priced on it, also when the call failed) beside the decoded rewrite.
func (o *ChattoneRewriteOp) Run(ctx context.Context, req ModelRequest) (ChattoneRewrite, ModelResult, error) {
	if o == nil || o.adapter == nil || ctx == nil {
		return ChattoneRewrite{}, ModelResult{}, ErrNotConfigured
	}
	output, err := ChattoneRewriteOutput()
	if err != nil {
		return ChattoneRewrite{}, ModelResult{}, err
	}
	if len(req.Tools) != 0 {
		return ChattoneRewrite{}, ModelResult{}, ErrInvalidModelRequest
	}
	req.Output = output
	req.RequiredFeatures = []ModelFeature{FeatureStructuredJSON}
	result, err := o.adapter.Invoke(ctx, req)
	if err != nil {
		return ChattoneRewrite{}, result, err
	}
	rewrite, err := DecodeChattoneRewrite(result)
	return rewrite, result, err
}
