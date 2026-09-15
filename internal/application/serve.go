package application

// The serve role's composition.
//
// This file is the whole of what "hcmnext serve" is made of. It moved here
// from cmd/hcmnext/main.go unchanged in behaviour and deliberately changed in
// ownership: a command may decide *that* a role runs, this package decides
// *what that role is*. The practical difference is that a test can now
// compose the deployed wiring - the same store, the same verifier, the same
// gateway, the same two transports, the same ordered shutdown - and swap one
// adapter, without rebuilding a parallel version of it and hoping the two
// stayed in step.
//
// Nothing here registers anything at init time, reads a package-level
// variable, or looks a dependency up by name at runtime. Every value below is
// constructed from ServeConfig and Options and passed to whoever needs it.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/legalevidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/preferencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workeridstore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Component names recorded in the composed [Graph]. They are constants so a
// test asserts on the same string the composition wrote, and so a rename is
// one edit rather than a silently diverging golden.
const (
	ComponentConfig                = "config"
	ComponentDatabasePool          = "database-pool"
	ComponentSchemaMigrator        = "schema-migrator"
	ComponentIntentStore           = "intent-store"
	ComponentCredentialVerifier    = "credential-verifier"
	ComponentLegalEvidenceVerifier = "legal-evidence-verifier"
	ComponentTelemetryProvider     = "telemetry-provider"
	ComponentEvidenceSink          = "evidence-sink"
	ComponentDomainInputs          = "domain-inputs"
	ComponentWorkerFacts           = "worker-facts"
	ComponentPayBandCatalog        = "pay-band-catalog"
	ComponentTransactionHistory    = "transaction-history"
	ComponentIncumbentConnector    = "incumbent-connector"
	ComponentObservationStore      = "observation-store"
	ComponentTrustedClock          = "trusted-clock"
	ComponentDiscoveryDocument     = "discovery-document"
	ComponentExecutionAuthority    = "execution-authority"
	ComponentProposalExecutor      = "proposal-executor"
	ComponentWorkflowResolver      = "workflow-resolver"
	ComponentWorkflowVersions      = "workflow-versions"
	ComponentWorkflowInstanceRead  = "workflow-instance-reader"
	ComponentWorkItemQueueRead     = "work-item-queue-reader"
	ComponentCell                  = "cell"
	ComponentIntentDefinitions     = "intent-definitions"
	ComponentCapabilityRegistry    = "capability-registry"
	ComponentCapabilityGateway     = "capability-gateway"
	ComponentIntentService         = "intent-service"
	ComponentJourneyEngine         = "journey-engine"
	ComponentPresentationPrefs     = "presentation-preferences"
	ComponentRoleAccess            = "role-access"
	ComponentGRPCSurface           = "grpc-surface"
	ComponentHTTPEdge              = "http-edge"
	ComponentWorkloadGRPC          = "workload:grpc-surface"
	ComponentWorkloadHTTP          = "workload:http-edge"
	ComponentShutdownHTTP          = "shutdown:stop-http-edge"
	ComponentShutdownGRPC          = "shutdown:stop-grpc-surface"
	ComponentShutdownTelemetry     = "shutdown:shutdown-telemetry"
	workloadNameGRPC               = "grpc-surface"
	workloadNameHTTP               = "http-edge"
	shutdownNameHTTP               = "stop-http-edge"
	shutdownNameGRPC               = "stop-grpc-surface"
	shutdownNameTelemetry          = "shutdown-telemetry"
	httpEdgeReadHeaderTimeoutValue = 10 * time.Second
)

// operationStoreAdapter keeps the transport contract at the composition
// boundary. The data package owns its record and sentinels; application is
// the only layer that translates them into the transport projection.
type operationStoreAdapter struct{ store *operationstore.Store }

var _ transportoperations.Store = operationStoreAdapter{}

func (a operationStoreAdapter) Get(ctx context.Context, tenant, operationID string) (transportoperations.Record, error) {
	record, err := a.store.Get(ctx, tenant, operationID)
	if err != nil {
		return transportoperations.Record{}, mapOperationStoreError(err)
	}
	return transportOperationRecord(record)
}

