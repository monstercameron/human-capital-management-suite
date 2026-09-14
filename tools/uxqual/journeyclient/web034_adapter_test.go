package journeyclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type web034Conn struct {
	invokes []string
	streams []string
	ctx     context.Context
	err     error
	stream  grpc.ClientStream
	opts    []grpc.CallOption
	args    any
	reply   any
}

func (c *web034Conn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	c.invokes = append(c.invokes, method)
	c.ctx = ctx
	c.args = args
	c.reply = reply
	c.opts = append([]grpc.CallOption(nil), opts...)
	return c.err
}

func (c *web034Conn) NewStream(ctx context.Context, _ *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	c.streams = append(c.streams, method)
	c.ctx = ctx
	c.opts = append([]grpc.CallOption(nil), opts...)
	if c.stream == nil {
		c.stream = &web034Stream{ctx: ctx}
	}
	if stream, ok := c.stream.(*web034Stream); ok {
		stream.ctx = ctx
	}
	return c.stream, c.err
}

type web034Stream struct {
	ctx       context.Context
	recv      any
	recvErr   error
	sent      any
	received  bool
	header    metadata.MD
	headerErr error
	trailer   metadata.MD
	closeErr  error
	sendErr   error
	closed    bool
}

func (s *web034Stream) Header() (metadata.MD, error) { return s.header, s.headerErr }
func (s *web034Stream) Trailer() metadata.MD         { return s.trailer }
func (s *web034Stream) CloseSend() error             { s.closed = true; return s.closeErr }
func (s *web034Stream) Context() context.Context     { return s.ctx }
func (s *web034Stream) SendMsg(v any) error          { s.sent = v; return s.sendErr }
func (s *web034Stream) RecvMsg(v any) error {
	if s.recvErr != nil {
		return s.recvErr
	}
	if s.received || s.recv == nil {
		return io.EOF
	}
	s.received = true
	if source, ok := s.recv.(*journeyv1.WatchJourneyResponse); ok {
		if target, ok := v.(*journeyv1.WatchJourneyResponse); ok {
			proto.Reset(target)
			proto.Merge(target, source)
		}
	}
	return nil
}

func web034Config(bearer string) RPCAdapterConfig {
	return RPCAdapterConfig{
		Bearer: bearer, MaxRequestBytes: 128, MaxResponseBytes: 256,
		MaxMetadataBytes: 512, MaxMetadataEntries: 16, MaxMetadataValueBytes: 64,
		DefaultDeadline: time.Second, MaxDeadline: 2 * time.Second,
		MaxStreamDeadline: 3 * time.Second,
	}
}

func assertBoundedCallOptions(t *testing.T, opts []grpc.CallOption, sendBytes, receiveBytes int) {
	t.Helper()
	if len(opts) < 3 {
		t.Fatalf("call options = %v, want adapter bounds and fail-fast policy", opts)
	}
	send, ok := opts[len(opts)-3].(grpc.MaxSendMsgSizeCallOption)
	if !ok || send.MaxSendMsgSize != sendBytes {
		t.Fatalf("send option = %#v, want %d-byte adapter bound", opts[len(opts)-3], sendBytes)
	}
	receive, ok := opts[len(opts)-2].(grpc.MaxRecvMsgSizeCallOption)
	if !ok || receive.MaxRecvMsgSize != receiveBytes {
		t.Fatalf("receive option = %#v, want %d-byte adapter bound", opts[len(opts)-2], receiveBytes)
	}
	failFast, ok := opts[len(opts)-1].(grpc.FailFastCallOption)
	if !ok || !failFast.FailFast {
		t.Fatalf("readiness option = %#v, want fail-fast true", opts[len(opts)-1])
	}
}

func TestTodo_WEB_034(t *testing.T) {
	conn := &web034Conn{}
	adapter := NewRPCAdapter(conn, web034Config("server-token"))
	client := journeyv1.NewJourneyServiceClient(adapter)
	if _, err := client.ListJourneys(context.Background(), &journeyv1.ListJourneysRequest{}); err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	if len(conn.invokes) != 1 || conn.invokes[0] != journeyv1.JourneyService_ListJourneys_FullMethodName {
		t.Fatalf("invoked methods = %v, want the canonical list method", conn.invokes)
	}
	md, ok := metadata.FromOutgoingContext(conn.ctx)
	if !ok || !reflect.DeepEqual(md.Get(AuthorizationHeader), []string{BearerScheme + "server-token"}) {
		t.Fatalf("outgoing authorization = %v, want the configured bearer", md)
	}
	if _, ok := conn.ctx.Deadline(); !ok {
		t.Fatal("a finite unary RPC received no default deadline")
	}
	if conn.args == nil || conn.reply == nil {
		t.Fatalf("canonical request/response were not passed through: %#v/%#v", conn.args, conn.reply)
	}
	assertBoundedCallOptions(t, conn.opts, 128, 256)
}

