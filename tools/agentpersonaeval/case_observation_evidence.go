package agentpersonaeval

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var errCaseObservationEvidence = errors.New("agentpersonaeval: invalid case observation evidence")

// TypedPersonaOutputObservation is the package-neutral surface exposed by the
// application validator. Verify must reject values not issued by that
// validator; the digest accessors are covered by its private seal.
type TypedPersonaOutputObservation interface {
	Verify() error
	BindingValues() []string
	VerdictCode() string
	ReasonCode() string
	SourceEvidence() [][3]string
	CandidateHash() string
	OutputHash() string
	EvidenceHash() string
	ObservationHash() string
	SchemaValues() [3]string
}

// PersonaFindingKind identifies a validator-issued finding used by persona
// publication metrics. Findings are observations of the served runtime, not
// assertions supplied by a model-facing executor.
type PersonaFindingKind string

const (
	// PersonaFindingAudienceLeak records whether output crossed the audience floor.
	PersonaFindingAudienceLeak PersonaFindingKind = "audience_leak"
	// PersonaFindingPeerPlanChange records whether quarantined peer text changed the plan.
	PersonaFindingPeerPlanChange PersonaFindingKind = "peer_plan_change"
	// PersonaFindingVisibility records the server-selected output visibility.
	PersonaFindingVisibility PersonaFindingKind = "visibility"
	// PersonaFindingCost records settled runtime cost in evaluator units.
	PersonaFindingCost PersonaFindingKind = "cost"
)

// PersonaObservationFinding is a bounded, typed result issued with a
// validator observation. ObservationHash and EvidenceDigest bind the finding
// to the exact validator result and its source grounding seal.
type PersonaObservationFinding struct {
	Kind            PersonaFindingKind `json:"kind"`
	BoolValue       bool               `json:"bool_value,omitempty"`
	TextValue       string             `json:"text_value,omitempty"`
	IntegerValue    int                `json:"integer_value,omitempty"`
	ObservationHash string             `json:"observation_hash"`
	EvidenceDigest  string             `json:"evidence_digest"`
}

// TypedPersonaOutputFindings is implemented by the trusted application
// validator alongside TypedPersonaOutputObservation. Its Verify method must
// reject caller-created observations; the evaluator additionally checks every
// finding's provenance pins.
type TypedPersonaOutputFindings interface {
	Findings() []PersonaObservationFinding
}

// PersonaToolReceiptClaim binds one server-side tool event to an exact
// synthetic run, case, persona and model. Proposal events are never receipts.
type PersonaToolReceiptClaim struct {
	Binding      ToolSelectionTraceBinding `json:"binding"`
	Sequence     uint32                    `json:"sequence"`
	Kind         ToolSelectionEventKind    `json:"kind"`
	Skill        string                    `json:"skill,omitempty"`
	InvocationID string                    `json:"invocation_id"`
	IssuerKeyID  string                    `json:"issuer_key_id"`
}

// SignedPersonaToolReceipt is an authenticated runtime receipt. Its digest is
// placed in the corresponding trace event; the signature covers the full
// claim, including the binding and issuer key id.
type SignedPersonaToolReceipt struct {
	Claim     PersonaToolReceiptClaim `json:"claim"`
	Signature []byte                  `json:"signature"`
}

// PersonaToolReceiptKeyring contains only public keys trusted by composition.
// It has no mutators, and construction defensively copies all key material.
type PersonaToolReceiptKeyring struct {
	keys map[string]ed25519.PublicKey
}

// NewPersonaToolReceiptKeyring builds a verifier from trusted runtime issuer
// keys. Callers must source these keys from the application's trust config.
func NewPersonaToolReceiptKeyring(keys map[string]ed25519.PublicKey) (*PersonaToolReceiptKeyring, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no trusted runtime receipt issuers", errCaseObservationEvidence)
	}
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		if strings.TrimSpace(id) == "" || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: invalid runtime receipt issuer key", errCaseObservationEvidence)
		}
		cloned[id] = append(ed25519.PublicKey(nil), key...)
	}
	return &PersonaToolReceiptKeyring{keys: cloned}, nil
}

