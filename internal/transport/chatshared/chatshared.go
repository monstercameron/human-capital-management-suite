// Package chatshared holds the serving mechanics the chat transports share:
// Connect unary registration and the trusted-request refusal for a
// caller-supplied server-derived field. One copy keeps the conversation and
// extensions projections answering byte-identically instead of drifting.
package chatshared

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// Reason and rule identifiers for refusing a request that selected trusted
// context. They mirror internal/transport's own so a caller that forges a
// principal gets the same answer whichever surface it forges it on.
const (
	ReasonCallerSelectedAuthority = "trusted_context.caller_selected_authority"
	RuleTrustedRequestBoundary    = "trusted_request_boundary.server_derived_field"
)

// RegisterUnary mounts one unary Connect procedure that calls the gRPC
// implementation directly, preserving outcomes.
func RegisterUnary[Req, Res any](mux *http.ServeMux, opts []connect.HandlerOption, proc string, fn func(context.Context, *Req) (*Res, error)) {
	mux.Handle(proc, connect.NewUnaryHandler(proc, func(c context.Context, r *connect.Request[Req]) (*connect.Response[Res], error) {
		v, err := fn(c, r.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(v), nil
	}, opts...))
}

// ServerDerived refuses a caller-supplied value for a field the server owns.
// Refusing rather than overwriting is the point: a client that believes it is
// posting as somebody else has to be told it is wrong, not quietly corrected.
func ServerDerived(field, what string) error {
	return envelope.New(envelope.CodeInvalidArgument, ReasonCallerSelectedAuthority,
		"the request may not select trusted context").
		WithViolation(field, what, RuleTrustedRequestBoundary)
}