func (a operationStoreAdapter) Cancel(ctx context.Context, tenant, operationID, idempotencyKey, reason string) (transportoperations.Record, error) {
	record, err := a.store.Cancel(ctx, tenant, operationID, idempotencyKey, reason)
	if err != nil {
		return transportoperations.Record{}, mapOperationStoreError(err)
	}
	return transportOperationRecord(record)
}

func transportOperationRecord(record operationstore.Record) (transportoperations.Record, error) {
	out := transportoperations.Record{
		OperationID: record.OperationID, TenantID: record.TenantID, Owner: record.Owner,
		RequestType: record.RequestType, State: streaming.OperationState(record.State), MetadataRef: record.MetadataRef, CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
	if len(record.ResultBytes) > 0 {
		out.Result = &intentsv1.TypedPayload{}
		if err := protomap.Unmarshal(record.ResultBytes, out.Result); err != nil {
			return transportoperations.Record{}, fmt.Errorf("decode operation result: %w", err)
		}
	}
	if len(record.ErrorBytes) > 0 {
		out.Error = &commonv1.ErrorDetail{}
		if err := protomap.Unmarshal(record.ErrorBytes, out.Error); err != nil {
			return transportoperations.Record{}, fmt.Errorf("decode operation error: %w", err)
		}
	}
	return out, nil
}

func mapOperationStoreError(err error) error {
	switch {
	case errors.Is(err, operationstore.ErrNotFound):
		return transportoperations.ErrNotFound
	case errors.Is(err, operationstore.ErrNotCancellable):
		return transportoperations.ErrNotCancellable
	case errors.Is(err, operationstore.ErrFenced):
		return fmt.Errorf("operation state transition fenced: %w", err)
	default:
		return err
	}
}

// ServeInput is everything ComposeServe needs that is not a decision it makes
// itself: the validated configuration, the database pool bootstrap opened,
// the process logger and identity, and the explicit composition seams.
type ServeInput struct {
	Config ServeConfig
	// Pool is the one pool this process opened. It is required for the
	// PostgreSQL store, the operator surface's workflow-instance reader and
	// the P1B execution driver; a composition that supplies its own store,
	// leaves -execution-authority off and does not exercise the operator
	// read may pass nil.
	Pool     *pgxadapter.Pool
	Logger   bootstrap.Logger
	Identity string
	Options  Options
}

// ComposeServe builds the serve role: the store, the verifier, the telemetry
// provider, the evidence sink, the cell (its intent and capability
// registries, its governed gateway and its application service), the optional
// P1B execution authority, both transports, their listeners and the ordered
// shutdown that drains them.
func ComposeServe(ctx context.Context, in ServeInput) (*App, error) {
	cfg := in.Config
	options, err := optionsForServeConfig(cfg, in.Options)
	if err != nil {
		return nil, err
	}
	logger := in.Logger
	if logger == nil {
		logger = discardLogger{}
	}
	graph := newGraphBuilder(RoleServe)
	graph.add(ComponentConfig, KindConfig, cfg)
	graph.add(ComponentDatabasePool, KindAdapter, in.Pool)

	graph.add(ComponentSchemaMigrator, KindAdapter, options.Migrate)
	if cfg.Migrate {
		if options.Migrate == nil {
			return nil, fmt.Errorf("application: -%s=true needs a schema migrator; the command supplies one", FieldMigrate)
		}
		if err := options.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
			return nil, err
		}
	}

	store, err := composeStore(in.Pool, cfg, options)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentIntentStore, KindAdapter, store, ComponentDatabasePool, ComponentConfig)
	if cfg.Tenant != "" {
		if err := store.Bootstrap(ctx, cfg.Tenant); err != nil {
			return nil, err
		}
		logger.Info("hcmnext.tenant_registered", "tenant", cfg.Tenant, "cell_id", cfg.CellID)
	}
	if cfg.DevBrowserLogin {
		workforceSummary, organizationSummary, seedErr := bootstrapLocalDevWorkforce(ctx, in.Pool, cfg.Tenant)
		if seedErr != nil {
			return nil, seedErr
		}
		if workforceSummary.Planned > 0 {
			logger.Info("hcmnext.local_dev_workforce_ready",
				"tenant", cfg.Tenant, "workers", workforceSummary.Planned, "workers_inserted", workforceSummary.Inserted,
				"organization_units", organizationSummary.UnitsPlanned, "organization_units_inserted", organizationSummary.UnitsInserted)
		}
	}

	verifier, err := composeVerifier(cfg, options)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentCredentialVerifier, KindAdapter, verifier, ComponentConfig)
	keys, keyErr := parseLegalEvidenceIssuerKeys(cfg.LegalEvidenceIssuerKeys)
	if keyErr != nil {
		return nil, keyErr
	}
	var legalEvidence app.LegalEvidenceVerifier
	if len(keys) > 0 {
		if in.Pool == nil {
			return nil, fmt.Errorf("application: -%s requires the database pool", FieldLegalEvidenceIssuerKeys)
		}
		backend := legalevidencestore.NewVerifier(legalevidencestore.New(in.Pool), newLegalEvidenceTenantAuthority(in.Pool, cfg.Tenant), keys...)
		legalEvidence = newLegalEvidenceAdapter(backend)
	}
	graph.add(ComponentLegalEvidenceVerifier, KindAdapter, legalEvidence, ComponentDatabasePool, ComponentConfig)

	newTelemetry := options.NewTelemetry
	if newTelemetry == nil {
		newTelemetry = NewTelemetryProvider
	}
	telemetryProvider, err := newTelemetry(ctx, in.Identity, cfg)
	if err != nil {
		return nil, fmt.Errorf("build telemetry provider: %w", err)
	}
	graph.add(ComponentTelemetryProvider, KindAdapter, telemetryProvider, ComponentConfig)
	telemetryCommitted := false
	defer func() {
		if telemetryCommitted || telemetryProvider == nil {
			return
		}
		LogTelemetryShutdown(logger, telemetryProvider.Shutdown(context.Background()))
	}()

	// One evidence sink for the whole process: the cell's gateway and gate
	// decisions and, when the execution authority is composed, the driver's
	// own execution evidence all land on it, so the journey's Inspect reads
	// one chronology.
	evidence := options.Evidence
	if evidence == nil {
		evidence = app.NewMemoryEvidenceSink()
	}
	graph.add(ComponentEvidenceSink, KindRegistry, evidence)

	workspaceEnabled := cfg.Workspace
	workerIDs := workeridstore.New(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	roleAccess := roleaccessstore.New(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), productFeatureCatalog()...)
	if cfg.Tenant != "" && in.Pool != nil {
		if err := roleAccess.Bootstrap(ctx, kernelvalues.TenantId(cfg.Tenant), "system:bootstrap"); err != nil {
			return nil, fmt.Errorf("bootstrap role access: %w", err)
		}
		if cfg.DevBrowserLogin && cfg.Tenant == demoworkforce.CompanyKey {
			if err := roleAccess.BootstrapLocalDevPersonaPermissions(ctx, kernelvalues.TenantId(cfg.Tenant)); err != nil {
				return nil, fmt.Errorf("bootstrap local development personas: %w", err)
			}
		}
	}
	cellConfig := app.CellConfig{
		Store:            store,
		Verifier:         verifier,
		LegalEvidence:    legalEvidence,
		Audience:         cfg.Audience,
		MaxDeadline:      cfg.MaxDeadline,
		Logger:           transport.LoggerFunc(RequestLogger(logger)),
		Workspace:        &workspaceEnabled,
		DevBrowserLogin:  cfg.DevBrowserLogin,
		DevPersonas:      composeDevPersonas(verifier, cfg, options.Now),
		PublicOrigin:     cfg.PublicOrigin,
		Evidence:         evidence,
		Telemetry:        telemetryProvider,
		WorkflowRecorder: schedulerRecorder(telemetryProvider, logger, options.Now),
		Inputs:           options.Inputs,
		Workers:          options.Workers,
		Bands:            options.Bands,
		Now:              options.Now,
		Clock:            options.Clock,
		IDs:              options.IDs,
		Preferences:      preferencestore.New(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)),
		RoleAccess:       roleAccess,
		WorkerIDs:        workerIDs,
	}
	graph.add(ComponentPresentationPrefs, KindAdapter, cellConfig.Preferences, ComponentDatabasePool)
	graph.add(ComponentRoleAccess, KindAdapter, cellConfig.RoleAccess, ComponentDatabasePool)
	graph.add(ComponentPayBandCatalog, KindPort, cellConfig.Bands)

	if cfg.ExecutionAuthority {
		composeExecution := options.ComposeExecution
		if composeExecution == nil {
			composeExecution = ComposeExecutionAuthority
		}
		if err := composeExecution(&cellConfig, in.Pool, evidence, cfg); err != nil {
			return nil, fmt.Errorf("compose the P1B execution authority: %w", err)
		}
		logger.Info("hcmnext.execution_authority_enabled",
			"role", cfg.ExecutionAuthorityRole, "cell_id", cfg.CellID)
	}
	graph.add(ComponentExecutionAuthority, KindGovernance, cellConfig.ExecutionAuthority, ComponentConfig, ComponentDatabasePool, ComponentEvidenceSink)
	graph.add(ComponentProposalExecutor, KindWorkflow, cellConfig.Executor, ComponentExecutionAuthority)
	graph.add(ComponentWorkflowResolver, KindWorkflow, cellConfig.ExecutionResolver, ComponentExecutionAuthority)
	graph.add(ComponentWorkflowVersions, KindWorkflow, cellConfig.ExecutionVersions, ComponentExecutionAuthority)

	cell, err := app.NewCell(cellConfig)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentCell, KindRegistry, cell,
		ComponentIntentStore, ComponentCredentialVerifier, ComponentTelemetryProvider,
		ComponentEvidenceSink, ComponentLegalEvidenceVerifier, ComponentPayBandCatalog, ComponentProposalExecutor)
	// The governed read ports, the connectivity plane and the trusted clock
	// are recorded as the cell resolved them, not as this root proposed them:
	// a seam left nil is a decision to take the cell's own default corpus,
	// and the graph should say which corpus that turned out to be.
	graph.add(ComponentDomainInputs, KindPort, cell.Inputs)
	graph.add(ComponentWorkerFacts, KindPort, cell.Workers)
	graph.add(ComponentTransactionHistory, KindPort, cell.Transactions)
	graph.add(ComponentIncumbentConnector, KindAdapter, cell.Incumbent)
	graph.add(ComponentObservationStore, KindAdapter, cell.Observations)
	graph.add(ComponentTrustedClock, KindEngine, cell.Clock)
	graph.add(ComponentDiscoveryDocument, KindRegistry, cell.Discovery, ComponentCell)
	graph.add(ComponentIntentDefinitions, KindRegistry, cell.Definitions, ComponentCell)
	graph.add(ComponentCapabilityRegistry, KindRegistry, cell.Capabilities, ComponentCell)
	graph.add(ComponentCapabilityGateway, KindGovernance, cell.Gateway, ComponentCapabilityRegistry)
	graph.add(ComponentIntentService, KindEngine, cell.Service, ComponentCell)
	graph.add(ComponentJourneyEngine, KindWorkflow, cell.Journey, ComponentCell)

	var schedulerWorkload, progressWorkload bootstrap.Workload
	if cfg.Scheduler {
		schedulerWorkload, err = composeSchedulerWorkload(cfg, in.Pool, in.Identity, cell, telemetryProvider, logger, options.Now)
		if err != nil {
			return nil, fmt.Errorf("compose workflow scheduler: %w", err)
		}
		// WF-RUN-020: whatever runs the scheduler also watches for work the
		// scheduler, a lease holder or a person was expected to move and did not.
		progressWorkload, _, err = composeProgressWorkload(cfg, in.Pool, telemetryProvider, logger, options.Now)
		if err != nil {
			return nil, fmt.Errorf("compose workflow progress sweep: %w", err)
		}
	}

	// internal/transport/cell chains the otelmw interceptors itself when this
	// cell was composed with a Telemetry provider (nil, when
	// -otel-exporter=none, means neither call adds one); no interceptor
	// options are passed here. It is the composition adapter, not app.Cell
	// directly, because only internal/transport may import grpc-go/Connect
	// (LIB-003).
	//
	// AdminService.GetWorkflowInstance (ADMIN-008) additionally needs the
	// application-side workflow instance reader, which app.Cell itself has no
	// field for because the operator surface is served whether or not the
	// execution authority is composed; the pool and the same tenant-key
	// derivation ComposeExecutionAuthority uses are in scope here, so this
	// composition root is what builds it.
	workflowInstanceReader := app.NewWorkflowInstanceReader(in.Pool,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	graph.add(ComponentWorkflowInstanceRead, KindPort, workflowInstanceReader, ComponentDatabasePool)
	// EP-WORK-001: the work-item queue reader is composed over the same pool
	// and tenant mapping; it powers WorkService.ListWorkItems/GetWorkItem on
	// both surfaces below.
	workQueueReader := app.NewWorkItemQueueReader(in.Pool,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	graph.add(ComponentWorkItemQueueRead, KindPort, workQueueReader, ComponentDatabasePool)
	operationStore := operationStoreAdapter{store: operationstore.New(in.Pool,
		func(tenant string) string { return pgstore.TenantID(tenant).String() })}

	grpcServer, err := transportcell.NewGRPCServerWithWorkflowInspectorAndOperations(
		cell, workflowInstanceReader, workQueueReader, operationStore, []byte(cfg.DevHMACKey))
	if err != nil {
		return nil, err
	}
	graph.add(ComponentGRPCSurface, KindTransport, grpcServer, ComponentCell, ComponentWorkflowInstanceRead)

	edgeHandler, err := transportcell.NewEdgeHandlerWithTunnelAndDependencies(
		cell, grpcServer, workflowInstanceReader, workQueueReader, operationStore, []byte(cfg.DevHMACKey))
	if err != nil {
		return nil, err
	}
	graph.add(ComponentHTTPEdge, KindTransport, edgeHandler, ComponentCell, ComponentGRPCSurface)

	listen := options.listen()
	grpcListener, err := listen("tcp", cfg.GRPCListen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", cfg.GRPCListen, err)
	}
	httpListener, err := listen("tcp", cfg.HTTPListen)
	if err != nil {
		_ = grpcListener.Close()
		return nil, fmt.Errorf("listen on %s: %w", cfg.HTTPListen, err)
	}
	httpServer := &http.Server{Handler: edgeHandler, ReadHeaderTimeout: httpEdgeReadHeaderTimeoutValue}

	workspacePath := "disabled"
	if workspaceEnabled {
		workspacePath = workspace.PathPromotion
	}
	logger.Info("hcmnext.serving",
		"grpc", grpcListener.Addr().String(),
		"http", httpListener.Addr().String(),
		"discovery", app.DiscoveryPath,
		"tunnel", transportcell.TunnelPath,
		"workspace", workspacePath,
		"dev_browser_login", cfg.DevBrowserLogin,
		"public_origin", cfg.PublicOrigin,
		"otel_exporter", cfg.OTelExporter,
		"definitions", cell.Definitions.Len(),
		"capabilities", len(cell.Capabilities.List()))
	if cfg.DevBrowserLogin {
		loginURL := "http://" + httpListener.Addr().String() + workspace.PathLogin
		logger.Info("hcmnext.dev_browser_login_enabled", "url", loginURL)
		fmt.Fprintf(os.Stdout, "hcmnext: open %s and choose a local persona (or use a bearer credential) to sign in\n", loginURL)
	}

	workloads := []bootstrap.Workload{
		{
			Name: workloadNameGRPC,
			Run: func(context.Context) error {
				if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, net.ErrClosed) {
					return err
				}
				return nil
			},
		},
		{
			Name: workloadNameHTTP,
			Run: func(context.Context) error {
				if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return err
				}
				return nil
			},
		},
	}
	if cfg.Scheduler {
		workloads = append(workloads, schedulerWorkload, progressWorkload)
	}
	// Both surfaces drain gracefully first: in-flight requests finish and new
	// ones are refused. A request that outlives the shutdown deadline is
	// stopped rather than allowed to hold the process open.
	shutdown := []bootstrap.ShutdownStep{
		{Name: shutdownNameHTTP, Run: httpServer.Shutdown},
		{
			Name: shutdownNameGRPC,
			Run: func(stepCtx context.Context) error {
				stopped := make(chan struct{})
				go func() {
					grpcServer.GracefulStop()
					close(stopped)
				}()
				select {
				case <-stopped:
					return nil
				case <-stepCtx.Done():
					grpcServer.Stop()
					<-stopped
					return stepCtx.Err()
				}
			},
		},
		{
			Name: shutdownNameTelemetry,
			Run: func(stepCtx context.Context) error {
				if telemetryProvider == nil {
					return nil
				}
				LogTelemetryShutdown(logger, telemetryProvider.Shutdown(stepCtx))
				return nil
			},
		},
	}
	graph.add(ComponentWorkloadGRPC, KindWorkload, workloads[0].Run, ComponentGRPCSurface)
	graph.add(ComponentWorkloadHTTP, KindWorkload, workloads[1].Run, ComponentHTTPEdge)
	if cfg.Scheduler {
		graph.add(ComponentWorkloadScheduler, KindWorkload, schedulerWorkload.Run, ComponentCell, ComponentDatabasePool)
		graph.add(ComponentWorkloadProgress, KindWorkload, progressWorkload.Run, ComponentDatabasePool, ComponentTelemetryProvider)
	}
	graph.add(ComponentShutdownHTTP, KindShutdown, shutdown[0].Run, ComponentHTTPEdge)
	graph.add(ComponentShutdownGRPC, KindShutdown, shutdown[1].Run, ComponentGRPCSurface)
	graph.add(ComponentShutdownTelemetry, KindShutdown, shutdown[2].Run, ComponentTelemetryProvider)

	// From here the caller's lifecycle owns the provider's lifetime through
	// the ordered shutdown step above. Earlier returns leave this function
	// responsible for cleaning up the partially composed provider.
	telemetryCommitted = true
	return &App{
		role:      RoleServe,
		graph:     graph.graph(),
		logger:    logger,
		cell:      cell,
		grpcAddr:  grpcListener.Addr().String(),
		httpAddr:  httpListener.Addr().String(),
		workloads: workloads,
		shutdown:  shutdown,
		listeners: []net.Listener{grpcListener, httpListener},
	}, nil
}

