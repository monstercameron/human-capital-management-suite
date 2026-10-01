package agentmodel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/schemaflux/schemafluxtest"
)

type testReservation struct {
	mu      sync.Mutex
	settled []agentbudget.Usage
	fails   int
}

func (r *testReservation) Settle(usage agentbudget.Usage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settled = append(r.settled, usage)
	return nil
}

func (r *testReservation) Fail() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fails++
	return nil
}

type testBudget struct {
	mu           sync.Mutex
	reservations []*testReservation
	err          error
}

func (b *testBudget) Reserve(context.Context, agentbudget.Request) (Reservation, error) {
	if b.err != nil {
		return nil, b.err
	}
	r := &testReservation{}
	b.mu.Lock()
	b.reservations = append(b.reservations, r)
	b.mu.Unlock()
	return r, nil
}

type testAudit struct {
	mu      sync.Mutex
	entries []agentaudit.Entry
	err     error
}

func (a *testAudit) Append(_ context.Context, entry agentaudit.Entry) (agentaudit.Record, error) {
	if a.err != nil {
		return agentaudit.Record{}, a.err
	}
	a.mu.Lock()
	a.entries = append(a.entries, entry)
	a.mu.Unlock()
	return agentaudit.Record{Entry: entry}, nil
}

type testSkills struct {
	record agentskills.SkillRecord
	err    error
}

func (s testSkills) ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if s.err != nil {
		return agentskills.SkillRecord{}, s.err
	}
	return s.record, nil
}

type testRedactor struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (r *testRedactor) Redact(_ context.Context, text string, _ []string) (Redaction, error) {
	if r.err != nil {
		return Redaction{}, r.err
	}
	r.mu.Lock()
	r.texts = append(r.texts, text)
	r.mu.Unlock()
	return Redaction{Text: text}, nil
}

type modelAnswer struct {
	Decision string `json:"decision" schemaflux:"required"`
}

func actor() ActorChain {
	return ActorChain{
		UserID: "user-1", AgentVersion: "agent-v1", InstallationID: "install-1",
		TaskID: "task-1", PlanRevision: "plan-1", StepID: "step-1", DelegationGrantID: "grant-1",
	}
}

func skill() agentskills.SkillRecord {
	return agentskills.SkillRecord{
		Definition: agentskills.SkillDefinition{
			ID: "review", Version: 1, RequiredPurposes: []string{"planning"},
			DataClassesRead: []string{"INTERNAL"},
		},
		Digest: "skill-digest", Status: agentskills.StatusActive,
	}
}

func request() Request {
	return Request{
		TenantID: "tenant-1", Actor: actor(), Purpose: "planning", Prompt: "Review this plan.",
		Skill:       agentskills.SkillPin{ID: "review", Version: 1, Digest: "skill-digest"},
		DataClasses: []string{"INTERNAL"}, Estimate: agentbudget.Usage{Steps: 1, Tokens: 100}, MaxRetries: 1,
	}
}

