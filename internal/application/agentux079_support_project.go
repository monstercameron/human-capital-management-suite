package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectviewstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The support desk's well-known project and service identity (AGENTUX-079).
// The project is created by preparation; the identity is minted in-process by
// the target source and its only authority is the support grant ledger row
// preparation records.
const (
	AgentUX079SupportProjectID      = "customer-support"
	AgentUX079SupportProjectName    = "Customer support"
	AgentUX079SupportServiceSubject = "local.support-desk.v1"
)

// AgentUX079SupportProjectReasonUnconfigured is the plain-words reason the
// preparation records when there is no project database to create the
// "Customer support" project in.
const AgentUX079SupportProjectReasonUnconfigured = "the project database is not configured"

// ErrAgentUX079SupportProjectUnavailable means the desk has no project to put
// a ticket in because the cell was composed without a project database.
var ErrAgentUX079SupportProjectUnavailable = errors.New("application: Customer support is not set up yet (" + AgentUX079SupportProjectReasonUnconfigured + ")")

// AgentUX079SupportProjectReceipt is what one preparation run did about the
// support desk's project and grant. Checked tells a run that found everything
// prepared from a run that never reached the step; Reason carries the
// plain-words state when nothing was done.
type AgentUX079SupportProjectReceipt struct {
	Checked, Created, GrantRecorded bool
	Reason                          string
}

// AgentUX079SupportProjectProvisioner creates the support project. It is
// satisfied by the project command adapter; preparation passes nil when the
// project database is not configured.
type AgentUX079SupportProjectProvisioner interface {
	CreateProject(context.Context, projectservice.ProjectRecord, string, string) (projectservice.ProjectRecord, error)
	GetProject(context.Context, string, string) (projectservice.ProjectRecord, error)
}

// AgentUX079SupportGrantLedger records and reads the service grant that lets
// Support Desk create a ticket on exactly the support project. It is
// satisfied by the agent support inbox store.
type AgentUX079SupportGrantLedger interface {
	SupportProjectCreateAllowed(context.Context, uuid.UUID, string, string) (bool, error)
	AppendSupportProjectGrant(context.Context, agentstore.SupportProjectGrant) error
}

// AgentUX079SupportProjectDeps are the ports preparation ensures the support
// project through. A nil Projects is the normal "not configured" state, not
// an error.
type AgentUX079SupportProjectDeps struct {
	Projects   AgentUX079SupportProjectProvisioner
	Grants     AgentUX079SupportGrantLedger
	TenantUUID func(values.TenantId) uuid.UUID
	Tenant     string
	Admin      string
	Now        func() time.Time
}

// prepareAgentUX079SupportProject opens the project database the command
// configured and ensures the support project through it. With no project
// database configured it records the plain reason and opens nothing.
func prepareAgentUX079SupportProject(ctx context.Context, config LocalAgentDemoConfig, agents *agentstore.Store, mapper func(values.TenantId) uuid.UUID, now time.Time) (AgentUX079SupportProjectReceipt, error) {
	var receipt AgentUX079SupportProjectReceipt
	if strings.TrimSpace(config.ProjectDatabaseURL) == "" {
		receipt.Checked = true
		receipt.Reason = AgentUX079SupportProjectReasonUnconfigured
		return receipt, nil
	}
	if ctx == nil || agents == nil || mapper == nil {
		return receipt, ErrLocalDevPersonaChatBootstrap
	}
	store, err := projectstore.New(ctx, projectstore.Config{DSN: config.ProjectDatabaseURL, CoreDSN: config.DatabaseURL, Schema: ProjectSchemaName, MaxConns: 4, MinConns: 1})
	if err != nil {
		return receipt, fmt.Errorf("open project database: %w", err)
	}
	defer store.Close()
	inbox, err := agentstore.NewSupportInboxStore(agents)
	if err != nil {
		return receipt, err
	}
	return ensureAgentUX079SupportProject(ctx, AgentUX079SupportProjectDeps{
		Projects:   &projectservice.StoreAdapter{Projects: store, Configs: projectconfigstore.New(store), Views: projectviewstore.New(store)},
		Grants:     inbox,
		TenantUUID: mapper, Tenant: config.Tenant, Admin: localAgentDemoAdmin,
		Now: func() time.Time { return now },
	})
}

