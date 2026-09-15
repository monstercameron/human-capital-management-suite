package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/orgfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/timeauth"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"

	"github.com/google/uuid"
)

// ObservationFreshnessBudget is how old a page's watermark may be before the
// connectivity plane reports it as stale. It is the connector contract's own
// budget, and it is a different question from the comparison's freshness
// policy: one judges the page, the other judges whether a finding drawn from
// it may be called a mismatch.
const ObservationFreshnessBudget = 24 * time.Hour

// CellConfig is everything a P1A cell needs that it cannot decide for itself.
type CellConfig struct {
	// Store is the persistence port. Required.
	Store Store
	// Verifier turns a presented credential into a principal. Required: a
	// listener that cannot authenticate must not start.
	Verifier trust.Verifier
	// Audience is the audience this cell answers to.
	Audience string
	// MaxDeadline caps every request's deadline. Zero means the transport
	// default.
	MaxDeadline time.Duration
	// Logger receives one structured record per completed request.
	Logger transport.Logger
	// Inputs resolves the governed reads a P1A intent needs. Nil means
	// [NewFixtureInputs], and then Workers and Bands default to the same
	// corpus.
	Inputs DomainInputs
	// Workers is the governed worker read port the people capability answers
	// from. Nil is only valid when Inputs is nil too.
	Workers people.WorkerFacts
	// Bands is the pay band catalog the rewards and promotion capabilities
	// evaluate against. Nil is only valid when Inputs is nil too.
	Bands rewards.PayBandCatalog
	// Incumbent is the external system of record the cross-system
	// diagnostics observe. Nil means the in-memory fake incumbent, which is
	// the P1A design-partner stand-in until a real provider edition is
	// selected (planning/next-steps.md M1).
	Incumbent connectivity.Connector
	// Connection is the tenant-scoped connection Incumbent is read under. Nil
	// means a connection drafted and enabled against the fake incumbent's own
	// published definition.
	Connection *connectivity.ConnectorConnection
	// Observations persists the immutable observation evidence every
	// comparison rests on. Nil means an in-memory store.
	Observations observe.ObservationStore
	// IDs and Clock are seams for deterministic composition. Nil means
	// UUIDv7 and the trusted host clock.
	IDs   intent.IDSource
	Clock intent.Clock
	// Now supplies the capability gateway's evidence timestamps and the
	// trusted clock's readings. Nil means time.Now in UTC.
	Now func() time.Time
	// Workspace controls whether the HTTP edge serves the human-facing
	// Promotion workspace (internal/humanwork/workspace) alongside the RPC
	// projection. Nil means served.
	//
	// It is a pointer because the default is on and the zero value of a bool
	// is off: a deployment that wants the API surface without the human one
	// has to say so, rather than getting it by forgetting to set a field.
	Workspace *bool
	// DevBrowserLogin enables the workspace's dev-only pasted-token sign-in
	// flow (internal/humanwork/workspace's PathLogin/PathLogout). Off by
	// default. It is a plain value, read back off the composed Cell by
	// internal/transport/cell, which is the package that actually knows
	// about internal/humanwork/workspace.Options.DevBrowserLogin - this
	// package must not import anything transport-shaped to state it.
	DevBrowserLogin bool
	// DevPersonas are immutable server-issued identities for the explicitly
	// enabled local browser login surface.
	DevPersonas []workspace.DevPersona
	// PublicOrigin is the canonical http(s) origin ("scheme://host[:port]")
	// this cell is publicly reached at, in the form the composition root's
	// own validation produced. Empty means the cell is reached directly: its
	// HTTP edge then derives every browser-facing authority from each
	// request. Set it when a proxy terminates TLS or rewrites Host on the
	// way in - the browser's Origin, the tunnel address the shells emit and
	// their connect-src all have to name the public authority, which the
	// request can no longer carry. Like DevBrowserLogin, this package only
	// carries the value; internal/transport/cell is what binds it into the
	// browser policy, the workspace handler and the tunnel's origin check.
	PublicOrigin string
	// Evidence is the sink every decision this cell records lands on: the
	// capability gateway's invocation/refusal evidence (CAP-002),
	// ExecuteIntent's GATE_ADMITTED/GATE_REFUSED entries (OBS-024) and the
	// journey's workforce decisions. Nil means a fresh sink, which is what
	// every composition before the journey used. A composition root that
	// also builds the execution driver passes the same sink to
	// internal/platform/execution's PromotionExecutionConfig.Evidence, so
	// the driver's own APPROVAL_COMPLETED/TASK_SUBMITTED/TERMINAL_WRITTEN
	// evidence is on the one list [Cell.Evidence] exposes and the journey's
	// Inspect reads.
	Evidence *MemoryEvidenceSink
	// Telemetry is the OTel provider every request is instrumented through.
	// Nil means off: a cell composed with no Telemetry publishes no spans or
	// metrics at all, rather than falling back to some default exporter a
	// caller did not ask for. Like DevBrowserLogin, this package only carries
	// the value; internal/transport/cell is what chains
	// otelmw.UnaryServerInterceptor/otelmw.NewConnectInterceptor from it,
	// because only internal/transport may import connect/grpc (LIB-003).
	Telemetry *hcmotel.Provider
	// WorkflowRecorder receives the governed workflow controls' operations
	// (spans and structured logs). Nil records nothing unless a caller
	// context carries its own recorder.
	WorkflowRecorder WorkflowRecorder
	// Preferences persists authenticated presentation state. It is optional
	// for non-workspace compositions; the product RPC reports UNAVAILABLE
	// when omitted rather than silently falling back to browser storage.
	Preferences preferences.Store
	// RoleAccess owns tenant-configurable role definitions, employee role
	// assignments, and per-role organization-directory boundaries.
	RoleAccess roleaccess.Store
	// WorkerIDs owns organization-scoped worker-number policy and atomic
	// reservations. Nil keeps legacy UUID-derived numbers for non-workspace
	// compositions.
	WorkerIDs workerids.Store

	// Idempotency composes ENDPOINT-004's Coordinator for SubmitIntent,
	// CancelIntent and SupersedeIntent (EP-INTENT-003). Nil means a fresh
	// [endpoint.NewCoordinator]: unlike ExecutionAuthority/Executor, an
	// idempotent replay guard for a governed write carries no authority of
	// its own, so a composed cell gets one by default rather than by opt-in.
	Idempotency *endpoint.Coordinator
	// SafePoints resolves CancelIntent's safe-point question for an
	// EXECUTING intent (WF-RUN-010). Nil leaves every such intent answered
	// as [intent.CancellationPointUnknown] (CANCELLATION_PENDING): no
	// composition here wires a real workflow safe-point reader yet, so a
	// cell never claims a clean cancel it cannot prove.
	SafePoints SafePoints

	// Executor is the caller-driven workflow driver ExecuteIntent runs an
	// approved promotion proposal through. Nil (the default for every
	// composition today) leaves EXECUTE unavailable regardless of
	// ExecutionAuthority: P1A cells never execute.
	//
	// internal/transport/cell, not this package, is what may build a real
	// *internal/workflow/execute.Driver and adapt it to this port: only
	// internal/transport may own that composition (see this file's own
	// Telemetry/DevBrowserLogin split for why).
	Executor ProposalExecutor
	// ExecutionAuthority is the explicit P1B gate ExecuteIntent requires
	// before it will run Executor at all. Nil means this cell behaves
	// byte-for-byte like the P1A cell of today.
	ExecutionAuthority *ExecutionAuthority
	// BindPromotionSteps, when set, receives this cell's governed
	// [PromotionStepServices] once the gateway exists (WF-RUN-034): the
	// composition root hands them to the execution driver it composed before
	// the cell. It requires ExecutionAuthority.
	BindPromotionSteps func(*PromotionStepServices) error
	// ExecutionResolver and ExecutionVersions are Executor's own workflow
	// resolver and version store. Required together with Executor; either
	// missing leaves ExecuteIntent refusing as unavailable past the
	// authority gate.
	ExecutionResolver runtime.WorkflowResolver
	ExecutionVersions workflowversion.Store
	// ExecutionCellID names the cell runtime.StartRequest.CellID records.
	// Empty means "cell-local".
	ExecutionCellID string
	// TenantUUID maps a tenant key onto the uuid this cell's composed Store
	// uses for that tenant's row (internal/intent/app/pgstore.TenantID,
	// wrapped, in every real composition). Required together with Executor.
	TenantUUID func(values.TenantId) uuid.UUID

	// ExecutionDB is the database the Promotion journey engine reads the
	// durable execution record through (the workflow instance, its node
	// executions, its WorkItems and their transitions, and the one ledger
	// fact the END node recorded) and claims/completes the approval WorkItem
	// in. Every statement runs inside a tenant-scoped transaction
	// (internal/data/tenancy.WithTenant), because every one of those tables is
	// row-level-security protected.
	//
	// It is the same pool CellConfig.Executor's own driver was composed over
	// in every real composition; it is a separate field because
	// [ProposalExecutor] is a narrow port that deliberately exposes no
	// database at all, and the journey engine needs one to read back what the
	// driver wrote. Nil leaves [Cell.Journey] nil.
	ExecutionDB dbport.Beginner
	// ExecutionFacts optionally supplies a composition-time fact source for
	// execution. Production compositions leave this nil so NewCell derives
	// DurableProposalFacts from ExecutionDB; an explicit source is useful for
	// a non-durable composition test and is never populated from a request.
	ExecutionFacts ExecutionFacts
	// LegalEvidence authenticates signed evaluation receipts and their exact
	// proposal bindings using deployment-configured trusted issuer keys.
	LegalEvidence LegalEvidenceVerifier
	// ExecutionApprover is the principal id the one approval WorkItem this
	// cell's promotion workflow raises is routed to, and therefore the
	// principal the journey engine records the decision as. Empty means
	// [DefaultJourneyApprover].
	//
	// It must be the same principal the composed Executor's own work-item
	// factory routes to (internal/platform/execution's
	// PromotionExecutionConfig.ApproverPrincipalID): the routed assignment is
	// what authorizes the decision, so a disagreement here is refused by
	// internal/workflow/steps/approval.Complete rather than silently accepted.
	ExecutionApprover string
	// ApprovalAuthority re-resolves a promotion approval's authority from
	// current durable facts when it is decided (WF-STEP-003). It must be
	// built from the same routing configuration as Executor's work-item
	// factory (internal/platform/execution.NewPromotionApprovalAuthority over
	// the same PromotionExecutionConfig). Nil refuses every promotion-class
	// approval decision rather than approving on routed facts alone.
	ApprovalAuthority ApprovalAuthoritySource
}

