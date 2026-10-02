package documenthubstore

import (
	"context"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SetAnnouncementDocuments converges only the READ grants owned by this
// announcement. Other announcements using the same service retain their grants.
func (s *Store) SetAnnouncementDocuments(ctx context.Context, tenant, owner, service, announcement string, documents []string, conversation ...string) error {
	if s == nil || ctx == nil || strings.TrimSpace(owner) == "" || strings.TrimSpace(service) == "" || strings.TrimSpace(announcement) == "" || len(documents) > 5 {
		return ErrDenied
	}
	purpose := "announcement:" + announcement
	scope := ""
	if len(conversation) == 1 {
		scope = conversation[0]
	}
	if len(conversation) > 1 {
		return ErrDenied
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		boundScopes := map[string]string{}
		// Validate all additions before changing any policy.
		for _, document := range documents {
			for _, action := range []string{ActionRead, ActionManage} {
				if err := authorizeTx(ctx, tx, tenant, document, "person", owner, action); err != nil {
					return err
				}
			}
			if scope != "" {
				var placed bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM document_active_pointer p JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id WHERE p.tenant_id=$1 AND p.document_id=$2 AND p.scope_kind='placement' AND p.scope_id=$3 AND d.custodian_id<>'' AND d.review_due_at IS NOT NULL)`, tenant, document, scope).Scan(&placed); err != nil {
					return err
				}
				if placed {
					boundScopes[document] = scope
				}
			}
		}
		rows, err := tx.Query(ctx, `SELECT id,document_id,subject_id,announcement_scope_id FROM document_grant WHERE tenant_id=$1 AND subject_kind='service' AND action='read' AND effect='allow' AND purpose=$2 AND revoked=false ORDER BY id`, tenant, purpose)
		if err != nil {
			return err
		}
		type liveGrant struct{ id, document, service, scope string }
		var live []liveGrant
		for rows.Next() {
			var g liveGrant
			if err := rows.Scan(&g.id, &g.document, &g.service, &g.scope); err != nil {
				rows.Close()
				return err
			}
			live = append(live, g)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		kept := map[string]bool{}
		for _, g := range live {
			if g.service == service && slices.Contains(documents, g.document) && g.scope == boundScopes[g.document] && !kept[g.document] {
				kept[g.document] = true
				continue
			}
			if err := revokeGrantTx(ctx, tx, tenant, g.id, owner); err != nil {
				return err
			}
		}
		for _, document := range documents {
			if !kept[document] {
				grant, err := grantActionTx(ctx, tx, tenant, GrantInput{DocumentID: document, SubjectKind: "service", SubjectID: service, Action: ActionRead, Effect: EffectAllow, Issuer: owner, Purpose: purpose})
				if err != nil {
					return err
				}
				if scope != "" {
					if _, err := tx.Exec(ctx, `UPDATE document_grant SET announcement_scope_id=$3 WHERE tenant_id=$1 AND id=$2 AND EXISTS(SELECT 1 FROM document_active_pointer p JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id WHERE p.tenant_id=$1 AND p.document_id=$4 AND p.scope_kind='placement' AND p.scope_id=$3 AND d.custodian_id<>'' AND d.review_due_at IS NOT NULL)`, tenant, grant.ID, scope, document); err != nil {
						return err
					}
				}
				kept[document] = true
			}
		}
		return nil
	})
}

// AuthorizeAnnouncementDocument prevents another announcement's service grant
// from widening this run's document set. A current explicit deny still wins.
func (s *Store) AuthorizeAnnouncementDocument(ctx context.Context, tenant, document, service, announcement string) error {
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenant, document, "service", service, ActionRead); err != nil {
			return err
		}
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM document_grant g WHERE g.tenant_id=$1 AND g.document_id=$2 AND g.subject_kind='service' AND g.subject_id=$3 AND g.purpose=$4 AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()) AND (g.announcement_scope_id='' OR EXISTS(SELECT 1 FROM document_active_pointer p JOIN document_deployment d ON d.tenant_id=p.tenant_id AND d.id=p.deployment_id WHERE p.tenant_id=g.tenant_id AND p.document_id=g.document_id AND p.scope_kind='placement' AND p.scope_id=g.announcement_scope_id AND d.custodian_id<>'' AND d.review_due_at IS NOT NULL)))`, tenant, document, service, "announcement:"+announcement).Scan(&live); err != nil {
			return err
		}
		if !live {
			return ErrDenied
		}
		return nil
	})
}
