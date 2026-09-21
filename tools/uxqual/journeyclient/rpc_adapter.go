package journeyclient

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// RPCAdapterConfig is the browser edge's bounded, per-call policy. It is a
// transport policy only: it contains no tenant, subject, role, purpose or
// business rule. The server remains the sole authority for those values.
type RPCAdapterConfig struct {
	Bearer string

	// MaxRequestBytes and MaxResponseBytes bound serialized protobuf messages.
	// Zero selects the conservative production defaults.
	MaxRequestBytes  int
	MaxResponseBytes int

	// Metadata limits apply before any metadata is forwarded. Only the small
	// observability allow-list is forwarded; every other caller-supplied key
	// (including authority-shaped and authorization keys) is rejected.
	MaxMetadataBytes      int
	MaxMetadataEntries    int
	MaxMetadataValueBytes int

	// A caller deadline is preserved when it is within MaxDeadline. Unary calls
	// without a deadline receive DefaultDeadline; a longer caller deadline is
	// capped. Non-positive values select the defaults.
	DefaultDeadline time.Duration
	MaxDeadline     time.Duration
	// MaxStreamDeadline caps an explicitly supplied stream deadline. A
	// stream with no caller deadline remains page-lifetime scoped.
	MaxStreamDeadline time.Duration
}

const (
	defaultRPCRequestBytes       = 1 << 20
	defaultRPCResponseBytes      = 4 << 20
	defaultRPCMetadataBytes      = 8 << 10
	defaultRPCMetadataEntries    = 32
	defaultRPCMetadataValueBytes = 2 << 10
	defaultRPCDeadline           = 30 * time.Second
	defaultRPCMaxDeadline        = 60 * time.Second
	defaultRPCMaxStreamDeadline  = 15 * time.Minute
	maxRPCBearerBytes            = 4096
)

// RPCAdapter is a small, native-testable browser RPC boundary. It delegates
// every operation to a caller-provided grpc.ClientConnInterface, so it never
// becomes a second service implementation and can be used over either the
// GoGRPCBridge WebSocket dialer or an ordinary native connection.
//
// The adapter intentionally does not expose a generic retry path. Generated
// unary and server-streaming clients retain their normal gRPC semantics, and
// the WASM dial composition disables connection retries for all calls.
type RPCAdapter struct {
	conn grpc.ClientConnInterface
	cfg  RPCAdapterConfig
	// methods is derived from the generated JourneyService descriptor once
	// per adapter. It is instance-owned rather than a mutable package registry.
	methods     map[string]rpcMethod
	contractErr error
	configErr   error
}

type rpcMethodKind uint8

const (
	rpcUnary rpcMethodKind = iota + 1
	rpcServerStream
)

type rpcMethod struct {
	kind          rpcMethodKind
	name          string
	shortName     string
	inputType     reflect.Type
	outputType    reflect.Type
	inputName     protoreflect.FullName
	outputName    protoreflect.FullName
	clientStreams bool
	serverStreams bool
}

// NewRPCAdapter returns a bounded adapter over conn. A nil connection is
// retained and produces a typed UNAVAILABLE error on use rather than a panic.
func NewRPCAdapter(conn grpc.ClientConnInterface, cfg RPCAdapterConfig) *RPCAdapter {
	cfg = normalizeRPCAdapterConfig(cfg)
	methods, err := canonicalRPCMethods()
	adapter := &RPCAdapter{conn: conn, cfg: cfg, methods: methods}
	if err != nil {
		adapter.contractErr = status.Error(codes.Internal, "browser RPC generated contract is inconsistent")
	}
	if cfg.Bearer != "" {
		adapter.configErr = validateRPCBearer(cfg.Bearer)
	}
	return adapter
}

// newWorkflowRPCAdapter applies the identical bounded browser-call policy to
// WorkflowService without broadening the JourneyService adapter's admitted
// method set. A generated client can therefore share the tunnel while each
// adapter still rejects calls outside its own descriptor.
func newWorkflowRPCAdapter(conn grpc.ClientConnInterface, cfg RPCAdapterConfig) *RPCAdapter {
	cfg = normalizeRPCAdapterConfig(cfg)
	methods, err := canonicalWorkflowRPCMethods()
	adapter := &RPCAdapter{conn: conn, cfg: cfg, methods: methods}
	if err != nil {
		adapter.contractErr = status.Error(codes.Internal, "browser RPC generated contract is inconsistent")
	}
	if cfg.Bearer != "" {
		adapter.configErr = validateRPCBearer(cfg.Bearer)
	}
	return adapter
}

