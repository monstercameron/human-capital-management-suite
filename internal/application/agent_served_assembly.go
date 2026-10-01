package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
)

// agentServedAssemblyInput uses the same owners as the served cell. A missing
// optional background policy keeps native triggers disabled; deployments that
// require them must opt into the startup check instead of accepting READY work
// without their current model authority.
type agentServedAssemblyInput struct {
	Core                   *pgxadapter.Pool
	AgentDatabase          composedAgentDatabase
	Cell                   *app.Cell
	Personas               *personaServeWiring
	Audience               *PersonaPublicAudienceAuthority
	Models                 CommonAgentModelPolicyAuthority
	ActionAuthority        AgentActionAuthority
	ActionAuthorityFactory func(*CommonAgentRuntime) (AgentActionAuthority, error)
	WorkerFactory          func(*CommonAgentRuntime, *AgentScheduleService, AgentWorkflowSourceAuthority) (*CommonAgentWorker, error)
	Projects               *projectservice.Service
	ProjectStore           *projectstore.Store
	ChatDB                 dbport.Beginner
	ScheduleSourceDB       dbport.Beginner
	ScheduleSourceFenceDB  dbport.Beginner
	Now                    func() time.Time
	TenantUUID             func(values.TenantId) uuid.UUID
	BackgroundRequired     bool
}

type agentServedAssembly struct {
	Common       *CommonAgentRuntime
	Authority    *CommonAgentAuthority
	Worker       *CommonAgentWorker
	Schedules    *AgentScheduleService
	Workflows    AgentWorkflowService
	Completion   AgentWorkflowCompletionBridge
	Controls     *AgentControlsSurface
	Owners       *AgentOwnerOperations
	Memory       *AgentMemoryOperations
	Actions      *AgentActionService
	Rollout      *AgentVersionRolloutService
	Portable     *AgentPortableService
	ProjectSkill *AgentProjectSkill
	Restore      AgentRestoreRuntime
	browserLogin bool
	publicOrigin string
	now          func() time.Time
}

