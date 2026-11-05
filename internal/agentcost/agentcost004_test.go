package agentcost

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// The RED state of AGENTCOST-004: public reporting describes a DevDay
// interface with no published price, limits, error behaviour or data
// controls, and none of it is confirmed from the provider's own
// documentation. These tests pin the research record, the emulation that
// stands in for the preview, and the plan that does not depend on it.

func TestTodo_AGENTCOST_004(t *testing.T) {
	findings := Agentcost004Findings()
	for _, want := range []string{"availability", "request", "probabilities", "price", "limits", "data handling", "gateway"} {
		found := false
		for _, finding := range findings {
			if strings.Contains(strings.ToLower(finding.Question+" "+finding.Answer), want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("the record does not answer %q", want)
		}
	}
	confirmed, unconfirmed := 0, 0
	for _, finding := range findings {
		if len(finding.Sources) == 0 {
			t.Fatalf("finding %q names no source", finding.Question)
		}
		if finding.Confirmed {
			confirmed++
		} else {
			unconfirmed++
		}
	}
	if confirmed == 0 || unconfirmed == 0 {
		t.Fatal("the record does not separate what is confirmed from what is not")
	}
	if status := Agentcost004AccessRequest(); !strings.Contains(strings.ToLower(status), "not requested") {
		t.Fatalf("access request status misstates the position: %q", status)
	}

	// The emulation answers the decision port through the existing
	// structured-output route, so accuracy can be measured before access.
	emulation := agentmodel.NewEmulatedDecisionsAdapter(&agentcost004FakeModels{
		answers: map[agentmodel.DecisionQuestion]string{
			agentmodel.DecisionBoolean: `{"yes_probability":0.9,"confidence":0.9}`,
			agentmodel.DecisionChoice:  `{"choice":1,"probabilities":[0.1,0.8,0.1],"confidence":0.8}`,
			agentmodel.DecisionScore:   `{"score":0.75,"confidence":0.75}`,
		},
	})
	ctx := context.Background()
	boolean, err := emulation.Ask(ctx, agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "screen.message", Question: agentmodel.DecisionBoolean, State: "I'll send the deck by Friday", Threshold: 0.8, TraceID: "t1", Processing: testAgentcost004Processing()})
	if err != nil || boolean.Abstained || boolean.Chosen != 1 {
		t.Fatalf("boolean emulation = %+v, %v", boolean, err)
	}
	choice, err := emulation.Ask(ctx, agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "reaction.choose", Question: agentmodel.DecisionChoice, Options: []string{"none", "like", "celebrate"}, State: "Thanks, that helps", Threshold: 0.7, TraceID: "t1", Processing: testAgentcost004Processing()})
	if err != nil || choice.Abstained || choice.Chosen != 1 || len(choice.Probabilities) != 3 {
		t.Fatalf("choice emulation = %+v, %v", choice, err)
	}
	// Below the threshold the emulation abstains like any adapter.
	strict, err := emulation.Ask(ctx, agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "reaction.choose", Question: agentmodel.DecisionChoice, Options: []string{"none", "like", "celebrate"}, State: "Thanks", Threshold: 0.95, TraceID: "t1", Processing: testAgentcost004Processing()})
	if err != nil || !strict.Abstained {
		t.Fatalf("strict emulation = %+v, %v", strict, err)
	}
	// A question about nothing, and a malformed model answer, both fail
	// closed instead of guessing.
	if _, err := emulation.Ask(ctx, agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "screen.message", Question: agentmodel.DecisionBoolean, Threshold: 0.8, TraceID: "t1", Processing: testAgentcost004Processing()}); err == nil {
		t.Fatal("the emulation answered without state")
	}
	broken := agentmodel.NewEmulatedDecisionsAdapter(&agentcost004FakeModels{answers: map[agentmodel.DecisionQuestion]string{agentmodel.DecisionChoice: `{"choice":9}`}})
	if _, err := broken.Ask(ctx, agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "reaction.choose", Question: agentmodel.DecisionChoice, Options: []string{"none", "like"}, State: "Thanks", Threshold: 0.7, TraceID: "t1", Processing: testAgentcost004Processing()}); err == nil {
		t.Fatal("a choice outside the options was delivered")
	}

	// The preview adapter sits behind a feature flag with a fallback: flag
	// off or primary failure both reach the fallback.
	calls := map[string]int{}
	flagged := &agentmodel.FlaggedDecisionAdapter{
		Primary:  &agentcost004ScriptedAdapter{name: "primary", calls: calls, fail: true},
		Fallback: &agentcost004ScriptedAdapter{name: "fallback", calls: calls},
		Enabled:  func() bool { return false },
	}
	question := agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: "screen.message", Question: agentmodel.DecisionBoolean, State: "hi", Threshold: 0.8, TraceID: "t1", Processing: testAgentcost004Processing()}
	if _, err := flagged.Ask(ctx, question); err != nil || calls["fallback"] != 1 || calls["primary"] != 0 {
		t.Fatalf("flag off did not use the fallback: %v %+v", err, calls)
	}
	flagged.Enabled = func() bool { return true }
	if _, err := flagged.Ask(ctx, question); err != nil || calls["fallback"] != 2 {
		t.Fatalf("a failing primary did not fall back: %v %+v", err, calls)
	}

	// The accuracy ceiling of the same model family, through the real
	// emulation path over our own screening cases.
	first, second := DecisionsEmulationCeiling(), DecisionsEmulationCeiling()
	if first.Calls == 0 || first != second {
		t.Fatalf("the ceiling harness is empty or nondeterministic: %+v", first)
	}
	if first.Accuracy < 0 || first.Accuracy > 1 {
		t.Fatalf("ceiling accuracy %v out of range", first.Accuracy)
	}

	// The plan ships without the preview: a first adapter now, the preview
	// adapter behind the flag, nothing requiring an unfinished contract.
	plan := Agentcost004Plan()
	if len(plan) != 3 || !strings.Contains(strings.ToLower(plan[0]), "emulation") || !strings.Contains(strings.ToLower(plan[1]), "flag") || !strings.Contains(strings.ToLower(plan[2]), "requires") {
		t.Fatalf("plan = %v", plan)
	}
}