// canonicalRPCMethods cross-checks the protoc and protoc-gen-go-grpc views of
// the service. A method is admitted only when both generated descriptors name
// the same complete set and agree about its stream shape. Keeping the result
// on the adapter makes later calls deterministic even if unrelated test code
// incorrectly mutates the exported generated grpc.ServiceDesc.
func canonicalRPCMethods() (map[string]rpcMethod, error) {
	service := journeyv1.File_hcmnext_journey_v1_journey_service_proto.Services().ByName("JourneyService")
	if service == nil {
		return nil, fmt.Errorf("protobuf JourneyService descriptor is missing")
	}
	return buildRPCMethods(service, journeyv1.JourneyService_ServiceDesc)
}

func canonicalWorkflowRPCMethods() (map[string]rpcMethod, error) {
	service := workflowv1.File_hcmnext_workflow_v1_workflow_service_proto.Services().ByName("WorkflowService")
	if service == nil {
		return nil, fmt.Errorf("protobuf WorkflowService descriptor is missing")
	}
	return buildRPCMethods(service, workflowv1.WorkflowService_ServiceDesc)
}

func buildRPCMethods(service protoreflect.ServiceDescriptor, grpcService grpc.ServiceDesc) (map[string]rpcMethod, error) {
	if service == nil || string(service.FullName()) != grpcService.ServiceName {
		return nil, fmt.Errorf("service names differ")
	}
	if metadataPath, ok := grpcService.Metadata.(string); !ok || metadataPath != service.ParentFile().Path() {
		return nil, fmt.Errorf("service metadata differs")
	}
	if grpcService.HandlerType == nil {
		return nil, fmt.Errorf("service handler type is missing")
	}

	declared := make(map[string]protoreflect.MethodDescriptor, service.Methods().Len())
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		declared[string(method.Name())] = method
	}
	methods := make(map[string]rpcMethod, len(declared))
	add := func(name string, kind rpcMethodKind, clientStreams, serverStreams bool) error {
		method, ok := declared[name]
		if !ok {
			return fmt.Errorf("grpc method %q is absent from protobuf", name)
		}
		if _, duplicate := methods[name]; duplicate {
			return fmt.Errorf("grpc method %q is duplicated", name)
		}
		if method.IsStreamingClient() != clientStreams || method.IsStreamingServer() != serverStreams {
			return fmt.Errorf("grpc method %q has the wrong stream shape", name)
		}
		inputType, err := registeredMessageType(method.Input().FullName())
		if err != nil {
			return err
		}
		outputType, err := registeredMessageType(method.Output().FullName())
		if err != nil {
			return err
		}
		fullName := "/" + grpcService.ServiceName + "/" + name
		methods[name] = rpcMethod{
			kind:          kind,
			name:          fullName,
			shortName:     name,
			inputType:     inputType,
			outputType:    outputType,
			inputName:     method.Input().FullName(),
			outputName:    method.Output().FullName(),
			clientStreams: clientStreams,
			serverStreams: serverStreams,
		}
		return nil
	}
	for _, method := range grpcService.Methods {
		if method.Handler == nil {
			return nil, fmt.Errorf("grpc unary method %q has no handler", method.MethodName)
		}
		if err := add(method.MethodName, rpcUnary, false, false); err != nil {
			return nil, err
		}
	}
	for _, stream := range grpcService.Streams {
		if stream.Handler == nil {
			return nil, fmt.Errorf("grpc stream %q has no handler", stream.StreamName)
		}
		if err := add(stream.StreamName, rpcServerStream, stream.ClientStreams, stream.ServerStreams); err != nil {
			return nil, err
		}
	}
	if len(methods) != len(declared) {
		return nil, fmt.Errorf("grpc and protobuf method sets differ")
	}
	byFullName := make(map[string]rpcMethod, len(methods))
	for _, method := range methods {
		byFullName[method.name] = method
	}
	return byFullName, nil
}

func registeredMessageType(name protoreflect.FullName) (reflect.Type, error) {
	messageType, err := protoregistry.GlobalTypes.FindMessageByName(name)
	if err != nil {
		return nil, fmt.Errorf("protobuf message %q is not registered: %w", name, err)
	}
	return reflect.TypeOf(messageType.New().Interface()), nil
}

