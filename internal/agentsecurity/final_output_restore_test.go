package agentsecurity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type restorationProbe struct {
	projection                  FinalOutputPersistence
	validationErr, authorityErr error
	validations, authorizations int
	mutate                      func(*FinalOutputRecoveryRecord)
}

func (p *restorationProbe) RevalidateFinalOutputRecovery(_ context.Context, record FinalOutputRecoveryRecord) (FinalOutputPersistence, error) {
	p.validations++
	if p.mutate != nil {
		p.mutate(&record)
	}
	return p.projection, p.validationErr
}

func (p *restorationProbe) AuthorizeRecoveredFinalOutput(_ context.Context, projection FinalOutputPersistence) error {
	p.authorizations++
	if _, _, err := projection.Payload(); err != nil {
		return err
	}
	return p.authorityErr
}

func fullSignedRecoveryRecord(t *testing.T, projection FinalOutputPersistence) (FinalOutputRecoveryRecord, *FinalOutputRecoveryAuthority, *FinalOutputRecoveryVerifier) {
	t.Helper()
	record, authority, verifier := signedRecoveryRecord(t, projection)
	payload, answer, err := projection.Payload()
	if err != nil {
		t.Fatal(err)
	}
	record.SealedPayload, err = json.Marshal(struct {
		Payload DraftOutput `json:"payload"`
		Answer  Answer      `json:"answer"`
	}{payload, answer})
	if err != nil {
		t.Fatal(err)
	}
	materials := make([]map[string]string, 0)
	for _, material := range projection.Materials() {
		value := map[string]string{"kind": string(material.Kind), "id": material.ID}
		if material.Value != "" {
			value["value"] = material.Value
		}
		materials = append(materials, value)
	}
	record.Materials, err = json.Marshal(materials)
	if err != nil {
		t.Fatal(err)
	}
	citations := make([]map[string]string, 0)
	for _, citation := range projection.Citations() {
		citations = append(citations, map[string]string{"source_id": citation.SourceID, "title": citation.Location, "location": citation.Location, "digest": citation.Digest})
	}
	record.Citations, err = json.Marshal(citations)
	if err != nil {
		t.Fatal(err)
	}
	record.RecoveryReceipt, err = authority.IssueFinalOutputRecoveryReceipt(record)
	if err != nil {
		t.Fatal(err)
	}
	return record, authority, verifier
}

func TestTodo_AGENTP_012_RecoveryRestoresOriginalReceiptAfterFreshAdmission(t *testing.T) {
	original := testRecoveryProjection(t)
	record, _, verifier := fullSignedRecoveryRecord(t, original)
	// A fresh opaque projection has identical gateway-validated output but a
	// new admission receipt. This simulates a changed workload identity/nonce.
	gateway, admission := outputValidationFixtureWithNonce(t, "fresh workload nonce")
	draft, answer := agent026Candidate(t)
	output, err := gateway.ValidateFinalOutput(context.Background(), admission, "people.lookup", FinalOutputCandidate{Complete: true, Draft: draft, Answer: answer}, outputRefs{"person:p1": true}, outputFields{allow: true}, outputClaims{"person-exists": true})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := gateway.IssueFinalOutputPersistence(context.Background(), admission, output, original.Identity())
	if err != nil || fresh.AdmissionDigest() == original.AdmissionDigest() || fresh.Digest() == original.Digest() {
		t.Fatalf("fresh current admission did not differ: %v", err)
	}
	probe := &restorationProbe{projection: fresh}
	restorer, err := NewFinalOutputRestorer(verifier, probe, probe)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RecoverFinalOutputPersistence(context.Background(), record, verifier, restorer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest() != original.Digest() || got.AdmissionDigest() != original.AdmissionDigest() || got.SemanticDigest() != original.SemanticDigest() || got.Identity() != original.Identity() || probe.validations != 1 || probe.authorizations != 1 {
		t.Fatalf("original binding not restored: %+v calls=%d/%d", got.Identity(), probe.validations, probe.authorizations)
	}
	if _, _, err := got.Payload(); err != nil {
		t.Fatalf("restored opaque payload: %v", err)
	}
}

func TestTodo_AGENTP_012_RecoveryRestorationRejectsTamperBeforeRevalidation(t *testing.T) {
	original := testRecoveryProjection(t)
	record, _, verifier := fullSignedRecoveryRecord(t, original)
	record.SealedPayload = []byte(`{"payload":{"Narrative":"forged"},"answer":{}}`)
	probe := &restorationProbe{projection: original}
	if _, err := RestoreFinalOutputPersistence(context.Background(), record, verifier, probe, probe); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) || probe.validations != 0 || probe.authorizations != 0 {
		t.Fatalf("tamper reached current owners: %v calls=%d/%d", err, probe.validations, probe.authorizations)
	}
}

