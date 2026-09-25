package application

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectrefs"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/workorderservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workorderstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workorderflow "github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

type composedWorkOrders struct {
	store      *workorderstore.Store
	service    *workorderservice.Service
	projection projectrefs.WorkOrderProjection
	close      func()
}

func composeWorkOrders(ctx context.Context, cfg ServeConfig, projects composedProjects, workers people.WorkerFacts, invitees projectservice.InviteeEligibility, pool *pgxadapter.Pool, now func() time.Time) (composedWorkOrders, error) {
	if strings.TrimSpace(cfg.WorkOrderDatabaseURL) == "" {
		return composedWorkOrders{}, nil
	}
	if projects.store == nil || projects.members == nil {
		return composedWorkOrders{}, fmt.Errorf("application: -%s requires project services for current membership authorization", FieldWorkOrderDatabaseURL)
	}
	if workers == nil || invitees == nil || pool == nil {
		return composedWorkOrders{}, fmt.Errorf("application: -%s requires the HCM worker directory and current employee standing", FieldWorkOrderDatabaseURL)
	}
	store, err := workorderstore.New(ctx, workorderstore.Config{DSN: cfg.WorkOrderDatabaseURL, CoreDSN: cfg.DatabaseURL, Schema: WorkOrderSchemaName, MaxConns: 8, MinConns: 1})
	if err != nil {
		return composedWorkOrders{}, fmt.Errorf("open work order database: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	templates := workOrderTemplateResolver{store: store}
	workerDirectory := hcmWorkOrderWorkerDirectory{Facts: workers, Pool: pool, Now: now}
	authorizer := workOrderAuthorizer{Projects: projects.members, Orders: store, Standing: invitees, Workers: workerDirectory}
	var cursorKey [32]byte
	if _, err := rand.Read(cursorKey[:]); err != nil {
		store.Close()
		return composedWorkOrders{}, fmt.Errorf("create work order cursor key: %w", err)
	}
	service := &workorderservice.Service{
		Auth: authorizer, Templates: templates, Workers: workerDirectory,
		Orders: store, Phases: workOrderTemplatePhaseGate{Templates: templates}, Clock: now, CursorKey: cursorKey[:],
		Reports:   NewSnapshotWorkOrderReportEngine(templates, store),
		Artifacts: workOrderArtifactAdapter{store: store},
	}
	return composedWorkOrders{
		store: store, service: service, close: store.Close,
		projection: workOrderLinkProjection{service: service},
	}, nil
}

type workOrderTemplateReader interface {
	GetPublishedTemplate(context.Context, string, string, string) (json.RawMessage, string, error)
}

type workOrderTemplateResolver struct{ store workOrderTemplateReader }

func (r workOrderTemplateResolver) ResolvePublished(ctx context.Context, tenant, id, version string) (workordertemplate.Published, error) {
	if r.store == nil || tenant == "" || id == "" || version == "" {
		return workordertemplate.Published{}, workorderservice.ErrUnavailable
	}
	raw, digest, err := r.store.GetPublishedTemplate(ctx, tenant, id, version)
	if err != nil {
		return workordertemplate.Published{}, err
	}
	return workordertemplate.RestorePublished(raw, digest)
}

type workOrderProjectReader interface {
	GetSnapshot(context.Context, string, string) (projectmemberstore.Snapshot, error)
}

type workOrderOrderReader interface {
	Get(context.Context, string, string) (workorder.Snapshot, error)
}

type workOrderAuthorizer struct {
	Projects workOrderProjectReader
	Orders   workOrderOrderReader
	Standing projectservice.InviteeEligibility
	Workers  workorderservice.WorkerDirectory
}

var _ workorderservice.Authorizer = workOrderAuthorizer{}

func (a workOrderAuthorizer) Authorize(ctx context.Context, principal *trust.Principal, projectID, orderID string, capability workorderaccess.Capability) error {
	if err := a.authorizeStanding(ctx, principal); err != nil {
		return err
	}
	if a.Orders == nil || a.Projects == nil || projectID == "" || orderID == "" {
		return workorderservice.ErrUnavailable
	}
	order, err := a.Orders.Get(ctx, principal.Tenant().String(), orderID)
	if err != nil {
		return projectaccess.ErrUnauthorized
	}
	if order.ID != orderID || order.TenantID != principal.Tenant().String() || order.ProjectID != projectID {
		return projectaccess.ErrUnauthorized
	}
	project, err := a.project(ctx, principal.Tenant().String(), projectID)
	if err != nil {
		return err
	}
	decision := workorderaccess.Authorize(workorderaccess.WorkOrder{
		Tenant: workorderaccess.TenantID(order.TenantID), Project: workorderaccess.ProjectID(order.ProjectID),
		ID: workorderaccess.WorkOrderID(order.ID), InitiatorID: workorderaccess.UserID(order.InitiatorID),
	}, project, workorderaccess.UserID(principal.Subject()), workorderaccess.TenantID(principal.Tenant().String()), capability)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", workorderaccess.ErrUnauthorized, decision.Reason)
	}
	return nil
}

func (a workOrderAuthorizer) AuthorizeCreate(ctx context.Context, principal *trust.Principal, projectID string) error {
	if err := a.authorizeStanding(ctx, principal); err != nil {
		return err
	}
	project, err := a.project(ctx, principal.Tenant().String(), projectID)
	if err != nil {
		return err
	}
	decision := projectaccess.Authorize(project, projectaccess.UserID(principal.Subject()), projectaccess.TenantID(principal.Tenant().String()), projectaccess.CreateTask, nil)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", workorderaccess.ErrUnauthorized, decision.Reason)
	}
	return nil
}

