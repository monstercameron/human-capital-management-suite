// Authorized export for HUB-038: one read-only assembly carrying scoped
// Markdown, the version and deployment manifest, artifact refs, the stable
// link map and hold state with the records policy. Export needs both the
// read and export capabilities live; links carry target ids only, never
// target bytes, and held versions are included, never dropped. The bundle
// is transient: nothing is persisted, so the export records class stays a
// governed declaration until an audit sink lands it.
package documenthubstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ExportedVersion is one version in an export bundle.
type ExportedVersion struct {
	ID, Title, Classification, Hash, Markdown, Status string
	CreatedAt                                         time.Time
}

// ExportedDeployment is one live scope deployment in an export bundle.
type ExportedDeployment struct {
	ScopeKind, ScopeID, VersionID, DeployerID string
	EffectiveAt                               time.Time
}

// ExportedLink is one stable link reference: ids and state only.
type ExportedLink struct {
	SourceVersionID, Label, TargetDocID, PinnedVersion, Block, State string
}

// ExportedHold is one hold record in an export bundle.
type ExportedHold struct {
	ID, Reason, PlacedBy string
	Active               bool
	PlacedAt             time.Time
}

// ExportBundle is the full authorized export of one document.
type ExportBundle struct {
	DocumentID, OwnerID, Lifecycle, RecordsPolicy string
	Versions                                      []ExportedVersion
	Deployments                                   []ExportedDeployment
	Attachments                                   []Attachment
	Links                                         []ExportedLink
	Holds                                         []ExportedHold
}

// ExportDocument assembles the export bundle for a reader holding the
// current read and export capabilities. Unknown documents fail as denied.
func (s *Store) ExportDocument(ctx context.Context, tenantID, docID, readerKind, readerID string) (ExportBundle, error) {
	var out ExportBundle
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, lifecycle, series string
		if err := tx.QueryRow(ctx, `SELECT owner_id,lifecycle,retention_series FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, docID).Scan(&owner, &lifecycle, &series); err != nil {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, tenantID, docID, readerKind, readerID, ActionRead); err != nil {
			return err
		}
		if err := authorizeTx(ctx, tx, tenantID, docID, readerKind, readerID, ActionExport); err != nil {
			return err
		}
		out = ExportBundle{DocumentID: docID, OwnerID: owner, Lifecycle: lifecycle, RecordsPolicy: series}
		rows, err := tx.Query(ctx, `SELECT id,title,classification,content_hash,normalized_markdown,status,created_at FROM document_version WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v ExportedVersion
			if err := rows.Scan(&v.ID, &v.Title, &v.Classification, &v.Hash, &v.Markdown, &v.Status, &v.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out.Versions = append(out.Versions, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		deploys, err := tx.Query(ctx, `SELECT p.scope_kind,p.scope_id,d.version_id,d.deployer_id,d.effective_at
			FROM document_active_pointer p JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id
			WHERE p.tenant_id=$1 AND p.document_id=$2 ORDER BY p.scope_kind, p.scope_id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for deploys.Next() {
			var d ExportedDeployment
			if err := deploys.Scan(&d.ScopeKind, &d.ScopeID, &d.VersionID, &d.DeployerID, &d.EffectiveAt); err != nil {
				deploys.Close()
				return err
			}
			out.Deployments = append(out.Deployments, d)
		}
		deploys.Close()
		if err := deploys.Err(); err != nil {
			return err
		}
		attach, err := tx.Query(ctx, `SELECT id,document_id,version_id,version_hash,artifact_id,filename,content_type,size_bytes,classification,quarantine_state,scanner_version FROM document_attachment WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for attach.Next() {
			var a Attachment
			if err := attach.Scan(&a.ID, &a.DocumentID, &a.VersionID, &a.VersionHash, &a.ArtifactID,
				&a.Filename, &a.ContentType, &a.SizeBytes, &a.Classification, &a.QuarantineState, &a.ScannerVersion); err != nil {
				attach.Close()
				return err
			}
			out.Attachments = append(out.Attachments, a)
		}
		attach.Close()
		if err := attach.Err(); err != nil {
			return err
		}
		links, err := tx.Query(ctx, `SELECT source_version_id,label,target_document_id,pinned_version_id,block_id,state FROM document_link WHERE tenant_id=$1 AND source_document_id=$2 ORDER BY source_version_id, label`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for links.Next() {
			var l ExportedLink
			if err := links.Scan(&l.SourceVersionID, &l.Label, &l.TargetDocID, &l.PinnedVersion, &l.Block, &l.State); err != nil {
				links.Close()
				return err
			}
			out.Links = append(out.Links, l)
		}
		links.Close()
		if err := links.Err(); err != nil {
			return err
		}
		holds, err := tx.Query(ctx, `SELECT id,reason,placed_by,active,placed_at FROM document_hold WHERE tenant_id=$1 AND document_id=$2 ORDER BY placed_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for holds.Next() {
			var h ExportedHold
			if err := holds.Scan(&h.ID, &h.Reason, &h.PlacedBy, &h.Active, &h.PlacedAt); err != nil {
				holds.Close()
				return err
			}
			out.Holds = append(out.Holds, h)
		}
		holds.Close()
		return holds.Err()
	})
	if err != nil {
		return ExportBundle{}, err
	}
	return out, nil
}