func TestTodo_WEB_034_Golden(t *testing.T) {
	methods, err := canonicalRPCMethods()
	if err != nil {
		t.Fatalf("canonical descriptors: %v", err)
	}
	got := make([]string, 0, len(methods))
	for _, method := range methods {
		got = append(got, fmt.Sprintf("%s|%d|%s|%s", method.name, method.kind, method.inputName, method.outputName))
	}
	sort.Strings(got)
	want := strings.Split(strings.TrimSpace(`
/hcmnext.journey.v1.JourneyService/CreateWorker|1|hcmnext.journey.v1.CreateWorkerRequest|hcmnext.journey.v1.CreateWorkerResponse
/hcmnext.journey.v1.JourneyService/DecideJourney|1|hcmnext.journey.v1.DecideJourneyRequest|hcmnext.journey.v1.DecideJourneyResponse
/hcmnext.journey.v1.JourneyService/EditProposal|1|hcmnext.journey.v1.EditProposalRequest|hcmnext.journey.v1.EditProposalResponse
/hcmnext.journey.v1.JourneyService/ExecuteJourney|1|hcmnext.journey.v1.ExecuteJourneyRequest|hcmnext.journey.v1.ExecuteJourneyResponse
/hcmnext.journey.v1.JourneyService/GetProductPreferences|1|hcmnext.journey.v1.GetProductPreferencesRequest|hcmnext.journey.v1.GetProductPreferencesResponse
/hcmnext.journey.v1.JourneyService/GetRoleAccess|1|hcmnext.journey.v1.GetRoleAccessRequest|hcmnext.journey.v1.GetRoleAccessResponse
/hcmnext.journey.v1.JourneyService/GetWorkerIDPolicy|1|hcmnext.journey.v1.GetWorkerIDPolicyRequest|hcmnext.journey.v1.GetWorkerIDPolicyResponse
/hcmnext.journey.v1.JourneyService/InspectJourney|1|hcmnext.journey.v1.InspectJourneyRequest|hcmnext.journey.v1.InspectJourneyResponse
/hcmnext.journey.v1.JourneyService/ListJourneys|1|hcmnext.journey.v1.ListJourneysRequest|hcmnext.journey.v1.ListJourneysResponse
/hcmnext.journey.v1.JourneyService/ListWorkers|1|hcmnext.journey.v1.ListWorkersRequest|hcmnext.journey.v1.ListWorkersResponse
/hcmnext.journey.v1.JourneyService/PreviewJourneyIntervention|1|hcmnext.journey.v1.PreviewJourneyInterventionRequest|hcmnext.journey.v1.PreviewJourneyInterventionResponse
/hcmnext.journey.v1.JourneyService/ProposeJourney|1|hcmnext.journey.v1.ProposeJourneyRequest|hcmnext.journey.v1.ProposeJourneyResponse
/hcmnext.journey.v1.JourneyService/ProposePromotion|1|hcmnext.journey.v1.ProposePromotionRequest|hcmnext.journey.v1.ProposePromotionResponse
/hcmnext.journey.v1.JourneyService/RecordWorkflowUse|1|hcmnext.journey.v1.RecordWorkflowUseRequest|hcmnext.journey.v1.RecordWorkflowUseResponse
/hcmnext.journey.v1.JourneyService/RequestJourneyIntervention|1|hcmnext.journey.v1.RequestJourneyInterventionRequest|hcmnext.journey.v1.RequestJourneyInterventionResponse
/hcmnext.journey.v1.JourneyService/SaveAccessRole|1|hcmnext.journey.v1.SaveAccessRoleRequest|hcmnext.journey.v1.SaveAccessRoleResponse
/hcmnext.journey.v1.JourneyService/SaveOrganizationVisibility|1|hcmnext.journey.v1.SaveOrganizationVisibilityRequest|hcmnext.journey.v1.SaveOrganizationVisibilityResponse
/hcmnext.journey.v1.JourneyService/SaveRoleOrganizationVisibility|1|hcmnext.journey.v1.SaveRoleOrganizationVisibilityRequest|hcmnext.journey.v1.SaveRoleOrganizationVisibilityResponse
/hcmnext.journey.v1.JourneyService/SaveRolePagePermission|1|hcmnext.journey.v1.SaveRolePagePermissionRequest|hcmnext.journey.v1.SaveRolePagePermissionResponse
/hcmnext.journey.v1.JourneyService/SaveTenantAppearance|1|hcmnext.journey.v1.SaveTenantAppearanceRequest|hcmnext.journey.v1.SaveTenantAppearanceResponse
/hcmnext.journey.v1.JourneyService/SaveUserPreferences|1|hcmnext.journey.v1.SaveUserPreferencesRequest|hcmnext.journey.v1.SaveUserPreferencesResponse
/hcmnext.journey.v1.JourneyService/SaveWorkerIDPolicy|1|hcmnext.journey.v1.SaveWorkerIDPolicyRequest|hcmnext.journey.v1.SaveWorkerIDPolicyResponse
/hcmnext.journey.v1.JourneyService/SaveWorkerRoleAssignment|1|hcmnext.journey.v1.SaveWorkerRoleAssignmentRequest|hcmnext.journey.v1.SaveWorkerRoleAssignmentResponse
/hcmnext.journey.v1.JourneyService/WatchJourney|2|hcmnext.journey.v1.WatchJourneyRequest|hcmnext.journey.v1.WatchJourneyResponse
`), "\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated method descriptor vector:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	protoService := journeyv1.File_hcmnext_journey_v1_journey_service_proto.Services().ByName("JourneyService")
	missing := journeyv1.JourneyService_ServiceDesc
	missing.Methods = append([]grpc.MethodDesc(nil), missing.Methods[1:]...)
	if _, err := buildRPCMethods(protoService, missing); err == nil {
		t.Fatal("descriptor cross-check accepted a missing generated method")
	}
	duplicate := journeyv1.JourneyService_ServiceDesc
	duplicate.Methods = append(append([]grpc.MethodDesc(nil), duplicate.Methods...), duplicate.Methods[0])
	if _, err := buildRPCMethods(protoService, duplicate); err == nil {
		t.Fatal("descriptor cross-check accepted a duplicated generated method")
	}
	wrongKind := journeyv1.JourneyService_ServiceDesc
	wrongKind.Streams = append([]grpc.StreamDesc(nil), wrongKind.Streams...)
	wrongKind.Streams[0].ClientStreams = true
	if _, err := buildRPCMethods(protoService, wrongKind); err == nil {
		t.Fatal("descriptor cross-check accepted a changed stream kind")
	}
}

// The Browser matrix row exercises the generated browser-facing streaming
// shape in a native harness. It deliberately does not claim a real browser or
// GoGRPCBridge tunnel; that qualification is a separate runtime gate.
func TestTodo_WEB_034_Browser(t *testing.T) {
	conn := &web034Conn{}
	stream := &web034Stream{recv: &journeyv1.WatchJourneyResponse{Cursor: "cursor-1"}, trailer: metadata.Pairs("x-result", "complete")}
	conn.stream = stream
	client := journeyv1.NewJourneyServiceClient(NewRPCAdapter(conn, web034Config("browser")))
	watch, err := client.WatchJourney(context.Background(), &journeyv1.WatchJourneyRequest{IntentId: "intent-1"})
	if err != nil {
		t.Fatalf("WatchJourney: %v", err)
	}
	got, err := watch.Recv()
	if err != nil || got.GetCursor() != "cursor-1" {
		t.Fatalf("WatchJourney response = %#v/%v, want the canonical stream response", got, err)
	}
	if len(conn.streams) != 1 || conn.streams[0] != journeyv1.JourneyService_WatchJourney_FullMethodName {
		t.Fatalf("stream methods = %v, want canonical WatchJourney", conn.streams)
	}
	if _, hasDeadline := watch.Context().Deadline(); hasDeadline {
		t.Fatal("page-lifetime stream without a caller deadline was given a synthetic deadline")
	}
	streamMetadata, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := streamMetadata.Get(AuthorizationHeader); !reflect.DeepEqual(got, []string{"Bearer browser"}) {
		t.Fatalf("stream authorization = %v, want server-provided bearer", got)
	}
	if _, err := watch.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal stream error = %v, want io.EOF", err)
	}
	select {
	case <-watch.Context().Done():
	default:
		t.Fatal("terminal EOF did not release the adapter-owned stream context")
	}
	if got := watch.Trailer().Get("x-result"); !reflect.DeepEqual(got, []string{"complete"}) {
		t.Fatalf("stream trailer = %v, want preserved terminal trailer", got)
	}
}