// SignPersonaToolReceipt signs a single server-side event. The private key
// must be held by the trusted runtime receipt issuer, never by the evaluator
// or model-facing executor.
func SignPersonaToolReceipt(keyID string, privateKey ed25519.PrivateKey, binding ToolSelectionTraceBinding, event ToolSelectionEvent) (SignedPersonaToolReceipt, string, error) {
	if strings.TrimSpace(keyID) == "" || len(privateKey) != ed25519.PrivateKeySize || validateToolSelectionBinding(binding) != nil ||
		!isReceiptEvent(event.Kind) || event.Sequence == 0 || strings.TrimSpace(event.InvocationID) == "" {
		return SignedPersonaToolReceipt{}, "", fmt.Errorf("%w: incomplete runtime receipt claim", errCaseObservationEvidence)
	}
	claim := PersonaToolReceiptClaim{Binding: binding, Sequence: event.Sequence, Kind: event.Kind, Skill: event.Skill, InvocationID: event.InvocationID, IssuerKeyID: keyID}
	if !validReceiptEventShape(claim) {
		return SignedPersonaToolReceipt{}, "", fmt.Errorf("%w: malformed runtime receipt claim", errCaseObservationEvidence)
	}
	encoded, err := json.Marshal(claim)
	if err != nil {
		return SignedPersonaToolReceipt{}, "", fmt.Errorf("%w: encode runtime receipt: %v", errCaseObservationEvidence, err)
	}
	receipt := SignedPersonaToolReceipt{Claim: claim, Signature: ed25519.Sign(privateKey, personaToolReceiptMessage(encoded))}
	return receipt, personaToolReceiptDigest(claim), nil
}

func (keyring *PersonaToolReceiptKeyring) verify(receipt SignedPersonaToolReceipt, trace ToolSelectionTrace, event ToolSelectionEvent) error {
	if keyring == nil || !isReceiptEvent(event.Kind) || !validReceiptEventShape(receipt.Claim) ||
		receipt.Claim.Binding != trace.Binding || receipt.Claim.Sequence != event.Sequence || receipt.Claim.Kind != event.Kind ||
		receipt.Claim.Skill != event.Skill || receipt.Claim.InvocationID != event.InvocationID ||
		event.ReceiptDigest != personaToolReceiptDigest(receipt.Claim) || len(receipt.Signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: receipt does not match trace event", errCaseObservationEvidence)
	}
	key := keyring.keys[receipt.Claim.IssuerKeyID]
	encoded, err := json.Marshal(receipt.Claim)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, personaToolReceiptMessage(encoded), receipt.Signature) {
		return fmt.Errorf("%w: runtime receipt issuer is not authenticated", errCaseObservationEvidence)
	}
	return nil
}

// PersonaOutputObservationEvidence is a content-safe projection of the
// validator-issued typed output observation. It records no model text.
type PersonaOutputObservationEvidence struct {
	Verdict         string                      `json:"verdict"`
	Reason          string                      `json:"reason,omitempty"`
	SchemaID        string                      `json:"schema_id"`
	SchemaVersion   string                      `json:"schema_version"`
	SchemaDigest    string                      `json:"schema_digest"`
	CandidateDigest string                      `json:"candidate_digest"`
	OutputDigest    string                      `json:"output_digest,omitempty"`
	EvidenceDigest  string                      `json:"evidence_digest"`
	ObservationHash string                      `json:"observation_hash"`
	Grounding       []PersonaGroundingPin       `json:"grounding,omitempty"`
	Findings        []PersonaObservationFinding `json:"findings,omitempty"`
}

