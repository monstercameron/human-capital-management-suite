// Package cell composes an application cell onto its published wire
// surfaces.
//
// It is intentionally the only adapter that knows both an app.Cell and the
// gRPC/Connect server constructors. Application code stays free of RPC
// runtime types; transport owns listener-facing composition and nothing
// semantic. That split is not a style preference here: LIB-003
// (definitions/architecture/library-firewall.yaml, protobuf_grpc) qualifies
// grpc-go, Connect and Protobuf as wire mechanics importable only from gen/,
// internal/transport, internal/intent/protomap, internal/engines/wire,
// tools/gen and the composition roots (cmd/*) - internal/intent/app is not on
// that list, so this package, not app.Cell, is what is allowed to call
// grpcserver.NewServer/edge.NewHandler, chain the otelmw interceptors, and
// render the discovery document's protojson error body.
//
// A composed *app.Cell already carries everything this package reads:
// Config (the shared admission configuration), Discovery (the rendered
// API-001 document), Telemetry (the OTel provider, or nil), and the
// WorkspaceEnabled/DevBrowserLogin decisions made at composition. This
// package adds no new decisions of its own; it only has the import rights
// app.Cell is not allowed to have.
package cell

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect"
	positionv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportadmin "github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	transportextensions "github.com/monstercameron/human-capital-management-suite/internal/transport/chatextensions"
	transportdataops "github.com/monstercameron/human-capital-management-suite/internal/transport/dataops"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	transporthealth "github.com/monstercameron/human-capital-management-suite/internal/transport/health"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/openapidoc"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/otelmw"
	transportposition "github.com/monstercameron/human-capital-management-suite/internal/transport/position"
	transportproject "github.com/monstercameron/human-capital-management-suite/internal/transport/project"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ServiceHandlers are the application-owned operator handlers additionally
// exposed on both published surfaces. The cell package only forwards these
// ports into the canonical admission and telemetry chains.
type ServiceHandlers struct {
	DataOps              transport.DataOpsHandler
	DataOpsStage         transportdataops.StageCSVHandler
	Integration          transport.IntegrationHandler
	IntegrationPublisher http.Handler
	Parameters           http.Handler
	Project              *transportproject.Dependencies
}

// NewGRPCServer builds the canonical gRPC surface over an already composed
// application cell. grpcserver owns the required admission interceptor
// chain; opts can only add mechanics after it. When c was composed with a
// Telemetry provider, NewGRPCServer chains otelmw.UnaryServerInterceptor and
// otelmw.StreamServerInterceptor after admission and after opts, so every
// call of either cardinality - including one added through a caller's own
// opts - is instrumented; a cell composed with no Telemetry adds nothing
// here at all.
//
// It is exactly [NewGRPCServerWithWorkflowInspector] with no workflow
// instance reader: AdminService.GetWorkflowInstance (ADMIN-008) is then
// UNAVAILABLE, matching every other optional transportadmin.Dependencies
// port a caller does not wire.
func NewGRPCServer(c *app.Cell, opts ...grpc.ServerOption) (*grpc.Server, error) {
	return NewGRPCServerWithWorkflowInspectorAndOperations(c, nil, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, opts...)
}

// NewGRPCServerWithWorkflowInspector is [NewGRPCServer] plus ADMIN-008's
// workflow-inspector wiring for AdminService.GetWorkflowInstance.
//
// instances is the application-side control projection used by workflow
// control. The admin inspector is configured independently through
// [configureAdmin] and reads its durable view with inspect.Load. c itself
// does not carry the control reader because workflow-control transport is
// composed separately from the execution authority. Nil disables that
// optional control projection.
func NewGRPCServerWithWorkflowInspector(
	c *app.Cell, instances app.WorkflowControlReader, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return NewGRPCServerWithWorkflowInspectorAndOperations(c, instances, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, opts...)
}

