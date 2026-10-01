package agentpersonaeval

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/tools/agenteval"
)

var (
	ErrIssuerConfig = errors.New("agentpersonaeval: invalid evaluation issuer")
	ErrIssuerRun    = errors.New("agentpersonaeval: authoritative evaluation run rejected")
)

// AuthoritativePersonaRunner executes persona cases and the AGENT2-025 suite
// through the composed task stack. Production composition must bind this to
// the served runner; the diagnostic EvaluatePersona function cannot issue a
// publication claim by itself.
type AuthoritativePersonaRunner interface {
	RunPersonaEvaluation(context.Context, PersonaVersion, []TaskCase) (PersonaEvidence, agenteval.Run, error)
}

// PersonaSuiteCatalog resolves a published suite reference to its immutable
// case corpus. User request data must never define publication thresholds.
type PersonaSuiteCatalog interface {
	ResolvePersonaSuite(string) ([]TaskCase, error)
}

// EvaluationIssuer is the publication-evidence signing boundary. Its private
// key must be provided only by the trusted application composition root.
type EvaluationIssuer struct {
	keyID      string
	privateKey ed25519.PrivateKey
	now        func() time.Time
	freshFor   time.Duration
	runner     AuthoritativePersonaRunner
	catalog    PersonaSuiteCatalog
}

// NewEvaluationIssuer creates a signer for claims issued by the composed
// authoritative persona evaluation runner.
func NewEvaluationIssuer(keyID string, privateKey ed25519.PrivateKey, now func() time.Time, freshFor time.Duration, runner AuthoritativePersonaRunner, catalog PersonaSuiteCatalog) (*EvaluationIssuer, error) {
	if strings.TrimSpace(keyID) == "" || len(privateKey) != ed25519.PrivateKeySize || now == nil || freshFor <= 0 || runner == nil || catalog == nil {
		return nil, ErrIssuerConfig
	}
	return &EvaluationIssuer{keyID: keyID, privateKey: append(ed25519.PrivateKey(nil), privateKey...), now: now, freshFor: freshFor, runner: runner, catalog: catalog}, nil
}

// IssuePersonaEvaluation runs the authoritative evaluator, verifies its exact
// release and suite pins, then signs the durable claim consumed by publication.
func (i *EvaluationIssuer) IssuePersonaEvaluation(ctx context.Context, request IssueRequest) (agentpersonastore.SignedPersonaEvaluation, error) {
	if i == nil || i.runner == nil || i.catalog == nil || ctx == nil || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.RunID) == "" || request.PersonaVersionNumber <= 0 {
		return agentpersonastore.SignedPersonaEvaluation{}, ErrIssuerConfig
	}
	if err := validatePersona(request.Persona); err != nil {
		return agentpersonastore.SignedPersonaEvaluation{}, err
	}
	if request.Persona.Version != fmt.Sprintf("v%d", request.PersonaVersionNumber) && request.Persona.Version != fmt.Sprint(request.PersonaVersionNumber) {
		return agentpersonastore.SignedPersonaEvaluation{}, fmt.Errorf("%w: persona version number mismatch", ErrIssuerRun)
	}
	cases, err := i.catalog.ResolvePersonaSuite(request.Persona.EvalSuiteRef)
	if err != nil || len(cases) == 0 {
		return agentpersonastore.SignedPersonaEvaluation{}, fmt.Errorf("%w: persona suite unavailable", ErrIssuerRun)
	}
	personaEvidence, taskRun, err := i.runner.RunPersonaEvaluation(ctx, request.Persona, cloneTaskCases(cases))
	if err != nil {
		return agentpersonastore.SignedPersonaEvaluation{}, fmt.Errorf("%w: %v", ErrIssuerRun, err)
	}
	if err := verifyIssuedRun(request.Persona, cases, personaEvidence, taskRun); err != nil {
		return agentpersonastore.SignedPersonaEvaluation{}, err
	}
	issued := i.now().UTC()
	if issued.IsZero() {
		return agentpersonastore.SignedPersonaEvaluation{}, ErrIssuerConfig
	}
	suiteDigest, err := combinedSuiteDigest(cases, agenteval.DefaultSuite())
	if err != nil {
		return agentpersonastore.SignedPersonaEvaluation{}, fmt.Errorf("%w: suite digest: %v", ErrIssuerRun, err)
	}
	runDigest, err := issuedRunDigest(personaEvidence, taskRun)
	if err != nil {
		return agentpersonastore.SignedPersonaEvaluation{}, fmt.Errorf("%w: run digest: %v", ErrIssuerRun, err)
	}
	claim := agentpersonastore.PersonaEvaluationClaim{
		TenantID: request.TenantID, RunID: request.RunID, PersonaID: request.Persona.PersonaID,
		PersonaVersion: request.PersonaVersionNumber, ProfileDigest: request.Persona.PersonaDigest,
		SuiteDigest: suiteDigest, RunDigest: runDigest, ModelDigest: request.Persona.ModelDigest,
		Passed: true, IssuedAt: issued, ExpiresAt: issued.Add(i.freshFor), KeyID: i.keyID,
	}
	return agentpersonastore.SignPersonaEvaluationClaim(i.privateKey, claim)
}

