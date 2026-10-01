package documenthubstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SeedUpgradeInput describes a maintenance-only append from a known legacy
// seed version to its deterministic tenant-specific replacement.
type SeedUpgradeInput struct {
	DocumentID, OwnerID, LegacyTitle, LegacyHash string
	AdaptedTitle, AdaptedMarkdown                string
}

// SeedUpgradeDecision is the bounded result of a read-only seed eligibility check.
type SeedUpgradeDecision struct {
	DocumentID string
	Eligible   bool
	Reason     string
}

// InspectPristineSeed reports whether a legacy seed row is eligible without
// changing data. Reasons are intentionally coarse and contain no document data.
func (s *Store) InspectPristineSeed(ctx context.Context, tenantID string, in SeedUpgradeInput) (SeedUpgradeDecision, error) {
	decision := SeedUpgradeDecision{DocumentID: in.DocumentID}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, home, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, in.DocumentID).Scan(&owner, &home, &lifecycle); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				decision.Reason = "missing"
				return nil
			}
			return err
		}
		if owner != in.OwnerID {
			decision.Reason = "owner"
			return nil
		}
		if home != "PERSONAL" || lifecycle == "DISPOSED" {
			decision.Reason = "lifecycle"
			return nil
		}
		for _, action := range []string{ActionRead, ActionPropose} {
			if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.OwnerID, action); err != nil {
				decision.Reason = "authorization"
				return nil
			}
		}
		var versions int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_version WHERE tenant_id=$1 AND document_id=$2`, tenantID, in.DocumentID).Scan(&versions); err != nil {
			return err
		}
		if versions != 1 {
			decision.Reason = "history"
			return nil
		}
		var title, hash, creator, status string
		if err := tx.QueryRow(ctx, `SELECT title,content_hash,creator_id,status FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND parent_id=''`, tenantID, in.DocumentID).Scan(&title, &hash, &creator, &status); err != nil {
			return err
		}
		if creator != in.OwnerID {
			decision.Reason = "creator"
			return nil
		}
		if status != "candidate" {
			decision.Reason = "status"
			return nil
		}
		if title != in.LegacyTitle || hash != in.LegacyHash {
			decision.Reason = "content"
			return nil
		}
		var pointers, deployments int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2`, tenantID, in.DocumentID).Scan(&pointers); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_deployment WHERE tenant_id=$1 AND document_id=$2`, tenantID, in.DocumentID).Scan(&deployments); err != nil {
			return err
		}
		if pointers != 0 || deployments != 0 {
			decision.Reason = "deployment"
			return nil
		}
		decision.Eligible = true
		decision.Reason = "eligible"
		return nil
	})
	return decision, err
}

// UpgradePristineSeed atomically appends a tenant-specific seed version only
// while the document is still one unpublished legacy candidate. Deployment
// history and active pointers are both rejected, so withdrawal cannot make a
// previously published document eligible again.
func (s *Store) UpgradePristineSeed(ctx context.Context, tenantID string, in SeedUpgradeInput) (bool, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(in.DocumentID) == "" || strings.TrimSpace(in.OwnerID) == "" ||
		strings.TrimSpace(in.LegacyTitle) == "" || strings.TrimSpace(in.LegacyHash) == "" || strings.TrimSpace(in.AdaptedTitle) == "" ||
		strings.TrimSpace(in.AdaptedMarkdown) == "" {
		return false, errors.New("document seed upgrade: incomplete input")
	}
	var upgraded bool
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, home, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, in.DocumentID).Scan(&owner, &home, &lifecycle); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return nil
			}
			return err
		}
		if owner != in.OwnerID || home != "PERSONAL" || lifecycle == "DISPOSED" {
			return nil
		}
		for _, action := range []string{ActionRead, ActionPropose} {
			if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.OwnerID, action); err != nil {
				return nil
			}
		}
		var versions int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_version WHERE tenant_id=$1 AND document_id=$2`, tenantID, in.DocumentID).Scan(&versions); err != nil {
			return err
		}
		if versions != 1 {
			return nil
		}
		var latest Version
		var status string
		if err := tx.QueryRow(ctx, `SELECT id,document_id,parent_id,creator_id,title,locale,classification,normalized_markdown,content_hash,renderer_profile,change_note,status,created_at FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND parent_id=''`, tenantID, in.DocumentID).Scan(&latest.ID, &latest.DocumentID, &latest.ParentID, &latest.CreatorID, &latest.Title, &latest.Locale, &latest.Classification, &latest.Markdown, &latest.Hash, &latest.Renderer, &latest.ChangeNote, &status, &latest.CreatedAt); err != nil {
			return err
		}
		if latest.CreatorID != in.OwnerID || status != "candidate" || latest.Title != in.LegacyTitle || latest.Hash != in.LegacyHash {
			return nil
		}
		var pointers, deployments int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2`, tenantID, in.DocumentID).Scan(&pointers); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_deployment WHERE tenant_id=$1 AND document_id=$2`, tenantID, in.DocumentID).Scan(&deployments); err != nil {
			return err
		}
		if pointers != 0 || deployments != 0 || in.AdaptedMarkdown == latest.Markdown {
			return nil
		}
		stored, err := insertVersionTx(ctx, tx, tenantID, Version{DocumentID: in.DocumentID, ParentID: latest.ID, CreatorID: in.OwnerID, Title: in.AdaptedTitle, Markdown: in.AdaptedMarkdown, Locale: latest.Locale, Classification: latest.Classification, Renderer: latest.Renderer})
		if err != nil {
			return err
		}
		if err := s.enqueueIndexTx(ctx, tx, tenantID, in.DocumentID, stored.ID); err != nil {
			return err
		}
		upgraded = true
		return nil
	})
	return upgraded, err
}
