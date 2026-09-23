// Withdrawal and redeploy for HUB-010: an authorized actor can withdraw a
// live deployment at once under the retire capability, and an old version
// can be redeployed through the normal reviewed deploy path. Both append
// records and events; neither rewrites history or touches immutable bytes.
package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// WithdrawInput names the live scope, who withdraws, the live version they
// saw, and the audited reason.
type WithdrawInput struct {
	DocumentID, ScopeKind, ScopeID, ActorID, ExpectedLive, Reason string
}

// Withdrawal records which deployment left the scope live-empty.
type Withdrawal struct {
	DeploymentID, DocumentID, VersionID, ScopeKind, ScopeID, ActorID, Reason string
}

// Withdraw removes the scope's active pointer. The deployment rows, version
// rows and prior events stay; a deployment.withdrawn event is appended and
// the version retires when no other scope keeps it live.
func (s *Store) Withdraw(ctx context.Context, tenantID string, in WithdrawInput) (Withdrawal, error) {
	if in.Reason == "" {
		return Withdrawal{}, errors.New("document withdraw: reason is required")
	}
	var result Withdrawal
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.ActorID, ActionRetire); err != nil {
			return err
		}
		var deploymentID, liveVersion string
		if err := tx.QueryRow(ctx, `SELECT deployment_id,version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND scope_kind=$3 AND scope_id=$4`,
			tenantID, in.DocumentID, in.ScopeKind, in.ScopeID).Scan(&deploymentID, &liveVersion); err != nil {
			return ErrStalePointer
		}
		if liveVersion != in.ExpectedLive {
			return ErrStalePointer
		}
		payload, err := withdrawalEventPayload(tenantID, deploymentID, in)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'deployment.withdrawn',$3)`, tenantID, in.DocumentID, payload); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND scope_kind=$3 AND scope_id=$4`,
			tenantID, in.DocumentID, in.ScopeKind, in.ScopeID); err != nil {
			return err
		}
		var liveElsewhere int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND version_id=$2`, tenantID, liveVersion).Scan(&liveElsewhere); err != nil {
			return err
		}
		if liveElsewhere == 0 {
			if _, err := tx.Exec(ctx, `UPDATE document_version SET status='retired' WHERE tenant_id=$1 AND id=$2`, tenantID, liveVersion); err != nil {
				return err
			}
		}
		result = Withdrawal{DeploymentID: deploymentID, DocumentID: in.DocumentID, VersionID: liveVersion, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID, ActorID: in.ActorID, Reason: in.Reason}
		return nil
	})
	if err != nil {
		return Withdrawal{}, err
	}
	return result, nil
}

// withdrawalEventPayload is the canonical withdrawal event body.
func withdrawalEventPayload(tenantID, deploymentID string, in WithdrawInput) (string, error) {
	raw, err := json.Marshal(map[string]string{
		"actor_id": in.ActorID, "deployment_id": deploymentID, "document_id": in.DocumentID,
		"reason": in.Reason, "scope_id": in.ScopeID, "scope_kind": in.ScopeKind,
		"tenant_id": tenantID, "version_id": in.ExpectedLive,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