// IssueRequest identifies the exact persona profile and suite to evaluate.
type IssueRequest struct {
	TenantID             string
	RunID                string
	Persona              PersonaVersion
	PersonaVersionNumber int64
}

func verifyIssuedRun(persona PersonaVersion, cases []TaskCase, evidence PersonaEvidence, run agenteval.Run) error {
	if err := evidence.Verify(); err != nil {
		return fmt.Errorf("%w: persona evidence: %v", ErrIssuerRun, err)
	}
	if err := run.Verify(); err != nil {
		return fmt.Errorf("%w: AGENT2-025 evidence: %v", ErrIssuerRun, err)
	}
	if !evidence.Passed || !run.Passed {
		return fmt.Errorf("%w: failed evaluation cannot be issued", ErrIssuerRun)
	}
	if err := validateLongHorizonBinding(evidence, run); err != nil {
		return fmt.Errorf("%w: %v", ErrIssuerRun, err)
	}
	if err := verifyIssuedCaseObservations(persona, cases, evidence); err != nil {
		return fmt.Errorf("%w: %v", ErrIssuerRun, err)
	}
	caseDigest, err := digestCases(sortedCases(cases))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIssuerRun, err)
	}
	if evidence.SuiteDigest != caseDigest || evidence.PersonaID != persona.PersonaID || evidence.PersonaVersion != persona.Version || evidence.PersonaDigest != persona.PersonaDigest || evidence.Model != persona.Model || evidence.ModelDigest != persona.ModelDigest || evidence.EvalSuiteRef != persona.EvalSuiteRef {
		return fmt.Errorf("%w: persona or exact case-suite digest mismatch", ErrIssuerRun)
	}
	defaultSuite := agenteval.DefaultSuite()
	tasks := make([]agenteval.TaskCase, len(run.Tasks))
	for index, result := range run.Tasks {
		tasks[index] = result.Task
	}
	got, err := suiteDigest(run.SuiteVersion, tasks, run.Thresholds)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIssuerRun, err)
	}
	want, err := suiteDigest(defaultSuite.Version, defaultSuite.Tasks, defaultSuite.Thresholds)
	if err != nil || run.SuiteVersion != defaultSuite.Version || got != want {
		return fmt.Errorf("%w: AGENT2-025 suite differs from the pinned release suite", ErrIssuerRun)
	}
	return nil
}

func verifyIssuedCaseObservations(persona PersonaVersion, cases []TaskCase, evidence PersonaEvidence) error {
	if len(evidence.Cases) != len(cases) {
		return fmt.Errorf("%w: per-case runtime observations are incomplete", ErrEvidence)
	}
	byID := make(map[string]CaseResult, len(evidence.Cases))
	for _, result := range evidence.Cases {
		if _, duplicate := byID[result.ID]; duplicate || result.Evidence == nil || result.Evidence.Verify() != nil {
			return fmt.Errorf("%w: case result lacks verified observation evidence", ErrEvidence)
		}
		byID[result.ID] = result
	}
	for _, testCase := range cases {
		result, ok := byID[testCase.ID]
		if !ok || !result.Passed {
			return fmt.Errorf("%w: case result is missing or failed", ErrEvidence)
		}
		binding := result.Evidence.Binding
		caseDigest, err := digestCases([]TaskCase{testCase})
		if err != nil || binding.CaseID != testCase.ID || binding.CaseDigest != caseDigest || binding.PersonaDigest != persona.PersonaDigest || binding.ModelDigest != persona.ModelDigest {
			return fmt.Errorf("%w: case observation is bound to another case or persona version", ErrEvidence)
		}
	}
	return nil
}

func sortedCases(cases []TaskCase) []TaskCase {
	ordered := cloneTaskCases(cases)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].ID < ordered[j-1].ID; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	return ordered
}

func cloneTaskCases(cases []TaskCase) []TaskCase {
	cloned := append([]TaskCase(nil), cases...)
	for i := range cloned {
		cloned[i].ExpectedCitations = append([]string(nil), cloned[i].ExpectedCitations...)
	}
	return cloned
}

func suiteDigest(version string, tasks any, thresholds any) (string, error) {
	data, err := json.Marshal(struct {
		Version    string `json:"version"`
		Tasks      any    `json:"tasks"`
		Thresholds any    `json:"thresholds"`
	}{version, tasks, thresholds})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent2-025-suite/v1\x00"), data...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func combinedSuiteDigest(cases []TaskCase, suite agenteval.Suite) (string, error) {
	caseHash, err := digestCases(sortedCases(cases))
	if err != nil {
		return "", err
	}
	taskHash, err := suiteDigest(suite.Version, suite.Tasks, suite.Thresholds)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("hcm-next-persona-combined-suite/v1\x00" + caseHash + "\x00" + taskHash))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func issuedRunDigest(persona PersonaEvidence, run agenteval.Run) (string, error) {
	data, err := json.Marshal(struct {
		PersonaDigest     string `json:"persona"`
		LongHorizonDigest string `json:"long_horizon"`
	}{persona.Digest, run.Digest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("hcm-next-persona-issued-run/v1\x00"), data...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