func TestTodo_WEB_034_Conformance(t *testing.T) {
	conn := &web034Conn{}
	adapter := NewRPCAdapter(conn, web034Config("conformance"))
	type contextKey struct{}
	shortDeadline := time.Now().Add(250 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "local-only"), shortDeadline)
	defer cancel()
	if err := adapter.Invoke(ctx, journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}, grpc.WaitForReady(true), grpc.MaxCallSendMsgSize(1<<30), grpc.MaxCallRecvMsgSize(1<<30)); err != nil {
		t.Fatalf("adapter invoke: %v", err)
	}
	gotDeadline, ok := conn.ctx.Deadline()
	if !ok || !gotDeadline.Equal(shortDeadline) {
		t.Fatalf("adapter deadline = %v/%v, want exact short caller deadline %v", gotDeadline, ok, shortDeadline)
	}
	if got := conn.ctx.Value(contextKey{}); got != "local-only" {
		t.Fatalf("adapter context value = %v, want preserved local cancellation context", got)
	}
	assertBoundedCallOptions(t, conn.opts, 128, 256)

	longCtx, longCancel := context.WithTimeout(context.Background(), time.Hour)
	defer longCancel()
	if err := adapter.Invoke(longCtx, journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}); err != nil {
		t.Fatalf("long-deadline invoke: %v", err)
	}
	if deadline, ok := conn.ctx.Deadline(); !ok || time.Until(deadline) > 2100*time.Millisecond || time.Until(deadline) < 1500*time.Millisecond {
		t.Fatalf("capped deadline remaining = %v/%v, want approximately 2s", time.Until(deadline), ok)
	}
	if err := adapter.Invoke(context.Background(), "/hcmnext.journey.v1.JourneyService/NotAServiceMethod", &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown method code = %v, want INVALID_ARGUMENT", status.Code(err))
	}
	if err := adapter.Invoke(context.Background(), journeyv1.JourneyService_WatchJourney_FullMethodName, &journeyv1.WatchJourneyRequest{}, &journeyv1.WatchJourneyResponse{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("stream through unary path code = %v, want INVALID_ARGUMENT", status.Code(err))
	}
	if _, err := adapter.NewStream(context.Background(), &grpc.StreamDesc{StreamName: "ListJourneys", ServerStreams: true}, journeyv1.JourneyService_ListJourneys_FullMethodName); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unary through stream path code = %v, want INVALID_ARGUMENT", status.Code(err))
	}
	if _, err := adapter.NewStream(context.Background(), nil, journeyv1.JourneyService_WatchJourney_FullMethodName); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil stream descriptor code = %v, want INVALID_ARGUMENT", status.Code(err))
	}
	if _, err := adapter.NewStream(context.Background(), &grpc.StreamDesc{StreamName: "WatchJourney", ClientStreams: true, ServerStreams: true}, journeyv1.JourneyService_WatchJourney_FullMethodName); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("forged stream descriptor code = %v, want INVALID_ARGUMENT", status.Code(err))
	}
	conn.err = web034DeniedStatus().Err()
	before := len(conn.invokes)
	err := adapter.Invoke(context.Background(), journeyv1.JourneyService_ProposeJourney_FullMethodName, &journeyv1.ProposeJourneyRequest{}, &journeyv1.ProposeJourneyResponse{})
	if !proto.Equal(status.Convert(err).Proto(), web034DeniedStatus().Proto()) {
		t.Fatalf("mutation refusal = %v, want exact upstream status", err)
	}
	if attempts := len(conn.invokes) - before; attempts != 1 {
		t.Fatalf("mutation attempts = %d, want exactly one with no adapter retry", attempts)
	}
}

