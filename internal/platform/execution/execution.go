// Package execution composes the real caller-driven promotion execution
// driver for IntentService.ExecuteIntent's P1B execution-authority gate.
//
// It is a composition-root helper, not a business layer: it wires together
// internal/workflow/execute's durable driver, internal/workflow/prototype's
// bounded promote_worker approval graph, internal/humanwork/workitem's real
// store and internal/transaction/idempotency's guard, and adapts the result
// to internal/intent/app.ProposalExecutor. That composition previously lived
// in internal/transport/cell, which put transport -> workflow and
// transport -> transaction edges into
// definitions/architecture/package-dependency-policy.yaml's ranked layer
// graph that transport (the experience/API adaptation layer) has no
// business asserting. internal/platform is the process-bootstrap/shared-
// plumbing root repository-layout.yaml already describes as importable by
// every command; package-dependency-policy.yaml does not rank it as a
// business layer at all (like cmd/* itself), which is exactly what a
// composition-root helper needs: it is wiring, not a layer with a direction
// invariant to enforce against it.
//
// Callers are command composition roots (cmd/hcmnext's
// composeExecutionAuthority) and cross-package integration tests
// (test/bootstrap) that need the same real wiring a command would build.
package execution

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/workload"
)

// defaultRetention is the caller-driven driver's idempotency retention when
// PromotionExecutionConfig.Retention is the zero value.
var defaultRetention = idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}

// defaultApproverPrincipalID is who the composed promotion approval workflow
// routes its one approval WorkItem to, when the composition root names no
// approver of its own. Relationship-based resolution (ManagerOf(worker),
// HRBPFor(worker.organization)) is a later P1B contract
// (planning/specs/workflow-runtime.md "Resolvers may target ..."); this
// release names one fixed operator instead, exactly like
// internal/workflow/prototype's own conformance fixture does.
const defaultApproverPrincipalID = "principal:promotion-approver"

// defaultManagerApproverPrincipalID keeps the execute plan's current-manager
// authority distinct even when older programmatic compositions set only the
// finance approver. Deployment configuration should name both explicitly.
const defaultManagerApproverPrincipalID = "principal:promotion-manager-approver"

// defaultRequiredRole is the principal role ExecuteIntent requires under a
// PromotionExecution's ExecutionAuthority, when the composition root names
// no role of its own.
const defaultRequiredRole = "promotion_operator"