// Cell is one composed P1A application cell: the registries, the governed
// gateway and the application service. It carries everything a transport
// composition needs to publish it, but does not publish it itself -
// internal/transport/cell attaches the gRPC server and the HTTP/Connect edge
// (including the OTel interceptors and the human-facing workspace), because
// only internal/transport may import grpc-go/Connect/protobuf directly
// (LIB-003; internal/intent/app is not on that qualification's allowed-roots
// list).
//
// Composition lives here rather than in cmd/hcmnext so that the bootstrap test
// runs the same wiring the binary runs. A cell a test assembles differently
// from the way the process assembles it proves nothing about the process.
type Cell struct {
	Service      *IntentService
	Definitions  *intent.Registry
	Capabilities *capability.Registry
	// Workers and Transactions are the governed read ports the operator
	// surface (internal/transport/admin) forwards to. They are the same
	// values the capability handlers answer from, so an operator never
	// reads through a second path.
	Workers      people.WorkerFacts
	Transactions intelligence.TransactionHistory
	Gateway      *capability.Gateway
	Evidence     *MemoryEvidenceSink
	Controls     Controls
	Config       transport.Config
	Inputs       DomainInputs

	// Incumbent, Connection and Observations are the connectivity plane this
	// cell observes the external system of record through. They are exposed so
	// an operator surface - and the conformance suite - can read the call log
	// and the recorded evidence without a second connection.
	Incumbent    connectivity.Connector
	Connection   *connectivity.ConnectorConnection
	Observations observe.ObservationStore

	// Clock is the temporal-evidence monitor behind the recording clock.
	Clock *timeauth.Monitor
	// Discovery is the rendered API-001 served shape.
	Discovery *manifest.DiscoveryDocument
	// Telemetry is the OTel provider from CellConfig, or nil. Exported so
	// internal/transport/cell can read it without this package exposing any
	// transport-shaped composition of its own.
	Telemetry   *hcmotel.Provider
	Preferences preferences.Store
	RoleAccess  roleaccess.Store
	WorkerIDs   workerids.Store

	// Journey is the live Promotion-journey engine the workspace's journey
	// page reads and acts through. It is non-nil only on a cell composed with
	// both a [ProposalExecutor] and a [CellConfig.ExecutionDB]: the journey is
	// the execution path made visible, and a cell that cannot execute has no
	// journey to show. A nil Journey travels to
	// internal/humanwork/workspace.Options unchanged, and the page reports
	// workspace.ErrJourneyUnavailable from its own nil check.
	Journey workspace.JourneyEngine

	// WorkflowControl and WorkflowTenantIDs are EP-WF-002's governed workflow
	// controls. Both are nil on a cell composed without an execution database
	// and tenant mapping; the workflow transport then refuses every control.
	WorkflowControl   *workflowcontrol.Controller
	WorkflowTenantIDs workflowcontrol.TenantIDs

	// locateWorker is the one worker-reference resolver this cell composed.
	// Every surface that accepts a reference somebody typed -- the
	// domain-input resolver, the workspace's read surface, the journey engine
	// -- is handed this value, so the same string always names the same
	// person. It is unexported for the same reason the two flags below are:
	// resolution is decided at composition, and a surface that could swap it
	// afterwards would be a second answer to "who is this".
	locateWorker WorkerLocator

	// positionReader is PROMOUX-004's real position.PositionFacts adapter
	// (internal/data/positionfacts), composed on the same condition as the
	// layered worker read below: without an execution database and a
	// tenant mapping there is no job_position table to read. Nil on a cell
	// composed without both, exactly like locateWorker falling back to the
	// corpus-only locator -- the workspace's read surface treats a nil
	// reader as "no position selected can be checked", never as "anything
	// goes".
	positionReader position.PositionFacts

	// managerFacts is PROMOUX-005's real org.WorkerFacts adapter
	// (internal/data/orgfacts), composed on the same condition as
	// positionReader above: without an execution database and a tenant
	// mapping there is no journey_worker table to walk a reporting chain
	// over. Nil on a cell composed without both -- the workspace's read
	// surface and any target-manager selection then treat a candidate as
	// unprovable, never as pre-authorized.
	managerFacts org.WorkerFacts

	// workspaceEnabled records whether the edge publishes the human-facing
	// workspace. It is not exported: whether a surface is served is decided
	// at composition, and a handler that could be switched on afterwards
	// would be a served shape the discovery document had already denied.
	// [Cell.WorkspaceEnabled] is the read-only accessor.
	workspaceEnabled bool
	// devBrowserLogin records whether the workspace's dev-only sign-in flow
	// is enabled. Same reasoning as workspaceEnabled: fixed at composition,
	// read through [Cell.DevBrowserLogin].
	devBrowserLogin bool
	devPersonas     []workspace.DevPersona
	// publicOrigin records the deployment's declared public origin. Same
	// reasoning as workspaceEnabled: fixed at composition, read through
	// [Cell.PublicOrigin].
	publicOrigin string
}