// NewGRPCServerWithWorkflowInspectorAndOperations is
// [NewGRPCServerWithWorkflowInspector] plus the durable operation store and
// the shared cursor-signing key used by both published transports. The
// application root owns construction of these adapters; this package only
// threads the already-composed ports into the listener-facing services.
//
// cursorKey signs new page and stream cursors; previousCursorKey is the
// retired signing key, accepted for verification only while in-flight
// cursors minted under it drain. thresholds resolves the read-only
// decision table WorkService.GetThresholdTable serves; nil keeps that one
// method UNAVAILABLE.
func NewGRPCServerWithWorkflowInspectorAndOperations(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newGRPCServerWithWorkflowInspectorAndOperations(c, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, nil, ServiceHandlers{}, opts...)
}

// NewGRPCServerWithWorkflowInspectorAndOperationsAndAdminDependencies is the
// canonical server constructor with a composition-root hook for additional
// trusted AdminService ports. The hook receives the ordinary cell-backed
// defaults; callers may fill optional ports but must not replace admission or
// authorization behavior in the handlers.
func NewGRPCServerWithWorkflowInspectorAndOperationsAndAdminDependencies(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, configureAdmin func(*transportadmin.Dependencies), opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newGRPCServerWithWorkflowInspectorAndOperations(c, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, configureAdmin, ServiceHandlers{}, opts...)
}

// NewGRPCServerWithWorkflowInspectorAndOperationsAndChatAndAdminDependenciesAndServices
// composes DataOps and Integration on the canonical server. StageCSV remains
// native gRPC because its generated contract is client-streaming.
func NewGRPCServerWithWorkflowInspectorAndOperationsAndChatAndAdminDependenciesAndServices(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store, cursorKey, previousCursorKey []byte,
	workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds,
	chatService chatcore.ConversationService, configureAdmin func(*transportadmin.Dependencies),
	services ServiceHandlers, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	srv, err := newGRPCServerWithWorkflowInspectorAndOperations(c, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, configureAdmin, services, opts...)
	if err != nil {
		return nil, err
	}
	if chatService != nil {
		RegisterChat(srv, chatService)
	}
	return srv, nil
}

func newGRPCServerWithWorkflowInspectorAndOperations(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, configureAdmin func(*transportadmin.Dependencies),
	services ServiceHandlers, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	if c == nil {
		return nil, fmt.Errorf("transport cell: application cell is required")
	}
	// Always chained: with no Telemetry provider the interceptors still put
	// the request and correlation ids into the logging context, so every log
	// line a call writes carries them; only the span is skipped.
	opts = append(opts,
		grpc.ChainUnaryInterceptor(otelmw.UnaryServerInterceptor(c.Telemetry)),
		grpc.ChainStreamInterceptor(otelmw.StreamServerInterceptor(c.Telemetry)),
	)
	srv, err := grpcserver.NewServer(grpcserver.Options{
		Config: c.Config, Intent: c.Service, Registry: c.Service,
		DataOps: services.DataOps, DataOpsStage: services.DataOpsStage, Integration: services.Integration,
		ServerOptions: opts,
	})
	if err != nil {
		return nil, err
	}
	// The operator surface (SVC-011 / ADMIN-001) is hosted by the same
	// server under the same interceptor chain; its distinct trust policy is
	// the operator role check inside internal/operations/admin, not a
	// second listener.
	adminDeps := transportadmin.Dependencies{
		Intent:             c.Service,
		WorkerFacts:        c.Workers,
		TransactionHistory: c.Transactions,
		CapabilityRegistry: c.Capabilities,
		Now:                c.Config.Now,
	}
	if configureAdmin != nil {
		configureAdmin(&adminDeps)
	}
	transportadmin.Register(srv, adminDeps)
	// The Promotion journey service (UX-009) is hosted by the same server
	// under the same interceptor chain - both halves of it, since
	// WatchJourney is a server-streaming RPC and grpcserver.NewServer chains
	// grpcserver.StreamInterceptor beside the unary one. c.Journey is nil on
	// a cell composed without the P1B execution authority and an execution
	// database; the service then answers UNAVAILABLE rather than being
	// absent, so a client learns the surface exists and why it cannot act.
	//
	// PollInterval is left at the package default: how hard the watch reads
	// the engine is a property of the service, and this composition has no
	// reason to hold an opinion about it.
	transportjourney.Register(srv, transportjourney.Dependencies{
		Engine: c.Journey, Preferences: c.Preferences, RoleAccess: c.RoleAccess, WorkerIDs: c.WorkerIDs,
		Knowledge:         app.KnowledgeSearchService{Source: c.KnowledgeSearch},
		CursorKey:         append([]byte(nil), cursorKey...),
		PreviousCursorKey: append([]byte(nil), previousCursorKey...),
		Invalidations:     journeyInvalidations(c),
	})
	// The workflow transport consumes its string-ID reader port. The existing
	// application reader remains owned by AdminService; this adapter supplies
	// the same durable record without moving database access into transport.
	workflowDeps := workflowDependencies(c, instances, cursorKey, previousCursorKey)
	transportworkflow.Register(srv, workflowDeps)
	// WorkService (EP-WORK-001) publishes the queue read endpoints under the
	// same interceptor chain and cursor key. A nil workQueue leaves the
	// methods answering UNAVAILABLE rather than absent, the same nil-tolerant
	// posture the workflow inspector takes.
	transporthumanwork.Register(srv, transporthumanwork.Dependencies{
		Queue: newWorkQueueReader(workQueue), CursorKey: append([]byte(nil), cursorKey...),
		PreviousCursorKey: append([]byte(nil), previousCursorKey...), Thresholds: thresholds,
		Claims: workWrites.Claims, Completions: workWrites.Completions,
		Decisions: workWrites.Decisions, Idempotency: workWrites.Idempotency,
		Authorize: workAuthorizer(c.RoleAccess),
	})
	transportoperations.Register(srv, transportoperations.Dependencies{Store: operationStore})
	transporthealth.RegisterServer(srv, healthServer(c))
	return srv, nil
}