type agentcost004FakeModels struct {
	answers    map[agentmodel.DecisionQuestion]string
	synthesise bool
}

func (f *agentcost004FakeModels) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}, OutputModes: []agentmodel.OutputMode{agentmodel.OutputSchema}, Features: []agentmodel.ModelFeature{agentmodel.FeatureStructuredJSON}}
}

func (f *agentcost004FakeModels) Invoke(ctx context.Context, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	schema := string(req.Output.Schema)
	question := agentmodel.DecisionBoolean
	switch {
	case strings.Contains(schema, `"choice"`):
		question = agentmodel.DecisionChoice
	case strings.Contains(schema, `"score"`):
		question = agentmodel.DecisionScore
	}
	_ = ctx
	answer := f.answers[question]
	if f.synthesise {
		answer = agentcost004SynthesisedAnswer(schema, question)
	}
	return agentmodel.ModelResult{Structured: []byte(answer), Provider: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-6-luna", Version: "2026-09-01"}}, nil
}

type agentcost004ScriptedAdapter struct {
	name  string
	calls map[string]int
	fail  bool
}

func (s *agentcost004ScriptedAdapter) Ask(context.Context, agentmodel.DecisionRequest) (agentmodel.DecisionResult, error) {
	s.calls[s.name]++
	if s.fail {
		return agentmodel.DecisionResult{}, errAgentcost004PrimaryDown
	}
	return agentmodel.DecisionResult{ContractVersion: agentmodel.DecisionContractVersion, Chosen: 0, Confidence: 1}, nil
}

func testAgentcost004Processing() agentmodel.ProcessingPolicy {
	return agentmodel.ProcessingPolicy{Residency: "eu", Retention: "run-record", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}
}

func TestTodo_AGENTCOST_004_Golden(t *testing.T) {
	const golden = "testdata/agentcost004_decisions_api.golden"
	got := RenderAgentcost004Record()
	if os.Getenv("AGENTCOST_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("Decisions API record changed; review it and regenerate with AGENTCOST_GOLDEN_UPDATE=1")
	}
	for _, text := range []string{"devday", "preview", "per-option probabilities", "model-reported", "price", "request preview access", "feature flag", "fallback", "emulation", "gateway", "residency", "beta", "gpt-6-luna"} {
		if !strings.Contains(strings.ToLower(got), text) {
			t.Fatalf("the record does not cover %q", text)
		}
	}
}

func TestTodo_AGENTCOST_004_Security(t *testing.T) {
	// No class travels further than the existing approved OpenAI route, and
	// the preview's own terms are unconfirmed, so the preview carries
	// nothing until its terms are read.
	verdicts := Agentcost004DataClassVerdicts()
	for _, class := range []string{"PUBLIC", "INTERNAL", "PII", "COMPENSATION", "BANK", "MEDICAL", "IMMIGRATION", "CASE", "SPECIAL_CATEGORY"} {
		verdict, ok := verdicts[class]
		if !ok || !strings.HasPrefix(strings.ToLower(verdict), "none") {
			t.Fatalf("class %s may reach the preview: %q", class, verdict)
		}
	}
	// Nothing in the product requires the unfinished contract: with the flag
	// off, the first adapter answers every decision point's shape.
	emulation := agentmodel.NewEmulatedDecisionsAdapter(&agentcost004FakeModels{synthesise: true})
	flagged := &agentmodel.FlaggedDecisionAdapter{Primary: &agentcost004ScriptedAdapter{name: "preview", calls: map[string]int{}, fail: true}, Fallback: emulation, Enabled: func() bool { return false }}
	for _, point := range agentmodel.Agentcost002DecisionPoints() {
		options := point.Options
		if point.Question == agentmodel.DecisionScore {
			options = nil
		}
		if point.Question == agentmodel.DecisionBoolean {
			options = nil
		}
		result, err := flagged.Ask(context.Background(), agentmodel.DecisionRequest{ContractVersion: agentmodel.DecisionContractVersion, Point: point.ID, Question: point.Question, Options: options, State: "demo state", Threshold: point.RequiredConfidence, TraceID: "t1", Processing: testAgentcost004Processing()})
		if err != nil || result.Abstained {
			t.Fatalf("%s has no shippable path with the flag off: %+v, %v", point.ID, result, err)
		}
	}
}