func composeAgentServedAssembly(in agentServedAssemblyInput) (*agentServedAssembly, error) {
	if in.AgentDatabase.store == nil {
		if in.BackgroundRequired || in.Models != nil || in.ActionAuthority != nil || in.ActionAuthorityFactory != nil || in.WorkerFactory != nil {
			return nil, fmt.Errorf("agent services: %w", agentrun.ErrAuthorityMissing)
		}
		return nil, nil
	}
	if in.Core == nil || in.Cell == nil || in.Cell.RoleAccess == nil {
		return nil, fmt.Errorf("agent services: current core authorities required")
	}
	if in.TenantUUID == nil {
		in.TenantUUID = tenantKeyMapper[values.TenantId](pgstore.TenantID)
	}
	in.Now = personaServeClock(in.Now)
	assembly := &agentServedAssembly{browserLogin: in.Cell.BrowserLoginEnabled(), publicOrigin: in.Cell.PublicOrigin(), now: in.Now}
	owners, err := NewAgentOwnerOperations(in.Core, in.TenantUUID, in.Cell.RoleAccess, in.Now)
	if err != nil {
		return nil, fmt.Errorf("agent owner controls: %w", err)
	}
	memory, err := NewAgentMemoryOperations(in.Core, in.TenantUUID, in.Cell.RoleAccess, in.Now)
	if err != nil {
		return nil, fmt.Errorf("agent memory controls: %w", err)
	}
	assembly.Controls = &AgentControlsSurface{Operations: &AgentOwnerControls{Owners: owners, Memory: memory}}
	assembly.Owners, assembly.Memory = owners, memory
	assembly.Actions, err = NewAgentActionService(in.Cell)
	if err != nil {
		return nil, fmt.Errorf("agent action commands: %w", err)
	}
	if in.ActionAuthority != nil {
		if in.ActionAuthorityFactory != nil {
			return nil, ErrAgentActionApproval
		}
		if err := assembly.Actions.BindAuthority(in.ActionAuthority); err != nil {
			return nil, fmt.Errorf("agent action authority: %w", err)
		}
	}
	authorizer := PersonaAdminCommandRoleAuthorizer{Roles: in.Cell.RoleAccess}
	assembly.Portable, err = NewDatabaseAgentPortableService(in.AgentDatabase.store, authorizer, in.TenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent portable commands: %w", err)
	}
	if in.Personas != nil && in.Audience != nil {
		sources, err := NewDatabasePersonaAdminValidationSources(in.AgentDatabase.store, in.AgentDatabase.store, in.Personas.adminSkills, in.Personas.adminGrantStore, in.TenantUUID)
		if err != nil {
			return nil, fmt.Errorf("agent rollout profiles: %w", err)
		}
		assembly.Rollout, err = ComposeAgentVersionRollout(in.AgentDatabase.personas, authorizer, TenantPersonaProfileBuilder{Source: sources.Profiles}, in.Audience)
		if err != nil {
			return nil, fmt.Errorf("agent version rollout: %w", err)
		}
	}
	if in.Projects != nil || in.ProjectStore != nil {
		if in.Projects == nil || in.ProjectStore == nil || in.Personas == nil {
			return nil, errAgentProjectSkillRegistration
		}
		grants, err := agentdelegationstore.New(in.Core, in.TenantUUID)
		if err != nil {
			return nil, fmt.Errorf("agent project grants: %w", err)
		}
		assembly.ProjectSkill, err = NewAgentProjectSkillForRequests(*in.Projects, grants, ProjectStoreAgentProposalStore{Store: in.ProjectStore}, in.Now)
		if err != nil {
			return nil, fmt.Errorf("agent project skill: %w", err)
		}
		if _, err := BindAgentProjectSkill(in.Personas.capabilities, in.Personas.skills, assembly.ProjectSkill); err != nil {
			return nil, fmt.Errorf("agent project capability: %w", err)
		}
	}
	if in.Models == nil {
		if in.ActionAuthorityFactory != nil {
			return nil, fmt.Errorf("agent action runtime authority: %w", agentrun.ErrAuthorityMissing)
		}
		if in.BackgroundRequired {
			return nil, fmt.Errorf("agent background model authority: %w", agentrun.ErrAuthorityMissing)
		}
		return assembly, nil
	}
	if in.Cell.WorkflowVersions == nil || in.Cell.Capabilities == nil || in.ChatDB == nil || in.WorkerFactory == nil || in.ScheduleSourceDB == nil || in.ScheduleSourceFenceDB == nil {
		return nil, fmt.Errorf("agent background source owners: %w", agentrun.ErrAuthorityMissing)
	}
	assembly.Schedules, err = NewAgentScheduleService(AgentScheduleServiceConfig{CoreDB: in.Core, SourceDB: in.ScheduleSourceDB, SourceFenceDB: in.ScheduleSourceFenceDB, Agents: in.AgentDatabase.store, TenantUUID: in.TenantUUID, Now: in.Now})
	if err != nil {
		return nil, err
	}
	resolveTenant := func(tenant string) uuid.UUID { return in.TenantUUID(values.TenantId(tenant)) }
	workflowSource := AgentWorkflowSourceAuthority{Core: in.Core, ResolveTenant: resolveTenant, Versions: in.Cell.WorkflowVersions}
	assembly.Authority, err = NewCommonAgentAuthority(CommonAgentAuthorityConfig{CoreDB: in.Core, Agents: in.AgentDatabase.store, TenantUUID: in.TenantUUID, Models: in.Models, Now: in.Now,
		Sources: map[agentrun.SourceKind]CommonAgentSourceAuthority{
			agentrun.SourceSchedule: assembly.Schedules,
			agentrun.SourceWorkflow: workflowSource,
		}})
	if err != nil {
		return nil, err
	}
	bilateral := DatabaseAgentBilateralPolicy{CoreDB: in.Core, ChatDB: in.ChatDB, TenantUUID: in.TenantUUID, Now: in.Now}
	assembly.Common, err = NewCommonAgentRuntime(CommonAgentRuntimeConfig{
		Stores:    DatabaseCommonAgentStores{Agents: in.AgentDatabase.store, TenantUUID: in.TenantUUID, SourceKeys: agentServedSourceKeys{Schedules: assembly.Schedules}},
		Authority: AgentBilateralAuthority{Delegate: assembly.Authority, Policy: bilateral}, Now: in.Now,
		ExecutionFences: map[agentrun.SourceKind]CommonAgentExecutionFence{agentrun.SourceSchedule: assembly.Schedules},
	})
	if err != nil {
		return nil, err
	}
	if in.ActionAuthorityFactory != nil {
		authority, err := in.ActionAuthorityFactory(assembly.Common)
		if err != nil || authority == nil {
			return nil, errors.Join(ErrAgentActionApproval, err)
		}
		if err := assembly.Actions.BindAuthority(authority); err != nil {
			return nil, fmt.Errorf("agent action runtime authority: %w", err)
		}
	}
	// Bind the binding-only reader, so publication/firing never recursively
	// re-enters the schedule source verifier through common admission.
	if err := assembly.Schedules.BindCommon(assembly.Common, AgentBilateralBindingPolicy{Delegate: assembly.Authority, Policy: bilateral}); err != nil {
		return nil, err
	}
	assembly.Worker, err = in.WorkerFactory(assembly.Common, assembly.Schedules, workflowSource)
	if err != nil || assembly.Worker == nil || assembly.Worker.cfg.Runtime != assembly.Common ||
		assembly.Worker.cfg.Sources[agentrun.SourceSchedule] == nil || assembly.Worker.cfg.Sources[agentrun.SourceWorkflow] == nil {
		return nil, errors.Join(ErrCommonAgentWorker, err)
	}
	assembly.Controls.Schedules = assembly.Schedules
	assembly.Workflows = AgentWorkflowService{Admission: assembly.Common}
	if err := RegisterAgentWorkflowCapabilities(in.Cell.Capabilities, assembly.Workflows); err != nil {
		return nil, fmt.Errorf("agent workflow capability: %w", err)
	}
	assembly.Completion = AgentWorkflowCompletionBridge{Evidence: assembly.Common, Current: assembly.Common, Core: in.Core, ResolveTenant: resolveTenant, Now: in.Now}
	assembly.Restore = AgentRestoreRuntime{Agents: in.AgentDatabase.store, Runtime: assembly.Common, TenantUUID: in.TenantUUID, PendingOwners: []AgentRestorePendingOwner{assembly.Schedules}, EffectOwner: assembly.Worker, Limit: 100}
	return assembly, nil
}