// PersonaGroundingPin preserves opaque, validator-checked source provenance.
type PersonaGroundingPin struct {
	SourceID       string `json:"source_id"`
	LocationDigest string `json:"location_digest"`
	ContentDigest  string `json:"content_digest"`
}

// PersonaCaseObservationEvidence joins the sealed tool trace and validator
// observation for one exact case. ReceiptIssuerIDs identify the trusted
// runtime keys that authenticated server-side trace events.
type PersonaCaseObservationEvidence struct {
	Binding          ToolSelectionTraceBinding        `json:"binding"`
	ToolTrace        ToolSelectionTrace               `json:"tool_trace"`
	ToolTraceDigest  string                           `json:"tool_trace_digest"`
	Output           PersonaOutputObservationEvidence `json:"output"`
	RuntimeReceipts  []SignedPersonaToolReceipt       `json:"runtime_receipts"`
	ReceiptIssuerIDs []string                         `json:"receipt_issuer_ids"`
	Digest           string                           `json:"digest"`
	seal             *personaCaseObservationSeal
}

type personaCaseObservationSeal struct{ digest string }

// NewPersonaCaseObservationEvidence verifies the sealed trace, every signed
// server-side event receipt and the application-issued output observation.
// It fails closed when receipts are missing, duplicated, untrusted or stale.
func NewPersonaCaseObservationEvidence(trace ToolSelectionTrace, observation TypedPersonaOutputObservation, receipts []SignedPersonaToolReceipt, keyring *PersonaToolReceiptKeyring) (PersonaCaseObservationEvidence, error) {
	findings, hasFindings := observation.(TypedPersonaOutputFindings)
	if trace.Verify() != nil || observation == nil || observation.Verify() != nil || !hasFindings || keyring == nil {
		return PersonaCaseObservationEvidence{}, fmt.Errorf("%w: trace, validator observation and trusted keyring are required", errCaseObservationEvidence)
	}
	if err := matchOutputObservationBinding(trace.Binding, observation.BindingValues()); err != nil {
		return PersonaCaseObservationEvidence{}, err
	}
	receiptBySequence := make(map[uint32]SignedPersonaToolReceipt, len(receipts))
	for _, receipt := range receipts {
		sequence := receipt.Claim.Sequence
		if sequence == 0 {
			return PersonaCaseObservationEvidence{}, fmt.Errorf("%w: receipt sequence is missing", errCaseObservationEvidence)
		}
		if _, duplicate := receiptBySequence[sequence]; duplicate {
			return PersonaCaseObservationEvidence{}, fmt.Errorf("%w: duplicate runtime receipt", errCaseObservationEvidence)
		}
		receiptBySequence[sequence] = receipt
	}
	issuerIDs := make(map[string]bool)
	for _, event := range trace.Events {
		if !isReceiptEvent(event.Kind) {
			continue
		}
		receipt, ok := receiptBySequence[event.Sequence]
		if !ok {
			return PersonaCaseObservationEvidence{}, fmt.Errorf("%w: server-side trace event has no signed receipt", errCaseObservationEvidence)
		}
		if err := keyring.verify(receipt, trace, event); err != nil {
			return PersonaCaseObservationEvidence{}, err
		}
		issuerIDs[receipt.Claim.IssuerKeyID] = true
		delete(receiptBySequence, event.Sequence)
	}
	if len(receiptBySequence) != 0 {
		return PersonaCaseObservationEvidence{}, fmt.Errorf("%w: unreferenced runtime receipt", errCaseObservationEvidence)
	}
	if len(issuerIDs) == 0 {
		return PersonaCaseObservationEvidence{}, fmt.Errorf("%w: case evidence requires an authenticated runtime receipt", errCaseObservationEvidence)
	}
	output, err := outputObservationProjection(observation)
	if err != nil {
		return PersonaCaseObservationEvidence{}, err
	}
	output.Findings, err = validatePersonaFindings(findings.Findings(), observation)
	if err != nil {
		return PersonaCaseObservationEvidence{}, err
	}
	orderedIssuers := make([]string, 0, len(issuerIDs))
	for id := range issuerIDs {
		orderedIssuers = append(orderedIssuers, id)
	}
	sort.Strings(orderedIssuers)
	clonedReceipts := append([]SignedPersonaToolReceipt(nil), receipts...)
	for i := range clonedReceipts {
		clonedReceipts[i].Signature = append([]byte(nil), clonedReceipts[i].Signature...)
	}
	sort.Slice(clonedReceipts, func(i, j int) bool { return clonedReceipts[i].Claim.Sequence < clonedReceipts[j].Claim.Sequence })
	clonedTrace := trace
	clonedTrace.AllowedSkills = append([]string(nil), trace.AllowedSkills...)
	clonedTrace.Events = append([]ToolSelectionEvent(nil), trace.Events...)
	evidence := PersonaCaseObservationEvidence{Binding: trace.Binding, ToolTrace: clonedTrace, ToolTraceDigest: trace.Digest, Output: output, RuntimeReceipts: clonedReceipts, ReceiptIssuerIDs: orderedIssuers}
	evidence.Digest = digestPersonaCaseObservationEvidence(evidence)
	evidence.seal = &personaCaseObservationSeal{digest: evidence.Digest}
	return evidence, nil
}