func TestTodo_WEB_034_Security(t *testing.T) {
	invoke := func(adapter *RPCAdapter, conn *web034Conn, ctx context.Context) error {
		return adapter.Invoke(ctx, journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{})
	}
	conn := &web034Conn{}
	adapter := NewRPCAdapter(conn, web034Config("server-issued"))
	for _, forbidden := range trust.ReservedMetadataKeys() {
		ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(forbidden, "attacker-selected"))
		if err := invoke(adapter, conn, ctx); status.Code(err) != codes.InvalidArgument {
			t.Errorf("reserved metadata %q code = %v, want INVALID_ARGUMENT", forbidden, status.Code(err))
		}
	}
	original := metadata.MD{
		"x-request-id":     {"request-safe"},
		"x-correlation-id": {"correlation.safe-1"},
		"traceparent":      {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		"tracestate":       {"vendor=value,tenant@system=opaque"},
	}
	wantOriginal := original.Copy()
	ctx := metadata.NewOutgoingContext(context.Background(), original)
	if err := invoke(adapter, conn, ctx); err != nil {
		t.Fatalf("safe metadata invoke: %v", err)
	}
	if !reflect.DeepEqual(original, wantOriginal) {
		t.Fatalf("adapter mutated caller metadata: got %v want %v", original, wantOriginal)
	}
	md, _ := metadata.FromOutgoingContext(conn.ctx)
	if got := md.Get(AuthorizationHeader); len(got) != 1 || got[0] != "Bearer server-issued" {
		t.Fatalf("authorization metadata = %v, want only server-issued credential", got)
	}
	if err := invoke(adapter, conn, metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer attacker"))); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("caller authorization metadata code = %v, want INVALID_ARGUMENT", status.Code(err))
	}
	if got := md.Get("x-request-id"); len(got) != 1 || got[0] != "request-safe" {
		t.Fatalf("safe request metadata = %v, want retained diagnostic", got)
	}
	if _, err := boundedMetadata(metadata.MD{"X-Request-Id": {"one"}}, web034Config("unused")); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("raw uppercase metadata code = %v, want INVALID_ARGUMENT", status.Code(err))
	}

	malformed := []struct {
		name string
		md   metadata.MD
		code codes.Code
	}{
		{name: "forged tenant", md: metadata.Pairs("x-tenant-id", "attacker"), code: codes.InvalidArgument},
		{name: "forged subject", md: metadata.Pairs("x-subject-id", "attacker"), code: codes.InvalidArgument},
		{name: "forged roles", md: metadata.Pairs("x-roles", "admin"), code: codes.InvalidArgument},
		{name: "forged purpose", md: metadata.Pairs("x-purpose", "payroll"), code: codes.InvalidArgument},
		{name: "space padded key", md: metadata.MD{" x-request-id": {"one"}}, code: codes.InvalidArgument},
		{name: "duplicate value", md: metadata.MD{"x-request-id": {"one", "two"}}, code: codes.InvalidArgument},
		{name: "empty value", md: metadata.Pairs("x-request-id", ""), code: codes.InvalidArgument},
		{name: "space value", md: metadata.Pairs("x-request-id", "one two"), code: codes.InvalidArgument},
		{name: "newline", md: metadata.Pairs("x-request-id", "line\nvalue"), code: codes.InvalidArgument},
		{name: "tab", md: metadata.Pairs("x-request-id", "one\ttwo"), code: codes.InvalidArgument},
		{name: "delete", md: metadata.Pairs("x-request-id", "one\x7ftwo"), code: codes.InvalidArgument},
		{name: "non ascii", md: metadata.Pairs("x-request-id", "café"), code: codes.InvalidArgument},
		{name: "bad traceparent", md: metadata.Pairs("traceparent", "00-00000000000000000000000000000000-0000000000000000-01"), code: codes.InvalidArgument},
		{name: "duplicate tracestate", md: metadata.Pairs("tracestate", "vendor=a,vendor=b"), code: codes.InvalidArgument},
		{name: "oversized", md: metadata.Pairs("x-request-id", strings.Repeat("x", 65)), code: codes.ResourceExhausted},
	}
	for _, test := range malformed {
		t.Run(test.name, func(t *testing.T) {
			before := len(conn.invokes)
			err := invoke(adapter, conn, metadata.NewOutgoingContext(context.Background(), test.md))
			if status.Code(err) != test.code {
				t.Fatalf("code = %v, want %v (%v)", status.Code(err), test.code, err)
			}
			if len(conn.invokes) != before {
				t.Fatalf("rejected metadata reached connection: %v", conn.invokes[before:])
			}
		})
	}

	for _, test := range []struct {
		name   string
		bearer string
		code   codes.Code
	}{
		{name: "missing", bearer: "", code: codes.Unauthenticated},
		{name: "leading whitespace", bearer: " token", code: codes.InvalidArgument},
		{name: "trailing whitespace", bearer: "token ", code: codes.InvalidArgument},
		{name: "scheme injection", bearer: "Bearer token", code: codes.InvalidArgument},
		{name: "line injection", bearer: "token\r\nrole:admin", code: codes.InvalidArgument},
		{name: "non ascii", bearer: "tøken", code: codes.InvalidArgument},
		{name: "padding in middle", bearer: "a=b", code: codes.InvalidArgument},
		{name: "oversized", bearer: strings.Repeat("a", maxRPCBearerBytes+1), code: codes.ResourceExhausted},
	} {
		t.Run("bearer "+test.name, func(t *testing.T) {
			bearerConn := &web034Conn{}
			err := invoke(NewRPCAdapter(bearerConn, web034Config(test.bearer)), bearerConn, context.Background())
			if status.Code(err) != test.code {
				t.Fatalf("code = %v, want %v (%v)", status.Code(err), test.code, err)
			}
			if len(bearerConn.invokes) != 0 {
				t.Fatalf("invalid bearer reached connection: %v", bearerConn.invokes)
			}
		})
	}

	credentialsConn := &web034Conn{}
	credentialsAdapter := NewRPCAdapter(credentialsConn, web034Config("server-issued"))
	err := credentialsAdapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}, grpc.PerRPCCredentials(web034InjectedCredentials{}))
	if status.Code(err) != codes.InvalidArgument || len(credentialsConn.invokes) != 0 {
		t.Fatalf("per-RPC credential injection = %v/calls %v, want local INVALID_ARGUMENT", err, credentialsConn.invokes)
	}
	err = credentialsAdapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}, grpc.CallAuthority("attacker.example"))
	if status.Code(err) != codes.InvalidArgument || len(credentialsConn.invokes) != 0 {
		t.Fatalf("authority override injection = %v/calls %v, want local INVALID_ARGUMENT", err, credentialsConn.invokes)
	}
	err = credentialsAdapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}, grpc.Header(nil))
	if status.Code(err) != codes.InvalidArgument || len(credentialsConn.invokes) != 0 {
		t.Fatalf("nil header option = %v/calls %v, want local INVALID_ARGUMENT", err, credentialsConn.invokes)
	}
}

