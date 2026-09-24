package edge_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// TestGRPCBridgeUnaryParity is the TOOL-008 transport-edge qualification
// fixture and the primary test for the edge selection recorded in this
// package's documentation.
//
// It runs the canonical Protobuf vector through the in-process canonical gRPC
// service and through the selected HTTP edge and requires them to agree on
// everything that matters: the domain result, request presence, the
// server-derived principal, the authorization result, the typed error,
// deadline and cancellation propagation, and the refusal to let
// unauthenticated metadata select trusted context.
//
// RED: any of those diverging fails this test, which is what disqualifies a
// candidate edge.
func TestGRPCBridgeUnaryParity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("identical domain result", func(t *testing.T) {
		vector := transporttest.CanonicalCreateIntentRequest()

		viaGRPC, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), clone(vector))
		if err != nil {
			t.Fatalf("gRPC CreateIntent: %v", err)
		}
		viaEdge, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(vector)))
		if err != nil {
			t.Fatalf("edge CreateIntent: %v", err)
		}

		if !proto.Equal(viaGRPC, viaEdge.Msg) {
			t.Fatalf("domain result differs:\ngrpc: %v\nedge: %v", viaGRPC, viaEdge.Msg)
		}
		if got := viaGRPC.GetIntent().GetIntentId(); got != transporttest.DeterministicIntentID(vector.GetIdempotencyKey()) {
			t.Errorf("intent id = %q, want the deterministic fixture id", got)
		}
	})

	t.Run("server-derived principal, scope and evidence are identical", func(t *testing.T) {
		h.intent.Reset()
		vector := transporttest.CanonicalCreateIntentRequest()

		if _, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), clone(vector)); err != nil {
			t.Fatalf("gRPC CreateIntent: %v", err)
		}
		if _, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(vector))); err != nil {
			t.Fatalf("edge CreateIntent: %v", err)
		}

		calls := h.intent.Calls()
		if len(calls) != 2 {
			t.Fatalf("recorded %d calls, want 2", len(calls))
		}
		viaGRPC, viaEdge := calls[0], calls[1]

		if viaGRPC.Transport != transport.KindGRPC || viaEdge.Transport != transport.KindHTTPEdge {
			t.Fatalf("transports recorded as %q and %q", viaGRPC.Transport, viaEdge.Transport)
		}
		if viaGRPC.TrustedFingerprint != viaEdge.TrustedFingerprint {
			t.Errorf("trusted fingerprint differs:\ngrpc: %s\nedge: %s", viaGRPC.TrustedFingerprint, viaEdge.TrustedFingerprint)
		}
		if viaGRPC.PrincipalFingerprint != viaEdge.PrincipalFingerprint {
			t.Errorf("principal fingerprint differs")
		}
		if viaGRPC.EvidenceID != viaEdge.EvidenceID {
			t.Errorf("evidence id differs: grpc=%q edge=%q", viaGRPC.EvidenceID, viaEdge.EvidenceID)
		}
		if viaGRPC.TenantID != transporttest.Tenant || viaEdge.TenantID != transporttest.Tenant {
			t.Errorf("tenant not server-derived: grpc=%q edge=%q", viaGRPC.TenantID, viaEdge.TenantID)
		}
		if viaGRPC.Purpose != transporttest.PurposeOperations || viaEdge.Purpose != transporttest.PurposeOperations {
			t.Errorf("purpose not server-derived: grpc=%q edge=%q", viaGRPC.Purpose, viaEdge.Purpose)
		}
		if !proto.Equal(viaGRPC.Initiator, viaEdge.Initiator) {
			t.Errorf("initiator differs: grpc=%v edge=%v", viaGRPC.Initiator, viaEdge.Initiator)
		}
		if got := viaGRPC.Initiator.GetPrincipalId(); got != transporttest.Subject {
			t.Errorf("initiator principal = %q, want the authenticated subject", got)
		}
		if got := viaGRPC.Initiator.GetIdentityAssuranceRef(); got != viaGRPC.EvidenceID {
			t.Errorf("initiator assurance ref = %q, want the authentication evidence id", got)
		}
		if !proto.Equal(viaGRPC.ScopeInRequest, viaEdge.ScopeInRequest) {
			t.Errorf("scope differs: grpc=%v edge=%v", viaGRPC.ScopeInRequest, viaEdge.ScopeInRequest)
		}
	})

	t.Run("request presence is preserved on both paths", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			parent *string
		}{
			{"absent optional field", nil},
			{"present empty optional field", proto.String("")},
			{"present populated optional field", proto.String("intent-parent")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h.intent.Reset()
				vector := transporttest.CanonicalCreateIntentRequest()
				vector.ParentIntentId = tc.parent

				if _, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), clone(vector)); err != nil {
					t.Fatalf("gRPC CreateIntent: %v", err)
				}
				if _, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(vector))); err != nil {
					t.Fatalf("edge CreateIntent: %v", err)
				}

				calls := h.intent.Calls()
				if len(calls) != 2 {
					t.Fatalf("recorded %d calls, want 2", len(calls))
				}
				grpcSeen := calls[0].Request.(*intentsv1.CreateIntentRequest).ParentIntentId
				edgeSeen := calls[1].Request.(*intentsv1.CreateIntentRequest).ParentIntentId
				if (grpcSeen == nil) != (tc.parent == nil) {
					t.Errorf("gRPC presence = %v, want present=%v", grpcSeen != nil, tc.parent != nil)
				}
				if (edgeSeen == nil) != (tc.parent == nil) {
					t.Errorf("edge presence = %v, want present=%v", edgeSeen != nil, tc.parent != nil)
				}
				if grpcSeen != nil && edgeSeen != nil && *grpcSeen != *edgeSeen {
					t.Errorf("value differs: grpc=%q edge=%q", *grpcSeen, *edgeSeen)
				}
			})
		}
	})

	t.Run("typed error is identical", func(t *testing.T) {
		request := &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID}

		_, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request))
		if grpcErr == nil {
			t.Fatal("gRPC GetIntent(missing) succeeded, want NOT_FOUND")
		}
		_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request)))
		if edgeErr == nil {
			t.Fatal("edge GetIntent(missing) succeeded, want NOT_FOUND")
		}

		viaGRPC := ownedFromGRPC(t, grpcErr)
		viaEdge := ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeNotFound {
			t.Errorf("owned code = %v, want NOT_FOUND", viaGRPC.Code())
		}
		if viaGRPC.EvidenceRef().ID != "ev:decision:not-found" {
			t.Errorf("evidence = %+v, want the domain decision reference", viaGRPC.EvidenceRef())
		}
		assertOwnedParity(t, "GetIntent(missing)", viaGRPC, viaEdge)
	})

	t.Run("authorization result is identical", func(t *testing.T) {
		request := &intentsv1.GetIntentRequest{
			IntentId: transporttest.KnownIntentID,
			Scope:    &commonv1.ScopeContext{Purpose: transporttest.PurposeUnauthorized},
		}

		_, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request))
		if grpcErr == nil {
			t.Fatal("gRPC GetIntent with an unauthorized purpose succeeded")
		}
		_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request)))
		if edgeErr == nil {
			t.Fatal("edge GetIntent with an unauthorized purpose succeeded")
		}

		viaGRPC := ownedFromGRPC(t, grpcErr)
		viaEdge := ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodePermissionDenied {
			t.Errorf("owned code = %v, want PERMISSION_DENIED", viaGRPC.Code())
		}
		assertOwnedParity(t, "GetIntent(unauthorized purpose)", viaGRPC, viaEdge)
	})

	t.Run("deadline propagates on both paths", func(t *testing.T) {
		request := &intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID}

		for _, tc := range []struct {
			name string
			call func(ctx context.Context) error
		}{
			{"grpc", func(ctx context.Context) error {
				_, err := h.grpcIntent.SimulateIntent(h.grpcContext(ctx), clone(request))
				return err
			}},
			{"edge", func(ctx context.Context) error {
				_, err := h.edgeIntent.SimulateIntent(ctx, edgeRequest(h, clone(request)))
				return err
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				callCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
				defer cancel()

				started := time.Now()
				if err := tc.call(callCtx); err == nil {
					t.Fatal("a blocked call with an expired deadline returned success")
				}
				if elapsed := time.Since(started); elapsed > 3*time.Second {
					t.Fatalf("the call took %s, so the deadline did not reach the server", elapsed)
				}
				record, ok := h.awaitRecord(func(r transport.LogRecord) bool {
					return strings.HasSuffix(r.Method, "SimulateIntent") && !r.Succeeded()
				})
				if !ok {
					t.Fatal("the server never recorded a failed SimulateIntent, so the deadline did not propagate")
				}
				if record.Code != envelope.CodeDeadlineExceeded && record.Code != envelope.CodeUnavailable {
					t.Fatalf("server recorded %v, want a deadline or cancellation outcome", record.Code)
				}
				h.mu.Lock()
				h.records = nil
				h.mu.Unlock()
			})
		}
	})

	t.Run("cancellation propagates on both paths", func(t *testing.T) {
		request := &intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID}

		for _, tc := range []struct {
			name string
			call func(ctx context.Context) error
		}{
			{"grpc", func(ctx context.Context) error {
				_, err := h.grpcIntent.SimulateIntent(h.grpcContext(ctx), clone(request))
				return err
			}},
			{"edge", func(ctx context.Context) error {
				_, err := h.edgeIntent.SimulateIntent(ctx, edgeRequest(h, clone(request)))
				return err
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				callCtx, cancel := context.WithCancel(ctx)
				go func() {
					time.Sleep(75 * time.Millisecond)
					cancel()
				}()
				defer cancel()

				if err := tc.call(callCtx); err == nil {
					t.Fatal("a canceled call returned success")
				}
				record, ok := h.awaitRecord(func(r transport.LogRecord) bool {
					return strings.HasSuffix(r.Method, "SimulateIntent") && !r.Succeeded()
				})
				if !ok {
					t.Fatal("the server never recorded a failed SimulateIntent, so cancellation did not propagate")
				}
				if record.Succeeded() {
					t.Fatalf("server recorded a success for a canceled call")
				}
				h.mu.Lock()
				h.records = nil
				h.mu.Unlock()
			})
		}
	})

	t.Run("unauthenticated metadata cannot select trusted context", func(t *testing.T) {
		request := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}

		_, grpcErr := h.grpcIntent.GetIntent(
			h.grpcContextWithoutCredential(ctx, "x-tenant", "victim-corp", "x-principal", "user-root"),
			clone(request))
		if grpcErr == nil {
			t.Fatal("gRPC accepted unauthenticated trusted-context metadata")
		}
		_, edgeErr := h.edgeIntent.GetIntent(ctx,
			edgeRequestWithoutCredential(clone(request), "x-tenant", "victim-corp", "x-principal", "user-root"))
		if edgeErr == nil {
			t.Fatal("the edge accepted unauthenticated trusted-context metadata")
		}

		viaGRPC := ownedFromGRPC(t, grpcErr)
		viaEdge := ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeInvalidArgument {
			t.Errorf("owned code = %v, want INVALID_ARGUMENT", viaGRPC.Code())
		}
		assertOwnedParity(t, "unauthenticated caller-selected authority", viaGRPC, viaEdge)
	})
}