// ensureAgentUX079SupportProject creates the "Customer support" project and
// its service grant when the project database is configured, and says so
// plainly when it is not. Every step is idempotent: a second run, or two
// runs racing each other, creates nothing twice.
func ensureAgentUX079SupportProject(ctx context.Context, deps AgentUX079SupportProjectDeps) (AgentUX079SupportProjectReceipt, error) {
	var receipt AgentUX079SupportProjectReceipt
	if deps.Projects == nil {
		receipt.Checked = true
		receipt.Reason = AgentUX079SupportProjectReasonUnconfigured
		return receipt, nil
	}
	if ctx == nil || deps.Grants == nil || deps.TenantUUID == nil || deps.Now == nil || deps.Tenant == "" || deps.Admin == "" {
		return receipt, ErrLocalDevPersonaChatBootstrap
	}
	tenantID := deps.TenantUUID(values.TenantId(deps.Tenant))
	if tenantID == uuid.Nil {
		return receipt, ErrLocalDevPersonaChatBootstrap
	}
	stored, err := deps.Projects.GetProject(ctx, deps.Tenant, AgentUX079SupportProjectID)
	if err != nil {
		created, createErr := deps.Projects.CreateProject(ctx, projectservice.ProjectRecord{ID: AgentUX079SupportProjectID, TenantID: deps.Tenant, OwnerID: deps.Admin, Name: AgentUX079SupportProjectName, Timezone: "UTC", State: project.LifecycleActive}, deps.Admin, "agentux079:support-project:v1")
		if createErr != nil {
			// A concurrent preparation may have won the create; the project
			// being there now is success, anything else is the real error.
			stored, err = deps.Projects.GetProject(ctx, deps.Tenant, AgentUX079SupportProjectID)
			if err != nil {
				return receipt, createErr
			}
		} else {
			stored = created
			receipt.Created = true
		}
	}
	if stored.TenantID != deps.Tenant {
		return receipt, ErrAgentUXDemoSupportDenied
	}
	allowed, err := deps.Grants.SupportProjectCreateAllowed(ctx, tenantID, AgentUX079SupportServiceSubject, AgentUX079SupportProjectID)
	if err != nil {
		return receipt, err
	}
	if allowed {
		receipt.Checked = true
		return receipt, nil
	}
	now := deps.Now().UTC()
	if now.IsZero() {
		return receipt, ErrLocalDevPersonaChatBootstrap
	}
	if err := deps.Grants.AppendSupportProjectGrant(ctx, agentstore.SupportProjectGrant{TenantID: tenantID, ServiceSubject: AgentUX079SupportServiceSubject, ProjectID: AgentUX079SupportProjectID, Revision: 1, Active: true, GrantedBy: deps.Admin, RecordedAt: now}); err != nil {
		if errors.Is(err, agentstore.ErrSupportInboxReplay) {
			allowed, checkErr := deps.Grants.SupportProjectCreateAllowed(ctx, tenantID, AgentUX079SupportServiceSubject, AgentUX079SupportProjectID)
			if checkErr == nil && allowed {
				receipt.Checked = true
				return receipt, nil
			}
		}
		return receipt, err
	}
	receipt.Checked = true
	receipt.GrantRecorded = true
	return receipt, nil
}

// AgentUX079SupportProjectInfo is the served project's current identity: the
// project, the status a new ticket starts in, and the workflow version the
// ticket is created against.
type AgentUX079SupportProjectInfo struct {
	ProjectID, InitialStatusID string
	WorkflowRevision           uint64
}

// AgentUX079SupportProjectResolver reads the support project's current state.
// The desk resolves targets per email, so a republished workflow is picked up
// without a restart.
type AgentUX079SupportProjectResolver interface {
	ResolveSupportProject(context.Context, string) (AgentUX079SupportProjectInfo, error)
}

// AgentUX079ProjectServiceResolver resolves the support project from the
// served project service's reads and published workflow.
type AgentUX079ProjectServiceResolver struct {
	Reads     projectservice.ReadRepository
	Workflows projectservice.WorkflowPort
}

// ResolveSupportProject returns the "Customer support" project's current
// ticket entry point: its first task type's initial status and the published
// workflow version.
func (r AgentUX079ProjectServiceResolver) ResolveSupportProject(ctx context.Context, tenant string) (AgentUX079SupportProjectInfo, error) {
	var info AgentUX079SupportProjectInfo
	if r.Reads == nil || r.Workflows == nil || ctx == nil || tenant == "" {
		return info, ErrAgentUXDemoSupportDenied
	}
	stored, err := r.Reads.GetProject(ctx, tenant, AgentUX079SupportProjectID)
	if err != nil || stored.TenantID != tenant {
		return info, ErrAgentUXDemoSupportDenied
	}
	published, err := r.Workflows.GetPublished(ctx, tenant, AgentUX079SupportProjectID)
	if err != nil || published.Version == 0 || len(published.Config.TaskTypes) == 0 || published.Config.TaskTypes[0].InitialStatus == "" {
		return info, ErrAgentUXDemoSupportDenied
	}
	return AgentUX079SupportProjectInfo{ProjectID: AgentUX079SupportProjectID, InitialStatusID: published.Config.TaskTypes[0].InitialStatus, WorkflowRevision: published.Version}, nil
}

