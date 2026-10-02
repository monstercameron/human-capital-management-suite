package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrAgentUXDemoSupportDenied = errors.New("application: support run denied")

const AgentUXDemoSupportPersonaID = "hcmnext.local.persona.support_desk"

type AgentUXDemoSupportPlan struct {
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	Severity         string `json:"severity"`
	NeedsHumanReview bool   `json:"needs_human_review"`
}

// All destinations and identity come from current server grants. A plan has
// no project, conversation, tenant, sender or tool-selection fields.
type AgentUXDemoSupportGrant struct {
	TenantID                                   uuid.UUID
	Principal                                  *trust.Principal
	ProjectID, ConversationID, InitialStatusID string
	WorkflowRevision                           uint64
	TicketPin, AlertPin                        agentskills.SkillPin
}

type AgentUXDemoSupportAuthority interface {
	AuthorizeSupportRun(context.Context, agentrun.Record, runstate.Run) (AgentUXDemoSupportGrant, error)
}
type AgentUXDemoSupportObjects interface {
	ReadSupportObject(context.Context, string, string) ([]byte, error)
	PutSupportPlan(context.Context, string, string, []byte) (string, error)
}
type AgentUXDemoSupportPlanner interface {
	// The adapter uses the admitted run's model route/budget and this stable
	// step key; implementations must not call an ungoverned model directly.
	PlanSupportEmail(context.Context, agentrun.Record, string, []agentmodel.ModelMessage) ([]byte, error)
}
type AgentUXDemoSupportJournal interface {
	WithSupportRunFence(context.Context, uuid.UUID, string, func() error) error
	GetSupportPlan(context.Context, uuid.UUID, string) (agentstore.SupportPlanReceipt, error)
	RecordSupportPlan(context.Context, agentstore.SupportPlanReceipt) error
	ReserveSupportEffect(context.Context, uuid.UUID, string, string, time.Time, uint32) error
}
type AgentUXDemoSupportProject interface {
	CreateTask(context.Context, *trust.Principal, projectservice.CreateTaskRequest) (projectservice.TaskRecord, error)
}
type AgentUXDemoSupportAlerts interface {
	// This port must use sealed public delivery and the current audience rule,
	// with the project link checked for every recipient before committing.
	PostSupportAlert(context.Context, agentrun.Record, AgentUXDemoSupportGrant, AgentUXDemoSupportAlert, string) (string, error)
}
type AgentUXDemoSupportAlert struct {
	Title, Severity, ClaimedCustomer, TicketID, TicketHref string
	NeedsHumanReview                                       bool
}

type AgentUXDemoSupportExecutor struct {
	Authority AgentUXDemoSupportAuthority
	Inbox     AgentUXDemoInboxRepository
	Journal   AgentUXDemoSupportJournal
	Objects   AgentUXDemoSupportObjects
	Planner   AgentUXDemoSupportPlanner
	Projects  AgentUXDemoSupportProject
	Alerts    AgentUXDemoSupportAlerts
	Catalog   *agentskills.Registry
	State     *runstate.Service
	WorkerID  string
	Now       func() time.Time
}

