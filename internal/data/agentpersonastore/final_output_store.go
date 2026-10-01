package agentpersonastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// FinalOutputRecord is the durable identity and sealed material metadata for
// one validated persona result. SealedPayload is retained for the server-owned
// delivery adapter and must never be treated as provider validation.
type FinalOutputRecord struct {
	TenantID          values.TenantId
	InvocationID      string
	AdmissionID       string
	RunID             string
	OutputID          string
	InvokerID         string
	ConversationID    string
	ThreadID          string
	ParentPostID      string
	PersonaID         string
	PersonaVersion    string
	InstallationID    string
	AdmissionDigest   string
	Agent026Digest    string
	PersistenceDigest string
	RecoveryReceipt   []byte
	Materials         json.RawMessage
	Citations         json.RawMessage
	SealedPayload     json.RawMessage
	CreatedAt         time.Time
}

func validateFinalOutputRecord(r FinalOutputRecord) error {
	if strings.TrimSpace(string(r.TenantID)) == "" || strings.TrimSpace(r.InvocationID) == "" || strings.TrimSpace(r.AdmissionID) == "" || strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.OutputID) == "" ||
		strings.TrimSpace(r.InvokerID) == "" || strings.TrimSpace(r.ConversationID) == "" || strings.TrimSpace(r.ThreadID) == "" ||
		strings.TrimSpace(r.ParentPostID) == "" || strings.TrimSpace(r.PersonaID) == "" || strings.TrimSpace(r.PersonaVersion) == "" ||
		strings.TrimSpace(r.InstallationID) == "" {
		return fmt.Errorf("%w: final output identity is incomplete", ErrInvalid)
	}
	if !sha256DigestPattern.MatchString(r.AdmissionDigest) || !sha256DigestPattern.MatchString(r.Agent026Digest) {
		return fmt.Errorf("%w: admission and AGENT-026 digests must be sha256 digests", ErrInvalid)
	}
	if !sha256DigestPattern.MatchString(r.PersistenceDigest) || len(r.RecoveryReceipt) == 0 {
		return fmt.Errorf("%w: persistence digest and server recovery receipt are required", ErrInvalid)
	}
	for name, value := range map[string]struct {
		raw  json.RawMessage
		want byte
	}{
		"materials": {r.Materials, '['}, "citations": {r.Citations, '['}, "sealed payload": {r.SealedPayload, '{'},
	} {
		if len(value.raw) == 0 || value.raw[0] != value.want || !json.Valid(value.raw) {
			return fmt.Errorf("%w: %s must be valid server-owned JSON", ErrInvalid, name)
		}
	}
	if err := validateFinalOutputMetadata(r.Materials, "materials", "id"); err != nil {
		return err
	}
	if err := validateFinalOutputMetadata(r.Citations, "citations", "source_id"); err != nil {
		return err
	}
	return nil
}

func validateFinalOutputMetadata(raw json.RawMessage, name, identityKey string) error {
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("%w: %s metadata is not an array of objects", ErrInvalid, name)
	}
	for _, entry := range entries {
		identity, ok := entry[identityKey].(string)
		if !ok || strings.TrimSpace(identity) == "" {
			return fmt.Errorf("%w: %s metadata requires %s", ErrInvalid, name, identityKey)
		}
		if name == "citations" {
			title, ok := entry["title"].(string)
			if !ok || strings.TrimSpace(title) == "" {
				return fmt.Errorf("%w: citation metadata requires title", ErrInvalid)
			}
		}
	}
	return nil
}

