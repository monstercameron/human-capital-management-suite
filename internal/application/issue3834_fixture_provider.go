package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// EnvLocalModelProvider selects the deterministic model provider in the local
// development profile ("fixture"). Every model-backed Chat feature (agent
// answers in a conversation, ambient screening, writing styles, translation
// through the governed gateway, private tasks) then calls a loopback provider
// inside this process instead of a real one, so the whole flow can be driven
// without MODEL_API_KEY. The provider sits at the very bottom of the stack: the
// gateway, lease, egress policy, data classification, budget ledger and audit
// chain all still run, only the HTTP answer is canned. It is refused in every
// other profile.
const EnvLocalModelProvider = "HCMNEXT_LOCAL_MODEL_PROVIDER"

const (
	localModelProviderFixture = "fixture"
	// localModelFixtureKey is the credential the loopback provider accepts. It is
	// not a secret: it only keeps unrelated local programs from using the endpoint.
	localModelFixtureKey = "fixture-local-development-key"
	// LocalModelFixtureLabel marks every canned persona answer so nobody mistakes
	// it for a real model's output.
	LocalModelFixtureLabel = "Fixture reply (local development)"
)

// localModelFixtureRequested reports whether the environment asks for the
// fixture provider, whatever the profile.
func localModelFixtureRequested(env func(string) string) bool {
	return env != nil && strings.EqualFold(strings.TrimSpace(env(EnvLocalModelProvider)), localModelProviderFixture)
}

// localModelAPIKeyMissing is "MODEL_API_KEY is not set" for the places that
// check it up front: a fixture run needs no key.
func localModelAPIKeyMissing(env func(string) string) bool {
	return env("MODEL_API_KEY") == "" && !localModelFixtureRequested(env)
}

var (
	localModelFixtureOnce sync.Once
	localModelFixtureURL  string
	localModelFixtureErr  error
)

// localModelFixtureEndpoint starts (once per process) and returns the loopback
// provider's base URL and credential. It refuses any profile but local
// development, so a production process that is handed the switch fails closed.
func localModelFixtureEndpoint(cfg ServeConfig, env func(string) string) (string, string, error) {
	if !localModelFixtureRequested(env) {
		return "", "", nil
	}
	if cfg.Profile != ServeProfileLocalDev {
		return "", "", modelConfigurationError(EnvLocalModelProvider + "=fixture is only available in the local development profile")
	}
	localModelFixtureOnce.Do(func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			localModelFixtureErr = fmt.Errorf("start the fixture model provider: %w", err)
			return
		}
		server := &http.Server{Handler: LocalModelFixtureHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = server.Serve(listener) }()
		localModelFixtureURL = "http://" + listener.Addr().String()
	})
	if localModelFixtureErr != nil {
		return "", "", localModelFixtureErr
	}
	return localModelFixtureURL, localModelFixtureKey, nil
}

// LocalModelFixtureHandler is the deterministic provider: the subset of the
// Responses API the platform's adapter speaks (a completion and an input token
// count). The same request always gets the same answer, nothing is stored and
// nobody is called.
func LocalModelFixtureHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/responses/input_tokens", fixtureAuthorized(func(w http.ResponseWriter, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "response.input_tokens", "input_tokens": fixtureTokens(string(body))})
	}))
	mux.HandleFunc("/responses", fixtureAuthorized(func(w http.ResponseWriter, body []byte) {
		var request fixtureRequest
		if json.Unmarshal(body, &request) != nil || strings.TrimSpace(request.Model) == "" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		text := fixtureAnswer(request)
		digest := sha256.Sum256(body)
		input, output := fixtureTokens(string(body)), fixtureTokens(text)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "fixture-" + hex.EncodeToString(digest[:8]), "model": request.Model, "status": "completed",
			"usage":  map[string]any{"input_tokens": input, "output_tokens": output, "total_tokens": input + output},
			"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": text}}}},
		})
	}))
	return mux
}

func fixtureAuthorized(next func(http.ResponseWriter, []byte)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+localModelFixtureKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, "unreadable request", http.StatusBadRequest)
			return
		}
		next(w, body)
	}
}

