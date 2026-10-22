package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrAgentUXDemoSupportUnavailable means the support desk is not composed far
// enough to turn an email into a ticket: no project to put it in, or no
// incident channel to tell.
var ErrAgentUXDemoSupportUnavailable = errors.New("application: support desk is not composed")

// AgentUXDemoSupportTargets are the destinations and the identity a ticket is
// made under. They come from server state (the "Customer support" project the
// administrator created, the incident channel, the service identity granted
// "create task" on exactly that project); an email never names any of them
// (AGENTUX-054).
type AgentUXDemoSupportTargets struct {
	TenantID                                   uuid.UUID
	Principal                                  *trust.Principal
	ProjectID, ConversationID, InitialStatusID string
	WorkflowRevision                           uint64
}

// AgentUXDemoSupportTargetSource resolves the current targets for a tenant.
type AgentUXDemoSupportTargetSource interface {
	SupportTargets(context.Context, string) (AgentUXDemoSupportTargets, error)
}

// AgentUXDemoSupportDeskPlanner classifies one stored email into a plan. A
// planner backed by a model must run through the governed gateway with the
// agent's route and budget; the local classifier below needs none.
type AgentUXDemoSupportDeskPlanner interface {
	PlanSupportEmail(ctx context.Context, tenant, message, key string, messages []agentmodel.ModelMessage) ([]byte, error)
}

// AgentUXDemoSupportDeskAlerts tells the incident channel about a ticket, once
// per email: the key is the email's, so a retry never repeats the alert.
type AgentUXDemoSupportDeskAlerts interface {
	PostSupportAlert(ctx context.Context, tenant, conversation string, alert AgentUXDemoSupportAlert, key string) (string, error)
}

// AgentUXDemoSupportResult is what one processed email produced.
type AgentUXDemoSupportResult struct {
	TicketID, AlertPost, Severity string
	NeedsHumanReview, Replayed    bool
}

// AgentUXDemoSupportDesk turns one stored customer email into one ticket and one
// alert. The email is untrusted data: it reaches the planner as quarantined
// text, the plan is decoded strictly and an email that asks for anything else
// is a "Needs human review" ticket and nothing more. Both effects are
// idempotent per email, bounded by the per-day limits and reserved in the
// journal before they happen, so a retry after a failure between them repeats
// neither (AGENTUX-054).
type AgentUXDemoSupportDesk struct {
	Inbox    AgentUXDemoInboxRepository
	Journal  AgentUXDemoSupportJournal
	Objects  AgentUXDemoSupportObjects
	Planner  AgentUXDemoSupportDeskPlanner
	Projects AgentUXDemoSupportProject
	Alerts   AgentUXDemoSupportDeskAlerts
	Targets  AgentUXDemoSupportTargetSource
	Now      func() time.Time
}

func (d *AgentUXDemoSupportDesk) ready() bool {
	return d != nil && d.Inbox != nil && d.Journal != nil && d.Objects != nil && d.Planner != nil && d.Projects != nil && d.Alerts != nil && d.Targets != nil && d.Now != nil
}

// Process handles the email stored under id. Running it again for the same email
// returns the same ticket and alert and creates nothing.
func (d *AgentUXDemoSupportDesk) Process(ctx context.Context, tenant, id string) (AgentUXDemoSupportResult, error) {
	var result AgentUXDemoSupportResult
	if !d.ready() || ctx == nil || tenant == "" || id == "" {
		return result, ErrAgentUXDemoSupportUnavailable
	}
	targets, err := d.Targets.SupportTargets(ctx, tenant)
	if err != nil {
		return result, err
	}
	if targets.TenantID == uuid.Nil || targets.Principal == nil || targets.Principal.Tenant().String() != tenant || targets.Principal.SubjectKind() != trust.SubjectKindService || !targets.Principal.AuthorizesPurpose("customer-support") || !d.Now().Before(targets.Principal.ExpiresAt()) || targets.ProjectID == "" || targets.ConversationID == "" || targets.InitialStatusID == "" || targets.WorkflowRevision == 0 {
		return result, ErrAgentUXDemoSupportDenied
	}
	err = d.Journal.WithSupportRunFence(ctx, targets.TenantID, id, func() error {
		var processErr error
		result, processErr = d.process(ctx, tenant, id, targets)
		return processErr
	})
	return result, err
}