func (a workOrderAuthorizer) AuthorizeList(ctx context.Context, principal *trust.Principal, projectID string) error {
	if err := a.authorizeStanding(ctx, principal); err != nil {
		return err
	}
	project, err := a.project(ctx, principal.Tenant().String(), projectID)
	if err != nil {
		return err
	}
	decision := projectaccess.Authorize(project, projectaccess.UserID(principal.Subject()), projectaccess.TenantID(principal.Tenant().String()), projectaccess.ReadProject, nil)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", workorderaccess.ErrUnauthorized, decision.Reason)
	}
	return nil
}

func (a workOrderAuthorizer) AuthorizeWorkEntry(ctx context.Context, principal *trust.Principal, projectID, orderID, workerID string) error {
	if err := a.authorizeStanding(ctx, principal); err != nil {
		return err
	}
	if a.Orders == nil || a.Workers == nil || projectID == "" || orderID == "" || workerID == "" {
		return workorderservice.ErrUnavailable
	}
	order, err := a.Orders.Get(ctx, principal.Tenant().String(), orderID)
	if err != nil || order.ID != orderID || order.TenantID != principal.Tenant().String() || order.ProjectID != projectID {
		return workorderaccess.ErrUnauthorized
	}
	targetID, found, err := a.Workers.ResolveWorker(ctx, principal.Tenant().String(), workerID)
	if err != nil {
		return err
	}
	if !found {
		return workorderaccess.ErrWorkerIneligible
	}
	eligible, err := a.Workers.ResolveEligible(ctx, principal.Tenant().String(), workerID)
	if err != nil {
		return err
	}
	if !eligible {
		return workorderaccess.ErrWorkerIneligible
	}
	assigned := false
	for _, assignment := range order.Assignments {
		assignedID, exists, lookupErr := a.Workers.ResolveWorker(ctx, principal.Tenant().String(), assignment.WorkerID)
		if lookupErr != nil {
			return lookupErr
		}
		if exists && assignedID == targetID {
			assigned = true
			break
		}
	}
	if !assigned {
		return workorderaccess.ErrUnauthorized
	}
	actorID, actorFound, err := a.Workers.ResolveWorker(ctx, principal.Tenant().String(), principal.Subject())
	if err != nil {
		return err
	}
	if actorFound && actorID == targetID {
		return a.Authorize(ctx, principal, projectID, orderID, workorderaccess.Request)
	}
	// A current project manager may enter time for an assigned worker when
	// acting as the accountable foreman. Contributors cannot write for others.
	return a.Authorize(ctx, principal, projectID, orderID, workorderaccess.RecordCost)
}

