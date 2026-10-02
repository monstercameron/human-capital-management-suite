package agentmodel_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel/openai"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// chatlangRecordedResponse is a recorded Responses API answer for a
// translation: the typed result as the provider returns it, wrapped the way the
// wire wraps it. Nothing here is a live call.
const chatlangRecordedResponse = `{"id":"resp-recorded-2","model":"gpt-6-luna","status":"completed","usage":{"input_tokens":150,"output_tokens":30,"total_tokens":180},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"source_language\":\"en\",\"text\":\"Bitte pruefe die 42 Dateien bis 2026-10-01.\",\"meaning_preserved\":true}"}]}]}`

type chatlangWire struct {
	mu     sync.Mutex
	bodies []map[string]json.RawMessage
	answer string
	status int
}

func (w *chatlangWire) handler() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		w.mu.Lock()
		w.bodies = append(w.bodies, body)
		answer, status := w.answer, w.status
		w.mu.Unlock()
		if status != 0 {
			rw.WriteHeader(status)
			return
		}
		io.WriteString(rw, answer)
	})
}

func chatlangOp(t *testing.T, wire *chatlangWire, model string) (*agentmodel.ChatlangTranslationOp, agentmodel.ModelRequest) {
	t.Helper()
	server := httptest.NewServer(wire.handler())
	t.Cleanup(server.Close)
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: model, Version: model}
	provider, err := openai.New(openai.Config{APIKey: "test-only-key", ModelProfile: "chat-translation-profile", Identity: identity, BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	pricing, err := agentmodel.NewPricingSchedule(agentmodel.PricingSchedule{Version: "rates-v1", Authority: "test-admin", Signature: "test-verified-signature", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerToken: 2, OutputMicrosPerToken: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	selection := agentmodel.ModelSelection{ProfileID: "chat-translation-profile", ProfileDigest: strings.Repeat("a", 64), Identity: identity}
	op, err := agentmodel.NewChatlangTranslationOp(provider, selection, pricing)
	if err != nil {
		t.Fatal(err)
	}
	req := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: "chat-translation-v1", ModelProfile: "chat-translation-profile",
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: "Translate one chat message."}, {Role: agentmodel.RoleUser, Content: "<untrusted_data>Please check the 42 files by 2026-10-01.</untrusted_data>"}},
		Deadline: time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxInputTokens: 6000, MaxOutputTokens: 4000, MaxCostMicros: 20000}, TraceID: "trace-chatlang-ops",
		Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}}
	return op, req
}

// TestTodo_CHATLANG_003_Ops_RecordedWire: language detection and translation are
// one structured operation on gpt-6-luna. The recorded answer decodes to the
// language found, the text and a verdict; the request names the model exactly,
// asks for the strict typed schema, makes one call, and carries neither a
// temperature (SchemaFlux v1.2.0 exempts only the gpt-5 prefix from sending one,
// so gpt-6 would be rejected by its own provider) nor a prompt cache key (its
// keys exceed the API's 64 characters).
func TestTodo_CHATLANG_003_Ops_RecordedWire(t *testing.T) {
	wire := &chatlangWire{answer: chatlangRecordedResponse}
	op, req := chatlangOp(t, wire, agentmodel.ChatlangDefaultModel)
	translation, result, err := op.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("recorded translation: %v", err)
	}
	if translation.SourceLanguage != "en" || translation.Text != "Bitte pruefe die 42 Dateien bis 2026-10-01." || !translation.Usable() {
		t.Fatalf("decoded %+v", translation)
	}
	if result.Usage.CostMicros != 150*2+30*8 || result.Usage.InputTokens != 150 || result.Usage.OutputTokens != 30 || result.Provider.ModelID != "gpt-6-luna" {
		t.Fatalf("usage not priced on the result: %+v", result)
	}
	if len(wire.bodies) != 1 {
		t.Fatalf("calls = %d, want exactly one provider call", len(wire.bodies))
	}
	body := wire.bodies[0]
	for _, banned := range []string{"temperature", "prompt_cache_key", "top_p", "tools"} {
		if _, present := body[banned]; present {
			t.Errorf("the request carries %q", banned)
		}
	}
	if string(body["model"]) != `"gpt-6-luna"` || string(body["store"]) != "false" {
		t.Errorf("model/store = %s / %s", body["model"], body["store"])
	}
	var text struct {
		Format struct {
			Type   string          `json:"type"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"format"`
	}
	if err := json.Unmarshal(body["text"], &text); err != nil || text.Format.Type != "json_schema" || !text.Format.Strict {
		t.Fatalf("output format = %s (%v)", body["text"], err)
	}
	want, err := agentmodel.ChatlangTranslationSchema()
	if err != nil {
		t.Fatal(err)
	}
	var left, right any
	_ = json.Unmarshal(want, &left)
	_ = json.Unmarshal(text.Format.Schema, &right)
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("wire schema differs from the SchemaFlux-derived schema:\n%s\n%s", leftJSON, rightJSON)
	}
}