// PromotionExecutionConfig configures [NewPromotionExecution]. DB and
// Terminal are required; every other field defaults to a bounded, named
// value.
type PromotionExecutionConfig struct {
	// Plan selects the executable promotion graph. The zero value is the
	// shipped prototype, preserving the pre-execute deployment behavior.
	Plan PromotionPlan
	// DB opens the transactions Start and each Advance run inside.
	DB execute.Beginner
	// StartRetry opts into bounded serializable START retries. Admission is
	// caller-owned and must be supplied on the options; this composition root
	// does not invent an always-admit policy.
	StartRetry *transactioncommit.RetryOptions
	// StartRetryFor builds a request-scoped retry policy from the immutable
	// START request and trusted persisted budget metadata.
	StartRetryFor func(context.Context, execute.StartRetryIdentity) (*transactioncommit.RetryOptions, error)
	// LeaseHolder names this process in WORKFLOW_INSTANCE leases the driver
	// takes around every caller-driven run (WF-RUN-036). Zero derives a
	// host-and-pid identity.
	LeaseHolder lease.Identity
	// ConflictFence is the application-composed durable conflict adapter for
	// governed transaction plans. Nil preserves legacy unfenced workflows.
	ConflictFence transactioncommit.ConflictFence
	// Terminal performs the one governed business write the workflow's END
	// node raises. This package never implements one itself — it is a port,
	// supplied by the composition root
	// (internal/workflow/execute/effects.LedgerTerminalWriter in production,
	// a recording fake in a test).
	Terminal execute.TerminalWriter
	// Guard is the idempotency store TX-006 requires. Nil means
	// idempotency.PostgresStore{}.
	Guard idempotency.Store
	// Retention is the idempotency retention policy. The zero value means
	// [defaultRetention].
	Retention idempotency.RetentionPolicy
	// Clock supplies the recording time. Nil means time.Now in UTC.
	Clock func() time.Time
	// ApproverPrincipalID is who the one approval WorkItem this workflow
	// raises is routed to. For the execute plan it is the finance approver;
	// empty means [defaultApproverPrincipalID].
	ApproverPrincipalID string
	// ManagerApproverPrincipalID is the distinct current-manager authority
	// used by the execute plan's second approval. Empty preserves the legacy
	// single-approver composition for the prototype plan.
	ManagerApproverPrincipalID string
	// FinancePartnerPrincipalID is the principal the executable plan's
	// FinancePartnerFor(cost_center) approval routes to (PROMOUX-015). Empty
	// keeps PROMOUX-003's class-scoped derivation of ApproverPrincipalID. The
	// value is configuration, never a literal in routing logic: no cost-center
	// relationship graph exists in this release to resolve it from.
	FinancePartnerPrincipalID string
	// Managers resolves the executable plan's CurrentManagerOf(worker)
	// approval (PROMOUX-015). Nil means [JourneyWorkerManagers], the
	// journey_worker manager relationship internal/data/orgfacts also reads.
	Managers ManagerResolver
	// AuthorityDigest names the signed P1B authority amendment this
	// composition asserts. Carried through as evidence; never verified here.
	AuthorityDigest string
	// RequiredRole is the principal role ExecuteIntent additionally requires
	// under the returned ExecutionAuthority. Empty means [defaultRequiredRole].
	RequiredRole string
	// Telemetry is the OTel provider OBS-023's spans are opened through
	// (the same *hcmotel.Provider a Cell's own CellConfig.Telemetry field
	// carries). Nil means [execute.NoopInstrumentation]: no spans, no log
	// lines.
	Telemetry *hcmotel.Provider
	// Logger receives the one required log/slog envelope line per
	// advancement and per terminal write, when Telemetry is non-nil. Nil
	// means a logging.Handler over os.Stderr.
	Logger *slog.Logger
	// Evidence is the sink OBS-024's three driver-recorded kinds
	// (APPROVAL_COMPLETED, TASK_SUBMITTED, TERMINAL_WRITTEN) land on. A
	// composition root passes the same sink it hands app.CellConfig.Evidence,
	// so the cell's evidence chronology - the gateway's capability decisions,
	// ExecuteIntent's gate decisions and the driver's own execution evidence
	// - is one list read back from one place, and the journey's Inspect can
	// show all of it. A composition serving traffic supplies the durable
	// internal/data/evidencestore (WF-RUN-035). Nil means a private in-memory
	// sink, exposed only as [PromotionExecution.Evidence], for unit
	// compositions.
	Evidence app.EvidenceStore
	// TimerDataset is the tzdb and calendar release a WAIT node's wake
	// requirement is resolved against (WF-RUN-004). When both versions are
	// set the driver is composed with this package's [TimerFactory] and
	// internal/workflow/timer's Reader, so a WAIT node parks on a durable
	// workflow_timer promise and resumes from it; the zero value composes no
	// timer ports, which leaves a WAIT node refused as an unsupported
	// continuation exactly as before. It is supplied, never read from the
	// environment: a dataset revision is a deployment decision.
	TimerDataset values.DatasetVersions
	// Versions is the durable compiled-version registry the shipped
	// workflows are published into and activated through (WF-COMP-006,
	// WF-RUN-035). A composition root serving traffic supplies
	// internal/data/workflowversionstore; nil keeps a private in-memory
	// registry that self-activates, for unit compositions only.
	Versions VersionRegistry
	// CellID names the cell whose local consistency boundary the executable
	// plan's core commit resolves against. Empty means "cell-local".
	CellID string
	// VersionApprover is the release approver a DRAFT shipped version is
	// durably approved under before activation. It must differ from the
	// publisher. Empty means [defaultVersionApprover].
	VersionApprover string
}

// poisonWorkOwner is who is accountable for QuarantinedWork the served driver
// files (WF-RUN-007), and poisonWorkSLA how long that owner has to act. They
// name one operations queue until relationship-based ownership exists, the
// same way defaultApproverPrincipalID names one approver.
const (
	poisonWorkOwner = "principal:workflow-operations"
	poisonWorkSLA   = 4 * time.Hour
)

// composedPoisonWork is the served driver's poison-work policy: durable
// runtime.QuarantineStore records owned by [poisonWorkOwner].
func composedPoisonWork() *execute.PoisonWorkPolicy {
	return &execute.PoisonWorkPolicy{Store: runtime.QuarantineStore{}, Owner: poisonWorkOwner, SLA: poisonWorkSLA}
}

const (
	versionPublisher       = "cmd/hcmnext:execution-authority"
	defaultVersionApprover = "cmd/hcmnext:workflow-release-approver"
)

// PromotionExecution is the composed EXECUTE-mode wiring for
// internal/workflow/prototype's bounded, executable promote_worker approval
// graph, shaped to plug directly into app.CellConfig's Executor,
// ExecutionResolver, ExecutionVersions and ExecutionAuthority fields.
type PromotionExecution struct {
	Executor  app.ProposalExecutor
	Resolver  runtime.WorkflowResolver
	Versions  version.Store
	Authority *app.ExecutionAuthority
	// Evidence is OBS-024's execution-evidence sink for this driver's own
	// three kinds (APPROVAL_COMPLETED, TASK_SUBMITTED, TERMINAL_WRITTEN):
	// the same store CAP-002's gateway uses. It is the store
	// [PromotionExecutionConfig.Evidence] supplied, or the private in-memory
	// one this composition created when none was.
	Evidence app.EvidenceStore
	Plan     PromotionPlan
	// steps is the executable plan's promotionsteps adapter; see
	// [PromotionExecution.BindStepServices].
	steps *promotionStepPorts
}

