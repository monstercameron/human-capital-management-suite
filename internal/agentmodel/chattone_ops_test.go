package agentmodel_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel/openai"
)

// chattoneRecordedResponse is a recorded Responses API answer for the
// tone rewrite: the typed result as the provider returns it, wrapped the way the
// wire wraps it. Nothing here is a live call.
const chattoneRecordedResponse = `{"id":"resp-recorded-1","model":"gpt-6-luna","status":"completed","usage":{"input_tokens":120,"output_tokens":40,"total_tokens":160},"output":[{"type":"message","content":[{"type":"output_text","text":"{\"text\":\"Could we look at the 42 files by 2026-10-01?\",\"meaning_preserved\":true}"}]}]}`

type chattoneWire struct {
	mu     sync.Mutex
	bodies []map[string]json.RawMessage
	answer string
	status int
}

func (w *chattoneWire) handler() http.Handler {
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

func chattoneOp(t *testing.T, wire *chattoneWire, model string) (*agentmodel.ChattoneRewriteOp, agentmodel.ModelRequest) {
	t.Helper()
	server := httptest.NewServer(wire.handler())
	t.Cleanup(server.Close)
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: model, Version: model}
	provider, err := openai.New(openai.Config{APIKey: "test-only-key", ModelProfile: "chat-tone-profile", Identity: identity, BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	pricing, err := agentmodel.NewPricingSchedule(agentmodel.PricingSchedule{Version: "rates-v1", Authority: "test-admin", Signature: "test-verified-signature", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerToken: 2, OutputMicrosPerToken: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	selection := agentmodel.ModelSelection{ProfileID: "chat-tone-profile", ProfileDigest: strings.Repeat("a", 64), Identity: identity}
	op, err := agentmodel.NewChattoneRewriteOp(provider, selection, pricing)
	if err != nil {
		t.Fatal(err)
	}
	req := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: "chat-tone-v1", ModelProfile: "chat-tone-profile",
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: "Rewrite tone only."}, {Role: agentmodel.RoleUser, Content: "<untrusted_data>You idiot, fix the 42 files by 2026-10-01.</untrusted_data>"}},
		Deadline: time.Now().Add(time.Minute), Limits: agentmodel.ModelLimits{MaxInputTokens: 6000, MaxOutputTokens: 4000, MaxCostMicros: 20000}, TraceID: "trace-chattone-ops",
		Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}}
	return op, req
}

// TestTodo_CHATTONE_002_Ops_RecordedWire: the rewrite is one structured
// operation on gpt-6-luna. The recorded answer decodes to text and a verdict,
// the request names the model exactly, asks for the strict typed schema, and
// carries neither a temperature (SchemaFlux v1.2.0 exempts only the gpt-5
// prefix from sending one, so gpt-6 would be rejected by its own provider) nor
// a prompt cache key (its keys exceed the API's 64 characters).
func TestTodo_CHATTONE_002_Ops_RecordedWire(t *testing.T) {
	wire := &chattoneWire{answer: chattoneRecordedResponse}
	op, req := chattoneOp(t, wire, agentmodel.ChattoneDefaultModel)
	rewrite, result, err := op.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("recorded rewrite: %v", err)
	}
	if rewrite.Text != "Could we look at the 42 files by 2026-10-01?" || !rewrite.MeaningPreserved || !rewrite.Usable() {
		t.Fatalf("decoded %+v", rewrite)
	}
	if result.Usage.CostMicros != 120*2+40*8 || result.Provider.ModelID != "gpt-6-luna" {
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
	want, err := agentmodel.ChattoneRewriteSchema()
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
	for _, field := range []string{`"text"`, `"meaning_preserved"`, `"additionalProperties":false`} {
		if !strings.Contains(string(leftJSON), field) {
			t.Errorf("schema lacks %s: %s", field, leftJSON)
		}
	}
}

// TestTodo_CHATTONE_002_Ops_Golden pins the schema bytes and digest a model
// profile records, so a change to the typed result is a new version that needs
// its own qualification.
func TestTodo_CHATTONE_002_Ops_Golden(t *testing.T) {
	schema, err := agentmodel.ChattoneRewriteSchema()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentmodel.ChattoneRewriteSchemaDigest()
	if err != nil || !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("digest %q: %v", digest, err)
	}
	const golden = `{"additionalProperties":false,"properties":{"meaning_preserved":{"type":"boolean"},"text":{"type":"string"}},"required":["text","meaning_preserved"],"type":"object"}`
	var compact any
	_ = json.Unmarshal(schema, &compact)
	got, _ := json.Marshal(compact)
	if string(got) != golden {
		t.Fatalf("schema = %s\nwant    %s", got, golden)
	}
	other, _ := agentmodel.ChattoneRewriteSchema()
	if string(other) != string(schema) {
		t.Fatal("the schema is not stable between calls")
	}
}

