package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// chattoneProvider is a deterministic stand-in for the model provider's HTTP
// API. It is an httptest server: no network, no key, no cost. It records what
// was sent and answers with a style-dependent rewrite of the draft it finds in
// the delimited data.
type chattoneProvider struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []chattoneProviderRequest
	status   int
	// rewrite replaces the default deterministic "model" when set.
	rewrite func(system, user string) string
}

type chattoneProviderRequest struct {
	Authorization, System, User, Raw string
	Store                            bool
}

func newChattoneProvider(t *testing.T) *chattoneProvider {
	t.Helper()
	p := &chattoneProvider{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses/input_tokens" {
			io.WriteString(w, `{"object":"response.input_tokens","input_tokens":20}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Store bool `json:"store"`
			Input []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"input"`
		}
		_ = json.Unmarshal(raw, &body)
		req := chattoneProviderRequest{Authorization: r.Header.Get("Authorization"), Store: body.Store, Raw: string(raw)}
		for _, m := range body.Input {
			switch m.Role {
			case "system":
				req.System = m.Content
			case "user":
				req.User = m.Content
			}
		}
		p.mu.Lock()
		p.requests = append(p.requests, req)
		status := p.status
		p.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"unavailable"}}`)
			return
		}
		rewrite := p.rewrite
		if rewrite == nil {
			rewrite = chattoneFakeRewrite
		}
		out, _ := json.Marshal(rewrite(req.System, req.User))
		io.WriteString(w, `{"id":"response-chattone","model":"test-model","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":`+string(out)+`}]}]}`)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *chattoneProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *chattoneProvider) last() chattoneProviderRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return chattoneProviderRequest{}
	}
	return p.requests[len(p.requests)-1]
}

// chattoneFakeRewrite is the deterministic "model": it removes an insult and a
// filler word, adds a style-specific frame, and, when the draft carries the
// marker FLIP, also drops a " not " so the meaning changes.
func chattoneFakeRewrite(system, user string) string {
	raw := strings.TrimSuffix(strings.TrimPrefix(user, "<untrusted_data>\n"), "\n</untrusted_data>")
	var data struct {
		Draft string `json:"draft"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil {
		return ""
	}
	cleaned := strings.NewReplacer("You idiot, ", "", " really", "").Replace(data.Draft)
	if strings.Contains(data.Draft, "FLIP") {
		return strings.ReplaceAll(cleaned, " not ", " ")
	}
	switch {
	case strings.Contains(system, "neutral, courteous"):
		return "Hello, " + cleaned + " Thank you."
	case strings.Contains(system, "warm, positive"):
		return "Hi! " + cleaned + " Thanks so much!"
	default:
		return strings.ReplaceAll(cleaned, "Please ", "")
	}
}

type chattoneAuditSpy struct {
	agentaudit.Store
	mu      sync.Mutex
	entries []agentaudit.Entry
}

func (s *chattoneAuditSpy) Append(ctx context.Context, e agentaudit.Entry) (agentaudit.Record, error) {
	s.mu.Lock()
	s.entries = append(s.entries, e)
	s.mu.Unlock()
	return s.Store.Append(ctx, e)
}

func chattoneEvaluation() agentmodel.ModelEvaluation {
	return agentmodel.ModelEvaluation{AgentVersionDigest: ChattoneAgentVersionDigest(), SuiteDigest: "sha256:chat-writing-style-suite", Passed: true}
}

