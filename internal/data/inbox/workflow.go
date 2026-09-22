package inbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/messagingmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// WorkflowNotice contains references only. Protected proposal content stays
// behind the workflow's current authorization check, never in message copy.
type WorkflowNotice struct {
	TenantID, WorkItemID, InstanceID                   uuid.UUID
	SubjectRef, Purpose, CorrelationID, AudienceDigest string
	CreatedAt                                          time.Time
}

// WorkflowRecord is a recipient-owned inbox record with its workflow linkage.
type WorkflowRecord struct {
	Record
	WorkItemID, InstanceID uuid.UUID
	Purpose, CorrelationID string
}

func noticeDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// PublishWorkflow must run in the routing transaction. Its deterministic key
// and transaction lock make concurrent/retried publication one logical notice.
// It does not contact a provider or satisfy the approval/delivery obligation.
func (s Store) PublishWorkflow(ctx context.Context, tx dbport.Tx, notice WorkflowNotice) (Record, error) {
	if notice.TenantID == uuid.Nil || notice.WorkItemID == uuid.Nil || notice.InstanceID == uuid.Nil || notice.SubjectRef == "" || notice.CorrelationID == "" || notice.AudienceDigest == "" || notice.CreatedAt.IsZero() || (notice.Purpose != "APPROVAL" && notice.Purpose != "TASK") {
		return Record{}, invalid("workflow_notice", "tenant, workflow, work item, recipient, purpose, resolution and correlation are required")
	}
	if err := tenancy.WithTenant(ctx, tx, notice.TenantID); err != nil {
		return Record{}, err
	}
	id := uuid.NewSHA1(notice.TenantID, []byte("workflow-notice/v1\x00"+notice.WorkItemID.String()+"\x00"+notice.SubjectRef))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id.String()); err != nil {
		return Record{}, err
	}
	if existing, err := s.Load(ctx, tx, notice.TenantID, notice.SubjectRef, id); err == nil {
		intent, err := messagingmeta.LoadMessageIntent(ctx, tx, notice.TenantID, id)
		if err != nil {
			return Record{}, err
		}
		if intent.WorkflowRef != notice.InstanceID.String() || intent.Purpose != notice.Purpose || intent.CorrelationKey != notice.CorrelationID {
			return Record{}, invalid("workflow_notice", "retry conflicts with the original notification")
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Record{}, err
	}
	metadata, err := json.Marshal(struct {
		WorkItemID uuid.UUID `json:"work_item_id"`
		Recipient  string    `json:"recipient"`
		Resolution string    `json:"resolution_digest"`
	}{notice.WorkItemID, notice.SubjectRef, notice.AudienceDigest})
	if err != nil {
		return Record{}, err
	}
	intent := messagingmeta.MessageIntent{TenantID: notice.TenantID, MessageIntentID: id, Purpose: notice.Purpose,
		AudienceExpression: metadata, TemplateKey: "workflow.attention", TemplateVersion: 1,
		Classification: "INTERNAL", Urgency: "NORMAL", DeliveryRequirement: "BEST_EFFORT", ResponseRequirement: "NONE",
		WorkflowRef: notice.InstanceID.String(), CorrelationKey: notice.CorrelationID, CreatedAt: notice.CreatedAt}
	if err := messagingmeta.InsertMessageIntent(ctx, tx, intent); err != nil {
		return Record{}, err
	}
	// A principal has one secure-inbox endpoint, separate from all external
	// addresses. Serialize its first creation across different work items.
	endpointID := uuid.NewSHA1(notice.TenantID, []byte("workflow-inbox/v1\x00"+notice.SubjectRef))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, endpointID.String()); err != nil {
		return Record{}, err
	}
	endpoint := messagingmeta.DeliveryEndpoint{TenantID: notice.TenantID, EndpointID: endpointID, PrincipalRef: notice.SubjectRef,
		Channel: "INBOX", AddressDigest: noticeDigest(notice.SubjectRef), Ownership: "BUSINESS", VerificationState: "VERIFIED",
		PurposeScope: json.RawMessage(`{}`), Locale: "en-US", EffectiveFrom: notice.CreatedAt, Status: "ACTIVE"}
	if existing, err := messagingmeta.LoadDeliveryEndpoint(ctx, tx, notice.TenantID, endpointID); errors.Is(err, dbport.ErrNoRows) {
		if err := messagingmeta.InsertDeliveryEndpoint(ctx, tx, endpoint); err != nil {
			return Record{}, err
		}
	} else if err != nil {
		return Record{}, err
	} else if existing.Status != "ACTIVE" || existing.PrincipalRef != notice.SubjectRef || existing.Channel != "INBOX" {
		return Record{}, invalid("endpoint", "inbox endpoint is not active for recipient")
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

// WorkflowNotices is a bounded, subject-scoped read. Callers must additionally
// recheck current workflow visibility before disclosing or linking a record.
func (s Store) WorkflowNotices(ctx context.Context, ex Executor, tenant uuid.UUID, subject string, limit int) ([]WorkflowRecord, error) {
	if tenant == uuid.Nil || subject == "" || limit < 1 || limit > 100 {
		return nil, invalid("workflow_notices", "tenant, subject and limit 1..100 required")
	}
	page, err := s.WorkflowNoticesPage(ctx, ex, tenant, subject, WorkflowPageQuery{PageQuery: PageQuery{Limit: limit}})
	return page.Records, err
}