func normalizeRPCAdapterConfig(cfg RPCAdapterConfig) RPCAdapterConfig {
	if cfg.MaxRequestBytes <= 0 {
		cfg.MaxRequestBytes = defaultRPCRequestBytes
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultRPCResponseBytes
	}
	if cfg.MaxMetadataBytes <= 0 {
		cfg.MaxMetadataBytes = defaultRPCMetadataBytes
	}
	if cfg.MaxMetadataEntries <= 0 {
		cfg.MaxMetadataEntries = defaultRPCMetadataEntries
	}
	if cfg.MaxMetadataValueBytes <= 0 {
		cfg.MaxMetadataValueBytes = defaultRPCMetadataValueBytes
	}
	if cfg.DefaultDeadline <= 0 {
		cfg.DefaultDeadline = defaultRPCDeadline
	}
	if cfg.MaxDeadline <= 0 {
		cfg.MaxDeadline = defaultRPCMaxDeadline
	}
	if cfg.MaxStreamDeadline <= 0 {
		cfg.MaxStreamDeadline = defaultRPCMaxStreamDeadline
	}
	if cfg.DefaultDeadline > cfg.MaxDeadline {
		cfg.DefaultDeadline = cfg.MaxDeadline
	}
	return cfg
}

func validateRPCBearer(bearer string) error {
	if len(bearer) > maxRPCBearerBytes {
		return status.Error(codes.ResourceExhausted, "browser RPC credential exceeds safety limit")
	}
	padding := false
	for i := 0; i < len(bearer); i++ {
		char := bearer[i]
		if char == '=' {
			padding = true
			continue
		}
		if padding || !isBearerTokenByte(char) {
			return status.Error(codes.InvalidArgument, "browser RPC credential is malformed")
		}
	}
	return nil
}

func isBearerTokenByte(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~+/", rune(char))
}

// Invoke implements grpc.ClientConnInterface's unary path. Errors from the
// canonical connection are returned unchanged, preserving status details.
func (a *RPCAdapter) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	if a == nil || isNilInterface(a.conn) {
		return status.Error(codes.Unavailable, "browser RPC connection is not configured")
	}
	if a.contractErr != nil {
		return a.contractErr
	}
	if a.configErr != nil {
		return a.configErr
	}
	methodSpec, ok := a.methods[method]
	if !ok || methodSpec.kind != rpcUnary {
		return status.Error(codes.InvalidArgument, "browser RPC method is not in the canonical JourneyService contract")
	}
	ctx, cancel, err := a.callContext(ctx, false)
	if err != nil {
		return err
	}
	defer cancel()
	if err := checkRPCMessage(args, methodSpec.inputType, methodSpec.inputName, a.cfg.MaxRequestBytes, "request"); err != nil {
		return err
	}
	if err := checkRPCMessage(reply, methodSpec.outputType, methodSpec.outputName, a.cfg.MaxResponseBytes, "response"); err != nil {
		return err
	}
	callOpts, err := boundedCallOptions(opts, a.cfg)
	if err != nil {
		return err
	}
	if err := a.conn.Invoke(ctx, method, args, reply, callOpts...); err != nil {
		return err
	}
	return checkRPCMessage(reply, methodSpec.outputType, methodSpec.outputName, a.cfg.MaxResponseBytes, "response")
}

// NewStream implements grpc.ClientConnInterface's streaming path. Request
// and response checks are kept in a stream wrapper so unary limits cannot be
// accidentally treated as a substitute for streaming enforcement.
func (a *RPCAdapter) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	if a == nil || isNilInterface(a.conn) {
		return nil, status.Error(codes.Unavailable, "browser RPC connection is not configured")
	}
	if a.contractErr != nil {
		return nil, a.contractErr
	}
	if a.configErr != nil {
		return nil, a.configErr
	}
	methodSpec, ok := a.methods[method]
	if !ok || methodSpec.kind != rpcServerStream {
		return nil, status.Error(codes.InvalidArgument, "browser RPC stream is not in the canonical JourneyService contract")
	}
	if desc == nil || desc.StreamName != methodSpec.shortName || desc.ClientStreams != methodSpec.clientStreams || desc.ServerStreams != methodSpec.serverStreams {
		return nil, status.Error(codes.InvalidArgument, "browser RPC stream descriptor does not match the canonical method")
	}
	ctx, cancel, err := a.callContext(ctx, true)
	if err != nil {
		return nil, err
	}
	callOpts, err := boundedCallOptions(opts, a.cfg)
	if err != nil {
		cancel()
		return nil, err
	}
	stream, err := a.conn.NewStream(ctx, desc, method, callOpts...)
	if err != nil {
		cancel()
		return nil, err
	}
	if isNilInterface(stream) {
		cancel()
		return nil, status.Error(codes.Unavailable, "browser RPC connection returned no stream")
	}
	return &boundedClientStream{ClientStream: stream, adapter: a, method: methodSpec, ctx: ctx, cancel: cancel}, nil
}

