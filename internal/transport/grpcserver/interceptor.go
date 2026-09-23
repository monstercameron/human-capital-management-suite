package grpcserver

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// call is one admitted gRPC call, whatever its cardinality: the enriched
// context admission produced, the immutable invocation it constructed, the
// deadline cancel to release, and the instant the boundary started timing.
//
// It exists so the unary and the streaming interceptor are two spellings of
// one boundary rather than two implementations of it. Everything either of
// them does to a call - cap the deadline, screen, authenticate, validate,
// derive trusted context, project the outcome, emit the one record - happens
// in [admit], [call.refuse] and [call.conclude] below, and the interceptors
// themselves only adapt grpc-go's two handler shapes onto them.
type call struct {
	// ctx is the enriched context: server-capped deadline, authenticated
	// trust.Principal, immutable transport.Invocation. It is the caller's
	// context, unmodified, when admission failed.
	ctx context.Context
	// cancel releases the capped deadline. It is never nil, including on the
	// admission-failure path, and every caller defers it.
	cancel context.CancelFunc
	// inv is the invocation admission constructed. Nil when admission failed.
	inv *transport.Invocation
	// start is when the boundary began timing this call, which is what the
	// record's Duration is measured from.
	start time.Time
}

// admit runs the whole trusted request boundary for one call and returns the
// enriched context to hand the handler.
//
// message is the decoded request message when the transport has one at this
// point, and nil when it does not. A unary call always has one, so its
// strict structural validation and its server-side trusted-field overwrite
// happen here, before the handler. A server-streaming call does not: grpc-go
// decodes the request message inside the generated method handler, after
// every interceptor has run, so [StreamInterceptor] passes nil and
// transport.Admit skips those two steps for it. Everything that decides
// whether the call is allowed to happen at all - the reserved-metadata
// screen, correlation, authentication - is message-independent and runs
// identically either way.
func admit(ctx context.Context, cfg transport.Config, method string, message proto.Message, streaming bool) (call, *envelope.Error) {
	c := call{start: time.Now()}

	// A server-capped deadline applies to every call, including one that
	// arrived without any deadline at all. Cancellation propagates through
	// the same context, so a caller that goes away stops the work.
	//
	// A server-streaming call is capped by the stream ceiling instead of the
	// unary request budget. It used to take the unary cap, which ended every
	// live watch at exactly thirty seconds with DEADLINE_EXCEEDED.
	if streaming {
		c.ctx, c.cancel = transport.CapStreamDeadline(ctx, cfg)
	} else {
		c.ctx, c.cancel = transport.CapDeadline(ctx, cfg)
	}

	md, _ := metadata.FromIncomingContext(c.ctx)
	admitted, inv, admitErr := transport.Admit(c.ctx, cfg, transport.AdmissionRequest{
		Metadata: transport.MapMetadata(md),
		Method:   method,
		Kind:     transport.KindGRPC,
		Message:  message,
	})
	if admitErr != nil {
		return c, admitErr
	}
	c.ctx, c.inv = admitted, inv
	return c, nil
}

// refuse ends a call that was never admitted. There is no invocation to
// record against - admission is what would have constructed one - so the
// record carries the correlation identifier the refusal itself resolved.
func (c call) refuse(cfg transport.Config, method string, admitErr *envelope.Error) error {
	return finish(cfg, method, nil, admitErr.CorrelationID(), c.start, admitErr)
}

// conclude ends an admitted call: it projects the handler's outcome through
// the owned error model, emits the one structured record, and returns what
// grpc-go should send to the caller.
//
// A handler that returned nothing is still checked against its own context,
// because a handler that observed cancellation and returned cleanly has not
// produced a successful call - it has produced an abandoned one, and the
// record must say so.
func (c call) conclude(cfg transport.Config, method string, handlerErr error) error {
	outcome := handlerErr
	if outcome == nil {
		outcome = c.ctx.Err()
	}
	if outcome != nil {
		return finish(cfg, method, c.inv, c.inv.RequestID(), c.start, transport.OwnedError(outcome, c.inv))
	}
	return finish(cfg, method, c.inv, c.inv.RequestID(), c.start, nil)
}

// UnaryInterceptor is the whole trusted request boundary for native gRPC
// unary methods.
//
// It is exported so a process that composes its own *grpc.Server still gets
// exactly this chain rather than an approximation of it. A server that
// serves streaming methods installs [StreamInterceptor] beside it;
// [NewServer] installs both.
func UnaryInterceptor(cfg transport.Config) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		message, _ := req.(proto.Message)

		c, admitErr := admit(ctx, cfg, info.FullMethod, message, false)
		defer c.cancel()
		if admitErr != nil {
			return nil, c.refuse(cfg, info.FullMethod, admitErr)
		}

		resp, err := handler(c.ctx, req)
		if outcome := c.conclude(cfg, info.FullMethod, err); outcome != nil {
			return nil, outcome
		}
		return resp, nil
	}
}

// finish emits the structured record for a completed call and returns the
// owned error to hand back to grpc-go. *envelope.Error implements grpc-go's
// status interface, so returning it directly produces the projected status
// code plus the canonical hcmnext.common.v1.ErrorDetail.
func finish(cfg transport.Config, method string, inv *transport.Invocation, requestID string, start time.Time, err *envelope.Error) error {
	if cfg.Logger != nil {
		cfg.Logger.LogRequest(transport.NewLogRecord(method, transport.KindGRPC, inv, requestID, time.Since(start), err))
	}
	if err == nil {
		return nil
	}
	return err
}
