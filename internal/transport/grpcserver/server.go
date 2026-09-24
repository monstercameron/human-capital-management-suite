// Package grpcserver exposes the canonical Human Capital Management Suite gRPC surface.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todos: ENDPOINT-002,
// ENDPOINT-003, CAP-003.
//
// Protobuf/gRPC is the canonical contract, so this package is the reference
// implementation of the trusted request boundary: the HTTP edge in
// internal/transport/edge is a projection of what happens here, not a second
// implementation of it. Both call the same transport.Admit, apply the same
// transport.Validate, and project failures through the same
// internal/transport/envelope error model.
//
// The service types in this package are adapters and nothing more. They hold a
// transport.IntentHandler or transport.RegistryHandler and forward to it. No
// method in this package decides anything about an HCM business operation.
package grpcserver

import (
	"errors"

	"google.golang.org/grpc"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportdataops "github.com/monstercameron/human-capital-management-suite/internal/transport/dataops"
	transportintegration "github.com/monstercameron/human-capital-management-suite/internal/transport/integration"
	transportnotification "github.com/monstercameron/human-capital-management-suite/internal/transport/notification"
)

// defaultMaxRecvMsgBytes bounds an inbound message. Bounded decoding is part
// of the strict-decoding contract, not a tuning knob.
const defaultMaxRecvMsgBytes = 4 << 20

// Options configures [NewServer].
type Options struct {
	// Config is the shared admission configuration. Pass the same value to the
	// HTTP edge so that both transports construct identical trusted context.
	Config transport.Config
	// Intent is the BusinessIntent lifecycle handler port. Optional: when nil
	// the IntentService is not registered.
	Intent transport.IntentHandler
	// Registry is the discovery handler port. Optional: when nil the
	// RegistryService is not registered.
	Registry transport.RegistryHandler
	// DataOps is the DataOps handler port. Optional: when nil the
	// DataOpsService is not registered.
	DataOps transport.DataOpsHandler
	// DataOpsStage backs the client-streaming StageCSV method. It is only valid
	// together with DataOps so the generated service has a complete handler.
	DataOpsStage transportdataops.StageCSVHandler
	// Integration is the Integration handler port. Optional: when nil the
	// IntegrationService is not registered.
	Integration transport.IntegrationHandler
	// Notifications is the notification feed handler port. Optional: when
	// nil the NotificationService is not registered.
	Notifications transport.NotificationHandler
	// MaxRecvMsgBytes bounds an inbound message. Zero means 4 MiB.
	MaxRecvMsgBytes int
	// ServerOptions are appended after the options this package sets, so a
	// caller can add credentials or keepalive policy without being able to
	// drop the interceptor chain.
	ServerOptions []grpc.ServerOption
}

// ErrNoHandlers is returned by [NewServer] when neither handler port is set: a
// server that publishes no method is a configuration mistake, not a valid
// degenerate case.
var ErrNoHandlers = errors.New("grpcserver: at least one handler port must be configured")

// ErrNoVerifier is returned by [NewServer] when no verifier is configured. A
// listener that cannot authenticate must not start; failing here rather than
// per request keeps an unauthenticated surface from ever existing.
var ErrNoVerifier = errors.New("grpcserver: a trust.Verifier is required")

// NewServer builds the canonical gRPC server with the full interceptor chain
// installed and the configured services registered.
//
// The interceptor chain is not optional and not reorderable. It caps the
// deadline, screens for caller-selected trusted context, authenticates,
// validates strictly, constructs the immutable invocation context, emits one
// structured log record and projects every failure through the owned error
// model. A caller who wants a server without one of those steps wants a
// different product.
//
// Both cardinalities are chained, and they are the same boundary:
// [UnaryInterceptor] for unary methods and [StreamInterceptor] for streaming
// ones share one implementation, so a service published on this server may
// declare a server-streaming RPC without opting out of admission. That is
// what makes hcmnext.journey.v1.JourneyService.WatchJourney a real stream
// rather than a long poll.
func NewServer(opts Options) (*grpc.Server, error) {
	if opts.Config.Verifier == nil {
		return nil, ErrNoVerifier
	}
	if opts.Intent == nil && opts.Registry == nil && opts.DataOps == nil && opts.Integration == nil && opts.Notifications == nil {
		return nil, ErrNoHandlers
	}
	if opts.DataOps == nil && opts.DataOpsStage != nil {
		return nil, errors.New("grpcserver: StageCSV requires the DataOps handler")
	}

	maxRecv := opts.MaxRecvMsgBytes
	if maxRecv <= 0 {
		maxRecv = defaultMaxRecvMsgBytes
	}

	serverOptions := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxRecv),
		grpc.ChainUnaryInterceptor(UnaryInterceptor(opts.Config)),
		grpc.ChainStreamInterceptor(StreamInterceptor(opts.Config)),
	}
	serverOptions = append(serverOptions, opts.ServerOptions...)

	srv := grpc.NewServer(serverOptions...)
	if opts.Intent != nil {
		intentsv1.RegisterIntentServiceServer(srv, &intentService{handler: opts.Intent})
	}
	if opts.Registry != nil {
		registryv1.RegisterRegistryServiceServer(srv, &registryService{handler: opts.Registry})
	}
	if opts.DataOps != nil {
		dataopsv1.RegisterDataOpsServiceServer(srv, &transportdataops.Service{Handler: opts.DataOps, Stage: opts.DataOpsStage})
	}
	if opts.Integration != nil {
		integrationv1.RegisterIntegrationServiceServer(srv, &transportintegration.Service{Handler: opts.Integration})
	}
	if opts.Notifications != nil {
		notificationv1.RegisterNotificationServiceServer(srv, &transportnotification.Service{Handler: opts.Notifications})
	}
	return srv, nil
}
