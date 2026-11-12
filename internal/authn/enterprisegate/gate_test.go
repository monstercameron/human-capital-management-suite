package enterprisegate

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var gateNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func completeRecord() Record {
	evidence := []EvidenceRef{
		{ID: "req-saml", TenantID: "tenant-1", Kind: EvidenceRequirement, SourceRef: "customer:evidence:requirement:saml", Digest: "sha256:req-saml", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
		{ID: "threat-saml", TenantID: "tenant-1", Kind: EvidenceThreatTest, SourceRef: "test:authn:saml-threat", Digest: "sha256:threat-saml", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
		{ID: "operate-saml", TenantID: "tenant-1", Kind: EvidenceOperabilityTest, SourceRef: "test:authn:saml-operability", Digest: "sha256:operate-saml", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
		{ID: "metadata-saml", TenantID: "tenant-1", Kind: EvidenceMetadata, SourceRef: "test:authn:saml-metadata", Digest: "sha256:metadata-saml", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
		{ID: "signature-saml", TenantID: "tenant-1", Kind: EvidenceSignature, SourceRef: "test:authn:saml-signature", Digest: "sha256:signature-saml", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
		{ID: "deprovision-saml", TenantID: "tenant-1", Kind: EvidenceDeprovision, SourceRef: "test:authn:saml-deprovision", Digest: "sha256:deprovision-saml", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
		{ID: "req-scim", TenantID: "tenant-1", Kind: EvidenceRequirement, SourceRef: "customer:evidence:requirement:scim", Digest: "sha256:req-scim", CollectedAt: gateNow.Add(-time.Hour), ExpiresAt: gateNow.Add(30 * 24 * time.Hour)},
	}
	return Record{
		SchemaVersion:      SchemaVersion,
		DecisionID:         "authn-008-decision-1",
		TenantID:           "tenant-1",
		PartnerManifestRef: "partner-manifest:PLACEHOLDER",
		Evidence:           evidence,
		DecisionOwner:      "identity-owner",
		DecisionMaker:      "security-reviewer",
		DecisionSignature:  "signature:authn-008-test",
		DecidedAt:          gateNow,
		Paths: []ProtocolPath{
			{Protocol: ProtocolSAML, Outcome: OutcomeApproved, Reason: "customer evidence and conformance suite justify the bounded path", Owner: "identity-owner", RequirementEvidenceID: "req-saml", ThreatEvidenceID: "threat-saml", OperabilityEvidenceID: "operate-saml", Conformance: ConformanceEvidence{MetadataEvidenceID: "metadata-saml", SignatureEvidenceID: "signature-saml", DeprovisionEvidenceID: "deprovision-saml"}},
			{Protocol: ProtocolSCIM, Outcome: OutcomeNotRequired, Reason: "customer evidence does not require provisioning support", Owner: "identity-owner", RequirementEvidenceID: "req-scim"},
		},
	}
}

func TestTodo_AUTHN_008(t *testing.T) {
	record := completeRecord()
	decision, err := EvaluateAt(record, gateNow)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != GateApproved || decision.Digest == "" {
		t.Fatalf("decision = %+v, want approved with digest", decision)
	}

	record.Paths[0].Conformance.DeprovisionEvidenceID = ""
	decision, err = EvaluateAt(record, gateNow)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != GateBlocked || !contains(decision.Missing, "SAML deprovision conformance evidence") {
		t.Fatalf("incomplete approved path = %+v, want blocked deprovision gate", decision)
	}
}

func TestTodo_AUTHN_008_Integration(t *testing.T) {
	record := completeRecord()
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Record
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	want, err := EvaluateAt(record, gateNow)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EvaluateAt(decoded, gateNow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip decision = %+v, want %+v", got, want)
	}
	record.Digest = Digest(record)
	if err := record.VerifyDigest(); err != nil {
		t.Fatal(err)
	}
	record.DecisionSignature = "signature:tampered"
	if _, err := EvaluateAt(record, gateNow); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tampered digest error = %v, want digest mismatch", err)
	}
}

func TestTodo_AUTHN_008_Security(t *testing.T) {
	record := completeRecord()
	record.Evidence[0].TenantID = "tenant-other"
	if _, err := EvaluateAt(record, gateNow); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("foreign-tenant evidence error = %v, want invalid record", err)
	}

	placeholder := PlaceholderRecord()
	decision, err := EvaluateAt(placeholder, gateNow)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status == GateApproved || len(decision.HumanInputsRequired) == 0 {
		t.Fatalf("placeholder decision = %+v, want a non-approved human gate", decision)
	}
	if strings.Contains(decision.Explanation, "customer-secret") {
		t.Fatal("explanation leaked protected evidence content")
	}
}

func TestTodo_AUTHN_008_Conformance(t *testing.T) {
	for _, protocol := range []Protocol{ProtocolSAML, ProtocolSCIM} {
		record := completeRecord()
		record.Paths[0].Protocol = protocol
		record.Paths[1].Protocol = otherProtocol(protocol)
		if _, err := EvaluateAt(record, gateNow); err != nil {
			t.Fatalf("protocol %s conformance: %v", protocol, err)
		}
	}
}

func TestTodo_AUTHN_008_Mutation(t *testing.T) {
	mutations := []func(*Record){
		func(r *Record) { r.PartnerManifestRef = "" },
		func(r *Record) { r.Paths[0].Owner = "" },
		func(r *Record) { r.Evidence[0].Digest = "" },
	}
	for i, mutate := range mutations {
		record := completeRecord()
		mutate(&record)
		if _, err := EvaluateAt(record, gateNow); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("mutation %d error = %v, want invalid record", i, err)
		}
	}

	for _, mutate := range []func(*Record){
		func(r *Record) { r.Paths[0].ThreatEvidenceID = "" },
		func(r *Record) { r.Paths[0].Conformance.SignatureEvidenceID = "" },
	} {
		record := completeRecord()
		mutate(&record)
		decision, err := EvaluateAt(record, gateNow)
		if err != nil || decision.Status == GateApproved {
			t.Fatalf("incomplete evidence decision = %+v, err=%v; want non-approved gate", decision, err)
		}
	}
}

func FuzzTodo_AUTHN_008(f *testing.F) {
	f.Add("SAML", "tenant-1", "partner-manifest:PLACEHOLDER")
	f.Add("SCIM", "", "")
	f.Fuzz(func(t *testing.T, protocol, tenant, partnerRef string) {
		record := PlaceholderRecord()
		record.TenantID = tenant
		record.PartnerManifestRef = partnerRef
		record.Paths[0].Protocol = Protocol(protocol)
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("validation panicked for fuzz input: %v", recovered)
			}
		}()
		_, _ = EvaluateAt(record, gateNow)
	})
}

func TestSchemaDescriptor(t *testing.T) {
	schema := Schema()
	if schema.Version != SchemaVersion || len(schema.RequiredFields) == 0 || len(schema.Protocols) != 2 {
		t.Fatalf("schema = %+v, want versioned SAML/SCIM schema", schema)
	}
}

func TestRecordValidateAt_SecurityBranches(t *testing.T) {
	now := gateNow
	cases := []struct {
		name   string
		mutate func(*Record)
		want   error
	}{
		{"schema", func(r *Record) { r.SchemaVersion = 99 }, ErrInvalidRecord},
		{"decision_id", func(r *Record) { r.DecisionID = "bad id" }, ErrInvalidRecord},
		{"manifest_prefix", func(r *Record) { r.PartnerManifestRef = "manifest:wrong" }, ErrInvalidRecord},
		{"empty_evidence", func(r *Record) { r.Evidence = nil }, ErrInvalidRecord},
		{"duplicate_evidence", func(r *Record) { r.Evidence = append(r.Evidence, r.Evidence[0]) }, ErrInvalidRecord},
		{"foreign_evidence_tenant", func(r *Record) { r.Evidence[0].TenantID = "tenant-other" }, ErrInvalidRecord},
		{"unknown_evidence_kind", func(r *Record) { r.Evidence[0].Kind = EvidenceKind("unknown") }, ErrInvalidRecord},
		{"bad_evidence_digest", func(r *Record) { r.Evidence[0].Digest = "not-sha256" }, ErrInvalidRecord},
		{"evidence_expiry_order", func(r *Record) { r.Evidence[0].ExpiresAt = r.Evidence[0].CollectedAt }, ErrInvalidRecord},
		{"missing_path", func(r *Record) { r.Paths[0].Owner = "" }, ErrInvalidRecord},
		{"duplicate_protocol", func(r *Record) { r.Paths[1].Protocol = r.Paths[0].Protocol }, ErrInvalidRecord},
		{"pending_reason_allowed", func(r *Record) {
			r.Paths[0].Outcome = OutcomePending
			r.Paths[0].Reason = ""
			r.DecidedAt = time.Time{}
			r.DecisionOwner, r.DecisionMaker, r.DecisionSignature = "", "", ""
		}, nil},
		{"final_reason_required", func(r *Record) { r.Paths[0].Reason = "" }, ErrInvalidRecord},
		{"bad_reference", func(r *Record) { r.Paths[0].ThreatEvidenceID = "bad ref" }, ErrInvalidRecord},
		{"missing_saml", func(r *Record) { r.Paths[0].Protocol = ProtocolSCIM; r.Paths[1].Protocol = ProtocolSCIM }, ErrInvalidRecord},
		{"partial_decision_metadata", func(r *Record) { r.DecisionOwner = "identity-owner"; r.DecidedAt = time.Time{} }, ErrInvalidRecord},
		{"self_approval", func(r *Record) { r.DecisionMaker = r.DecisionOwner }, ErrInvalidRecord},
		{"future_decision", func(r *Record) { r.DecidedAt = now.Add(time.Second) }, ErrInvalidRecord},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := completeRecord()
			tc.mutate(&record)
			err := record.ValidateAt(now)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("ValidateAt = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateAt = %v, want errors.Is(%v)", err, tc.want)
			}
		})
	}
	if err := completeRecord().ValidateAt(time.Time{}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("zero evaluation time = %v, want ErrInvalidRecord", err)
	}
}

