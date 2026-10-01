package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// proposalRevisionReader validates the execution's pinned proposal against
// the tenant-scoped append-only proposal_revision row. The intent envelope is
// a read projection and does not own this authoritative row.
type proposalRevisionReader struct {
	db       dbport.Beginner
	tenantID func(values.TenantId) uuid.UUID
	digester intent.Digester
}

func newProposalRevisionReader(db dbport.Beginner, tenantID func(values.TenantId) uuid.UUID, digester intent.Digester) (*proposalRevisionReader, error) {
	if db == nil || tenantID == nil || digester == nil {
		return nil, errors.New("app: durable proposal revision reader requires database, tenant mapping and digester")
	}
	return &proposalRevisionReader{db: db, tenantID: tenantID, digester: digester}, nil
}

// Validate proves the supplied immutable revision is the exact durable
// revision for tenantKey and intentID before a promotion step invokes a
// capability.
func (r *proposalRevisionReader) Validate(ctx context.Context, tenantKey, intentID string, expected intent.ProposalRevision) error {
	if r == nil || r.db == nil || r.tenantID == nil || r.digester == nil {
		return errors.New("app: durable proposal revision reader is unavailable")
	}
	tenant := values.TenantId(tenantKey)
	if tenantKey == "" || expected.Tenant != tenant || expected.IntentID != intentID || expected.ProposalRevisionID == "" || expected.Revision == 0 || expected.MaterialDigest.Digest == "" {
		return errors.New("app: supplied proposal revision has incomplete or mismatched identity")
	}
	tenantUUID := r.tenantID(tenant)
	if tenantUUID == uuid.Nil {
		return errors.New("app: proposal revision tenant mapping is unavailable")
	}
	intentUUID, err := executionIntentUUID(intentID)
	if err != nil {
		return fmt.Errorf("app: proposal revision intent identity: %w", err)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("app: begin proposal revision read: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantUUID); err != nil {
		return fmt.Errorf("app: scope proposal revision read: %w", err)
	}
	stored, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantUUID, intentUUID, expected.Revision)
	if err != nil {
		return fmt.Errorf("app: load durable proposal revision: %w", err)
	}
	return validateProposalRevisionRow(stored, tenantUUID, expected, r.digester)
}

func validateProposalRevisionRow(stored intentcontrol.Revision, tenantUUID uuid.UUID, expected intent.ProposalRevision, digester intent.Digester) error {
	if stored.TenantID != tenantUUID || stored.IntentID.String() != expected.IntentID || stored.Revision != expected.Revision ||
		stored.ProposalDigest != expected.MaterialDigest.Digest || stored.MaterialDigest != expected.MaterialDigest.Digest ||
		stored.SchemaRef != executionProposalSchemaRef {
		return errors.New("app: durable proposal revision row does not match the pinned identity or digest")
	}
	decoded, err := intentcontrol.DecodeFullProposal(stored.Payload, fullProposalVerifier{digester})
	if err != nil {
		return fmt.Errorf("app: verify durable proposal revision payload: %w", err)
	}
	if decoded.Tenant != expected.Tenant || decoded.IntentID != expected.IntentID || decoded.Revision != expected.Revision ||
		decoded.ProposalRevisionID != expected.ProposalRevisionID ||
		!reflect.DeepEqual(decoded.MaterialDigest, expected.MaterialDigest) ||
		!reflect.DeepEqual(decoded.MaterialPayload(), expected.MaterialPayload()) {
		return errors.New("app: durable proposal revision payload does not match the pinned material")
	}
	return nil
}
