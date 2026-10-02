package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

func chattoneDraftOf(user string) string {
	var data struct {
		Draft string `json:"draft"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(user, "<untrusted_data>\n"), "\n</untrusted_data>")), &data)
	return data.Draft
}

// chattoneGoodRewrite is a deterministic "model" that does what the suite asks:
// it removes the hostile wording, keeps every fact and frames the text by style.
func chattoneGoodRewrite(system, user string) string {
	cleaned := strings.NewReplacer("You idiot, ", "", " really", "", "garbage", "weak", " Stop wasting my time.", "", "Shut up about", "About",
		"pathetic excuse for a ", "", ", you useless clown", "", "a joke", "weak", "Du Idiot, ", "", "Eres un inútil, ", "", "on earth ", "").Replace(chattoneDraftOf(user))
	switch {
	case strings.Contains(system, "neutral, courteous"):
		return "Hello, " + cleaned + " Thank you."
	case strings.Contains(system, "warm, positive"):
		return "Hi! " + cleaned + " Thanks so much!"
	default:
		return cleaned
	}
}

type chattoneFunctionModel func(system, user string) string

func (f chattoneFunctionModel) Rewrite(_ context.Context, p chatrewrite.Prompt) (string, error) {
	return f(p.Instruction, p.Data), nil
}

func chattoneQualificationService(t *testing.T, model chatrewrite.Model) *chatrewrite.Service {
	t.Helper()
	verifier, err := NewChattoneOutboundVerifier()
	if err != nil {
		t.Fatal(err)
	}
	return &chatrewrite.Service{Registry: chatrewrite.NewRegistry(), Model: model, Policy: chattoneAllowAllPolicy{}, Meaning: ChattoneMeaningGuard{}, Outbound: verifier, Ledger: chatrewrite.NewMemoryLedger(1000), Now: time.Now}
}

func TestTodo_CHATTONE_004_Qualification(t *testing.T) {
	suite := ChattoneQualificationSuite()
	if len(suite) != 30 {
		t.Fatalf("the suite is %d cases, want 10 drafts in each of 3 styles", len(suite))
	}
	styles := map[string]int{}
	for _, c := range suite {
		styles[c.Style]++
		if c.Draft == "" {
			t.Fatalf("case %s has no draft", c.ID)
		}
	}
	if styles["professional"] != 10 || styles["friendly"] != 10 || styles["concise"] != 10 {
		t.Fatalf("styles %v", styles)
	}
	id := chatrewrite.Identity{Tenant: "qualification", Person: "qualification", Conversation: "qualification"}
	model := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "test-model", Version: "test-version"}

	good := RunChattoneQualification(context.Background(), chattoneQualificationService(t, chattoneFunctionModel(chattoneGoodRewrite)), id, model, time.Now)
	if !good.Passed || len(good.Cases) != 30 || good.AgentVersion != ChattoneAgentVersionDigest() || !strings.HasPrefix(good.SuiteDigest, "sha256:") {
		for _, c := range good.Cases {
			if !c.Passed {
				t.Logf("%s: %s", c.ID, c.Reason)
			}
		}
		t.Fatalf("a model that does what is asked did not pass: %+v", good.Evaluation())
	}
	for _, c := range good.Cases {
		if !c.Passed || !strings.HasPrefix(c.OutputDigest, "sha256:") || c.Reason != "" {
			t.Fatalf("case %+v", c)
		}
	}
	if again := RunChattoneQualification(context.Background(), chattoneQualificationService(t, chattoneFunctionModel(chattoneGoodRewrite)), id, model, time.Now); again.SuiteDigest != good.SuiteDigest {
		t.Fatal("the suite digest is not stable")
	}

	for name, tc := range map[string]struct {
		model  chattoneFunctionModel
		reason string
	}{
		"keeps the hostile wording": {func(_, u string) string { return "Hello, " + chattoneDraftOf(u) + " Thank you." }, "hostile wording remains"},
		"returns the draft":         {func(_, u string) string { return chattoneDraftOf(u) }, "the rewrite is the draft"},
		"loses the facts":           {func(_, _ string) string { return "All sorted." }, "the service refused"},
	} {
		q := RunChattoneQualification(context.Background(), chattoneQualificationService(t, tc.model), id, model, time.Now)
		failed := 0
		for _, c := range q.Cases {
			if !c.Passed {
				failed++
			}
		}
		if q.Passed || failed == 0 || q.Evaluation().Passed {
			t.Fatalf("%s: a model that does not do what is asked passed (%d failed)", name, failed)
		}
		reasons := map[string]bool{}
		for _, c := range q.Cases {
			if !c.Passed {
				reasons[strings.SplitN(c.Reason, ":", 2)[0]] = true
			}
		}
		if !reasons[strings.SplitN(tc.reason, ":", 2)[0]] {
			t.Fatalf("%s: reasons %v, want %q", name, reasons, tc.reason)
		}
	}
}

func TestTodo_CHATTONE_004_QualifyLive(t *testing.T) {
	provider := newChattoneProvider(t)
	provider.rewrite = chattoneGoodRewrite
	base := modelDeploymentFixture(t)
	base.BaseURL = provider.server.URL
	if !strings.HasPrefix(base.BaseURL, "http://127.0.0.1") {
		t.Fatalf("a qualification test must never reach a real provider: %q", base.BaseURL)
	}
	q, dep, err := QualifyChattoneModel(context.Background(), ChattoneQualifyConfig{Base: base, APIKey: "test-only-provider-key", MaxLatency: 20 * time.Second, MaxCostMicros: 20000, Now: time.Now})
	if err != nil || !q.Passed || len(q.Cases) != 30 {
		t.Fatalf("live qualification: %v %+v", err, q.Evaluation())
	}
	if q.CostMicros <= 0 || q.CostMicros%120 != 0 || provider.calls() != int(q.CostMicros/120) {
		t.Fatalf("recorded cost %d for %d calls", q.CostMicros, provider.calls())
	}
	// The run sent only the suite's synthetic drafts, with the instruction text
	// the version names, and used the provider key it was given.
	for _, r := range provider.requests {
		if r.Authorization != "Bearer test-only-provider-key" || !strings.HasPrefix(r.System, chatrewrite.BaseInstruction) || r.Store {
			t.Fatalf("request %+v", r)
		}
	}
	// The document it returns carries the run's own evidence and is one the
	// service accepts.
	raw, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePersonaModelDeployment(raw)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := chattoneQualify(loaded)
	if err != nil || qualified.profile.Evaluation != q.Evaluation() || qualified.profile.Evaluation.SuiteDigest != q.SuiteDigest {
		t.Fatalf("document does not carry the run's evidence: %v %+v", err, qualified.profile.Evaluation)
	}

	// A model that does not pass yields no document.
	failing := newChattoneProvider(t)
	failing.rewrite = func(_, user string) string { return chattoneDraftOf(user) }
	base.BaseURL = failing.server.URL
	q, dep, err = QualifyChattoneModel(context.Background(), ChattoneQualifyConfig{Base: base, APIKey: "test-only-provider-key", MaxLatency: 20 * time.Second, MaxCostMicros: 20000, Now: time.Now})
	if !errors.Is(err, ErrChattoneNotQualified) || q.Passed || len(dep.Profiles) != 0 || q.CostMicros == 0 {
		t.Fatalf("a failing model: %v passed=%v profiles=%d", err, q.Passed, len(dep.Profiles))
	}
	if _, _, err := QualifyChattoneModel(context.Background(), ChattoneQualifyConfig{Base: base, MaxLatency: time.Second, MaxCostMicros: 1, Now: time.Now}); err == nil {
		t.Fatal("a run without a provider key was accepted")
	}
}
