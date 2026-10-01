package agentpersonaeval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/tools/agenteval"
)

type personaSafetySource struct{ omitLast bool }

func (s personaSafetySource) EvaluatePersonaSafety(_ context.Context, _ PersonaVersion, cases []TaskCase) ([]agentsecurity.EvalFixture, error) {
	if s.omitLast && len(cases) > 0 {
		cases = cases[:len(cases)-1]
	}
	fixtures := make([]agentsecurity.EvalFixture, 0, len(cases))
	for _, testCase := range cases {
		fixtures = append(fixtures, agentsecurity.EvalFixture{Name: testCase.ID, SafetyPass: true, GroundingPass: true, ToolSelectionPass: true})
	}
	return fixtures, nil
}

func TestTodo_AGENTP_021_ComposedRunnerFailsClosedWithoutRuntimeObservationEvidence(t *testing.T) {
	persona := p21Persona()
	personaExecutor := &p21Executor{}
	longCalls := 0
	release := agentsecurity.Release{Agent: "persona-evaluator", AgentBuild: "build-1", AgentVersion: "eval-v1", Model: "configured-model", ModelDigest: "sha256:" + strings.Repeat("d", 64), Tool: "evaluation-tool", ToolVersion: 1, PromptHash: "sha256:" + strings.Repeat("e", 64)}
	runner, err := NewComposedPersonaRunner(release, personaExecutor, personaSafetySource{}, agenteval.TaskExecutorFunc(func(_ context.Context, task agenteval.TaskCase) (agenteval.TaskOutcome, error) {
		longCalls++
		return agenteval.TaskOutcome{Completed: true, VerifyPassed: 1, VerifyTotal: 1, Steps: 1, WallClock: time.Millisecond, CostMicros: 2}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = runner.RunPersonaEvaluation(context.Background(), persona, p21Cases())
	if err == nil || !strings.Contains(err.Error(), "verified case observation evidence is required") {
		t.Fatalf("composed issuance accepted executor assertions without signed runtime and typed output evidence: %v", err)
	}
	if len(personaExecutor.called) != 1 || longCalls != 0 {
		t.Fatalf("runner continued after missing case evidence: personaCalls=%d taskCalls=%d", len(personaExecutor.called), longCalls)
	}
}

func TestTodo_AGENTP_021_ComposedRunnerRejectsIncompleteSafetyProof(t *testing.T) {
	persona := p21Persona()
	release := agentsecurity.Release{Agent: "persona-evaluator", AgentBuild: "build-1", AgentVersion: "eval-v1", Model: "model", ModelDigest: "sha256:" + strings.Repeat("d", 64), Tool: "tool", ToolVersion: 1, PromptHash: "sha256:" + strings.Repeat("e", 64)}
	runner, err := NewComposedPersonaRunner(release, &p21Executor{}, personaSafetySource{omitLast: true}, agenteval.TaskExecutorFunc(func(context.Context, agenteval.TaskCase) (agenteval.TaskOutcome, error) {
		return agenteval.TaskOutcome{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runner.RunPersonaEvaluation(context.Background(), persona, p21Cases()); err == nil || !strings.Contains(err.Error(), "fixture set") {
		t.Fatalf("incomplete AGENT-004 evidence was accepted: %v", err)
	}
	if _, err := NewComposedPersonaRunner(release, &p21Executor{}, personaSafetySource{}, nil); err == nil {
		t.Fatal("missing task executor was accepted")
	}
	if _, err := NewComposedPersonaRunner(agentsecurity.Release{}, &p21Executor{}, personaSafetySource{}, agenteval.TaskExecutorFunc(func(context.Context, agenteval.TaskCase) (agenteval.TaskOutcome, error) {
		return agenteval.TaskOutcome{}, nil
	})); err == nil {
		t.Fatal("unbound evaluator release was accepted")
	}
}

func TestTodo_AGENTP_021_StaticSuiteCatalogIsImmutable(t *testing.T) {
	original := p21Cases()
	catalog, err := NewStaticPersonaSuiteCatalog(map[string][]TaskCase{"persona/comp-analyst/v3": original})
	if err != nil {
		t.Fatal(err)
	}
	original[0].InputText = "mutated after construction"
	resolved, err := catalog.ResolvePersonaSuite("persona/comp-analyst/v3")
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].ID != "authority-private-worker" {
		t.Fatalf("catalog cases are not sorted: %+v", resolved[0])
	}
	for _, got := range resolved {
		if got.ID == "in-scope-comp" && got.InputText == "mutated after construction" {
			t.Fatal("catalog retained caller-owned suite memory")
		}
	}
	resolved[0].InputText = "mutated resolved copy"
	resolvedAgain, err := catalog.ResolvePersonaSuite("persona/comp-analyst/v3")
	if err != nil {
		t.Fatal(err)
	}
	if resolvedAgain[0].InputText == "mutated resolved copy" {
		t.Fatal("catalog exposed stored suite memory")
	}
	if _, err := catalog.ResolvePersonaSuite("missing"); err == nil {
		t.Fatal("unknown suite reference resolved")
	}
	missingKind := p21Cases()[:4]
	if _, err := NewStaticPersonaSuiteCatalog(map[string][]TaskCase{"bad": missingKind}); err == nil {
		t.Fatal("incomplete release suite accepted")
	}
	if _, err := NewStaticPersonaSuiteCatalog(map[string][]TaskCase{"bad": append(p21Cases(), p21Cases()[0])}); err == nil {
		t.Fatal("duplicate release case accepted")
	}
	seed := sha256.Sum256([]byte("catalog digest check"))
	if !validDigest("sha256:" + hex.EncodeToString(seed[:])) {
		t.Fatal("valid suite digest rejected")
	}
}