// PromotionPlan selects which published promotion workflow the execution
// authority resolves. It is intentionally a small closed vocabulary: the
// resolver and the step/work-item ports must always agree on the same graph.
type PromotionPlan string

const (
	PLAN_PROTOTYPE PromotionPlan = "prototype"
	PLAN_EXECUTE   PromotionPlan = "execute"
)

// NewPromotionExecution composes the caller-driven driver
// (internal/workflow/execute.Driver) for internal/workflow/prototype's
// bounded promote_worker approval graph: a data-driven resolver bound to the
// one compiled and activated plan, a step runner that parks on APPROVAL and
// completes on END, a work-item factory backed by the real
// internal/humanwork/workitem store, and cfg.Terminal for the END node's
// governed business write.
//
// This is deliberately the smallest executable shape next-steps.md's P1B
// item 1 names (promote_worker in EXECUTE mode): it performs no capability
// invocation and no domain mutation of its own. The workflow parks for one
// human approval and, once resumed with an APPROVED outcome, records the
// promotion outcome as a governed ledger fact through cfg.Terminal.
func NewPromotionExecution(cfg PromotionExecutionConfig) (*PromotionExecution, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("platform execution: promotion execution needs a database Beginner")
	}
	if cfg.Terminal == nil {
		return nil, fmt.Errorf("platform execution: promotion execution needs a TerminalWriter")
	}
	if cfg.StartRetry != nil && cfg.StartRetry.Admit == nil {
		return nil, fmt.Errorf("platform execution: StartRetry requires an admission callback")
	}
	if cfg.StartRetry != nil && cfg.StartRetryFor != nil {
		return nil, fmt.Errorf("platform execution: StartRetry and StartRetryFor are mutually exclusive")
	}
	if cfg.StartRetry != nil && (cfg.StartRetry.Prepare != nil || cfg.StartRetry.ResolveAmbiguous != nil) {
		return nil, fmt.Errorf("platform execution: StartRetry does not accept commit-plan Prepare or ResolveAmbiguous callbacks")
	}
	var startRetry *transactioncommit.RetryOptions
	if cfg.StartRetry != nil {
		copy := *cfg.StartRetry
		startRetry = &copy
	}
	startRetryFor := cfg.StartRetryFor
	leaseHolder := cfg.LeaseHolder
	if leaseHolder == (lease.Identity{}) {
		leaseHolder = defaultInstanceHolder()
	}
	guard := cfg.Guard
	if guard == nil {
		guard = idempotency.PostgresStore{}
	}
	retention := cfg.Retention
	if retention == (idempotency.RetentionPolicy{}) {
		retention = defaultRetention
	}
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	approver := cfg.ApproverPrincipalID
	if approver == "" {
		approver = defaultApproverPrincipalID
	}
	managerApprover := cfg.ManagerApproverPrincipalID
	if managerApprover == "" {
		if cfg.Plan == PLAN_EXECUTE {
			// Preserve the default executable composition's two concrete
			// principals. A directly constructed factory with no manager still
			// derives authority-class identities from its base.
			managerApprover = defaultManagerApproverPrincipalID
		} else {
			managerApprover = approver
		}
	}
	role := cfg.RequiredRole
	if role == "" {
		role = defaultRequiredRole
	}
	selected := cfg.Plan
	if selected == "" {
		selected = PLAN_PROTOTYPE
	}
	if selected != PLAN_PROTOTYPE && selected != PLAN_EXECUTE {
		return nil, fmt.Errorf("platform execution: unknown promotion plan %q", selected)
	}
	if selected == PLAN_EXECUTE && managerApprover == approver {
		return nil, fmt.Errorf("platform execution: finance and manager approvers must be distinct for the execute plan")
	}

	prototypePlan, err := prototype.CompileApproval()
	if err != nil {
		return nil, fmt.Errorf("platform execution: compile the promotion approval workflow: %w", err)
	}
	executePlan, err := promotionexec.Compile()
	if err != nil {
		return nil, fmt.Errorf("platform execution: compile the promotion execute workflow: %w", err)
	}
	versions, err := composeVersions(cfg, clock())
	if err != nil {
		return nil, err
	}

	selectedPlan := prototypePlan
	selectedWorkflowID := prototypePlan.WorkflowID
	if selected == PLAN_EXECUTE {
		selectedPlan = executePlan
		selectedWorkflowID = executePlan.WorkflowID
	}
	effectiveDates := &sync.Map{}
	// WF-RUN-034: the executable plan's governed steps; the application binds
	// its gateway-invoking services after composing the cell.
	steps := &promotionStepPorts{db: cfg.DB, cellID: cfg.CellID, authorityDigest: cfg.AuthorityDigest}
	if steps.cellID == "" {
		steps.cellID = "cell-local"
	}

	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: selectedWorkflowID,
		Pin:        version.Pin{CompiledPlanDigest: selectedPlan.Digest()},
		Plan:       selectedPlan,
	}}}

	// OBS-023: no spans/logs at all unless a composition root supplies a
	// Telemetry provider — exactly the same opt-in shape CellConfig.Telemetry
	// already uses.
	var instrumentation execute.Instrumentation = execute.NoopInstrumentation{}
	var recorder observe.Recorder
	if cfg.Telemetry != nil {
		logger := cfg.Logger
		if logger == nil {
			logger = slog.New(logging.NewHandler(os.Stderr, logging.WithService("hcmnext-workflow-execute"), logging.WithClock(clock)))
		}
		instrumentation = NewOTelInstrumentation(cfg.Telemetry, logger, clock)
		recorder = NewObserveRecorder(cfg.Telemetry, logger, clock)
	}

	// OBS-024: this driver's own three evidence kinds
	// (APPROVAL_COMPLETED/TASK_SUBMITTED/TERMINAL_WRITTEN) are recorded
	// through the same capability evidence sink mechanism CAP-002's gateway
	// already uses - on the cell's own sink when the composition root passed
	// one, otherwise on a sink dedicated to this execution wiring.
	var evidenceSink app.EvidenceStore = app.NewMemoryEvidenceSink()
	if cfg.Evidence != nil {
		evidenceSink = cfg.Evidence
	}
	evidence := capabilityEvidenceAdapter{sink: evidenceSink, now: clock}

	options := execute.Options{
		DB:            cfg.DB,
		StartRetry:    startRetry,
		StartRetryFor: startRetryFor,
		ConflictFence: cfg.ConflictFence,
		Steps:         promotionStepRunner{plan: selected, effectiveDates: effectiveDates, ports: steps},
		WorkItems:     promotionWorkItems{approver: approver, managerApprover: managerApprover, financePartner: cfg.FinancePartnerPrincipalID, managers: managerFallback{base: cfg.Managers, fallback: managerApprover}, plan: selected},
		Terminal:      cfg.Terminal,
		Guard:         guard,
		Retention:     retention,
		Clock:         clock,
		// WF-RUN-028: Resume loads the durable WorkItem itself, through the
		// real internal/humanwork/workitem store this composition already
		// uses to create and route it. workitem.Store satisfies
		// execute.WorkItemReader structurally, so no adapter type is needed
		// here (and none may import github.com/google/uuid directly --
		// tools/policy/libfirewall's semantic firewall reserves that import
		// to internal/humanwork and the other roots it names).
		Items:           workitem.Store{},
		Instrumentation: instrumentation,
		Recorder:        recorder,
		Evidence:        evidence,
		// WF-RUN-021: every start is admitted against resolved workload limits
		// before any row is written; each verdict is traced and logged.
		Workload: &runtime.WorkloadGate{
			Snapshot: workload.DefaultSnapshot(),
			Observe:  NewWorkloadObserver(cfg.Telemetry, cfg.Logger).Observe,
		},
		// WF-RUN-036: every served run advances under a WORKFLOW_INSTANCE lease
		// fence -- its own, or the one a dispatcher already holds -- verified
		// in each advance transaction before any step writes.
		Leases:        NewInstanceLeaser(leaseHolder, 0),
		FenceVerifier: lease.Fenced{Manager: lease.Manager{}},
		// WF-RUN-007: an exhausted node with no failure route lands durable
		// QuarantinedWork and routes its instance instead of rolling back.
		PoisonWork: composedPoisonWork(),
		// WF-RUN-006: runtime.Decide is the one retry decision for every
		// failed node with a compiled retry policy; a backoff parks on a
		// RETRY_BACKOFF timer that ResumeTimer reads back through the reader.
		NodeRetry:   composedNodeRetry(),
		TimerReader: timer.Reader{},
		// WF-RUN-037: a failed DOWNSTREAM_EFFECT or DERIVED_UPDATE settles
		// durably against the committed core and takes its compiled failure
		// route instead of aborting the run.
		EffectRoles: composedEffectRoles(),
	}
	// WF-RUN-009: a durable registry that records governed quarantines also
	// tells every advancement which disposition a live instance takes.
	if quarantine, ok := cfg.Versions.(execute.VersionQuarantine); ok {
		options.Quarantine = quarantine
	}
	if cfg.TimerDataset != (values.DatasetVersions{}) {
		factory, factoryErr := NewTimerFactory(TimerFactoryConfig{Scheduler: timer.Scheduler{}, Dataset: cfg.TimerDataset, EffectiveDates: effectiveDates})
		if factoryErr != nil {
			return nil, factoryErr
		}
		options.Timers = factory
		options.TimerReader = timer.Reader{}
	}
	// WF-RUN-005: a SIGNAL node parks on a durable subscription and resumes
	// from its matched receipt (see signals.go).
	options.Signals, options.SignalReader = SignalSubscriptions{}, SignalSubscriptions{}
	driver, err := execute.New(options)
	if err != nil {
		return nil, fmt.Errorf("platform execution: build the promotion execution driver: %w", err)
	}

	return &PromotionExecution{
		Executor: executeDriverAdapter{driver: driver},
		Resolver: resolver,
		Versions: versions,
		Authority: &app.ExecutionAuthority{
			AuthorityDigest:     cfg.AuthorityDigest,
			AdmittedIntentTypes: map[string]bool{promotion.IntentType: true},
			RequiredRole:        role,
		},
		Evidence: evidenceSink,
		Plan:     selected,
		steps:    steps,
	}, nil
}