// NewGRPCServerWithWorkflowInspectorAndOperationsAndChat composes the
// optional chat service on the same authenticated gRPC server.
func NewGRPCServerWithWorkflowInspectorAndOperationsAndChat(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store, cursorKey, previousCursorKey []byte,
	workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds,
	chatService chatcore.ConversationService, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return NewGRPCServerWithWorkflowInspectorAndOperationsAndChatAndAdminDependencies(c, instances, workQueue,
		operationStore, cursorKey, previousCursorKey, workWrites, thresholds, chatService, nil, opts...)
}

// NewGRPCServerWithWorkflowInspectorAndOperationsAndChatAndAdminDependencies
// adds application-composed AdminService ports to the canonical listener.
func NewGRPCServerWithWorkflowInspectorAndOperationsAndChatAndAdminDependencies(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store, cursorKey, previousCursorKey []byte,
	workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds,
	chatService chatcore.ConversationService, configureAdmin func(*transportadmin.Dependencies), opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	srv, err := newGRPCServerWithWorkflowInspectorAndOperations(c, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, configureAdmin, ServiceHandlers{}, opts...)
	if err != nil {
		return nil, err
	}
	if chatService != nil {
		RegisterChat(srv, chatService)
	}
	return srv, nil
}

// NewTunnelGRPCServer builds the workspace-only gRPC surface the tunnel
// bridges. It registers exactly the services the workspace page uses —
// IntentService, JourneyService, WorkflowService and WorkService — under
// the same admission and telemetry interceptor chain as the main server,
// and nothing else: the operator surfaces (AdminService,
// OnboardingService), the registry, the operation store, health and chat
// stay on the direct gRPC surface. [tunnelAllowedServices] names the set
// and TestTunnelServesOnlyWorkspaceServices fails the build if this
// constructor and that set ever disagree.
//
// The tunnel bridges this server, never the main one, so a method the page
// has no business calling is UNIMPLEMENTED at the bridge before
// authentication, authorization or any handler runs.
func NewTunnelGRPCServer(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServer(c, instances, workQueue, cursorKey, previousCursorKey, workWrites, thresholds, nil, nil, opts...)
}

