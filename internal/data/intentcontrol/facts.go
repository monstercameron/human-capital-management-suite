package intentcontrol

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/projection/critical"
)

// The reads and the one parent-row write WF-RUN-027's durable approval facts
// need.
//
// # Why a proposal_revision writer lives in this package
//
// intent_decision, intent_result, intent_certificate and the transaction plan
// all foreign-key to proposal_revision (tenant_id, intent_id, revision), a
// migration 00004 table whose only other writer is the ledger projection in
// internal/data/projection/critical. A caller that wants to record the
// decision that admits a proposal revision to execution therefore cannot do so
// until that parent row exists, and the P1A intent write path mints its
// proposal revision as an in-memory simulation artifact without ever storing
// one (see internal/intent/app's ExecuteIntent). [RevisionStore.Materialize]
// is the narrow, idempotent insert that closes that gap -- it is the "the
// intent write path is what should populate ... the proposal set tables"
// wiring this package's own doc.go names, expressed as a store method rather
// than as raw SQL copied into an application package.
//
// It is deliberately insert-only and ON CONFLICT DO NOTHING: proposal_revision
// is append-only in the schema, so a second call for the same revision must
// leave the first row exactly as it is rather than fail the caller's whole
// transaction on a primary-key violation.

// Revision is one proposal_revision row: the immutable revision every
// intent-control child row hangs off.
type Revision struct {
	TenantID uuid.UUID
	IntentID uuid.UUID
	Revision uint64

	ProposalDigest string
	MaterialDigest string

	// SchemaRef names the schema Payload validates against. Payload is the
	// canonical bytes of the revision; the schema forbids a row carrying
	// neither a payload nor an artifact reference, and this store always
	// writes the payload form.
	SchemaRef string
	Payload   []byte

	ProducedBy string
	ProducedAt time.Time
}

// Validate rejects a revision row that could not be stored.
func (r Revision) Validate() error {
	if err := requireID("tenant_id", r.TenantID); err != nil {
		return err
	}
	if err := requireID("intent_id", r.IntentID); err != nil {
		return err
	}
	if r.Revision == 0 {
		return invalid("revision", "a proposal revision starts at 1")
	}
	if err := requireDigest("proposal_digest", r.ProposalDigest); err != nil {
		return err
	}
	if err := requireDigest("material_digest", r.MaterialDigest); err != nil {
		return err
	}
	if err := requireText("schema_ref", r.SchemaRef); err != nil {
		return err
	}
	if err := requireText("produced_by", r.ProducedBy); err != nil {
		return err
	}
	if len(r.Payload) == 0 {
		return invalid("payload", "canonical payload bytes are absent")
	}
	return requireInstant("produced_at", r.ProducedAt)
}

// RevisionStore writes and reads proposal_revision, the parent row every table
// migration 00024 adds keys to.
type RevisionStore struct{}

// Load returns the immutable parent row and its explicitly persisted payload.
// Callers must decode the payload according to SchemaRef; an opaque or legacy
// payload is never treated as a reconstructed ProposalRevision.
func (s RevisionStore) Load(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64) (Revision, error) {
	if err := requireID("tenant_id", tenantID); err != nil {
		return Revision{}, err
	}
	if err := requireID("intent_id", intentID); err != nil {
		return Revision{}, err
	}
	if revision == 0 {
		return Revision{}, invalid("revision", "a proposal revision starts at 1")
	}
	if revision > uint64(1<<63-1) {
		return Revision{}, invalid("revision", "value exceeds PostgreSQL bigint range")
	}
	var out Revision
	var n int64
	err := ex.QueryRow(ctx, `
		SELECT tenant_id, intent_id, revision, proposal_digest, material_digest,
			schema_ref, payload, produced_by, produced_at
		FROM proposal_revision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3`, tenantID, intentID, int64(revision)).Scan(
		&out.TenantID, &out.IntentID, &n, &out.ProposalDigest, &out.MaterialDigest,
		&out.SchemaRef, &out.Payload, &out.ProducedBy, &out.ProducedAt)
	if err != nil {
		if isNoRows(err) {
			return Revision{}, fmt.Errorf("%w: proposal_revision %s/%d", ErrNotFound, intentID, revision)
		}
		return Revision{}, fmt.Errorf("intentcontrol: load proposal revision %s/%d: %w", intentID, revision, err)
	}
	out.Revision = uint64(n)
	out.ProducedAt = out.ProducedAt.UTC()
	return out, nil
}