// promotionPublishDefinition is the publication form of the landed execute
// definition. promotionexec.Compile applies these two compiler projections
// internally; version.Publish recompiles from a definition, so the
// composition root supplies the same immutable projection and capability
// manifests at the publication boundary.
func promotionPublishDefinition() workflow.Definition {
	def := promotionexec.Definition()
	def.DeclaredModes = []workflow.ExecutionMode{workflow.ModeExecute}
	def.Nodes = append([]workflow.Node(nil), def.Nodes...)
	types := make(map[string]workflow.StepType, len(def.Nodes))
	for _, node := range def.Nodes {
		types[node.ID] = node.Type
	}
	edges := make([]workflow.Edge, 0, len(def.Edges))
	seen := map[string]bool{}
	for _, edge := range def.Edges {
		route := edge.RouteKey
		switch types[edge.From] {
		case workflow.StepWait:
			if route == "FIRED" {
				route = "SUCCEEDED"
			}
		case workflow.StepTask:
			switch route {
			case "REAPPROVED":
				route = "SUCCEEDED"
			case "WITHDRAWN":
				route = "CANCELLED"
			case "INVALIDATED":
				continue
			}
		case workflow.StepObserve:
			switch route {
			case "CONSISTENT":
				route = "PASS"
			case "DEGRADED":
				route = "PARTIAL"
			}
		}
		edge.RouteKey = route
		key := edge.From + "\x00" + edge.To + "\x00" + edge.RouteKey
		if !seen[key] {
			seen[key] = true
			edges = append(edges, edge)
		}
	}
	def.Edges = edges
	return def
}