// TestTodo_CHATLANG_003_Ops_Golden pins the schema bytes and digest a model
// profile records, so a change to the typed result is a new version that needs
// its own qualification.
func TestTodo_CHATLANG_003_Ops_Golden(t *testing.T) {
	schema, err := agentmodel.ChatlangTranslationSchema()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentmodel.ChatlangTranslationSchemaDigest()
	if err != nil || !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("digest %q: %v", digest, err)
	}
	const golden = `{"additionalProperties":false,"properties":{"meaning_preserved":{"type":"boolean"},"source_language":{"type":"string"},"text":{"type":"string"}},"required":["source_language","text","meaning_preserved"],"type":"object"}`
	var compact any
	_ = json.Unmarshal(schema, &compact)
	got, _ := json.Marshal(compact)
	if string(got) != golden {
		t.Fatalf("schema = %s\nwant    %s", got, golden)
	}
	other, _ := agentmodel.ChatlangTranslationSchema()
	if string(other) != string(schema) {
		t.Fatal("the schema is not stable between calls")
	}
}

// TestTodo_CHATLANG_003_Ops_Verdict: a verdict that fails, is missing or comes
// with anything else is not a usable translation; the message stays as written.
func TestTodo_CHATLANG_003_Ops_Verdict(t *testing.T) {
	wrap := func(structured string) string {
		quoted, _ := json.Marshal(structured)
		return `{"id":"r","model":"gpt-6-luna","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15},"output":[{"type":"message","content":[{"type":"output_text","text":` + string(quoted) + `}]}]}`
	}
	t.Run("verdict false is returned but not usable", func(t *testing.T) {
		op, req := chatlangOp(t, &chatlangWire{answer: wrap(`{"source_language":"en","text":"Nein.","meaning_preserved":false}`)}, agentmodel.ChatlangDefaultModel)
		got, _, err := op.Run(context.Background(), req)
		if err != nil || got.MeaningPreserved || got.Usable() {
			t.Fatalf("a failed verdict was usable: %+v %v", got, err)
		}
	})
	for name, structured := range map[string]string{
		"missing text":     `{"source_language":"en","meaning_preserved":true}`,
		"empty text":       `{"source_language":"en","text":"  ","meaning_preserved":true}`,
		"missing language": `{"text":"Hallo","meaning_preserved":true}`,
		"extra field":      `{"source_language":"en","text":"Hallo","meaning_preserved":true,"note":"x"}`,
		"not json":         `Hallo`,
	} {
		t.Run(name, func(t *testing.T) {
			op, req := chatlangOp(t, &chatlangWire{answer: wrap(structured)}, agentmodel.ChatlangDefaultModel)
			got, _, err := op.Run(context.Background(), req)
			if err == nil || got.Usable() {
				t.Fatalf("%s accepted: %+v", name, got)
			}
			if strings.Contains(err.Error(), "Hallo") {
				t.Fatalf("the error carries provider text: %v", err)
			}
		})
	}
}

// TestTodo_CHATLANG_003_Ops_NoSilentFallback: the model named is the model
// called. A provider that answers as another model is refused, a provider
// failure is a failure and is not retried, and an unnamed selection cannot be built.
func TestTodo_CHATLANG_003_Ops_NoSilentFallback(t *testing.T) {
	if got := agentmodel.ChatlangModelID(""); got != "gpt-6-luna" {
		t.Fatalf("default model = %q", got)
	}
	if got := agentmodel.ChatlangModelID("  gpt-6-sol "); got != "gpt-6-sol" {
		t.Fatalf("configured model = %q", got)
	}
	wire := &chatlangWire{answer: strings.Replace(chatlangRecordedResponse, `"model":"gpt-6-luna"`, `"model":"gpt-5-mini"`, 1)}
	op, req := chatlangOp(t, wire, agentmodel.ChatlangDefaultModel)
	if got, _, err := op.Run(context.Background(), req); err == nil || got.Usable() {
		t.Fatalf("an answer from another model was accepted: %+v %v", got, err)
	}
	failing := &chatlangWire{status: http.StatusServiceUnavailable}
	op, req = chatlangOp(t, failing, agentmodel.ChatlangDefaultModel)
	if _, _, err := op.Run(context.Background(), req); err == nil || len(failing.bodies) != 1 {
		t.Fatalf("a provider failure was retried or swallowed: %v after %d calls", err, len(failing.bodies))
	}
	other := &chatlangWire{answer: strings.Replace(chatlangRecordedResponse, "gpt-6-luna", "gpt-6-sol", 1)}
	op, req = chatlangOp(t, other, "gpt-6-sol")
	if got, _, err := op.Run(context.Background(), req); err != nil || !got.Usable() || string(other.bodies[0]["model"]) != `"gpt-6-sol"` {
		t.Fatalf("configured model not honoured: %+v %v %s", got, err, other.bodies[0]["model"])
	}
	if _, err := agentmodel.NewChatlangTranslationOp(nil, agentmodel.ModelSelection{}, nil); !errors.Is(err, agentmodel.ErrNotConfigured) {
		t.Fatalf("an unconfigured operation was built: %v", err)
	}
}