func (d *AgentUXDemoSupportDesk) process(ctx context.Context, tenant, id string, targets AgentUXDemoSupportTargets) (AgentUXDemoSupportResult, error) {
	var result AgentUXDemoSupportResult
	receipt, err := d.Inbox.Get(ctx, targets.TenantID, id)
	if err != nil || receipt.TenantID != targets.TenantID || receipt.MessageID != id {
		return result, ErrAgentUXDemoSupportDenied
	}
	raw, err := d.Objects.ReadSupportObject(ctx, tenant, receipt.ContentRef)
	if err != nil || len(raw) > 70*1024 || personaRunBytesDigest(raw) != receipt.ContentDigest {
		return result, ErrAgentUXDemoSupportDenied
	}
	var email agentdemo.Email
	if json.Unmarshal(raw, &email) != nil {
		return result, ErrAgentUXDemoSupportDenied
	}
	email.IdempotencyKey = "stored-support-email"
	address, err := agentuxDemoValidateEmail(email)
	if err != nil || address.Address != receipt.ClaimedSenderAddress {
		return result, ErrAgentUXDemoSupportDenied
	}
	plan, err := d.plan(ctx, tenant, targets, receipt, email)
	if err != nil {
		return result, err
	}
	ticketID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(tenant+"\x00support-ticket\x00"+id)).String()
	request := projectservice.CreateTaskRequest{ProjectID: targets.ProjectID, ID: ticketID, Title: plan.Title, Description: agentuxDemoSupportDescription(plan, receipt), InitialStatusID: targets.InitialStatusID, Priority: strings.ToUpper(plan.Severity), IdempotencyKey: "support.create_ticket:" + id, ExpectedWorkflowRevision: targets.WorkflowRevision}
	// The reservation counts this email's effect once however often it is
	// retried; a ticket made before a crash is returned by its stable key.
	if err := d.Journal.ReserveSupportEffect(ctx, targets.TenantID, id, agentskills.SupportCreateTicket, d.Now(), agentskills.SupportDailyLimit); err != nil {
		return result, err
	}
	task, err := d.Projects.CreateTask(trust.WithPrincipal(ctx, targets.Principal), targets.Principal, request)
	if err != nil {
		return result, err
	}
	if task.ID != ticketID || task.TenantID != tenant || task.ProjectID != targets.ProjectID || task.Title != plan.Title || task.Priority != request.Priority {
		return result, ErrAgentUXDemoSupportDenied
	}
	alert := AgentUXDemoSupportAlert{Title: plan.Title, Severity: plan.Severity, ClaimedCustomer: receipt.ClaimedSenderName, TicketID: ticketID, TicketHref: "/workspace/app/project?" + url.Values{"project": {targets.ProjectID}, "task": {ticketID}, "view": {"task"}}.Encode(), NeedsHumanReview: plan.NeedsHumanReview}
	if err := d.Journal.ReserveSupportEffect(ctx, targets.TenantID, id, agentskills.SupportAlertChannel, d.Now(), agentskills.SupportDailyLimit); err != nil {
		return result, err
	}
	post, err := d.Alerts.PostSupportAlert(ctx, tenant, targets.ConversationID, alert, "support.alert_channel:"+id)
	if err != nil || post == "" {
		return result, errors.Join(ErrAgentUXDemoSupportDenied, err)
	}
	return AgentUXDemoSupportResult{TicketID: ticketID, AlertPost: post, Severity: plan.Severity, NeedsHumanReview: plan.NeedsHumanReview}, nil
}

// plan returns the stored plan for the email, or asks the planner once and
// stores what it said, so the model is never asked twice about one email.
func (d *AgentUXDemoSupportDesk) plan(ctx context.Context, tenant string, targets AgentUXDemoSupportTargets, receipt agentstore.SupportInboxMessage, email agentdemo.Email) (AgentUXDemoSupportPlan, error) {
	stored, err := d.Journal.GetSupportPlan(ctx, targets.TenantID, receipt.MessageID)
	if err == nil {
		raw, readErr := d.Objects.ReadSupportObject(ctx, tenant, stored.PlanRef)
		if readErr != nil || personaRunBytesDigest(raw) != stored.PlanDigest {
			return AgentUXDemoSupportPlan{}, ErrAgentUXDemoSupportDenied
		}
		return agentuxDemoDecodeSupportPlan(raw, email)
	}
	if !errors.Is(err, agentstore.ErrSupportInboxNotFound) {
		return AgentUXDemoSupportPlan{}, err
	}
	messages, err := AgentUXDemoSupportModelMessages(email)
	if err != nil {
		return AgentUXDemoSupportPlan{}, err
	}
	raw, err := d.Planner.PlanSupportEmail(ctx, tenant, receipt.MessageID, receipt.MessageID+":support-classify:v1", messages)
	if err != nil {
		return AgentUXDemoSupportPlan{}, err
	}
	plan, err := agentuxDemoDecodeSupportPlan(raw, email)
	if err != nil {
		return plan, err
	}
	raw, _ = json.Marshal(plan)
	ref, err := d.Objects.PutSupportPlan(ctx, tenant, receipt.MessageID, raw)
	if err != nil {
		return plan, err
	}
	if err := d.Journal.RecordSupportPlan(ctx, agentstore.SupportPlanReceipt{TenantID: targets.TenantID, MessageID: receipt.MessageID, RunID: receipt.MessageID, PlanRef: ref, PlanDigest: personaRunBytesDigest(raw), RecordedAt: d.Now().UTC()}); err != nil {
		return plan, err
	}
	return plan, nil
}

// AgentUXDemoSupportQueue runs the desk for an email as soon as the owner's
// simulation has stored it. There is no mail server and no other trigger in the
// local cell, so the run is the request's own: replaying a simulation returns
// the same ticket and creates nothing new. A desk that is not composed (no
// project or incident channel yet) leaves the email stored and answers that it is
// unavailable, rather than a receipt that promises a ticket that never comes.
type AgentUXDemoSupportQueue struct{ Desk *AgentUXDemoSupportDesk }

func (q AgentUXDemoSupportQueue) QueueSupportEmail(ctx context.Context, tenant, id string) error {
	if q.Desk == nil || !q.Desk.ready() {
		return ErrAgentUXDemoSupportUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if _, err := q.Desk.Process(ctx, tenant, id); err != nil {
		return fmt.Errorf("support desk: %w", err)
	}
	return nil
}

var _ AgentUXDemoSupportRunQueue = AgentUXDemoSupportQueue{}