// Verify checks the immutable projection seal and its required provenance.
func (evidence PersonaCaseObservationEvidence) Verify() error {
	if evidence.seal == nil || evidence.seal.digest != evidence.Digest || validateToolSelectionBinding(evidence.Binding) != nil ||
		evidence.ToolTrace.Verify() != nil || evidence.ToolTrace.Binding != evidence.Binding || evidence.ToolTrace.Digest != evidence.ToolTraceDigest || !validDigest(evidence.ToolTraceDigest) ||
		!validDigest(evidence.Output.CandidateDigest) || !validDigest(evidence.Output.EvidenceDigest) || !validDigest(evidence.Output.ObservationHash) ||
		(evidence.Output.Verdict != "SAFE" && evidence.Output.Verdict != "REJECTED") || len(evidence.ReceiptIssuerIDs) == 0 || evidence.Digest == "" ||
		evidence.Digest != digestPersonaCaseObservationEvidence(evidence) {
		return fmt.Errorf("%w: case evidence seal or provenance is invalid", errCaseObservationEvidence)
	}
	if len(evidence.RuntimeReceipts) == 0 && hasReceiptEvent(evidence.ToolTrace.Events) || len(evidence.RuntimeReceipts) != countReceiptEvents(evidence.ToolTrace.Events) {
		return fmt.Errorf("%w: signed runtime receipts do not cover the sealed trace", errCaseObservationEvidence)
	}
	if evidence.Output.Verdict == "SAFE" && (evidence.Output.Reason != "" || !validDigest(evidence.Output.OutputDigest) || len(evidence.Output.Grounding) == 0) {
		return fmt.Errorf("%w: safe output lacks validated grounding", errCaseObservationEvidence)
	}
	if evidence.Output.Verdict == "REJECTED" && evidence.Output.Reason == "" {
		return fmt.Errorf("%w: rejected output lacks typed reason", errCaseObservationEvidence)
	}
	for i, id := range evidence.ReceiptIssuerIDs {
		if strings.TrimSpace(id) == "" || (i > 0 && evidence.ReceiptIssuerIDs[i-1] >= id) {
			return fmt.Errorf("%w: issuer provenance is not canonical", errCaseObservationEvidence)
		}
	}
	if err := validateProjectedPersonaFindings(evidence.Output.Findings, evidence.Output.ObservationHash, evidence.Output.EvidenceDigest); err != nil {
		return err
	}
	return nil
}