type web034InjectedCredentials struct{}

func (web034InjectedCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer attacker"}, nil
}

func (web034InjectedCredentials) RequireTransportSecurity() bool { return false }

func TestTodo_WEB_034_Integration(t *testing.T) {
	const bufSize = 1 << 20
	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	journeyv1.RegisterJourneyServiceServer(server, web034Service{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := journeyv1.NewJourneyServiceClient(NewRPCAdapter(conn, web034Config("integration")))
	var header, trailer metadata.MD
	_, err = client.ListJourneys(context.Background(), &journeyv1.ListJourneysRequest{}, grpc.Header(&header), grpc.Trailer(&trailer))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status code = %v, want PERMISSION_DENIED", status.Code(err))
	}
	wantStatus := web034DeniedStatus()
	if !proto.Equal(status.Convert(err).Proto(), wantStatus.Proto()) {
		t.Fatalf("status proto = %v, want exact %v", status.Convert(err).Proto(), wantStatus.Proto())
	}
	if got := header.Get("x-web034-header"); !reflect.DeepEqual(got, []string{"canonical"}) {
		t.Fatalf("header = %v, want preserved canonical header", got)
	}
	if got := trailer.Get("x-web034-trailer"); !reflect.DeepEqual(got, []string{"canonical"}) {
		t.Fatalf("trailer = %v, want preserved canonical trailer", got)
	}
}

type web034Service struct {
	journeyv1.UnimplementedJourneyServiceServer
}

func (web034Service) ListJourneys(ctx context.Context, _ *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	if err := grpc.SetHeader(ctx, metadata.Pairs("x-web034-header", "canonical")); err != nil {
		return nil, err
	}
	grpc.SetTrailer(ctx, metadata.Pairs("x-web034-trailer", "canonical"))
	return nil, web034DeniedStatus().Err()
}

func web034DeniedStatus() *status.Status {
	owned, err := status.New(codes.PermissionDenied, "denied").WithDetails(&commonv1.ErrorDetail{ReasonRef: "web034.integration.denied", Retryable: true})
	if err != nil {
		panic(err)
	}
	return owned
}

func TestTodo_WEB_034_Fault(t *testing.T) {
	conn := &web034Conn{}
	adapter := NewRPCAdapter(conn, web034Config("fault"))
	large := &journeyv1.ProposeJourneyRequest{BusinessReason: strings.Repeat("x", 129)}
	err := adapter.Invoke(context.Background(), journeyv1.JourneyService_ProposeJourney_FullMethodName, large, &journeyv1.ProposeJourneyResponse{})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized request code = %v, want RESOURCE_EXHAUSTED", status.Code(err))
	}
	if len(conn.invokes) != 0 {
		t.Fatalf("oversized request reached the connection: %v", conn.invokes)
	}

	for _, test := range []struct {
		name  string
		args  any
		reply any
	}{
		{name: "nil request", args: nil, reply: &journeyv1.ListJourneysResponse{}},
		{name: "typed nil request", args: (*journeyv1.ListJourneysRequest)(nil), reply: &journeyv1.ListJourneysResponse{}},
		{name: "non protobuf request", args: "not protobuf", reply: &journeyv1.ListJourneysResponse{}},
		{name: "wrong protobuf request", args: &journeyv1.ListWorkersRequest{}, reply: &journeyv1.ListJourneysResponse{}},
		{name: "nil response", args: &journeyv1.ListJourneysRequest{}, reply: nil},
		{name: "typed nil response", args: &journeyv1.ListJourneysRequest{}, reply: (*journeyv1.ListJourneysResponse)(nil)},
		{name: "wrong protobuf response", args: &journeyv1.ListJourneysRequest{}, reply: &journeyv1.ListWorkersResponse{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := len(conn.invokes)
			err := adapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, test.args, test.reply)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want INVALID_ARGUMENT (%v)", status.Code(err), err)
			}
			if len(conn.invokes) != before {
				t.Fatalf("invalid message reached connection: %v", conn.invokes[before:])
			}
		})
	}

	responseConn := &web034OversizeResponseConn{}
	responseAdapter := NewRPCAdapter(responseConn, web034Config("fault"))
	err = responseAdapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{})
	if status.Code(err) != codes.ResourceExhausted || responseConn.calls != 1 {
		t.Fatalf("oversized response = %v/calls %d, want one call then RESOURCE_EXHAUSTED", err, responseConn.calls)
	}

	cancelConn := &web034CancellationConn{started: make(chan struct{})}
	cancelAdapter := NewRPCAdapter(cancelConn, web034Config("fault"))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- cancelAdapter.Invoke(ctx, journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{})
	}()
	<-cancelConn.started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("post-dispatch cancellation error = %v, want context.Canceled", err)
	}
	if cancelConn.calls.Load() != 1 {
		t.Fatalf("cancelled call attempts = %d, want exactly one", cancelConn.calls.Load())
	}

	alreadyCancelled, cancelAlready := context.WithCancel(context.Background())
	cancelAlready()
	before := len(conn.invokes)
	if err := adapter.Invoke(alreadyCancelled, journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-dispatch cancellation error = %v, want context.Canceled", err)
	}
	if len(conn.invokes) != before {
		t.Fatalf("pre-cancelled call reached connection: %v", conn.invokes[before:])
	}
}

