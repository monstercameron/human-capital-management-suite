package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/messagingmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// Status events a workflow reports to the person who started the request.
const (
	// StatusInReview is published when the request is routed to someone for a
	// decision or a task. EventRef is the routed work item.
	StatusInReview = "IN_REVIEW"
	// StatusFinished is published by the run's terminal write. EventRef is
	// the END node that was reached.
	StatusFinished = "FINISHED"

	workflowStatusTemplate = "workflow.status"
	workflowStatusPurpose  = "WORKFLOW_UPDATE"
)

// WorkflowStatusNotice tells the requester that their request moved. Like
// WorkflowNotice it carries references only: what the request is and how it
// ended stay behind the workflow's current authorization check.
type WorkflowStatusNotice struct {
	TenantID, InstanceID                                       uuid.UUID
	SubjectRef, Event, EventRef, CorrelationID, AudienceDigest string
	CreatedAt                                                  time.Time
}

// WorkflowStatusRecord is a recipient-owned status notice with its linkage.
type WorkflowStatusRecord struct {
	Record
	InstanceID                     uuid.UUID
	Event, EventRef, CorrelationID string
}

// PublishWorkflowStatus must run in the transaction that made the change it
// reports, so a rolled-back advancement leaves no notice. One (run, event,
// event reference, recipient) is one notice however often it is retried. It
// contacts no provider and is not delivery evidence.
func (s Store) PublishWorkflowStatus(ctx context.Context, tx dbport.Tx, notice WorkflowStatusNotice) (Record, error) {
	if notice.TenantID == uuid.Nil || notice.InstanceID == uuid.Nil || notice.SubjectRef == "" || notice.EventRef == "" ||
		notice.CorrelationID == "" || notice.AudienceDigest == "" || notice.CreatedAt.IsZero() ||
		(notice.Event != StatusInReview && notice.Event != StatusFinished) {
		return Record{}, invalid("workflow_status", "tenant, workflow, recipient, event, event reference, resolution and correlation are required")
	}
	if err := tenancy.WithTenant(ctx, tx, notice.TenantID); err != nil {
		return Record{}, err
	}
	id := uuid.NewSHA1(notice.TenantID, []byte("workflow-status/v1\x00"+notice.InstanceID.String()+"\x00"+notice.Event+"\x00"+notice.EventRef+"\x00"+notice.SubjectRef))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id.String()); err != nil {
		return Record{}, err
	}
	if existing, err := s.Load(ctx, tx, notice.TenantID, notice.SubjectRef, id); err == nil {
		intent, err := messagingmeta.LoadMessageIntent(ctx, tx, notice.TenantID, id)
		if err != nil {
			return Record{}, err
		}
		if intent.WorkflowRef != notice.InstanceID.String() || intent.Purpose != workflowStatusPurpose || intent.TemplateKey != workflowStatusTemplate {
			return Record{}, invalid("workflow_status", "retry conflicts with the original notification")
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Record{}, err
	}
	metadata, err := json.Marshal(struct {
		Event      string `json:"event"`
		EventRef   string `json:"event_ref"`
		Recipient  string `json:"recipient"`
		Resolution string `json:"resolution_digest"`
	}{notice.Event, notice.EventRef, notice.SubjectRef, notice.AudienceDigest})
	if err != nil {
		return Record{}, err
	}
	intent := messagingmeta.MessageIntent{TenantID: notice.TenantID, MessageIntentID: id, Purpose: workflowStatusPurpose,
		AudienceExpression: metadata, TemplateKey: workflowStatusTemplate, TemplateVersion: 1,
		Classification: "INTERNAL", Urgency: "NORMAL", DeliveryRequirement: "BEST_EFFORT", ResponseRequirement: "NONE",
		WorkflowRef: notice.InstanceID.String(), CorrelationKey: notice.CorrelationID, CreatedAt: notice.CreatedAt}
	if err := messagingmeta.InsertMessageIntent(ctx, tx, intent); err != nil {
		return Record{}, err
	}
	endpointID, err := statusInboxEndpoint(ctx, tx, notice.TenantID, notice.SubjectRef, notice.CreatedAt)
	if err != nil {
		return Record{}, err
	}
	recipient := messagingmeta.RecipientMessage{TenantID: notice.TenantID, RecipientMessageID: id, MessageIntentID: id,
		RecipientRef: notice.SubjectRef, EndpointID: endpointID, RenderedDigest: noticeDigest(string(metadata)), Classification: "INTERNAL",
		CorrelationKey: notice.CorrelationID, RecipientState: "UNSEEN", SatisfactionState: "PENDING", CreatedAt: notice.CreatedAt, UpdatedAt: notice.CreatedAt}
	if err := messagingmeta.InsertRecipientMessage(ctx, tx, recipient); err != nil {
		return Record{}, err
	}
	record := Record{TenantID: notice.TenantID, InboxRecordID: id, SubjectRef: notice.SubjectRef, RecipientMessageID: id,
		ReadState: Unread, CreatedAt: notice.CreatedAt, StateChangedAt: notice.CreatedAt, Version: 1}
	if err := s.Create(ctx, tx, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

// statusInboxEndpoint returns the recipient's one secure-inbox endpoint,
// creating it on first use. It derives the same identifier PublishWorkflow
// does, so a person who receives both kinds of notice has one endpoint.
func statusInboxEndpoint(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, subject string, at time.Time) (uuid.UUID, error) {
	endpointID := uuid.NewSHA1(tenant, []byte("workflow-inbox/v1\x00"+subject))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, endpointID.String()); err != nil {
		return uuid.Nil, err
	}
	existing, err := messagingmeta.LoadDeliveryEndpoint(ctx, tx, tenant, endpointID)
	switch {
	case errors.Is(err, dbport.ErrNoRows):
		return endpointID, messagingmeta.InsertDeliveryEndpoint(ctx, tx, messagingmeta.DeliveryEndpoint{TenantID: tenant, EndpointID: endpointID,
			PrincipalRef: subject, Channel: "INBOX", AddressDigest: noticeDigest(subject), Ownership: "BUSINESS", VerificationState: "VERIFIED",
			PurposeScope: json.RawMessage(`{}`), Locale: "en-US", EffectiveFrom: at, Status: "ACTIVE"})
	case err != nil:
		return uuid.Nil, err
	case existing.Status != "ACTIVE" || existing.PrincipalRef != subject || existing.Channel != "INBOX":
		return uuid.Nil, invalid("endpoint", "inbox endpoint is not active for recipient")
	}
	return endpointID, nil
}

// WorkflowStatusNotices is a bounded, recipient-scoped read, newest first.
// Callers must recheck current workflow visibility before disclosing a record.
func (s Store) WorkflowStatusNotices(ctx context.Context, ex Executor, tenant uuid.UUID, subject string, limit int) ([]WorkflowStatusRecord, error) {
	if limit < 1 || limit > 100 {
		return nil, invalid("workflow_status", "limit 1..100 required")
	}
	where, args, _, err := pagePredicate(tenant, subject, PageQuery{Limit: limit})
	if err != nil {
		return nil, err
	}
	args = append(args, workflowStatusTemplate, limit)
	rows, err := ex.Query(ctx, `SELECT i.inbox_record_id,i.recipient_message_id,i.read_state,i.version,i.created_at,i.state_changed_at,
		m.audience_expression->>'event',m.audience_expression->>'event_ref',m.workflow_ref,m.correlation_key,i.archived,i.pinned
		FROM inbox_record i JOIN recipient_message r ON r.tenant_id=i.tenant_id AND r.recipient_message_id=i.recipient_message_id
		JOIN message_intent m ON m.tenant_id=r.tenant_id AND m.message_intent_id=r.message_intent_id
		WHERE `+where+fmt.Sprintf(" AND r.recipient_ref=$2 AND m.template_key=$%d ORDER BY i.created_at DESC,i.inbox_record_id DESC LIMIT $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("inbox: list workflow status: %w", err)
	}
	defer rows.Close()
	out := make([]WorkflowStatusRecord, 0, limit)
	for rows.Next() {
		row := WorkflowStatusRecord{Record: Record{TenantID: tenant, SubjectRef: subject}}
		var instance string
		var version int64
		if err := rows.Scan(&row.InboxRecordID, &row.RecipientMessageID, &row.ReadState, &version, &row.CreatedAt, &row.StateChangedAt,
			&row.Event, &row.EventRef, &instance, &row.CorrelationID, &row.Archived, &row.Pinned); err != nil {
			return nil, err
		}
		if row.InstanceID, err = uuid.Parse(instance); err != nil {
			return nil, fmt.Errorf("inbox: invalid stored workflow: %w", err)
		}
		row.Version = uint64(version)
		row.CreatedAt, row.StateChangedAt = row.CreatedAt.UTC(), row.StateChangedAt.UTC()
		out = append(out, row)
	}
	return out, rows.Err()
}
