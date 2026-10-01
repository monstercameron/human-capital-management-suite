package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentauditstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentbudgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentsettingstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ComponentAgentRuntime is the composed agent platform (UXBLIND-122).
const ComponentAgentRuntime = "agent-runtime"

const (
	agentAudience = "hcm-agent-tool-gateway"
	agentWorkload = "workload/agent-worker"
)

var errAgentRuntimeInput = errors.New("application: the agent runtime needs a pool, a cell and a tenant setting")

// agentRuntimeInput is what composeAgentRuntime is built from. Env answers
// environment questions (SchemaFlux's key) so a test never reads the process
// environment; nil answers nothing.
type agentRuntimeInput struct {
	Pool      *pgxadapter.Pool
	Cell      *app.Cell
	Config    ServeConfig
	Logger    interface{ Info(string, ...any) }
	Env       func(string) string
	Now       func() time.Time
	Tenants   []string
	Resources *AgentResourceRuntime
}

// agentRuntime is the composed result: the platform (which is also the wake
// tick the scheduler runs), the Starter behind the Agents page and the model
// selection that was made.
type agentRuntime struct {
	Platform    *agentsystem.Platform
	Starter     *agentStarter
	Controller  *agentController
	Model       agentModel
	Owner       *agentToolOwner
	Resources   *AgentResourceRuntime
	Budget      *agentbudget.Ledger
	Audit       agentaudit.Store
	TypedModels *AgentTypedModelDispatchBinding
}

// agentBudgetPolicy holds one task, one user-day and one tenant-month to
// ceilings that comfortably admit the two-step self-service plan and refuse a
// runaway.
func agentBudgetPolicy() agentbudget.Policy {
	return agentbudget.Policy{
		TaskDefault:   agentbudget.Limits{Steps: 20, Tokens: 50_000, WallClock: 30 * time.Minute, SpendMicros: 1_000_000},
		UserDaily:     agentbudget.Limits{Steps: 200, Tokens: 500_000, WallClock: 4 * time.Hour, SpendMicros: 10_000_000},
		TenantMonthly: agentbudget.Limits{Steps: 20_000, Tokens: 50_000_000, WallClock: 400 * time.Hour, SpendMicros: 1_000_000_000},
	}
}

