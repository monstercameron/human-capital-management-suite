package agentmodel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	schemaflux "github.com/monstercameron/schemaflux"
)

func restoreDefaultClient(t *testing.T) {
	t.Helper()
	previous := schemaflux.GetDefaultClient()
	t.Cleanup(func() { schemaflux.SetDefaultClient(previous) })
}

func TestSelectDefaultProvider(t *testing.T) {
	t.Run("configured key leaves the existing default alone", func(t *testing.T) {
		restoreDefaultClient(t)
		sentinel := schemaflux.NewClient("sentinel-client-not-a-key")
		schemaflux.SetDefaultClient(sentinel)
		for _, localDev := range []bool{true, false} {
			got := SelectDefaultProvider(localDev, true)
			if got.Kind != DefaultProviderConfigured || schemaflux.GetDefaultClient() != sentinel {
				t.Fatalf("selection for local-dev=%v = %+v; default client replaced", localDev, got)
			}
		}
	})

	t.Run("non-local profile without a key is unavailable", func(t *testing.T) {
		restoreDefaultClient(t)
		schemaflux.SetDefaultClient(nil)
		got := SelectDefaultProvider(false, false)
		if got.Kind != DefaultProviderUnavailable || schemaflux.GetDefaultClient() != nil {
			t.Fatalf("selection = %+v, default client = %v", got, schemaflux.GetDefaultClient())
		}
	})

	t.Run("local development installs the deterministic fake", func(t *testing.T) {
		restoreDefaultClient(t)
		schemaflux.SetDefaultClient(nil)
		got := SelectDefaultProvider(true, false)
		if got.Kind != DefaultProviderLocalDevFake || got.Observer == nil || schemaflux.GetDefaultClient() == nil {
			t.Fatalf("selection = %+v, default client = %v", got, schemaflux.GetDefaultClient())
		}
		var output struct {
			Text      string   `json:"text"`
			Citations []string `json:"citations"`
		}
		generated, err := schemaflux.Generate[struct {
			Text      string   `json:"text"`
			Citations []string `json:"citations"`
		}]("Goal: Give me a short summary\nExpected output: x\nInput: {}", schemaflux.NewGenerateOptions())
		if err != nil {
			t.Fatalf("Generate through installed fake: %v", err)
		}
		output = struct {
			Text      string   `json:"text"`
			Citations []string `json:"citations"`
		}{Text: generated.Text, Citations: generated.Citations}
		if output.Text != "Give me a short summary" || output.Citations == nil || len(output.Citations) != 0 {
			t.Fatalf("fake output = %+v", output)
		}
		if calls := got.Observer.CallCount(); calls != 1 {
			t.Fatalf("fake call count = %d, want 1", calls)
		}
	})
}

