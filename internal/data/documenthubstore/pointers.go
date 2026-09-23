// Deployment pointers for HUB-007: immutable deployment records plus one
// active pointer per document scope. Readers resolve the pointer; writers
// move it. Candidate submits never touch either table, so unreviewed work
// cannot leak into reader views.
package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrNoDeployment is returned when a scope has no active deployment.
var ErrNoDeployment = errors.New("document deployment: no active deployment for scope")

// Deployment is the active deployment a scope resolves to.
type Deployment struct {
	ID, DocumentID, VersionID      string
	ScopeKind, ScopeID, DeployerID string
	EffectiveAt                    time.Time
}

// ResolveDeployment returns the active deployment for one document scope.
// Scopes are exact: the default pointer and each placement resolve
// independently, and a scope with no endorsement resolves nothing.
func (s *Store) ResolveDeployment(ctx context.Context, tenantID, docID, scopeKind, scopeID string) (Deployment, error) {
	var d Deployment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT d.id,d.document_id,d.version_id,d.scope_kind,d.scope_id,d.deployer_id,d.effective_at
			FROM document_active_pointer p JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
			WHERE p.tenant_id=$1 AND p.document_id=$2 AND p.scope_kind=$3 AND p.scope_id=$4`,
			tenantID, docID, scopeKind, scopeID).Scan(&d.ID, &d.DocumentID, &d.VersionID, &d.ScopeKind, &d.ScopeID, &d.DeployerID, &d.EffectiveAt)
	})
	if err != nil {
		return Deployment{}, ErrNoDeployment
	}
	return d, nil
}