func (a workOrderAuthorizer) authorizeStanding(ctx context.Context, principal *trust.Principal) error {
	if principal == nil || principal.Subject() == "" || principal.Tenant().String() == "" || a.Standing == nil {
		return workorderservice.ErrUnavailable
	}
	_, err := a.Standing.CheckInvitee(ctx, principal, principal.Subject())
	return err
}

func (a workOrderAuthorizer) project(ctx context.Context, tenant, projectID string) (projectaccess.Project, error) {
	if a.Projects == nil || tenant == "" || projectID == "" {
		return projectaccess.Project{}, workorderservice.ErrUnavailable
	}
	snapshot, err := a.Projects.GetSnapshot(ctx, tenant, projectID)
	if err != nil {
		return projectaccess.Project{}, err
	}
	project := projectaccess.Project{Tenant: projectaccess.TenantID(snapshot.TenantID), ID: projectaccess.ProjectID(snapshot.ProjectID), Memberships: make([]projectaccess.Membership, 0, len(snapshot.Members))}
	for _, member := range snapshot.Members {
		project.Memberships = append(project.Memberships, projectaccess.Membership{
			Tenant: projectaccess.TenantID(member.TenantID), User: projectaccess.UserID(member.UserID), Role: member.Role, State: member.State,
		})
	}
	return project, nil
}

type hcmWorkOrderWorkerDirectory struct {
	Facts people.WorkerFacts
	Pool  *pgxadapter.Pool
	Now   func() time.Time
}

var _ workorderservice.WorkerDirectory = hcmWorkOrderWorkerDirectory{}

func (d hcmWorkOrderWorkerDirectory) ResolveEligible(ctx context.Context, tenant, workerID string) (bool, error) {
	if d.Facts == nil || d.Now == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(workerID) == "" {
		return false, workorderservice.ErrUnavailable
	}
	at := d.Now().UTC()
	date, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return false, err
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		return false, err
	}
	resolvedID, found, err := d.ResolveWorker(ctx, tenant, workerID)
	if err != nil || !found {
		return false, err
	}
	query := people.FactQuery{
		Tenant: values.TenantId(tenant), Worker: values.EntityRef{Tenant: values.TenantId(tenant), Kind: people.KindWorker, Id: resolvedID},
		AsOf: people.AsOf{EffectiveOn: date, KnownAt: knownAt}, Fields: []people.FieldID{people.FieldLifecycleStatus},
	}
	if err := query.Validate(); err != nil {
		return false, fmt.Errorf("invalid HCM worker reference: %w", err)
	}
	facts, err := d.Facts.WorkerFactsAt(ctx, query)
	if err != nil {
		return false, err
	}
	if err := facts.Validate(); err != nil {
		return false, err
	}
	if !facts.Exists || facts.Worker != query.Worker {
		return false, nil
	}
	fact, found := facts.Lookup(people.FieldLifecycleStatus)
	if !found {
		return false, nil
	}
	status, ok := fact.Value.Get()
	return ok && strings.EqualFold(strings.TrimSpace(status), "active"), nil
}

func (d hcmWorkOrderWorkerDirectory) ResolveWorker(ctx context.Context, tenant, workerRef string) (string, bool, error) {
	if d.Pool == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(workerRef) == "" {
		return "", false, workorderservice.ErrUnavailable
	}
	tenantID := pgstore.TenantID(tenant)
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return "", false, err
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, workerRef)
	if err != nil || !found {
		return "", found, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return row.WorkerID.String(), true, nil
}

type workOrderArtifactAdapter struct {
	store interface {
		GetArtifact(context.Context, string, string, string, string, string) (workorderstore.ArtifactRecord, error)
		RecordArtifact(context.Context, string, string, string, string, string, uint64, string, string, []byte) (workorderstore.ArtifactRecord, error)
	}
}