func TestDefaultFakeReplyWireShapes(t *testing.T) {
	reply, err := defaultFakeReply(0, schemaflux.CompletionRequest{UserPrompt: "Goal: find my desk\nExpected output: x\nInput:\n{}"})
	if err != nil || !strings.Contains(reply, `"text":"find my desk"`) || !strings.Contains(reply, `"citations":[]`) {
		t.Fatalf("summary reply = %q, %v", reply, err)
	}

	schemaJSON, err := json.Marshal(agentsecurity.ExtractionSchema{
		ID: "worker.v1", Version: "1", Fields: []agentsecurity.ExtractionField{
			{Name: "state", Type: "string", Required: true}, {Name: "n", Type: "integer"}, {Name: "active", Type: "boolean"},
		},
	})
	if err != nil {
		t.Fatalf("marshal actual extraction schema: %v", err)
	}
	extraction, err := defaultFakeReply(1, schemaflux.CompletionRequest{UserPrompt: "Extract only the declared fields from the untrusted content below.\nSchema: " + string(schemaJSON) + "\nUntrusted content:\nx"})
	if err != nil {
		t.Fatalf("extraction reply: %v", err)
	}
	var wire struct {
		SchemaID      string `json:"schema_id"`
		SchemaVersion string `json:"schema_version"`
		Values        []struct {
			Name      string `json:"name"`
			ValueJSON string `json:"value_json"`
			Location  string `json:"location"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(extraction), &wire); err != nil {
		t.Fatalf("decode extraction reply: %v", err)
	}
	if wire.SchemaID != "worker.v1" || wire.SchemaVersion != "1" || len(wire.Values) != 3 ||
		wire.Values[0].Name != "state" || wire.Values[0].ValueJSON != `""` ||
		wire.Values[1].Name != "n" || wire.Values[1].ValueJSON != "0" ||
		wire.Values[2].Name != "active" || wire.Values[2].ValueJSON != "false" || wire.Values[0].Location != "content:1" {
		t.Fatalf("extraction wire = %+v", wire)
	}
	if _, err := defaultFakeReply(2, schemaflux.CompletionRequest{UserPrompt: "Extract only the declared fields\nno schema here"}); err == nil {
		t.Fatal("an extraction prompt without a schema answered")
	}
	phrase, err := defaultFakeReply(3, schemaflux.CompletionRequest{UserPrompt: "Goal: Reply with exactly: Your request is ready.\nExpected output: x"})
	var phraseOutput struct {
		Text string `json:"text"`
	}
	if err == nil {
		err = json.Unmarshal([]byte(phrase), &phraseOutput)
	}
	if err != nil || phraseOutput.Text != "Your request is ready." || strings.Contains(phraseOutput.Text, "Draft reply") || strings.Contains(phraseOutput.Text, "confirmed plan") {
		t.Fatalf("exact-phrase reply = %q, %v", phrase, err)
	}
}

func TestInitializeConfiguredDefaultProvider(t *testing.T) {
	restoreDefaultClient(t)
	schemaflux.SetDefaultClient(nil)
	t.Setenv("SCHEMAFLUX_PROVIDER", "local")
	t.Setenv("SCHEMAFLUX_API_KEY", "")
	if err := InitializeConfiguredDefaultProvider(); err != nil {
		t.Fatalf("InitializeConfiguredDefaultProvider: %v", err)
	}
	if client := schemaflux.GetDefaultClient(); client == nil {
		t.Fatal("configured initialization did not install a SchemaFlux client")
	} else if err := client.Err(); err != nil {
		t.Fatalf("configured initialization installed an unusable client: %v", err)
	}
}

func TestInitializeConfiguredDefaultProviderUsesConfiguredEndpoint(t *testing.T) {
	restoreDefaultClient(t)
	var calls atomic.Int32
	handlerErrors := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
			handlerErrors <- fmt.Sprintf("request = %s %s, want POST Responses endpoint", r.Method, r.URL.Path)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			handlerErrors <- "request did not carry authorization"
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-test","status":"completed","model":"test","output":[{"type":"message","content":[{"type":"output_text","text":"{\"answer\":\"configured\"}"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("SCHEMAFLUX_PROVIDER", "openai")
	t.Setenv("SCHEMAFLUX_API_KEY", "synthetic-test-key")
	t.Setenv("SCHEMAFLUX_OPENAI_BASE_URL", server.URL)
	if err := InitializeConfiguredDefaultProvider(); err != nil {
		t.Fatalf("InitializeConfiguredDefaultProvider: %v", err)
	}
	got, err := schemaflux.Generate[struct {
		Answer string `json:"answer"`
	}]("return a typed answer", schemaflux.NewGenerateOptions())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	select {
	case handlerErr := <-handlerErrors:
		t.Fatal(handlerErr)
	default:
	}
	if got.Answer != "configured" || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d, want configured/1", got, calls.Load())
	}
}

func TestInitializeConfiguredDefaultProviderRejectsInvalidProvider(t *testing.T) {
	restoreDefaultClient(t)
	t.Setenv("SCHEMAFLUX_PROVIDER", "not-a-provider")
	t.Setenv("SCHEMAFLUX_API_KEY", "synthetic-test-key")
	if err := InitializeConfiguredDefaultProvider(); err == nil {
		t.Fatal("invalid configured provider initialized successfully")
	}
}