func productFeatureCatalog() []roleaccess.FeatureDefinition {
	flattened := productui.FlattenFeatureDefinitions()
	result := make([]roleaccess.FeatureDefinition, 0, len(flattened))
	for _, item := range flattened {
		result = append(result, roleaccess.FeatureDefinition{
			PageID: string(item.Page), FeatureID: string(item.Feature.ID),
			View: item.Feature.View, Create: item.Feature.Create,
			Update: item.Feature.Update, Delete: item.Feature.Delete,
		})
	}
	return result
}

func optionsForServeConfig(cfg ServeConfig, options Options) (Options, error) {
	if cfg.LocalDevNow == "" {
		return options, nil
	}
	at, err := time.Parse(time.RFC3339, cfg.LocalDevNow)
	if err != nil || cfg.Profile != ServeProfileLocalDev {
		return Options{}, fmt.Errorf("application: invalid local development clock; validate configuration before composition")
	}
	if options.Now == nil {
		pinned := at.UTC()
		options.Now = func() time.Time { return pinned }
	}
	return options, nil
}

// composeStore builds the persistence adapter, or takes the supplied one.
func composeStore(pool *pgxadapter.Pool, cfg ServeConfig, options Options) (app.Store, error) {
	if options.NewStore != nil {
		store, err := options.NewStore(pool, cfg)
		if err != nil {
			return nil, err
		}
		if store == nil {
			return nil, fmt.Errorf("application: the supplied store factory returned no store")
		}
		return store, nil
	}
	return pgstore.New(pool, pgstore.WithCellID(cfg.CellID))
}