func (e *AgentUXDemoSupportExecutor) Execute(ctx context.Context, admission agentrun.Record, run runstate.Run) (runstate.Run, error) {
	if e == nil || ctx == nil || e.Authority == nil || e.Inbox == nil || e.Journal == nil || e.Objects == nil || e.Planner == nil || e.Projects == nil || e.Alerts == nil || e.Catalog == nil || e.State == nil || e.WorkerID == "" || e.Now == nil {
		return run, ErrAgentUXDemoSupportDenied
	}
	r := admission.Request
	if admission.Decision != agentrun.DecisionAccepted || r.Source.Kind != agentrun.SourceKind(AgentUXDemoSupportSourceKind) || r.Source.Ref == "" || r.Source.Ref != r.CauseID || r.Source.Key == "" || r.Persona == nil || r.Persona.ID != AgentUXDemoSupportPersonaID || r.Principal.Mode != agentrun.ModeSponsored || r.Principal.InvokerID != "" || r.Principal.DelegatedCredentialRef != "" || run.ID != admission.ID || run.AdmissionID != admission.ID || run.TenantID != r.Source.TenantID || run.RequestDigest != admission.RequestDigest || run.ActorID != r.Principal.SponsorID || run.PrincipalMode != agentrun.ModeSponsored || run.AgentID != r.Agent.AgentID || run.AgentVersion != r.Agent.Version || run.AgentDigest != r.Agent.Digest || run.ContextDigest != r.Context.Digest {
		return run, ErrAgentUXDemoSupportDenied
	}
	if run.CancelRequested || run.ExpireRequested || run.FailureRequested || (run.State != runstate.StateReady && run.State != runstate.StateRunning && run.State != runstate.StateReconciling && run.State != runstate.StateCompleted) || (run.State != runstate.StateCompleted && !e.Now().Before(run.Deadline)) {
		return run, ErrAgentUXDemoSupportDenied
	}
	if run.State == runstate.StateRunning && (run.Lease == nil || run.Lease.Owner != e.WorkerID || run.Lease.Fence != run.Fence || !e.Now().Before(run.Lease.Until)) {
		return run, runstate.ErrLease
	}
	grant, err := e.Authority.AuthorizeSupportRun(ctx, admission, run)
	if err != nil || grant.TenantID == uuid.Nil || grant.Principal == nil || grant.Principal.Tenant().String() != run.TenantID || grant.Principal.SubjectKind() != trust.SubjectKindService || grant.Principal.Subject() != r.Principal.SponsorID || !grant.Principal.AuthorizesPurpose("customer-support") || !e.Now().Before(grant.Principal.ExpiresAt()) || grant.ProjectID == "" || grant.ConversationID != r.Audience.ID || grant.InitialStatusID == "" || grant.WorkflowRevision == 0 {
		return run, ErrAgentUXDemoSupportDenied
	}
	if run.State == runstate.StateCompleted {
		if len(run.Effects) != 2 || run.Effects[0].ID != agentskills.SupportCreateTicket+":"+r.Source.Ref || run.Effects[1].ID != agentskills.SupportAlertChannel+":"+r.Source.Ref || run.Effects[0].Status != runstate.EffectApplied || run.Effects[1].Status != runstate.EffectApplied {
			return run, ErrAgentUXDemoSupportDenied
		}
		return run, nil
	}
	err = e.Journal.WithSupportRunFence(ctx, grant.TenantID, r.Source.Ref, func() error {
		var executeErr error
		if run.State == runstate.StateReady {
			run, executeErr = e.State.Claim(ctx, run.ID, e.WorkerID, e.Now(), time.Minute)
			if executeErr != nil {
				return executeErr
			}
		}
		run, executeErr = e.execute(ctx, admission, run, grant)
		if executeErr == nil && run.State == runstate.StateRunning && len(run.Effects) == 2 && run.Effects[0].Status == runstate.EffectApplied && run.Effects[1].Status == runstate.EffectApplied {
			alert := run.Effects[1]
			run, executeErr = e.State.Checkpoint(ctx, run.ID, e.WorkerID, run.Fence, run.Version, runstate.PhaseDelivery, 1, alert.ResultRef, alert.ResultDigest, e.Now())
		}
		return executeErr
	})
	return run, err
}