type promotionCapabilities map[capability.Key]capability.Record

func (r promotionCapabilities) Lookup(key capability.Key) (capability.Record, bool) {
	record, ok := r[key]
	return record, ok
}

func promotionPublishOptions() workflow.Options {
	readOnly := func(id, owner, scope string) capability.Record {
		return capability.Record{Definition: capability.Definition{
			ID: id, Version: 1, OwnerDomain: owner,
			RequestSchema:  capability.SchemaRef{SchemaID: id + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			ResponseSchema: capability.SchemaRef{SchemaID: id + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			ErrorSchema:    capability.SchemaRef{SchemaID: id + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
			EffectClass:    capability.EffectReadOnly, IdempotencyPolicyRef: "idempotency.promotion." + owner + ".v1", AuthZScopeRef: scope,
			LegalBasisRef: "legal.promotion.execution/v1", EntitlementRef: "entitlement.promotion.execution/v1", SLOClassRef: "slo.promotion.execution/v1", TestRef: "conformance:" + id + "/v1",
		}, Status: capability.StatusActive, Digest: "sha256:promotionexec-" + owner}
	}
	mutating := readOnly("hcmnext.people.promote_worker", "people", "scope:people.write")
	mutating.Definition.EffectClass = capability.EffectInternalMutation
	return workflow.Options{Phase: workflow.PhaseP1B, Capabilities: promotionCapabilities{
		{ID: "hcmnext.people.explain_worker_state", Version: 1}:        readOnly("hcmnext.people.explain_worker_state", "people", "scope:people.read"),
		{ID: "hcmnext.rewards.simulate_compensation", Version: 1}:      readOnly("hcmnext.rewards.simulate_compensation", "rewards", "scope:rewards.read"),
		{ID: "hcmnext.rewards.evaluate_pay_band_position", Version: 1}: readOnly("hcmnext.rewards.evaluate_pay_band_position", "rewards", "scope:rewards.read"),
		{ID: "internal/governance/revalidate", Version: 1}:             readOnly("internal/governance/revalidate", "governance", "scope:governance.read"),
		{ID: "hcmnext.people.promote_worker", Version: 1}:              mutating,
		{ID: "hcmnext.payroll.observe_promotion", Version: 1}:          readOnly("hcmnext.payroll.observe_promotion", "payroll", "scope:observation.read"),
		{ID: "hcmnext.access.observe_promotion", Version: 1}:           readOnly("hcmnext.access.observe_promotion", "access", "scope:observation.read"),
		{ID: "hcmnext.reconciliation.observe_promotion", Version: 1}:   readOnly("hcmnext.reconciliation.observe_promotion", "reconciliation", "scope:observation.read"),
	}}
}

// runPrototypeStep runs internal/workflow/prototype's two node types: it
// parks on APPROVAL and returns a bare outcome on END. It invokes no
// capability and performs no business mutation itself — the driver's own
// continuation sink is what runs the composed TerminalWriter at END.
func runPrototypeStep(req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	switch req.Node.Type {
	case workflow.StepApproval:
		return frontier.NodeOutcome{
			NodeID: req.Node.ID, Await: frontier.AwaitWorkItem,
			AwaitRef: prototype.ApprovalRequirementID,
		}, runtime.GovernanceRefs{}, nil
	case workflow.StepEnd:
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{},
			fmt.Errorf("platform execution: promotion execution has no step for %s", req.Node.Type)
	}
}

// approvalDecisionWindow is how long after the WorkItem is raised the routed
// approver has to decide. It is the WorkItem's DeadlineAt and, through
// prototype.CompileApprovalRequirement, the compiled requirement's DecideBy
// and Expiry, so the deadline the item shows and the deadline Resolve
// enforces are one value.
const approvalDecisionWindow = 48 * time.Hour

// promotionWorkItems creates and routes the approval WorkItems the promotion
// graphs raise: the prototype's one approval to the configured approver, and
// the executable plan's finance and manager approvals to the principals
// [promotionWorkItems.resolveApprovers] derives from the reference workflow.
type promotionWorkItems struct {
	approver        string
	managerApprover string
	financePartner  string
	managers        ManagerResolver
	plan            PromotionPlan
}

// managerFallback preserves configured demo authorities only for subjects
// outside the worker relationship graph. A graph worker with no resolved
// manager still fails closed; resolveApprovers checks separation afterwards.
type managerFallback struct {
	base     ManagerResolver
	fallback string
}

func (m managerFallback) CurrentManagerOf(ctx context.Context, ex workitem.Executor, tenantID uuid.UUID, workerRef string) (ManagerOf, error) {
	base := m.base
	if base == nil {
		base = JourneyWorkerManagers{}
	}
	answer, err := base.CurrentManagerOf(ctx, ex, tenantID, workerRef)
	if err == nil && !answer.InGraph && m.fallback != "" {
		answer.ManagerPrincipal = m.fallback
	}
	return answer, err
}

var _ execute.WorkItemFactory = promotionWorkItems{}

// CreateAndRoute implements execute.WorkItemFactory.
//
// The assignment it records carries the real compiled requirement's digests
// (prototype.CompileApprovalRequirement over this factory's approver and the
// item's own deadline), not placeholders: internal/workflow/steps/approval's
// Resolve re-checks a completed item against exactly those digests before it
// will produce the typed outcome the driver resumes from, and a caller that
// rebuilds the requirement from the stored row (its DeadlineAt, its routed
// approver) has to arrive at the same digest this routing recorded.
func (f promotionWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (ret0 workitem.WorkItem, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.create_work_item", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	plan := f.plan
	if plan == "" {
		plan = PLAN_PROTOTYPE
	}
	if plan == PLAN_EXECUTE && req.Continuation.TargetNodeID == promotionexec.NodeReapproval {
		return f.createExecuteTask(ctx, ex, req)
	}
	var requirement humanwork.ApprovalRequirement
	var err error
	// owner is who this specific node's WorkItem is routed to. For the
	// executable plan it is never f.approver directly: PROMOUX-015 routes the
	// finance approval to the configured finance partner and the manager
	// approval to the worker's current manager, and refuses a route in which
	// the two coincide or either is the requester or the subject.
	owner := f.approver
	termRef := termConfiguredApprover
	directoryVersion := directoryConfiguredApprover
	deadline := req.CreatedAt.Add(approvalDecisionWindow)
	if plan == PLAN_EXECUTE {
		route, routeErr := f.resolveApprovers(ctx, ex, req)
		if routeErr != nil {
			return workitem.WorkItem{}, routeErr
		}
		if req.Continuation.TargetNodeID == promotionexec.NodeApproveFinance {
			owner, termRef, directoryVersion = route.finance.principal, route.finance.termRef, route.finance.directoryVersion
			requirement, err = promotionexec.CompileFinanceApprovalRequirement(owner, deadline)
		} else {
			owner, termRef, directoryVersion = route.manager.principal, route.manager.termRef, route.manager.directoryVersion
			requirement, err = promotionexec.CompileManagerApprovalRequirement(owner, deadline)
		}
	} else {
		requirement, err = prototype.CompileApprovalRequirement(owner, deadline)
	}
	if err != nil {
		return workitem.WorkItem{}, fmt.Errorf("platform execution: compile the approval requirement: %w", err)
	}
	item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
		WorkType: requirement.RequirementID, CorrelationID: req.CorrelationID,
		WorkflowInstanceID: req.Continuation.InstanceID, NodeID: req.Continuation.TargetNodeID,
		ProposalRef: req.Proposal.Revision.MaterialDigest.Digest, SubjectRefs: req.SubjectRefs,
		PolicyRouteRef: "route.promotion.execution-authority/v1", Visibility: workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: req.Proposal.Revision.OrganizationScopeID,
		// The stored deadline is the compiled requirement's own, already
		// truncated to the second it will be read back at.
		DeadlineAt: requirement.Deadline.Expiry.Time(), CreatedAt: req.CreatedAt,
	}, requirement.RequirementID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	store := workitem.Store{}
	meta := workitem.TransitionMeta{
		ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.created", At: req.CreatedAt,
	}
	created, err := store.Create(ctx, ex, item, meta)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	resolution := humanwork.Resolution{
		RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision,
		Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{
			{PrincipalID: owner, Via: humanwork.SourceDirect, TermRef: termRef},
		},
		ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
		DirectoryVersion:  directoryVersion,
		ExpressionDigest:  requirement.ExpressionDigest,
		RequirementDigest: requirement.Digest(),
		QuorumRequired:    requirement.Quorum.MinApprovals,
	}
	assignment := workitem.Assignment{
		Resolution: resolution, GovernancePolicyRef: requirement.Source.GovernancePolicyRef,
		Trigger: workitem.TriggerInitialRouting, ChosenOwner: owner,
	}
	return store.Route(ctx, ex, created.TenantID, created.WorkItemID, created.ItemVersion, assignment,
		workitem.TransitionMeta{
			ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.routed", At: req.CreatedAt,
		})
}

func (f promotionWorkItems) createExecuteTask(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	approver := f.managerApprover
	if approver == "" {
		var err error
		approver, err = promotionexec.ManagerApproverFor(f.approver)
		if err != nil {
			return workitem.WorkItem{}, fmt.Errorf("platform execution: derive the reapproval owner: %w", err)
		}
	}
	item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
		Kind: workitem.KindTask, WorkType: "task.promotion.reapproval/v1", CorrelationID: req.CorrelationID,
		WorkflowInstanceID: req.Continuation.InstanceID, NodeID: req.Continuation.TargetNodeID,
		ProposalRef: req.Proposal.Revision.MaterialDigest.Digest, SubjectRefs: req.SubjectRefs,
		PolicyRouteRef: "route.promotion.hr_business_partner/v1", Visibility: workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: req.Proposal.Revision.OrganizationScopeID,
		DeadlineAt:          req.CreatedAt.Add(approvalDecisionWindow), CreatedAt: req.CreatedAt,
	})
	if err != nil {
		return workitem.WorkItem{}, err
	}
	store := workitem.Store{}
	meta := workitem.TransitionMeta{ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.created", At: req.CreatedAt}
	created, err := store.Create(ctx, ex, item, meta)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	resolution := humanwork.Resolution{
		RequirementID: "task.promotion.reapproval/v1", RequirementRevision: 1, Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{{PrincipalID: approver, Via: humanwork.SourceDirect, TermRef: "term:execution-authority-approver"}},
		ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt), DirectoryVersion: "directory.execution-authority/1",
		ExpressionDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", RequirementDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000", QuorumRequired: 1,
	}
	return store.Route(ctx, ex, created.TenantID, created.WorkItemID, created.ItemVersion, workitem.Assignment{Resolution: resolution, GovernancePolicyRef: "policy.promotion.reapproval/v1", Trigger: workitem.TriggerInitialRouting, ChosenOwner: approver}, meta)
}

// executeDriverAdapter adapts *execute.Driver to app.ProposalExecutor. It is
// the one place this package's own request/result shapes and
// internal/intent/app's port-owned shapes convert into one another, so
// internal/intent/app never needs to import internal/workflow/execute
// itself.
type executeDriverAdapter struct{ driver *execute.Driver }

var _ app.ProposalExecutor = executeDriverAdapter{}

func (a executeDriverAdapter) Execute(ctx context.Context, start runtime.StartRequest) (app.ExecutionResult, error) {
	result, err := a.driver.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		return app.ExecutionResult{}, noActiveVersion(err)
	}
	instanceID, err := executionResultInstanceID(result, result.Start.InstanceID.String())
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, instanceID), nil
}