// TestTodo_CHATTONE_002_Ops_Verdict: a verdict that fails, is missing or comes
// with anything else is not a usable rewrite; the caller keeps the original.
func TestTodo_CHATTONE_002_Ops_Verdict(t *testing.T) {
	wrap := func(structured string) string {
		quoted, _ := json.Marshal(structured)
		return `{"id":"r","model":"gpt-6-luna","status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15},"output":[{"type":"message","content":[{"type":"output_text","text":` + string(quoted) + `}]}]}`
	}
	t.Run("verdict false is returned but not usable", func(t *testing.T) {
		wire := &chattoneWire{answer: wrap(`{"text":"Fine, no.","meaning_preserved":false}`)}
		op, req := chattoneOp(t, wire, agentmodel.ChattoneDefaultModel)
		rewrite, _, err := op.Run(context.Background(), req)
		if err != nil || rewrite.MeaningPreserved || rewrite.Usable() {
			t.Fatalf("a failed verdict was usable: %+v %v", rewrite, err)
		}
	})
	// SchemaFlux fills a field the provider left out with its zero value, so a
	// missing verdict arrives as false: never usable.
	t.Run("missing verdict reads as false", func(t *testing.T) {
		wire := &chattoneWire{answer: wrap(`{"text":"Could we talk?"}`)}
		op, req := chattoneOp(t, wire, agentmodel.ChattoneDefaultModel)
		if rewrite, _, _ := op.Run(context.Background(), req); rewrite.Usable() {
			t.Fatalf("a rewrite with no verdict was usable: %+v", rewrite)
		}
	})
	for name, structured := range map[string]string{
		"missing text": `{"meaning_preserved":true}`,
		"empty text":   `{"text":"  ","meaning_preserved":true}`,
		"extra field":  `{"text":"Could we talk?","meaning_preserved":true,"note":"x"}`,
		"not json":     `Could we talk?`,
	} {
		t.Run(name, func(t *testing.T) {
			wire := &chattoneWire{answer: wrap(structured)}
			op, req := chattoneOp(t, wire, agentmodel.ChattoneDefaultModel)
			rewrite, _, err := op.Run(context.Background(), req)
			if err == nil || rewrite.Usable() {
				t.Fatalf("%s accepted: %+v", name, rewrite)
			}
			if strings.Contains(err.Error(), "Could we talk") {
				t.Fatalf("the error carries provider text: %v", err)
			}
		})
	}
}

// TestTodo_CHATTONE_002_Ops_NoSilentFallback: the model named is the model
// called. A provider that answers as another model is refused, a provider
// failure is a failure, and an unnamed selection cannot be built.
func TestTodo_CHATTONE_002_Ops_NoSilentFallback(t *testing.T) {
	if got := agentmodel.ChattoneModelID(""); got != "gpt-6-luna" {
		t.Fatalf("default model = %q", got)
	}
	if got := agentmodel.ChattoneModelID("  gpt-6-sol "); got != "gpt-6-sol" {
		t.Fatalf("configured model = %q", got)
	}
	wire := &chattoneWire{answer: strings.Replace(chattoneRecordedResponse, `"model":"gpt-6-luna"`, `"model":"gpt-5-mini"`, 1)}
	op, req := chattoneOp(t, wire, agentmodel.ChattoneDefaultModel)
	if rewrite, _, err := op.Run(context.Background(), req); err == nil || rewrite.Usable() {
		t.Fatalf("an answer from another model was accepted: %+v %v", rewrite, err)
	}
	failing := &chattoneWire{status: http.StatusServiceUnavailable}
	op, req = chattoneOp(t, failing, agentmodel.ChattoneDefaultModel)
	if _, _, err := op.Run(context.Background(), req); err == nil || len(failing.bodies) != 1 {
		t.Fatalf("a provider failure was retried or swallowed: %v after %d calls", err, len(failing.bodies))
	}
	// A different configured model is sent as itself.
	other := &chattoneWire{answer: strings.Replace(chattoneRecordedResponse, "gpt-6-luna", "gpt-6-sol", 1)}
	op, req = chattoneOp(t, other, "gpt-6-sol")
	if rewrite, _, err := op.Run(context.Background(), req); err != nil || !rewrite.Usable() || string(other.bodies[0]["model"]) != `"gpt-6-sol"` {
		t.Fatalf("configured model not honoured: %+v %v %s", rewrite, err, other.bodies[0]["model"])
	}
	if _, err := agentmodel.NewChattoneRewriteOp(nil, agentmodel.ModelSelection{}, nil); !errors.Is(err, agentmodel.ErrNotConfigured) {
		t.Fatalf("an unconfigured operation was built: %v", err)
	}
}

// TestTodo_CHATTONE_002_Ops_Request: tools are refused, a cancelled context
// makes no call, and the request's own text reaches the provider untouched.
func TestTodo_CHATTONE_002_Ops_Request(t *testing.T) {
	wire := &chattoneWire{answer: chattoneRecordedResponse}
	op, req := chattoneOp(t, wire, agentmodel.ChattoneDefaultModel)
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
	if err := json.Unmarshal(wire.bodies[0]["input"], &input); err != nil || len(input) < 2 || input[0].Content != "Rewrite tone only." || input[1].Content != req.Messages[1].Content {
		t.Fatalf("messages changed on the way: %s (%v)", wire.bodies[0]["input"], err)
	}
	var nilOp *agentmodel.ChattoneRewriteOp
	if _, _, err := nilOp.Run(context.Background(), req); !errors.Is(err, agentmodel.ErrNotConfigured) {
		t.Fatalf("nil op: %v", err)
	}
}