// newTunnelGRPCServer is the one tunnel constructor. The two chat services
// are members of [tunnelAllowedServices] and are always registered: with the
// composed implementation when chat is enabled, with the generated
// Unimplemented stubs otherwise, so a disabled product answers UNIMPLEMENTED
// at the handler and the policy/constructor agreement holds in both modes.
func newTunnelGRPCServer(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocument(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, nil, nil, nil, opts...)
}

func newTunnelGRPCServerWithDocument(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps *transportposition.Dependencies,
	projectService transportproject.Service,
	opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocumentAndProjectActivity(
		c, instances, workQueue, cursorKey, previousCursorKey, workWrites, thresholds,
		chatService, extensions, documentService, positionDeps, projectService, nil, nil, opts...)
}

func newTunnelGRPCServerWithDocumentAndProjectActivity(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps *transportposition.Dependencies,
	projectService transportproject.Service,
	projectActivity transportproject.ActivityService,
	projectSearch transportproject.TaskSearchService,
	opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	if c == nil {
		return nil, fmt.Errorf("transport cell: application cell is required")
	}
	// The same chain as the main server: admission first, then the
	// telemetry interceptors that put the request and correlation ids into
	// the logging context. Mirrors
	// NewGRPCServerWithWorkflowInspectorAndOperations; the two must stay
	// identical or tunneled calls would observe and log differently from
	// direct ones.
	opts = append(opts,
		grpc.ChainUnaryInterceptor(otelmw.UnaryServerInterceptor(c.Telemetry)),
		grpc.ChainStreamInterceptor(otelmw.StreamServerInterceptor(c.Telemetry)),
	)
	srv, err := grpcserver.NewServer(grpcserver.Options{
		Config: c.Config, Intent: c.Service, Registry: nil, ServerOptions: opts,
	})
	if err != nil {
		return nil, err
	}
	transportjourney.Register(srv, transportjourney.Dependencies{
		Engine: c.Journey, Preferences: c.Preferences, RoleAccess: c.RoleAccess, WorkerIDs: c.WorkerIDs,
		Knowledge:         app.KnowledgeSearchService{Source: c.KnowledgeSearch},
		CursorKey:         append([]byte(nil), cursorKey...),
		PreviousCursorKey: append([]byte(nil), previousCursorKey...),
		Invalidations:     journeyInvalidations(c),
	})
	transportworkflow.Register(srv, workflowDependencies(c, instances, cursorKey, previousCursorKey))
	transporthumanwork.Register(srv, transporthumanwork.Dependencies{
		Queue: newWorkQueueReader(workQueue), CursorKey: append([]byte(nil), cursorKey...),
		PreviousCursorKey: append([]byte(nil), previousCursorKey...), Thresholds: thresholds,
		Claims: workWrites.Claims, Completions: workWrites.Completions,
		Decisions: workWrites.Decisions, Idempotency: workWrites.Idempotency,
		Authorize: workAuthorizer(c.RoleAccess),
	})
	registerTunnelChat(srv, chatService, extensions)
	RegisterDocument(srv, documentService, cursorKey)
	if positionDeps == nil {
		positionv1.RegisterPositionServiceServer(srv, positionv1.UnimplementedPositionServiceServer{})
	} else {
		transportposition.Register(srv, *positionDeps)
	}
	if projectService == nil {
		projectv1.RegisterProjectServiceServer(srv, projectv1.UnimplementedProjectServiceServer{})
	} else {
		transportproject.Register(srv, transportproject.Dependencies{Service: projectService, Activity: projectActivity, Search: projectSearch})
	}
	return srv, nil
}

// NewEdgeHandler builds the HTTP/Connect edge over an already composed
// application cell. It serves the non-RPC discovery and optional workspace
// routes beside the RPC projection under the same admission configuration.
// When c was composed with a Telemetry provider, NewEdgeHandler adds
// connect.WithInterceptors(otelmw.NewConnectInterceptor(provider)) through
// edge's HandlerOptions, mirroring NewGRPCServer exactly.
func NewEdgeHandler(c *app.Cell, opts ...connect.HandlerOption) (http.Handler, error) {
	return buildEdgeHandler(c, nil, opts...)
}