// composeVerifier builds the credential verifier, or takes the supplied one.
func composeVerifier(cfg ServeConfig, options Options) (trust.Verifier, error) {
	if options.NewVerifier != nil {
		verifier, err := options.NewVerifier(cfg)
		if err != nil {
			return nil, err
		}
		if verifier == nil {
			return nil, fmt.Errorf("application: the supplied verifier factory returned no verifier")
		}
		return verifier, nil
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(cfg.DevHMACKey),
		Issuer:   cfg.Issuer,
		Audience: cfg.Audience,
		Now:      options.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("build the credential verifier: %w", err)
	}
	return verifier, nil
}

type developmentTokenIssuer interface {
	Issue(trust.Claims) (string, error)
}

// composeDevPersonas creates local identities only when the operator enabled
// the dev browser login and the configured verifier can issue development
// tokens. A federation verifier therefore never acquires an implicit issuer.
func composeDevPersonas(verifier trust.Verifier, cfg ServeConfig, now func() time.Time) []workspace.DevPersona {
	if !cfg.DevBrowserLogin || cfg.Tenant != demoworkforce.CompanyKey {
		return nil
	}
	issuer, ok := verifier.(developmentTokenIssuer)
	if !ok {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	timestamp := now().UTC()
	// id, workerNumber, access, and purpose are the only facts specific to
	// this demo-tenant binding; the role bundle each persona is issued comes
	// from workspace.DevPersonaRoleSets, the one fixture the sign-in page's
	// derived copy (loginPersonaDescription) and this package's own tests
	// also read, so a persona's promised copy and its signed roles cannot
	// drift apart (UXAUDIT-014 REFACTOR).
	type personaSpec struct {
		id, workerNumber, access, purpose string
	}
	specs := []personaSpec{
		// PROMOUX-015: the four slots are one separated promotion. admin
		// (Rafael Torres, Director of People Operations) is the manager
		// approver -- he manages Linh Tran -- and the execution operator.
		// hiring-manager (Darius Bennett, Chief People Officer) is the
		// proposer: Linh's skip-level manager, so the reference workflow's
		// CurrentManagerOf(worker) approval routes to somebody else.
		// finance-partner (Thomas Baker, Finance Director) is the finance
		// approver the local-dev profile's -execution-finance-partner names.
		// individual-contributor (Linh Tran) is the employee.
		{id: "admin", workerNumber: "HC-21050", access: "HCM administrator", purpose: "compensation_review"},
		{id: "hiring-manager", workerNumber: "HC-21004", access: "Hiring manager", purpose: "compensation_review"},
		// finance-partner replaced UXAUDIT-014's worker_self payroll-manager
		// slot. Its finance_partner role is backed by authz.PolicyTable under
		// compensation_review, the purpose it signs, and by roleaccess's
		// narrow finance_partner page grant. Do not give this slot a payroll
		// label or purpose: no role bundle backs one (UXAUDIT-014).
		{id: "finance-partner", workerNumber: "HC-21054", access: "Finance partner", purpose: "compensation_review"},
		{id: "individual-contributor", workerNumber: "HC-21051", access: "Individual contributor", purpose: "self_service_view"},
	}
	workers, err := demoworkforce.Plan(pgstore.TenantID(cfg.Tenant))
	if err != nil {
		return nil
	}
	workersByNumber := make(map[string]demoworkforce.Employee, len(workers))
	for _, worker := range workers {
		workersByNumber[worker.Row.WorkerNumber] = worker
	}
	personas := make([]workspace.DevPersona, 0, len(specs))
	for _, spec := range specs {
		worker, found := workersByNumber[spec.workerNumber]
		if !found || worker.Row.WorkerKey == "" || worker.Row.LegalName == "" || worker.Row.LifecycleStatus != "active" {
			continue
		}
		roles, ok := workspace.DevPersonaRoles(spec.id)
		if !ok {
			// No canonical role bundle is named for this persona id: issuing
			// an unscoped credential would be worse than not offering the
			// persona at all.
			continue
		}
		token, err := issuer.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: worker.Row.WorkerKey, SubjectKind: "human", Tenant: cfg.Tenant,
			OrganizationScopeID: "org:" + cfg.Tenant + ":people-ops", Roles: roles, Purposes: []string{spec.purpose},
			AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-local-persona-" + spec.id,
			IssuedAtUnix: timestamp.Add(-time.Minute).Unix(), ExpiresAtUnix: timestamp.Add(8 * time.Hour).Unix(),
		})
		if err != nil {
			continue
		}
		access := spec.access
		if access == "" {
			// Derived from this worker's own record (their real job title),
			// not hand-written, so a slot with no access label of its own can
			// only ever say what this specific credential actually is. No
			// current spec leaves access blank; the fallback stays so a future
			// one cannot re-assert a capability its role does not hold.
			access = worker.JobTitle + " (self-service)"
		}
		personas = append(personas, workspace.DevPersona{ID: spec.id, Name: worker.Row.LegalName, Access: access, Roles: roles, Token: token, WorkerRef: worker.Row.WorkerKey})
	}
	return personas
}

