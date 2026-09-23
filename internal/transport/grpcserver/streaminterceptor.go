package grpcserver

import (
	"context"

	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// StreamInterceptor is the trusted request boundary for native gRPC
// streaming methods. It is [UnaryInterceptor]'s boundary, not a second one:
// both run the same [admit], the same [call.refuse] and the same
// [call.conclude], so a streaming method is admitted, deadline-capped,
// logged and error-projected exactly as a unary method is.
//
// A stream is admitted once, when it opens, and the admission decision holds
// for the life of that stream. That is the whole reason the wrapper below
// exists: the handler must see the enriched context rather than the raw
// transport one, or the admitted principal and invocation would be
// unreachable from inside it and the method would run untrusted.
//
// Two differences from the unary path are inherent to the shape of a stream
// rather than choices made here, and both are named on [admit]: the request
// message is not available to admission (grpc-go decodes it inside the
// generated handler, after every interceptor has run), and the one record
// this emits covers the whole stream, so its Duration is the stream's
// lifetime rather than one round trip's.
//
// It is exported for the same reason [UnaryInterceptor] is: a process that
// composes its own *grpc.Server installs this rather than approximating it.
func StreamInterceptor(cfg transport.Config) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		c, admitErr := admit(ss.Context(), cfg, info.FullMethod, nil, true)
		defer c.cancel()
		if admitErr != nil {
			return c.refuse(cfg, info.FullMethod, admitErr)
		}
		return c.conclude(cfg, info.FullMethod, handler(srv, &admittedStream{ServerStream: ss, ctx: c.ctx}))
	}
}

// admittedStream is the stream a streaming handler actually receives: the
// transport's own stream in every respect except its context, which is the
// admitted one.
//
// Everything else is the embedded stream's, deliberately. SendMsg, RecvMsg,
// the header and trailer methods and any interface grpc-go adds later all
// forward unchanged, so this wrapper cannot become a place where streaming
// behaviour quietly diverges from what the transport does.
//
// Cancellation needs no forwarding: the admitted context is a child of the
// transport stream's own, so a client that goes away, a deadline that
// expires and the server's own deadline cap all end it through the same
// Done channel the handler is already selecting on.
type admittedStream struct {
	grpc.ServerStream

	ctx context.Context
}

// Context returns the admitted context: server-capped deadline,
// authenticated trust.Principal, immutable transport.Invocation.
func (s *admittedStream) Context() context.Context { return s.ctx }
