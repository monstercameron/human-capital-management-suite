package critical

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

// ProjectionName is the checkpoint this package advances
// (internal/data/projection.Checkpoint.ProjectionName). It is distinct from
// any projection name a caller's own internal/data/outbox.Commit call
// advances on the same stream - see the package doc's "why this is a safe
// second checkpoint on the same event."
const ProjectionName = "critical.intent_and_proposal"

// ApplyRequest is one ledger event to project, addressed the same way
// internal/data/ledger.AppendReceipt and internal/data/ledger.EventRecord
// address an event: by stream and sequence, never by caller-supplied
// ordering.
type ApplyRequest struct {
	Tenant    uuid.UUID
	StreamKey string
	// Sequence is the event's sequence on StreamKey
	// (internal/data/ledger.AppendReceipt.Sequence or
	// internal/data/ledger.EventRecord.Sequence).
	Sequence int64
	// Digest is the event's own canonical digest, recorded on the checkpoint
	// exactly as internal/data/projection.Apply already does for any
	// projection.
	Digest string
	// SchemaRef and Payload are the event's typed payload, passed to Mapper
	// unchanged.
	SchemaRef string
	Payload   []byte
}

// ApplyResult reports what Apply did.
type ApplyResult struct {
	Checkpoint projection.Checkpoint
	// Applied is false when Sequence was already applied - a replay that
	// changed nothing (DATA-006: "a replayed event is a no-op").
	Applied bool
	// Target names which read-model table changed. It is only meaningful
	// when Applied is true.
	Target Target
}

// ErrUnsupportedSchema reports an event whose schema neither
// Mapper.MapIntentInstance nor Mapper.MapProposalRevision claimed. The
// checkpoint has already advanced by the time this is returned by [Apply]'s
// caller sees it - see the package doc for why that is the intended
// behavior for an unrecognized event kind sharing a stream with recognized
// ones - so a caller that treats this as fatal must roll back the whole
// transaction, exactly as any other error from [Apply] requires.
type ErrUnsupportedSchema struct {
	SchemaRef string
}

func (ErrUnsupportedSchema) Code() string { return "CRITICAL_PROJECTION_UNSUPPORTED_SCHEMA" }

func (e ErrUnsupportedSchema) Error() string {
	return fmt.Sprintf("%s: schema %q maps to neither intent_instance nor proposal_revision", e.Code(), e.SchemaRef)
}

// ErrStaleInstanceVersion reports an intent_instance write whose
// InstanceVersion did not strictly exceed what is already stored. In the
// intended calling path through [Apply] this can never happen - the
// checkpoint's per-stream sequence guard already serializes and orders every
// mutation to one intent's row - so seeing it means a caller wrote to
// intent_instance through a path other than [Apply].
type ErrStaleInstanceVersion struct {
	IntentID        uuid.UUID
	InstanceVersion int64
}

func (ErrStaleInstanceVersion) Code() string { return "CRITICAL_PROJECTION_STALE_INSTANCE_VERSION" }

func (e ErrStaleInstanceVersion) Error() string {
	return fmt.Sprintf("%s: intent %s instance_version %d did not advance the stored row",
		e.Code(), e.IntentID, e.InstanceVersion)
}

// ErrProposalRevisionConflict reports a second, different write attempted
// against an existing (tenant, intent, revision) - proposal_revision is
// append-only (migrations/00004, proposal_revision_append_only), so a
// revision's identity can never be reused for different content.
type ErrProposalRevisionConflict struct {
	IntentID uuid.UUID
	Revision int64
}

func (ErrProposalRevisionConflict) Code() string {
	return "CRITICAL_PROJECTION_PROPOSAL_REVISION_CONFLICT"
}

func (e ErrProposalRevisionConflict) Error() string {
	return fmt.Sprintf("%s: intent %s revision %d is already recorded with different content",
		e.Code(), e.IntentID, e.Revision)
}

// Apply advances this package's checkpoint by exactly one event and, only
// when that advance is genuinely new (not a replay, not a gap), decodes the
// event through mapper and upserts the read model it names. See the package
// doc for the full DATA-006 contract.
func Apply(ctx context.Context, tx dbport.Tx, mapper Mapper, req ApplyRequest) (ApplyResult, error) {
	if err := projection.EnsureProjection(ctx, tx, req.Tenant, ProjectionName, req.StreamKey); err != nil {
		return ApplyResult{}, fmt.Errorf("critical: ensure checkpoint: %w", err)
	}

	result, err := projection.Apply(ctx, tx, projection.ApplyRequest{
		Tenant:         req.Tenant,
		ProjectionName: ProjectionName,
		StreamKey:      req.StreamKey,
		Sequence:       req.Sequence,
		Digest:         req.Digest,
	})
	if err != nil {
		return ApplyResult{}, err
	}
	if !result.Applied {
		return ApplyResult{Checkpoint: result.Checkpoint, Applied: false}, nil
	}

	if row, ok, err := mapper.MapIntentInstance(req.SchemaRef, req.Payload); err != nil {
		return ApplyResult{}, err
	} else if ok {
		row.Tenant = req.Tenant
		if err := upsertIntentInstance(ctx, tx, row); err != nil {
			return ApplyResult{}, err
		}
		return ApplyResult{Checkpoint: result.Checkpoint, Applied: true, Target: TargetIntentInstance}, nil
	}

	if row, ok, err := mapper.MapProposalRevision(req.SchemaRef, req.Payload); err != nil {
		return ApplyResult{}, err
	} else if ok {
		row.Tenant = req.Tenant
		if err := insertProposalRevision(ctx, tx, row); err != nil {
			return ApplyResult{}, err
		}
		return ApplyResult{Checkpoint: result.Checkpoint, Applied: true, Target: TargetProposalRevision}, nil
	}

	return ApplyResult{Checkpoint: result.Checkpoint, Applied: true, Target: TargetNone}, ErrUnsupportedSchema{SchemaRef: req.SchemaRef}
}