func TestTodo_AGENTP_012_RecoveryRestorationRequiresExactSignedContentAndCurrentAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*FinalOutputRecoveryRecord, *restorationProbe)
	}{
		{"signed mismatched payload", func(r *FinalOutputRecoveryRecord, _ *restorationProbe) { r.SealedPayload = []byte(`{}`) }},
		{"signed mismatched materials", func(r *FinalOutputRecoveryRecord, _ *restorationProbe) { r.Materials = []byte(`[]`) }},
		{"signed mismatched citations", func(r *FinalOutputRecoveryRecord, _ *restorationProbe) { r.Citations = []byte(`[]`) }},
		{"signed mismatched persistence", func(r *FinalOutputRecoveryRecord, _ *restorationProbe) { r.PersistenceDigest = digestContent("wrong") }},
		{"wrong fresh identity", func(_ *FinalOutputRecoveryRecord, p *restorationProbe) { p.projection.identity.InvokerID = "foreign" }},
		{"wrong fresh semantic seal", func(_ *FinalOutputRecoveryRecord, p *restorationProbe) {
			p.projection.semanticDigest = digestContent("wrong")
		}},
		{"broken fresh opaque seal", func(_ *FinalOutputRecoveryRecord, p *restorationProbe) { p.projection.digest = digestContent("wrong") }},
		{"schema-source grant revoked", func(_ *FinalOutputRecoveryRecord, p *restorationProbe) { p.validationErr = context.Canceled }},
		{"disclosure revoked", func(_ *FinalOutputRecoveryRecord, p *restorationProbe) { p.authorityErr = context.Canceled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := testRecoveryProjection(t)
			record, authority, verifier := fullSignedRecoveryRecord(t, original)
			probe := &restorationProbe{projection: original}
			tc.mutate(&record, probe)
			var err error
			record.RecoveryReceipt, err = authority.IssueFinalOutputRecoveryReceipt(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = RestoreFinalOutputPersistence(context.Background(), record, verifier, probe, probe); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
				t.Fatalf("inconsistent signed row recovered: %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_012_RecoveryRestorationDetachesCallbackRecordAndRequiresEveryOwner(t *testing.T) {
	original := testRecoveryProjection(t)
	record, _, verifier := fullSignedRecoveryRecord(t, original)
	probe := &restorationProbe{projection: original, mutate: func(r *FinalOutputRecoveryRecord) {
		r.SealedPayload[0] = '!'
		r.Materials[0] = '!'
		r.Citations[0] = '!'
	}}
	if _, err := RestoreFinalOutputPersistence(context.Background(), record, verifier, probe, probe); err != nil {
		t.Fatalf("callback mutation escaped detached input: %v", err)
	}
	if record.SealedPayload[0] != '{' || record.Materials[0] != '[' || record.Citations[0] != '[' {
		t.Fatal("callback changed caller signed row")
	}
	for _, tc := range []struct {
		verifier  *FinalOutputRecoveryVerifier
		validator FinalOutputRecoveryRevalidator
		authority FinalOutputRecoveryCurrentAuthority
	}{
		{nil, probe, probe}, {verifier, nil, probe}, {verifier, probe, nil},
	} {
		if _, err := NewFinalOutputRestorer(tc.verifier, tc.validator, tc.authority); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
			t.Fatalf("incomplete restorer composed: %v", err)
		}
	}
	var absent *FinalOutputRestorer
	if _, err := absent.RehydrateFinalOutput(context.Background(), record); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
		t.Fatalf("nil restorer=%v", err)
	}
}
