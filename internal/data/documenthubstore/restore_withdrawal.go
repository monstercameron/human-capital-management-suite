// RestoreWithdrawal undoes a prior Withdraw (HUB-010/DOCS-07): it puts the
// withdrawn version's content back live on the scope it was withdrawn
// from. For the default personal-document scope this re-establishes a
// state that was never reviewed to begin with (HUB-012's
// share-without-review path, sharing.go's SharePersonalDocumentRole), so
// restoring it does not newly require review either — it is Undo, not a
// new publication. Any other scope (a team/channel placement, HUB-014)
// did require review to go live, so it still does to come back:
// RestoreWithdrawal delegates those to the ordinary reviewed Deploy path,
// keeping HUB-010's fresh-evidence rule exactly where it already applied.
//
// The withdrawn row itself cannot always be reused as-is: migration
// 00008's version lifecycle is append-only and 'retired' is terminal (a
// retired row can never move back to 'deployed' — "rollback is a new
// deployment", that migration's own comment). Withdraw retires a version
// the instant nothing else keeps it live, which is exactly the ordinary
// single-deployment personal-document case this restores. So when the
// withdrawn version is already retired, RestoreWithdrawal clones its
// exact content (title, Markdown, locale, classification) into a new
// version parented on it and deploys that clone; when it is merely
// 'stale' (still kept live by some other scope), the same row is reused
// directly. Either way no byte the caller did not already author changes:
// this is the prior authorized state coming back, never new content.
package documenthubstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// RestoreInput names the scope and version a caller wants restored (the
// same identifiers Withdrawal reported) and who is restoring it.
type RestoreInput struct {
	DocumentID, ScopeKind, ScopeID, ActorID, VersionID string
}

// RestoreWithdrawal re-publishes VersionID to the named scope. It refuses
// when something is already live there (ErrStalePointer: the withdrawal
// was superseded, so blindly restoring would clobber it) and requires the
// same retire capability Withdraw itself required, since restoring is
// that withdrawal's own undo, not a fresh grant of publish authority.
func (s *Store) RestoreWithdrawal(ctx context.Context, tenantID string, in RestoreInput) (Deployment, error) {
	if in.ScopeKind != "default" {
		// A team/channel placement went live only under HUB-014's review
		// and deploy capability; restoring it still needs that same fresh
		// evidence, so this is not Undo's job to bypass.
		return s.Deploy(ctx, tenantID, DeployInput{
			DocumentID: in.DocumentID, VersionID: in.VersionID, ScopeKind: in.ScopeKind, ScopeID: in.ScopeID,
			DeployerID: in.ActorID,
		})
	}
	var result Deployment
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		version, err := loadVersion(ctx, tx, tenantID, in.DocumentID, in.VersionID)
		if err != nil {
			return err
		}
		if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.ActorID, ActionRetire); err != nil {
			return err
		}
		var liveVersion string
		err = tx.QueryRow(ctx, `SELECT version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND scope_kind='default' AND scope_id=''`,
			tenantID, in.DocumentID).Scan(&liveVersion)
		if err == nil {
			return ErrStalePointer
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM document_version WHERE tenant_id=$1 AND id=$2`, tenantID, in.VersionID).Scan(&status); err != nil {
			return err
		}
		restoreVersionID := in.VersionID
		if status == "retired" {
			// The withdrawn row itself is a terminal, append-only state
			// once retired (see the package doc comment above): clone its
			// exact content instead of trying to resurrect it, so restore
			// still lands the same title and bytes the caller withdrew.
			clone, err := insertVersionTx(ctx, tx, tenantID, Version{
				DocumentID: in.DocumentID, ParentID: version.ID, CreatorID: version.CreatorID,
				Title: version.Title, Markdown: version.Markdown, Locale: version.Locale,
				Classification: version.Classification, Renderer: version.Renderer,
			})
			if err != nil {
				return err
			}
			restoreVersionID = clone.ID
		}
		deploymentID := "docd-" + uuid.NewString()
		if _, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id) VALUES($1,$2,$3,$4,'default','',$5)`,
			deploymentID, tenantID, in.DocumentID, restoreVersionID, in.ActorID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_active_pointer(tenant_id,document_id,scope_kind,scope_id,deployment_id,version_id) VALUES($1,$2,'default','',$3,$4)`,
			tenantID, in.DocumentID, deploymentID, restoreVersionID); err != nil {
			return err
		}
		// candidate->deployed (a fresh clone) and stale->deployed (the
		// same row, still kept live elsewhere) are both allowed
		// transitions; retired->deployed is never reached here.
		if _, err := tx.Exec(ctx, `UPDATE document_version SET status='deployed' WHERE tenant_id=$1 AND id=$2`, tenantID, restoreVersionID); err != nil {
			return err
		}
		payload, err := restoreEventPayload(tenantID, deploymentID, in, restoreVersionID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'deployment.restored',$3)`, tenantID, in.DocumentID, payload); err != nil {
			return err
		}
		if err := s.enqueueIndexTx(ctx, tx, tenantID, in.DocumentID, restoreVersionID); err != nil {
			return err
		}
		result = Deployment{
			ID: deploymentID, DocumentID: in.DocumentID, VersionID: restoreVersionID,
			ScopeKind: "default", ScopeID: "", DeployerID: in.ActorID, EffectiveAt: version.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return Deployment{}, err
	}
	return result, nil
}

// restoreEventPayload is the canonical restore event body. version_id is
// the version actually put live: in.VersionID's row when it was still
// reusable, or a content-identical clone's id when the original had
// already been retired. restored_from always names what the caller asked
// to restore, so an outbox consumer can tell a clone from a reuse.
func restoreEventPayload(tenantID, deploymentID string, in RestoreInput, restoredVersionID string) (string, error) {
	raw, err := json.Marshal(map[string]string{
		"actor_id": in.ActorID, "deployment_id": deploymentID, "document_id": in.DocumentID,
		"scope_id": in.ScopeID, "scope_kind": in.ScopeKind,
		"tenant_id": tenantID, "version_id": restoredVersionID, "restored_from": in.VersionID,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