// NewCell composes a cell.
func NewCell(cfg CellConfig) (*Cell, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("app: a Store is required to compose a cell")
	case cfg.Verifier == nil:
		return nil, errors.New("app: a trust.Verifier is required to compose a cell")
	}

	defs, err := definitions.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("app: compile the definition catalog: %w", err)
	}
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		return nil, fmt.Errorf("app: build the canonical digester: %w", err)
	}

	inputs, workers, bands := cfg.Inputs, cfg.Workers, cfg.Bands
	var fixtureBacked *FixtureInputs
	if inputs == nil {
		fixtureBacked, err = NewFixtureInputs()
		if err != nil {
			return nil, err
		}
		inputs = fixtureBacked
		if workers == nil {
			workers = fixtureBacked.Workers()
		}
		if bands == nil {
			bands = fixtureBacked.Bands()
		}
	}
	if workers == nil || bands == nil {
		return nil, errors.New("app: a cell with its own DomainInputs must also supply Workers and Bands")
	}

	// A cell that can execute can also create employees, and a created
	// employee has to be visible to every governed read this cell performs -
	// the capability gateway's explain_worker_state handler, the workspace's
	// read surface and the journey engine's own current-placement read alike.
	// So the durable population is layered onto the corpus reader exactly
	// once, here, and the composed value is what every one of those paths is
	// handed. There is deliberately no second reader for created workers: a
	// second read path is a second place a field can be disclosed from, and
	// the whole point of routing everything through one WorkerFacts is that
	// the authorization decision and the recorded evidence are the same
	// whichever population the worker came from.
	//
	// The condition is the journey's own: without an execution database there
	// is no journey_worker table to read, and without a tenant mapping there
	// is no way to scope the read to a tenant. Either missing leaves the
	// corpus reader untouched, so a cell composed the way P1A composed one
	// behaves exactly as it did.
	if cfg.ExecutionDB != nil && cfg.TenantUUID != nil {
		workers = workforce.NewLayeredWorkerFacts(workers, cfg.ExecutionDB, cfg.TenantUUID)
	}
	// The domain-input resolver pins a promotion's baseline through its own
	// worker read, and a corpus-backed one loaded a private corpus reader in
	// its constructor. Rebinding it to the composed value is what keeps "one
	// cell-level WorkerFacts" true rather than aspirational: without it a
	// created worker would be visible to the capability handlers and invisible
	// to the simulation that has to certify the promotion.
	if fixtureBacked != nil {
		fixtureBacked.BindWorkers(workers)
	}
	// The one worker-reference resolver, composed on the same condition as
	// the layered read: without a database and a tenant mapping there is no
	// created population to resolve against, and the corpus locator is what
	// this cell always used.
	locateWorker := newWorkerLocator(cfg.ExecutionDB, cfg.TenantUUID)
	if fixtureBacked != nil {
		fixtureBacked.BindWorkerLocator(locateWorker)
	}

	// PROMOUX-004: the same condition as the layered worker read above.
	// Without an execution database and a tenant mapping there is no
	// job_position table this cell could read at all, and positionReader
	// stays nil -- which the workspace's read surface and the domain-input
	// resolver both treat as "cannot be checked", not "assume valid".
	var positionReader position.PositionFacts
	if cfg.ExecutionDB != nil && cfg.TenantUUID != nil {
		positionReader = positionfacts.Reader{DB: cfg.ExecutionDB, TenantUUID: cfg.TenantUUID}
	}
	if fixtureBacked != nil {
		fixtureBacked.BindPositionReader(positionReader)
	}

	// PROMOUX-005: the same condition as positionReader above. Without an
	// execution database and a tenant mapping there is no journey_worker
	// table this cell could walk a reporting chain over, and managerFacts
	// stays nil -- which the workspace's read surface and
	// evaluateTargetManagerSelection both treat as "cannot be checked", not
	// "assume safe".
	var managerFacts org.WorkerFacts
	if cfg.ExecutionDB != nil && cfg.TenantUUID != nil {
		managerFacts = orgfacts.NewReader(cfg.ExecutionDB, cfg.TenantUUID)
	}

	incumbent, connection, err := resolveConnectivity(cfg)
	if err != nil {
		return nil, err
	}
	observations := cfg.Observations
	if observations == nil {
		observations = observe.NewMemoryStore()
	}
	external, err := NewExternalObservations(incumbent, connection, observations, ObservationFreshnessBudget)
	if err != nil {
		return nil, err
	}
	if fixtureBacked != nil {
		fixtureBacked.BindExternalSource(external.SourceRef())
	}

	handlers := &domainHandlers{
		workers:      workers,
		bands:        bands,
		history:      &workerFieldHistory{workers: workers},
		observations: external,
		transactions: &ledgerTransactions{store: cfg.Store, defs: defs},
	}

	caps, err := newCapabilityRegistry(handlers)
	if err != nil {
		return nil, err
	}
	sink := cfg.Evidence
	if sink == nil {
		sink = NewMemoryEvidenceSink()
	}
	var gatewayOptions []capability.GatewayOption
	if cfg.Now != nil {
		gatewayOptions = append(gatewayOptions, capability.WithClock(cfg.Now))
	}
	gateway := capability.NewGateway(caps, sink, gatewayOptions...)
	controls := NewControls(defs, caps)

	// The recording clock is monitored, not read directly: every instant this
	// cell stamps onto the chronology has passed a drift and uncertainty check
	// first. A caller-supplied Clock still wins, because a deterministic
	// composition (a fixture, a replay) is pinning the time on purpose.
	monitor, err := timeauth.NewMonitor(newHostClock(cfg.Now), timeauth.Options{})
	if err != nil {
		return nil, fmt.Errorf("app: build the temporal-evidence monitor: %w", err)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = NewTrustedClock(monitor)
	}

	svc, err := NewIntentService(Options{
		Definitions:  defs,
		Capabilities: caps,
		Gateway:      gateway,
		Store:        cfg.Store,
		Inputs:       inputs,
		Digester:     digester,
		Controls:     controls,
		IDs:          cfg.IDs,
		Clock:        clock,

		ProposalExecutor:   cfg.Executor,
		ExecutionAuthority: cfg.ExecutionAuthority,
		ExecutionResolver:  cfg.ExecutionResolver,
		ExecutionVersions:  cfg.ExecutionVersions,
		ExecutionCellID:    cfg.ExecutionCellID,
		TenantUUID:         cfg.TenantUUID,
		// WF-RUN-027: a cell composed with the execution database resolves a
		// bound proposal revision's approval decisions and supersession from
		// migration 00024's own rows rather than from the approval flags its
		// caller presented. A cell composed without that database has nowhere
		// to read those facts from and cannot start an execution.
		ExecutionFacts: executionFactsForConfig(cfg),
		// OBS-024: GATE_REFUSED/GATE_ADMITTED land on the same evidence sink
		// as every CAP-002 invocation/refusal, so Cell.Evidence reads both
		// back from one place.
		Evidence:      sink,
		LegalEvidence: cfg.LegalEvidence,

		Idempotency: idempotencyOf(cfg),
		SafePoints:  cfg.SafePoints,
	})
	if err != nil {
		return nil, err
	}

	endpoints, err := manifest.Build()
	if err != nil {
		return nil, fmt.Errorf("app: build the endpoint manifest: %w", err)
	}
	discovery, err := manifest.RenderDiscoveryDocument(endpoints, defs.Definitions(), caps.List())
	if err != nil {
		return nil, fmt.Errorf("app: render the discovery document: %w", err)
	}

	workspaceEnabled := cfg.Workspace == nil || *cfg.Workspace

	// The journey engine is composed only when this cell can actually run one:
	// a driver to execute through and the database that driver wrote to. A
	// typed-nil interface would defeat the page's own nil check, so the field
	// is left at its zero value rather than assigned a nil *journeyEngine.
	var journey workspace.JourneyEngine
	if cfg.Executor != nil && cfg.ExecutionDB != nil {
		engine := newJourneyEngine(svc, cfg.ExecutionDB, cfg.ExecutionApprover, cfg.Now, locateWorker, cfg.WorkerIDs)
		engine.recorder = cfg.WorkflowRecorder
		engine.authority = cfg.ApprovalAuthority
		journey = engine
	}
	workflowControl, workflowTenantIDs, err := composeWorkflowControl(cfg.ExecutionDB, cfg.TenantUUID, cfg.Now, cfg.WorkflowRecorder)
	if err != nil {
		return nil, err
	}

	cell := &Cell{
		Journey:           journey,
		WorkflowControl:   workflowControl,
		WorkflowTenantIDs: workflowTenantIDs,
		WorkerIDs:         cfg.WorkerIDs,
		locateWorker:      locateWorker,
		positionReader:    positionReader,
		managerFacts:      managerFacts,

		workspaceEnabled: workspaceEnabled,
		devBrowserLogin:  cfg.DevBrowserLogin,
		devPersonas:      append([]workspace.DevPersona(nil), cfg.DevPersonas...),
		publicOrigin:     cfg.PublicOrigin,

		Service:      svc,
		Definitions:  defs,
		Capabilities: caps,
		Workers:      workers,
		Transactions: handlers.transactions,
		Gateway:      gateway,
		Evidence:     sink,
		Controls:     controls,
		Inputs:       inputs,
		Incumbent:    incumbent,
		Connection:   connection,
		Observations: observations,
		Clock:        monitor,
		Discovery:    discovery,
		Telemetry:    cfg.Telemetry,
		Preferences:  cfg.Preferences,
		RoleAccess:   cfg.RoleAccess,
		Config: transport.Config{
			Verifier:    cfg.Verifier,
			Audience:    cfg.Audience,
			Now:         cfg.Now,
			MaxDeadline: cfg.MaxDeadline,
			Logger:      cfg.Logger,
		},
	}
	// WF-RUN-034: the execution driver was composed before this cell built
	// the gateway its promotion steps invoke, so the steps bind now.
	if cfg.BindPromotionSteps != nil {
		services, err := NewPromotionStepServices(cell)
		if err != nil {
			return nil, err
		}
		if err := cfg.BindPromotionSteps(services); err != nil {
			return nil, fmt.Errorf("app: bind the promotion step services: %w", err)
		}
	}
	return cell, nil
}