type boundedClientStream struct {
	grpc.ClientStream
	adapter *RPCAdapter
	method  rpcMethod
	ctx     context.Context
	cancel  context.CancelFunc
}

func (s *boundedClientStream) SendMsg(m any) error {
	if err := checkRPCMessage(m, s.method.inputType, s.method.inputName, s.adapter.cfg.MaxRequestBytes, "request"); err != nil {
		s.cancel()
		return err
	}
	if err := s.ClientStream.SendMsg(m); err != nil {
		s.cancel()
		return err
	}
	return nil
}

func (s *boundedClientStream) RecvMsg(m any) error {
	if err := checkRPCMessage(m, s.method.outputType, s.method.outputName, s.adapter.cfg.MaxResponseBytes, "response"); err != nil {
		s.cancel()
		return err
	}
	if err := s.ClientStream.RecvMsg(m); err != nil {
		s.cancel()
		return err
	}
	if err := checkRPCMessage(m, s.method.outputType, s.method.outputName, s.adapter.cfg.MaxResponseBytes, "response"); err != nil {
		s.cancel()
		return err
	}
	return nil
}

func (s *boundedClientStream) Header() (metadata.MD, error) {
	header, err := s.ClientStream.Header()
	if err != nil {
		s.cancel()
	}
	return header, err
}

func (s *boundedClientStream) Trailer() metadata.MD {
	return s.ClientStream.Trailer()
}

func (s *boundedClientStream) CloseSend() error {
	if err := s.ClientStream.CloseSend(); err != nil {
		s.cancel()
		return err
	}
	return nil
}

func (s *boundedClientStream) Context() context.Context {
	return s.ctx
}

// callContext rebuilds outgoing metadata from a closed allow-list and adds
// only the configured bearer. This prevents a copied browser context from
// selecting tenant, subject, roles, purpose or any other trusted value.
func (a *RPCAdapter) callContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, func() {}, status.Error(codes.InvalidArgument, "browser RPC context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, func() {}, err
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	clean, err := boundedMetadata(md, a.cfg)
	if err != nil {
		return nil, func() {}, err
	}
	if a.cfg.Bearer == "" {
		return nil, func() {}, status.Error(codes.Unauthenticated, "browser RPC credential is not configured")
	}
	clean.Set(AuthorizationHeader, BearerScheme+a.cfg.Bearer)
	ctx = metadata.NewOutgoingContext(ctx, clean)
	if stream {
		if callerDeadline, ok := ctx.Deadline(); ok && time.Until(callerDeadline) > a.cfg.MaxStreamDeadline {
			bounded, cancel := context.WithTimeout(ctx, a.cfg.MaxStreamDeadline)
			return bounded, cancel, nil
		}
		bounded, cancel := context.WithCancel(ctx)
		return bounded, cancel, nil
	}
	deadline := a.cfg.DefaultDeadline
	if callerDeadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(callerDeadline); remaining <= a.cfg.MaxDeadline {
			return ctx, func() {}, nil
		}
		deadline = a.cfg.MaxDeadline
	}
	bounded, cancel := context.WithTimeout(ctx, deadline)
	return bounded, cancel, nil
}

