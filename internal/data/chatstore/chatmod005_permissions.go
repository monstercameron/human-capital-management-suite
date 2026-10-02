package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CHATMOD-005: the four moderation permissions are rows a workspace
// administrator writes (AssignModerationPermission). Nothing read them back
// for a page, and the filter service did not read them at all. These are the
// two reads.

// ModerationPermissionRows lists the stored answers for the whole workspace
// and, when cid names one, for that conversation. Only a current workspace
// administrator may read them: they are the same person who may write them.
func (s *ModeratedAdapter) ModerationPermissionRows(ctx context.Context, p chat.Principal, t, cid string) ([]chat.ModerationPermissionRow, error) {
	if p.TenantID != t || p.SubjectID == "" {
		return nil, chat.ErrPermissionDenied
	}
	var out []chat.ModerationPermissionRow
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		var current string
		if err := tx.QueryRow(ctx, `SELECT role FROM chat_moderation_role WHERE tenant_id=$1 AND subject_id=$2 AND role=$3`, t, p.SubjectID, chat.WorkspaceAdministratorModerationRole).Scan(&current); err != nil {
			return chat.ErrPermissionDenied
		}
		rows, err := tx.Query(ctx, `SELECT conversation_id,role,permission,allowed FROM chat_moderation_permission WHERE tenant_id=$1 AND conversation_id IN ('',$2) ORDER BY conversation_id,role,permission`, t, cid)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row chat.ModerationPermissionRow
			if err = rows.Scan(&row.ConversationID, &row.Role, &row.Permission, &row.Allowed); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// ManageFiltersRows answers, for each role that has a stored answer, whether
// it holds "Manage filters" in a conversation ("" is the whole workspace). A
// conversation's own row wins over the workspace's, as it does for every other
// moderation permission. The filter authority combines it with the roles it
// resolved itself; nothing here decides for a person.
func (s *FilterStore) ManageFiltersRows(ctx context.Context, tenantID, cid string) (map[string]bool, error) {
	out := map[string]bool{}
	err := s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		// The workspace's rows come first, so a conversation's row replaces them.
		rows, err := tx.Query(ctx, `SELECT role,allowed FROM chat_moderation_permission WHERE tenant_id=$1 AND permission=$2 AND conversation_id IN ('',$3) ORDER BY conversation_id`, tenantID, chat.PermissionManageFilters, cid)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var role string
			var allowed bool
			if err = rows.Scan(&role, &allowed); err != nil {
				return err
			}
			out[role] = allowed
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