// idempotencyOf returns cfg's own coordinator, or a fresh one. A cell always
// gets a working idempotency coordinator for its governed writes; only the
// choice of instance (shared across cells, or one per cell) is cfg's to make.
func idempotencyOf(cfg CellConfig) *endpoint.Coordinator {
	if cfg.Idempotency != nil {
		return cfg.Idempotency
	}
	return endpoint.NewCoordinator()
}

// resolveConnectivity returns the connector and the usable connection the
// cross-system diagnostics read through.
//
// The default is the in-memory fake incumbent, and that is a deployment fact
// rather than a test convenience: P1A has selected no provider edition
// (planning/next-steps.md M1), so the alternative to a declared stand-in would
// be a cell that cannot answer three of its eight intents at all.
func resolveConnectivity(cfg CellConfig) (connectivity.Connector, *connectivity.ConnectorConnection, error) {
	incumbent := cfg.Incumbent
	if incumbent == nil {
		built, err := fakeincumbent.New(fakeincumbent.Options{})
		if err != nil {
			return nil, nil, fmt.Errorf("app: build the stand-in incumbent: %w", err)
		}
		incumbent = built
	}
	if cfg.Connection != nil {
		return incumbent, cfg.Connection, nil
	}
	connection, err := defaultConnection(incumbent)
	if err != nil {
		return nil, nil, err
	}
	return incumbent, connection, nil
}

