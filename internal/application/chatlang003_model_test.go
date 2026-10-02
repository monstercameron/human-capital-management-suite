package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// chatlangProvider is a deterministic stand-in for the model provider's HTTP
// API: an httptest server, so no network, no key and no cost. It answers a
// translation request with the text labelled by the target language.
type chatlangProvider struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []chatlangProviderRequest
	status   int
	damage   bool
}
type chatlangProviderRequest struct {
	Authorization, System, User string
	Store                       bool
}

func newChatlangProvider(t *testing.T) *chatlangProvider {
	t.Helper()
	p := &chatlangProvider{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses/input_tokens" {
			io.WriteString(w, `{"object":"response.input_tokens","input_tokens":20}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Store bool `json:"store"`
			Input []struct{ Role, Content string }
		}
		_ = json.Unmarshal(raw, &body)
		req := chatlangProviderRequest{Authorization: r.Header.Get("Authorization"), Store: body.Store}
		for _, m := range body.Input {
			if m.Role == "system" {
				req.System = m.Content
			} else if m.Role == "user" {
				req.User = m.Content
			}
		}
		p.mu.Lock()
		p.requests = append(p.requests, req)
		status, damage := p.status, p.damage
		p.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"unavailable"}}`)
			return
		}
		var data struct{ Target, Text string }
		inner := strings.TrimSuffix(strings.TrimPrefix(req.User, "<untrusted_data>\n"), "\n</untrusted_data>")
		_ = json.Unmarshal([]byte(inner), &data)
		answer := "[" + data.Target + "] " + data.Text
		if damage {
			answer = strings.NewReplacer("⟦", "", "⟧", "").Replace(answer)
		}
		out, _ := json.Marshal(answer)
		io.WriteString(w, `{"id":"response-chatlang","model":"test-model","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":`+string(out)+`}]}]}`)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *chatlangProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}
func (p *chatlangProvider) last() chatlangProviderRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) == 0 {
		return chatlangProviderRequest{}
	}
	return p.requests[len(p.requests)-1]
}

type chatlangAuditSpy struct {
	agentaudit.Store
	mu      sync.Mutex
	entries []agentaudit.Entry
}

func (s *chatlangAuditSpy) Append(ctx context.Context, e agentaudit.Entry) (agentaudit.Record, error) {
	s.mu.Lock()
	s.entries = append(s.entries, e)
	s.mu.Unlock()
	return s.Store.Append(ctx, e)
}

func chatlangEvaluation() agentmodel.ModelEvaluation {
	return agentmodel.ModelEvaluation{AgentVersionDigest: "hcm-chat-translation/v1", SuiteDigest: "sha256:chat-translation-suite", Passed: true}
}