// TestTodo_TOOL_008_Golden pins the fixture vector's result so that a change
// in either transport, or in admission, has to be an intentional golden
// update rather than a quiet drift.
func TestTodo_TOOL_008_Golden(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vector := transporttest.CanonicalCreateIntentRequest()

	viaGRPC, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), clone(vector))
	if err != nil {
		t.Fatalf("gRPC CreateIntent: %v", err)
	}
	viaEdge, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(vector)))
	if err != nil {
		t.Fatalf("edge CreateIntent: %v", err)
	}

	intent := viaGRPC.GetIntent()
	golden := map[string]string{
		"intent_id":             transporttest.DeterministicIntentID(vector.GetIdempotencyKey()),
		"tenant_id":             transporttest.Tenant,
		"organization_scope_id": transporttest.OrganizationScopeID,
		"purpose":               transporttest.PurposeOperations,
		"initiator.principal":   transporttest.Subject,
		"definition":            transporttest.KnownDefinitionID,
	}
	got := map[string]string{
		"intent_id":             intent.GetIntentId(),
		"tenant_id":             intent.GetTenantId(),
		"organization_scope_id": intent.GetOrganizationScopeId(),
		"purpose":               intent.GetPurpose(),
		"initiator.principal":   intent.GetInitiator().GetPrincipalId(),
		"definition":            intent.GetDefinition().GetIntentTypeId(),
	}
	for key, want := range golden {
		if got[key] != want {
			t.Errorf("golden %s = %q, want %q", key, got[key], want)
		}
	}

	// Byte-level equality of the two responses, not just proto.Equal: the
	// endpoint contract's parity requirement is about the result, and
	// deterministic serialization is the strongest available statement of it.
	grpcBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(viaGRPC)
	if err != nil {
		t.Fatalf("marshal gRPC response: %v", err)
	}
	edgeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(viaEdge.Msg)
	if err != nil {
		t.Fatalf("marshal edge response: %v", err)
	}
	if string(grpcBytes) != string(edgeBytes) {
		t.Errorf("serialized responses differ:\ngrpc: %x\nedge: %x", grpcBytes, edgeBytes)
	}

	// The evidence identifier is derived from the credential, so it is stable
	// across transports and across runs.
	if intent.GetCorrelationId() == "" || !strings.HasPrefix(intent.GetCorrelationId(), "ev:authn:") {
		t.Errorf("correlation id = %q, want the authentication evidence reference", intent.GetCorrelationId())
	}
}