type web034OversizeResponseConn struct{ calls int }

func (c *web034OversizeResponseConn) Invoke(_ context.Context, _ string, _, reply any, _ ...grpc.CallOption) error {
	c.calls++
	reply.(proto.Message).ProtoReflect().SetUnknown(bytes.Repeat([]byte{0xa2, 0x06, 0x7d}, 100))
	return nil
}

func (*web034OversizeResponseConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("unexpected stream")
}

type web034CancellationConn struct {
	started chan struct{}
	calls   atomic.Int64
}

func (c *web034CancellationConn) Invoke(ctx context.Context, _ string, _, _ any, _ ...grpc.CallOption) error {
	c.calls.Add(1)
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}

func (*web034CancellationConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("unexpected stream")
}

func TestRPCAdapterStreamFaultsReleaseResources(t *testing.T) {
	t.Run("nil connection stream", func(t *testing.T) {
		adapter := NewRPCAdapter(web034NilStreamConn{}, web034Config("stream"))
		_, err := adapter.NewStream(context.Background(), &journeyv1.JourneyService_ServiceDesc.Streams[0], journeyv1.JourneyService_WatchJourney_FullMethodName)
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("code = %v, want UNAVAILABLE (%v)", status.Code(err), err)
		}
	})

	for _, test := range []struct {
		name string
		run  func(grpc.ClientStream) error
	}{
		{name: "wrong send type", run: func(stream grpc.ClientStream) error { return stream.SendMsg(&journeyv1.ListJourneysRequest{}) }},
		{name: "oversized send", run: func(stream grpc.ClientStream) error {
			return stream.SendMsg(&journeyv1.WatchJourneyRequest{IntentId: strings.Repeat("x", 129)})
		}},
		{name: "wrong receive type", run: func(stream grpc.ClientStream) error { return stream.RecvMsg(&journeyv1.ListJourneysResponse{}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &web034Conn{stream: &web034Stream{recv: &journeyv1.WatchJourneyResponse{}}}
			adapter := NewRPCAdapter(conn, web034Config("stream"))
			stream, err := adapter.NewStream(context.Background(), &journeyv1.JourneyService_ServiceDesc.Streams[0], journeyv1.JourneyService_WatchJourney_FullMethodName)
			if err != nil {
				t.Fatalf("NewStream: %v", err)
			}
			if err := test.run(stream); status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("fault code = %v, want INVALID_ARGUMENT or RESOURCE_EXHAUSTED (%v)", status.Code(err), err)
			}
			select {
			case <-stream.Context().Done():
			default:
				t.Fatal("local terminal stream fault did not release context")
			}
		})
	}

	t.Run("oversized receive and trailer", func(t *testing.T) {
		response := &journeyv1.WatchJourneyResponse{}
		response.ProtoReflect().SetUnknown(bytes.Repeat([]byte{0xa2, 0x06, 0x7d}, 100))
		underlying := &web034Stream{recv: response, trailer: metadata.Pairs("x-final", "kept")}
		adapter := NewRPCAdapter(&web034Conn{stream: underlying}, web034Config("stream"))
		stream, err := adapter.NewStream(context.Background(), &journeyv1.JourneyService_ServiceDesc.Streams[0], journeyv1.JourneyService_WatchJourney_FullMethodName)
		if err != nil {
			t.Fatalf("NewStream: %v", err)
		}
		err = stream.RecvMsg(&journeyv1.WatchJourneyResponse{})
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("oversized response code = %v, want RESOURCE_EXHAUSTED (%v)", status.Code(err), err)
		}
		if got := stream.Trailer().Get("x-final"); !reflect.DeepEqual(got, []string{"kept"}) {
			t.Fatalf("trailer after local bound failure = %v, want preserved", got)
		}
		select {
		case <-stream.Context().Done():
		default:
			t.Fatal("oversized response did not release stream context")
		}
	})

	t.Run("upstream status details and trailer", func(t *testing.T) {
		underlying := &web034Stream{recvErr: web034DeniedStatus().Err(), trailer: metadata.Pairs("x-final", "denied")}
		adapter := NewRPCAdapter(&web034Conn{stream: underlying}, web034Config("stream"))
		stream, err := adapter.NewStream(context.Background(), &journeyv1.JourneyService_ServiceDesc.Streams[0], journeyv1.JourneyService_WatchJourney_FullMethodName)
		if err != nil {
			t.Fatalf("NewStream: %v", err)
		}
		err = stream.RecvMsg(&journeyv1.WatchJourneyResponse{})
		if !proto.Equal(status.Convert(err).Proto(), web034DeniedStatus().Proto()) {
			t.Fatalf("stream status = %v, want exact %v", status.Convert(err).Proto(), web034DeniedStatus().Proto())
		}
		if got := stream.Trailer().Get("x-final"); !reflect.DeepEqual(got, []string{"denied"}) {
			t.Fatalf("terminal trailer = %v, want preserved", got)
		}
		select {
		case <-stream.Context().Done():
		default:
			t.Fatal("upstream terminal status did not release stream context")
		}
	})
}