type fixtureRequest struct {
	Model string `json:"model"`
	Input []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"input"`
	Text *struct {
		Format struct {
			Type   string          `json:"type"`
			Schema json.RawMessage `json:"schema"`
		} `json:"format"`
	} `json:"text"`
}

// fixtureTokens is a stable stand-in for a tokenizer: about four bytes a token.
func fixtureTokens(text string) int64 { return int64(len(text)/4) + 1 }

func (r fixtureRequest) lastUser() string {
	for i := len(r.Input) - 1; i >= 0; i-- {
		if r.Input[i].Role == "user" && strings.TrimSpace(r.Input[i].Content) != "" {
			return r.Input[i].Content
		}
	}
	return ""
}

func (r fixtureRequest) developer() string {
	for _, m := range r.Input {
		if m.Role == "developer" {
			return m.Content
		}
	}
	return ""
}

// fixtureUntrustedJSON strips the delimiter a task wraps its data in and returns the JSON
// inside, or "" when the content is not delimited data.
func fixtureUntrustedJSON(content string) string {
	for _, pair := range [][2]string{{agentDocumentReferenceDataBegin, agentDocumentReferenceDataEnd}, {"<untrusted_data>", "</untrusted_data>"}} {
		if strings.HasPrefix(content, pair[0]) && strings.HasSuffix(content, pair[1]) {
			return strings.TrimSpace(content[len(pair[0]) : len(content)-len(pair[1])])
		}
	}
	return ""
}

// fixtureAnswer picks the answer for a request by the output contract the
// platform pinned, which is what tells the tasks apart.
func fixtureAnswer(r fixtureRequest) string {
	format, schema := "", json.RawMessage(nil)
	if r.Text != nil {
		format, schema = r.Text.Format.Type, r.Text.Format.Schema
	}
	user := r.lastUser()
	data := fixtureUntrustedJSON(user)
	switch format {
	case "json_schema":
		var shape struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(schema, &shape) != nil {
			return "{}"
		}
		if shape.Type == "string" {
			return fixtureJSON(fixturePersonaReply(user))
		}
		_, hasSource := shape.Properties["source_language"]
		_, hasText := shape.Properties["text"]
		_, hasVerdict := shape.Properties["meaning_preserved"]
		switch {
		case hasSource && hasText && hasVerdict:
			return fixtureTranslation(data)
		case hasText && hasVerdict:
			return fixtureRewrite(data)
		}
		return fixtureGeneric(schema)
	case "json_object":
		return fixtureAmbient(r.developer(), data)
	}
	return fixturePersonaReply(user)
}

func fixtureJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// fixturePersonaReply is the answer to a question put to an agent. It names
// itself as a fixture and repeats nothing the asker wrote, so a mention or a
// link in the question can never be echoed into the channel.
func fixturePersonaReply(question string) string {
	digest := sha256.Sum256([]byte(question))
	return LocalModelFixtureLabel + ": no AI model was called, so this is a canned answer. Connect a real model to get a real one. (reference " + hex.EncodeToString(digest[:3]) + ")"
}

// fixtureTranslation labels the text with its target language, as the
// translation test engine does; protected placeholders pass through untouched.
func fixtureTranslation(data string) string {
	var in struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Text   string `json:"text"`
	}
	if json.Unmarshal([]byte(data), &in) != nil || strings.TrimSpace(in.Text) == "" {
		return fixtureJSON(map[string]any{"source_language": "und", "text": "[fixture]", "meaning_preserved": false})
	}
	source := strings.TrimSpace(in.Source)
	if source == "" || source == "auto" || source == "und" {
		source = "en"
	}
	if in.Target == "" || in.Target == source {
		return fixtureJSON(map[string]any{"source_language": source, "text": in.Text, "meaning_preserved": true})
	}
	return fixtureJSON(map[string]any{"source_language": source, "text": "[" + in.Target + "] " + in.Text, "meaning_preserved": true})
}

// fixtureRewrite tidies the draft (a capital letter and a closing full stop)
// and changes nothing else, so the local meaning guard and the placeholder
// check pass and the preview is visibly the same message.
func fixtureRewrite(data string) string {
	var in struct {
		Draft string `json:"draft"`
	}
	if json.Unmarshal([]byte(data), &in) != nil || strings.TrimSpace(in.Draft) == "" {
		return fixtureJSON(map[string]any{"text": "-", "meaning_preserved": false})
	}
	text := strings.TrimSpace(in.Draft)
	if first, size := utf8.DecodeRuneInString(text); first != utf8.RuneError && strings.ToUpper(string(first)) != string(first) && !strings.HasPrefix(text, "⟦") {
		text = strings.ToUpper(string(first)) + text[size:]
	}
	if last, _ := utf8.DecodeLastRuneInString(text); !strings.ContainsRune(".!?…:;)⟧\"'`", last) {
		text += "."
	}
	return fixtureJSON(map[string]any{"text": text, "meaning_preserved": true})
}

// fixtureAmbient screens a message with the same deterministic extractor the
// preparation fixtures use, so the offers it makes match the documented ones.
func fixtureAmbient(developer, data string) string {
	agent := ""
	for _, name := range []string{"task-catcher", "reminder"} {
		if developer != "" && developer == AgentUXAmbientInstructions(name) {
			agent = name
		}
	}
	var in struct {
		Message string `json:"message"`
		Zone    string `json:"author_time_zone"`
	}
	empty := map[string]any{"kind": "", "title": "", "owner": "", "date": "", "clock": "", "event": "", "explicit": false}
	if agent == "" || json.Unmarshal([]byte(data), &in) != nil {
		return fixtureJSON(empty)
	}
	p := AgentUXAmbientFixtureProposal(AgentUXAmbientMessage{Body: in.Message, Zone: in.Zone}, agent)
	if p.Kind == "" {
		return fixtureJSON(empty)
	}
	if strings.TrimSpace(p.Title) == "" {
		p.Title = in.Message
	}
	if len(p.Title) > 500 {
		p.Title = p.Title[:500]
		for !utf8.ValidString(p.Title) {
			p.Title = p.Title[:len(p.Title)-1]
		}
	}
	return fixtureJSON(map[string]any{"kind": p.Kind, "title": p.Title, "owner": p.Owner, "date": p.Date, "clock": p.Clock, "event": p.Event, "explicit": p.Explicit})
}

// fixtureGeneric is the smallest value that satisfies a pinned schema this
// provider has no purpose-built answer for.
func fixtureGeneric(schema json.RawMessage) string {
	var node any
	if json.Unmarshal(schema, &node) != nil {
		return "{}"
	}
	return fixtureJSON(fixtureValue(node, 0))
}

func fixtureValue(node any, depth int) any {
	object, ok := node.(map[string]any)
	if !ok || depth > 8 {
		return nil
	}
	if options, ok := object["enum"].([]any); ok && len(options) > 0 {
		return options[0]
	}
	switch object["type"] {
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		out := map[string]any{}
		properties, _ := object["properties"].(map[string]any)
		for name, child := range properties {
			out[name] = fixtureValue(child, depth+1)
		}
		return out
	}
	return nil
}