// PutFinalOutput persists one immutable, security-validated result and issues
// its recovery receipt using the server-owned authority.
func (s *TenantStore) PutFinalOutput(ctx context.Context, projection agentsecurity.FinalOutputPersistence, authority *agentsecurity.FinalOutputRecoveryAuthority) error {
	if s == nil || ctx == nil {
		return fmt.Errorf("%w: context and tenant store are required", ErrInvalid)
	}
	if authority == nil {
		return agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	identity := projection.Identity()
	if values.TenantId(identity.TenantID) != s.tenant {
		return fmt.Errorf("%w: final output tenant mismatch", ErrInvalid)
	}
	payload, answer, err := projection.Payload()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	sealedPayload, err := json.Marshal(struct {
		Payload any `json:"payload"`
		Answer  any `json:"answer"`
	}{Payload: payload, Answer: answer})
	if err != nil {
		return fmt.Errorf("%w: final output payload: %v", ErrInvalid, err)
	}
	materials, err := json.Marshal(persistedMaterials(projection.Materials()))
	if err != nil {
		return fmt.Errorf("%w: final output materials: %v", ErrInvalid, err)
	}
	citations, err := json.Marshal(persistedCitations(projection.Citations()))
	if err != nil {
		return fmt.Errorf("%w: final output citations: %v", ErrInvalid, err)
	}
	r := FinalOutputRecord{
		TenantID: values.TenantId(identity.TenantID), InvocationID: identity.InvocationID, AdmissionID: identity.AdmissionID, RunID: identity.RunID, OutputID: identity.OutputID,
		InvokerID: identity.InvokerID, ConversationID: identity.ConversationID, ThreadID: identity.ThreadID,
		ParentPostID: identity.PostID, PersonaID: identity.PersonaID, PersonaVersion: identity.PersonaVersion,
		InstallationID: identity.InstallationID, AdmissionDigest: projection.AdmissionDigest(), Agent026Digest: projection.SemanticDigest(),
		PersistenceDigest: projection.Digest(),
		Materials:         materials, Citations: citations, SealedPayload: sealedPayload,
	}
	recoveryRecord := recoveryRecordFromFinalOutput(r)
	r.RecoveryReceipt, err = authority.IssueFinalOutputRecoveryReceipt(recoveryRecord)
	if err != nil {
		return agentsecurity.ErrFinalOutputRecoveryUnavailable
	}
	if err := validateFinalOutputRecord(r); err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `INSERT INTO persona_final_outputs
		(tenant_id,invocation_id,admission_id,run_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,
		 persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb,$18::jsonb,$19::jsonb,CURRENT_TIMESTAMP) ON CONFLICT DO NOTHING`,
		s.tenantID, r.InvocationID, r.AdmissionID, r.RunID, r.OutputID, r.InvokerID, r.ConversationID, r.ThreadID, r.ParentPostID,
		r.PersonaID, r.PersonaVersion, r.InstallationID, r.AdmissionDigest, r.Agent026Digest, r.PersistenceDigest, r.RecoveryReceipt,
		[]byte(r.Materials), []byte(r.Citations), []byte(r.SealedPayload))
	if err != nil {
		return fmt.Errorf("agentpersonastore: insert final output: %w", err)
	}
	if result != 1 {
		return ErrConflict
	}
	return commit(ctx, tx)
}

func recoveryRecordFromFinalOutput(r FinalOutputRecord) agentsecurity.FinalOutputRecoveryRecord {
	return agentsecurity.FinalOutputRecoveryRecord{
		Identity:        finalOutputIdentity(r),
		AdmissionDigest: r.AdmissionDigest, SemanticDigest: r.Agent026Digest, PersistenceDigest: r.PersistenceDigest,
		RecoveryReceipt: append([]byte(nil), r.RecoveryReceipt...), Materials: append(json.RawMessage(nil), r.Materials...), Citations: append(json.RawMessage(nil), r.Citations...), SealedPayload: append(json.RawMessage(nil), r.SealedPayload...),
	}
}

type persistedMaterial struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Value string `json:"value,omitempty"`
}

func persistedMaterials(materials []agentsecurity.OutputMaterial) []persistedMaterial {
	out := make([]persistedMaterial, 0, len(materials))
	for _, material := range materials {
		out = append(out, persistedMaterial{Kind: string(material.Kind), ID: material.ID, Value: material.Value})
	}
	return out
}

type persistedCitation struct {
	SourceID string `json:"source_id"`
	Title    string `json:"title"`
	Location string `json:"location"`
	Digest   string `json:"digest"`
}

func persistedCitations(citations []agentsecurity.Citation) []persistedCitation {
	out := make([]persistedCitation, 0, len(citations))
	for _, citation := range citations {
		// FinalOutputPersistence exposes validated source locations, but no
		// independently authored title. Persist the validated location as the
		// citation title so display metadata cannot be supplied by the caller.
		out = append(out, persistedCitation{SourceID: citation.SourceID, Title: citation.Location, Location: citation.Location, Digest: citation.Digest})
	}
	return out
}

