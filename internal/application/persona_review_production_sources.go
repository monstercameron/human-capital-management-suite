package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaAdminReviewSources contains the independently backed authorization
// and evidence-writing dependencies required by PersonaAdminCommandSurface.
type PersonaAdminReviewSources struct {
	Authority PersonaReviewAuthority
	Writer    PersonaReviewEvidenceWriter
}

// NewPersonaAdminReviewSources builds review dependencies from the normal
// tenant-serving agent pool and the separately credentialed review-authority
// pool. The writer pool's concrete type ensures callers cannot accidentally
// pass the tenant-serving pool as the evidence writer.
func NewPersonaAdminReviewSources(agentDB *agentstore.Store, reviewDB *agentstore.PersonaReviewAuthorityStore, tenantUUID func(values.TenantId) uuid.UUID) (PersonaAdminReviewSources, error) {
	if agentDB == nil || reviewDB == nil || tenantUUID == nil {
		return PersonaAdminReviewSources{}, ErrPersonaReviewUnavailable
	}
	authority, err := NewCurrentPersonaReviewGrantAuthority(agentDB, tenantUUID)
	if err != nil {
		return PersonaAdminReviewSources{}, ErrPersonaReviewUnavailable
	}
	writer, err := agentpersonastore.NewDurablePersonaReviewIssuer(reviewDB, tenantUUID)
	if err != nil {
		return PersonaAdminReviewSources{}, ErrPersonaReviewUnavailable
	}
	return PersonaAdminReviewSources{Authority: authority, Writer: writer}, nil
}

// CurrentPersonaReviewGrantAuthority reads persona-review grants using the
// tenant-serving agent role. It has no evidence-writing capability.
type CurrentPersonaReviewGrantAuthority struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

// NewCurrentPersonaReviewGrantAuthority binds grant reads to the ordinary
// agent application pool and canonical tenant mapping.
func NewCurrentPersonaReviewGrantAuthority(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) (*CurrentPersonaReviewGrantAuthority, error) {
	if db == nil || tenantUUID == nil {
		return nil, ErrPersonaReviewUnavailable
	}
	return &CurrentPersonaReviewGrantAuthority{db: db, tenantUUID: tenantUUID}, nil
}

// AuthorizePersonaReview checks the current, unrevoked tenant grant in a
// tenant-scoped transaction. The durable writer repeats this check while
// holding its grant lock before recording evidence.
func (a *CurrentPersonaReviewGrantAuthority) AuthorizePersonaReview(ctx context.Context, tenant values.TenantId, reviewerID string) error {
	if a == nil || a.db == nil || a.tenantUUID == nil || ctx == nil || tenant.Validate() != nil || reviewerID == "" || strings.TrimSpace(reviewerID) != reviewerID {
		return ErrPersonaReviewDenied
	}
	tenantID := a.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return ErrPersonaReviewDenied
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("application: begin persona-review grant lookup: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("application: scope persona-review grant lookup: %w", err)
	}
	var current bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM persona_review_grant
		WHERE tenant_id=$1 AND principal_id=$2 AND permission='persona:review'
		AND granted_at <= clock_timestamp() AND expires_at > clock_timestamp() AND revoked_at IS NULL)`, tenantID, reviewerID).Scan(&current)
	if err != nil {
		return fmt.Errorf("application: query current persona-review grant: %w", err)
	}
	if !current {
		return ErrPersonaReviewDenied
	}
	return nil
}

var _ PersonaReviewAuthority = (*CurrentPersonaReviewGrantAuthority)(nil)
var _ PersonaReviewEvidenceWriter = (*agentpersonastore.DurablePersonaReviewIssuer)(nil)