// upsertIntentInstance inserts a new intent or replaces a stored one, but
// only when row.InstanceVersion strictly exceeds what is stored - the
// table-level half of the DATA-006 "stale version overwrites newer row" RED
// guard described in the package doc.
func upsertIntentInstance(ctx context.Context, tx dbport.Tx, row IntentInstanceRow) error {
	affected, err := tx.Exec(ctx, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version,
			request_digest, request_digest_algorithm, idempotency_key,
			request_state, execution_state, business_state, consistency_state, obligation_state,
			instance_version, created_at, recorded_at, last_transition_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (tenant_id, intent_id) DO UPDATE SET
			definition_ref           = EXCLUDED.definition_ref,
			definition_version       = EXCLUDED.definition_version,
			request_digest           = EXCLUDED.request_digest,
			request_digest_algorithm = EXCLUDED.request_digest_algorithm,
			idempotency_key          = EXCLUDED.idempotency_key,
			request_state            = EXCLUDED.request_state,
			execution_state          = EXCLUDED.execution_state,
			business_state           = EXCLUDED.business_state,
			consistency_state        = EXCLUDED.consistency_state,
			obligation_state         = EXCLUDED.obligation_state,
			instance_version         = EXCLUDED.instance_version,
			recorded_at              = EXCLUDED.recorded_at,
			last_transition_at       = EXCLUDED.last_transition_at
		WHERE intent_instance.instance_version < EXCLUDED.instance_version`,
		row.Tenant, row.IntentID, row.DefinitionRef, row.DefinitionVersion,
		row.RequestDigest, row.RequestDigestAlgorithm, row.IdempotencyKey,
		row.RequestState, row.ExecutionState, row.BusinessState, row.ConsistencyState, row.ObligationState,
		row.InstanceVersion, row.CreatedAt, row.RecordedAt, row.LastTransitionAt)
	if err != nil {
		return fmt.Errorf("critical: project intent_instance %s: %w", row.IntentID, err)
	}
	if affected != 1 {
		return ErrStaleInstanceVersion{IntentID: row.IntentID, InstanceVersion: row.InstanceVersion}
	}
	return nil
}

// Writer is the minimal database capability the shared proposal_revision
// writer needs. Both dbport.Tx and intentcontrol.Executor satisfy it, which
// is what makes RevisionStore.Materialize a thin call into this package's
// single CAS-guarded insert rather than a second writer with its own SQL.
type Writer interface {
	Exec(ctx context.Context, sql string, args ...any) (rowsAffected int64, err error)
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}

// InsertProposalRevision inserts one immutable proposal_revision row and
// reports whether this call created it. A second attempt at the same
// (tenant, intent, revision) is a no-op only when it carries the identical
// proposal digest; any difference is [ErrProposalRevisionConflict] rather
// than a silently ignored write. An empty DigestAlgorithm defaults to
// "sha256" to match the column default. This is the single writer both
// [Apply] and intentcontrol.RevisionStore.Materialize share.
func InsertProposalRevision(ctx context.Context, ex Writer, row ProposalRevisionRow) (bool, error) {
	if row.DigestAlgorithm == "" {
		row.DigestAlgorithm = "sha256"
	}
	if !row.ProducedAt.IsZero() {
		row.ProducedAt = row.ProducedAt.UTC()
	}
	var artifactRef *string
	if row.ArtifactRef != "" {
		artifactRef = &row.ArtifactRef
	}
	affected, err := ex.Exec(ctx, `
		INSERT INTO proposal_revision (
			tenant_id, intent_id, revision, proposal_digest, material_digest,
			digest_algorithm, schema_ref, payload, artifact_ref, produced_by, produced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (tenant_id, intent_id, revision) DO NOTHING`,
		row.Tenant, row.IntentID, row.Revision, row.ProposalDigest, row.MaterialDigest,
		row.DigestAlgorithm, row.SchemaRef, row.Payload, artifactRef, row.ProducedBy, row.ProducedAt)
	if err != nil {
		return false, fmt.Errorf("critical: project proposal_revision %s/%d: %w", row.IntentID, row.Revision, err)
	}
	if affected == 1 {
		return true, nil
	}

	var storedDigest string
	err = ex.QueryRow(ctx, `
		SELECT proposal_digest FROM proposal_revision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3`,
		row.Tenant, row.IntentID, row.Revision).Scan(&storedDigest)
	if err != nil {
		return false, fmt.Errorf("critical: read existing proposal_revision %s/%d: %w", row.IntentID, row.Revision, err)
	}
	if storedDigest != row.ProposalDigest {
		return false, ErrProposalRevisionConflict{IntentID: row.IntentID, Revision: row.Revision}
	}
	return false, nil
}

// insertProposalRevision inserts one immutable proposal_revision row. A
// second attempt at the same (tenant, intent, revision) is a no-op only when
// it carries the identical proposal digest; any difference is
// [ErrProposalRevisionConflict] rather than a silently ignored write.
func insertProposalRevision(ctx context.Context, tx dbport.Tx, row ProposalRevisionRow) error {
	_, err := InsertProposalRevision(ctx, tx, row)
	return err
}