// ServeSpec declares the serve role for internal/platform/bootstrap: its
// configuration, its validation, its database dependency and the workloads
// bootstrap runs and drains. It is the one call a command makes.
func ServeSpec(args []string, opts ...Option) bootstrap.Spec {
	options := Options{}.Apply(opts...)
	fields := ServeConfigFieldsForArgs(args)

	// The store needs a pool it can Begin and Query on, and bootstrap's
	// DBPool port is deliberately narrower than that. The factory therefore
	// keeps the adapter for Build while still handing bootstrap the port it
	// owns, so there is exactly one pool, opened and closed once, on
	// bootstrap's schedule rather than on this file's.
	var pool *pgxadapter.Pool

	logger := options.Logger
	if logger == nil {
		logger = slog.New(logging.NewHandler(os.Stdout, logging.WithService("hcmnext")))
	}

	return bootstrap.Spec{
		Role:             bootstrap.RoleHCMNext,
		Args:             args,
		Logger:           logger,
		ConfigFields:     fields,
		Validate:         ValidateServeValues,
		DatabaseURLField: FieldDatabaseURL,
		HealthAddr:       HealthAddrOf(args),
		DBPoolFactory: func(ctx context.Context, url string) (bootstrap.DBPool, error) {
			opened, err := pgxadapter.NewPool(ctx, url, nil)
			if err != nil {
				return nil, fmt.Errorf("connect: %w", err)
			}
			pool = opened
			return opened, nil
		},
		Build: func(ctx context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
			cfg, err := ServeConfigFromValues(deps.Values)
			if err != nil {
				return bootstrap.Runtime{}, err
			}
			composed, err := ComposeServe(ctx, ServeInput{
				Config:   cfg,
				Pool:     pool,
				Logger:   deps.Logger,
				Identity: deps.Identity,
				Options:  options,
			})
			if err != nil {
				return bootstrap.Runtime{}, err
			}
			return composed.Runtime(), nil
		},
		ShutdownDeadline: shutdownGraceForArgs(args),
	}
}

