// Atomic deploy for HUB-009: one transaction checks the approved review
// against the exact version hash, checks the deployer's grant, compares
// and swaps the scoped pointer, advances version lifecycle, and publishes
// the outbox event. Any failure rolls everything back, so the previous
// deployment stays live and no event escapes for a deploy that did not
// commit.
package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrNoApprovedReview is returned when no approved review binds the version
// hash for the scope; ErrStalePointer when the pointer moved under the
// deploy's expected base.
var (
	ErrNoApprovedReview = errors.New("document deploy: no approved review for version and scope")
	ErrStalePointer     = errors.New("document deploy: scoped pointer moved")
)

// DeployInput names the version, the scope, who deploys, and the live
// version the deployer saw, or empty when the scope has no deployment yet.
type DeployInput struct {
	DocumentID, VersionID, ScopeKind, ScopeID, DeployerID, ExpectedLive string
}

// Deploy publishes one reviewed version to one scope.
func (s *Store) Deploy(ctx context.Context, tenantID string, in DeployInput) (Deployment, error) {
	var result Deployment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		version, err := loadVersion(ctx, tx, tenantID, in.DocumentID, in.VersionID)
		if err != nil {
			return err
		}
		if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.DeployerID, ActionDeploy); err != nil {
			return err
		}
		review, err := latestReviewTx(ctx, tx, tenantID, in.DocumentID, in.VersionID, in.ScopeKind, in.ScopeID)
		if err != nil {
			return ErrNoApprovedReview
		}
		if review.Decision != ReviewApproved || review.VersionHash != version.Hash {
			return ErrNoApprovedReview
		}
		if err := requireLinksResolved(ctx, tx, tenantID, in.DocumentID, in.VersionID, in.ScopeKind, "person", in.DeployerID); err != nil {
			return err
		}
		var currentDeployment, currentVersion string
		err = tx.QueryRow(ctx, `SELECT deployment_id,version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND scope_kind=$3 AND scope_id=$4`,
			tenantID, in.DocumentID, in.ScopeKind, in.ScopeID).Scan(&currentDeployment, &currentVersion)
		if err != nil {
			currentDeployment, currentVersion = "", ""
		}
		if currentVersion != in.ExpectedLive {
			return ErrStalePointer
		}
		deployment := Deployment{
			ID:          "docd-" + uuid.NewString(),
			DocumentID:  in.DocumentID,
			VersionID:   in.VersionID,
			ScopeKind:   in.ScopeKind,
			ScopeID:     in.ScopeID,
			DeployerID:  in.DeployerID,
			EffectiveAt: version.CreatedAt,
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id,prior_deployment_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			deployment.ID, tenantID, deployment.DocumentID, deployment.VersionID, deployment.ScopeKind, deployment.ScopeID, deployment.DeployerID, currentDeployment); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_active_pointer(tenant_id,document_id,scope_kind,scope_id,deployment_id,version_id) VALUES($1,$2,$3,$4,$5,$6)
			ON CONFLICT (tenant_id,document_id,scope_kind,scope_id) DO UPDATE SET deployment_id=EXCLUDED.deployment_id, version_id=EXCLUDED.version_id, updated_at=now()`,
			tenantID, in.DocumentID, in.ScopeKind, in.ScopeID, deployment.ID, in.VersionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE document_version SET status='deployed' WHERE tenant_id=$1 AND id=$2`, tenantID, in.VersionID); err != nil {
			return err
		}
		if currentVersion != "" && currentVersion != in.VersionID {
			var liveElsewhere int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND version_id=$2 AND NOT (document_id=$3 AND scope_kind=$4 AND scope_id=$5)`,
				tenantID, currentVersion, in.DocumentID, in.ScopeKind, in.ScopeID).Scan(&liveElsewhere); err != nil {
				return err
			}
			if liveElsewhere == 0 {
				if _, err := tx.Exec(ctx, `UPDATE document_version SET status='stale' WHERE tenant_id=$1 AND id=$2`, tenantID, currentVersion); err != nil {
					return err
				}
			}
		}
		payload, err := deploymentEventPayload(tenantID, deployment, currentDeployment, review.ID, version.Hash)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'deployment.published',$3)`, tenantID, in.DocumentID, payload); err != nil {
			return err
		}
		result = deployment
		return nil
	})
	if err != nil {
		return Deployment{}, err
	}
	return result, nil
}

// deploymentEventPayload is the canonical publication event body: the new
// deployment, the prior pointer it replaced, the approving review, and the
// exact bytes hash it publishes.
func deploymentEventPayload(tenantID string, d Deployment, priorDeployment, reviewID, versionHash string) (string, error) {
	raw, err := json.Marshal(map[string]string{
		"deployer_id": d.DeployerID, "deployment_id": d.ID, "document_id": d.DocumentID,
		"prior_deployment_id": priorDeployment, "review_id": reviewID,
		"scope_id": d.ScopeID, "scope_kind": d.ScopeKind,
		"tenant_id": tenantID, "version_hash": versionHash, "version_id": d.VersionID,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