// AgentUX079SupportTargets is the production target source for the support
// desk: the prepared project and incident channel, the current grant check,
// and a fresh short-lived service identity. An email never names any of them.
type AgentUX079SupportTargets struct {
	TenantUUID func(values.TenantId) uuid.UUID
	Projects   AgentUX079SupportProjectResolver
	Grants     AgentUXDemoSupportProjectGrants
	Now        func() time.Time
}

// SupportTargets resolves one tenant's ticket destinations. Without a project
// database it answers plainly; anything else missing or revoked is denied.
func (s AgentUX079SupportTargets) SupportTargets(ctx context.Context, tenant string) (AgentUXDemoSupportTargets, error) {
	var targets AgentUXDemoSupportTargets
	if s.Projects == nil {
		return targets, ErrAgentUX079SupportProjectUnavailable
	}
	if ctx == nil || s.Grants == nil || s.TenantUUID == nil || s.Now == nil || tenant != localAgentDemoTenant {
		return targets, ErrAgentUXDemoSupportDenied
	}
	info, err := s.Projects.ResolveSupportProject(ctx, tenant)
	if err != nil {
		return targets, ErrAgentUXDemoSupportDenied
	}
	if info.ProjectID != AgentUX079SupportProjectID || info.InitialStatusID == "" || info.WorkflowRevision == 0 {
		return targets, ErrAgentUXDemoSupportDenied
	}
	tenantID := s.TenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return targets, ErrAgentUXDemoSupportDenied
	}
	allowed, err := s.Grants.SupportProjectCreateAllowed(ctx, tenantID, AgentUX079SupportServiceSubject, AgentUX079SupportProjectID)
	if err != nil || !allowed {
		return targets, ErrAgentUXDemoSupportDenied
	}
	now := s.Now()
	if now.IsZero() {
		return targets, ErrAgentUXDemoSupportDenied
	}
	// The local cell mints this identity in-process, fresh per email with a
	// one-hour life; its authority is the grant ledger row checked above, not
	// this envelope.
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: AgentUX079SupportServiceSubject, SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodMutualTLS, Assurance: trust.AssuranceSubstantial, SessionRef: "support-desk", Purposes: []string{"customer-support"}, IssuedAt: now, ExpiresAt: now.Add(time.Hour), CredentialDigest: "local-dev:" + tenant + ":" + AgentUX079SupportServiceSubject})
	if err != nil {
		return targets, ErrAgentUXDemoSupportDenied
	}
	return AgentUXDemoSupportTargets{TenantID: tenantID, Principal: principal, ProjectID: info.ProjectID, ConversationID: localDevPersonaDemoConversationID(tenant, localAgentDemoSupportRoom), InitialStatusID: info.InitialStatusID, WorkflowRevision: info.WorkflowRevision}, nil
}

var _ AgentUXDemoSupportTargetSource = AgentUX079SupportTargets{}

// AgentUX079SupportDeskPorts builds the served desk's project ports from the
// composed project service and the agent store: a ticket service whose
// authorizer carries the support grant exception for exactly the support
// project, and the target source that resolves the prepared project. A cell
// missing either database gets nil ports, which is the desk's
// composed-but-unavailable state rather than a promised ticket.
func AgentUX079SupportDeskPorts(service projectservice.Service, adapter *projectservice.StoreAdapter, agents *agentstore.Store, mapper func(values.TenantId) uuid.UUID, now func() time.Time) (AgentUXDemoSupportProject, AgentUXDemoSupportTargetSource) {
	if adapter == nil || agents == nil || mapper == nil || now == nil || service.Auth == nil {
		return nil, nil
	}
	inbox, err := agentstore.NewSupportInboxStore(agents)
	if err != nil {
		return nil, nil
	}
	service.Auth = AgentUXDemoSupportProjectAuthorizer{Human: service.Auth, Grants: inbox, TenantUUID: mapper}
	return service, AgentUX079SupportTargets{TenantUUID: mapper, Projects: AgentUX079ProjectServiceResolver{Reads: adapter, Workflows: adapter}, Grants: inbox, Now: now}
}