func boundedMetadata(md metadata.MD, cfg RPCAdapterConfig) (metadata.MD, error) {
	clean := metadata.MD{}
	entries, bytes := 0, 0
	keys := make([]string, 0, len(md))
	for key := range md {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, rawKey := range keys {
		values := md[rawKey]
		key := strings.ToLower(rawKey)
		if rawKey != key {
			return nil, status.Error(codes.InvalidArgument, "browser RPC metadata key is malformed")
		}
		if key != "x-request-id" && key != "x-correlation-id" && key != "traceparent" && key != "tracestate" {
			return nil, status.Error(codes.InvalidArgument, "browser RPC metadata key is not permitted")
		}
		if len(values) != 1 {
			return nil, status.Error(codes.InvalidArgument, "browser RPC metadata value is malformed")
		}
		value := values[0]
		entries++
		entryBytes := len(key) + len(value)
		if entries > cfg.MaxMetadataEntries || len(value) > cfg.MaxMetadataValueBytes || entryBytes > cfg.MaxMetadataBytes || bytes > cfg.MaxMetadataBytes-entryBytes {
			return nil, status.Error(codes.ResourceExhausted, "browser RPC metadata exceeds safety limits")
		}
		if !validRPCMetadataValue(key, value) {
			return nil, status.Error(codes.InvalidArgument, "browser RPC metadata value is malformed")
		}
		bytes += entryBytes
		clean.Set(key, value)
	}
	return clean, nil
}

func validRPCMetadataValue(key, value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	switch key {
	case "x-request-id", "x-correlation-id":
		for i := 0; i < len(value); i++ {
			char := value[i]
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._:", rune(char))) {
				return false
			}
		}
		return true
	case "traceparent":
		parts := strings.Split(value, "-")
		return len(parts) == 4 && parts[0] == "00" && len(parts[1]) == 32 && lowerHex(parts[1]) && parts[1] != strings.Repeat("0", 32) && len(parts[2]) == 16 && lowerHex(parts[2]) && parts[2] != strings.Repeat("0", 16) && len(parts[3]) == 2 && lowerHex(parts[3])
	case "tracestate":
		seen := map[string]struct{}{}
		for _, member := range strings.Split(value, ",") {
			pair := strings.SplitN(member, "=", 2)
			if len(pair) != 2 || pair[0] == "" || pair[1] == "" || !validTraceStateKey(pair[0]) {
				return false
			}
			if _, duplicate := seen[pair[0]]; duplicate {
				return false
			}
			seen[pair[0]] = struct{}{}
		}
		return true
	}
	return false
}

func lowerHex(value string) bool {
	for i := 0; i < len(value); i++ {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}

func validTraceStateKey(key string) bool {
	for i := 0; i < len(key); i++ {
		char := key[i]
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("_-/.*@", rune(char))) {
			return false
		}
	}
	return key[0] >= 'a' && key[0] <= 'z' || key[0] >= '0' && key[0] <= '9'
}

func checkRPCMessage(message any, expectedType reflect.Type, expectedName protoreflect.FullName, limit int, kind string) error {
	if isNilInterface(message) {
		return status.Error(codes.InvalidArgument, "browser RPC message is not a canonical protobuf")
	}
	p, ok := message.(proto.Message)
	if !ok || reflect.TypeOf(p) != expectedType || p.ProtoReflect().Descriptor().FullName() != expectedName {
		return status.Error(codes.InvalidArgument, "browser RPC message is not a canonical protobuf")
	}
	if size := proto.Size(p); size > limit {
		return status.Error(codes.ResourceExhausted, fmt.Sprintf("browser RPC %s exceeds safety limit", kind))
	}
	return nil
}

func boundedCallOptions(opts []grpc.CallOption, cfg RPCAdapterConfig) ([]grpc.CallOption, error) {
	bounded := make([]grpc.CallOption, 0, len(opts)+3)
	for _, option := range opts {
		if isNilInterface(option) {
			return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
		}
		switch typed := option.(type) {
		case grpc.EmptyCallOption, *grpc.EmptyCallOption, grpc.StaticMethodCallOption, *grpc.StaticMethodCallOption:
			bounded = append(bounded, option)
		case grpc.HeaderCallOption:
			if typed.HeaderAddr == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case *grpc.HeaderCallOption:
			if typed.HeaderAddr == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case grpc.TrailerCallOption:
			if typed.TrailerAddr == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case *grpc.TrailerCallOption:
			if typed.TrailerAddr == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case grpc.PeerCallOption:
			if typed.PeerAddr == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case *grpc.PeerCallOption:
			if typed.PeerAddr == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case grpc.OnFinishCallOption:
			if typed.OnFinish == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case *grpc.OnFinishCallOption:
			if typed.OnFinish == nil {
				return nil, status.Error(codes.InvalidArgument, "browser RPC call option is malformed")
			}
			bounded = append(bounded, option)
		case grpc.FailFastCallOption, *grpc.FailFastCallOption,
			grpc.MaxRecvMsgSizeCallOption, *grpc.MaxRecvMsgSizeCallOption,
			grpc.MaxSendMsgSizeCallOption, *grpc.MaxSendMsgSizeCallOption:
			// The adapter's fail-fast and byte bounds below are authoritative.
		default:
			return nil, status.Error(codes.InvalidArgument, "browser RPC call option is not permitted")
		}
	}
	return append(bounded,
		grpc.MaxCallSendMsgSize(cfg.MaxRequestBytes),
		grpc.MaxCallRecvMsgSize(cfg.MaxResponseBytes),
		grpc.WaitForReady(false),
	), nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
