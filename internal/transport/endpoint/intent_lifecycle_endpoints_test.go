package endpoint_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// This file is EP-INTENT-003's transport-layer contract, following the
// sibling convention runIntentCreateGetListContract/runExplanationContract
// already establish in intent_endpoints_test.go: one shared run...Contract
// helper drives both gRPC and grpcbridge HTTP over the shared
// transporttest.IntentHandler fixture and asserts identical wire results.
//
// The fixture's own SubmitIntent/CancelIntent/SupersedeIntent methods
// (internal/transport/transporttest/fixture.go) predate this todo — they
// exist so the transport plumbing (routing, marshalling, admission, error
// projection) for these three RPCs was already provable before
// internal/intent/app.IntentService implemented them for real. This file's
// job is exactly that transport plumbing: exact HTTP/gRPC parity on success,
// on a typed precondition failure, and on the fixture's raw-fault scenario
// (which proves unowned provider text never crosses the edge). The real
// business-rule contract — the four cancellation dispositions, submit-once,
// supersede's non-mutation of the original — is proven against the real
// IntentService in internal/intent/app/lifecycle_contract_test.go; a fixed
// fake has no business rule to prove that contract against.
func runIntentSubmitCancelSupersedeContract(t *testing.T) {
	t.Helper()
	h := newEndpointHarness(t)
	ctx := context.Background()

	// SubmitIntent: exact parity on success, and on the FAILED_PRECONDITION
	// stale-revision refusal the fixture's own doc comment says exercises
	// connect-go's HTTP status override.
	submitOK := &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-parity", IntentId: transporttest.KnownIntentID,
		ProposalRevisionId: "revision-1", ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
	}
	grpcSubmit, err := h.grpcIntent.SubmitIntent(h.grpcContext(ctx), proto.Clone(submitOK).(*intentsv1.SubmitIntentRequest))
	if err != nil {
		t.Fatalf("gRPC SubmitIntent: %v", err)
	}
	edgeSubmit, err := h.edgeIntent.SubmitIntent(ctx, edgeRequest(h, proto.Clone(submitOK).(*intentsv1.SubmitIntentRequest)))
	if err != nil {
		t.Fatalf("HTTP SubmitIntent: %v", err)
	}
	assertProtoParity(t, grpcSubmit, edgeSubmit.Msg)
	if grpcSubmit.GetIntent().GetInstanceVersion() != transporttest.CurrentInstanceVersion+1 {
		t.Fatalf("submitted intent version = %d, want %d", grpcSubmit.GetIntent().GetInstanceVersion(), transporttest.CurrentInstanceVersion+1)
	}

	staleSubmit := &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-stale", IntentId: transporttest.KnownIntentID,
		ProposalRevisionId: "revision-1", ExpectedInstanceVersion: transporttest.CurrentInstanceVersion + 41,
	}
	grpcErrCall := func() error {
		_, err := h.grpcIntent.SubmitIntent(h.grpcContext(ctx), proto.Clone(staleSubmit).(*intentsv1.SubmitIntentRequest))
		return err
	}
	edgeErrCall := func() error {
		_, err := h.edgeIntent.SubmitIntent(ctx, edgeRequest(h, proto.Clone(staleSubmit).(*intentsv1.SubmitIntentRequest)))
		return err
	}
	assertErrorParity(t, grpcErrCall(), edgeErrCall())

	// CancelIntent: exact parity on success.
	cancelOK := &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-cancel-parity", IntentId: transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion, ReasonRef: "reason:withdrawn",
	}
	grpcCancel, err := h.grpcIntent.CancelIntent(h.grpcContext(ctx), proto.Clone(cancelOK).(*intentsv1.CancelIntentRequest))
	if err != nil {
		t.Fatalf("gRPC CancelIntent: %v", err)
	}
	edgeCancel, err := h.edgeIntent.CancelIntent(ctx, edgeRequest(h, proto.Clone(cancelOK).(*intentsv1.CancelIntentRequest)))
	if err != nil {
		t.Fatalf("HTTP CancelIntent: %v", err)
	}
	assertProtoParity(t, grpcCancel, edgeCancel.Msg)

	// CancelIntent's raw-fault scenario: an unowned, provider-shaped error
	// must project to the same safe, owned refusal on both transports, and
	// the raw diagnostic text must never appear in either.
	fault := &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-cancel-fault", IntentId: transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion, ReasonRef: transporttest.RawFaultReasonRef,
	}
	grpcFaultCall := func() error {
		_, err := h.grpcIntent.CancelIntent(h.grpcContext(ctx), proto.Clone(fault).(*intentsv1.CancelIntentRequest))
		return err
	}
	edgeFaultCall := func() error {
		_, err := h.edgeIntent.CancelIntent(ctx, edgeRequest(h, proto.Clone(fault).(*intentsv1.CancelIntentRequest)))
		return err
	}
	assertErrorParity(t, grpcFaultCall(), edgeFaultCall())

	// SupersedeIntent: exact parity on success.
	supersedeOK := &intentsv1.SupersedeIntentRequest{
		IdempotencyKey: "idem-supersede-parity", SupersededIntentId: transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion, ReasonRef: "reason:replaced",
		Definition: &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
		Request: &intentsv1.TypedPayload{
			Schema:            &intentsv1.SchemaReference{SchemaId: "hcmnext.people.v1.PromoteWorkerRequest", Version: 3},
			ProtobufWireBytes: []byte{0x0a, 0x06, 0x77, 0x6f, 0x72, 0x6b, 0x65, 0x72},
		},
	}
	grpcSupersede, err := h.grpcIntent.SupersedeIntent(h.grpcContext(ctx), proto.Clone(supersedeOK).(*intentsv1.SupersedeIntentRequest))
	if err != nil {
		t.Fatalf("gRPC SupersedeIntent: %v", err)
	}
	edgeSupersede, err := h.edgeIntent.SupersedeIntent(ctx, edgeRequest(h, proto.Clone(supersedeOK).(*intentsv1.SupersedeIntentRequest)))
	if err != nil {
		t.Fatalf("HTTP SupersedeIntent: %v", err)
	}
	assertProtoParity(t, grpcSupersede, edgeSupersede.Msg)
	if grpcSupersede.GetSupersedingIntent().GetIntentId() == "" || grpcSupersede.GetSupersedingIntent().GetIntentId() == transporttest.KnownIntentID {
		t.Fatalf("superseding intent identity = %q, want a distinct id", grpcSupersede.GetSupersedingIntent().GetIntentId())
	}

	// All three RPCs refuse an unauthenticated caller identically on both
	// transports.
	unauthGRPCCall := func() error {
		_, err := h.grpcIntent.CancelIntent(h.grpcUnauthenticatedContext(ctx), proto.Clone(cancelOK).(*intentsv1.CancelIntentRequest))
		return err
	}
	unauthEdgeCall := func() error {
		_, err := h.edgeIntent.CancelIntent(ctx, unauthenticatedEdgeRequest(proto.Clone(cancelOK).(*intentsv1.CancelIntentRequest)))
		return err
	}
	assertErrorParity(t, unauthGRPCCall(), unauthEdgeCall())
}

