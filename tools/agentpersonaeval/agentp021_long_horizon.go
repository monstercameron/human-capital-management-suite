package agentpersonaeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/agenteval"
)

const (
	// PersonaLongHorizonEvidenceVersion identifies the combined persona and
	// AGENT2-025 evidence contract.
	PersonaLongHorizonEvidenceVersion = "AGENTP-021/v2"
	// PersonaLongHorizonSuiteVersion is the pinned synthetic task suite.
	PersonaLongHorizonSuiteVersion = "agent2-025/v1"
)

var (
	// ErrLongHorizonMissing means no AGENT2-025 outcome was supplied.
	ErrLongHorizonMissing = errors.New("agentpersonaeval: long-horizon evidence is required")
	// ErrLongHorizonStale means the outcome is for a different persona release.
	ErrLongHorizonStale = errors.New("agentpersonaeval: long-horizon evidence is stale")
	// ErrLongHorizonFailed means the supplied AGENT2-025 run did not pass.
	ErrLongHorizonFailed = errors.New("agentpersonaeval: long-horizon evaluation did not pass")
	// ErrLongHorizonUnsealed means the supplied or combined seal is invalid.
	ErrLongHorizonUnsealed = errors.New("agentpersonaeval: long-horizon evidence seal is broken")
)

// LongHorizonEvidence binds a persona evaluation to the real sealed
// AGENT2-025 synthetic-tenant outcome. The duplicated pins make an audit
// record self-describing; Digest seals both nested records and those pins.
type LongHorizonEvidence struct {
	EvidenceVersion   string          `json:"evidence_version"`
	Persona           PersonaEvidence `json:"persona"`
	LongHorizon       agenteval.Run   `json:"long_horizon"`
	PersonaID         string          `json:"persona_id"`
	PersonaVersion    string          `json:"persona_version"`
	PersonaDigest     string          `json:"persona_digest"`
	Model             string          `json:"model"`
	ModelDigest       string          `json:"model_digest"`
	LongHorizonDigest string          `json:"long_horizon_digest"`
	Passed            bool            `json:"passed"`
	Digest            string          `json:"digest"`
}

// EvaluatePersonaWithLongHorizon runs the persona cases and requires the
// already-sealed AGENT2-025 result for the exact same release. It performs no
// provider calls itself; callers supply the real runner outcome.
func EvaluatePersonaWithLongHorizon(persona PersonaVersion, cases []TaskCase, executor TaskExecutor, longHorizon agenteval.Run) (LongHorizonEvidence, error) {
	personaEvidence, err := EvaluatePersona(nil, persona, cases, executor)
	if err != nil {
		return LongHorizonEvidence{}, err
	}
	return SealLongHorizonEvidence(personaEvidence, longHorizon)
}

// SealLongHorizonEvidence combines an existing persona result with a passing,
// sealed AGENT2-025 run after checking every release pin.
func SealLongHorizonEvidence(persona PersonaEvidence, longHorizon agenteval.Run) (LongHorizonEvidence, error) {
	if err := persona.Verify(); err != nil {
		return LongHorizonEvidence{}, fmt.Errorf("%w: persona evidence: %v", ErrLongHorizonUnsealed, err)
	}
	if isEmptyLongHorizon(longHorizon) {
		return LongHorizonEvidence{}, ErrLongHorizonMissing
	}
	if err := longHorizon.Verify(); err != nil {
		return LongHorizonEvidence{}, fmt.Errorf("%w: %v", ErrLongHorizonUnsealed, err)
	}
	if !longHorizon.Passed {
		return LongHorizonEvidence{}, ErrLongHorizonFailed
	}
	if err := validateLongHorizonBinding(persona, longHorizon); err != nil {
		return LongHorizonEvidence{}, err
	}
	evidence := LongHorizonEvidence{
		EvidenceVersion: PersonaLongHorizonEvidenceVersion, Persona: persona,
		LongHorizon: longHorizon, PersonaID: persona.PersonaID,
		PersonaVersion: persona.PersonaVersion, PersonaDigest: persona.PersonaDigest,
		Model: persona.Model, ModelDigest: persona.ModelDigest,
		LongHorizonDigest: longHorizon.Digest, Passed: persona.Passed,
	}
	evidence.Passed = persona.Passed && longHorizon.Passed
	evidence.Digest = digestLongHorizonEvidence(evidence)
	return evidence, nil
}

// Verify checks both nested seals, their exact release binding and the
// combined evidence seal. It refuses a stale or absent AGENT2-025 outcome.
func (evidence LongHorizonEvidence) Verify() error {
	if evidence.Digest == "" || digestLongHorizonEvidence(evidence) != evidence.Digest {
		return ErrLongHorizonUnsealed
	}
	if evidence.EvidenceVersion != PersonaLongHorizonEvidenceVersion {
		return fmt.Errorf("%w: unsupported evidence version", ErrLongHorizonStale)
	}
	if err := evidence.Persona.Verify(); err != nil {
		return fmt.Errorf("%w: persona evidence: %v", ErrLongHorizonUnsealed, err)
	}
	if isEmptyLongHorizon(evidence.LongHorizon) {
		return ErrLongHorizonMissing
	}
	if err := evidence.LongHorizon.Verify(); err != nil {
		return fmt.Errorf("%w: %v", ErrLongHorizonUnsealed, err)
	}
	if !evidence.LongHorizon.Passed {
		return ErrLongHorizonFailed
	}
	if err := validateLongHorizonBinding(evidence.Persona, evidence.LongHorizon); err != nil {
		return err
	}
	if evidence.LongHorizonDigest != evidence.LongHorizon.Digest || evidence.PersonaID != evidence.Persona.PersonaID || evidence.PersonaVersion != evidence.Persona.PersonaVersion || evidence.PersonaDigest != evidence.Persona.PersonaDigest || evidence.Model != evidence.Persona.Model || evidence.ModelDigest != evidence.Persona.ModelDigest || !evidence.Passed {
		return fmt.Errorf("%w: duplicated release pins do not match", ErrLongHorizonStale)
	}
	return nil
}

func validateLongHorizonBinding(persona PersonaEvidence, run agenteval.Run) error {
	release := run.Security.Release
	if run.SuiteVersion != PersonaLongHorizonSuiteVersion || release.PersonaID != persona.PersonaID || release.PersonaVersion != persona.PersonaVersion || release.Model != persona.Model || release.ModelDigest != persona.ModelDigest || release.PromptHash != persona.PersonaDigest {
		return fmt.Errorf("%w: suite, persona, model or digest pin mismatch", ErrLongHorizonStale)
	}
	return nil
}

func isEmptyLongHorizon(run agenteval.Run) bool {
	return strings.TrimSpace(run.Digest) == "" || strings.TrimSpace(run.SuiteVersion) == ""
}

func digestLongHorizonEvidence(evidence LongHorizonEvidence) string {
	copy := evidence
	copy.Digest = ""
	encoded, _ := json.Marshal(copy)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-long-horizon/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