func TestEvaluateWrappersAndStatuses(t *testing.T) {
	record := completeRecord()
	// Evaluate reads the real clock, so its record must stay unexpired at the
	// time the test runs; the explicit-clock wrappers keep the gateNow fixture.
	live := completeRecord()
	for i := range live.Evidence {
		live.Evidence[i].ExpiresAt = time.Now().UTC().Add(30 * 24 * time.Hour)
	}
	for name, evaluate := range map[string]func(Record) (Decision, error){
		"evaluate": func(Record) (Decision, error) { return Evaluate(live) },
		"explain":  func(r Record) (Decision, error) { return Explain(r, gateNow) },
		"method":   func(r Record) (Decision, error) { return r.Explain(gateNow) },
	} {
		t.Run(name, func(t *testing.T) {
			got, err := evaluate(record)
			if err != nil || got.Status != GateApproved || got.Digest == "" {
				t.Fatalf("decision=%+v err=%v, want approved decision", got, err)
			}
		})
	}
	for name, outcome := range map[string]Outcome{"rejected": OutcomeRejected, "not_required": OutcomeNotRequired} {
		t.Run(name, func(t *testing.T) {
			r := completeRecord()
			r.Paths[0].Outcome, r.Paths[0].Reason = outcome, "customer decision"
			r.Paths[0].ThreatEvidenceID, r.Paths[0].OperabilityEvidenceID = "", ""
			r.Paths[0].Conformance = ConformanceEvidence{}
			r.Paths[1].Outcome, r.Paths[1].Reason = outcome, "customer decision"
			decision, err := EvaluateAt(r, gateNow)
			if err != nil || decision.Status != GateNotRequired {
				t.Fatalf("decision=%+v err=%v, want no-support-required", decision, err)
			}
		})
	}
	pending := completeRecord()
	pending.Paths[0].Outcome, pending.Paths[0].Reason = OutcomePending, ""
	pending.Paths[1].Outcome, pending.Paths[1].Reason = OutcomePending, ""
	pending.DecidedAt, pending.DecisionOwner, pending.DecisionMaker, pending.DecisionSignature = time.Time{}, "", "", ""
	decision, err := EvaluateAt(pending, gateNow)
	if err != nil || decision.Status != GatePending || len(decision.Missing) != 2 {
		t.Fatalf("pending decision=%+v err=%v", decision, err)
	}
}

func TestDigestVerificationBoundaries(t *testing.T) {
	record := completeRecord()
	if err := record.VerifyDigest(); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("missing digest = %v, want ErrDigestMismatch", err)
	}
	record.Digest = Digest(record)
	if err := record.VerifyDigest(); err != nil {
		t.Fatalf("valid digest = %v", err)
	}
	record.DecisionSignature = "tampered"
	if err := record.VerifyDigest(); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tampered digest = %v, want ErrDigestMismatch", err)
	}
	if Version() != SchemaVersion {
		t.Fatalf("Version = %d, want %d", Version(), SchemaVersion)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func otherProtocol(protocol Protocol) Protocol {
	if protocol == ProtocolSAML {
		return ProtocolSCIM
	}
	return ProtocolSAML
}