// TestTodo_CHATLANG_003_Ops_Request: tools are refused, a cancelled context makes
// no call, and the request's own text reaches the provider untouched.
func TestTodo_CHATLANG_003_Ops_Request(t *testing.T) {
	wire := &chatlangWire{answer: chatlangRecordedResponse}
	op, req := chatlangOp(t, wire, agentmodel.ChatlangDefaultModel)
	withTool := req
	withTool.Tools = []agentmodel.ToolSchema{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	if _, _, err := op.Run(context.Background(), withTool); !errors.Is(err, agentmodel.ErrInvalidModelRequest) || len(wire.bodies) != 0 {
		t.Fatalf("a request with tools reached the provider: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := op.Run(ctx, req); !errors.Is(err, context.Canceled) || len(wire.bodies) != 0 {
		t.Fatalf("a cancelled call reached the provider: %v", err)
	}
	if _, _, err := op.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var input []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(wire.bodies[0]["input"], &input); err != nil || len(input) < 2 || input[0].Content != "Translate one chat message." || input[1].Content != req.Messages[1].Content {
		t.Fatalf("messages changed on the way: %s (%v)", wire.bodies[0]["input"], err)
	}
	var nilOp *agentmodel.ChatlangTranslationOp
	if _, _, err := nilOp.Run(context.Background(), req); !errors.Is(err, agentmodel.ErrNotConfigured) {
		t.Fatalf("nil op: %v", err)
	}
}

// TestChatlangLiveSmoke is the owner's one live call, and it does nothing unless
// asked. It makes a single real request to the model provider through the same
// typed operation the product uses, prints what came back, and checks the answer
// is usable. Run it from a shell that holds the key:
//
//	HCMNEXT_LIVE_CHAT_TRANSLATION=1 MODEL_API_KEY=... go test -count=1 -v -run TestChatlangLiveSmoke ./internal/agentmodel/
//
// HCMNEXT_CHAT_TRANSLATION_MODEL names another model (the default is gpt-6-luna).
// The price in the schedule below is a placeholder, so the cost it prints is not
// real; the token counts are.
func TestChatlangLiveSmoke(t *testing.T) {
	if os.Getenv("HCMNEXT_LIVE_CHAT_TRANSLATION") != "1" {
		t.Skip("set HCMNEXT_LIVE_CHAT_TRANSLATION=1 (and MODEL_API_KEY) for the live call")
	}
	key := os.Getenv("MODEL_API_KEY")
	if key == "" {
		t.Fatal("MODEL_API_KEY is not set")
	}
	model := agentmodel.ChatlangModelID(os.Getenv("HCMNEXT_CHAT_TRANSLATION_MODEL"))
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: model, Version: model}
	provider, err := openai.New(openai.Config{APIKey: key, ModelProfile: "chat-translation-live", Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	pricing, err := agentmodel.NewPricingSchedule(agentmodel.PricingSchedule{Version: "live-smoke", Authority: "smoke", Signature: "smoke", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerToken: 1, OutputMicrosPerToken: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	selection := agentmodel.ModelSelection{ProfileID: "chat-translation-live", ProfileDigest: strings.Repeat("a", 64), Identity: identity}
	op, err := agentmodel.NewChatlangTranslationOp(provider, selection, pricing)
	if err != nil {
		t.Fatal(err)
	}
	instruction, data := chatlang.StructuredPrompt(chatlang.Request{Tenant: "smoke", Source: "und", Target: "de", Text: "Please do not approve the payment before ⟦HCM:abcd1234:0⟧, and thank @⟦HCM:abcd1234:1⟧ for the review."})
	req := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: "chat-translation-v1", ModelProfile: "chat-translation-live",
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: instruction}, {Role: agentmodel.RoleUser, Content: data}},
		Deadline: time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxInputTokens: 4000, MaxOutputTokens: 3000, MaxCostMicros: 20000}, TraceID: "trace-chatlang-live-smoke",
		Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}}
	translation, result, err := op.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("live call: %v", err)
	}
	t.Logf("model %s: language %q, usable %v, tokens %d in / %d out, placeholder cost %d", result.Provider.ModelID, translation.SourceLanguage, translation.Usable(), result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.CostMicros)
	t.Logf("translation: %s", translation.Text)
	if !translation.Usable() || !strings.EqualFold(translation.SourceLanguage, "en") || strings.Count(translation.Text, "⟦HCM:abcd1234:0⟧") != 1 || strings.Count(translation.Text, "⟦HCM:abcd1234:1⟧") != 1 {
		t.Fatalf("the live answer is not a usable translation of the English text: %+v", translation)
	}
}
