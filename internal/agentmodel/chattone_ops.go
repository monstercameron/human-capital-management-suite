package agentmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
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
	return defaultedModelID(configured, ChattoneDefaultModel)
}

var chattoneRewriteSchemaCache schemaCache

// ChattoneRewriteSchema is SchemaFlux's own Go-derived schema for
// ChattoneRewrite, so the schema pinned in a model profile and sent to the
// provider is exactly the one SchemaFlux would generate and validate against.
func ChattoneRewriteSchema() (json.RawMessage, error) {
	return chattoneRewriteSchemaCache.get(func() (json.RawMessage, error) {
		return deriveTypedSchema(ChattoneDefaultModel, ChattoneRewrite{Text: "probe", MeaningPreserved: true}, ErrChattoneRewrite)
	})
}

// ChattoneRewriteSchemaDigest is the digest a model profile records as its
// output schema for the tone rewrite task.
func ChattoneRewriteSchemaDigest() (string, error) {
	return typedSchemaDigest(ChattoneRewriteSchema)
}

// ChattoneRewriteOutput is the output constraint a tone rewrite request carries.
func ChattoneRewriteOutput() (OutputConstraint, error) {
	return typedSchemaOutput(ChattoneRewriteSchema)
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
// no process-default client.
type ChattoneRewriteOp struct {
	op *typedChatOp[ChattoneRewrite]
}

// NewChattoneRewriteOp binds the operation to a provider, an approved model
// selection and its pricing. The selection must name a model; there is no
// substitute.
func NewChattoneRewriteOp(provider ModelAdapter, selection ModelSelection, pricing *PricingSchedule) (*ChattoneRewriteOp, error) {
	op, err := newTypedChatOp(provider, selection, pricing, ChattoneRewriteOutput, DecodeChattoneRewrite)
	if err != nil {
		return nil, err
	}
	return &ChattoneRewriteOp{op: op}, nil
}

// Run performs one rewrite. The request's messages carry the instruction and
// the delimited text; Run sets the typed output contract and refuses a request
// that brings tools. It returns the provider's normalized result (usage is
// priced on it, also when the call failed) beside the decoded rewrite.
func (o *ChattoneRewriteOp) Run(ctx context.Context, req ModelRequest) (ChattoneRewrite, ModelResult, error) {
	if o == nil {
		return ChattoneRewrite{}, ModelResult{}, ErrNotConfigured
	}
	return o.op.run(ctx, req)
}