// TestTodo_TOOL_008_Integration runs every published procedure through both
// transports. The qualification claim is about the edge, not about one method,
// so a method that only works on gRPC has to fail something.
func TestTodo_TOOL_008_Integration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	type roundTrip struct {
		name string
		grpc func() (proto.Message, error)
		edge func() (proto.Message, error)
	}

	trips := []roundTrip{
		{
			name: "IntentService.CreateIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.CreateIntent(h.grpcContext(ctx), transporttest.CanonicalCreateIntentRequest())
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, transporttest.CanonicalCreateIntentRequest()))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.GetIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.ListIntents",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.ListIntents(h.grpcContext(ctx), &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 10}})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.ListIntents(ctx, edgeRequest(h, &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 10}}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.SimulateIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.SimulateIntent(h.grpcContext(ctx), &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.SimulateIntent(ctx, edgeRequest(h, &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.ExecuteIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.ExecuteIntent(h.grpcContext(ctx), executeRequest())
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.ExecuteIntent(ctx, edgeRequest(h, executeRequest()))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.SubmitIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.SubmitIntent(h.grpcContext(ctx), submitRequest())
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.SubmitIntent(ctx, edgeRequest(h, submitRequest()))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.CancelIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.CancelIntent(h.grpcContext(ctx), cancelRequest("cancel/reason"))
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.CancelIntent(ctx, edgeRequest(h, cancelRequest("cancel/reason")))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.SupersedeIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.SupersedeIntent(h.grpcContext(ctx), supersedeRequest())
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.SupersedeIntent(ctx, edgeRequest(h, supersedeRequest()))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.ExplainIntent",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.ExplainIntent(h.grpcContext(ctx), &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.ExplainIntent(ctx, edgeRequest(h, &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.ListIntentTimeline",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.ListIntentTimeline(h.grpcContext(ctx), &intentsv1.ListIntentTimelineRequest{IntentId: transporttest.KnownIntentID})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.ListIntentTimeline(ctx, edgeRequest(h, &intentsv1.ListIntentTimelineRequest{IntentId: transporttest.KnownIntentID}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.RecommendIntentAction",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.RecommendIntentAction(h.grpcContext(ctx), &intentsv1.RecommendIntentActionRequest{})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.RecommendIntentAction(ctx, edgeRequest(h, &intentsv1.RecommendIntentActionRequest{}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.GetIntentDeepLink",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.GetIntentDeepLink(h.grpcContext(ctx), &intentsv1.GetIntentDeepLinkRequest{})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.GetIntentDeepLink(ctx, edgeRequest(h, &intentsv1.GetIntentDeepLinkRequest{}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.InspectIntentFields",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.InspectIntentFields(h.grpcContext(ctx), &intentsv1.InspectIntentFieldsRequest{})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.InspectIntentFields(ctx, edgeRequest(h, &intentsv1.InspectIntentFieldsRequest{}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "IntentService.ExportIntentFields",
			grpc: func() (proto.Message, error) {
				return h.grpcIntent.ExportIntentFields(h.grpcContext(ctx), &intentsv1.ExportIntentFieldsRequest{})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeIntent.ExportIntentFields(ctx, edgeRequest(h, &intentsv1.ExportIntentFieldsRequest{}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "RegistryService.ListIntentDefinitions",
			grpc: func() (proto.Message, error) {
				return h.grpcRegistry.ListIntentDefinitions(h.grpcContext(ctx), &registryv1.ListIntentDefinitionsRequest{})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeRegistry.ListIntentDefinitions(ctx, edgeRequest(h, &registryv1.ListIntentDefinitionsRequest{}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "RegistryService.GetIntentDefinition",
			grpc: func() (proto.Message, error) {
				return h.grpcRegistry.GetIntentDefinition(h.grpcContext(ctx), definitionRequest())
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeRegistry.GetIntentDefinition(ctx, edgeRequest(h, definitionRequest()))
				return msgOrNil(res, err)
			},
		},
		{
			name: "RegistryService.ListCapabilities",
			grpc: func() (proto.Message, error) {
				return h.grpcRegistry.ListCapabilities(h.grpcContext(ctx), &registryv1.ListCapabilitiesRequest{})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeRegistry.ListCapabilities(ctx, edgeRequest(h, &registryv1.ListCapabilitiesRequest{}))
				return msgOrNil(res, err)
			},
		},
		{
			name: "RegistryService.GetCapability",
			grpc: func() (proto.Message, error) {
				return h.grpcRegistry.GetCapability(h.grpcContext(ctx), &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID})
			},
			edge: func() (proto.Message, error) {
				res, err := h.edgeRegistry.GetCapability(ctx, edgeRequest(h, &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID}))
				return msgOrNil(res, err)
			},
		},
	}

	if len(trips) != len(edge.Procedures()) {
		t.Fatalf("the fixture covers %d procedures but the edge publishes %d", len(trips), len(edge.Procedures()))
	}

	for _, trip := range trips {
		t.Run(trip.name, func(t *testing.T) {
			viaGRPC, grpcErr := trip.grpc()
			viaEdge, edgeErr := trip.edge()

			if (grpcErr == nil) != (edgeErr == nil) {
				t.Fatalf("outcome differs: grpc err=%v edge err=%v", grpcErr, edgeErr)
			}
			if grpcErr != nil {
				assertOwnedParity(t, trip.name, ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr))
				return
			}
			if !proto.Equal(viaGRPC, viaEdge) {
				t.Fatalf("result differs:\ngrpc: %v\nedge: %v", viaGRPC, viaEdge)
			}
		})
	}
}

// TestTodo_TOOL_008_Security proves the edge cannot be talked past the trusted
// boundary: no credential, a forged credential, an expired credential and
// reserved metadata all fail the same way on both transports.
func TestTodo_TOOL_008_Security(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	request := &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}

	t.Run("no credential", func(t *testing.T) {
		_, grpcErr := h.grpcIntent.GetIntent(h.grpcContextWithoutCredential(ctx), clone(request))
		_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequestWithoutCredential(clone(request)))
		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("an unauthenticated call succeeded: grpc=%v edge=%v", grpcErr, edgeErr)
		}
		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeUnauthenticated {
			t.Errorf("owned code = %v, want UNAUTHENTICATED", viaGRPC.Code())
		}
		assertOwnedParity(t, "no credential", viaGRPC, viaEdge)
	})

	t.Run("forged credential", func(t *testing.T) {
		forged := h.token[:len(h.token)-4] + "AAAA"
		_, grpcErr := h.grpcIntent.GetIntent(
			h.grpcContextWithoutCredential(ctx, transport.AuthorizationMetadataKey, forged), clone(request))
		_, edgeErr := h.edgeIntent.GetIntent(ctx,
			edgeRequestWithoutCredential(clone(request), transport.AuthorizationMetadataKey, forged))
		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("a forged credential succeeded: grpc=%v edge=%v", grpcErr, edgeErr)
		}
		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeUnauthenticated {
			t.Errorf("owned code = %v, want UNAUTHENTICATED", viaGRPC.Code())
		}
		assertOwnedParity(t, "forged credential", viaGRPC, viaEdge)
	})

	t.Run("expired credential", func(t *testing.T) {
		h.clock.Advance(2 * time.Hour)
		defer h.clock.Advance(-2 * time.Hour)

		_, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx), clone(request))
		_, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, clone(request)))
		if grpcErr == nil || edgeErr == nil {
			t.Fatalf("an expired credential succeeded: grpc=%v edge=%v", grpcErr, edgeErr)
		}
		viaGRPC, viaEdge := ownedFromGRPC(t, grpcErr), ownedFromEdge(t, edgeErr)
		if viaGRPC.Code() != envelope.CodeUnauthenticated {
			t.Errorf("owned code = %v, want UNAUTHENTICATED", viaGRPC.Code())
		}
		assertOwnedParity(t, "expired credential", viaGRPC, viaEdge)
	})

	t.Run("no failure discloses why authentication failed", func(t *testing.T) {
		_, err := h.edgeIntent.GetIntent(ctx, edgeRequestWithoutCredential(clone(request)))
		owned := ownedFromEdge(t, err)
		for _, forbidden := range []string{"signature", "issuer", "audience", "expired", "assurance"} {
			if strings.Contains(strings.ToLower(owned.Message()), forbidden) {
				t.Errorf("the authentication failure message discloses %q: %q", forbidden, owned.Message())
			}
		}
	})
}

