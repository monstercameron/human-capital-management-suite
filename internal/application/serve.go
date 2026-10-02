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
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application/dataopsimport"
	applicationprojectactivity "github.com/monstercameron/human-capital-management-suite/internal/application/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectrefs"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	dataanalytics "github.com/monstercameron/human-capital-management-suite/internal/data/analytics"
	dataconfigbundlekill "github.com/monstercameron/human-capital-management-suite/internal/data/configbundlekill"
	"github.com/monstercameron/human-capital-management-suite/internal/data/configparamstore"
	dataconfigregistry "github.com/monstercameron/human-capital-management-suite/internal/data/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/contentregistrystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dataopsartifactstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dataopsstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/evidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/health"
	"github.com/monstercameron/human-capital-management-suite/internal/data/i18ncatalogstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/integrationregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/legalevidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledgerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/preferencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectcommentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectlinkstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectviewstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workeridstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	platformcache "github.com/monstercameron/human-capital-management-suite/internal/platform/cache"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	telemetrylifecycle "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportadmin "github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	transportconformance "github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	transportparameters "github.com/monstercameron/human-capital-management-suite/internal/transport/parameters"
	transportproject "github.com/monstercameron/human-capital-management-suite/internal/transport/project"
	transportreviewparticipants "github.com/monstercameron/human-capital-management-suite/internal/transport/reviewparticipants"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
	transporttimeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	transportwebhook "github.com/monstercameron/human-capital-management-suite/internal/transport/webhook"
	transportworkorder "github.com/monstercameron/human-capital-management-suite/internal/transport/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// Component names recorded in the composed [Graph]. They are constants so a
// test asserts on the same string the composition wrote, and so a rename is
// one edit rather than a silently diverging golden.
const (
	ComponentConfig                   = "config"
	ComponentDatabasePool             = "database-pool"
	ComponentSchemaMigrator           = "schema-migrator"
	ComponentProjectSchemaMigrator    = "project-schema-migrator"
	ComponentProjectService           = "project-service"
	ComponentWorkOrderSchemaMigrator  = "workorder-schema-migrator"
	ComponentWorkOrderService         = "workorder-service"
	ComponentIntentStore              = "intent-store"
	ComponentCredentialVerifier       = "credential-verifier"
	ComponentLegalEvidenceVerifier    = "legal-evidence-verifier"
	ComponentTelemetryProvider        = "telemetry-provider"
	ComponentTelemetryLifecycle       = "telemetry-lifecycle"
	ComponentEvidenceSink             = "evidence-sink"
	ComponentDomainInputs             = "domain-inputs"
	ComponentWorkerFacts              = "worker-facts"
	ComponentPayBandCatalog           = "pay-band-catalog"
	ComponentTransactionHistory       = "transaction-history"
	ComponentIncumbentConnector       = "incumbent-connector"
	ComponentObservationStore         = "observation-store"
	ComponentTrustedClock             = "trusted-clock"
	ComponentDiscoveryDocument        = "discovery-document"
	ComponentExecutionAuthority       = "execution-authority"
	ComponentConfigBundleControl      = "configbundle-control"
	ComponentConfigBundleReceiver     = "configbundle-receiver"
	ComponentStoreHealth              = "store-health"
	ComponentPilotReliability         = "pilot-reliability"
	ComponentProductSliceReleaseGate  = "product-slice-release-gate"
	ComponentProposalExecutor         = "proposal-executor"
	ComponentWorkflowResolver         = "workflow-resolver"
	ComponentWorkflowVersions         = "workflow-versions"
	ComponentWorkflowControlRead      = "workflow-control-reader"
	ComponentNotificationFeed         = "notification-feed"
	ComponentProviderWebhookReceivers = "provider-webhook-receivers"
	ComponentWorkItemQueueRead        = "work-item-queue-reader"
	ComponentCell                     = "cell"
	ComponentIntentDefinitions        = "intent-definitions"
	ComponentCapabilityRegistry       = "capability-registry"
	ComponentCapabilityGateway        = "capability-gateway"
	ComponentIntentService            = "intent-service"
	ComponentJourneyEngine            = "journey-engine"
	ComponentPresentationPrefs        = "presentation-preferences"
	ComponentRoleAccess               = "role-access"
	ComponentGRPCSurface              = "grpc-surface"
	ComponentHTTPEdge                 = "http-edge"
	ComponentEdgeQualification        = "edge-qualification"
	ComponentWorkloadGRPC             = "workload:grpc-surface"
	ComponentWorkloadHTTP             = "workload:http-edge"
	ComponentShutdownHTTP             = "shutdown:stop-http-edge"
	ComponentShutdownGRPC             = "shutdown:stop-grpc-surface"
	ComponentShutdownTelemetry        = "shutdown:shutdown-telemetry"
	ComponentChatService              = "chat-service"
	ComponentChatDatabasePool         = "chat-database-pool"
	ComponentShutdownChat             = "shutdown:close-chat-database"
	ComponentAgentDatabasePool        = "agent-database-pool"
	ComponentAgentPersonaStore        = "agent-persona-store"
	ComponentShutdownAgentDatabase    = "shutdown:close-agent-database"
	ComponentDocumentStore            = "document-store"
	ComponentShutdownDocument         = "shutdown:close-document-database"
	ComponentShutdownWorkOrder        = "shutdown:close-workorder-database"
	workloadNameGRPC                  = "grpc-surface"
	workloadNameHTTP                  = "http-edge"
	shutdownNameHTTP                  = "stop-http-edge"
	shutdownNameGRPC                  = "stop-grpc-surface"
	shutdownNameChat                  = "close-chat-database"
	shutdownNameAgentDatabase         = "close-agent-database"
	shutdownNameWorkOrder             = "close-workorder-database"
	shutdownNameDocument              = "close-document-database"
	shutdownNameTelemetry             = "shutdown-telemetry"
	httpEdgeReadHeaderTimeoutValue    = 10 * time.Second
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
	// WritingStyles and Voice supply governed provider and durable policy ports.
	WritingStyles *ChattoneService
	Voice         VoiceService
	Config        ServeConfig
	// Pool is the one pool this process opened. It is required for the
	// PostgreSQL store, the workflow-control projection and
	// the P1B execution driver; a composition that supplies its own store,
	// leaves -execution-authority off and does not exercise the operator
	// read may pass nil.
	Pool     *pgxadapter.Pool
	Logger   bootstrap.Logger
	Identity string
	Options  Options
}

type composedProjects struct {
	service            projectservice.Service
	activity           *applicationprojectactivity.Service
	search             projectsearch.Service
	store              *projectstore.Store
	members            *projectmemberstore.Store
	adapter            *projectservice.StoreAdapter
	chatLinks          projectrefs.ChatLinks
	documentPlacements projectrefs.DocumentPlacements
	workItems          projectrefs.WorkItemProjection
	close              func()
}

type tenantProjectCreator struct{}

func (tenantProjectCreator) AuthorizeCreate(_ context.Context, tenantID, actor string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(actor) == "" {
		return projectaccess.ErrUnauthorized
	}
	return nil
}

