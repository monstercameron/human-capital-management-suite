// Independent backup and restore for HUB-042: a document snapshot carries
// immutable versions, scoped deployments with their pointers, the full
// grant set and the aggregate outbox, and restore replays them
// byte-faithfully into a fresh scope with hash verification. Derived
// indexes are never snapshotted: the reconciler rebuilds them to a
// consistent watermark after restore. Attachments and comments travel by
// reference policy, not bytes: artifact ids live in version Markdown and
// protected storage keeps the bytes. Restore refuses existing documents
// and tampered hashes, and only the owning custodian restores.
package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrRestoreExists   = errors.New("document restore: document already exists")
	ErrRestoreTampered = errors.New("document restore: version hash mismatch")
)

// SnapshotVersion is one byte-faithful version in a backup.
type SnapshotVersion struct {
	ID, ParentID, CreatorID, Title, Locale, Classification string
	Markdown, Hash, Renderer, ChangeNote, Status           string
	CreatedAt                                              time.Time
}

// SnapshotDeployment is one byte-faithful deployment in a backup.
type SnapshotDeployment struct {
	ID, ScopeKind, ScopeID, VersionID, DeployerID, PriorDeploymentID string
	EffectiveAt, CreatedAt                                           time.Time
	ReviewDueAt                                                      *time.Time
}

// SnapshotPointer is one live scope pointer in a backup.
type SnapshotPointer struct {
	ScopeKind, ScopeID, DeploymentID, VersionID string
}

// SnapshotGrant is one byte-faithful grant in a backup, live or revoked.
type SnapshotGrant struct {
	ID, SubjectKind, SubjectID, Action, Effect, IssuerID, Purpose string
	ExpiresAt, RevokedAt                                          *time.Time
	Revision                                                      int64
	Revoked                                                       bool
	RevokedBy                                                     string
}

// SnapshotEvent is one aggregate outbox event in a backup.
type SnapshotEvent struct {
	EventType, Payload string
	CreatedAt          time.Time
}

// SnapshotBundle is the independent backup of one document.
type SnapshotBundle struct {
	DocumentID, OwnerID, Home, Lifecycle, Ceiling, RecordsPolicy string
	Versions                                                     []SnapshotVersion
	Deployments                                                  []SnapshotDeployment
	Pointers                                                     []SnapshotPointer
	Grants                                                       []SnapshotGrant
	Outbox                                                       []SnapshotEvent
	Watermark                                                    int64
}

// RestoreReport counts the rows one restore replayed.
type RestoreReport struct {
	Versions, Deployments, Pointers, Grants, Events int
}