type web034NilStreamConn struct{}

func (web034NilStreamConn) Invoke(context.Context, string, any, any, ...grpc.CallOption) error {
	return errors.New("unexpected invoke")
}

func (web034NilStreamConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, nil
}

func TestRPCAdapterNilConnectionAndDeadlineEdges(t *testing.T) {
	var typedNil *web034Conn
	for name, adapter := range map[string]*RPCAdapter{
		"nil adapter":    nil,
		"nil connection": NewRPCAdapter(nil, web034Config("fault")),
		"typed nil":      NewRPCAdapter(typedNil, web034Config("fault")),
	} {
		t.Run(name, func(t *testing.T) {
			err := adapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{})
			if status.Code(err) != codes.Unavailable {
				t.Fatalf("code = %v, want UNAVAILABLE (%v)", status.Code(err), err)
			}
		})
	}

	conn := &web034Conn{}
	adapter := NewRPCAdapter(conn, web034Config("deadline"))
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := adapter.Invoke(expired, journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired error = %v, want context.DeadlineExceeded", err)
	}
	if len(conn.invokes) != 0 {
		t.Fatalf("expired call reached connection: %v", conn.invokes)
	}

	longStreamCtx, cancelLongStream := context.WithTimeout(context.Background(), time.Hour)
	defer cancelLongStream()
	stream, err := adapter.NewStream(longStreamCtx, &journeyv1.JourneyService_ServiceDesc.Streams[0], journeyv1.JourneyService_WatchJourney_FullMethodName)
	if err != nil {
		t.Fatalf("long stream: %v", err)
	}
	if deadline, ok := stream.Context().Deadline(); !ok || time.Until(deadline) > 3100*time.Millisecond || time.Until(deadline) < 2500*time.Millisecond {
		t.Fatalf("stream deadline remaining = %v/%v, want approximately 3s", time.Until(deadline), ok)
	}
	_ = stream.CloseSend()
}