func (e *AgentUXDemoSupportExecutor) execute(ctx context.Context, admission agentrun.Record, run runstate.Run, grant AgentUXDemoSupportGrant) (runstate.Run, error) {
	id := admission.Request.Source.Ref
	receipt, err := e.Inbox.Get(ctx, grant.TenantID, id)
	if err != nil || receipt.TenantID != grant.TenantID || receipt.MessageID != id {
		return run, ErrAgentUXDemoSupportDenied
	}
	raw, err := e.Objects.ReadSupportObject(ctx, run.TenantID, receipt.ContentRef)
	if err != nil || len(raw) > 70*1024 || personaRunBytesDigest(raw) != receipt.ContentDigest {
		return run, ErrAgentUXDemoSupportDenied
	}
	var email agentdemo.Email
	if json.Unmarshal(raw, &email) != nil {
		return run, ErrAgentUXDemoSupportDenied
	}
	email.IdempotencyKey = "stored-support-email"
	address, err := agentuxDemoValidateEmail(email)
	if err != nil || address.Address != receipt.ClaimedSenderAddress {
		return run, ErrAgentUXDemoSupportDenied
	}
	plan, err := e.plan(ctx, admission, grant, receipt, email)
	if err != nil {
		return run, err
	}
	request := projectservice.CreateTaskRequest{ProjectID: grant.ProjectID, ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(run.TenantID+"\x00support-ticket\x00"+id)).String(), Title: plan.Title, Description: agentuxDemoSupportDescription(plan, receipt), InitialStatusID: grant.InitialStatusID, Priority: strings.ToUpper(plan.Severity), IdempotencyKey: "support.create_ticket:" + id, ExpectedWorkflowRevision: grant.WorkflowRevision}
	args, _ := json.Marshal(request)
	ticketID := request.ID
	run, err = e.effect(ctx, admission, run, grant, grant.TicketPin, agentskills.SupportCreateTicket, args, func() (string, string, error) {
		task, err := e.Projects.CreateTask(trust.WithPrincipal(ctx, grant.Principal), grant.Principal, request)
		if err != nil {
			return "", "", err
		}
		if task.ID != request.ID || task.TenantID != run.TenantID || task.ProjectID != grant.ProjectID || task.Title != plan.Title || task.Priority != request.Priority {
			return "", "", ErrAgentUXDemoSupportDenied
		}
		return task.ID, personaRunBytesDigest(args), nil
	})
	if err != nil {
		return run, err
	}
	alert := AgentUXDemoSupportAlert{Title: plan.Title, Severity: plan.Severity, ClaimedCustomer: receipt.ClaimedSenderName, TicketID: ticketID, TicketHref: "/workspace/app/project?" + url.Values{"project": {grant.ProjectID}, "task": {ticketID}, "view": {"task"}}.Encode(), NeedsHumanReview: plan.NeedsHumanReview}
	args, _ = json.Marshal(alert)
	return e.effect(ctx, admission, run, grant, grant.AlertPin, agentskills.SupportAlertChannel, args, func() (string, string, error) {
		post, err := e.Alerts.PostSupportAlert(ctx, admission, grant, alert, "support.alert_channel:"+id)
		if err != nil || post == "" {
			return "", "", errors.Join(ErrAgentUXDemoSupportDenied, err)
		}
		return post, personaRunBytesDigest(args), nil
	})
}