// composeAgentRuntime builds the agent platform over the PostgreSQL stores for
// every profile that has a database. Agents are still off until a tenant
// administrator turns them on: the Starter refuses with ErrDisabled and the
// wake tick skips a tenant whose setting is off.
//
// It returns (nil, nil) when there is no pool, and an error when a store
// cannot be built or the budget ledger cannot be restored; the caller logs
// that and serves without agents rather than failing the process.
func composeAgentRuntime(ctx context.Context, in agentRuntimeInput) (*agentRuntime, error) {
	if in.Pool == nil {
		return nil, nil
	}
	if in.Cell == nil || in.Cell.Evidence == nil || in.Cell.Workers == nil || in.Cell.RoleAccess == nil {
		return nil, errAgentRuntimeInput
	}
	now := in.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	tenantUUID := tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)

	// The tenant setting. A cell composed without an execution database has
	// none of its own, so the agent runtime supplies the same PostgreSQL store
	// and hands it to the workspace through the cell before the handler is
	// built.
	settings := in.Cell.AgentSettings
	if settings == nil {
		store, err := agentsettingstore.New(in.Pool, tenantUUID)
		if err != nil {
			return nil, fmt.Errorf("agent settings store: %w", err)
		}
		in.Cell.AgentSettings = store
		settings = store
	}

	grants, err := agentdelegationstore.New(in.Pool, tenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent delegation store: %w", err)
	}
	tasks, err := agentrunstore.New(in.Pool, tenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent run store: %w", err)
	}
	var audit agentaudit.Store
	auditStore, err := agentauditstore.New(in.Pool, tenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent audit store: %w", err)
	}
	audit = auditStore
	budgetStore, err := agentbudgetstore.New(in.Pool, tenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent budget store: %w", err)
	}
	ledger, err := agentbudget.NewWithPersistence(agentBudgetPolicy(), now, budgetStore)
	if err != nil {
		return nil, err
	}
	// Restore each served tenant's usage so a restart does not reset the
	// ceilings. The ledger is process-wide and refuses to load a task twice,
	// so this runs once, here.
	for _, tenant := range in.Tenants {
		state, err := budgetStore.Load(ctx, kernelvalues.TenantId(tenant), now())
		if err != nil {
			return nil, fmt.Errorf("load agent budget for %s: %w", tenant, err)
		}
		if err := ledger.Restore(state); err != nil {
			return nil, fmt.Errorf("restore agent budget for %s: %w", tenant, err)
		}
	}

	// The cell layers the durable population behind the corpus only when it is
	// composed with an execution database; without one the agent read layers it
	// itself, so a created worker still resolves.
	workers := in.Cell.Workers
	if !in.Config.ExecutionAuthority {
		workers = workforce.NewLayeredWorkerFacts(workers, in.Pool, tenantUUID)
	}
	reader := ownWorkerReader{db: in.Pool, tenantUUID: tenantUUID, workers: workers, now: now}
	var evidence capability.EvidenceSink = in.Cell.Evidence
	caps, gateway, err := newAgentCapabilities(reader, evidence, now)
	if err != nil {
		return nil, err
	}
	skills, err := newAgentSkills(caps)
	if err != nil {
		return nil, err
	}
	inspector, err := newAgentDLPInspector()
	if err != nil {
		return nil, err
	}
	egress, err := newAgentEgress(inspector)
	if err != nil {
		return nil, err
	}

	var typedModels *AgentTypedModelDispatchBinding
	var model agentModel
	if strings.TrimSpace(in.Config.AgentModelConfigFile) != "" || (in.Config.Profile == ServeProfileLocalDev && in.Env != nil && strings.TrimSpace(in.Env("MODEL_API_KEY")) != "") {
		typedModels = &AgentTypedModelDispatchBinding{}
		model = agentModel{Kind: agentModelConfigured, Typed: typedModels}
	} else {
		model = selectAgentModel(in.Config.Profile, in.Env)
	}
	if model.Kind == agentModelConfigured && typedModels == nil {
		if err := initializeConfiguredAgentModel(); err != nil {
			return nil, err
		}
	}
	var typedDispatch agentmodel.TypedModelDispatcher
	if typedModels != nil {
		typedDispatch = typedModels
	}
	if in.Logger != nil {
		in.Logger.Info("hcmnext.agent_model_selected", "provider", string(model.Kind), "profile", in.Config.Profile)
	}
	owner := newAgentToolOwner(gateway, model)
	resourceRuntime := in.Resources
	if resourceRuntime == nil {
		resourceRuntime, err = NewAgentResourceRuntime(AgentResourceRuntimeConfig{Policy: DefaultAgentResourcePolicy(in.Config.CellID)})
		if err != nil {
			return nil, fmt.Errorf("agent resource policy: %w", err)
		}
	}
	providerID := string(model.Kind)
	if !model.Available() {
		providerID = ""
	}
	if model.Kind == agentModelConfigured {
		providerID = "openai"
	}
	if typedModels != nil {
		providerID = ""
	}
	authority := agentAuthority{roles: in.Cell.RoleAccess, db: in.Pool, tenantUUID: tenantUUID}

	// Step credentials live five minutes and are minted and verified inside
	// one process, so a per-process signing key is enough; a restart only
	// invalidates credentials nobody holds.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("agent token secret: %w", err)
	}
	platform, err := agentsystem.NewPlatform(agentsystem.Config{
		Skills: skills, Budget: ledger, Audit: audit, Redactor: agentRedactor{inspector: inspector}, Egress: egress,
		Grants: grants, Tasks: tasks, Authority: authority, Owner: owner,
		TokenSecret: secret, Audience: agentAudience, Workload: agentWorkload,
		ModelEstimate: agentbudget.Usage{Steps: 1, Tokens: 4_000, WallClock: 2 * time.Minute, SpendMicros: 100_000},
		WakeGate:      agentWakeGate(settings),
		Clock:         now,
		TypedDispatch: typedDispatch,
		StepAdmission: AgentTaskStepAdmission{Runtime: resourceRuntime, ProviderID: providerID},
	})
	if err != nil {
		return nil, err
	}
	starter := &agentStarter{
		platform: platform, settings: settings, authority: authority, inspector: inspector, model: model,
		now: now, wait: agentStartWait, driveFor: agentDriveTimeout, newID: randomAgentTaskID,
	}
	controller := &agentController{platform: platform, settings: settings, authority: authority, now: now}
	return &agentRuntime{Platform: platform, Starter: starter, Controller: controller, Model: model, Owner: owner, Resources: resourceRuntime, Budget: ledger, Audit: audit, TypedModels: typedModels}, nil
}

// agentLogger is the slice of the composition logger the agent wiring uses.
type agentLogger interface {
	Info(string, ...any)
	Error(string, ...any)
}

// wireAgentRuntime composes the agent runtime and attaches it to the cell:
// the Agents page client, the task starter and the wake tick. A composition
// failure is logged and leaves the cell without agents (the page then says so)
// rather than failing the process; agents are an optional surface.
func wireAgentRuntime(ctx context.Context, in ServeInput, cell *app.Cell, logger agentLogger, graph *graphBuilder, now func() time.Time) *agentRuntime {
	runtime, err := composeAgentRuntime(ctx, agentRuntimeInput{
		Pool: in.Pool, Cell: cell, Config: in.Config, Logger: logger, Env: os.Getenv, Now: now, Tenants: in.Config.ServedTenants(),
	})
	if err != nil {
		logger.Error("hcmnext.agent_runtime_unavailable", "error", err.Error())
		return nil
	}
	if runtime == nil {
		return nil
	}
	if err := composeServedAgentTaskModel(ctx, in.Config, runtime, os.Getenv, now); err != nil {
		logger.Error("hcmnext.agent_task_model_unavailable", "error", err.Error())
	}
	cell.Agents = agentStartAvailabilityClient{AgentClient: agentclient.FromPlatformWithControls(runtime.Platform, cell.AgentSettings, runtime.Controller), starter: runtime.Starter}
	cell.AgentStarter = runtime.Starter
	cell.AgentController = runtime.Controller
	cell.AgentWaker = runtime.Platform
	graph.add(ComponentAgentRuntime, KindEngine, runtime.Platform, ComponentDatabasePool, ComponentCell)
	return runtime
}
