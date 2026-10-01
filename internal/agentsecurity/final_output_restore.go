package agentsecurity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// FinalOutputRecoveryRevalidator validates the signed output again through
// current schema, source and grant owners and returns a newly gateway-sealed
// projection. Its admission may have a new workload fingerprint after restart;
// its output identity and every semantic element must remain exactly equal.
type FinalOutputRecoveryRevalidator interface {
	RevalidateFinalOutputRecovery(context.Context, FinalOutputRecoveryRecord) (FinalOutputPersistence, error)
}

// FinalOutputRecoveryCurrentAuthority rechecks current disclosure authority
// for the restored original identity and signed digests. Restoring validation
// evidence never restores expired or revoked permission to disclose it.
type FinalOutputRecoveryCurrentAuthority interface {
	AuthorizeRecoveredFinalOutput(context.Context, FinalOutputPersistence) error
}

// FinalOutputRestorer composes signature verification, fresh gateway
// validation, and current authority for restart recovery. It never constructs
// typed output from raw persisted JSON.
type FinalOutputRestorer struct {
	verifier    *FinalOutputRecoveryVerifier
	revalidator FinalOutputRecoveryRevalidator
	authority   FinalOutputRecoveryCurrentAuthority
}

func NewFinalOutputRestorer(verifier *FinalOutputRecoveryVerifier, revalidator FinalOutputRecoveryRevalidator, authority FinalOutputRecoveryCurrentAuthority) (*FinalOutputRestorer, error) {
	if verifier == nil || nilValue(revalidator) || nilValue(authority) {
		return nil, ErrFinalOutputRecoveryUnavailable
	}
	return &FinalOutputRestorer{verifier: verifier, revalidator: revalidator, authority: authority}, nil
}

func (r *FinalOutputRestorer) RehydrateFinalOutput(ctx context.Context, record FinalOutputRecoveryRecord) (FinalOutputPersistence, error) {
	if r == nil {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	return RestoreFinalOutputPersistence(ctx, record, r.verifier, r.revalidator, r.authority)
}

// RestoreFinalOutputPersistence permits a fresh admission fingerprint while
// preserving only the authority digests authenticated by the original server
// receipt. Every signed payload, citation and material is compared to freshly
// validated data; then the original persistence digest is recomputed before
// current disclosure authorization. No unsigned JSON can mint a projection.
func RestoreFinalOutputPersistence(ctx context.Context, record FinalOutputRecoveryRecord, verifier *FinalOutputRecoveryVerifier, revalidator FinalOutputRecoveryRevalidator, authority FinalOutputRecoveryCurrentAuthority) (FinalOutputPersistence, error) {
	if ctx == nil || ctx.Err() != nil || verifier == nil || nilValue(revalidator) || nilValue(authority) {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	record = cloneFinalOutputRecoveryRecord(record)
	if !validDigest(record.AdmissionDigest) || !validDigest(record.SemanticDigest) || !validDigest(record.PersistenceDigest) ||
		verifier.VerifyFinalOutputRecovery(ctx, record) != nil {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	fresh, err := revalidator.RevalidateFinalOutputRecovery(ctx, cloneFinalOutputRecoveryRecord(record))
	if err != nil || fresh.Identity() != record.Identity || fresh.SemanticDigest() != record.SemanticDigest {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	payload, answer, err := fresh.Payload()
	if err != nil || !restoredJSONMatches(record, payload, answer, fresh.Materials(), fresh.Citations()) {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	// Payload and materials originate from the fresh opaque projection. Only
	// the original signed admission receipt replaces the new admission value.
	bound, err := persistenceDigest(record.Identity, payload, answer, fresh.materials, fresh.citations, record.SemanticDigest, record.AdmissionDigest)
	if err != nil || bound != record.PersistenceDigest {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	restored := FinalOutputPersistence{identity: record.Identity, payload: payload, answer: answer,
		materials: fresh.Materials(), citations: fresh.Citations(), semanticDigest: record.SemanticDigest,
		admissionDigest: record.AdmissionDigest, digest: bound}
	if ctx.Err() != nil || authority.AuthorizeRecoveredFinalOutput(ctx, restored) != nil {
		return FinalOutputPersistence{}, ErrFinalOutputRecoveryUnavailable
	}
	return restored, nil
}

func cloneFinalOutputRecoveryRecord(record FinalOutputRecoveryRecord) FinalOutputRecoveryRecord {
	record.RecoveryReceipt = bytes.Clone(record.RecoveryReceipt)
	record.Materials = bytes.Clone(record.Materials)
	record.Citations = bytes.Clone(record.Citations)
	record.SealedPayload = bytes.Clone(record.SealedPayload)
	return record
}

func restoredJSONMatches(record FinalOutputRecoveryRecord, payload DraftOutput, answer Answer, materials []OutputMaterial, citations []Citation) bool {
	sealedPayload, err := json.Marshal(struct {
		Payload DraftOutput `json:"payload"`
		Answer  Answer      `json:"answer"`
	}{payload, answer})
	if err != nil || !equalRecoveryJSON(record.SealedPayload, sealedPayload) {
		return false
	}
	// These field names are the durable output-row schema, including the
	// citation title derived from its validated location by the store owner.
	type storedMaterial struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Value string `json:"value,omitempty"`
	}
	storedMaterials := make([]storedMaterial, 0, len(materials))
	for _, material := range materials {
		storedMaterials = append(storedMaterials, storedMaterial{string(material.Kind), material.ID, material.Value})
	}
	encoded, err := json.Marshal(storedMaterials)
	if err != nil || !equalRecoveryJSON(record.Materials, encoded) {
		return false
	}
	type storedCitation struct {
		SourceID string `json:"source_id"`
		Title    string `json:"title"`
		Location string `json:"location"`
		Digest   string `json:"digest"`
	}
	storedCitations := make([]storedCitation, 0, len(citations))
	for _, citation := range citations {
		storedCitations = append(storedCitations, storedCitation{citation.SourceID, citation.Location, citation.Location, citation.Digest})
	}
	encoded, err = json.Marshal(storedCitations)
	return err == nil && equalRecoveryJSON(record.Citations, encoded)
}

func equalRecoveryJSON(left, right []byte) bool {
	canonical := func(raw []byte) ([]byte, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, ErrFinalOutputRecoveryUnavailable
		}
		return json.Marshal(value)
	}
	a, err := canonical(left)
	if err != nil {
		return false
	}
	b, err := canonical(right)
	return err == nil && bytes.Equal(a, b)
}