func (e *AgentUXDemoSupportExecutor) plan(ctx context.Context, admission agentrun.Record, grant AgentUXDemoSupportGrant, emailReceipt agentstore.SupportInboxMessage, email agentdemo.Email) (AgentUXDemoSupportPlan, error) {
	stored, err := e.Journal.GetSupportPlan(ctx, grant.TenantID, emailReceipt.MessageID)
	var raw []byte
	if err == nil {
		if stored.RunID != admission.ID {
			return AgentUXDemoSupportPlan{}, ErrAgentUXDemoSupportDenied
		}
		raw, err = e.Objects.ReadSupportObject(ctx, admission.Request.Source.TenantID, stored.PlanRef)
		if err != nil || personaRunBytesDigest(raw) != stored.PlanDigest {
			return AgentUXDemoSupportPlan{}, ErrAgentUXDemoSupportDenied
		}
	} else if errors.Is(err, agentstore.ErrSupportInboxNotFound) {
		messages, err := AgentUXDemoSupportModelMessages(email)
		if err != nil {
			return AgentUXDemoSupportPlan{}, err
		}
		raw, err = e.Planner.PlanSupportEmail(ctx, admission, admission.ID+":support-classify:v1", messages)
		if err != nil {
			return AgentUXDemoSupportPlan{}, err
		}
		plan, err := agentuxDemoDecodeSupportPlan(raw, email)
		if err != nil {
			return plan, err
		}
		raw, _ = json.Marshal(plan)
		ref, err := e.Objects.PutSupportPlan(ctx, admission.Request.Source.TenantID, admission.ID, raw)
		if err != nil {
			return plan, err
		}
		if err := e.Journal.RecordSupportPlan(ctx, agentstore.SupportPlanReceipt{TenantID: grant.TenantID, MessageID: emailReceipt.MessageID, RunID: admission.ID, PlanRef: ref, PlanDigest: personaRunBytesDigest(raw), RecordedAt: e.Now().UTC()}); err != nil {
			return plan, err
		}
	} else {
		return AgentUXDemoSupportPlan{}, err
	}
	return agentuxDemoDecodeSupportPlan(raw, email)
}

func (e *AgentUXDemoSupportExecutor) effect(ctx context.Context, admission agentrun.Record, run runstate.Run, grant AgentUXDemoSupportGrant, pin agentskills.SkillPin, skill string, args []byte, apply func() (string, string, error)) (runstate.Run, error) {
	current, err := e.Authority.AuthorizeSupportRun(ctx, admission, run)
	if err != nil || current.ProjectID != grant.ProjectID || current.ConversationID != grant.ConversationID || current.InitialStatusID != grant.InitialStatusID || current.WorkflowRevision != grant.WorkflowRevision || current.TicketPin != grant.TicketPin || current.AlertPin != grant.AlertPin || current.Principal == nil || current.Principal.Subject() != grant.Principal.Subject() || current.Principal.SubjectKind() != trust.SubjectKindService || current.Principal.Tenant().String() != run.TenantID || !current.Principal.AuthorizesPurpose("customer-support") || !e.Now().Before(current.Principal.ExpiresAt()) || !e.Now().Before(grant.Principal.ExpiresAt()) || current.TenantID != grant.TenantID {
		return run, ErrAgentUXDemoSupportDenied
	}
	record, err := e.Catalog.ResolvePin(pin)
	if err != nil || pin.ID != skill || record.Status != agentskills.StatusActive {
		return run, ErrAgentUXDemoSupportDenied
	}
	matched := false
	for _, def := range agentskills.SupportEffectSkills() {
		if def.Skill.ID == skill && reflect.DeepEqual(record.Definition, def.Skill) {
			matched = true
		}
	}
	if !matched {
		return run, ErrAgentUXDemoSupportDenied
	}
	id := skill + ":" + admission.Request.Source.Ref
	digest := personaRunBytesDigest(args)
	pending := false
	for _, effect := range run.Effects {
		if effect.ID != id {
			continue
		}
		if effect.ArgumentsDigest != digest || effect.IdempotencyKey != id {
			return run, ErrAgentUXDemoSupportDenied
		}
		if effect.Status == runstate.EffectApplied {
			return run, nil
		}
		if effect.Status != runstate.EffectUnknown {
			return run, ErrAgentUXDemoSupportDenied
		}
		pending = true
	}
	if run.State != runstate.StateReconciling {
		if run.State != runstate.StateRunning || run.Lease == nil || run.Lease.Owner != e.WorkerID || run.Lease.Fence != run.Fence || !e.Now().Before(run.Lease.Until) || run.CancelRequested || run.ExpireRequested || run.FailureRequested {
			return run, runstate.ErrLease
		}
	} else if !pending {
		return run, runstate.ErrInvalid
	}
	if err := e.Journal.ReserveSupportEffect(ctx, grant.TenantID, admission.Request.Source.Ref, skill, e.Now(), agentskills.SupportDailyLimit); err != nil {
		return run, err
	}
	if !pending {
		run, err = e.State.BeginEffect(ctx, run.ID, e.WorkerID, id, id, digest, run.Fence, run.Version, e.Now())
		if err != nil {
			return run, err
		}
	}
	ref, resultDigest, err := apply()
	// A timeout may have happened after commit. Keep UNKNOWN; retry asks the
	// owning service with the same immutable key rather than asserting failure.
	if err != nil {
		return run, err
	}
	if run.State == runstate.StateReconciling {
		run, err = e.State.ReconcileEffect(ctx, run.ID, id, run.Version, runstate.EffectApplied, ref, resultDigest, e.Now())
		if err != nil {
			return run, err
		}
		if run.State == runstate.StateReady {
			return e.State.Claim(ctx, run.ID, e.WorkerID, e.Now(), time.Minute)
		}
		return run, nil
	}
	return e.State.ResolveEffect(ctx, run.ID, e.WorkerID, id, run.Fence, run.Version, runstate.EffectApplied, ref, resultDigest, e.Now())
}

