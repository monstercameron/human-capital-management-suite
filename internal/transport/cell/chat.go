package cell

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	transportextensions "github.com/monstercameron/human-capital-management-suite/internal/transport/chatextensions"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/chatresource"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportposition "github.com/monstercameron/human-capital-management-suite/internal/transport/position"
	transportproject "github.com/monstercameron/human-capital-management-suite/internal/transport/project"
	transportworkorder "github.com/monstercameron/human-capital-management-suite/internal/transport/workorder"
)

const ChatExtensionsProcedurePrefix = "/hcmnext.chat.v1.ChatExtensionsService/"

func RegisterChatExtensions(srv *grpc.Server, service transportextensions.Service) {
	if srv == nil || service == nil {
		return
	}
	transportextensions.Register(srv, transportextensions.Dependencies{Service: service})
}

func OverlayChatExtensions(next http.Handler, cfg transport.Config, service transportextensions.Service) http.Handler {
	if service == nil {
		return next
	}
	mux := http.NewServeMux()
	mux.Handle(ChatExtensionsProcedurePrefix, chatPreAdmission(cfg, transportextensions.NewHandler(
		transportextensions.Dependencies{Service: service},
		connect.WithInterceptors(chatAdmission{cfg: cfg}),
	)))
	mux.Handle("/", next)
	return mux
}

// RegisterChat adds the chat service to the already-authenticated gRPC
// surface. A nil service deliberately leaves the optional product absent.
func RegisterChat(srv *grpc.Server, service chatcore.ConversationService) {
	if srv == nil || service == nil {
		return
	}
	transportchat.Register(srv, transportchat.Dependencies{Service: service})
}

// NewTunnelGRPCServerWithChat is the tunnel server the workspace page
// actually bridges: the core workspace services, work orders and chat services,
// which the browser client speaks over the same tunnel. Chat is a member of
// [tunnelAllowedServices], so both services are always registered — with the
// composed implementation when chat is enabled, and with the generated
// Unimplemented stubs otherwise, so a call into a disabled product answers
// UNIMPLEMENTED at the handler rather than silently vanishing at the bridge
// and so the policy/constructor agreement test holds in either mode.
func NewTunnelGRPCServerWithChat(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServer(c, instances, workQueue, cursorKey, previousCursorKey, workWrites, thresholds, chatService, extensions, opts...)
}

// NewTunnelGRPCServerWithChatAndDocument composes the workspace document
// service alongside chat on the existing authenticated browser tunnel.
func NewTunnelGRPCServerWithChatAndDocument(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocument(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, documentService, nil, nil, opts...)
}

// NewTunnelGRPCServerWithChatDocumentAndPosition composes the typed position
// read service onto the authenticated browser tunnel.
func NewTunnelGRPCServerWithChatDocumentAndPosition(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps transportposition.Dependencies, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocument(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, documentService, &positionDeps, nil, opts...)
}

// NewTunnelGRPCServerWithChatDocumentPositionAndProject adds ProjectService
// to the admitted browser tunnel. The same service value is also registered
// on the direct listener by the application composition root.
func NewTunnelGRPCServerWithChatDocumentPositionAndProject(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps transportposition.Dependencies, projectService transportproject.Service, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocument(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, documentService, &positionDeps, projectService, opts...)
}

// NewTunnelGRPCServerWithChatDocumentPositionAndProjectActivity adds the
// project comment/activity application port while preserving the older
// project-only constructor for callers that do not compose activity storage.
func NewTunnelGRPCServerWithChatDocumentPositionAndProjectActivity(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps transportposition.Dependencies, projectService transportproject.Service,
	projectActivity transportproject.ActivityService, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocumentAndProjectActivity(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, documentService, &positionDeps, projectService, projectActivity, nil, opts...)
}

// NewTunnelGRPCServerWithChatDocumentPositionProjectActivityAndSearch adds the
// authorized task-search port to the same browser tunnel ProjectService.
func NewTunnelGRPCServerWithChatDocumentPositionProjectActivityAndSearch(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps transportposition.Dependencies, projectService transportproject.Service,
	projectActivity transportproject.ActivityService, projectSearch transportproject.TaskSearchService,
	opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocumentAndProjectActivity(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, documentService, &positionDeps, projectService, projectActivity, projectSearch, opts...)
}

// NewTunnelGRPCServerWithChatDocumentPositionProjectActivityAndSearchAndWorkOrder
// adds WorkOrderService to the authenticated browser tunnel. Its service
// dependencies are composed by the application root and shared with the
// direct gRPC and Connect surfaces.
func NewTunnelGRPCServerWithChatDocumentPositionProjectActivityAndSearchAndWorkOrder(
	c *app.Cell, instances app.WorkflowControlReader, workQueue app.WorkItemQueueReader,
	cursorKey, previousCursorKey []byte, workWrites transporthumanwork.WritePorts,
	thresholds transporthumanwork.Thresholds, chatService chatcore.ConversationService,
	extensions transportextensions.Service, documentService transportdocument.Service,
	positionDeps transportposition.Dependencies, projectService transportproject.Service,
	projectActivity transportproject.ActivityService, projectSearch transportproject.TaskSearchService,
	workOrder *transportworkorder.Dependencies, opts ...grpc.ServerOption,
) (*grpc.Server, error) {
	return newTunnelGRPCServerWithDocumentAndProjectActivityAndWorkOrder(c, instances, workQueue, cursorKey, previousCursorKey,
		workWrites, thresholds, chatService, extensions, documentService, &positionDeps,
		projectService, projectActivity, projectSearch, workOrder, opts...)
}