// GetFinalOutput returns a tenant-scoped immutable result by output identity.
func (s *TenantStore) GetFinalOutput(ctx context.Context, outputID string) (FinalOutputRecord, error) {
	if s == nil || ctx == nil || strings.TrimSpace(outputID) == "" {
		return FinalOutputRecord{}, fmt.Errorf("%w: context, tenant store, and output are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return FinalOutputRecord{}, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, finalOutputSelect+` WHERE tenant_id=$1 AND output_id=$2`, s.tenantID, strings.TrimSpace(outputID))
	r, err := scanFinalOutput(row, s.tenant)
	if err != nil {
		return FinalOutputRecord{}, err
	}
	if err := commit(ctx, tx); err != nil {
		return FinalOutputRecord{}, err
	}
	return r, nil
}

// RecoverFinalOutput rehydrates a durable result only after the server-owned
// receipt verifier accepts every bound field.
func (s *TenantStore) RecoverFinalOutput(ctx context.Context, outputID string, verifier *agentsecurity.FinalOutputRecoveryVerifier, rehydrator agentsecurity.FinalOutputRecoveryRehydrator) (agentsecurity.FinalOutputPersistence, error) {
	r, err := s.GetFinalOutput(ctx, outputID)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	projection, err := agentsecurity.RecoverFinalOutputPersistence(ctx, recoveryRecordFromFinalOutput(r), verifier, rehydrator)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	return projection, nil
}

// RecoverFinalOutputForIdentity recovers only a result with the exact
// admission, run, invocation and delivery identity supplied by the trusted
// caller. Output IDs alone never authorize private result retrieval.
func (s *TenantStore) RecoverFinalOutputForIdentity(ctx context.Context, expected agentsecurity.FinalOutputIdentity, verifier *agentsecurity.FinalOutputRecoveryVerifier, rehydrator agentsecurity.FinalOutputRecoveryRehydrator) (agentsecurity.FinalOutputPersistence, error) {
	if s == nil || ctx == nil || values.TenantId(expected.TenantID) != s.tenant || strings.TrimSpace(expected.OutputID) == "" {
		return agentsecurity.FinalOutputPersistence{}, fmt.Errorf("%w: complete expected identity in tenant scope is required", ErrInvalid)
	}
	record, err := s.GetFinalOutput(ctx, expected.OutputID)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	if finalOutputIdentity(record) != expected {
		return agentsecurity.FinalOutputPersistence{}, ErrNotFound
	}
	projection, err := agentsecurity.RecoverFinalOutputPersistence(ctx, recoveryRecordFromFinalOutput(record), verifier, rehydrator)
	if err != nil {
		return agentsecurity.FinalOutputPersistence{}, err
	}
	return projection, nil
}

func finalOutputIdentity(r FinalOutputRecord) agentsecurity.FinalOutputIdentity {
	return agentsecurity.FinalOutputIdentity{
		TenantID: string(r.TenantID), OutputID: r.OutputID, InvocationID: r.InvocationID,
		AdmissionID: r.AdmissionID, RunID: r.RunID, InvokerID: r.InvokerID,
		ConversationID: r.ConversationID, ThreadID: r.ThreadID, PostID: r.ParentPostID,
		PersonaID: r.PersonaID, PersonaVersion: r.PersonaVersion, InstallationID: r.InstallationID,
	}
}

// GetFinalOutputByInvocation returns the one idempotent output for an
// invocation, refusing ambiguity rather than selecting an arbitrary result.
func (s *TenantStore) GetFinalOutputByInvocation(ctx context.Context, invocationID string) (FinalOutputRecord, error) {
	if s == nil || ctx == nil || strings.TrimSpace(invocationID) == "" {
		return FinalOutputRecord{}, fmt.Errorf("%w: context, tenant store, and invocation are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return FinalOutputRecord{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, finalOutputSelect+` WHERE tenant_id=$1 AND invocation_id=$2 ORDER BY output_id COLLATE "C"`, s.tenantID, strings.TrimSpace(invocationID))
	if err != nil {
		return FinalOutputRecord{}, fmt.Errorf("agentpersonastore: query final output by invocation: %w", err)
	}
	defer rows.Close()
	var out FinalOutputRecord
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return FinalOutputRecord{}, fmt.Errorf("%w: multiple outputs for invocation", ErrConflict)
		}
		var scanErr error
		out, scanErr = scanFinalOutputRow(rows, s.tenant)
		if scanErr != nil {
			return FinalOutputRecord{}, scanErr
		}
	}
	if err := rows.Err(); err != nil {
		return FinalOutputRecord{}, fmt.Errorf("agentpersonastore: read final output: %w", err)
	}
	if count == 0 {
		return FinalOutputRecord{}, ErrNotFound
	}
	if err := commit(ctx, tx); err != nil {
		return FinalOutputRecord{}, err
	}
	return out, nil
}

const finalOutputSelect = `SELECT invocation_id,admission_id,run_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,
	persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at
	FROM persona_final_outputs`

type finalOutputScanner interface{ Scan(...any) error }

func scanFinalOutput(row finalOutputScanner, tenant values.TenantId) (FinalOutputRecord, error) {
	var r FinalOutputRecord
	if err := scanFinalOutputFields(row, &r); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return FinalOutputRecord{}, ErrNotFound
		}
		return FinalOutputRecord{}, fmt.Errorf("agentpersonastore: scan final output: %w", err)
	}
	r.TenantID, r.CreatedAt = tenant, r.CreatedAt.UTC()
	if err := validateFinalOutputRecord(r); err != nil {
		return FinalOutputRecord{}, err
	}
	return r, nil
}

func scanFinalOutputRow(row finalOutputScanner, tenant values.TenantId) (FinalOutputRecord, error) {
	return scanFinalOutput(row, tenant)
}

func scanFinalOutputFields(row finalOutputScanner, r *FinalOutputRecord) error {
	return row.Scan(&r.InvocationID, &r.AdmissionID, &r.RunID, &r.OutputID, &r.InvokerID, &r.ConversationID, &r.ThreadID, &r.ParentPostID,
		&r.PersonaID, &r.PersonaVersion, &r.InstallationID, &r.AdmissionDigest, &r.Agent026Digest,
		&r.PersistenceDigest, &r.RecoveryReceipt, &r.Materials, &r.Citations, &r.SealedPayload, &r.CreatedAt)
}