// defaultConnection drafts and enables a connection against the connector's
// own published definition.
//
// It walks the real lifecycle - DRAFT, VALIDATING, READY, ACTIVE - with
// recorded evidence at every step rather than constructing an ACTIVE
// connection directly, because the lifecycle is the thing that makes a
// connection auditable and skipping it here would mean the composed cell never
// exercises it.
func defaultConnection(incumbent connectivity.Connector) (*connectivity.ConnectorConnection, error) {
	descriptor := incumbent.Descriptor()
	registry := connectivity.NewRegistry()
	definition := fakeincumbent.DefaultDefinition()
	definition.ConnectorID = descriptor.ConnectorID
	definition.Version = descriptor.Version
	definition.Bounds = incumbent.Bounds()

	published, err := registry.Publish(definition, connectivity.PublicationMeta{
		PublishedBy: "build:hcmnext",
		PublishedAt: connectorPublishedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("app: publish the incumbent connector definition: %w", err)
	}
	credential, err := connectivity.ParseCredentialRef("secretref://hcmnext/incumbent/client")
	if err != nil {
		return nil, err
	}
	connection, err := connectivity.NewConnection(published, connectivity.ConnectionSpec{
		ConnectionID:     descriptor.ConnectionID,
		TenantID:         string(defaultConnectionTenant),
		OrgID:            "org-default",
		SystemID:         "sys-incumbent",
		Environment:      connectivity.EnvironmentSandbox,
		Residency:        "us-east",
		ConnectorID:      published.Definition.ConnectorID,
		ConnectorVersion: published.Definition.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    credential,
		Scopes:           []string{"worker.read", "position.read", "compensation.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"incumbent.invalid"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...),
		Bounds:       published.Definition.Bounds,
		CreatedAt:    connectorPublishedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("app: draft the incumbent connection: %w", err)
	}
	for i, step := range []connectivity.LifecycleState{
		connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive,
	} {
		if err := connection.Transition(step, connectivity.TransitionEvidence{
			Reason:      "cell_composition",
			ActorRef:    "service:hcmnext",
			EvidenceRef: "evd:connection:" + string(step),
			OccurredAt:  connectorPublishedAt.Add(time.Duration(i+1) * time.Minute),
		}); err != nil {
			return nil, fmt.Errorf("app: enable the incumbent connection: %w", err)
		}
	}
	return connection, nil
}

// connectorPublishedAt is the publication and drafting instant of the
// compiled-in connector definition. It is a build constant rather than a clock
// read so that composing this cell twice produces the same connector digest,
// and therefore the same pinned control context.
var connectorPublishedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// defaultConnectionTenant is the tenant the compiled-in connection is scoped
// to. It is the design-partner corpus tenant, because that is the only
// population this release observes.
const defaultConnectionTenant = fixtures.Tenant

// WorkspaceEnabled reports the immutable serving decision made while the cell
// was composed. internal/transport/cell reads it to decide whether to mount
// the HTML workspace beside the RPC edge.
func (c *Cell) WorkspaceEnabled() bool { return c.workspaceEnabled }

// DevBrowserLogin reports the immutable dev-only workspace sign-in decision
// made while the cell was composed. internal/transport/cell reads it when
// building the workspace handler, the same way it reads WorkspaceEnabled.
func (c *Cell) DevBrowserLogin() bool { return c.devBrowserLogin }

// DevPersonas returns a copy of the local-development identities composed for
// the workspace. Production compositions leave this empty.
func (c *Cell) DevPersonas() []workspace.DevPersona {
	return append([]workspace.DevPersona(nil), c.devPersonas...)
}

// PublicOrigin reports the public origin the cell was composed with, or the
// empty string when the deployment reaches the cell directly.
// internal/transport/cell reads it when building the workspace handler, the
// browser policy and the tunnel's origin check.
func (c *Cell) PublicOrigin() string { return c.publicOrigin }