// NewEdgeHandlerWithDependencies builds the HTTP edge with the same workflow
// reader, operation store and cursor key as the gRPC surface. It is the
// non-tunnel counterpart of [NewEdgeHandlerWithTunnelAndDependencies].
func NewEdgeHandlerWithDependencies(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, opts ...connect.HandlerOption,
) (http.Handler, error) {
	return buildEdgeHandlerWithDependencies(c, nil, instances, workQueue, operationStore, cursorKey, previousCursorKey, workWrites, thresholds, opts...)
}

// NewEdgeHandlerWithTunnelAndDependenciesAndChat overlays the optional chat
// Connect projection on the canonical edge and leaves the tunnel and all
// existing routes owned by the ordinary edge composition.
func NewEdgeHandlerWithTunnelAndDependenciesAndChat(
	c *app.Cell, grpcServer *grpc.Server, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store, cursorKey, previousCursorKey []byte,
	workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds,
	chatService chatcore.ConversationService, opts ...connect.HandlerOption,
) (http.Handler, error) {
	return NewEdgeHandlerWithTunnelAndDependenciesAndChatAndServices(c, grpcServer, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, chatService, ServiceHandlers{}, opts...)
}

// NewEdgeHandlerWithTunnelAndDependenciesAndChatAndServices adds the DataOps
// and Integration application handlers to the ordinary edge. They use the
// same admission and telemetry middleware as every other Connect procedure.
func NewEdgeHandlerWithTunnelAndDependenciesAndChatAndServices(
	c *app.Cell, grpcServer *grpc.Server, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store, cursorKey, previousCursorKey []byte,
	workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds,
	chatService chatcore.ConversationService, services ServiceHandlers, opts ...connect.HandlerOption,
) (http.Handler, error) {
	h, err := newEdgeHandlerWithDependenciesAndServices(c, grpcServer, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, services, opts...)
	if err != nil {
		return nil, err
	}
	return chatHTTPOverlay(h, c.Config, chatService), nil
}

// buildEdgeHandler is the one edge composition both [NewEdgeHandler] and
// [NewEdgeHandlerWithTunnel] run. A nil grpcServer means no tunnel is
// mounted, which is exactly what NewEdgeHandler has always built; a non-nil
// one adds [TunnelPath] to the same mux and changes nothing else.
func buildEdgeHandler(c *app.Cell, grpcServer *grpc.Server, opts ...connect.HandlerOption) (http.Handler, error) {
	return buildEdgeHandlerWithDependencies(c, grpcServer, nil, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, opts...)
}

func buildEdgeHandlerWithDependencies(c *app.Cell, grpcServer *grpc.Server, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader, operationStore transportoperations.Store, cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds, opts ...connect.HandlerOption) (http.Handler, error) {
	return newEdgeHandlerWithDependenciesAndServices(c, grpcServer, instances, workQueue, operationStore,
		cursorKey, previousCursorKey, workWrites, thresholds, ServiceHandlers{}, opts...)
}