func shutdownGraceForArgs(args []string) time.Duration {
	if requestedServeProfile(args) == ServeProfileLocalDev {
		return time.Second
	}
	return ShutdownGrace
}

// HealthAddrOf pre-resolves -health-addr from args with the same precedence
// bootstrap.Run applies (flag, then environment, then default). Spec.HealthAddr
// is the one setting Run reads before it parses Spec.ConfigFields, so the
// serve role resolves it the way cmd/worker and cmd/scheduler do: a pure,
// side-effect-free rerun of the parse Run performs moments later. A bad flag
// here yields an empty address and is reported, correctly, by Run's own parse.
func HealthAddrOf(args []string) string {
	values, err := bootstrap.ParseConfig(args, nil, ServeConfigFieldsForArgs(args))
	if err != nil {
		return ""
	}
	return values.String(FieldHealthAddr)
}

// RequestLogger emits one structured record per completed request. It is
// deliberately field-by-field rather than a formatted blob: the fields are
// the contract, and the redacting handler behind them is what keeps a request
// log from becoming an export channel.
func RequestLogger(logger bootstrap.Logger) func(transport.LogRecord) {
	return func(record transport.LogRecord) {
		if logger == nil {
			return
		}
		outcome := "OK"
		if !record.Succeeded() {
			outcome = record.Code.String() + " " + record.ReasonRef
		}
		logger.Info("hcmnext.request",
			"method", record.Method,
			"transport", record.Transport,
			"request_id", record.RequestID,
			"tenant", record.TenantID,
			"purpose", record.Purpose,
			"outcome", outcome,
			"error_type", record.ErrorType,
			"duration", record.Duration.String())
	}
}

// discardLogger is the composition's own no-op logger. It exists so
// ComposeServe never has to branch on "are we under test": a caller that has
// no logger gets one that discards, and every log call site below stays
// unconditional.
type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}
