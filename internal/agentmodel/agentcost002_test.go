package agentmodel

import (
	"os"
	"strings"
	"testing"
)

// The RED state of AGENTCOST-002: every model interaction goes through the
// text-generation contract, including calls whose answer is yes or no. These
// tests pin the decision inventory and the provider-neutral decision port.

func TestTodo_AGENTCOST_002(t *testing.T) {
	points := Agentcost002DecisionPoints()
	if len(points) != 10 {
		t.Fatalf("decision points = %d, want 10", len(points))
	}
	seen := map[string]bool{}
	for _, point := range points {
		if point.ID == "" || seen[point.ID] {
			t.Fatalf("decision point without a unique id: %+v", point)
		}
		seen[point.ID] = true
		if point.Question == "" || len(point.Options) < 2 {
			t.Fatalf("%s: question type or option set missing", point.ID)
		}
		if point.WrongYes == "" || point.WrongNo == "" {
			t.Fatalf("%s: cost of a wrong answer in each direction is missing", point.ID)
		}
		if point.RequiredConfidence <= 0 || point.RequiredConfidence > 1 {
			t.Fatalf("%s: required confidence %v is outside (0,1]", point.ID, point.RequiredConfidence)
		}
		if point.Suite == "" {
			t.Fatalf("%s: no labelled suite gates enablement", point.ID)
		}
		if point.Escalation != "model" && point.Escalation != "person" {
			t.Fatalf("%s: abstention must escalate to the general model or a person", point.ID)
		}
	}
	for _, want := range []string{"screen.message", "audience.todo_reminder", "search.route", "reaction.choose", "support.severity", "support.needs_review", "instruction.detect", "citation.supported", "scope.skills", "model.profile"} {
		if !seen[want] {
			t.Fatalf("decision point %q is missing from the inventory", want)
		}
	}

	// The port asks typed questions and refuses untyped ones.
	boolean := DecisionRequest{ContractVersion: DecisionContractVersion, Point: "screen.message", Question: DecisionBoolean, Threshold: 0.8, TraceID: "t1", Processing: testDecisionProcessing()}
	if err := ValidateDecisionRequest(boolean); err != nil {
		t.Fatalf("a yes-or-no question is refused: %v", err)
	}
	choice := DecisionRequest{ContractVersion: DecisionContractVersion, Point: "reaction.choose", Question: DecisionChoice, Options: []string{"like", "celebrate", "none"}, Threshold: 0.7, TraceID: "t1", Processing: testDecisionProcessing()}
	if err := ValidateDecisionRequest(choice); err != nil {
		t.Fatalf("a one-of-N question is refused: %v", err)
	}
	score := DecisionRequest{ContractVersion: DecisionContractVersion, Point: "support.severity", Question: DecisionScore, Threshold: 0.6, TraceID: "t1", Processing: testDecisionProcessing()}
	if err := ValidateDecisionRequest(score); err != nil {
		t.Fatalf("a score question is refused: %v", err)
	}
	bad := []DecisionRequest{
		{Point: "screen.message", Question: DecisionBoolean, Threshold: 0.8, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Question: DecisionBoolean, Threshold: 0.8, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Point: "screen.message", Threshold: 0.8, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Point: "screen.message", Question: DecisionBoolean, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Point: "screen.message", Question: DecisionBoolean, Threshold: 1.5, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Point: "reaction.choose", Question: DecisionChoice, Options: []string{"only"}, Threshold: 0.7, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Point: "support.severity", Question: DecisionScore, Options: []string{"low"}, Threshold: 0.6, TraceID: "t1", Processing: testDecisionProcessing()},
		{ContractVersion: DecisionContractVersion, Point: "screen.message", Question: DecisionBoolean, Threshold: 0.8, Processing: testDecisionProcessing()},
	}
	for i, req := range bad {
		if err := ValidateDecisionRequest(req); err == nil {
			t.Fatalf("request %d validates: %+v", i, req)
		}
	}

	// Below the threshold the port abstains and names where the question goes.
	uncertain := ApplyDecisionThreshold(choice, DecisionResult{Chosen: 0, Probabilities: []float64{0.4, 0.35, 0.25}, Confidence: 0.4})
	if !uncertain.Abstained || uncertain.EscalateTo == "" {
		t.Fatalf("an uncertain answer does not abstain: %+v", uncertain)
	}
	sure := ApplyDecisionThreshold(choice, DecisionResult{Chosen: 1, Probabilities: []float64{0.1, 0.8, 0.1}, Confidence: 0.8})
	if sure.Abstained || sure.Chosen != 1 {
		t.Fatalf("a confident answer abstains: %+v", sure)
	}
	// A result that disagrees with itself is refused, not delivered.
	if err := ValidateDecisionResult(choice, DecisionResult{Chosen: 5, Probabilities: []float64{0.5, 0.5}, Confidence: 0.5}); err == nil {
		t.Fatal("a choice outside the options validates")
	}
	if err := ValidateDecisionResult(choice, DecisionResult{Chosen: 0, Probabilities: []float64{0.5, 0.5}, Confidence: 0.5}); err == nil {
		t.Fatal("probabilities that do not cover the options validate")
	}
}

func testDecisionProcessing() ProcessingPolicy {
	return ProcessingPolicy{Residency: "eu", Retention: "run-record", TrainingUse: UseDenied, Logging: UseDenied}
}

func TestTodo_AGENTCOST_002_Golden(t *testing.T) {
	const golden = "testdata/agentcost002_decisions.golden"
	got := RenderAgentcost002Record()
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
		t.Fatal("decision record changed; review it and regenerate with AGENTCOST_GOLDEN_UPDATE=1")
	}
	for _, text := range []string{"screen.message", "model.profile", "abstain", "route policy", "outbound verifier", "run budget", "sealed", "labelled suite", "threshold"} {
		if !strings.Contains(strings.ToLower(got), text) {
			t.Fatalf("the record does not cover %q", text)
		}
	}
}

func TestTodo_AGENTCOST_002_Security(t *testing.T) {
	forbidden := Agentcost002ForbiddenSoloActions()
	for _, want := range []string{"admit a person", "publish", "widen an audience", "spend above a limit", "side effect whose refusal direction is unsafe"} {
		found := false
		for _, action := range forbidden {
			if strings.Contains(strings.ToLower(action), want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("a decision may act alone to %q: no such prohibition", want)
		}
	}
	// No decision point answers a forbidden question, and none runs without a
	// threshold that can abstain.
	for _, point := range Agentcost002DecisionPoints() {
		lower := strings.ToLower(point.ID + " " + strings.Join(point.Options, " "))
		for _, verb := range []string{"admit", "publish", "widen", "spend", "execute"} {
			if strings.Contains(lower, verb) {
				t.Fatalf("%s asks a forbidden question", point.ID)
			}
		}
	}
	highStakes := map[string]bool{"support.needs_review": true, "citation.supported": true, "instruction.detect": true}
	for _, point := range Agentcost002DecisionPoints() {
		if highStakes[point.ID] && point.Escalation != "person" {
			t.Fatalf("%s abstains to the model instead of a person", point.ID)
		}
	}
}