func (a workOrderArtifactAdapter) GetArtifact(ctx context.Context, tenant, orderID, actor, key, kind string) (workorderservice.ArtifactRecord, error) {
	if a.store == nil {
		return workorderservice.ArtifactRecord{}, workorderservice.ErrArtifactUnavailable
	}
	record, err := a.store.GetArtifact(ctx, tenant, orderID, actor, key, kind)
	if errors.Is(err, workorderstore.ErrNotFound) {
		return workorderservice.ArtifactRecord{}, workorderservice.ErrArtifactNotFound
	}
	if err != nil {
		return workorderservice.ArtifactRecord{}, err
	}
	return workOrderArtifactRecord(record), nil
}

func (a workOrderArtifactAdapter) RecordArtifact(ctx context.Context, tenant, orderID, projectID, actor, key string, revision uint64, kind, digest string, payload []byte) (workorderservice.ArtifactRecord, error) {
	if a.store == nil {
		return workorderservice.ArtifactRecord{}, workorderservice.ErrArtifactUnavailable
	}
	record, err := a.store.RecordArtifact(ctx, tenant, orderID, projectID, actor, key, revision, kind, digest, payload)
	if err != nil {
		return workorderservice.ArtifactRecord{}, err
	}
	return workOrderArtifactRecord(record), nil
}

func workOrderArtifactRecord(record workorderstore.ArtifactRecord) workorderservice.ArtifactRecord {
	return workorderservice.ArtifactRecord{
		ID: record.ID, TenantID: record.TenantID, WorkOrderID: record.WorkOrderID, ProjectID: record.ProjectID,
		Kind: record.Kind, ActorID: record.ActorID, IdempotencyKey: record.IdempotencyKey,
		CommandDigest: record.CommandDigest, SourceRevision: record.SourceRevision,
		Payload: append([]byte(nil), record.Payload...), CreatedAt: record.CreatedAt,
	}
}

type workOrderTemplatePhaseGate struct{ Templates workorderservice.Templates }

var _ workorderservice.PhaseGate = workOrderTemplatePhaseGate{}

func (g workOrderTemplatePhaseGate) EvaluateTransition(ctx context.Context, principal *trust.Principal, order workorder.Snapshot, input workorder.TransitionInput) error {
	if g.Templates == nil || principal == nil {
		return workorderservice.ErrUnavailable
	}
	template, err := g.Templates.ResolvePublished(ctx, order.TenantID, order.TemplateID, order.TemplateVersion)
	if err != nil {
		return err
	}
	facts := workorderflow.GateFacts{
		WorkOrderID: order.ID, OrderRevision: order.Revision, TemplateDigest: order.TemplateDigest,
		WorkflowVersion: order.WorkflowVersion, WorkflowDigest: order.WorkflowDigest,
		WorkflowInstanceID: order.WorkflowInstanceID,
	}
	for _, request := range order.Requests {
		if request.DefinitionID != "" && request.Status == workorder.RequestApproved {
			facts.Decisions = append(facts.Decisions, workorderflow.DecisionFact{ID: request.DefinitionID, Satisfied: true})
		}
	}
	// Evidence and runtime-completion facts remain empty until their owning
	// authorities are composed. The evaluator fails closed for missing facts;
	// client EvidenceRefs are never treated as verified evidence.
	decision, err := workorderflow.EvaluateTransition(template, order, input.Target, facts)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: governed transition blocked by %d requirement(s)", workorder.ErrTransition, len(decision.Blockers))
	}
	return nil
}

type workOrderLinkProjection struct{ service *workorderservice.Service }

var _ projectrefs.WorkOrderProjection = workOrderLinkProjection{}

func (p workOrderLinkProjection) ReadAuthorizedWorkOrder(ctx context.Context, principal *trust.Principal, id string) (projectlink.Preview, bool, error) {
	if p.service == nil || principal == nil || id == "" {
		return projectlink.Preview{}, false, nil
	}
	order, err := p.service.Get(ctx, principal, workorderservice.ScopedRequest{WorkOrderID: id})
	if err != nil {
		if errors.Is(err, workorderservice.ErrUnavailable) {
			return projectlink.Preview{}, false, err
		}
		return projectlink.Preview{}, false, nil
	}
	return projectlink.Preview{Kind: projectlink.WorkOrder, ID: order.ID}, true, nil
}