// SnapshotDocument captures the independent backup of one document for
// its owner or a manager.
func (s *Store) SnapshotDocument(ctx context.Context, tenantID, docID, actorID string) (SnapshotBundle, error) {
	var out SnapshotBundle
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeOwnerTx(ctx, tx, tenantID, docID, actorID); err != nil {
			return err
		}
		var owner, home, lifecycle, ceiling, series string
		if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle,classification_ceiling,retention_series FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, docID).Scan(&owner, &home, &lifecycle, &ceiling, &series); err != nil {
			return ErrDenied
		}
		out = SnapshotBundle{DocumentID: docID, OwnerID: owner, Home: home, Lifecycle: lifecycle, Ceiling: ceiling, RecordsPolicy: series}
		rows, err := tx.Query(ctx, `SELECT id,parent_id,creator_id,title,locale,classification,normalized_markdown,content_hash,renderer_profile,change_note,status,created_at FROM document_version WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v SnapshotVersion
			if err := rows.Scan(&v.ID, &v.ParentID, &v.CreatorID, &v.Title, &v.Locale, &v.Classification,
				&v.Markdown, &v.Hash, &v.Renderer, &v.ChangeNote, &v.Status, &v.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out.Versions = append(out.Versions, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		deploys, err := tx.Query(ctx, `SELECT id,scope_kind,scope_id,version_id,deployer_id,prior_deployment_id,effective_at,created_at,review_due_at FROM document_deployment WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for deploys.Next() {
			var d SnapshotDeployment
			if err := deploys.Scan(&d.ID, &d.ScopeKind, &d.ScopeID, &d.VersionID, &d.DeployerID, &d.PriorDeploymentID, &d.EffectiveAt, &d.CreatedAt, &d.ReviewDueAt); err != nil {
				deploys.Close()
				return err
			}
			out.Deployments = append(out.Deployments, d)
		}
		deploys.Close()
		if err := deploys.Err(); err != nil {
			return err
		}
		pointers, err := tx.Query(ctx, `SELECT scope_kind,scope_id,deployment_id,version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 ORDER BY scope_kind, scope_id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for pointers.Next() {
			var p SnapshotPointer
			if err := pointers.Scan(&p.ScopeKind, &p.ScopeID, &p.DeploymentID, &p.VersionID); err != nil {
				pointers.Close()
				return err
			}
			out.Pointers = append(out.Pointers, p)
		}
		pointers.Close()
		if err := pointers.Err(); err != nil {
			return err
		}
		grants, err := tx.Query(ctx, `SELECT id,subject_kind,subject_id,action,effect,issuer_id,purpose,expires_at,revision,revoked,revoked_at,revoked_by FROM document_grant WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at, id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for grants.Next() {
			var g SnapshotGrant
			if err := grants.Scan(&g.ID, &g.SubjectKind, &g.SubjectID, &g.Action, &g.Effect, &g.IssuerID, &g.Purpose, &g.ExpiresAt, &g.Revision, &g.Revoked, &g.RevokedAt, &g.RevokedBy); err != nil {
				grants.Close()
				return err
			}
			out.Grants = append(out.Grants, g)
		}
		grants.Close()
		if err := grants.Err(); err != nil {
			return err
		}
		events, err := tx.Query(ctx, `SELECT event_type,payload,created_at FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2 ORDER BY id`,
			tenantID, docID)
		if err != nil {
			return err
		}
		for events.Next() {
			var e SnapshotEvent
			if err := events.Scan(&e.EventType, &e.Payload, &e.CreatedAt); err != nil {
				events.Close()
				return err
			}
			out.Outbox = append(out.Outbox, e)
		}
		events.Close()
		if err := events.Err(); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM document_outbox WHERE tenant_id=$1 AND aggregate_id=$2`,
			tenantID, docID).Scan(&out.Watermark)
	})
	if err != nil {
		return SnapshotBundle{}, err
	}
	return out, nil
}

// RestoreDocument replays a backup into a fresh scope under the owning
// custodian. Every version hash is verified before anything is written,
// so tampered backups fail closed with nothing stored.
func (s *Store) RestoreDocument(ctx context.Context, tenantID string, snap SnapshotBundle, actorID string) (RestoreReport, error) {
	var out RestoreReport
	if actorID != snap.OwnerID {
		return RestoreReport{}, ErrDenied
	}
	for _, v := range snap.Versions {
		if v.Hash == "" || v.Hash != HashContent(NormalizeMarkdown(v.Markdown)) {
			return RestoreReport{}, ErrRestoreTampered
		}
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var exists int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document WHERE tenant_id=$1 AND id=$2`,
			tenantID, snap.DocumentID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return ErrRestoreExists
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document(id,tenant_id,owner_id,home,lifecycle,classification_ceiling,retention_series) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			snap.DocumentID, tenantID, snap.OwnerID, snap.Home, snap.Lifecycle, snap.Ceiling, snap.RecordsPolicy); err != nil {
			return err
		}
		for _, v := range snap.Versions {
			if _, err := tx.Exec(ctx, `INSERT INTO document_version(id,tenant_id,document_id,parent_id,creator_id,title,locale,classification,normalized_markdown,content_hash,renderer_profile,change_note,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
				v.ID, tenantID, snap.DocumentID, v.ParentID, v.CreatorID, v.Title, v.Locale, v.Classification,
				v.Markdown, v.Hash, v.Renderer, v.ChangeNote, v.Status); err != nil {
				return err
			}
			out.Versions++
		}
		for _, d := range snap.Deployments {
			if _, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id,effective_at,review_due_at,prior_deployment_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				d.ID, tenantID, snap.DocumentID, d.VersionID, d.ScopeKind, d.ScopeID, d.DeployerID, d.EffectiveAt, d.ReviewDueAt, d.PriorDeploymentID); err != nil {
				return err
			}
			out.Deployments++
		}
		for _, p := range snap.Pointers {
			if _, err := tx.Exec(ctx, `INSERT INTO document_active_pointer(tenant_id,document_id,scope_kind,scope_id,deployment_id,version_id) VALUES($1,$2,$3,$4,$5,$6)`,
				tenantID, snap.DocumentID, p.ScopeKind, p.ScopeID, p.DeploymentID, p.VersionID); err != nil {
				return err
			}
			out.Pointers++
		}
		for _, g := range snap.Grants {
			if _, err := tx.Exec(ctx, `INSERT INTO document_grant(id,tenant_id,document_id,subject_kind,subject_id,action,effect,issuer_id,purpose,expires_at,revision,revoked,revoked_at,revoked_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
				g.ID, tenantID, snap.DocumentID, g.SubjectKind, g.SubjectID, g.Action, g.Effect, g.IssuerID, g.Purpose, g.ExpiresAt, g.Revision, g.Revoked, g.RevokedAt, g.RevokedBy); err != nil {
				return err
			}
			out.Grants++
		}
		for _, e := range snap.Outbox {
			if _, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload,created_at) VALUES($1,$2,$3,$4,$5)`,
				tenantID, snap.DocumentID, e.EventType, e.Payload, e.CreatedAt); err != nil {
				return err
			}
			out.Events++
		}
		return nil
	})
	if err != nil {
		return RestoreReport{}, err
	}
	return out, nil
}