// executionResultInstanceID reads RESOLVED identity only from the durable
// proof, never from the intentionally empty original StartReceipt.
func executionResultInstanceID(result execute.Result, legacyInstanceID string) (string, error) {
	if result.Status != execute.StatusResolved {
		return legacyInstanceID, nil
	}
	if result.StartResolution == nil || result.StartResolution.Instance.InstanceID == uuid.Nil {
		return "", fmt.Errorf("%w: resolved START has no durable instance proof", transactioncommit.ErrCommitAmbiguous)
	}
	return result.StartResolution.Instance.InstanceID.String(), nil
}

func (a executeDriverAdapter) Resume(ctx context.Context, req app.ExecutionResumeRequest) (app.ExecutionResult, error) {
	result, err := a.driver.Resume(ctx, execute.ResumeRequest{
		Start: req.Start, InstanceID: req.InstanceID, ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		// WF-RUN-028: only the WorkItem's identity and expected version cross
		// this boundary -- the driver reloads the row itself, through
		// workitem.Store, rather than trust req.WorkItem's own fields.
		WorkItemID: req.WorkItem.WorkItemID, ExpectedWorkItemVersion: req.WorkItem.ItemVersion,
		Outcome: req.Outcome,
	})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, req.InstanceID.String()), nil
}