// TestTodo_TOOL_008_Conformance checks that the edge publishes exactly the
// canonical gRPC surface, under the same names, and that its JSON encoding is
// as usable as its binary one.
func TestTodo_TOOL_008_Conformance(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	t.Run("procedure names are the gRPC method names", func(t *testing.T) {
		published := make(map[string]bool, len(edge.Procedures()))
		for _, p := range edge.Procedures() {
			published[p] = true
		}
		services := []struct {
			full    string
			methods []string
		}{
			{"hcmnext.intents.v1.IntentService", []string{
				"CreateIntent", "GetIntent", "ListIntents", "SimulateIntent", "ExecuteIntent", "SubmitIntent",
				"CancelIntent", "SupersedeIntent", "ExplainIntent", "ListIntentTimeline",
				"RecommendIntentAction", "GetIntentDeepLink", "InspectIntentFields", "ExportIntentFields",
			}},
			{"hcmnext.registry.v1.RegistryService", []string{
				"ListIntentDefinitions", "GetIntentDefinition", "ListCapabilities", "GetCapability",
			}},
		}
		expected := 0
		for _, service := range services {
			for _, method := range service.methods {
				expected++
				want := "/" + service.full + "/" + method
				if !published[want] {
					t.Errorf("the edge does not publish %s", want)
				}
			}
		}
		if len(published) != expected {
			t.Errorf("the edge publishes %d procedures, want exactly the %d canonical methods", len(published), expected)
		}
	})

	t.Run("JSON and binary encodings agree", func(t *testing.T) {
		vector := transporttest.CanonicalCreateIntentRequest()

		binary, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, clone(vector)))
		if err != nil {
			t.Fatalf("edge CreateIntent (binary): %v", err)
		}
		asJSON, err := h.edgeJSON.CreateIntent(ctx, edgeRequest(h, clone(vector)))
		if err != nil {
			t.Fatalf("edge CreateIntent (json): %v", err)
		}
		if !proto.Equal(binary.Msg, asJSON.Msg) {
			t.Errorf("binary and JSON results differ:\nbinary: %v\njson:   %v", binary.Msg, asJSON.Msg)
		}
	})

	t.Run("an unpublished path is not reachable", func(t *testing.T) {
		status, _ := h.postJSON(t, "/hcmnext.intents.v1.IntentService/DeleteEverything", `{}`, nil)
		if status == 200 {
			t.Fatal("an unpublished procedure answered 200")
		}
	})
}