func agentuxDemoDecodeSupportPlan(raw []byte, email agentdemo.Email) (AgentUXDemoSupportPlan, error) {
	var p AgentUXDemoSupportPlan
	if len(raw) == 0 || len(raw) > 8*1024 {
		return p, ErrAgentUXDemoSupportDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || !agentuxDemoSupportPlain(p.Title, 80) || !agentuxDemoSupportPlain(p.Summary, 2000) || agentuxDemoSentenceCount(p.Summary) > 3 {
		return p, ErrAgentUXDemoSupportDenied
	}
	if p.Severity != "Low" && p.Severity != "Normal" && p.Severity != "High" && p.Severity != "Urgent" {
		return p, ErrAgentUXDemoSupportDenied
	}
	text := strings.ToLower(email.Subject + "\n" + email.Body)
	for _, phrase := range []string{"ignore instructions", "ignore previous", "ignore all", "system:", "reveal", "delete ticket", "close ticket", "send data", "email someone", "send salaries", "password", "another tenant", "tenant_id", "ignore les", "ignoriere", "تجاهل"} {
		if strings.Contains(text, phrase) {
			p.NeedsHumanReview = true
		}
	}
	if p.Severity == "Urgent" {
		p.NeedsHumanReview = true
	}
	if p.NeedsHumanReview {
		p.Severity = "Normal"
	}
	return p, nil
}

func agentuxDemoSupportPlain(text string, max int) bool {
	if !utf8.ValidString(text) || text != strings.TrimSpace(text) || text == "" || utf8.RuneCountInString(text) > max || strings.ContainsAny(text, "<>\r\n\x00") {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func agentuxDemoSentenceCount(text string) int {
	n := 0
	inEnd := false
	for _, r := range text {
		end := strings.ContainsRune(".!?。！？؟", r)
		if end && !inEnd {
			n++
		}
		inEnd = end
	}
	if n == 0 {
		return 1
	}
	if !strings.ContainsRune(".!?。！？؟", []rune(strings.TrimSpace(text))[len([]rune(strings.TrimSpace(text)))-1]) {
		n++
	}
	return n
}

func agentuxDemoSupportDescription(plan AgentUXDemoSupportPlan, email agentstore.SupportInboxMessage) string {
	label := ""
	if plan.NeedsHumanReview {
		label = "Needs human review\n\n"
	}
	return fmt.Sprintf("%s%s\n\nCustomer (claimed, unverified): %s <%s>\nSeverity: %s\nStored email reference: %s\nEmail digest: %s", label, plan.Summary, email.ClaimedSenderName, email.ClaimedSenderAddress, plan.Severity, email.ContentRef, email.ContentDigest)
}
