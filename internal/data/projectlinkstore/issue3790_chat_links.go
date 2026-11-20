package projectlinkstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
)

// ChatLinkedTask is one live task made from, or linked to, a chat message.
type ChatLinkedTask struct {
	PostID, ProjectID, TaskID, Title, StatusID, AssigneeID, DueDate string
}

// ListByChatConversation finds the live tasks that link a message of one
// conversation, so the conversation can show what its messages became. The
// caller must still authorize the conversation for the viewer and each task's
// project before disclosing a row.
func (s *Store) ListByChatConversation(ctx context.Context, tenantID, conversationID string, limit int32) ([]ChatLinkedTask, error) {
	if s == nil || s.projects == nil || strings.TrimSpace(tenantID) == "" || !validID(conversationID) || limit < 1 || limit > 500 {
		return nil, ErrInvalidRecord
	}
	out := make([]ChatLinkedTask, 0, limit)
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT l.target_id,l.project_id,l.task_id,t.title,t.status_id,t.assignee_id,COALESCE(to_char(t.due_date,'YYYY-MM-DD'),'') FROM project_task_link l JOIN project_task t ON t.tenant_id=l.tenant_id AND t.project_id=l.project_id AND t.id=l.task_id WHERE l.tenant_id=$1 AND l.kind=$2 AND l.conversation_id=$3 AND l.removed_at IS NULL AND t.archived=false ORDER BY t.created_at,l.task_id LIMIT $4`, tenantID, string(projectlink.ChatPost), conversationID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x ChatLinkedTask
			if err := rows.Scan(&x.PostID, &x.ProjectID, &x.TaskID, &x.Title, &x.StatusID, &x.AssigneeID, &x.DueDate); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