func newEdgeHandlerWithDependenciesAndServices(c *app.Cell, grpcServer *grpc.Server, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader, operationStore transportoperations.Store, cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts, thresholds transporthumanwork.Thresholds, services ServiceHandlers, opts ...connect.HandlerOption) (http.Handler, error) {
	if c == nil {
		return nil, fmt.Errorf("transport cell: application cell is required")
	}
	opts = append(opts, connect.WithInterceptors(otelmw.NewConnectInterceptor(c.Telemetry)))
	rpc, err := edge.NewHandler(edge.Options{
		Config: c.Config, Intent: c.Service, Registry: c.Service,
		DataOps: services.DataOps, Integration: services.Integration, Project: services.Project,
		Journey: &transportjourney.Dependencies{
			Engine: c.Journey, Preferences: c.Preferences, RoleAccess: c.RoleAccess, WorkerIDs: c.WorkerIDs,
			Knowledge:         app.KnowledgeSearchService{Source: c.KnowledgeSearch},
			CursorKey:         append([]byte(nil), cursorKey...),
			PreviousCursorKey: append([]byte(nil), previousCursorKey...),
		},
		Workflow: workflowDependenciesRef(c, instances, cursorKey, previousCursorKey),
		Work: &transporthumanwork.Dependencies{Queue: newWorkQueueReader(workQueue), CursorKey: append([]byte(nil), cursorKey...),
			PreviousCursorKey: append([]byte(nil), previousCursorKey...), Thresholds: thresholds,
			Claims: workWrites.Claims, Completions: workWrites.Completions,
			Decisions: workWrites.Decisions, Idempotency: workWrites.Idempotency,
			Authorize: workAuthorizer(c.RoleAccess)},
		Operations: &transportoperations.Dependencies{Store: operationStore},
		Health:     healthServer(c), HandlerOptions: opts,
	})
	if err != nil {
		return nil, err
	}
	publicOrigin, err := cellPublicOrigin(c.PublicOrigin())
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	var routes []workspace.Route
	if c.WorkspaceEnabled() {
		ws, wsErr := workspace.NewHandler(workspace.Options{
			Cell:            c.WorkspacePort(),
			Config:          c.Config,
			Now:             c.Config.Now,
			DevBrowserLogin: c.DevBrowserLogin(),
			OIDCFlow:        c.OIDCFlow, OIDCTenant: c.OIDCTenant, OIDCIssuerURL: c.OIDCIssuerURL,
			OIDCSessionIssuer: c.OIDCSessionIssuer,
			Secure:            strings.HasPrefix(strings.ToLower(c.OIDCRedirectURI), "https://"),
			DevPersonas:       c.DevPersonas(),
			DevDirectory:      c.DevDirectory(),
			RoleAccess:        c.RoleAccess,
			BrandAssets:       c.BrandAssets,
			Preferences:       c.Preferences,
			PageLedger:        c.PageLedger,
			Catalogs:          c.Catalogs,
			PublicOrigin:      c.PublicOrigin(),
		})
		if wsErr != nil {
			return nil, fmt.Errorf("transport cell: compose the promotion workspace: %w", wsErr)
		}
		mux.Handle(app.WorkspacePath, ws)
		routes = workspace.Routes()
		// A browser opening the bare origin is looking for the workspace,
		// not for an RPC procedure named "/". The pattern is method- and
		// path-exact, so every Connect procedure still falls through to rpc
		// below; a process composed without the workspace keeps the plain
		// RPC answer at the root.
		mux.Handle(RootPattern, rootRedirect(workspace.PathProductHome))
	}
	if services.Parameters != nil {
		mux.Handle("/v1/parameters/", trustedPrincipalHTTP(c.Config, services.Parameters))
	}
	if services.IntegrationPublisher != nil {
		mux.Handle("/v1/integration/connector-definitions", trustedPrincipalHTTP(c.Config, services.IntegrationPublisher))
	}
	discovery, err := newDiscoveryHandler(c.Config, c.Discovery, routes)
	if err != nil {
		return nil, fmt.Errorf("transport cell: render the discovery document: %w", err)
	}
	mux.Handle(app.DiscoveryPath, discovery)
	mux.Handle(openapidoc.Path, trustedPrincipalHTTP(c.Config, openapidoc.Handler()))
	if grpcServer != nil {
		var publicHost string
		if publicOrigin != nil {
			publicHost = publicOrigin.Host
		}
		tunnel, tunnelErr := newTunnelHandler(c, grpcServer, publicHost)
		if tunnelErr != nil {
			return nil, tunnelErr
		}
		mux.Handle(TunnelPath, tunnel)
	}
	mux.Handle("/", rpc)
	return edge.BrowserPolicy(mux, browserPolicyOptions(publicOrigin)), nil
}

func healthServer(c *app.Cell) *transporthealth.Server {
	if c != nil && c.Health != nil {
		return c.Health
	}
	return transporthealth.New(transporthealth.Dependencies{})
}