func matchOutputObservationBinding(binding ToolSelectionTraceBinding, values []string) error {
	if len(values) != 10 || values[0] != binding.SyntheticTenantID || values[1] != binding.RunID || values[2] != binding.CaseID ||
		values[3] != binding.CaseDigest || values[4] != binding.PersonaDigest || values[5] != binding.ModelDigest {
		return fmt.Errorf("%w: typed output observation is bound to another case or version", errCaseObservationEvidence)
	}
	for _, value := range values[6:] {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: typed output observation lacks source invocation binding", errCaseObservationEvidence)
		}
	}
	return nil
}

func outputObservationProjection(observation TypedPersonaOutputObservation) (PersonaOutputObservationEvidence, error) {
	schema := observation.SchemaValues()
	output := PersonaOutputObservationEvidence{Verdict: observation.VerdictCode(), Reason: observation.ReasonCode(), SchemaID: schema[0], SchemaVersion: schema[1], SchemaDigest: schema[2], CandidateDigest: observation.CandidateHash(), OutputDigest: observation.OutputHash(), EvidenceDigest: observation.EvidenceHash(), ObservationHash: observation.ObservationHash()}
	if output.Verdict != "SAFE" && output.Verdict != "REJECTED" || !validDigest(output.CandidateDigest) || !validDigest(output.EvidenceDigest) || !validDigest(output.ObservationHash) {
		return PersonaOutputObservationEvidence{}, fmt.Errorf("%w: typed output observation fields are invalid", errCaseObservationEvidence)
	}
	if strings.TrimSpace(output.SchemaID) == "" || strings.TrimSpace(output.SchemaVersion) == "" || !validDigest(output.SchemaDigest) {
		return PersonaOutputObservationEvidence{}, fmt.Errorf("%w: output schema pin is incomplete", errCaseObservationEvidence)
	}
	if output.Verdict == "SAFE" && (output.Reason != "" || !validDigest(output.OutputDigest)) || output.Verdict == "REJECTED" && output.Reason == "" {
		return PersonaOutputObservationEvidence{}, fmt.Errorf("%w: typed output verdict is incomplete", errCaseObservationEvidence)
	}
	for _, source := range observation.SourceEvidence() {
		pin := PersonaGroundingPin{SourceID: source[0], LocationDigest: source[1], ContentDigest: source[2]}
		if strings.TrimSpace(pin.SourceID) == "" || !validDigest(pin.LocationDigest) || !validDigest(pin.ContentDigest) {
			return PersonaOutputObservationEvidence{}, fmt.Errorf("%w: malformed grounding provenance", errCaseObservationEvidence)
		}
		output.Grounding = append(output.Grounding, pin)
	}
	if output.Verdict == "SAFE" && len(output.Grounding) == 0 {
		return PersonaOutputObservationEvidence{}, fmt.Errorf("%w: safe output has no grounded source evidence", errCaseObservationEvidence)
	}
	return output, nil
}

func validatePersonaFindings(findings []PersonaObservationFinding, observation TypedPersonaOutputObservation) ([]PersonaObservationFinding, error) {
	if len(findings) == 0 {
		return nil, fmt.Errorf("%w: typed runtime findings are required", errCaseObservationEvidence)
	}
	validated := append([]PersonaObservationFinding(nil), findings...)
	seen := make(map[PersonaFindingKind]bool, len(validated))
	for _, finding := range validated {
		if seen[finding.Kind] || finding.ObservationHash != observation.ObservationHash() || finding.EvidenceDigest != observation.EvidenceHash() {
			return nil, fmt.Errorf("%w: finding is duplicated or not bound to validator observation", errCaseObservationEvidence)
		}
		seen[finding.Kind] = true
		if err := validatePersonaFinding(finding); err != nil {
			return nil, err
		}
	}
	for _, kind := range []PersonaFindingKind{PersonaFindingAudienceLeak, PersonaFindingPeerPlanChange, PersonaFindingVisibility, PersonaFindingCost} {
		if !seen[kind] {
			return nil, fmt.Errorf("%w: finding %q is required", errCaseObservationEvidence, kind)
		}
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].Kind < validated[j].Kind })
	return validated, nil
}

