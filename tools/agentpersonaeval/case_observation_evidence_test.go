package agentpersonaeval

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

type bridgeObservation struct {
	binding   []string
	verdict   string
	reason    string
	sources   [][3]string
	candidate string
	output    string
	evidence  string
	hash      string
	schema    [3]string
	findings  []PersonaObservationFinding
	err       error
}

func (o bridgeObservation) Verify() error           { return o.err }
func (o bridgeObservation) BindingValues() []string { return append([]string(nil), o.binding...) }
func (o bridgeObservation) VerdictCode() string     { return o.verdict }
func (o bridgeObservation) ReasonCode() string      { return o.reason }
func (o bridgeObservation) SourceEvidence() [][3]string {
	return append([][3]string(nil), o.sources...)
}
func (o bridgeObservation) CandidateHash() string   { return o.candidate }
func (o bridgeObservation) OutputHash() string      { return o.output }
func (o bridgeObservation) EvidenceHash() string    { return o.evidence }
func (o bridgeObservation) ObservationHash() string { return o.hash }
func (o bridgeObservation) SchemaValues() [3]string { return o.schema }
func (o bridgeObservation) Findings() []PersonaObservationFinding {
	if o.findings != nil {
		return append([]PersonaObservationFinding(nil), o.findings...)
	}
	return []PersonaObservationFinding{
		{Kind: PersonaFindingAudienceLeak, BoolValue: false, ObservationHash: o.hash, EvidenceDigest: o.evidence},
		{Kind: PersonaFindingPeerPlanChange, BoolValue: false, ObservationHash: o.hash, EvidenceDigest: o.evidence},
		{Kind: PersonaFindingVisibility, TextValue: "PRIVATE", ObservationHash: o.hash, EvidenceDigest: o.evidence},
		{Kind: PersonaFindingCost, IntegerValue: 1, ObservationHash: o.hash, EvidenceDigest: o.evidence},
	}
}

type findinglessBridgeObservation struct{ bridgeObservation }

func (o findinglessBridgeObservation) Findings() []PersonaObservationFinding { return nil }

func bridgeDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func bridgeKey(t *testing.T) (ed25519.PrivateKey, *PersonaToolReceiptKeyring) {
	t.Helper()
	seed := sha256.Sum256([]byte("trusted persona runtime issuer"))
	private := ed25519.NewKeyFromSeed(seed[:])
	keyring, err := NewPersonaToolReceiptKeyring(map[string]ed25519.PublicKey{"runtime-v1": private.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	return private, keyring
}

func bridgeBinding(t *testing.T) ToolSelectionTraceBinding {
	t.Helper()
	persona, testCase := p21Persona(), p21Cases()[0]
	caseHash, err := digestCases([]TaskCase{testCase})
	if err != nil {
		t.Fatal(err)
	}
	return ToolSelectionTraceBinding{SyntheticTenantID: "synthetic-persona-01", RunID: "run-01", CaseID: testCase.ID, CaseDigest: caseHash, PersonaDigest: persona.PersonaDigest, ModelDigest: persona.ModelDigest}
}

func bridgeOutput(binding ToolSelectionTraceBinding) bridgeObservation {
	return bridgeObservation{
		binding: []string{binding.SyntheticTenantID, binding.RunID, binding.CaseID, binding.CaseDigest, binding.PersonaDigest, binding.ModelDigest, "invocation-01", "conversation-01", "thread-01", "post-01"},
		verdict: "SAFE", candidate: bridgeDigest("candidate"), output: bridgeDigest("safe-output"), evidence: bridgeDigest("grounding"), hash: bridgeDigest("observation"),
		sources: [][3]string{{"policy.remote-work.v1", bridgeDigest("location"), bridgeDigest("content")}}, schema: [3]string{"persona.chat-reply.v1", "1", bridgeDigest("schema")},
	}
}

func signedBridgeTrace(t *testing.T) (ToolSelectionTrace, []SignedPersonaToolReceipt, ed25519.PrivateKey, *PersonaToolReceiptKeyring) {
	t.Helper()
	binding := bridgeBinding(t)
	private, keyring := bridgeKey(t)
	base := []ToolSelectionEvent{
		{Sequence: 1, Kind: ToolSelectionAdmitted, Skill: "compensation.lookup", InvocationID: "invoke-01"},
		{Sequence: 2, Kind: ToolSelectionExecuted, Skill: "compensation.lookup", InvocationID: "invoke-01"},
	}
	receipts := make([]SignedPersonaToolReceipt, 0, len(base))
	for i := range base {
		receipt, digest, err := SignPersonaToolReceipt("runtime-v1", private, binding, base[i])
		if err != nil {
			t.Fatal(err)
		}
		base[i].ReceiptDigest = digest
		receipts = append(receipts, receipt)
	}
	trace, err := newToolSelectionTrace(binding, []string{"compensation.lookup"}, base)
	if err != nil {
		t.Fatal(err)
	}
	return trace, receipts, private, keyring
}

func TestTodo_AGENTP_021_CaseEvidenceAuthenticatesBothObservationSources(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	evidence, err := NewPersonaCaseObservationEvidence(trace, bridgeOutput(trace.Binding), receipts, keyring)
	if err != nil {
		t.Fatal(err)
	}
	trace.Events[0].Skill = "caller-mutated"
	receipts[0].Signature[0] ^= 0xff
	if err := evidence.Verify(); err != nil {
		t.Fatalf("evidence retained aliases to caller-owned trace or receipt slices: %v", err)
	}
	if evidence.ToolTraceDigest != trace.Digest || evidence.Output.ObservationHash != bridgeDigest("observation") || len(evidence.ReceiptIssuerIDs) != 1 || evidence.ReceiptIssuerIDs[0] != "runtime-v1" {
		t.Fatalf("case evidence lost trace, output or issuer provenance: %+v", evidence)
	}
}

func TestTodo_AGENTP_021_CaseEvidenceRejectsUnauthenticatedOrMismatchedEvidence(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	for _, test := range []struct {
		name        string
		observation bridgeObservation
		receipts    []SignedPersonaToolReceipt
		keys        *PersonaToolReceiptKeyring
	}{
		{name: "observation from another case", observation: func() bridgeObservation { v := bridgeOutput(trace.Binding); v.binding[2] = "other-case"; return v }(), receipts: receipts, keys: keyring},
		{name: "missing runtime receipt", observation: bridgeOutput(trace.Binding), receipts: receipts[:1], keys: keyring},
		{name: "invalid validator seal", observation: func() bridgeObservation { v := bridgeOutput(trace.Binding); v.err = errors.New("unissued"); return v }(), receipts: receipts, keys: keyring},
		{name: "no trust roots", observation: bridgeOutput(trace.Binding), receipts: receipts, keys: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPersonaCaseObservationEvidence(trace, test.observation, test.receipts, test.keys); err == nil {
				t.Fatal("untrusted or mismatched case observation was accepted")
			}
		})
	}
	foreignSeed := sha256.Sum256([]byte("untrusted signer"))
	foreign := ed25519.NewKeyFromSeed(foreignSeed[:])
	wrongKeys, err := NewPersonaToolReceiptKeyring(map[string]ed25519.PublicKey{"runtime-v1": foreign.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersonaCaseObservationEvidence(trace, bridgeOutput(trace.Binding), receipts, wrongKeys); err == nil || !strings.Contains(err.Error(), "authenticated") {
		t.Fatalf("receipt signed by an unknown authority accepted: %v", err)
	}
}

func TestTodo_AGENTP_021_CaseEvidenceRequiresTypedRuntimeFindings(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	withoutFindings := bridgeOutput(trace.Binding)
	observation := findinglessBridgeObservation{bridgeObservation: withoutFindings}
	if _, err := NewPersonaCaseObservationEvidence(trace, observation, receipts, keyring); err == nil || !strings.Contains(err.Error(), "typed runtime findings") {
		t.Fatalf("case evidence accepted a validator observation without typed findings: %v", err)
	}
}

func TestTodo_AGENTP_021_CaseEvidenceBindsFindingsToValidatorSeals(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	observation := bridgeOutput(trace.Binding)
	findings := observation.Findings()
	findings[0].EvidenceDigest = bridgeDigest("other-evidence")
	observation.findings = findings
	if _, err := NewPersonaCaseObservationEvidence(trace, observation, receipts, keyring); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("finding with foreign evidence digest was accepted: %v", err)
	}
}

func TestTodo_AGENTP_021_IssuanceMetricsUseVerifiedFindings(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	evidence, err := NewPersonaCaseObservationEvidence(trace, bridgeOutput(trace.Binding), receipts, keyring)
	if err != nil {
		t.Fatal(err)
	}
	observation := TaskObservation{AudienceLeak: true, PlanChangedByPeer: true, Visibility: "PUBLIC", Cost: 99, Citations: []string{"caller-source"}, CaseEvidence: &evidence}
	got := observationWithVerifiedFindings(observation)
	if got.AudienceLeak || got.PlanChangedByPeer || got.Visibility != "PRIVATE" || got.Cost != 1 || len(got.Citations) != 1 || got.Citations[0] != "policy.remote-work.v1" {
		t.Fatalf("caller-supplied metrics were not replaced by verified findings: %+v", got)
	}
}

func TestTodo_AGENTP_021_CaseEvidenceRejectsForgedReceiptFields(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	receipts[0].Claim.InvocationID = "attacker-invocation"
	if _, err := NewPersonaCaseObservationEvidence(trace, bridgeOutput(trace.Binding), receipts, keyring); err == nil {
		t.Fatal("receipt with modified invocation binding was accepted")
	}
	trace.Events[1].ReceiptDigest = bridgeDigest("caller-provided")
	trace.Digest = digestToolSelectionTrace(trace)
	if _, err := NewPersonaCaseObservationEvidence(trace, bridgeOutput(trace.Binding), receipts, keyring); err == nil {
		t.Fatal("resealed trace with forged receipt digest was accepted")
	}
}

func TestTodo_AGENTP_021_CaseEvidenceSealAndProposalOnlyTrace(t *testing.T) {
	trace, receipts, _, keyring := signedBridgeTrace(t)
	evidence, err := NewPersonaCaseObservationEvidence(trace, bridgeOutput(trace.Binding), receipts, keyring)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Output.Verdict = "SAFE"
	evidence.Output.OutputDigest = bridgeDigest("replacement")
	if err := evidence.Verify(); err == nil {
		t.Fatal("mutated per-case evidence retained its validator seal")
	}

	proposalOnly, err := newToolSelectionTrace(trace.Binding, []string{"compensation.lookup"}, []ToolSelectionEvent{{Sequence: 1, Kind: ToolSelectionProposal, ProposalName: "compensation.lookup"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersonaCaseObservationEvidence(proposalOnly, bridgeOutput(trace.Binding), nil, keyring); err == nil {
		t.Fatal("model proposal without a server-authenticated receipt became case evidence")
	}
}