func gatewayFixture(t *testing.T) (*Gateway, *testBudget, *testAudit, *testRedactor) {
	t.Helper()
	budget := &testBudget{}
	audit := &testAudit{}
	redactor := &testRedactor{}
	gateway, err := New(Config{
		Skills: testSkills{record: skill()}, Budget: budget, Audit: audit, Redactor: redactor,
		Clock: func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return gateway, budget, audit, redactor
}

func TestTodo_AGENT2_026(t *testing.T) {
	provider := schemafluxtest.New().Reply(`{"decision":"approve"}`)
	defer schemafluxtest.Install(t, provider)()

	gateway, budget, audit, redactor := gatewayFixture(t)
	got, err := Generate[modelAnswer](context.Background(), gateway, request())
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Value.Decision != "approve" || got.Attempts != 1 {
		t.Fatalf("result = %+v, want typed answer and one attempt", got)
	}
	if got.Usage.Steps != 1 || got.Usage.Tokens < 0 || got.PromptDigest == "" || got.OutputDigest == "" {
		t.Fatalf("usage/digests = %+v, want accounted call", got)
	}
	if len(budget.reservations) != 1 || len(budget.reservations[0].settled) != 1 {
		t.Fatalf("budget reservations = %+v, want one settled reservation", budget.reservations)
	}
	if len(audit.entries) != 1 || audit.entries[0].Kind != agentaudit.EventModelCall {
		t.Fatalf("audit entries = %+v, want one model call", audit.entries)
	}
	if len(redactor.texts) != 2 {
		t.Fatalf("redaction calls = %d, want prompt and output", len(redactor.texts))
	}
}

func TestTodo_AGENT2_026_Security(t *testing.T) {
	provider := schemafluxtest.New().Reply(`{"decision":"approve"}`)
	defer schemafluxtest.Install(t, provider)()

	gateway, _, audit, _ := gatewayFixture(t)
	req := request()
	req.DataClasses = []string{"RESTRICTED"}
	_, err := Generate[modelAnswer](context.Background(), gateway, req)
	if !errors.Is(err, ErrSkillDenied) {
		t.Fatalf("unauthorised class error = %v, want ErrSkillDenied", err)
	}
	if provider.CallCount() != 0 || len(audit.entries) != 0 {
		t.Fatalf("provider calls=%d audit=%d, want no side effects", provider.CallCount(), len(audit.entries))
	}

	req = request()
	req.Actor.UserID = ""
	_, err = Generate[modelAnswer](context.Background(), gateway, req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid actor error = %v, want ErrInvalidRequest", err)
	}
}

func TestTodo_AGENT2_026_Fault(t *testing.T) {
	provider := schemafluxtest.New().Fail(errors.New("provider unavailable"))
	defer schemafluxtest.Install(t, provider)()

	gateway, budget, audit, _ := gatewayFixture(t)
	req := request()
	req.MaxRetries = 1
	_, err := Generate[modelAnswer](context.Background(), gateway, req)
	if !errors.Is(err, ErrRetryLimit) {
		t.Fatalf("fault error = %v, want ErrRetryLimit", err)
	}
	if provider.CallCount() != 2 || len(budget.reservations) != 2 || len(audit.entries) != 2 {
		t.Fatalf("calls=%d reservations=%d audits=%d, want two bounded attempts", provider.CallCount(), len(budget.reservations), len(audit.entries))
	}
	for i, reservation := range budget.reservations {
		if reservation.fails != 1 || len(reservation.settled) != 0 {
			t.Fatalf("reservation %d = %+v, want failed and unsettled", i, reservation)
		}
	}
}

func TestTodo_AGENT2_026_Golden(t *testing.T) {
	provider := schemafluxtest.New().Reply(`{"decision":"approve"}`)
	defer schemafluxtest.Install(t, provider)()

	gateway, _, _, _ := gatewayFixture(t)
	if _, err := Generate[modelAnswer](context.Background(), gateway, request()); err != nil {
		t.Fatal(err)
	}
	prompt := provider.LastRequest().UserPrompt
	if !strings.Contains(prompt, `"skill":"review"`) || !strings.Contains(prompt, `"digest":"skill-digest"`) {
		t.Fatalf("prompt = %q, want deterministic pinned skill context", prompt)
	}
	if strings.Contains(prompt, "grant-1") || strings.Contains(prompt, "user-1") {
		t.Fatalf("prompt contains actor identity: %q", prompt)
	}
}

func TestTodo_AGENT2_026_Integration(t *testing.T) {
	provider := schemafluxtest.New().Reply(`{"decision":"approve"}`)
	defer schemafluxtest.Install(t, provider)()

	budget := &testBudget{}
	redactor := &testRedactor{}
	audit := agentaudit.NewMemoryStore()
	gateway, err := New(Config{
		Skills: testSkills{record: skill()}, Budget: budget, Audit: audit, Redactor: redactor,
		Clock: func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Generate[modelAnswer](context.Background(), gateway, request()); err != nil {
		t.Fatal(err)
	}
	views, err := audit.Query(context.Background(), agentaudit.Query{
		Viewer: agentaudit.Viewer{TenantID: "tenant-1", UserID: "user-1", Role: agentaudit.ViewerUser},
		TaskID: "task-1", Kinds: []agentaudit.EventKind{agentaudit.EventModelCall},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Actor != actor() || views[0].ArgumentsDigest == "" || views[0].ResultDigest == "" {
		t.Fatalf("audit views = %+v, want one actor-bound model call", views)
	}
	if len(views[0].Edges) != 1 || views[0].Edges[0].Kind != agentaudit.EdgeTask || views[0].Edges[0].From != "task-1" || views[0].Edges[0].To != views[0].EventID {
		t.Fatalf("audit edges = %+v, want task provenance edge", views[0].Edges)
	}
}