func TestRPCAdapterConcurrentUseIsInstanceOwned(t *testing.T) {
	conn := &web034ConcurrentConn{}
	adapter := NewRPCAdapter(conn, web034Config("concurrent"))
	const calls = 64
	var group sync.WaitGroup
	errorsSeen := make(chan error, calls)
	for i := 0; i < calls; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsSeen <- adapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{})
		}()
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent invoke: %v", err)
		}
	}
	if got := conn.calls.Load(); got != calls {
		t.Fatalf("connection calls = %d, want %d", got, calls)
	}
	if conn.badMetadata.Load() != 0 {
		t.Fatalf("calls with changed authorization = %d, want zero", conn.badMetadata.Load())
	}
}

type web034ConcurrentConn struct {
	calls       atomic.Int64
	badMetadata atomic.Int64
}

func (c *web034ConcurrentConn) Invoke(ctx context.Context, _ string, _, _ any, _ ...grpc.CallOption) error {
	c.calls.Add(1)
	md, _ := metadata.FromOutgoingContext(ctx)
	if got := md.Get(AuthorizationHeader); !reflect.DeepEqual(got, []string{"Bearer concurrent"}) {
		c.badMetadata.Add(1)
	}
	return nil
}

func (*web034ConcurrentConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("unexpected stream")
}

func TestTodo_WEB_034_Latency(t *testing.T) {
	budget := latencygate.Budget{Name: "browser RPC adapter boundary", P95: 2 * time.Millisecond, Warmups: 3, Samples: 25}
	result, err := latencygate.Measure(budget, func() error {
		conn := &web034Conn{}
		adapter := NewRPCAdapter(conn, web034Config("latency"))
		return adapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, &journeyv1.ListJourneysRequest{}, &journeyv1.ListJourneysResponse{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}

func BenchmarkBrowserRPCAdapter(b *testing.B) {
	b.ReportAllocs()
	conn := &web034Conn{}
	adapter := NewRPCAdapter(conn, web034Config("benchmark"))
	request := &journeyv1.ListJourneysRequest{}
	response := &journeyv1.ListJourneysResponse{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := adapter.Invoke(context.Background(), journeyv1.JourneyService_ListJourneys_FullMethodName, request, response); err != nil {
			b.Fatal(err)
		}
	}
}