// trustedPrincipalHTTP authenticates the small non-RPC parameter control API
// with the same verifier, reserved-header screen and credential quota as the
// Connect edge, then gives its application service only the verified
// Principal. Parameter scope, environment and author remain server-derived.
func trustedPrincipalHTTP(cfg transport.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _, err := transport.PreAdmit(r.Context(), cfg, transport.MapMetadata(r.Header), r.URL.Path)
		if err != nil {
			http.Error(w, "request authentication failed", err.HTTPStatus())
			return
		}
		next.ServeHTTP(w, r.WithContext(trust.WithPrincipal(r.Context(), principal)))
	})
}

// cellPublicOrigin parses the deployment's declared public origin once, so
// the browser policy, the workspace shells and the tunnel's origin check all
// bind to the same authority. It returns nil for the empty default. The
// application root has already canonicalized the value; this parse is the
// boundary check for callers that compose a cell without it.
func cellPublicOrigin(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("transport cell: public origin must be an absolute http(s) origin like https://hcm.example.com; got %q", raw)
	}
	return u, nil
}

// browserPolicyOptions derives the edge's browser boundary from the declared
// public origin. An https origin means a proxy terminated TLS upstream: the
// browser's Origin carries that scheme while the listener stays plain HTTP,
// so it must be named explicitly or every state-changing request is refused,
// and the cookies the policy normalizes must be Secure. With no public
// origin the same-origin default remains: the cell is the origin.
func browserPolicyOptions(publicOrigin *url.URL) edge.BrowserPolicyOptions {
	if publicOrigin == nil {
		return edge.BrowserPolicyOptions{}
	}
	return edge.BrowserPolicyOptions{
		AllowedOrigins: []string{publicOrigin.Scheme + "://" + publicOrigin.Host},
		SecureCookies:  publicOrigin.Scheme == "https",
	}
}

// discoveryHandler serves the pre-rendered API-001 discovery document to an
// authenticated caller. It is rendered once, at composition, rather than on
// every request: the served shape is fixed by what was composed, not by
// anything a request could influence.
type discoveryHandler struct {
	config   transport.Config
	document []byte
}

// newDiscoveryHandler pre-renders doc, splicing in the workspace's own
// published routes when the workspace is served, so the discovery document
// never advertises a route this build does not actually mount.
func newDiscoveryHandler(cfg transport.Config, doc *manifest.DiscoveryDocument, routes []workspace.Route) (*discoveryHandler, error) {
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if len(routes) > 0 {
		body, err = spliceWorkspaceRoutes(body, routes)
		if err != nil {
			return nil, err
		}
	}
	return &discoveryHandler{config: cfg, document: body}, nil
}

// spliceWorkspaceRoutes adds the workspace's routes to the rendered discovery
// document under [app.WorkspaceRoutesKey], without decoding and re-encoding
// the document manifest.Build already rendered.
func spliceWorkspaceRoutes(document []byte, routes []workspace.Route) ([]byte, error) {
	encoded, err := json.Marshal(routes)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimRight(document, " \t\r\n")
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("transport cell: the discovery document is not a JSON object")
	}
	out := make([]byte, 0, len(trimmed)+len(encoded)+len(app.WorkspaceRoutesKey)+8)
	out = append(out, trimmed[:len(trimmed)-1]...)
	if len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) > 0 {
		out = append(out, ',')
	}
	out = append(out, '"')
	out = append(out, app.WorkspaceRoutesKey...)
	out = append(out, '"', ':')
	out = append(out, encoded...)
	return append(out, '}'), nil
}

// ServeHTTP implements [http.Handler].
func (h *discoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDiscoveryError(w, envelope.New(envelope.CodeInvalidArgument,
			"discovery.method_not_allowed", "the discovery document is read with GET"))
		return
	}
	if _, _, ownedErr := transport.PreAdmit(r.Context(), h.config,
		transport.MapMetadata(r.Header), app.DiscoveryPath); ownedErr != nil {
		writeDiscoveryError(w, ownedErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.document)
}

// writeDiscoveryError projects an admission refusal onto the discovery
// route's own plain-JSON error shape.
func writeDiscoveryError(w http.ResponseWriter, ownedErr *envelope.Error) {
	body, err := protojson.Marshal(ownedErr.Detail())
	if err != nil {
		http.Error(w, "{}", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ownedErr.HTTPStatus())
	_, _ = w.Write(body)
}