// chatlangTestDeployment is the approval document for the translation task,
// derived from the shared deployment fixture, pointed at the fake provider and
// covering the workspace the Chat rig uses.
func chatlangTestDeployment(t *testing.T, provider *chatlangProvider, tenant string) PersonaModelDeployment {
	t.Helper()
	base := modelDeploymentFixture(t)
	base.BaseURL = provider.server.URL
	base.Credential.Scopes = []OpenAIModelCredentialScope{{TenantID: tenant, Region: "us", Purpose: "persona.reply", Destination: base.Profiles[0].ID}}
	dep, err := NewChatlangDeployment(base, chatlangEvaluation(), 20*time.Second, 20000)
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

type chatlangGatewayStack struct {
	provider *chatlangProvider
	engine   ChatlangGatewayEngine
	binding  *ChatlangDeploymentBinding
	ledger   *agentbudget.Ledger
	audit    *chatlangAuditSpy
	evidence *ChatlangModelEvidence
	dep      PersonaModelDeployment
}

func newChatlangGatewayStack(t *testing.T, tenant string, policy agentbudget.Policy) chatlangGatewayStack {
	t.Helper()
	provider := newChatlangProvider(t)
	dep := chatlangTestDeployment(t, provider, tenant)
	ledger, err := agentbudget.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	spy := &chatlangAuditSpy{Store: agentaudit.NewMemoryStore()}
	evidence := &ChatlangModelEvidence{Audit: spy, Now: time.Now}
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	composed, _, err := ComposePersonaRuntimeTypedModel(dep, "test-only-provider-key", material.OutputSeed, material.WorkloadSeed,
		PersonaModelDeploymentDependencies{Budget: agentmodel.BudgetAdapter{Ledger: ledger}, Routes: evidence, Sources: evidence, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewChatlangDeploymentBinding(dep, composed.Leases, ledger, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return chatlangGatewayStack{provider: provider, engine: ChatlangGatewayEngine{Gateway: composed.Gateway, Binding: binding}, binding: binding, ledger: ledger, audit: spy, evidence: evidence, dep: dep}
}

func TestTodo_CHATLANG_003_Deployment(t *testing.T) {
	provider := newChatlangProvider(t)
	dep := chatlangTestDeployment(t, provider, "tenant-a")
	raw, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePersonaModelDeployment(raw)
	if err != nil {
		t.Fatalf("the generated approval document does not load: %v", err)
	}
	q, err := chatlangQualify(loaded)
	if err != nil {
		t.Fatal(err)
	}
	task := ChatlangTaskProfile()
	profile := q.profile
	if len(loaded.Profiles) != 1 || profile.TaskProfileIDs[0] != task.ID || profile.SemanticsDigest != task.SemanticsDigest || profile.OutputSchemaDigest != chatlangOutputSchemaDigest || profile.Evaluation != chatlangEvaluation() || agentmodel.ModelProfileDigest(profile) != profile.ProfileDigest {
		t.Fatalf("approval document shape: %+v", profile)
	}
	if len(loaded.Destinations) != 1 || len(loaded.Destinations[0].Purposes) != 1 || loaded.Destinations[0].Purposes[0] != ChatlangPurpose {
		t.Fatal("destination does not carry only the translation purpose")
	}
	for _, scope := range loaded.Credential.Scopes {
		if scope.Purpose != ChatlangPurpose || scope.Destination != profile.ID {
			t.Fatal("credential scope outside the translation purpose", scope)
		}
	}
	if q.limits.MaxInputTokens != chatlangMaxInputTokens || q.limits.MaxOutputTokens < 500 || q.limits.MaxCostMicros != 20000 || q.region != "us" {
		t.Fatalf("limits %+v region %q", q.limits, q.region)
	}
	if _, err := chatlangQualify(modelDeploymentFixture(t)); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("an unqualified deployment qualified: %v", err)
	}
	// The writing-style qualification is not translation's: tasks do not share evidence.
	other := loaded
	other.Profiles = append([]agentmodel.ModelProfile(nil), loaded.Profiles...)
	other.Profiles[0].TaskProfileIDs = []string{"chat-writing-style-v1"}
	if _, err := chatlangQualify(other); !errors.Is(err, errChatlangBinding) {
		t.Fatalf("a writing-style profile qualified for translation: %v", err)
	}
	for name, mutate := range map[string]func(*PersonaModelDeployment){
		"evaluation not passed": func(d *PersonaModelDeployment) { d.Profiles[0].Evaluation.Passed = false },
		"other semantics":       func(d *PersonaModelDeployment) { d.Profiles[0].SemanticsDigest = "other" },
		"slower than the task":  func(d *PersonaModelDeployment) { d.Profiles[0].MaxLatency = 2 * time.Minute },
		"dearer than the task":  func(d *PersonaModelDeployment) { d.Profiles[0].MaxCostMicros = 20001 },
		"no text source rule":   func(d *PersonaModelDeployment) { d.Terms[0].SourceRules = d.Terms[0].SourceRules[:1] },
		"no purpose":            func(d *PersonaModelDeployment) { d.Destinations[0].Purposes = []string{"persona.reply"} },
		"unapproved":            func(d *PersonaModelDeployment) { d.Terms[0].Approved = false },
		"two profiles":          func(d *PersonaModelDeployment) { d.Profiles = append(d.Profiles, d.Profiles[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := loaded
			bad.Profiles = append([]agentmodel.ModelProfile(nil), loaded.Profiles...)
			bad.Terms = append([]agentegress.ProviderTerms(nil), loaded.Terms...)
			bad.Terms[0].SourceRules = append([]agentegress.ProviderSourceRule(nil), loaded.Terms[0].SourceRules...)
			bad.Destinations = append(bad.Destinations[:0:0], loaded.Destinations...)
			mutate(&bad)
			if _, err := chatlangQualify(bad); !errors.Is(err, errChatlangBinding) {
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
		"failed evaluation": {agentmodel.ModelEvaluation{AgentVersionDigest: "a", SuiteDigest: "s"}, time.Second, 1000},
		"no suite digest":   {agentmodel.ModelEvaluation{AgentVersionDigest: "a", Passed: true}, time.Second, 1000},
		"slow":              {chatlangEvaluation(), 2 * time.Minute, 1000},
		"expensive":         {chatlangEvaluation(), time.Second, ChatlangTaskProfile().MaxCostMicros + 1},
		"free":              {chatlangEvaluation(), time.Second, 0},
	} {
		if _, err := NewChatlangDeployment(base, tc.evaluation, tc.latency, tc.cost); !errors.Is(err, errChatlangBinding) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTodo_CHATLANG_003_ProductionGateway(t *testing.T) {
	stack := newChatlangGatewayStack(t, "tenant-a", ChatlangBudgetPolicy())
	ctx := context.Background()
	request := chatlang.Request{Tenant: "tenant-a", Message: "m", Revision: 1, Source: "en", Target: "de", Text: "Please review ⟦HCM:abcd1234:0⟧ today.", Context: []string{"Earlier message"}}
	if !stack.engine.Ready() {
		t.Fatal("a bound model reports not ready")
	}
	response, err := stack.engine.Translate(ctx, request)
	if err != nil || response.Text != "[de] Please review ⟦HCM:abcd1234:0⟧ today." {
		t.Fatalf("translate %+v %v", response, err)
	}
	instruction, data := chatlang.Prompt(request)
	sent := stack.provider.last()
	if stack.provider.calls() != 1 || sent.Authorization != "Bearer test-only-provider-key" || sent.Store || sent.System != instruction || sent.User != data {
		t.Fatalf("what left the deployment: calls %d %+v", stack.provider.calls(), sent)
	}
	// The cost is measured from the provider's answer and carried to the caller
	// for its usage line: 20 input tokens at 2 and 10 output tokens at 8.
	if response.CostMicros != 120 || response.InputTokens != 20 || response.OutputTokens != 10 || response.Provider != "openai" || response.Model == "" {
		t.Fatalf("usage %+v", response)
	}
	// And in the dedicated ledger, against the workspace's monthly task.
	taskID := ChatlangBudgetTaskID("tenant-a", time.Now())
	var task *agentbudget.TaskSnapshot
	snapshot := stack.ledger.Snapshot()
	for i := range snapshot.Tasks {
		if snapshot.Tasks[i].ID == taskID {
			task = &snapshot.Tasks[i]
		}
	}
	if task == nil || task.TenantID != "tenant-a" || task.UserID != chatlangBudgetUser || task.Used.Steps != 1 || task.Used.Tokens != 30 || task.Used.SpendMicros != 120 || task.Reserved.Steps != 0 {
		t.Fatalf("budget task %+v", task)
	}
	// Every routing decision is in the append-only audit chain, with no message text.
	stack.audit.mu.Lock()
	entries := append([]agentaudit.Entry(nil), stack.audit.entries...)
	stack.audit.mu.Unlock()
	if len(entries) != 1 || entries[0].Action != "model.route" || entries[0].TenantID != "tenant-a" || entries[0].Actor.TaskID != taskID || entries[0].Kind != agentaudit.EventModelCall {
		t.Fatalf("audit entries %+v", entries)
	}
	for _, f := range entries[0].Fields {
		if strings.Contains(f.Value, "review") || strings.Contains(f.Value, "Earlier message") {
			t.Fatal("the route record carries message text")
		}
	}
	if _, err := stack.engine.Translate(ctx, request); err != nil || stack.provider.calls() != 2 {
		t.Fatalf("a second call mints its own lease: %v calls %d", err, stack.provider.calls())
	}

	t.Run("a workspace the credential does not cover reaches nothing", func(t *testing.T) {
		before := stack.provider.calls()
		other := request
		other.Tenant = "tenant-b"
		if _, err := stack.engine.Translate(ctx, other); err == nil || stack.provider.calls() != before {
			t.Fatalf("an out-of-scope workspace was served: %v", err)
		}
	})
	t.Run("provider failure records no spend", func(t *testing.T) {
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
		if _, err := stack.engine.Translate(ctx, request); err == nil || errors.Is(err, chatlang.ErrBudget) {
			t.Fatalf("provider outage: %v", err)
		}
		if after := spent(); after.SpendMicros != before.SpendMicros || after.Tokens != before.Tokens {
			t.Fatalf("a failed call was charged: %+v -> %+v", before, after)
		}
	})
	t.Run("an engine that is not bound refuses", func(t *testing.T) {
		if _, err := (ChatlangGatewayEngine{}).Translate(ctx, request); !errors.Is(err, chatlang.ErrUnavailable) {
			t.Fatal(err)
		}
		if (ChatlangGatewayEngine{}).Ready() || (*ChatlangDeploymentBinding)(nil).Ready() {
			t.Fatal("ready with nothing bound")
		}
	})
}

func TestTodo_CHATLANG_003_Evidence(t *testing.T) {
	stack := newChatlangGatewayStack(t, "tenant-a", ChatlangBudgetPolicy())
	req, err := stack.binding.Bind(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	_, data := chatlang.Prompt(chatlang.Request{Source: "en", Target: "de", Text: "Please fix it now."})
	provenance := []string{"chat-translation:" + req.Route.TraceID}
	req.Dispatch.Outbound.DeclaredFields = []string{"model.message.1"}
	req.Dispatch.Outbound.Fields = []agentegress.Field{{Name: "model.message.1", Value: data, Class: "INTERNAL", Provenance: provenance}}
	req.Dispatch.FieldSources = map[string]string{"model.message.1": chatlangSourceText}
	ctx := context.WithValue(context.Background(), chatlangEvidenceKey{}, req)
	good := agentegress.SourceClassificationRequest{Tenant: "tenant-a", Purpose: ChatlangPurpose, FieldName: "model.message.1", SourceClass: chatlangSourceText, DataClass: "INTERNAL", Provenance: provenance, ValueDigest: chattoneDigest(data), MessageRole: agentmodel.RoleUser}
	if err := stack.evidence.VerifySourceClassification(ctx, good); err != nil {
		t.Fatalf("honest source refused: %v", err)
	}
	for name, mutate := range map[string]func(*agentegress.SourceClassificationRequest){
		"other tenant":      func(s *agentegress.SourceClassificationRequest) { s.Tenant = "tenant-b" },
		"other purpose":     func(s *agentegress.SourceClassificationRequest) { s.Purpose = ChattonePurpose },
		"other field":       func(s *agentegress.SourceClassificationRequest) { s.FieldName = "model.message.0" },
		"other source":      func(s *agentegress.SourceClassificationRequest) { s.SourceClass = chattoneSourceDraft },
		"lower class":       func(s *agentegress.SourceClassificationRequest) { s.DataClass = "PUBLIC" },
		"other bytes":       func(s *agentegress.SourceClassificationRequest) { s.ValueDigest = chattoneDigest("other") },
		"other provenance":  func(s *agentegress.SourceClassificationRequest) { s.Provenance = []string{"chat-translation:other"} },
		"text as the role":  func(s *agentegress.SourceClassificationRequest) { s.MessageRole = agentmodel.RoleSystem },
		"writing provenace": func(s *agentegress.SourceClassificationRequest) { s.Provenance = []string{"chat-writing-style:x"} },
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
	if err := (*ChatlangModelEvidence)(nil).VerifySourceClassification(ctx, good); err == nil {
		t.Fatal("nil evidence verified")
	}
	if err := stack.evidence.RecordRoute(context.Background(), agentmodel.RouteRecord{TraceID: req.Route.TraceID, Digest: "d"}); err == nil {
		t.Fatal("a route was recorded without a request built for this call")
	}
	if err := stack.evidence.RecordRoute(ctx, agentmodel.RouteRecord{TraceID: "other", Digest: "d"}); err == nil {
		t.Fatal("a route for another trace was recorded")
	}
}

func TestTodo_CHATLANG_003_GovernedRoute(t *testing.T) {
	// The whole path with the governed model route as the engine: a German
	// reader in an English channel, the usage line priced from the provider's
	// answer, the monthly limit enforced from those lines, and a model that
	// loses a placeholder discarded. The provider is an httptest stand-in.
	rig := newChatlangRig(t)
	stack := newChatlangGatewayStack(t, "host", ChatlangBudgetPolicy())
	var err error
	rig.runtime, err = NewChatlangRuntime(rig.chat.store, rig.governance, stack.engine, ChatlangFilterScreen{Filters: rig.chat.filters}, time.Now, []string{"host"})
	if err != nil {
		t.Fatal(err)
	}
	rig.enable(chatlang.Workspace{BudgetMicros: 200})
	rig.reads("bruno", "de")
	post := rig.send("gw1", "Please review the report with @carla before 2026-10-01.")
	rig.drain()
	got, mark := rig.read("bruno", post)
	if mark.State != "ready" || got.Text != "[de] Please review the report with @carla before 2026-10-01." || got.Producer.Provider != "openai" || stack.provider.calls() != 1 {
		t.Fatalf("%+v %+v calls=%d", got, mark, stack.provider.calls())
	}
	if strings.Contains(stack.provider.last().User, "@carla") || strings.Contains(stack.provider.last().User, "2026-10-01") {
		t.Fatalf("a protected item left the deployment: %s", stack.provider.last().User)
	}
	spent, limit, err := rig.governance.Budget(context.Background(), "host")
	if err != nil || spent != 120 || limit != 200 {
		t.Fatalf("spent %d of %d: %v", spent, limit, err)
	}
	// 120 of 200 is left: one more call fits and takes it past the limit.
	second := rig.send("gw2", "Please send the schedule to the team before the meeting.")
	rig.drain()
	third := rig.send("gw3", "Please confirm the budget with the team before tomorrow.")
	rig.read("bruno", third) // past the limit nothing is requested at send; a reader asks on reading
	rig.drain()
	if _, mark := rig.read("bruno", second); mark.State != "ready" {
		t.Fatalf("second: %+v", mark)
	}
	if got, mark := rig.read("bruno", third); mark.State != "fallback" || got.Text != "Please confirm the budget with the team before tomorrow." || stack.provider.calls() != 2 {
		t.Fatalf("at the limit the original is shown and the provider is not called: %+v %+v calls=%d", got, mark, stack.provider.calls())
	}
	// A provider whose answer loses a placeholder is discarded, never stored.
	rig.enable(chatlang.Workspace{BudgetMicros: 100000})
	stack.provider.mu.Lock()
	stack.provider.damage = true
	stack.provider.mu.Unlock()
	fourth := rig.send("gw4", "Please ask @carla to review the plan today.")
	rig.drain()
	if got, mark := rig.read("bruno", fourth); mark.State != "fallback" || strings.Contains(got.Text, "[de]") || rig.count(`SELECT count(*) FROM chatrender_rendering WHERE post_id=$1`, fourth.ID) != 0 {
		t.Fatalf("a damaged translation was kept: %+v %+v", got, mark)
	}
}