type agentServedSourceKeys struct{ Schedules *AgentScheduleService }

func (s agentServedSourceKeys) ResolveSourceKey(ctx context.Context, request agentrun.Request) (string, error) {
	switch request.Source.Kind {
	case agentrun.SourceSchedule:
		if s.Schedules == nil {
			return "", agentrun.ErrAuthorityMissing
		}
		return s.Schedules.ResolveSourceKey(ctx, request)
	case agentrun.SourceWorkflow:
		source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
		return source.Key, err
	default:
		return "", agentrun.ErrSourceConverter
	}
}

// TickTenant is driven by the root's tenant loop. It plans native occurrences
// and retries durable completion signals without a process-local queue.
func (s *agentServedAssembly) TickTenant(ctx context.Context, tenant string) error {
	if s == nil || s.Common == nil || s.Schedules == nil || s.Worker == nil {
		return nil
	}
	_, restoreErr := s.Restore.TickTenant(ctx, tenant, s.now().UTC())
	planErr := s.Schedules.TickTenant(ctx, tenant)
	_, scheduleErr := s.Worker.Sweep(ctx, tenant, agentrun.SourceSchedule, 100)
	_, workflowErr := s.Worker.Sweep(ctx, tenant, agentrun.SourceWorkflow, 100)
	_, completionErr := s.Completion.Sweep(ctx, s.Common, tenant, 100)
	return errors.Join(restoreErr, planErr, scheduleErr, workflowErr, completionErr)
}

// BindPlatform connects task retention to the same governed memory owner that
// serves controls. Owner metrics already read this cell's canonical Core task
// and budget stores; installation identity must be bound by its admission owner.
func (s *agentServedAssembly) BindPlatform(runtime *agentRuntime) error {
	if s == nil || s.Memory == nil || s.Owners == nil {
		return errAgentRuntimeInput
	}
	return BindAgentRuntimeMemory(runtime, s.Memory)
}

func (s *agentServedAssembly) Overlay(next http.Handler, admission transport.Config) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if s == nil {
		return next
	}
	fallback := next
	next = OverlayAgentControlsSurface(next, s.Controls, admission)
	next = OverlayAgentPortableHTTP(next, s.Portable, admission)
	if s.Rollout != nil {
		next = OverlayAgentVersionRolloutHTTP(next, s.Rollout, admission)
	}
	actions := NewAgentActionHandler(s.Actions)
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, AgentActionsPath) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, _, denied := transport.Admit(r.Context(), admission, transport.AdmissionRequest{Metadata: transport.MapMetadata(r.Header), Method: r.URL.Path, Kind: transport.KindHTTPEdge})
		if denied != nil {
			http.Error(w, "request denied", denied.HTTPStatus())
			return
		}
		actions.ServeHTTP(w, r.WithContext(ctx))
	})
	options := edge.BrowserPolicyOptions{}
	if origin, err := url.Parse(s.publicOrigin); err == nil && origin.Scheme != "" && origin.Host != "" {
		options.AllowedOrigins = []string{s.publicOrigin}
		options.SecureCookies = origin.Scheme == "https"
	}
	guarded := edge.BrowserPolicy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The existing workspace cookie adapter preserves every screened header
		// and prefers an explicit credential. Only the served login decision can
		// enable cookie substitution, after the browser policy has run.
		metadata := workspace.AdmissionMetadata(r, s.browserLogin)
		if len(r.Header.Values("Authorization")) == 0 && len(metadata.Get(transport.AuthorizationMetadataKey)) > 0 && agentServedMutation(r.Method) {
			cookie, err := r.Cookie(edge.BrowserCSRFCookieName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				http.Error(w, "browser proof required", http.StatusForbidden)
				return
			}
		}
		request := r.Clone(r.Context())
		if credential := metadata.Get(transport.AuthorizationMetadataKey); len(credential) > 0 {
			request.Header["Authorization"] = append([]string(nil), credential...)
		}
		routes.ServeHTTP(w, request)
	}), options)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !agentServedPath(r.URL.Path) {
			fallback.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}

func agentServedPath(path string) bool {
	return strings.HasPrefix(path, AgentActionsPath) || path == agentcontrols.Path || strings.HasPrefix(path, agentcontrols.Path+"/") ||
		path == AgentPortableCatalogPath || path == AgentPortableExportPath || path == AgentPortableImportPath || path == AgentPortableDraftPath || path == AgentVersionRolloutPath
}

func agentServedMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
