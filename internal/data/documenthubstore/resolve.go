// Read-time link resolution for HUB-021: unpinned links track the scope's
// live pointer, pinned links return their exact version forever. Every
// resolution carries the version hash as evidence and requires the read
// action on the target, so following a link never bypasses policy.
package documenthubstore

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrNoResolution is returned when a link names no readable live version:
// an unknown or unreadable target, or a scope with no deployment.
var ErrNoResolution = errors.New("document link: no readable resolution")

// LinkResolution is one resolved read: what the reader saw, pinned or not.
type LinkResolution struct {
	TargetDocID, ResolvedVersionID, VersionHash, Block string
	Pinned                                             bool
}

// ResolveLink resolves one canonical link for one principal in one scope.
func (s *Store) ResolveLink(ctx context.Context, tenantID, scopeKind, scopeID string, link DocLink, subjectKind, subjectID string) (LinkResolution, error) {
	if link.State != LinkValid || link.TargetDocID == "" {
		return LinkResolution{}, ErrNoResolution
	}
	var res LinkResolution
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, link.TargetDocID, subjectKind, subjectID, ActionRead); err != nil {
			return err
		}
		if link.PinnedVersion != "" {
			// A pinned citation is meaningful only when this exact version
			// was deployed in the requested context. A read grant alone must
			// not make candidate or unrelated-scope content resolvable.
			var deployed int
			if err := tx.QueryRow(ctx, `SELECT 1 FROM document_deployment WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3 AND scope_kind=$4 AND scope_id=$5 LIMIT 1`,
				tenantID, link.TargetDocID, link.PinnedVersion, scopeKind, scopeID).Scan(&deployed); err != nil {
				return ErrNoResolution
			}
			version, err := loadVersionByID(ctx, tx, tenantID, link.PinnedVersion)
			if err != nil {
				return ErrNoResolution
			}
			if version.DocumentID != link.TargetDocID {
				return ErrNoResolution
			}
			res = LinkResolution{TargetDocID: link.TargetDocID, ResolvedVersionID: version.ID, VersionHash: version.Hash, Block: link.Block, Pinned: true}
			return nil
		}
		var versionID, hash string
		if err := tx.QueryRow(ctx, `SELECT p.version_id,v.content_hash FROM document_active_pointer p JOIN document_version v ON v.tenant_id=p.tenant_id AND v.id=p.version_id WHERE p.tenant_id=$1 AND p.document_id=$2 AND p.scope_kind=$3 AND p.scope_id=$4`,
			tenantID, link.TargetDocID, scopeKind, scopeID).Scan(&versionID, &hash); err != nil {
			return ErrNoResolution
		}
		res = LinkResolution{TargetDocID: link.TargetDocID, ResolvedVersionID: versionID, VersionHash: hash, Pinned: false}
		return nil
	})
	if err != nil {
		return LinkResolution{}, err
	}
	return res, nil
}

// loadVersionByID loads a version without assuming its document, for pinned
// resolution; callers verify the document match.
func loadVersionByID(ctx context.Context, tx dbport.Tx, tenantID, id string) (Version, error) {
	var v Version
	err := tx.QueryRow(ctx, `SELECT id,document_id,parent_id,creator_id,title,locale,classification,normalized_markdown,content_hash,renderer_profile,change_note,created_at FROM document_version WHERE tenant_id=$1 AND id=$2`,
		tenantID, id).Scan(&v.ID, &v.DocumentID, &v.ParentID, &v.CreatorID, &v.Title, &v.Locale, &v.Classification, &v.Markdown, &v.Hash, &v.Renderer, &v.ChangeNote, &v.CreatedAt)
	if err != nil {
		return Version{}, err
	}
	return v, nil
}
