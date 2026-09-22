package inbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type WorkflowPageQuery struct {
	PageQuery
	// Empty purpose selects both APPROVAL and TASK notices.
	Purpose    string
	InstanceID uuid.UUID
}

type WorkflowPage struct {
	Records []WorkflowRecord
	Next    *Position
}

// WorkflowNoticesPage filters in PostgreSQL before limiting. Records contain
// references only: callers must still apply current workflow authorization.
func (s Store) WorkflowNoticesPage(ctx context.Context, ex Executor, tenant uuid.UUID, subject string, q WorkflowPageQuery) (WorkflowPage, error) {
	where, args, limit, err := pagePredicate(tenant, subject, q.PageQuery)
	if err != nil {
		return WorkflowPage{}, err
	}
	if q.Purpose != "" && q.Purpose != "APPROVAL" && q.Purpose != "TASK" {
		return WorkflowPage{}, invalid("purpose", "expected APPROVAL or TASK")
	}
	where += " AND r.recipient_ref=$2 AND m.template_key='workflow.attention'"
	if q.Purpose != "" {
		args = append(args, q.Purpose)
		where += fmt.Sprintf(" AND m.purpose=$%d", len(args))
	}
	if q.InstanceID != uuid.Nil {
		args = append(args, q.InstanceID.String())
		where += fmt.Sprintf(" AND m.workflow_ref=$%d", len(args))
	}
	args = append(args, limit+1)
	rows, err := ex.Query(ctx, `SELECT i.inbox_record_id,i.recipient_message_id,i.read_state,i.version,i.created_at,
		i.state_changed_at,m.audience_expression->>'work_item_id',m.workflow_ref,m.purpose,m.correlation_key,i.archived,i.pinned
		FROM inbox_record i JOIN recipient_message r ON r.tenant_id=i.tenant_id AND r.recipient_message_id=i.recipient_message_id
		JOIN message_intent m ON m.tenant_id=r.tenant_id AND m.message_intent_id=r.message_intent_id
		WHERE `+where+fmt.Sprintf(" ORDER BY i.created_at DESC,i.inbox_record_id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return WorkflowPage{}, fmt.Errorf("inbox: list workflow page: %w", err)
	}
	defer rows.Close()
	out := WorkflowPage{Records: make([]WorkflowRecord, 0, limit)}
	for rows.Next() {
		row := WorkflowRecord{Record: Record{TenantID: tenant, SubjectRef: subject}}
		var item, instance string
		var version int64
		if err := rows.Scan(&row.InboxRecordID, &row.RecipientMessageID, &row.ReadState, &version, &row.CreatedAt, &row.StateChangedAt, &item, &instance, &row.Purpose, &row.CorrelationID, &row.Archived, &row.Pinned); err != nil {
			return WorkflowPage{}, err
		}
		row.WorkItemID, err = uuid.Parse(item)
		if err != nil {
			return WorkflowPage{}, fmt.Errorf("inbox: invalid stored work item: %w", err)
		}
		row.InstanceID, err = uuid.Parse(instance)
		if err != nil {
			return WorkflowPage{}, fmt.Errorf("inbox: invalid stored workflow: %w", err)
		}
		row.Version = uint64(version)
		row.CreatedAt, row.StateChangedAt = row.CreatedAt.UTC(), row.StateChangedAt.UTC()
		if len(out.Records) == limit {
			last := out.Records[limit-1]
			out.Next = &Position{CreatedAt: last.CreatedAt, RecordID: last.InboxRecordID}
			break
		}
		out.Records = append(out.Records, row)
	}
	if err := rows.Err(); err != nil {
		return WorkflowPage{}, err
	}
	return out, nil
}
