package execute

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

var (
	ErrMessageBeforeEffectivePoint = errors.New("workflow execute: message is before the governed effective point")
	ErrTeamNotificationOrdering    = errors.New("workflow execute: employee notification must precede team notification")
)

// MessageReleaseRequest is the execute-side ordering proof for a message
// effect. It contains only governed timing and ordering evidence; releasing
// the provider-independent MessageIntent remains a separate operation.
type MessageReleaseRequest struct {
	Intent                       messaging.MessageIntent
	EffectivePoint               time.Time
	Now                          time.Time
	EmployeeNotificationRecorded bool
}

// AuthorizeMessageRelease refuses a team notification until the promotion's
// effective point is reached and the employee notification ordering constraint
// has been recorded. It has no provider side effect.
func AuthorizeMessageRelease(req MessageReleaseRequest) error {
	if err := req.Intent.Validate(); err != nil {
		return fmt.Errorf("workflow execute: validate message intent: %w", err)
	}
	if req.EffectivePoint.IsZero() || req.Now.IsZero() {
		return errors.New("workflow execute: message release requires effective point and Now")
	}
	if req.Now.Before(req.EffectivePoint) {
		return ErrMessageBeforeEffectivePoint
	}
	if strings.EqualFold(strings.TrimSpace(req.Intent.AudienceExpression), "TEAM") && !req.EmployeeNotificationRecorded {
		return ErrTeamNotificationOrdering
	}
	return nil
}

// PublishWorkItemMessage resolves the assigned principal at the message gate
// and records the resulting secure inbox notice in the routing transaction.
// The caller already holds the transaction that creates/routes the work item,
// so a failed resolution or persistence rolls back both effects.
func PublishWorkItemMessage(ctx context.Context, tx dbport.Tx, item workitem.WorkItem, resolution audience.Resolution) (retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.execute.publish_work_item_message", item)
	defer func() { observe.Done(op, retErr) }()
	if tx == nil {
		return errors.New("workflow execute: message publication requires a transaction")
	}
	owner := item.Assignment.ChosenOwner
	if owner == "" {
		return errors.New("workflow execute: resolved recipient is required")
	}
	if _, authorized := item.Assignment.Resolution.Authorizes(owner); !authorized {
		return errors.New("workflow execute: owner is outside the resolved assignment")
	}
	principal := workItemAudiencePrincipal(item.TenantID, owner)
	spec := audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{principal}}
	if len(resolution.Principals) != 1 || resolution.Principals[0] != principal ||
		resolution.Expression != spec.Expression() || resolution.ResolvedAt != values.NewInstant(item.RecordedAt) ||
		!validAudienceDigest(resolution.ResultDigest) {
		return errors.New("workflow execute: no authorized recipient")
	}
	purpose := "TASK"
	messagePurpose := messaging.PurposeTaskAssigned
	if item.Kind == workitem.KindApproval {
		purpose = "APPROVAL"
		messagePurpose = messaging.PurposeApprovalRequired
	}
	expiresAt := item.DeadlineAt
	if expiresAt.Before(item.RecordedAt) {
		expiresAt = item.RecordedAt
	}
	intent := messaging.MessageIntent{IntentID: item.WorkItemID.String(), TenantID: item.TenantID.String(),
		OrganizationScope: item.OrganizationScopeID, Purpose: messagePurpose, AudienceExpression: resolution.Expression,
		AudienceResolutionPolicy: "audience.current-route/v1", ContentRef: "workflow.attention",
		ParametersRef: item.WorkItemID.String(), Classification: "INTERNAL", Urgency: "NORMAL",
		DeliveryRequirement: messaging.RequirementBestEffort, ReplyMode: messaging.ReplyNotAllowed,
		WorkflowInstanceID: item.WorkflowInstanceID.String(), HumanTaskID: item.WorkItemID.String(),
		CorrelationID: item.CorrelationID, AvailableAt: &item.RecordedAt, ExpiresAt: expiresAt}
	if err := AuthorizeMessageRelease(MessageReleaseRequest{Intent: intent, EffectivePoint: item.RecordedAt, Now: item.RecordedAt}); err != nil {
		return err
	}
	_, err := (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{
		TenantID: item.TenantID, WorkItemID: item.WorkItemID, InstanceID: item.WorkflowInstanceID,
		SubjectRef: owner, Purpose: purpose, CorrelationID: item.CorrelationID,
		AudienceDigest: resolution.ResultDigest, CreatedAt: item.RecordedAt,
	})
	return err
}

func workItemAudiencePrincipal(tenant uuid.UUID, subject string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant.String()), Kind: "principal", Id: uuid.NewSHA1(tenant, []byte("notification-principal/v1\x00"+subject)).String()}
}

func validAudienceDigest(digest string) bool {
	if len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	return err == nil && len(decoded) == 32
}
