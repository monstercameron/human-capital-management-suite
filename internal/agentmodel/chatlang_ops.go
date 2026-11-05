package agentmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// ChatlangDefaultModel is the model the chat translation operation asks for when
// a deployment names none. It is a default, never a fallback: a deployment that
// names another model gets that model, and a call the named model cannot serve
// fails instead of being served by a different one.
const ChatlangDefaultModel = "gpt-6-luna"

// ErrChatlangTranslation means the provider's answer to a translation is not the
// operation's typed result. Callers treat it as "not translated": the message
// stays as written.
var ErrChatlangTranslation = errors.New("agentmodel: translation result is invalid")

// ChatlangTranslation is the one structured result of translating a chat
// message: the language the model found the message to be written in, the
// translation, and the model's own verdict on whether the translation keeps the
// meaning. Detection and translation are one operation, so a message whose
// language is not recorded is detected in the same call that translates it.
// SourceLanguage is a language tag, or "und" when the text has no language of its
// own (a name, a number, an emoji). When the text is already in the target
// language the model returns it unchanged. The verdict is a second opinion
// beside HCM's own placeholder and glossary checks, never a replacement for
// them; a false verdict means the message stays as written.
type ChatlangTranslation struct {
	SourceLanguage   string `json:"source_language" schemaflux:"required"`
	Text             string `json:"text" schemaflux:"required"`
	MeaningPreserved bool   `json:"meaning_preserved"`
}

// Usable reports whether the translation may be offered at all: it has text and
// the model vouched for the meaning.
func (t ChatlangTranslation) Usable() bool {
	return strings.TrimSpace(t.Text) != "" && t.MeaningPreserved
}

// ChatlangModelID returns the model identity to call: the configured one, or
// ChatlangDefaultModel when none is configured.
func ChatlangModelID(configured string) string {
	return defaultedModelID(configured, ChatlangDefaultModel)
}

var chatlangTranslationSchemaCache schemaCache

// ChatlangTranslationSchema is SchemaFlux's own Go-derived schema for
// ChatlangTranslation, so the schema pinned in a model profile and sent to the
// provider is exactly the one SchemaFlux would generate and validate against.
func ChatlangTranslationSchema() (json.RawMessage, error) {
	return chatlangTranslationSchemaCache.get(func() (json.RawMessage, error) {
		return deriveTypedSchema(ChatlangDefaultModel, ChatlangTranslation{SourceLanguage: "en", Text: "probe", MeaningPreserved: true}, ErrChatlangTranslation)
	})
}

// ChatlangTranslationSchemaDigest is the digest a model profile records as its
// output schema for the translation task.
func ChatlangTranslationSchemaDigest() (string, error) {
	return typedSchemaDigest(ChatlangTranslationSchema)
}

// ChatlangTranslationOutput is the output constraint a translation request carries.
func ChatlangTranslationOutput() (OutputConstraint, error) {
	return typedSchemaOutput(ChatlangTranslationSchema)
}

// DecodeChatlangTranslation reads the typed result out of a normalized provider
// result. It refuses a failure or refusal, unknown or missing fields, trailing
// bytes and empty text or language, and never echoes provider bytes in its error.
func DecodeChatlangTranslation(result ModelResult) (ChatlangTranslation, error) {
	if result.Failure != nil || result.Refusal != nil || result.Finish != FinishComplete || len(result.ToolProposals) != 0 || len(result.RequestedActions) != 0 {
		return ChatlangTranslation{}, ErrChatlangTranslation
	}
	var shape struct {
		SourceLanguage   *string `json:"source_language"`
		Text             *string `json:"text"`
		MeaningPreserved *bool   `json:"meaning_preserved"`
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Structured))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&shape) != nil || shape.SourceLanguage == nil || shape.Text == nil || shape.MeaningPreserved == nil ||
		strings.TrimSpace(*shape.SourceLanguage) == "" || strings.TrimSpace(*shape.Text) == "" {
		return ChatlangTranslation{}, ErrChatlangTranslation
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ChatlangTranslation{}, ErrChatlangTranslation
	}
	return ChatlangTranslation{SourceLanguage: *shape.SourceLanguage, Text: *shape.Text, MeaningPreserved: *shape.MeaningPreserved}, nil
}

// ChatlangTranslationOp is the translation as one SchemaFlux typed operation
// over a pinned provider adapter: one provider call, no retries inside the
// library, no process-default client.
type ChatlangTranslationOp struct {
	op *typedChatOp[ChatlangTranslation]
}

// NewChatlangTranslationOp binds the operation to a provider, an approved model
// selection and its pricing. The selection must name a model; there is no
// substitute.
func NewChatlangTranslationOp(provider ModelAdapter, selection ModelSelection, pricing *PricingSchedule) (*ChatlangTranslationOp, error) {
	op, err := newTypedChatOp(provider, selection, pricing, ChatlangTranslationOutput, DecodeChatlangTranslation)
	if err != nil {
		return nil, err
	}
	return &ChatlangTranslationOp{op: op}, nil
}

// Run performs one translation. The request's messages carry the instruction
// and the delimited text; Run sets the typed output contract and refuses a
// request that brings tools. It returns the provider's normalized result (usage
// is priced on it, also when the call failed) beside the decoded translation.
func (o *ChatlangTranslationOp) Run(ctx context.Context, req ModelRequest) (ChatlangTranslation, ModelResult, error) {
	if o == nil {
		return ChatlangTranslation{}, ModelResult{}, ErrNotConfigured
	}
	return o.op.run(ctx, req)
}