// msgOrNil unwraps a connect response into a proto.Message.
func msgOrNil[T any](res *connect.Response[T], err error) (proto.Message, error) {
	if err != nil {
		return nil, err
	}
	msg, _ := any(res.Msg).(proto.Message)
	return msg, nil
}

// submitRequest returns a SubmitIntent request that satisfies the fixture's
// expected revision.
func submitRequest() *intentsv1.SubmitIntentRequest {
	return &intentsv1.SubmitIntentRequest{
		IdempotencyKey:          "idem-submit-1",
		IntentId:                transporttest.KnownIntentID,
		ProposalRevisionId:      "revision-1",
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
	}
}

// executeRequest returns a valid ExecuteIntent request. The transporttest
// fixture answers it deterministically (mirroring SubmitIntent's own
// expected-instance-version check), so this exercises the round trip itself,
// not the real EXECUTE authority gate (internal/intent/app owns that).
func executeRequest() *intentsv1.ExecuteIntentRequest {
	return &intentsv1.ExecuteIntentRequest{
		IdempotencyKey:          "idem-execute-1",
		IntentId:                transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId: "revision-1",
			Approved:           true,
			ApprovalRef:        "approval-1",
		},
	}
}

// cancelRequest returns a CancelIntent request naming reason.
func cancelRequest(reason string) *intentsv1.CancelIntentRequest {
	return &intentsv1.CancelIntentRequest{
		IdempotencyKey:          "idem-cancel-1",
		IntentId:                transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
		ReasonRef:               reason,
	}
}

// supersedeRequest returns a valid SupersedeIntent request.
func supersedeRequest() *intentsv1.SupersedeIntentRequest {
	return &intentsv1.SupersedeIntentRequest{
		IdempotencyKey:          "idem-supersede-1",
		SupersededIntentId:      transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
		Definition:              &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
		ReasonRef:               "supersede/reason",
	}
}

// definitionRequest returns a valid GetIntentDefinition request.
func definitionRequest() *registryv1.GetIntentDefinitionRequest {
	return &registryv1.GetIntentDefinitionRequest{
		Definition: &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
	}
}