func composeProjects(ctx context.Context, cfg ServeConfig, chat any, docs any, workItems projectrefs.WorkItemProjection, invitees projectservice.InviteeEligibility, now func() time.Time) (composedProjects, error) {
	if strings.TrimSpace(cfg.ProjectDatabaseURL) == "" {
		return composedProjects{}, nil
	}
	store, err := projectstore.New(ctx, projectstore.Config{
		DSN: cfg.ProjectDatabaseURL, CoreDSN: cfg.DatabaseURL, Schema: ProjectSchemaName, MaxConns: 8, MinConns: 1,
	})
	if err != nil {
		return composedProjects{}, fmt.Errorf("open project database: %w", err)
	}
	members, err := projectmemberstore.New(store)
	if err != nil {
		store.Close()
		return composedProjects{}, err
	}
	links, err := projectlinkstore.New(store)
	if err != nil {
		store.Close()
		return composedProjects{}, err
	}
	var cursorKey [32]byte
	if _, err := rand.Read(cursorKey[:]); err != nil {
		store.Close()
		return composedProjects{}, fmt.Errorf("create project cursor key: %w", err)
	}
	adapter := &projectservice.StoreAdapter{
		Projects: store, Members: members, Configs: projectconfigstore.New(store),
		Views: projectviewstore.New(store), Links: links, CursorKey: cursorKey[:],
	}
	chatLinks, _ := chat.(projectrefs.ChatLinks)
	documentPlacements, _ := docs.(projectrefs.DocumentPlacements)
	refs := projectrefs.Adapter{Chat: chatLinks, Documents: documentPlacements, WorkItems: workItems}
	adapter.LinkResolver = projectlink.Resolver{Authorization: refs, Targets: refs}
	authorizer := projectservice.MembershipAuthorizer{Membership: adapter, Creator: tenantProjectCreator{}, Standing: invitees}
	service := projectservice.Service{
		Auth:     authorizer,
		Commands: adapter, Reads: adapter, Views: adapter, Pages: adapter,
		Workflows: adapter, Links: adapter, LinkResolver: adapter.LinkResolver,
		Members: adapter, Invitees: invitees,
	}
	comments, err := projectcommentstore.New(store)
	if err != nil {
		store.Close()
		return composedProjects{}, err
	}
	if now == nil {
		now = time.Now
	}
	activity, err := applicationprojectactivity.New(applicationprojectactivity.Config{
		Authorizer: authorizer, Store: comments,
		Sanitizer: applicationprojectactivity.NewPlainTextSanitizer(),
		Mentions:  members,
		CursorKey: cursorKey[:], Now: now,
	})
	if err != nil {
		store.Close()
		return composedProjects{}, fmt.Errorf("compose project activity: %w", err)
	}
	search := projectsearch.Service{Auth: members, Standing: invitees, Tasks: projectsearch.StoreRepository{Store: store}, CursorKey: cursorKey[:]}
	return composedProjects{service: service, activity: activity, search: search, store: store, members: members, adapter: adapter,
		chatLinks: chatLinks, documentPlacements: documentPlacements, workItems: workItems, close: store.Close}, nil
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
	if _, err := NewExperienceContracts(); err != nil {
		return nil, fmt.Errorf("application: compile experience contracts: %w", err)
	}
	if err := transportconformance.ValidateServingContract(); err != nil {
		return nil, fmt.Errorf("application: qualify product-slice release gate: %w", err)
	}
	logger := in.Logger
	if logger == nil {
		logger = discardLogger{}
	}
	analyticsService := dataanalytics.NewService()
	graph := newGraphBuilder(RoleServe)
	graph.add(ComponentConfig, KindConfig, cfg)
	assuranceRegister := composeAssuranceRegister()
	graph.add(ComponentAssuranceRegister, KindGovernance, assuranceRegister, ComponentConfig)
	edgeEvidence, err := qualifyServedEdge()
	if err != nil {
		return nil, fmt.Errorf("application: qualify served edge: %w", err)
	}
	graph.add(ComponentEdgeQualification, KindGovernance, edgeEvidence, ComponentConfig)
	graph.add(ComponentDatabasePool, KindAdapter, in.Pool)
	pilotReliability, err := loadPilotReliability(time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("application: %w", err)
	}
	graph.add(ComponentPilotReliability, KindGovernance, pilotReliability, ComponentConfig)
	graph.add(ComponentProductSliceReleaseGate, KindGovernance, transportconformance.ServingContractID, ComponentConfig)

	graph.add(ComponentSchemaMigrator, KindAdapter, options.Migrate)
	if cfg.Migrate {
		if options.Migrate == nil {
			return nil, fmt.Errorf("application: -%s=true needs a schema migrator; the command supplies one", FieldMigrate)
		}
		if err := options.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
			return nil, err
		}
	}
	graph.add(ComponentProjectSchemaMigrator, KindAdapter, options.MigrateProject)
	if strings.TrimSpace(cfg.ProjectDatabaseURL) != "" && cfg.Migrate {
		if options.MigrateProject == nil {
			return nil, fmt.Errorf("application: -%s requires a project schema migrator; the command supplies one", FieldProjectDatabaseURL)
		}
		if err := options.MigrateProject(ctx, cfg.ProjectDatabaseURL, cfg.DatabaseURL, ProjectSchemaName, logger); err != nil {
			return nil, err
		}
	}
	graph.add(ComponentWorkOrderSchemaMigrator, KindAdapter, options.MigrateWorkOrder)
	if strings.TrimSpace(cfg.WorkOrderDatabaseURL) != "" && cfg.Migrate {
		if options.MigrateWorkOrder == nil {
			return nil, fmt.Errorf("application: -%s requires a work order schema migrator; the command supplies one", FieldWorkOrderDatabaseURL)
		}
		if err := options.MigrateWorkOrder(ctx, cfg.WorkOrderDatabaseURL, cfg.DatabaseURL, WorkOrderSchemaName, logger); err != nil {
			return nil, err
		}
	}

	if err := applyClockMigrations(ctx, cfg, options, logger); err != nil {
		return nil, err
	}

	// Persona definitions and installations live on their own bounded pool.
	// Keep the pool's lifetime in this composition, so every startup failure
	// after this point rolls it back and normal shutdown closes it exactly once.
	agentDatabase, err := composeAgentDatabase(ctx, cfg)
	if err != nil {
		return nil, err
	}
	agentDatabaseCommitted := false
	defer func() {
		if !agentDatabaseCommitted && agentDatabase.close != nil {
			agentDatabase.close()
		}
	}()
	if agentDatabase.store != nil {
		graph.add(ComponentAgentDatabasePool, KindAdapter, agentDatabase.store, ComponentConfig)
		graph.add(ComponentAgentPersonaStore, KindAdapter, agentDatabase.personas, ComponentAgentDatabasePool)
	}

	store, err := composeStore(in.Pool, cfg, options)
	if err != nil {
		return nil, err
	}
	graph.add(ComponentIntentStore, KindAdapter, store, ComponentDatabasePool, ComponentConfig)
	// Every served tenant is registered, and every served demo company's
	// workforce seeded, on start: one process serves them all, and each
	// request is scoped by its own credential's tenant.
	for _, tenant := range cfg.ServedTenants() {
		if err := store.Bootstrap(ctx, tenant); err != nil {
			return nil, err
		}
		logger.Info("hcmnext.tenant_registered", "tenant", tenant, "cell_id", cfg.CellID)
	}
	if cfg.DevBrowserLogin && cfg.DevWorkforceBootstrap {
		populationProvisioner := NewLocalDemoPopulationProvisioner(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), options.Now)
		homeOrganizationProvisioner := NewLocalDemoHomeOrganizationProvisioner(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), options.Now)
		for _, tenant := range cfg.ServedTenants() {
			workforceSummary, organizationSummary, seedErr := bootstrapLocalDevWorkforce(ctx, in.Pool, tenant)
			if seedErr != nil {
				return nil, seedErr
			}
			if _, isDemo := demoworkforce.PackFor(tenant); in.Pool != nil && isDemo {
				populationSummary, populationErr := populationProvisioner.Provision(ctx, tenant)
				if populationErr != nil {
					return nil, populationErr
				}
				if populationSummary.Created > 0 || populationSummary.Existing > 0 {
					logger.Info("hcmnext.local_dev_population_ready", "tenant", tenant,
						"facts_created", populationSummary.Created, "facts_existing", populationSummary.Existing)
				}
				homeOrganizationSummary, homeOrganizationErr := homeOrganizationProvisioner.Provision(ctx, tenant)
				if homeOrganizationErr != nil {
					return nil, homeOrganizationErr
				}
				if homeOrganizationSummary.Created > 0 || homeOrganizationSummary.Existing > 0 {
					logger.Info("hcmnext.local_dev_home_organizations_ready", "tenant", tenant,
						"facts_created", homeOrganizationSummary.Created, "facts_existing", homeOrganizationSummary.Existing)
				}
			}
			if workforceSummary.Planned > 0 {
				logger.Info("hcmnext.local_dev_workforce_ready",
					"tenant", tenant, "workers", workforceSummary.Planned, "workers_inserted", workforceSummary.Inserted,
					"organization_units", organizationSummary.UnitsPlanned, "organization_units_inserted", organizationSummary.UnitsInserted)
			}
		}
	}

	verifier, machineAuth, err := composeServedVerifier(cfg, options, in.Pool)
	if err != nil {
		return nil, err
	}
	baseVerifier := verifier
	oidcFlow, oidcSessions, err := composeOIDCWorkspaceLogin(ctx, in.Pool, cfg, baseVerifier, options.Now)
	if err != nil {
		return nil, err
	}
	if oidcSessions != nil {
		verifier = oidcSessions
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
	telemetryLifecycle, err := telemetrylifecycle.New(telemetrylifecycle.DefaultPolicy())
	if err != nil {
		return nil, fmt.Errorf("build telemetry lifecycle policy: %w", err)
	}
	graph.add(ComponentTelemetryLifecycle, KindAdapter, telemetryLifecycle, ComponentConfig, ComponentTelemetryProvider)
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
	// one chronology. It is durable (WF-RUN-035): the evidence a restart
	// must still show is a tenant-scoped row, never process memory.
	evidence := options.Evidence
	if evidence == nil {
		evidence = composeEvidenceStore(in.Pool)
	}
	graph.add(ComponentEvidenceSink, KindRegistry, evidence)

	// REV-004-02: the serve cell owns one governed-disposition gate so
	// retention disposition, legal-hold enforcement and verified deletion
	// are reachable from the running process instead of library-only.
	disposition := NewDispositionGate()
	graph.add(ComponentDispositionGate, KindAdapter, disposition, ComponentConfig)
	installationLifecycle, err := composeInstallationLifecycle(options.Now)
	if err != nil {
		return nil, fmt.Errorf("application: compose installation lifecycle: %w", err)
	}
	graph.add(ComponentInstallationLifecycle, KindGovernance, installationLifecycle, ComponentConfig)

	workspaceEnabled := cfg.Workspace
	workerIDs := workeridstore.New(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	roleAccess := roleaccessstore.New(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), productFeatureCatalog()...)
	for _, tenant := range cfg.ServedTenants() {
		if in.Pool == nil {
			break
		}
		if err := roleAccess.Bootstrap(ctx, kernelvalues.TenantId(tenant), "system:bootstrap"); err != nil {
			return nil, fmt.Errorf("bootstrap role access: %w", err)
		}
		if _, isDemo := demoworkforce.PackFor(tenant); cfg.DevBrowserLogin && isDemo {
			if err := roleAccess.BootstrapLocalDevPersonaPermissions(ctx, kernelvalues.TenantId(tenant)); err != nil {
				return nil, fmt.Errorf("bootstrap local development personas: %w", err)
			}
			// The role catalog exists now, so every seeded worker can be given
			// the durable role set migrations/00238 provides for. This adds
			// rows only: no grant or gating rule changes.
			assigned, err := bootstrapLocalDevRoleAssignments(ctx, in.Pool, tenant)
			if err != nil {
				return nil, err
			}
			if assigned.Workers > 0 {
				logger.Info("hcmnext.local_dev_role_assignments_ready",
					"tenant", tenant, "workers", assigned.Workers, "assignments", assigned.Assignments)
			}
			branded, err := bootstrapLocalDevBranding(ctx, in.Pool, tenant)
			if err != nil {
				return nil, err
			}
			if branded > 0 {
				logger.Info("hcmnext.local_dev_branding_ready", "tenant", tenant, "scopes", branded)
			}
		}
	}
	cellConfig := app.CellConfig{
		Health:           newServeHealth(cfg, in.Pool, options.HealthPoolPing, options.CurrentHealthConfigFingerprint, options.HealthCheckInterval, options.HealthCheckTimeout),
		Store:            store,
		Verifier:         verifier,
		LegalEvidence:    legalEvidence,
		Audience:         cfg.Audience,
		MaxDeadline:      cfg.MaxDeadline,
		Logger:           transport.LoggerFunc(RequestLogger(logger)),
		EventLogger:      eventLogger(logger),
		Workspace:        &workspaceEnabled,
		DevBrowserLogin:  cfg.DevBrowserLogin,
		DevPersonas:      append(composeDevPersonas(baseVerifier, cfg, options.Now), composeDevEmployeePersonas(baseVerifier, cfg, options.Now)...),
		DevDirectory:     composeDevDirectory(cfg),
		PublicOrigin:     cfg.PublicOrigin,
		Evidence:         evidence,
		Telemetry:        telemetryProvider,
		WorkflowRecorder: schedulerRecorder(telemetryProvider, logger, options.Now),
		// WF-RUN-016: the governed repair door is composed from the same pool
		// and tenant mapping as the controls; only its external-system
		// adapters are a seam, because none exists in this repository yet.
		RepairEffect:         options.RepairEffect,
		RepairObservation:    options.RepairObservation,
		RepairReconciliation: options.RepairReconciliation,
		Inputs:               options.Inputs,
		Workers:              options.Workers,
		Bands:                options.Bands,
		Now:                  options.Now,
		Clock:                options.Clock,
		IDs:                  options.IDs,
		Preferences:          preferencestore.New(in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)),
		KnowledgeSearch:      contentregistrystore.New(in.Pool),
		RoleAccess:           roleAccess,
		PageLedger:           pageledgerstore.New(in.Pool, pgstore.TenantID),
		Catalogs:             i18ncatalogstore.New(in.Pool, pgstore.TenantID),
		WorkerIDs:            workerIDs,
		// REV-096-01: the promotion routing predicate's production reader. A
		// subject's most recently finalized calibration is resolved from the
		// tenant's own performance rows; a subject with none, or a rating that
		// does not validate, is a miss and stays on the plan's own digest.
		PerformanceRatings: composeCalibratedRatings(in.Pool),
		// The plan this cell executes, so a top-band subject can be pinned to
		// the variant at all. The pin still requires a configured
		// HighPerformerVariantDigest, which stays empty until the variant is
		// registered with the execution authority.
		PromotionPlan: cfg.WorkflowPlan,
	}
	applyWorkspaceOIDCLogin(&cellConfig, cfg, oidcFlow, oidcSessions)
	cellConfig.WorkflowCapabilityPolicy, cellConfig.WorkflowPaletteExtensions = localDevelopmentWorkflowAuthoring(cfg)
	graph.add(ComponentPresentationPrefs, KindAdapter, cellConfig.Preferences, ComponentDatabasePool)
	graph.add(ComponentRoleAccess, KindAdapter, cellConfig.RoleAccess, ComponentDatabasePool)
	graph.add("page-ledger-store", KindAdapter, cellConfig.PageLedger, ComponentDatabasePool)
	graph.add(ComponentPayBandCatalog, KindPort, cellConfig.Bands)

	if cfg.ExecutionAuthority {
		composeExecution := options.ComposeExecution
		if composeExecution == nil {
			composeExecution = composeExecutionAuthorityWith(options.ProviderReceipts)
		}
		if err := composeExecution(&cellConfig, in.Pool, evidence, cfg); err != nil {
			return nil, fmt.Errorf("compose the P1B execution authority: %w", err)
		}
		logger.Info("hcmnext.execution_authority_enabled",
			"role", cfg.ExecutionAuthorityRole, "cell_id", cfg.CellID)
		// A local-development cell is left able to run the reference promotion
		// end to end, the same guarantee the workforce seed gives. Any other
		// profile keeps the governed CLI release path.
		released, err := bootstrapLocalDevWorkflowVersions(ctx, cfg, cellConfig.ExecutionVersions, options.Now)
		if err != nil {
			return nil, err
		}
		clockPublishNow := options.Now
		if clockPublishNow == nil {
			clockPublishNow = time.Now
		}
		clockVersions, err := bootstrapLocalDevTimeclockWorkflowVersion(ctx, cfg, cellConfig.ExecutionVersions, clockPublishNow)
		if err != nil {
			return nil, err
		}
		released = append(released, clockVersions...)
		correctionVersions, err := bootstrapLocalDevMissingPunchWorkflowVersion(ctx, cfg, cellConfig.ExecutionVersions, clockPublishNow)
		if err != nil {
			return nil, err
		}
		released = append(released, correctionVersions...)
		for _, v := range released {
			logger.Info("hcmnext.local_dev_workflow_version_ready",
				"tenant", cfg.Tenant, "workflow", v.WorkflowID, "semantic_version", v.SemanticVersion,
				"status", string(v.Status), "approved_by", platformexecution.DevReleaseApprover,
				"command", "hcmnext workflow-version bootstrap-dev")
		}
	}
	graph.add(ComponentExecutionAuthority, KindGovernance, cellConfig.ExecutionAuthority, ComponentConfig, ComponentDatabasePool, ComponentEvidenceSink)
	graph.add(ComponentProposalExecutor, KindWorkflow, cellConfig.Executor, ComponentExecutionAuthority)
	graph.add(ComponentWorkflowResolver, KindWorkflow, cellConfig.ExecutionResolver, ComponentExecutionAuthority)
	graph.add(ComponentWorkflowVersions, KindWorkflow, cellConfig.ExecutionVersions, ComponentExecutionAuthority)

	cell, err := app.NewCell(cellConfig)
	if err != nil {
		return nil, err
	}
	platformAgentRuntime := wireAgentRuntime(ctx, in, cell, logger, graph, options.Now)
	personaWiring, personaWiringErr := composePersonaServeWiring(in.Pool, cell, agentDatabase.personas, options.Now)
	if personaWiringErr != nil {
		logger.Error("hcmnext.persona_catalog_unavailable", "stage", personaServeWiringStage(personaWiringErr))
	}
	// The local-dev demo cell runs evaluations with the same local verifier
	// and key the agent-demo preparation uses. It is prepared before anything
	// binds the persona store, because preparation replaces that store with
	// one that can verify the local seal: a catalog bound to the earlier
	// store ran an evaluation and then could not read its result back. No
	// other profile gets an evaluator here; the page then says evaluation is
	// run by the platform team. The demo tenant may be one of several this
	// process serves; the runner itself refuses every other tenant.
	var localEvaluation PersonaAdminEvaluationRunner
	if personaWiring != nil && agentDatabase.store != nil && cfg.Profile == ServeProfileLocalDev && slices.Contains(cfg.ServedTenants(), localAgentDemoTenant) {
		runner, evaluationErr := personaWiring.prepareLocalAdminEvaluation(agentDatabase.store, cfg.Profile, localAgentDemoTenant, personaServeClock(options.Now), cfg.ServedTenants()...)
		if evaluationErr != nil {
			logger.Error("hcmnext.persona_admin_evaluation_unavailable", "stage", "prepare", "error", evaluationErr.Error())
		} else {
			localEvaluation = runner
		}
	}
	var serviceHandlers transportcell.ServiceHandlers
	if in.Pool != nil {
		tenantUUID := tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)
		publishedDefinitions := integrationregistry.New(in.Pool)
		dataOpsHandler, serviceErr := cell.NewDataOpsHandler(dataopsartifactstore.New(in.Pool, tenantUUID), options.Now)
		if serviceErr != nil {
			return nil, fmt.Errorf("application: compose authorized DataOps handlers: %w", serviceErr)
		}
		stageHandler := dataopsimport.New(dataopsstore.New(in.Pool), tenantUUID, options.Now)
		integrationService, serviceErr := NewIntegrationService(IntegrationOptions{
			Definitions: publishedDefinitions,
			TenantID: func(tenant string) uuid.UUID {
				return tenantUUID(kernelvalues.TenantId(tenant))
			},
			Connections: []IntegrationConnection{{
				Connection:  cell.Connection,
				Connector:   cell.Incumbent,
				OrgID:       cell.Connection.OrgID(),
				SystemID:    cell.Connection.SystemID(),
				AuthMode:    cell.Connection.AuthMode(),
				Environment: cell.Connection.Environment(),
				Residency:   cell.Connection.Residency(),
			}},
			Observations: cell.Observations,
			Now:          options.Now,
		})
		if serviceErr != nil {
			return nil, fmt.Errorf("application: compose published Integration registry: %w", serviceErr)
		}
		parameterHandler := transportparameters.NewHandler(nil)
		if cfg.ParameterEnvironment != "" {
			registry := dataconfigregistry.New(in.Pool)
			servedParameters, parameterErr := platformconfig.NewServedParameters(
				TrustedParameterPrincipal{},
				ActiveRegistryParameterDefinitions{Registry: registry, TenantUUID: func(tenant string) uuid.UUID {
					return tenantUUID(kernelvalues.TenantId(tenant))
				}},
				OrganizationParameterScopes{Graph: StoredParameterOrganizationGraph{DB: in.Pool, TenantUUID: tenantUUID}, Clock: WallClockParameterClock{}},
				CellParameterEnvironment{Environment: platformconfig.ParameterEnvironment(cfg.ParameterEnvironment)},
				WallClockParameterClock{},
				configparamstore.New(in.Pool, func(tenant string) uuid.UUID {
					return tenantUUID(kernelvalues.TenantId(tenant))
				}),
			)
			if parameterErr != nil {
				return nil, fmt.Errorf("application: compose served tenant parameters: %w", parameterErr)
			}
			parameterHandler = transportparameters.NewHandler(servedParameters)
		}
		serviceHandlers = transportcell.ServiceHandlers{
			DataOps: dataOpsHandler, DataOpsStage: stageHandler, Integration: integrationService,
			IntegrationPublisher: newIntegrationDefinitionPublisher(publishedDefinitions, func(tenant string) uuid.UUID {
				return tenantUUID(kernelvalues.TenantId(tenant))
			}, options.Now),
			Parameters: parameterHandler,
		}
	}
	clockRuntime, err := composeConfiguredClock(ctx, cfg, in.Pool, options)
	if err != nil {
		return nil, err
	}
	if clockRuntime == nil {
		clockRuntime, err = composeLocalDevSelfClock(ctx, localDevSelfClockInput{Config: cfg, Pool: in.Pool, Logger: logger, Options: options, Telemetry: telemetryProvider})
		if err != nil {
			return nil, err
		}
	}
	if clockRuntime == nil {
		// The workspace tells people the time clock is not turned on; the
		// operator's detail belongs here, not on that page.
		logger.Info("hcmnext.time_clock_not_enabled",
			"enable_with", "-"+FieldTimeDatabaseURL+" and -"+FieldTimeWorkerTokenKey+" with the signed registry flags, or run -profile=local-dev with the demo workforce, which serves the worker self clock on its own")
	}
	clockCommitted := false
	defer func() {
		if clockRuntime != nil && !clockCommitted {
			clockRuntime.Close()
		}
	}()
	if clockRuntime != nil {
		if clockRuntime.API != nil {
			serviceHandlers.ClockDevice = clockRuntime.API
			graph.add("service:clock-device", KindEngine, clockRuntime.API, ComponentDatabasePool, ComponentConfig)
		}
		if clockRuntime.Worker != nil {
			serviceHandlers.ClockWorker = clockRuntime.Worker
			graph.add("service:clock-worker", KindEngine, clockRuntime.Worker, ComponentDatabasePool, ComponentConfig)
		}
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

	chatSessions := options.ChatSessionRevocation
	if chatSessions == nil {
		if checker, ok := verifier.(session.RevocationChecker); ok {
			chatSessions = checker
		}
	}
	var chatFacts ChatAuthorityFacts
	if chatSessions != nil || cfg.Profile == ServeProfileLocalDev {
		chatFacts = newCurrentWorkerChatFacts(roleAccess, in.Pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), chatSessions)
	}
	chatComposition := ChatComposition{}
	if personaWiring != nil {
		chatComposition = personaWiring.chatComposition()
	}
	chatRuntime, err := composeChat(ctx, cfg, options.Now, chatFacts, in.Pool, cell.Service, chatComposition)
	if err != nil {
		// composeChat returns nil unless -chat-enabled is set, so this is an
		// enabled surface that cannot answer: mounting its routes would serve
		// nothing but denials, and the failure must be visible at startup
		// rather than hidden behind an empty log line.
		logger.Error("hcmnext.chat_unavailable", "error", err.Error())
		return nil, fmt.Errorf("compose chat: %w", err)
	}
	chatCommitted := false
	if chatRuntime.moderationStore != nil && agentDatabase.store != nil {
		chatRuntime.moderationStore.RunTrace = chatremoveRunTrace{DB: agentDatabase.store,
			TenantUUID: tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)}
	}
	defer func() {
		if !chatCommitted && chatRuntime.close != nil {
			chatRuntime.close()
		}
	}()
	if options.PersonaInvocation != nil && (personaWiring == nil || chatRuntime.service == nil) {
		return nil, fmt.Errorf("compose configured persona invocation: %w", errPersonaInvocationProductionComposition)
	}
	var personaAudience *PersonaPublicAudienceAuthority
	if chatRuntime.extensions != nil && chatRuntime.extensions.TodoStore != nil {
		personaAudience = NewPersonaPublicAudienceAuthority(chatRuntime.extensions.TodoStore)
	}
	if personaWiring != nil && chatRuntime.service != nil {
		if bindErr := personaWiring.bindChat(chatRuntime.service); bindErr != nil {
			logger.Error("hcmnext.persona_catalog_unavailable", "stage", personaServeWiringStage(bindErr))
		} else {
			if cell.Agents != nil {
				cell.Agents = personaWiring.bindAgents(cell.Agents)
			}
			var placements []PersonaAdminPlacementSource
			if personaAudience != nil {
				placements = append(placements, personaAudience)
			}
			if bindErr := personaWiring.bindAdminCommands(agentDatabase.store, agentDatabase.review, placements...); bindErr != nil {
				logger.Error("hcmnext.persona_admin_commands_unavailable", "stage", personaServeWiringStage(bindErr))
			} else if localEvaluation != nil {
				if evaluationErr := personaWiring.bindAdminEvaluation(localEvaluation); evaluationErr != nil {
					logger.Error("hcmnext.persona_admin_evaluation_unavailable", "stage", "bind", "error", evaluationErr.Error())
				}
			}
		}
		cell.PersonaAdminClientFactory = personaWiring.adminClientFactory()
	}
	graph.add(ComponentChatService, KindEngine, chatRuntime.service, ComponentConfig)
	documentRuntime, err := composeDocument(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if documentRuntime.store != nil {
		// documentRoutes is core's document ID route directory (HUB-002), in
		// memory for this process the same way chat's CHAT-002 directory is;
		// a durable composition swaps this for a persisted implementation of
		// documenthubstore.DocumentRouteDirectory without touching call
		// sites. agentApps resolves an installed chat agent's own
		// installation for HUB-031 agent search; a chat composition that
		// never ran (chatRuntime.extensions is nil) leaves it nil, and
		// AgentSearchDocuments then refuses every call as not installed
		// rather than silently granting one.
		documentRoutes := documenthubstore.NewMemoryDocumentRoutes()
		var agentApps agentInstallationRepository
		if chatRuntime.extensions != nil && chatRuntime.extensions.Apps != nil {
			agentApps = chatRuntime.extensions.Apps.Repo
		}
		documentRuntime.service = documentService{
			store: documentRuntime.store, recipientAllowed: documentRecipientValidator(in.Pool), embedder: documentRuntime.embedder, indexer: documentRuntime.indexer,
			ownerNames: documentOwnerNames(in.Pool), people: documentPeopleDirectory(in.Pool),
			routes: documentRoutes, aud: documentAudienceEligibility(chatRuntime.service), agentApps: agentApps, agentNow: options.Now,
		}
		documentRuntime.service = withDocumentChat(documentRuntime.service, chatRuntime.service)
		if personaWiring != nil {
			previewReader, ok := documentRuntime.service.(PersonaAdminDocumentReader)
			if !ok {
				return nil, fmt.Errorf("compose persona admin document reader: document preview service unavailable")
			}
			if bindErr := personaWiring.bindAdminDocumentReader(previewReader); bindErr != nil {
				return nil, fmt.Errorf("compose persona admin document reader: %w", bindErr)
			}
			policySearcher, searcherErr := NewPersonaPolicyDocumentSearcher(documentRuntime.service, documentRuntime.store, WorkspaceDocumentDirectory{DB: in.Pool, TenantUUID: tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)})
			if searcherErr != nil {
				return nil, fmt.Errorf("compose persona policy document source: %w", searcherErr)
			}
			if _, bindErr := bindPersonaPolicySearchSkill(personaWiring.capabilities, personaWiring.skills, policySearcher); bindErr != nil {
				logger.Error("hcmnext.persona_policy_search_unavailable", "error", bindErr.Error())
			}
			// Assistant's second read-only skill rides the same searcher, and Agent
			// setup shows how many workspace documents it can search.
			if bindErr := personaWiring.bindWorkspaceSearch(policySearcher); bindErr != nil {
				logger.Error("hcmnext.persona_workspace_search_unavailable", "error", bindErr.Error())
			}
		}
		// CHATBUG-006: an agent answer's sources become links only when the
		// reader's projection can ask the hub whether this reader may open them.
		chatRuntime.bindAgentSourceAccess(documentRuntime.store)
	}
	agentTaskDocuments, err := bindAgentTaskDocumentRuntime(platformAgentRuntime, documentRuntime.store)
	if err != nil {
		return nil, fmt.Errorf("compose agent task document resolver: %w", err)
	}
	if err := composeServedAgentTaskModel(ctx, in.Config, platformAgentRuntime, agentTaskDocuments, os.Getenv, options.Now); err != nil {
		logger.Error("hcmnext.agent_task_model_unavailable", "error", err.Error())
	}
	documentCommitted := false
	defer func() {
		if !documentCommitted && documentRuntime.close != nil {
			documentRuntime.close()
		}
	}()
	if documentRuntime.store != nil {
		graph.add(ComponentDocumentStore, KindAdapter, documentRuntime.store, ComponentConfig)
	}
	var localPolicyRegistry composedAgentModelPolicyRegistry
	localPolicyRegistryCommitted := false
	defer func() {
		if !localPolicyRegistryCommitted && localPolicyRegistry.close != nil {
			localPolicyRegistry.close()
		}
	}()
	var localPersonaBootstrap LocalDevPersonaBootstrapper
	if cfg.Profile == ServeProfileLocalDev && personaWiring != nil && agentDatabase.store != nil &&
		chatRuntime.extensions != nil && chatRuntime.extensions.TodoStore != nil && os.Getenv(EnvLocalAgentPolicyAuthorityDatabaseURL) != "" {
		material, materialErr := LoadOrCreateLocalPersonaModelSigningMaterial(localPersonaModelSigningPath)
		if materialErr != nil {
			return nil, materialErr
		}
		deploymentPath := cfg.AgentModelConfigFile
		if deploymentPath == "" {
			deploymentPath = localPersonaModelDeploymentPath
		}
		localPolicyRegistry, err = composeLocalAgentModelPolicyRegistry(ctx, agentModelPolicyRegistryCompositionInput{
			Config: cfg, Core: in.Pool, Agents: agentDatabase, Material: material,
			PublisherDSN: os.Getenv(EnvLocalAgentPolicyAuthorityDatabaseURL), Now: personaServeClock(options.Now),
			CurrentDeployment: PersonaModelDeploymentPolicyAuthority{Routes: agentDatabase.store,
				Deployment: func(context.Context, kernelvalues.TenantId) (PersonaModelDeployment, error) {
					return LoadPersonaModelDeployment(deploymentPath)
				}, TenantUUID: tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), Now: personaServeClock(options.Now)},
		})
		if err != nil {
			return nil, fmt.Errorf("compose local agent model policy: %w", err)
		}
		localPersonaBootstrap, err = personaWiring.localDevBootstrap(cfg.Profile, in.Pool, agentDatabase.store, localPolicyRegistry.References, chatRuntime.extensions.TodoStore, agentDatabase.review)
		if err != nil {
			return nil, fmt.Errorf("compose local persona bootstrap: %w", err)
		}
		routeAuthorityDSN := os.Getenv("HCMNEXT_PERSONA_MODEL_ROUTE_AUTHORITY_DATABASE_URL")
		if routeAuthorityDSN != "" {
			privateKey, keyErr := loadOrCreateLocalAgentDemoEvaluationKey(filepath.FromSlash(localAgentDemoEvaluationPath))
			if keyErr != nil {
				return nil, fmt.Errorf("compose local persona runtime evaluator: %w", keyErr)
			}
			mapper := tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)
			evaluations, authorityErr := localAgentDemoEvaluationAuthority(privateKey, localAgentDemoTenant, mapper, personaServeClock(options.Now))
			if authorityErr != nil {
				return nil, fmt.Errorf("compose local persona runtime authority: %w", authorityErr)
			}
			provisioner := &LocalPersonaRuntimeProvisioner{
				Tenants: cfg.ServedTenants(),
				Config:  LocalAgentDemoConfig{Tenant: localAgentDemoTenant, DatabaseURL: cfg.DatabaseURL, AgentDatabaseURL: cfg.AgentDatabaseURL, ChatDatabaseURL: cfg.ChatDatabaseURL, DocumentDatabaseURL: cfg.DocumentDatabaseURL, RouteAuthorityDatabaseURL: routeAuthorityDSN, AgentOwnerDatabaseURL: os.Getenv("HCMNEXT_AGENT_OWNER_DATABASE_URL")},
				Core:    in.Pool, Agents: agentDatabase.store, Personas: personaWiring.store, Evaluations: evaluations, Signing: material, DeploymentPath: deploymentPath, Now: personaServeClock(options.Now),
			}
			if bindErr := personaWiring.bindAdminRuntimeProvisioner(provisioner); bindErr != nil {
				return nil, fmt.Errorf("compose local persona runtime publication: %w", bindErr)
			}
		}
	}
	cell.PersonaAdminClientFactory = withLocalDevPersonaBootstrapAvailability(cell.PersonaAdminClientFactory, localPersonaBootstrap != nil)
	var servedPersonaInvocation *PersonaInvocationProductionRuntime
	var personaShareFloor *PersonaRuntimeAudienceFloor
	var announcementSurface *AgentAnnouncementControlSurface
	var announcementWorker *AgentAnnouncementWorker
	if personaWiring != nil {
		if streaming, ok := chatRuntime.service.(*streamingChatService); ok && streaming.personaInvocation == nil {
			var bodyClassifier *LocalDevPersonaPostClassifier
			var classifiers []PersonaAcceptedHumanPostClassifier
			if chatRuntime.extensions != nil && chatRuntime.extensions.TodoStore != nil {
				var classifierErr error
				bodyClassifier, classifierErr = NewLocalDevPersonaPostClassifier(chatRuntime.extensions.TodoStore, cfg.Profile == ServeProfileLocalDev)
				if classifierErr != nil {
					return nil, fmt.Errorf("compose served persona post classifier: %w", classifierErr)
				}
				classifiers = append(classifiers, bodyClassifier)
			}
			invocationConfig := options.PersonaInvocation
			if invocationConfig == nil {
				modelFactory, modelErr := newPersonaServedProviderConfiguration(ctx, cfg, platformAgentRuntime, os.Getenv)
				if modelErr != nil {
					return nil, fmt.Errorf("compose served persona provider: %w", modelErr)
				}
				if modelFactory != nil {
					floor := &PersonaRuntimeAudienceFloor{Chat: chatRuntime.service, Personas: agentDatabase.personas,
						Authority: NewPersonaAudienceFloorAdapter(personaAudience, personaAudience, personaAudience), Classes: personaAudience, BodyClasses: bodyClassifier,
						Documents: AgentAnnouncementHubAuthority{Store: documentRuntime.store, Now: time.Now, Classes: agentUX035Classes(chatRuntime.store)}}
					personaShareFloor = floor
					invocationConfig, modelErr = composePersonaRuntimeDependencies(ctx, personaRuntimeCompositionInput{
						AgentDatabase: agentDatabase, Pool: in.Pool, Cell: cell, Personas: personaWiring,
						Chat: streaming, ChatRuntime: chatRuntime, Documents: documentRuntime,
						ModelFactory: modelFactory, AudienceSnapshots: personaAudience, AudienceFloor: floor,
					})
					if modelErr != nil {
						return nil, fmt.Errorf("compose served persona owners: %w", modelErr)
					}
				}
			}
			announcementSurface, announcementWorker, err = composeAgentAnnouncements(personaRuntimeCompositionInput{AgentDatabase: agentDatabase, Pool: in.Pool, Cell: cell, Personas: personaWiring, Chat: streaming, ChatRuntime: chatRuntime, Documents: documentRuntime, AudienceSnapshots: personaAudience}, invocationConfig)
			if err != nil {
				return nil, fmt.Errorf("compose agent announcements: %w", err)
			}
			personaRuntime, invocationErr := composeServedPersonaInvocation(streaming, personaWiring, agentDatabase, invocationConfig, logger, classifiers...)
			if invocationErr != nil {
				logger.Error("hcmnext.persona_invocation_unavailable", "stage", "production_run_composition_invalid")
				return nil, fmt.Errorf("compose configured persona invocation: %w", invocationErr)
			}
			if personaRuntime == nil {
				logger.Error("hcmnext.persona_invocation_unavailable", "stage", "production_run_composition_missing", "missing_ports", personaInvocationServeMissingPorts(), "enable_with", personaServedProviderUnavailableReason(cfg, os.Getenv))
				// AGENTUX-075: a question to an agent is still answered, with the reason.
				if bindErr := bindPersonaModelUnavailable(streaming, personaWiring, agentDatabase, logger); bindErr != nil {
					logger.Error("hcmnext.persona_model_unavailable_notice_unbound", "error_type", fmt.Sprintf("%T", bindErr))
				}
			} else {
				if platformAgentRuntime != nil && platformAgentRuntime.Starter != nil && invocationConfig != nil {
					taskPersonas, taskPersonaErr := NewAgentTaskPersonaResolver(invocationConfig.Authority, personaWiring.aud, personaWiring.store)
					if taskPersonaErr != nil {
						logger.Error("hcmnext.agent_task_persona_unavailable", "error", taskPersonaErr.Error())
					} else {
						platformAgentRuntime.Starter.personas = taskPersonas
					}
				}
				servedPersonaInvocation = personaRuntime
				graph.add("persona-run-model-worker", KindEngine, personaRuntime.Worker, ComponentChatService, ComponentAgentDatabasePool)
			}
		}
	}
	// Both WorkService and safe project references use the same tenant-scoped
	// WorkItem reader. The project projection still applies current WorkService
	// action and row visibility checks before exposing its small summary.
	workQueueReader := app.NewWorkItemQueueReader(in.Pool,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	graph.add(ComponentWorkItemQueueRead, KindPort, workQueueReader, ComponentDatabasePool)
	var projectWorkItems projectrefs.WorkItemProjection
	var projectInvitees projectservice.InviteeEligibility
	if in.Pool != nil {
		projectWorkItems = projectrefs.WorkItemProjector{
			Source: workQueueReader, Authorize: transportcell.WorkActionAuthorizer(cell.RoleAccess),
			TenantID: tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), Now: options.Now,
		}
		projectInvitees = projectservice.WorkforceInviteeEligibility{
			DB: in.Pool, TenantID: tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID),
		}
	}
	projects, err := composeProjects(ctx, cfg, chatRuntime.service, documentRuntime.service, projectWorkItems, projectInvitees, options.Now)
	if err != nil {
		return nil, fmt.Errorf("compose projects: %w", err)
	}
	projectsCommitted := false
	defer func() {
		if !projectsCommitted && projects.close != nil {
			projects.close()
		}
	}()
	if projects.store != nil {
		graph.add(ComponentProjectService, KindEngine, projects.service, ComponentProjectSchemaMigrator)
		serviceHandlers.Project = &transportproject.Dependencies{Service: projects.service, Activity: projects.activity, Search: projects.search}
	}
	workOrders, err := composeWorkOrders(ctx, cfg, projects, cell.Workers, projectInvitees, in.Pool, options.Now)
	if err != nil {
		return nil, fmt.Errorf("compose work orders: %w", err)
	}
	workOrdersCommitted := false
	defer func() {
		if !workOrdersCommitted && workOrders.close != nil {
			workOrders.close()
		}
	}()
	var workOrderDeps *transportworkorder.Dependencies
	if workOrders.store != nil {
		graph.add(ComponentWorkOrderService, KindEngine, workOrders.service, ComponentWorkOrderSchemaMigrator, ComponentProjectService, ComponentWorkerFacts)
		workOrderDeps = &transportworkorder.Dependencies{Service: workOrders.service}
		serviceHandlers.WorkOrder = workOrderDeps
		refs := projectrefs.Adapter{
			Chat: projects.chatLinks, Documents: projects.documentPlacements,
			WorkItems: projects.workItems, WorkOrders: workOrders.projection,
		}
		resolver := projectlink.Resolver{Authorization: refs, Targets: refs}
		projects.adapter.LinkResolver = resolver
		projects.service.LinkResolver = resolver
		serviceHandlers.Project = &transportproject.Dependencies{Service: projects.service, Activity: projects.activity, Search: projects.search}
	}

	if in.WritingStyles == nil {
		// The chat writing-style controls: composed only over a qualified model
		// (see composeServedChattone); otherwise the features endpoint reports
		// them off and the client shows no controls.
		writingStyles, writingReason, writingErr := composeServedChattone(ctx, chattoneServeInput{Config: cfg, Runtime: platformAgentRuntime, Chat: chatRuntime, Facts: chatFacts, Now: options.Now})
		logChattoneComposition(logger, writingStyles, writingReason, writingErr)
		if writingErr == nil && writingStyles != nil {
			in.WritingStyles = writingStyles
		} else if writingErr == nil {
			// Not provisioned: the features answer says why, so the composer
			// states it instead of silently having no controls.
			in.WritingStyles = chattoneUnprovisionedService(writingReason, chatRuntime.store, chatFacts, options.Now)
		}
	}
	// CHATLANG-003: translation of messages into each reader's language. It is
	// composed over a qualified model (or, in the local development profile only,
	// the deterministic test engine); every workspace stays off until its
	// administrator turns it on.
	chatlangRuntime, chatlangReason, chatlangErr := composeServedChatlang(ctx, chatlangServeInput{Config: cfg, Runtime: platformAgentRuntime, Chat: chatRuntime, Now: options.Now, Tenants: cfg.ServedTenants()})
	logChatlangComposition(logger, chatlangRuntime, chatlangReason, chatlangErr)
	// CHATVOICE: voice messages, their transcripts and Listen, over OpenAI through
	// the model gateway's key and the channel's outside-service switch. A missing
	// key leaves transcripts "unavailable"; it never disables recording.
	chatVoiceRuntime, chatVoiceReason, chatVoiceErr := composeServedChatVoice(chatVoiceServeInput{Config: cfg, Chat: chatRuntime, Media: options.ChatMedia, Now: options.Now, Tenants: cfg.ServedTenants()})
	logChatVoiceComposition(logger, chatVoiceRuntime, chatVoiceReason, chatVoiceErr)
	if chatVoiceErr == nil && chatVoiceRuntime != nil {
		in.Voice = chatVoiceRuntime.Service
	}
	agentServiceInput := agentServedAssemblyInput{
		Core: in.Pool, AgentDatabase: agentDatabase, Cell: cell, Personas: personaWiring,
		Audience: personaAudience, Now: options.Now, WritingStyles: in.WritingStyles,
		Gates: chatRuntime.gates, Renderings: chatRuntime.renderings, ChannelStatus: chatRuntime.status, ChatSearch: chatRuntime.search, Filters: chatRuntime.filters, Locations: chatRuntime.locations, LocationPictures: chatRuntime.locationPictures, ChatMaintenance: chatRuntime.store,
	}
	if announcementSurface != nil {
		agentServiceInput.Announcements = announcementSurface
		agentServiceInput.AnnouncementWorker = announcementWorker
	}
	if projects.store != nil {
		agentServiceInput.Projects, agentServiceInput.ProjectStore = &projects.service, projects.store
	}
	agentServices, err := composeAgentServedAssembly(agentServiceInput)
	if err != nil {
		return nil, fmt.Errorf("compose served agent services: %w", err)
	}
	if agentServices != nil && platformAgentRuntime != nil {
		if err := agentServices.BindPlatform(platformAgentRuntime); err != nil {
			return nil, fmt.Errorf("bind served agent task and memory owners: %w", err)
		}
	}
	// AGENTCOST-006: every served persona run is held to its agent's spend limits.
	if agentServices != nil && agentServices.Access != nil && servedPersonaInvocation != nil && servedPersonaInvocation.Worker != nil {
		agentServices.Access.Gate.Logger = logger
		servedPersonaInvocation.Worker.BindCostGate(agentServices.Access.Gate)
	}
	var schedulerWorkload, progressWorkload bootstrap.Workload
	if cfg.Scheduler {
		schedulerWorkload, err = composeServedSchedulers(cfg, in.Pool, in.Identity, cell, telemetryProvider, logger, options.Now)
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
	// Workflow-control reads use this separate control projection. The admin
	// inspector below uses inspect.Load through WorkflowInstanceInspector.
	workflowControlReader := app.NewWorkflowControlReader(in.Pool,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	graph.add(ComponentWorkflowControlRead, KindPort, workflowControlReader, ComponentDatabasePool)
	workflowInspector := app.NewWorkflowInstanceInspector(in.Pool,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID), workflowversionstore.Store{DB: in.Pool})
	// EP-WORK-001: the reader above powers WorkService.ListWorkItems and
	// GetWorkItem on both surfaces below.
	operationStore := operationStoreAdapter{store: operationstore.New(in.Pool,
		func(tenant string) string { return pgstore.TenantID(tenant).String() })}

	// PROMO-EXEC-004: the WorkService write surface is driven by the
	// application-owned adapters over this composition's pool, with one
	// ENDPOINT-004 coordinator per composed server.
	workWrites := newServedWorkQueueWrites(in.Pool, options.Now)
	workWritePorts := transporthumanwork.WritePorts{
		Claims: workWrites, Completions: workWrites, Decisions: workWrites,
		Idempotency: endpoint.NewCoordinator(),
	}

	// INTAPI-006: page and stream cursors are signed with the dedicated
	// page-cursor key (plus its retired predecessor while a rotation is in
	// progress), never with the development HMAC key that authenticates
	// credentials. ServeConfig.Validate refuses a configuration that reuses
	// the dev key, so by the time this composition runs the two are
	// distinct by construction.
	pageCursorKey := []byte(cfg.PageCursorKey)
	previousPageCursorKey := []byte(cfg.PageCursorPreviousKey)
	// INTAPI-006: GetThresholdTable is served through the reference
	// promotion-approval decision table until a tenant publishes its own.
	thresholds := newServedThresholds()
	chainRegistry, err := hashchain.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("application: build ledger explorer chain registry: %w", err)
	}
	chainDigester := hashchain.NewDigester(chainRegistry)
	configureAdmin := func(deps *transportadmin.Dependencies) {
		ConfigureLedgerExplorerAdminDependencies(deps, in.Pool, chainDigester)
		deps.WorkflowInspector = workflowInspector
	}

	grpcServer, err := transportcell.NewGRPCServerWithWorkflowInspectorAndOperationsAndChatAndAdminDependenciesAndServices(
		cell, workflowControlReader, workQueueReader, operationStore, pageCursorKey, previousPageCursorKey,
		workWritePorts, thresholds, chatRuntime.service, configureAdmin, serviceHandlers)
	if err != nil {
		return nil, err
	}
	var notificationFeed *NotificationFeed
	workflowNotifications, notificationErr := requiredWorkflowNotificationReader(cell.Journey, cfg.ExecutionAuthority)
	if notificationErr != nil {
		return nil, notificationErr
	}
	if workflowNotifications != nil {
		notificationFeed, err = NewNotificationFeed(in.Pool,
			func(tenant kernelvalues.TenantId) (uuid.UUID, error) { return pgstore.TenantID(string(tenant)), nil },
			journeyNotificationVisibility{engine: cell.Journey, notifications: workflowNotifications}, cfg.PageCursorKey, options.Now)
		if err != nil {
			return nil, err
		}
		transportcell.RegisterNotificationFeed(grpcServer, notificationFeed)
	} else {
		logger.Info("hcmnext.notification_feed_unavailable", "reason", "served journey engine does not expose current notification authorization")
	}
	graph.add(ComponentNotificationFeed, KindAdapter, notificationFeed, ComponentDatabasePool, ComponentJourneyEngine)
	transportadmin.RegisterLedgerEvidence(grpcServer, NewLedgerEvidenceExport(in.Pool))
	transportcell.RegisterChatExtensions(grpcServer, integrate1ChatExtensions(chatRuntime))
	transportcell.RegisterDocument(grpcServer, documentRuntime.service, pageCursorKey)
	if projects.store != nil {
		transportproject.Register(grpcServer, transportproject.Dependencies{Service: projects.service, Activity: projects.activity, Search: projects.search})
	}
	graph.add(ComponentGRPCSurface, KindTransport, grpcServer, ComponentCell, ComponentWorkflowControlRead, ComponentNotificationFeed)

	// INTAPI-006: the tunnel bridges a workspace-only server, never the
	// main one, so the operator surfaces stay off the browser route.
	// The browser chat client speaks both chat services over this tunnel,
	// so they are registered here (composed when chat is enabled, generated
	// stubs otherwise) and admitted by the tunnel's service allowlist.
	var positionDB dbport.Beginner
	if in.Pool != nil {
		positionDB = in.Pool
	}
	positionReads := NewPositionReadService(
		positionDB,
		tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID),
		PositionPageViewer(cell.RoleAccess),
		PositionPageAuthorizer(cell.RoleAccess, func(ctx context.Context, principal *trust.Principal) (string, error) {
			if cell.Journey == nil || principal == nil {
				return "", fmt.Errorf("application: viewer assignment is unavailable")
			}
			workers, _, err := cell.Journey.ListWorkers(ctx)
			if err != nil {
				return "", err
			}
			for _, worker := range workers {
				if strings.TrimSpace(worker.SubjectID) == principal.Subject() {
					return strings.TrimSpace(worker.OrgUnit), nil
				}
			}
			return "", fmt.Errorf("application: viewer assignment is unavailable")
		}), options.Now,
	)
	positionDeps := positionTransportDependencies(positionReads)
	var browserProjectService transportproject.Service
	var browserProjectActivity transportproject.ActivityService
	var browserProjectSearch transportproject.TaskSearchService
	if projects.store != nil {
		browserProjectService = projects.service
		browserProjectActivity = projects.activity
		browserProjectSearch = projects.search
	}
	// The browser reads and drives the worker self clock over the same tunnel
	// as every other page; nil is the workspace not running the clock.
	var browserWorkerClock transporttimeclock.WorkerSelfService
	if clockRuntime != nil && clockRuntime.Worker != nil {
		browserWorkerClock = clockRuntime.Worker
	}
	tunnelServer, err := transportcell.NewTunnelGRPCServerWithWorkerClock(
		cell, workflowControlReader, workQueueReader, pageCursorKey, previousPageCursorKey, workWritePorts, thresholds,
		chatRuntime.service, integrate1ChatExtensions(chatRuntime), documentRuntime.service, positionDeps, browserProjectService, browserProjectActivity, browserProjectSearch, workOrderDeps, browserWorkerClock)
	if err != nil {
		return nil, err
	}
	reviewParticipantsReads := ReviewParticipantsReadService{
		Graphs: performancestore.New(in.Pool), Roles: cell.RoleAccess,
		ResolveGraphTenant: func(tenant kernelvalues.TenantId) (performance.TenantID, error) {
			return pgstore.TenantID(string(tenant)), nil
		},
		ResolveWorker: func(ctx context.Context, principal *trust.Principal) (string, error) {
			if cell.Journey == nil || principal == nil {
				return "", ErrReviewParticipantsBinding
			}
			workers, _, err := cell.Journey.ListWorkers(ctx)
			if err != nil {
				return "", err
			}
			for _, worker := range workers {
				if strings.TrimSpace(worker.SubjectID) == principal.Subject() {
					workerID := strings.TrimSpace(worker.WorkerID)
					if workerID != "" {
						return workerID, nil
					}
					return "", ErrReviewParticipantsBinding
				}
			}
			return "", ErrReviewParticipantsBinding
		},
	}
	transportreviewparticipants.Register(tunnelServer, transportreviewparticipants.Dependencies{
		Read:     reviewParticipantsReads.Read,
		IsDenied: func(err error) bool { return errors.Is(err, ErrReviewParticipantsDenied) },
	})
	edgeHandler, err := transportcell.NewEdgeHandlerWithTunnelAndDependenciesAndChatAndServices(
		cell, tunnelServer, workflowControlReader, workQueueReader, operationStore, pageCursorKey, previousPageCursorKey, workWritePorts, thresholds, chatRuntime.service, serviceHandlers)
	if err != nil {
		return nil, err
	}
	edgeHandler = transportcell.OverlayChatExtensions(edgeHandler, cell.Config, integrate1ChatExtensions(chatRuntime))
	edgeHandler = overlayIntegrate1Chat(edgeHandler, chatRuntime, in.Voice, cell.Config, chatremoveNameDirectory{Read: documentOwnerNames(in.Pool)})
	edgeHandler = OverlayChatcmd002Cards(edgeHandler, chatcmd002CardPort(chatRuntime), cell.Config)
	if cfg.ChatEnabled {
		mediaCfg := options.ChatMedia.WithDefaults(cfg.ChatMediaRoot, cfg.ArtifactRoot)
		if chatRuntime.extensions != nil {
			mediaCfg.Authorize = chatRuntime.extensions.AuthorizeMedia
		} else {
			mediaCfg.Authorize = nil
		}
		edgeHandler, err = chatattach001Overlay(edgeHandler, mediaCfg, cell.Config, cfg.Profile, chatRuntime)
		if err != nil {
			return nil, fmt.Errorf("compose chat attachments: %w", err)
		}
	}
	if personaWiring != nil && chatRuntime.service != nil && agentDatabase.store != nil {
		personaChat, surfaceErr := personaWiring.chatSurface(chatRuntime.service, agentDatabase.store)
		if surfaceErr != nil {
			return nil, fmt.Errorf("compose persona chat surface: %w", surfaceErr)
		}
		personaChat.Share = NewPersonaAnswerShareGate(personaShareFloor, documentRuntime.store)
		if chatRuntime.store != nil {
			// AGENTUX-070: a channel's manager requires private agent answers in the
			// channel's own persona policy.
			personaChat.ChannelPolicies = chatRuntime.store
		}
		if chatRuntime.core != nil {
			// CHATBUG-067: the private card to share is read from the Chat service
			// itself; the served decorator chain does not pass that read through.
			personaChat.Cards = chatRuntime.core
		}
		if in.WritingStyles != nil {
			in.WritingStyles.Authority = ChattoneChatAuthority{Chat: personaChat}
		}
		// CHATSEARCH-003: agents, a person's own tasks and their own
		// announcements are found from Chat search.
		if searchErr := composeChatSearchAgents(chatRuntime.search, personaChat, platformAgentRuntime, announcementSurface); searchErr != nil {
			return nil, fmt.Errorf("compose agent search: %w", searchErr)
		}
		edgeHandler = OverlayPersonaChatSurface(edgeHandler, personaChat, cell.Config,
			PersonaChatBrowserOptions{PublicOrigin: cell.PublicOrigin(), BrowserLogin: cell.BrowserLoginEnabled(), Icons: agentDatabase.personas})
	}
	if agentServices == nil {
		agentServices = &agentServedAssembly{browserLogin: cell.BrowserLoginEnabled(), publicOrigin: cell.PublicOrigin(), Gates: chatRuntime.gates, Renderings: chatRuntime.renderings, ChannelStatus: chatRuntime.status, ChatSearch: chatRuntime.search, Filters: chatRuntime.filters, Locations: chatRuntime.locations, LocationPictures: chatRuntime.locationPictures, ChatMaintenance: chatRuntime.store}
	}
	if err := agentServices.BindAgentUX(chatRuntime.store, chatRuntime.extensions, agentDatabase.store, options.Now); err != nil {
		return nil, fmt.Errorf("bind served ambient agents: %w", err)
	}
	edgeHandler = agentServices.Overlay(edgeHandler, cell.Config)
	edgeHandler = OverlayChatVoiceFeatures(edgeHandler, in.Voice, cell.Config)
	if localPersonaBootstrap != nil {
		edgeHandler = OverlayLocalDevPersonaBootstrap(edgeHandler, localPersonaBootstrap, cell.Config,
			LocalDevPersonaBootstrapBrowserOptions{PublicOrigin: cell.PublicOrigin(), BrowserLogin: cell.BrowserLoginEnabled()})
	}
	if documentRuntime.store != nil {
		edgeHandler = OverlayDocumentMedia(edgeHandler, documentRuntime.store, DefaultDocumentMediaRoot(cfg.ChatMediaRoot, cfg.ArtifactRoot), cell.Config)
		edgeHandler = transportcell.DocumentHTTPOverlay(edgeHandler, cell.Config, documentRuntime.service, pageCursorKey)
	}
	configBundleControl, err := newConfigBundleControl(cfg, cell.Config.Now, dataconfigregistry.New(in.Pool))
	if err != nil {
		return nil, err
	}
	if configBundleControl != nil {
		configBundleControl.configureKillSwitchPersistence(dataconfigbundlekill.New(in.Pool, configBundleControl.receiptPublic))
		edgeHandler = overlayConfigBundleControl(edgeHandler, cell.Config, configBundleControl)
		graph.add(ComponentConfigBundleControl, KindAdapter, configBundleControl, ComponentConfig, ComponentCredentialVerifier)
	}
	if in.Pool != nil {
		registry, _, registryErr := health.LoadOrDefaultDispositionRegistry(health.DefaultRegistryPath)
		if registryErr != nil {
			return nil, fmt.Errorf("load store-health disposition registry: %w", registryErr)
		}
		probe := health.NewProbe(in.Pool, registry, health.DefaultPolicy())
		edgeHandler = overlayStoreHealth(edgeHandler, cell.Config, probe, platformcache.New[health.Snapshot](platformcache.Config{TTL: 10 * time.Second}))
		graph.add(ComponentStoreHealth, KindAdapter, probe, ComponentDatabasePool, ComponentCredentialVerifier)
	}
	edgeHandler = composeSIEMHTTP(edgeHandler, cell.Config, options.SIEMRingResolver, in.Pool)
	providerWebhooks, err := composeProviderWebhookReceivers(cfg, in.Pool, options.Now)
	if err != nil {
		return nil, err
	}
	if providerWebhooks.payroll != nil || providerWebhooks.iam != nil {
		edgeHandler = transportwebhook.OverlayProviderReceivers(edgeHandler, providerWebhooks.payroll, providerWebhooks.iam)
		graph.add(ComponentProviderWebhookReceivers, KindTransport, providerWebhooks, ComponentDatabasePool, ComponentConfig)
	}
	kioskAssets := cfg.ClockKiosk
	if options.ClockKiosk.WASMPath != "" || options.ClockKiosk.WASMExecPath != "" {
		kioskAssets = options.ClockKiosk
	}
	edgeHandler, err = overlayClockKiosk(edgeHandler, kioskAssets)
	if err != nil {
		return nil, err
	}
	httpDependencies := []string{ComponentCell, ComponentGRPCSurface, ComponentEdgeQualification}
	if providerWebhooks.payroll != nil || providerWebhooks.iam != nil {
		httpDependencies = append(httpDependencies, ComponentProviderWebhookReceivers)
	}
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
	if routes, routeErr := machineAuth.routes(cfg, httpListener.Addr().String()); routeErr != nil {
		_ = grpcListener.Close()
		_ = httpListener.Close()
		return nil, routeErr
	} else {
		edgeHandler = mountMachineAuth(edgeHandler, routes)
	}
	graph.add(ComponentHTTPEdge, KindTransport, edgeHandler, httpDependencies...)
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
			Run: func(ctx context.Context) error {
				finished := make(chan error, 1)
				go func() { finished <- grpcServer.Serve(grpcListener) }()
				select {
				case err := <-finished:
					if err != nil && !errors.Is(err, net.ErrClosed) {
						return err
					}
					return nil
				case <-ctx.Done():
					// Bootstrap drains workloads before running its ordered
					// shutdown steps. Serve ignores context, so quiesce the
					// listener here; the later step remains an idempotent guard.
					stopped := make(chan struct{})
					go func() { grpcServer.GracefulStop(); close(stopped) }()
					select {
					case <-stopped:
					case <-time.After(ShutdownGrace / 2):
						grpcServer.Stop()
						<-stopped
					}
					if err := <-finished; err != nil && !errors.Is(err, net.ErrClosed) {
						return err
					}
					return nil
				}
			},
		},
		{
			Name: workloadNameHTTP,
			Run: func(ctx context.Context) error {
				finished := make(chan error, 1)
				go func() { finished <- httpServer.Serve(httpListener) }()
				select {
				case err := <-finished:
					if err != nil && !errors.Is(err, http.ErrServerClosed) {
						return err
					}
					return nil
				case <-ctx.Done():
					shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ShutdownGrace/2)
					defer cancel()
					if err := httpServer.Shutdown(shutdownCtx); err != nil {
						_ = httpServer.Close()
						return err
					}
					if err := <-finished; err != nil && !errors.Is(err, http.ErrServerClosed) {
						return err
					}
					return nil
				}
			},
		},
	}
	if configBundleControl != nil {
		workloads = append(workloads, bootstrap.Workload{
			Name: "configbundle-receiver",
			Run: func(ctx context.Context) error {
				return configBundleControl.runReceiver(ctx, time.Second)
			},
		})
	}
	if cfg.Scheduler {
		workloads = append(workloads, schedulerWorkload, progressWorkload)
	}
	if personaWorkload := personaInvocationBackgroundWorkload(servedPersonaInvocation, cfg.ServedTenants(), logger); personaWorkload != nil {
		workloads = append(workloads, *personaWorkload)
	}
	if translationWorkload := chatlangBackgroundWorkload(chatlangRuntime); translationWorkload != nil {
		workloads = append(workloads, *translationWorkload)
	}
	if voiceWorkload := chatVoiceBackgroundWorkload(chatVoiceRuntime); voiceWorkload != nil {
		workloads = append(workloads, *voiceWorkload)
	}
	if ephemeralWorkload := agentEphemeralPruneWorkload(chatRuntime.store, cfg.ServedTenants(), options.Now, logger); ephemeralWorkload != nil {
		workloads = append(workloads, *ephemeralWorkload)
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
			Name: shutdownNameChat,
			Run: func(context.Context) error {
				if chatRuntime.close != nil {
					chatRuntime.close()
				}
				return nil
			},
		},
		{
			Name: shutdownNameAgentDatabase,
			Run: func(context.Context) error {
				if agentDatabase.close != nil {
					agentDatabase.close()
				}
				return nil
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
	if documentRuntime.close != nil {
		shutdown = append(shutdown, bootstrap.ShutdownStep{
			Name: shutdownNameDocument,
			Run: func(context.Context) error {
				documentRuntime.close()
				return nil
			},
		})
	}
	if workOrders.close != nil {
		shutdown = append(shutdown, bootstrap.ShutdownStep{
			Name: shutdownNameWorkOrder,
			Run:  func(context.Context) error { workOrders.close(); return nil },
		})
	}
	if clockRuntime != nil {
		shutdown = append(shutdown, bootstrap.ShutdownStep{
			Name: "shutdown:close-time-database",
			Run:  func(context.Context) error { clockRuntime.Close(); return nil },
		})
	}
	if projects.close != nil {
		shutdown = append(shutdown, bootstrap.ShutdownStep{
			Name: "shutdown:close-project-database",
			Run:  func(context.Context) error { projects.close(); return nil },
		})
	}
	if localPolicyRegistry.close != nil {
		shutdown = append(shutdown, bootstrap.ShutdownStep{Name: "shutdown:close-agent-policy-authority", Run: func(context.Context) error {
			localPolicyRegistry.close()
			return nil
		}})
	}
	graph.add(ComponentWorkloadGRPC, KindWorkload, workloads[0].Run, ComponentGRPCSurface)
	graph.add(ComponentWorkloadHTTP, KindWorkload, workloads[1].Run, ComponentHTTPEdge)
	if configBundleControl != nil {
		graph.add(ComponentConfigBundleReceiver, KindWorkload, func(ctx context.Context) error {
			return configBundleControl.runReceiver(ctx, time.Second)
		}, ComponentConfigBundleControl)
	}
	if cfg.Scheduler {
		graph.add(ComponentWorkloadScheduler, KindWorkload, schedulerWorkload.Run, ComponentCell, ComponentDatabasePool)
		graph.add(ComponentWorkloadProgress, KindWorkload, progressWorkload.Run, ComponentDatabasePool, ComponentTelemetryProvider)
	}
	graph.add(ComponentShutdownHTTP, KindShutdown, shutdown[0].Run, ComponentHTTPEdge)
	graph.add(ComponentShutdownGRPC, KindShutdown, shutdown[1].Run, ComponentGRPCSurface)
	graph.add(ComponentShutdownChat, KindShutdown, shutdown[2].Run, ComponentChatService)
	if agentDatabase.store != nil {
		graph.add(ComponentShutdownAgentDatabase, KindShutdown, shutdown[3].Run, ComponentAgentDatabasePool)
	} else {
		graph.add(ComponentShutdownAgentDatabase, KindShutdown, shutdown[3].Run)
	}
	graph.add(ComponentShutdownTelemetry, KindShutdown, shutdown[4].Run, ComponentTelemetryProvider)
	if documentRuntime.close != nil {
		graph.add(ComponentShutdownDocument, KindShutdown, shutdown[5].Run, ComponentDocumentStore)
	}
	if projects.close != nil {
		graph.add("shutdown:close-project-database", KindShutdown, func(context.Context) error { projects.close(); return nil }, ComponentProjectService)
	}
	if workOrders.close != nil {
		graph.add(ComponentShutdownWorkOrder, KindShutdown, func(context.Context) error { workOrders.close(); return nil }, ComponentWorkOrderService)
	}

	// From here the caller's lifecycle owns the provider's lifetime through
	// the ordered shutdown step above. Earlier returns leave this function
	// responsible for cleaning up the partially composed provider.
	clockCommitted = true
	telemetryCommitted = true
	agentDatabaseCommitted = true
	chatCommitted = true
	documentCommitted = true
	projectsCommitted = true
	workOrdersCommitted = true
	localPolicyRegistryCommitted = true
	return &App{
		role:                  RoleServe,
		graph:                 graph.graph(),
		logger:                logger,
		cell:                  cell,
		analytics:             analyticsService,
		installationLifecycle: installationLifecycle,
		assuranceRegister:     assuranceRegister,
		disposition:           disposition,
		telemetryLifecycle:    telemetryLifecycle,
		pilotReliability:      pilotReliability,
		grpcAddr:              grpcListener.Addr().String(),
		httpAddr:              httpListener.Addr().String(),
		workloads:             workloads,
		shutdown:              shutdown,
		listeners:             []net.Listener{grpcListener, httpListener},
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

// composeEvidenceStore builds the durable evidence store over pool, scoping a
// capability decision's tenant key to the same storage tenant the composed
// intent store derives. A nil pool yields a store that refuses every record
// with evidencestore.ErrInvalid rather than one holding a typed-nil pool: a
// composition that has no database and still records evidence must supply
// its own sink explicitly (WithEvidence), never fall back to memory.
func composeEvidenceStore(pool *pgxadapter.Pool) *evidencestore.Store {
	var db dbport.Beginner
	if pool != nil {
		db = pool
	}
	return evidencestore.New(db, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
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
// Tenant IdP configuration selects the federation verifier
// (internal/trust/federation over the governed issuer registry); otherwise
// the development HMAC verifier composes, which is available only through
// an explicit -dev-hmac-key or the local-dev profile default.
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
	if federationConfigured(cfg) {
		verifier, err := composeFederationVerifier(cfg, options.Now)
		if err != nil {
			return nil, fmt.Errorf("build the federation verifier: %w", err)
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

// devTokenIssuerOf returns the development token issuer behind the served
// verifier. The machine-client ServedVerifier (INTAPI-001) wraps the local-dev
// HMAC verifier as its Dev fallback and deliberately does not expose Issue
// itself, so the issuer is taken from Dev; outside local-dev Dev is nil and no
// issuer exists.
func devTokenIssuerOf(verifier trust.Verifier) (developmentTokenIssuer, bool) {
	if issuer, ok := verifier.(developmentTokenIssuer); ok {
		return issuer, true
	}
	if served, ok := verifier.(*trust.ServedVerifier); ok && served.Dev != nil {
		return served.Dev, true
	}
	if wrapped, ok := verifier.(*machineFallbackVerifier); ok {
		if issuer, ok := wrapped.fallback.(developmentTokenIssuer); ok {
			return issuer, true
		}
	}
	return nil, false
}

// composeDevPersonas creates local identities only when the operator enabled
// the dev browser login and the configured verifier can issue development
// tokens. A federation verifier therefore never acquires an implicit issuer.
// ConfigureLedgerExplorerAdminDependencies wires the read-only ledger ports
// over the caller's existing tenant-scoped querier. Until a trusted policy
// and classification source can authorize raw ledger values, payload and
// provenance are explicitly withheld; OperatorRole alone never opens them.
func ConfigureLedgerExplorerAdminDependencies(deps *transportadmin.Dependencies, q dbport.Querier, digester *hashchain.Digester) {
	if deps == nil {
		return
	}
	deps.ResolveLedgerTenant = func(_ context.Context, tenantKey string) (uuid.UUID, error) {
		return pgstore.TenantID(tenantKey), nil
	}
	if q == nil || digester == nil {
		return
	}
	deps.ListLedgerStream = func(ctx context.Context, tenant uuid.UUID, stream string) (explorer.StreamListingView, error) {
		return explorer.StreamListing(ctx, q, tenant, stream, ledgerMetadataOnlyDecision())
	}
	deps.VerifyLedgerChain = func(ctx context.Context, tenant uuid.UUID, stream string) (explorer.ChainView, error) {
		return explorer.VerifyChain(ctx, q, digester, tenant, stream)
	}
}

func ledgerMetadataOnlyDecision() *authz.Decision {
	return &authz.Decision{
		PolicyVersions:     []string{authz.PolicyVersion},
		Purpose:            "operator_diagnostics",
		SubjectDisclosable: true,
		Fields: map[authz.FieldID]authz.FieldRuling{
			explorer.FieldPayload:    {Effect: authz.EffectDenied, RuleID: "admin.explorer.policy_unavailable"},
			explorer.FieldProvenance: {Effect: authz.EffectDenied, RuleID: "admin.explorer.policy_unavailable"},
		},
	}
}

func composeDevPersonas(verifier trust.Verifier, cfg ServeConfig, now func() time.Time) []workspace.DevPersona {
	issuer, ok := devTokenIssuerOf(verifier)
	if !ok {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	var personas []workspace.DevPersona
	for _, pack := range devServedCompanies(cfg) {
		personas = append(personas, composeCompanyPersonas(issuer, cfg, pack, now)...)
	}
	return personas
}

// composeCompanyPersonas mints one company's four quick-pick personas. The
// default tenant's personas keep the bare slot ids ("admin"); every other
// company's are namespaced by its tenant ("ironridge-demo:admin"), so two
// companies' quick picks can never shadow one another.
//
// The worker each slot signs in as, its access label and its purpose are the
// company's own pack data (demoworkforce.Pack.Personas); the role bundle each
// persona is issued comes from workspace.DevPersonaRoleSets, the one fixture
// the sign-in page's derived copy (loginPersonaDescription) and this
// package's own tests also read, so a persona's promised copy and its signed
// roles cannot drift apart (UXAUDIT-014 REFACTOR).
//
// PROMOUX-015: for HarborCare the four slots are one separated promotion.
// admin (Rafael Torres, Director of People Operations) is the manager
// approver -- he manages Linh Tran -- and the execution operator.
// hiring-manager (Darius Bennett, Chief People Officer) is the proposer:
// Linh's skip-level manager, so the reference workflow's
// CurrentManagerOf(worker) approval routes to somebody else. finance-partner
// (Thomas Baker, Finance Director) is the finance approver the local-dev
// profile's -execution-finance-partner names. individual-contributor (Linh
// Tran) is the employee. The finance-partner slot's finance_partner role is
// backed by authz.PolicyTable under compensation_review, the purpose it
// signs; do not give it a payroll label or purpose (UXAUDIT-014).
// localPersonaPurposes derives every local sign-in purpose from the same P1A
// rule table production sessions use. A persona mention is only declared when
// the member's effective role bundle has its explicit invocation rule; the
// later agent gate still narrows skills, audience and organization scope.
func localPersonaPurposes(primary string, roleBundles ...[]string) []string {
	roles := []string{string(authz.RoleWorkerSelf)}
	if len(roleBundles) > 0 {
		roles = roleBundles[0]
	}
	purposes := []string{primary}
	if primary != authz.PurposePersonaMention && authz.RolesAuthorizePurpose(roles, authz.PurposePersonaMention) {
		purposes = append(purposes, authz.PurposePersonaMention)
	}
	return purposes
}

func composeCompanyPersonas(issuer developmentTokenIssuer, cfg ServeConfig, pack *demoworkforce.Pack, now func() time.Time) []workspace.DevPersona {
	workers, err := pack.Plan(pgstore.TenantID(pack.Key))
	if err != nil {
		return nil
	}
	workersByNumber := make(map[string]demoworkforce.Employee, len(workers))
	for _, worker := range workers {
		workersByNumber[worker.Row.WorkerNumber] = worker
	}
	personas := make([]workspace.DevPersona, 0, len(pack.Personas))
	for _, spec := range pack.Personas {
		worker, found := workersByNumber[spec.WorkerNumber]
		if !found || worker.Row.WorkerKey == "" || worker.Row.LegalName == "" || worker.Row.LifecycleStatus != "active" {
			continue
		}
		roles, ok := workspace.DevPersonaRoles(spec.ID)
		if !ok {
			// No canonical role bundle is named for this persona id: issuing
			// an unscoped credential would be worse than not offering the
			// persona at all.
			continue
		}
		id := spec.ID
		if pack.Key != cfg.Tenant {
			id = pack.Key + ":" + spec.ID
		}
		claims := trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: worker.Row.WorkerKey, SubjectKind: "human", Tenant: pack.Key,
			OrganizationScopeID: pack.OrgScope(), Roles: roles, Purposes: localPersonaPurposes(spec.Purpose, roles),
			AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-local-persona-" + id,
		}
		issueToken := func() (string, error) {
			timestamp := now().UTC()
			claims.IssuedAtUnix = timestamp.Add(-time.Minute).Unix()
			claims.ExpiresAtUnix = timestamp.Add(8 * time.Hour).Unix()
			return issuer.Issue(claims)
		}
		token, err := issueToken()
		if err != nil {
			continue
		}
		access := spec.Access
		if access == "" {
			// Derived from this worker's own record (their real job title),
			// not hand-written, so a slot with no access label of its own can
			// only ever say what this specific credential actually is. No
			// current spec leaves access blank; the fallback stays so a future
			// one cannot re-assert a capability its role does not hold.
			access = worker.JobTitle + " (self-service)"
		}
		personas = append(personas, workspace.DevPersona{ID: id, Name: worker.Row.LegalName, Access: access, Roles: roles, Token: token, IssueToken: issueToken, WorkerRef: worker.Row.WorkerKey, Company: pack.Key, Slot: spec.ID})
	}
	return personas
}

// ServeSpec declares the serve role for internal/platform/bootstrap: its
// configuration, its validation, its database dependency and the workloads
// bootstrap runs and drains. It is the one call a command makes.
func ServeSpec(args []string, opts ...Option) bootstrap.Spec {
	options := Options{}.Apply(opts...)
	fields := ServeConfigFieldsForArgs(args)
	if options.CurrentHealthConfigFingerprint == nil {
		configArgs := append([]string(nil), args...)
		configFields := append([]bootstrap.Field(nil), fields...)
		options.CurrentHealthConfigFingerprint = func() (string, error) {
			current, err := bootstrap.ParseConfig(configArgs, os.LookupEnv, configFields)
			if err != nil {
				return "", err
			}
			return current.Fingerprint(), nil
		}
	}

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
