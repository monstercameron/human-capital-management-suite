package chatstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *Store) ChannelStatusMembershipCurrent(ctx context.Context, tenant, conversation, home, subject string) (bool, error) {
	active := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' AND joined_at<=statement_timestamp() AND left_at IS NULL)`, tenant, conversation, home, subject).Scan(&active)
	})
	return active, err
}

func (s *Store) SearchChannelStatusPermissions(ctx context.Context, tenant, query string, recheck func(context.Context) error) ([]chat.ChannelStatusPermissionGrant, error) {
	if recheck == nil {
		return nil, chat.ErrPermissionDenied
	}
	if tenant == "" || len(query) > 512 {
		return nil, chat.ErrInvalidArgument
	}
	out := []chat.ChannelStatusPermissionGrant{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := recheck(ctx); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT tenant_id,conversation_id,role_id,change_open,change_restricted,post_announcements,revision FROM chatstate_permission WHERE tenant_id=$1 AND position(lower($2) in lower(role_id))>0 ORDER BY role_id,conversation_id LIMIT 50`, tenant, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var grant chat.ChannelStatusPermissionGrant
			if err := rows.Scan(&grant.TenantID, &grant.ConversationID, &grant.RoleID, &grant.ChangeOpen, &grant.ChangeRestricted, &grant.PostAnnouncements, &grant.Revision); err != nil {
				return err
			}
			out = append(out, grant)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) ChannelStatusRolePermissions(ctx context.Context, tenantID, conversation string, roles []string) (chatpolicy.StatusPermissions, error) {
	p := chatpolicy.StatusPermissions{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT coalesce(bool_or(change_open),false),coalesce(bool_or(change_restricted),false),coalesce(bool_or(post_announcements),false) FROM chatstate_permission WHERE tenant_id=$1 AND conversation_id IN ('',$2) AND role_id=ANY($3::text[])`, tenantID, conversation, nonNil(roles)).Scan(&p.ChangeOpen, &p.ChangeRestricted, &p.PostAnnouncements)
	})
	return p, err
}

func (s *Store) PutChannelStatusPermission(ctx context.Context, actor chat.Principal, grant chat.ChannelStatusPermissionGrant, expected uint64, reason string, recheck func(context.Context, chat.ChannelStatusPermissionGrant) error) (chat.ChannelStatusPermissionGrant, error) {
	if actor.TenantID != grant.TenantID || actor.SubjectID == "" {
		return grant, chat.ErrPermissionDenied
	}
	if recheck == nil || grant.TenantID == "" || strings.TrimSpace(grant.RoleID) == "" || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return grant, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, grant.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chatstate-permissions:"+grant.TenantID); err != nil {
			return err
		}
		if grant.ConversationID != "" {
			if err := fenceContextWrite(ctx, tx, grant.TenantID, grant.ConversationID); err != nil {
				return err
			}
		}
		previous := chat.ChannelStatusPermissionGrant{TenantID: grant.TenantID, ConversationID: grant.ConversationID, RoleID: grant.RoleID}
		err := tx.QueryRow(ctx, `SELECT revision,change_open,change_restricted,post_announcements FROM chatstate_permission WHERE tenant_id=$1 AND conversation_id=$2 AND role_id=$3 FOR UPDATE`, grant.TenantID, grant.ConversationID, grant.RoleID).Scan(&previous.Revision, &previous.ChangeOpen, &previous.ChangeRestricted, &previous.PostAnnouncements)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if previous.Revision != expected {
			return chat.ErrConflict
		}
		if err = recheck(ctx, previous); err != nil {
			return err
		}
		grant.Revision = previous.Revision + 1
		_, err = tx.Exec(ctx, `INSERT INTO chatstate_permission(tenant_id,conversation_id,role_id,change_open,change_restricted,post_announcements,revision) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,conversation_id,role_id) DO UPDATE SET change_open=EXCLUDED.change_open,change_restricted=EXCLUDED.change_restricted,post_announcements=EXCLUDED.post_announcements,revision=EXCLUDED.revision`, grant.TenantID, grant.ConversationID, grant.RoleID, grant.ChangeOpen, grant.ChangeRestricted, grant.PostAnnouncements, grant.Revision)
		if err != nil {
			return err
		}
		if grant.ConversationID != "" {
			return emitAdapterEvent(ctx, tx, grant.TenantID, grant.ConversationID, "conversation.status_permission_changed", actor.TenantID, actor.SubjectID, grant.RoleID, grant.Revision, grant)
		}
		return recordChatEvent(ctx, tx, grant.TenantID, "", "chatstate-permission:"+grant.RoleID, "conversation.status_permission_changed", actor.TenantID, actor.SubjectID, grant.RoleID, "", "", grant.RoleID, grant.Revision, 0, []byte(`{}`))
	})
	return grant, err
}

// SearchChannelStatuses filters by active caller membership before returning
// status metadata. The service then rechecks current access for every result.
func (s *Store) SearchChannelStatuses(ctx context.Context, p chat.Principal, tenantID, query string, archived bool) ([]chat.ChannelStatus, error) {
	out := []chat.ChannelStatus{}
	if p.TenantID == "" || p.SubjectID == "" || tenantID == "" || len(query) > 512 {
		return nil, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.tenant_id,c.id,c.lifecycle,c.settings_revision,c.status_changed_by,c.status_changed_at,c.status_reason,c.status_until,c.name FROM chat_conversation c WHERE c.tenant_id=$1 AND c.kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') AND (c.lifecycle='ARCHIVED')=$5 AND position(lower($4) in lower(c.name))>0 AND EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=c.tenant_id AND m.conversation_id=c.id AND m.home_tenant_id=$2 AND m.member_id=$3 AND m.state='active') ORDER BY c.name,c.id LIMIT 50`, tenantID, p.TenantID, p.SubjectID, query, archived)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			status, err := chatstateScan(rows)
			if err != nil {
				return err
			}
			out = append(out, status)
		}
		return rows.Err()
	})
	return out, err
}