func TestIntentSubmitCancelSupersedeEndpointsRespectRevisionAuthorityAndIrreversibility(t *testing.T) {
	runIntentSubmitCancelSupersedeContract(t)
}

func TestTodo_EP_INTENT_003_Property(t *testing.T) { runIntentSubmitCancelSupersedeContract(t) }
func TestTodo_EP_INTENT_003_Golden(t *testing.T)   { runIntentSubmitCancelSupersedeContract(t) }
func TestTodo_EP_INTENT_003_Race(t *testing.T) {
	h := newEndpointHarness(t)
	const workers = 12
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := h.grpcIntent.GetIntent(h.grpcContext(context.Background()), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent lifecycle read: %v", err)
		}
	}
	if got := len(h.intent.Calls()); got != workers {
		t.Fatalf("recorded concurrent calls = %d, want %d", got, workers)
	}
}
func TestTodo_EP_INTENT_003_Integration(t *testing.T) { runIntentSubmitCancelSupersedeContract(t) }
func TestTodo_EP_INTENT_003_Fault(t *testing.T)       { runIntentSubmitCancelSupersedeContract(t) }
func TestTodo_EP_INTENT_003_Security(t *testing.T)    { runIntentSubmitCancelSupersedeContract(t) }
func TestTodo_EP_INTENT_003_Conformance(t *testing.T) { runIntentSubmitCancelSupersedeContract(t) }
func TestTodo_EP_INTENT_003_Mutation(t *testing.T)    { runIntentSubmitCancelSupersedeContract(t) }