// chattoneTestDeployment is the approval document for the writing-style task,
// derived from the shared deployment fixture and pointed at the fake provider.
func chattoneTestDeployment(t *testing.T, provider *chattoneProvider) PersonaModelDeployment {
	t.Helper()
	base := modelDeploymentFixture(t)
	base.BaseURL = provider.server.URL
	dep, err := NewChattoneDeployment(base, chattoneEvaluation(), 20*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

func chattoneBudgetPolicy(userSteps int64) agentbudget.Policy {
	p := agentBudgetPolicy()
	p.UserDaily.Steps = userSteps
	return p
}

type chattoneGatewayStack struct {
	provider *chattoneProvider
	model    ChattoneGatewayModel
	binding  *ChattoneDeploymentBinding
	ledger   *agentbudget.Ledger
	audit    *chattoneAuditSpy
	evidence *ChattoneModelEvidence
	dep      PersonaModelDeployment
}

func newChattoneGatewayStack(t *testing.T, policy agentbudget.Policy) chattoneGatewayStack {
	t.Helper()
	provider := newChattoneProvider(t)
	dep := chattoneTestDeployment(t, provider)
	ledger, err := agentbudget.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	spy := &chattoneAuditSpy{Store: agentaudit.NewMemoryStore()}
	evidence := &ChattoneModelEvidence{Audit: spy, Now: time.Now}
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	composed, _, err := ComposePersonaRuntimeTypedModel(dep, "test-only-provider-key", material.OutputSeed, material.WorkloadSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewChattoneDeploymentBinding(dep, composed.Leases, ledger, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return chattoneGatewayStack{provider: provider, model: ChattoneGatewayModel{Gateway: composed.Gateway, Binding: binding}, binding: binding, ledger: ledger, audit: spy, evidence: evidence, dep: dep}
}

func chattoneTestPrompt(tenant, draft string, context ...string) chatrewrite.Prompt {
	data, _ := json.Marshal(struct {
		Draft           string   `json:"draft"`
		RegisterContext []string `json:"register_context"`
	}{draft, context})
	return chatrewrite.Prompt{Identity: chatrewrite.Identity{Tenant: tenant, Person: "alice", Conversation: "room"}, TaskProfile: chatrewrite.TaskProfileID,
		Instruction: "Rewrite tone only. " + chatrewrite.DefaultStyles()[0].Instruction, Data: "<untrusted_data>\n" + string(data) + "\n</untrusted_data>"}
}

func TestTodo_CHATTONE_004_Deployment(t *testing.T) {
	provider := newChattoneProvider(t)
	dep := chattoneTestDeployment(t, provider)
	raw, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePersonaModelDeployment(raw)
	if err != nil {
		t.Fatalf("the generated approval document does not load: %v", err)
	}
	q, err := chattoneQualify(loaded)
	if err != nil {
		t.Fatal(err)
	}
	task, outputDigest, toolDigest := ChattoneModelEvidenceProfile()
	profile := q.profile
	if len(loaded.Profiles) != 1 || len(loaded.Terms) != 1 || len(loaded.Destinations) != 1 || len(loaded.Clearances) != 1 ||
		profile.TaskProfileIDs[0] != task.ID || profile.SemanticsDigest != task.SemanticsDigest || profile.OutputSchemaDigest != outputDigest || profile.ToolSchemaDigest != toolDigest ||
		profile.Evaluation != chattoneEvaluation() || profile.MaxLatency != 20*time.Second || profile.MaxCostMicros != 20000 || agentmodel.ModelProfileDigest(profile) != profile.ProfileDigest {
		t.Fatalf("approval document shape: %+v", profile)
	}
	if loaded.Destinations[0].Name != profile.ID || len(loaded.Destinations[0].Purposes) != 1 || loaded.Destinations[0].Purposes[0] != ChattonePurpose {
		t.Fatal("destination does not carry only the writing-style purpose")
	}
	for _, scope := range loaded.Credential.Scopes {
		if scope.Purpose != ChattonePurpose || scope.Destination != profile.ID {
			t.Fatal("credential scope outside the writing-style purpose", scope)
		}
	}
	sources := map[string]bool{}
	for _, rule := range loaded.Terms[0].SourceRules {
		sources[rule.Class] = true
	}
	if len(sources) != 2 || !sources[chattoneSourceInstruction] || !sources[chattoneSourceDraft] {
		t.Fatalf("terms approve more or less than the two writing-style fields: %v", sources)
	}
	if q.limits.MaxInputTokens != chattoneMaxInputTokens || q.limits.MaxOutputTokens < 500 || q.limits.MaxOutputTokens > chattoneMaxOutputTokens || q.limits.MaxCostMicros != 20000 || q.region != "us" {
		t.Fatalf("limits %+v region %q", q.limits, q.region)
	}

	// A deployment that does not qualify a model for this task is not eligible.
	if _, err := chattoneQualify(modelDeploymentFixture(t)); !errors.Is(err, errChattoneBinding) {
		t.Fatalf("an unqualified deployment qualified: %v", err)
	}
	for name, mutate := range map[string]func(*PersonaModelDeployment){
		"evaluation not passed": func(d *PersonaModelDeployment) { d.Profiles[0].Evaluation.Passed = false },
		"evaluated another instruction version": func(d *PersonaModelDeployment) {
			d.Profiles[0].Evaluation.AgentVersionDigest = "hcm-chat-writing-style/other"
		},
		"other semantics":      func(d *PersonaModelDeployment) { d.Profiles[0].SemanticsDigest = "other" },
		"other output":         func(d *PersonaModelDeployment) { d.Profiles[0].OutputSchemaDigest = "other" },
		"slower than the task": func(d *PersonaModelDeployment) { d.Profiles[0].MaxLatency = 2 * time.Minute },
		"dearer than the task": func(d *PersonaModelDeployment) { d.Profiles[0].MaxCostMicros = 20001 },
		"no draft source rule": func(d *PersonaModelDeployment) {
			d.Terms[0].SourceRules = d.Terms[0].SourceRules[:1]
		},
		"no purpose":   func(d *PersonaModelDeployment) { d.Destinations[0].Purposes = []string{"persona.reply"} },
		"unapproved":   func(d *PersonaModelDeployment) { d.Terms[0].Approved = false },
		"two profiles": func(d *PersonaModelDeployment) { d.Profiles = append(d.Profiles, d.Profiles[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := loaded
			bad.Profiles = append([]agentmodel.ModelProfile(nil), loaded.Profiles...)
			bad.Terms = append([]agentegress.ProviderTerms(nil), loaded.Terms...)
			bad.Terms[0].SourceRules = append([]agentegress.ProviderSourceRule(nil), loaded.Terms[0].SourceRules...)
			bad.Destinations = append(bad.Destinations[:0:0], loaded.Destinations...)
			mutate(&bad)
			if _, err := chattoneQualify(bad); !errors.Is(err, errChattoneBinding) {
				t.Fatalf("accepted: %v", err)
			}
		})
	}

	// The generator never invents evidence and never exceeds the task's bounds.
	base := modelDeploymentFixture(t)
	for name, tc := range map[string]struct {
		evaluation agentmodel.ModelEvaluation
		latency    time.Duration
		cost       int64
	}{
		"failed evaluation": {agentmodel.ModelEvaluation{AgentVersionDigest: ChattoneAgentVersionDigest(), SuiteDigest: "s"}, time.Second, 1000},
		"another version":   {agentmodel.ModelEvaluation{AgentVersionDigest: "hcm-chat-writing-style/other", SuiteDigest: "s", Passed: true}, time.Second, 1000},
		"no suite digest":   {agentmodel.ModelEvaluation{AgentVersionDigest: ChattoneAgentVersionDigest(), Passed: true}, time.Second, 1000},
		"slow":              {chattoneEvaluation(), 2 * time.Minute, 1000},
		"expensive":         {chattoneEvaluation(), time.Second, ChattoneTaskProfile().MaxCostMicros + 1},
		"free":              {chattoneEvaluation(), time.Second, 0},
	} {
		if _, err := NewChattoneDeployment(base, tc.evaluation, tc.latency, tc.cost); !errors.Is(err, errChattoneBinding) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTodo_CHATTONE_004_ProductionGateway(t *testing.T) {
	stack := newChattoneGatewayStack(t, agentBudgetPolicy())
	ctx := context.Background()
	prompt := chattoneTestPrompt("tenant-a", "You idiot, the deploy really broke prod again.", "Keep replies short")
	if !stack.model.Ready() {
		t.Fatal("a bound model reports not ready")
	}
	text, err := stack.model.Rewrite(ctx, prompt)
	if err != nil || text != "Hello, the deploy broke prod again. Thank you." {
		t.Fatalf("rewrite %q %v", text, err)
	}
	sent := stack.provider.last()
	if stack.provider.calls() != 1 || sent.Authorization != "Bearer test-only-provider-key" || sent.Store || sent.System != prompt.Instruction || sent.User != prompt.Data {
		t.Fatalf("what left the deployment: calls %d %+v", stack.provider.calls(), sent)
	}

	// Cost is recorded where the call is made, in the shared ledger, against the
	// writer's per-day task: one step and the priced tokens (20 in * 2 + 10 out * 8).
	taskID := ChattoneBudgetTaskID(prompt.Identity, time.Now())
	var task *agentbudget.TaskSnapshot
	snapshot := stack.ledger.Snapshot()
	for i := range snapshot.Tasks {
		if snapshot.Tasks[i].ID == taskID {
			task = &snapshot.Tasks[i]
		}
	}
	if task == nil || task.TenantID != "tenant-a" || task.UserID != "alice" || task.Used.Steps != 1 || task.Used.Tokens != 30 || task.Used.SpendMicros != 120 || task.Reserved.Steps != 0 || task.Limit.Steps != chattoneDailyModelCalls {
		t.Fatalf("budget task %+v", task)
	}

	// Every routing decision is in the append-only audit chain, as the person's
	// own act, with no draft text.
	stack.audit.mu.Lock()
	entries := append([]agentaudit.Entry(nil), stack.audit.entries...)
	stack.audit.mu.Unlock()
	if len(entries) != 1 || entries[0].Action != "model.route" || entries[0].TenantID != "tenant-a" || entries[0].Actor.UserID != "alice" || entries[0].Actor.TaskID != taskID || entries[0].Kind != agentaudit.EventModelCall {
		t.Fatalf("audit entries %+v", entries)
	}
	for _, f := range entries[0].Fields {
		if strings.Contains(f.Value, "idiot") || strings.Contains(f.Value, "Keep replies short") {
			t.Fatal("the route record carries message text")
		}
	}

	// A second call mints its own lease and takes its own step.
	if _, err := stack.model.Rewrite(ctx, prompt); err != nil || stack.provider.calls() != 2 {
		t.Fatalf("second call: %v calls %d", err, stack.provider.calls())
	}

	t.Run("a workspace the credential does not cover reaches nothing", func(t *testing.T) {
		before := stack.provider.calls()
		if _, err := stack.model.Rewrite(ctx, chattoneTestPrompt("tenant-b", "Fix it now please.")); err == nil || stack.provider.calls() != before {
			t.Fatalf("an out-of-scope workspace was served: %v", err)
		}
	})
	t.Run("a prompt that is not the writing-style task is refused before binding", func(t *testing.T) {
		bad := prompt
		bad.TaskProfile = "other"
		before := stack.provider.calls()
		if _, err := stack.model.Rewrite(ctx, bad); !errors.Is(err, chatrewrite.ErrUnavailable) || stack.provider.calls() != before {
			t.Fatalf("other task: %v", err)
		}
	})
	t.Run("provider failure is not a limit and records no spend", func(t *testing.T) {
		stack.provider.mu.Lock()
		stack.provider.status = http.StatusServiceUnavailable
		stack.provider.mu.Unlock()
		defer func() {
			stack.provider.mu.Lock()
			stack.provider.status = 0
			stack.provider.mu.Unlock()
		}()
		spent := func() agentbudget.Usage {
			for _, task := range stack.ledger.Snapshot().Tasks {
				if task.ID == taskID {
					return task.Used
				}
			}
			return agentbudget.Usage{}
		}
		before := spent()
		_, err := stack.model.Rewrite(ctx, prompt)
		if err == nil || errors.Is(err, chatrewrite.ErrLimit) {
			t.Fatalf("provider outage: %v", err)
		}
		if after := spent(); after.SpendMicros != before.SpendMicros || after.Tokens != before.Tokens {
			t.Fatalf("a failed call was charged: %+v -> %+v", before, after)
		}
	})
}

func TestTodo_CHATTONE_004_Evidence(t *testing.T) {
	stack := newChattoneGatewayStack(t, agentBudgetPolicy())
	prompt := chattoneTestPrompt("tenant-a", "Please fix it now.")
	// Build the request exactly as the gateway model does, then tamper with what
	// the evidence is asked to vouch for.
	req, err := stack.binding.BindWritingStyle(context.Background(), prompt.Identity, ChattoneTaskProfile())
	if err != nil {
		t.Fatal(err)
	}
	provenance := []string{"chat-writing-style:" + req.Route.TraceID}
	req.Dispatch.Outbound.DeclaredFields = []string{"model.message.1"}
	req.Dispatch.Outbound.Fields = []agentegress.Field{{Name: "model.message.1", Value: prompt.Data, Class: "INTERNAL", Provenance: provenance}}
	req.Dispatch.FieldSources = map[string]string{"model.message.1": chattoneSourceDraft}
	ctx := context.WithValue(context.Background(), chattoneEvidenceKey{}, req)
	good := agentegress.SourceClassificationRequest{Tenant: "tenant-a", Purpose: ChattonePurpose, FieldName: "model.message.1", SourceClass: chattoneSourceDraft, DataClass: "INTERNAL", Provenance: provenance, ValueDigest: chattoneDigest(prompt.Data), MessageRole: agentmodel.RoleUser}
	if err := stack.evidence.VerifySourceClassification(ctx, good); err != nil {
		t.Fatalf("honest source refused: %v", err)
	}
	for name, mutate := range map[string]func(*agentegress.SourceClassificationRequest){
		"other tenant":      func(s *agentegress.SourceClassificationRequest) { s.Tenant = "tenant-b" },
		"other purpose":     func(s *agentegress.SourceClassificationRequest) { s.Purpose = "persona.reply" },
		"other field":       func(s *agentegress.SourceClassificationRequest) { s.FieldName = "model.message.0" },
		"other source":      func(s *agentegress.SourceClassificationRequest) { s.SourceClass = "persona-invoking-post" },
		"lower class":       func(s *agentegress.SourceClassificationRequest) { s.DataClass = "PUBLIC" },
		"other bytes":       func(s *agentegress.SourceClassificationRequest) { s.ValueDigest = chattoneDigest("other") },
		"other provenance":  func(s *agentegress.SourceClassificationRequest) { s.Provenance = []string{"chat-writing-style:other"} },
		"draft as the role": func(s *agentegress.SourceClassificationRequest) { s.MessageRole = agentmodel.RoleSystem },
	} {
		s := good
		mutate(&s)
		if err := stack.evidence.VerifySourceClassification(ctx, s); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	if err := stack.evidence.VerifySourceClassification(context.Background(), good); err == nil {
		t.Fatal("a source was verified without a request built for this call")
	}
	if err := (*ChattoneModelEvidence)(nil).VerifySourceClassification(ctx, good); err == nil {
		t.Fatal("nil evidence verified")
	}
	if err := stack.evidence.RecordRoute(context.Background(), agentmodel.RouteRecord{TraceID: req.Route.TraceID, Digest: "d"}); err == nil {
		t.Fatal("a route was recorded without a request built for this call")
	}
	if err := stack.evidence.RecordRoute(ctx, agentmodel.RouteRecord{TraceID: "other", Digest: "d"}); err == nil {
		t.Fatal("a route for another trace was recorded")
	}
	if err := stack.evidence.RecordRoute(ctx, agentmodel.RouteRecord{TraceID: req.Route.TraceID, AgentVersionDigest: req.Route.Pin.AgentVersionDigest, TaskProfileID: req.Route.Task.ID, Region: req.Route.Task.Region, Selected: agentmodel.ModelSelection{ProfileID: "other"}, Digest: "d"}); err == nil {
		t.Fatal("a route to another model was recorded")
	}
}

func TestTodo_CHATTONE_004_BudgetExhausted(t *testing.T) {
	// The platform's user-day ceiling is the shared one: one step a day here.
	stack := newChattoneGatewayStack(t, chattoneBudgetPolicy(1))
	prompt := chattoneTestPrompt("tenant-a", "Please fix the broken deploy now.")
	if _, err := stack.model.Rewrite(context.Background(), prompt); err != nil {
		t.Fatal(err)
	}
	_, err := stack.model.Rewrite(context.Background(), prompt)
	if !errors.Is(err, chatrewrite.ErrLimit) || stack.provider.calls() != 1 {
		t.Fatalf("a spent budget reached the provider or was not reported as a limit: %v calls %d", err, stack.provider.calls())
	}
	// And through the rewrite service the writer sees the same plain limit, no draft.
	service := &chatrewrite.Service{Registry: chatrewrite.NewRegistry(), Model: stack.model, Policy: chattoneAllowAll{}, Meaning: ChattoneMeaningGuard{}, Outbound: mustChattoneOutbound(t), Ledger: chatrewrite.NewMemoryLedger(60)}
	out, err := service.Rewrite(context.Background(), chatrewrite.Request{Identity: prompt.Identity, Draft: "Please fix the broken deploy now.", StyleID: "professional"})
	if !errors.Is(err, chatrewrite.ErrLimit) || out != "" || stack.provider.calls() != 1 {
		t.Fatalf("service: %q %v calls %d", out, err, stack.provider.calls())
	}
}

type chattoneAllowAll struct{}

func (chattoneAllowAll) Accept(context.Context, chatrewrite.Identity, string) (bool, error) {
	return true, nil
}

func mustChattoneOutbound(t *testing.T) *ChattoneOutboundVerifier {
	t.Helper()
	v, err := NewChattoneOutboundVerifier()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTodo_CHATTONE_004_ServedNotProvisioned(t *testing.T) {
	ctx := context.Background()
	chatOK := composedChat{service: (*chat.Service)(nil), renderings: &ChatRenderingSurface{}, filters: &chatfilter.Service{}}
	ledger, err := agentbudget.New(agentBudgetPolicy())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &agentRuntime{Budget: ledger, Audit: agentaudit.NewMemoryStore()}
	key := func(k string) string {
		if k == "MODEL_API_KEY" {
			return "test-only-provider-key"
		}
		return ""
	}
	t.Chdir(t.TempDir())
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(dep PersonaModelDeployment) string {
		raw, err := json.Marshal(dep)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "deployment.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, tc := range map[string]struct {
		in     chattoneServeInput
		reason string
		fail   bool
	}{
		"chat not composed":  {chattoneServeInput{Config: ServeConfig{}, Runtime: runtime, Env: key}, "not composed", false},
		"no document":        {chattoneServeInput{Config: ServeConfig{}, Runtime: runtime, Chat: chatOK, Env: key}, EnvChatWritingStyleModelConfigFile, false},
		"local without file": {chattoneServeInput{Config: ServeConfig{Profile: ServeProfileLocalDev}, Runtime: runtime, Chat: chatOK, Env: key}, EnvChatWritingStyleModelConfigFile, false},
		"no budget or audit": {chattoneServeInput{Config: ServeConfig{}, Runtime: &agentRuntime{}, Chat: chatOK, Env: func(k string) string { return "x" }}, "budget ledger", false},
		"no provider key": {chattoneServeInput{Config: ServeConfig{}, Runtime: runtime, Chat: chatOK, Env: func(k string) string {
			return map[bool]string{true: "/does/not/matter"}[k == EnvChatWritingStyleModelConfigFile]
		}}, "MODEL_API_KEY", false},
		"unreadable document": {chattoneServeInput{Config: ServeConfig{}, Runtime: runtime, Chat: chatOK, Env: func(k string) string {
			return map[string]string{EnvChatWritingStyleModelConfigFile: filepath.Join(t.TempDir(), "missing.json"), "MODEL_API_KEY": "k"}[k]
		}}, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			svc, reason, err := composeServedChattone(ctx, tc.in)
			if svc != nil || (err != nil) != tc.fail || !strings.Contains(reason, tc.reason) {
				t.Fatalf("service %v reason %q err %v", svc, reason, err)
			}
		})
	}

	// A deployment that qualifies no model for the task is the plain "not
	// provisioned" state, and one for another cell is a configuration error.
	provider := newChattoneProvider(t)
	unqualified := modelDeploymentFixture(t)
	unqualified.BaseURL = provider.server.URL
	cfg := ServeConfig{CellID: unqualified.Worker.Cell, PersonaOutputSigningSeed: material.OutputSeed, PersonaWorkloadSigningSeed: material.WorkloadSeed}
	env := func(path string) func(string) string {
		return func(k string) string {
			switch k {
			case EnvChatWritingStyleModelConfigFile:
				return path
			case "MODEL_API_KEY":
				return "test-only-provider-key"
			}
			return ""
		}
	}
	svc, reason, err := composeServedChattone(ctx, chattoneServeInput{Config: cfg, Runtime: runtime, Chat: chatOK, Env: env(write(unqualified))})
	if svc != nil || err != nil || !strings.Contains(reason, "no model profile is qualified") {
		t.Fatalf("unqualified deployment: %v %q %v", svc, reason, err)
	}
	qualified := chattoneTestDeployment(t, provider)
	cfg.CellID = "another-cell"
	if svc, _, err := composeServedChattone(ctx, chattoneServeInput{Config: cfg, Runtime: runtime, Chat: chatOK, Env: env(write(qualified))}); svc != nil || !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("a deployment for another cell was composed: %v %v", svc, err)
	}
	cfg.CellID = qualified.Worker.Cell
	svc, reason, err = composeServedChattone(ctx, chattoneServeInput{Config: cfg, Runtime: runtime, Chat: chatOK, Env: env(write(qualified))})
	if err != nil || svc == nil || reason != "" {
		t.Fatalf("a qualified deployment was not composed: %v %q %v", svc, reason, err)
	}
	// Composed but opt-in: no workspace is on, the controls are not offered, and
	// nothing is sent to the provider.
	if _, enabled := svc.Rewrite.Registry.Styles("tenant-a"); enabled || provider.calls() != 0 {
		t.Fatal("composition turned a workspace on or called the provider")
	}
	if !svc.Rewrite.Model.(ChattoneGatewayModel).Ready() {
		t.Fatal("a qualified model is not ready")
	}
}