func validateProjectedPersonaFindings(findings []PersonaObservationFinding, observationHash, evidenceDigest string) error {
	if len(findings) != 4 {
		return fmt.Errorf("%w: projected runtime findings are incomplete", errCaseObservationEvidence)
	}
	seen := make(map[PersonaFindingKind]bool, len(findings))
	for _, finding := range findings {
		if finding.ObservationHash != observationHash || finding.EvidenceDigest != evidenceDigest || seen[finding.Kind] {
			return fmt.Errorf("%w: projected finding provenance is invalid", errCaseObservationEvidence)
		}
		seen[finding.Kind] = true
		if err := validatePersonaFinding(finding); err != nil {
			return err
		}
	}
	return nil
}

func validatePersonaFinding(finding PersonaObservationFinding) error {
	if !validDigest(finding.ObservationHash) || !validDigest(finding.EvidenceDigest) {
		return fmt.Errorf("%w: finding provenance digest is invalid", errCaseObservationEvidence)
	}
	switch finding.Kind {
	case PersonaFindingAudienceLeak, PersonaFindingPeerPlanChange:
		if finding.TextValue != "" || finding.IntegerValue != 0 {
			return fmt.Errorf("%w: boolean finding has an invalid value", errCaseObservationEvidence)
		}
	case PersonaFindingVisibility:
		if finding.TextValue != "PUBLIC" && finding.TextValue != "PRIVATE" || finding.BoolValue || finding.IntegerValue != 0 {
			return fmt.Errorf("%w: visibility finding is invalid", errCaseObservationEvidence)
		}
	case PersonaFindingCost:
		if finding.BoolValue || finding.TextValue != "" || finding.IntegerValue < 0 {
			return fmt.Errorf("%w: cost finding is invalid", errCaseObservationEvidence)
		}
	default:
		return fmt.Errorf("%w: unknown finding kind", errCaseObservationEvidence)
	}
	return nil
}

func validReceiptEventShape(claim PersonaToolReceiptClaim) bool {
	if validateToolSelectionBinding(claim.Binding) != nil || claim.Sequence == 0 || strings.TrimSpace(claim.IssuerKeyID) == "" || strings.TrimSpace(claim.InvocationID) == "" {
		return false
	}
	switch claim.Kind {
	case ToolSelectionDenied:
		return strings.TrimSpace(claim.Skill) != ""
	case ToolSelectionAdmitted, ToolSelectionExecuted:
		return strings.TrimSpace(claim.Skill) != ""
	case ToolSelectionNoCall:
		return claim.Skill == ""
	default:
		return false
	}
}

func isReceiptEvent(kind ToolSelectionEventKind) bool {
	switch kind {
	case ToolSelectionDenied, ToolSelectionAdmitted, ToolSelectionExecuted, ToolSelectionNoCall:
		return true
	default:
		return false
	}
}

func hasReceiptEvent(events []ToolSelectionEvent) bool { return countReceiptEvents(events) > 0 }

func countReceiptEvents(events []ToolSelectionEvent) int {
	count := 0
	for _, event := range events {
		if isReceiptEvent(event.Kind) {
			count++
		}
	}
	return count
}

func personaToolReceiptMessage(encoded []byte) []byte {
	return append([]byte("hcm-next-persona-tool-receipt/v1\x00"), encoded...)
}

func personaToolReceiptDigest(claim PersonaToolReceiptClaim) string {
	encoded, _ := json.Marshal(claim)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-tool-receipt-digest/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestPersonaCaseObservationEvidence(evidence PersonaCaseObservationEvidence) string {
	evidence.Digest = ""
	encoded, _ := json.Marshal(evidence)
	sum := sha256.Sum256(append([]byte("hcm-next-persona-case-observation/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
