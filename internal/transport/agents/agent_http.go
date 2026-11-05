package agents

import (
	"context"
	"net/http"
	"strconv"

	"connectrpc.com/connect"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// ProcedurePrefix is the canonical Connect/gRPC HTTP path prefix for the
// agent service. A composition mounts this prefix after applying the same
// admission the gRPC surface applies, exactly as
// internal/transport/document.ProcedurePrefix is mounted for documents.
const ProcedurePrefix = "/hcmnext.agent.v1.AgentService/"

// MaxRequestBytes bounds a decoded Connect request body, matching the
// canonical edge's own bound.
const MaxRequestBytes = 4 << 20

// NewHandler builds the Connect HTTP projection of the agent RPC methods.
// Every handler calls the same *Server method the gRPC adapter calls, so the
// HTTP and native RPC paths share one implementation and can never diverge on
// capability and authorization decisions, expected revisions, prompt and mode
// validation, or typed errors.
func NewHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := NewServer(deps)
	// A caller-supplied option still wins: the bound is prepended, not
	// appended, so a composition that needs a different ceiling can say so.
	opts = append([]connect.HandlerOption{connect.WithReadMaxBytes(MaxRequestBytes)}, opts...)
	mux := http.NewServeMux()
	registerAgentUnary(mux, opts, agentv1.AgentService_SetAgentsEnabled_FullMethodName, s.SetAgentsEnabled)
	registerAgentUnary(mux, opts, agentv1.AgentService_StartAgentTask_FullMethodName, s.StartAgentTask)
	registerAgentUnary(mux, opts, agentv1.AgentService_GetAgentTask_FullMethodName, s.GetAgentTask)
	registerAgentUnary(mux, opts, agentv1.AgentService_ListAgentTasks_FullMethodName, s.ListAgentTasks)
	registerAgentUnary(mux, opts, agentv1.AgentService_ControlAgentTask_FullMethodName, s.ControlAgentTask)
	return mux
}

func registerAgentUnary[Req, Res any](mux *http.ServeMux, opts []connect.HandlerOption, proc string, fn func(context.Context, *Req) (*Res, error)) {
	mux.Handle(proc, connect.NewUnaryHandler(proc, func(c context.Context, r *connect.Request[Req]) (*connect.Response[Res], error) {
		v, err := fn(c, r.Msg)
		if err != nil {
			return nil, httpError(err)
		}
		return connect.NewResponse(v), nil
	}, opts...))
}

// httpOwnedCause carries an owned condition as the cause of a Connect wire
// error while keeping the wire message identical to the canonical projection.
type httpOwnedCause struct {
	owned *envelope.Error
}

func (e httpOwnedCause) Error() string { return e.owned.Message() }

func (e httpOwnedCause) Unwrap() error { return e.owned }

// httpError projects an owned condition onto the Connect wire error the
// gRPC adapter already carries via [envelope.Error.GRPCStatus]: the same code
// mapping, the same message, the same typed detail and the same Retry-After.
// The owned condition stays reachable through [connect.Error.Unwrap] so a
// composed admission interceptor (which reclassifies handler errors through
// transport.OwnedError) recovers the exact typed refusal instead of coercing
// it to a generic fault. Without the projection, a bare envelope.Error
// reaching the Connect runtime falls back to CodeUnknown, so an HTTP caller
// would see a different, less specific error than a native RPC caller for the
// identical refusal.
func httpError(err error) error {
	if err == nil {
		return nil
	}
	owned, ok := envelope.As(err)
	if !ok {
		return err
	}
	connectErr := connect.NewError(connect.Code(owned.GRPCCode()), httpOwnedCause{owned: owned})
	if retryAfter := owned.RetryAfter(); retryAfter > 0 {
		connectErr.Meta().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	if detail, err := connect.NewErrorDetail(owned.Detail()); err == nil {
		connectErr.AddDetail(detail)
	}
	return connectErr
}