// registerTunnelChat puts both chat services on a tunnel server: composed
// when supplied, generated Unimplemented stubs otherwise.
func registerTunnelChat(srv *grpc.Server, chatService chatcore.ConversationService, extensions transportextensions.Service) {
	if chatService != nil {
		transportchat.Register(srv, transportchat.Dependencies{Service: chatService})
	} else {
		chatv1.RegisterConversationServiceServer(srv, chatv1.UnimplementedConversationServiceServer{})
	}
	if extensions != nil {
		transportextensions.Register(srv, transportextensions.Dependencies{Service: extensions})
	} else {
		chatv1.RegisterChatExtensionsServiceServer(srv, chatv1.UnimplementedChatExtensionsServiceServer{})
	}
}

// chatHTTPOverlay mounts the chat Connect projection ahead of the existing
// edge. The projection itself performs the same trusted-context check as the
// gRPC adapter; the outer edge remains the owner of discovery, workspace and
// tunnel routes.
func chatHTTPOverlay(next http.Handler, cfg transport.Config, service chatcore.ConversationService) http.Handler {
	if service == nil {
		return next
	}
	mux := http.NewServeMux()
	chatHandler := transportchat.NewHandler(
		transportchat.Dependencies{Service: service},
		connect.WithInterceptors(chatAdmission{cfg: cfg}),
	)
	mux.Handle(transportchat.ProcedurePrefix, chatPreAdmission(cfg, chatHandler))
	mux.Handle("/v1/conversations/", chatresource.NewHandler(cfg, service))
	mux.Handle("/", next)
	return mux
}

// chatPreAdmission runs the first three admission steps - the reserved-metadata
// screen, correlation and authentication - before the Connect handler decodes
// anything.
//
// It has to happen out here for the same reason the canonical edge does it out
// here: screening a body for a caller who has not authenticated yet would hand
// that caller a validity oracle a gRPC caller does not get. The result is
// carried on the context, and [chatAdmission] reuses it rather than
// authenticating twice.
func chatPreAdmission(cfg transport.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _, _, err := transport.WithPreAdmission(r.Context(), cfg, transport.MapMetadata(r.Header), r.URL.Path)
		if err != nil {
			connect.NewErrorWriter().Write(w, r, edge.ToConnectError(err))
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// chatAdmission is the chat overlay's half of the trusted request boundary.
//
// The overlay used to call transport.Admit from an outer http.Handler, where
// the request body is still an undecoded stream. Admission therefore ran with
// no message, and skipped the two steps that need one: per-method structural
// validation, and the server-side overwrite of the request's trusted fields.
// An interceptor sees the decoded message, so both steps now happen for chat
// exactly as they do for the canonical edge and the gRPC interceptor.
type chatAdmission struct {
	cfg transport.Config
}

// WrapUnary implements [connect.Interceptor].
func (i chatAdmission) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		start := time.Now()
		ctx, cancel := transport.CapDeadline(ctx, i.cfg)
		defer cancel()

		message, _ := req.Any().(proto.Message)
		procedure := req.Spec().Procedure

		admitted, inv, admitErr := transport.Admit(ctx, i.cfg, transport.AdmissionRequest{
			Metadata: transport.MapMetadata(req.Header()),
			Method:   procedure,
			Kind:     transport.KindHTTPEdge,
			Message:  message,
		})
		if admitErr != nil {
			return nil, i.finish(procedure, nil, admitErr.CorrelationID(), start, admitErr)
		}
		resp, err := next(admitted, req)
		if err != nil {
			return nil, i.finish(procedure, inv, inv.RequestID(), start, transport.OwnedError(err, inv))
		}
		i.finish(procedure, inv, inv.RequestID(), start, nil)
		return resp, nil
	}
}

// WrapStreamingClient implements [connect.Interceptor].
func (i chatAdmission) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements [connect.Interceptor]. A server-streaming
// chat handler decodes its request message itself, after every interceptor has
// run, so there is no message to validate here; the screen, correlation and
// authentication already happened in [chatPreAdmission], and the handler
// resolves its principal from the context the same way the unary methods do.
func (i chatAdmission) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()
		admitted, inv, admitErr := transport.Admit(ctx, i.cfg, transport.AdmissionRequest{
			Metadata: transport.MapMetadata(conn.RequestHeader()),
			Method:   conn.Spec().Procedure,
			Kind:     transport.KindHTTPEdge,
		})
		if admitErr != nil {
			return i.finish(conn.Spec().Procedure, nil, admitErr.CorrelationID(), start, admitErr)
		}
		err := next(admitted, conn)
		if err != nil {
			return i.finish(conn.Spec().Procedure, inv, inv.RequestID(), start, transport.OwnedError(err, inv))
		}
		i.finish(conn.Spec().Procedure, inv, inv.RequestID(), start, nil)
		return nil
	}
}

// finish records the request and returns the wire error.
func (i chatAdmission) finish(procedure string, inv *transport.Invocation, requestID string, start time.Time, err *envelope.Error) error {
	if i.cfg.Logger != nil {
		i.cfg.Logger.LogRequest(transport.NewLogRecord(procedure, transport.KindHTTPEdge, inv, requestID, time.Since(start), err))
	}
	if err == nil {
		return nil
	}
	return edge.ToConnectError(err)
}