// awaitingContinuationKinds are the [frontier.IntentKind] values that leave
// an instance parked, waiting on a durable continuation to be satisfied.
// IntentReady and IntentComplete are not: the former is ordinary work the
// driver already drained before returning, and the latter is the terminal
// itself, not something still to wait on.
var awaitingContinuationKinds = map[frontier.IntentKind]bool{
	frontier.IntentWorkItemRequired:           true,
	frontier.IntentSignalSubscriptionRequired: true,
	frontier.IntentTimerRequired:              true,
}

// adaptExecutionResult projects one execute.Result onto app.ExecutionResult.
// instanceID is passed as its already-rendered string form (rather than the
// google/uuid.UUID type itself, which this package must not leak past its
// own adapter boundary) because execute.Result.Start (which itself carries
// an instance id) is only populated by Execute, never by Resume.
//
// WF-RUN-032: ParkedContinuationRefs and ParkedWorkItems are built as two
// separate typed lists -- the durable continuation record a frontier intent
// raised, and the durable WorkItem it may have raised -- because the
// deprecated ParkedContinuations string field below (kept only for wire
// compatibility) was found naming work-item ids under a continuation name.
func adaptExecutionResult(result execute.Result, instanceID string) app.ExecutionResult {
	visited := make([]string, 0, len(result.Advances))
	for _, adv := range result.Advances {
		visited = append(visited, adv.NodeID)
	}
	parked := make([]string, 0, len(result.WorkItems))
	for _, item := range result.WorkItems {
		parked = append(parked, item.WorkType+":"+item.WorkItemID.String())
	}
	continuations := make([]app.ContinuationRef, 0)
	for _, adv := range result.Advances {
		for _, rec := range adv.Continuations {
			if !awaitingContinuationKinds[rec.Kind] {
				continue
			}
			id := runtime.ContinuationID(rec.TenantID, rec.InstanceID, rec.SourceNodeID, rec.SourceAttempt, rec.TargetNodeID, rec.Kind)
			continuations = append(continuations, app.ContinuationRef{
				ContinuationID: id.String(), Kind: string(rec.Kind), TargetNodeID: rec.TargetNodeID,
			})
		}
	}
	workItems := make([]app.WorkItemRef, 0, len(result.WorkItems))
	for _, item := range result.WorkItems {
		workItems = append(workItems, app.WorkItemRef{
			WorkItemID: item.WorkItemID.String(), Kind: string(item.Kind), NodeID: item.NodeID,
		})
	}
	out := app.ExecutionResult{
		Parked:          result.Status == execute.StatusParked,
		InstanceID:      instanceID,
		InstanceVersion: result.InstanceVersion,
		VisitedNodes:    visited,
		//lint:ignore SA1019 wire compatibility: the deprecated port field feeds parked_continuations on the wire; the typed refs beside it are the replacement.
		ParkedContinuations:    parked,
		ParkedContinuationRefs: continuations,
		ParkedWorkItems:        workItems,
		EvidenceIDs:            append([]string(nil), result.EvidenceIDs...),
	}
	switch result.Status {
	case execute.StatusParked:
		out.Status = app.ExecutionResultParked
	case execute.StatusComplete:
		out.Status = app.ExecutionResultComplete
	case execute.StatusResolved:
		out.Status = app.ExecutionResultResolved
		if result.StartResolution != nil {
			resolved := result.StartResolution
			out.InstanceID = resolved.Instance.InstanceID.String()
			out.InstanceVersion = resolved.Instance.InstanceVersion
			out.ResolvedStart = &app.ResolvedStartState{
				RuntimeStatus: resolved.Instance.RuntimeStatus, CurrentNodeIDs: append([]string(nil), resolved.Instance.CurrentNodeIDs...),
				WorkflowID: resolved.Instance.WorkflowID, WorkflowVersion: resolved.Instance.WorkflowVersion,
				CompiledPlanDigest: resolved.Instance.CompiledPlanHash, SemanticVersion: resolved.SemanticVersion,
				Lifecycle: resolved.Instance.CompletionDimensions,
			}
		}
	}
	return out
}
