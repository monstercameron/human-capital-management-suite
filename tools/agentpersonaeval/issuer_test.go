package agentpersonaeval

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/tools/agenteval"
)

type issuerRunner struct {
	persona PersonaEvidence
	run     agenteval.Run
	err     error
}

type issuerCatalog struct {
	cases []TaskCase
	err   error
}

func (c issuerCatalog) ResolvePersonaSuite(string) ([]TaskCase, error) { return c.cases, c.err }

func (r issuerRunner) RunPersonaEvaluation(context.Context, PersonaVersion, []TaskCase) (PersonaEvidence, agenteval.Run, error) {
	return r.persona, r.run, r.err
}

func TestTodo_AGENTP_021_IssuerRejectsPassingClaimsWithoutCaseReceipts(t *testing.T) {
	persona := p21Persona()
	composed, err := NewComposedPersonaRunner(
		agentsecurity.Release{Agent: "persona-evaluator", AgentBuild: "test-build", AgentVersion: "eval-v1", Model: "configured-model", ModelDigest: "sha256:" + strings.Repeat("d", 64), Tool: "persona-eval", ToolVersion: 1, PromptHash: "sha256:" + strings.Repeat("e", 64)},
		&p21Executor{}, personaSafetySource{}, agenteval.TaskExecutorFunc(func(context.Context, agenteval.TaskCase) (agenteval.TaskOutcome, error) {
			return agenteval.TaskOutcome{Completed: true, VerifyPassed: 1, VerifyTotal: 1, Steps: 1, WallClock: time.Millisecond, CostMicros: 2}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	seed := sha256.Sum256([]byte("AGENTP-021 issuer test key"))
	private := ed25519.NewKeyFromSeed(seed[:])
	issuedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	catalog, err := NewStaticPersonaSuiteCatalog(map[string][]TaskCase{persona.EvalSuiteRef: p21Cases()})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewEvaluationIssuer("eval-2026-09", private, func() time.Time { return issuedAt }, 24*time.Hour, composed, catalog)
	if err != nil {
		t.Fatal(err)
	}
	_, err = issuer.IssuePersonaEvaluation(context.Background(), IssueRequest{
		TenantID: "tenant-test", RunID: "run-exact", Persona: persona, PersonaVersionNumber: 3,
	})
	if !errors.Is(err, ErrIssuerRun) || !strings.Contains(err.Error(), "verified case observation evidence is required") {
		t.Fatalf("issuer signed caller assertions without authenticated case receipts: %v", err)
	}
}

func TestTodo_AGENTP_021_IssuerRejectsForgedOrStaleResults(t *testing.T) {
	persona := p21Persona()
	evidence, err := EvaluatePersona(context.Background(), persona, p21Cases(), &p21Executor{})
	if err != nil {
		t.Fatal(err)
	}
	run := issuerLongHorizonRun(t, persona)
	seed := sha256.Sum256([]byte("AGENTP-021 issuer refusal key"))
	base := IssueRequest{TenantID: "tenant-test", RunID: "run-1", Persona: persona, PersonaVersionNumber: 3}
	for _, tc := range []struct {
		name    string
		result  issuerRunner
		catalog issuerCatalog
		request IssueRequest
		want    error
	}{
		{name: "runner error", result: issuerRunner{err: errors.New("runner unavailable")}, catalog: issuerCatalog{cases: p21Cases()}, request: base, want: ErrIssuerRun},
		{name: "suite changed", result: issuerRunner{persona: evidence, run: run}, catalog: func() issuerCatalog {
			cases := p21Cases()
			cases = append(cases, TaskCase{ID: "extra", Kind: CaseInScope, InputText: "extra", ExpectedSkill: "compensation.lookup", ExpectedOutcome: OutcomeComplete, Audience: "invoker"})
			return issuerCatalog{cases: cases}
		}(), request: base, want: ErrIssuerRun},
		{name: "suite unavailable", result: issuerRunner{persona: evidence, run: run}, catalog: issuerCatalog{err: errors.New("missing")}, request: base, want: ErrIssuerRun},
		{name: "version mismatch", result: issuerRunner{persona: evidence, run: run}, catalog: issuerCatalog{cases: p21Cases()}, request: func() IssueRequest { request := base; request.PersonaVersionNumber = 4; return request }(), want: ErrIssuerRun},
		{name: "tampered AGENT2-025 digest", result: issuerRunner{persona: evidence, run: func() agenteval.Run {
			changed := run
			changed.Digest = "sha256:" + strings.Repeat("0", 64)
			return changed
		}()}, catalog: issuerCatalog{cases: p21Cases()}, request: base, want: ErrIssuerRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer, err := NewEvaluationIssuer("eval-v1", ed25519.NewKeyFromSeed(seed[:]), func() time.Time { return time.Now().UTC() }, time.Hour, tc.result, tc.catalog)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := issuer.IssuePersonaEvaluation(context.Background(), tc.request); !errors.Is(err, tc.want) {
				t.Fatalf("issue error=%v, want %v", err, tc.want)
			}
		})
	}
	failed := evidence
	failed.Passed = false
	failed.Digest = digestEvidence(failed)
	issuer, err := NewEvaluationIssuer("eval-v1", ed25519.NewKeyFromSeed(seed[:]), func() time.Time { return time.Now().UTC() }, time.Hour, issuerRunner{persona: failed, run: run}, issuerCatalog{cases: p21Cases()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.IssuePersonaEvaluation(context.Background(), base); !errors.Is(err, ErrIssuerRun) {
		t.Fatalf("failed result issued: %v", err)
	}
}

func issuerLongHorizonRun(t *testing.T, persona PersonaVersion) agenteval.Run {
	t.Helper()
	suite := agenteval.DefaultSuite()
	release := agentsecurity.Release{Agent: "persona-evaluator", AgentBuild: "test", AgentVersion: "test-v1", PersonaID: persona.PersonaID, PersonaVersion: persona.Version, Model: persona.Model, ModelDigest: persona.ModelDigest, Tool: "persona-evaluation", ToolVersion: 1, PromptHash: persona.PersonaDigest}
	outcome := agenteval.TaskOutcome{Completed: true, VerifyPassed: 1, VerifyTotal: 1, Steps: 1, WallClock: time.Millisecond, CostMicros: 1}
	run, err := agenteval.NewEvaluator().Evaluate(context.Background(), agenteval.Request{Release: release, Suite: suite, Fixtures: []agentsecurity.EvalFixture{{Name: "safe", SafetyPass: true, GroundingPass: true, ToolSelectionPass: true}}, Executor: agenteval.TaskExecutorFunc(func(context.Context, agenteval.TaskCase) (agenteval.TaskOutcome, error) { return outcome, nil })})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