// Materialize inserts the revision row unless it is already stored, and
// reports whether this call is the one that created it.
//
// It is a thin call into critical.InsertProposalRevision, the single
// CAS-guarded proposal_revision writer the ledger-commit projection shares:
// an identical second write is an idempotent no-op, while a second write
// with a different proposal digest is a conflict rather than a silently
// ignored overwrite. proposal_revision carries migration 00004's append-only
// trigger, so those are the only outcomes an insert can have, and a caller
// re-executing the same simulated revision can tell the two apart without
// inspecting a driver error.
func (s RevisionStore) Materialize(ctx context.Context, ex Executor, in Revision) (bool, error) {
	if err := in.Validate(); err != nil {
		return false, err
	}
	if in.Revision > uint64(1<<63-1) {
		return false, invalid("revision", "value exceeds PostgreSQL bigint range")
	}
	created, err := critical.InsertProposalRevision(ctx, ex, critical.ProposalRevisionRow{
		Tenant:          in.TenantID,
		IntentID:        in.IntentID,
		Revision:        int64(in.Revision),
		ProposalDigest:  in.ProposalDigest,
		MaterialDigest:  in.MaterialDigest,
		DigestAlgorithm: "sha256",
		SchemaRef:       in.SchemaRef,
		Payload:         in.Payload,
		ProducedBy:      in.ProducedBy,
		ProducedAt:      in.ProducedAt.UTC(),
	})
	if err != nil {
		return false, fmt.Errorf("intentcontrol: materialize proposal revision %s/%d: %w",
			in.IntentID, in.Revision, err)
	}
	return created, nil
}

// MaterialDigestOf reads back the material digest one stored revision carries,
// so a caller can prove the revision it is about to bind a decision to is the
// revision that is actually stored.
func (s RevisionStore) MaterialDigestOf(
	ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64,
) (string, error) {
	var stored string
	err := ex.QueryRow(ctx, `
		SELECT material_digest FROM proposal_revision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3`,
		tenantID, intentID, int64(revision)).Scan(&stored)
	if err != nil {
		if isNoRows(err) {
			return "", fmt.Errorf("%w: proposal_revision %s/%d", ErrNotFound, intentID, revision)
		}
		return "", fmt.Errorf("intentcontrol: read proposal revision %s/%d: %w", intentID, revision, err)
	}
	return stored, nil
}

// ForRevision returns every decision recorded against one exact proposal
// revision, ordered by decision id so two reads of the same stored rows
// produce the same slice.
//
// It is the read half of WF-RUN-027: the runtime resolves "is this revision
// approved" from these rows, never from a boolean its caller asserted. The
// rows are returned whole -- outcome, bound proposal digest and all -- because
// the runtime's own binding check compares the stored proposal_digest against
// the revision it is being asked to start, and a store that answered only
// "approved: yes" could not be checked that way.
func (s DecisionStore) ForRevision(
	ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, revision uint64,
) ([]Decision, error) {
	rows, err := ex.Query(ctx, `
		SELECT decision_id, requirement_id, decision_kind, decision_outcome,
			proposal_digest, control_digest, materiality_class,
			decided_by, authority_ref, decision_reason, decided_at, recorded_at
		FROM intent_decision
		WHERE tenant_id = $1 AND intent_id = $2 AND revision = $3
		ORDER BY decision_id`, tenantID, intentID, int64(revision))
	if err != nil {
		return nil, fmt.Errorf("intentcontrol: load decisions %s/%d: %w", intentID, revision, err)
	}
	defer rows.Close()

	var out []Decision
	for rows.Next() {
		d := Decision{TenantID: tenantID, IntentID: intentID, Revision: revision}
		if err := rows.Scan(&d.DecisionID, &d.RequirementID, &d.Kind, &d.Outcome,
			&d.ProposalDigest, &d.ControlDigest, &d.MaterialityClass,
			&d.DecidedBy, &d.AuthorityRef, &d.Reason, &d.DecidedAt, &d.RecordedAt); err != nil {
			return nil, fmt.Errorf("intentcontrol: scan decision of %s/%d: %w", intentID, revision, err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intentcontrol: load decisions %s/%d: %w", intentID, revision, err)
	}
	return out, nil
}

// SupersededBy reports the intent that superseded intentID, if any.
//
// # The edge direction, stated once
//
// A SUPERSEDES edge is written parent = the superseding intent, child = the
// superseded one. That is the direction the schema's own
// intent_relationship_single_parent constraint makes meaningful: it allows one
// parent per (relationship_type, child), so under this reading a superseded
// intent has at most one superseder -- exactly the invariant supersession
// needs -- while a superseder may supersede several intents in ordinal order.
// Reading the edge the other way round would enforce the far less useful "each
// superseder supersedes at most one intent".
//
// It is also the direction [RelationshipStore.Link]'s own cycle walk assumes:
// that walk climbs from child to parent, so a SUPERSEDES chain walked upward
// is "and what superseded that", which terminates at the current intent.
func (s RelationshipStore) SupersededBy(
	ctx context.Context, ex Executor, tenantID, intentID uuid.UUID,
) (uuid.UUID, bool, error) {
	var parent uuid.UUID
	err := ex.QueryRow(ctx, `
		SELECT parent_intent_id FROM intent_relationship
		WHERE tenant_id = $1 AND relationship_type = $2 AND child_intent_id = $3`,
		tenantID, RelationSupersedes, intentID).Scan(&parent)
	if err != nil {
		if isNoRows(err) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("intentcontrol: read supersession of %s: %w", intentID, err)
	}
	return parent, true, nil
}
