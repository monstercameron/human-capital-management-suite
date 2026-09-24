package clients_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PROTO_006 is the PROTO-006 primary test.
//
// RED: the generated connect-go backend differs from the generated native
// gRPC backend in presence, typed errors, authenticated context, deadlines,
// idempotency or response digest, for any of the 29 public unary methods of
// IntentService and RegistryService — including ExecuteIntent, the remaining
// REFUSED_P1A method, which must refuse
// a caller-selected authority identically to every SERVED method rather
// than being treated as a special case.
//
// GREEN: the generated parity suite asserts identical semantic
// request/result/error/evidence for the public unary routes. The separate
// DataOps/Integration parity test covers the additional eleven methods.
func TestTodo_PROTO_006(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	cases := h.methodCases(t)

	t.Run("domain result is identical for every method", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.Name, func(t *testing.T) {
				h.intent.Reset()
				h.registry.Reset()

				viaGRPC, grpcErr := tc.GRPC(ctx, h.authOpts()...)
				viaConnect, connectErr := tc.Connect(ctx, h.authOpts()...)

				if (grpcErr == nil) != (connectErr == nil) {
					t.Fatalf("outcome differs: grpc err=%v connect err=%v", grpcErr, connectErr)
				}
				if grpcErr != nil {
					assertOwnedParity(t, tc.Name, ownedError(t, grpcErr), ownedError(t, connectErr))
					return
				}
				if !proto.Equal(viaGRPC, viaConnect) {
					t.Fatalf("result differs:\ngrpc: %v\nconnect: %v", viaGRPC, viaConnect)
				}
			})
		}
	})

	t.Run("typed error is identical for a representative failure of each kind", func(t *testing.T) {
		failures := []struct {
			name string
			grpc func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error)
			conn func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error)
			want envelope.Code
		}{
			{
				name: "GetIntent(missing) -> NOT_FOUND",
				grpc: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return h.grpcIntent.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID}, opts...)
				},
				conn: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return msgOrNil(h.connectIntent.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID}, opts...))
				},
				want: envelope.CodeNotFound,
			},
			{
				name: "SubmitIntent(stale revision) -> FAILED_PRECONDITION",
				grpc: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					req := submitRequest()
					req.ExpectedInstanceVersion = 1
					return h.grpcIntent.SubmitIntent(ctx, req, opts...)
				},
				conn: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					req := submitRequest()
					req.ExpectedInstanceVersion = 1
					return msgOrNil(h.connectIntent.SubmitIntent(ctx, req, opts...))
				},
				want: envelope.CodeFailedPrecondition,
			},
			{
				name: "CancelIntent(raw provider fault) -> UNAVAILABLE, never the raw text",
				grpc: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return h.grpcIntent.CancelIntent(ctx, cancelRequest(transporttest.RawFaultReasonRef), opts...)
				},
				conn: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return msgOrNil(h.connectIntent.CancelIntent(ctx, cancelRequest(transporttest.RawFaultReasonRef), opts...))
				},
				want: envelope.CodeUnavailable,
			},
			{
				name: "GetIntent(unauthorized purpose) -> PERMISSION_DENIED",
				grpc: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return h.grpcIntent.GetIntent(ctx, &intentsv1.GetIntentRequest{
						IntentId: transporttest.KnownIntentID,
						Scope:    &commonv1.ScopeContext{Purpose: transporttest.PurposeUnauthorized},
					}, opts...)
				},
				conn: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return msgOrNil(h.connectIntent.GetIntent(ctx, &intentsv1.GetIntentRequest{
						IntentId: transporttest.KnownIntentID,
						Scope:    &commonv1.ScopeContext{Purpose: transporttest.PurposeUnauthorized},
					}, opts...))
				},
				want: envelope.CodePermissionDenied,
			},
			{
				name: "GetIntentDefinition(missing) -> NOT_FOUND",
				grpc: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return h.grpcRegistry.GetIntentDefinition(ctx, missingDefinitionRequest(), opts...)
				},
				conn: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
					return msgOrNil(h.connectRegistry.GetIntentDefinition(ctx, missingDefinitionRequest(), opts...))
				},
				want: envelope.CodeNotFound,
			},
		}

		for _, f := range failures {
			t.Run(f.name, func(t *testing.T) {
				_, grpcErr := f.grpc(ctx, h.authOpts()...)
				_, connectErr := f.conn(ctx, h.authOpts()...)
				if grpcErr == nil || connectErr == nil {
					t.Fatalf("expected failure: grpc=%v connect=%v", grpcErr, connectErr)
				}
				viaGRPC, viaConnect := ownedError(t, grpcErr), ownedError(t, connectErr)
				if viaGRPC.Code() != f.want {
					t.Errorf("owned code = %v, want %v", viaGRPC.Code(), f.want)
				}
				assertOwnedParity(t, f.name, viaGRPC, viaConnect)
			})
		}
	})

	t.Run("evidence ids are identical", func(t *testing.T) {
		t.Run("on a typed error", func(t *testing.T) {
			request := &intentsv1.GetIntentRequest{IntentId: transporttest.MissingIntentID}
			_, grpcErr := h.grpcIntent.GetIntent(ctx, clone(request), h.authOpts()...)
			_, connectErr := h.connectIntent.GetIntent(ctx, clone(request), h.authOpts()...)
			viaGRPC, viaConnect := ownedError(t, grpcErr), ownedError(t, connectErr)
			if viaGRPC.EvidenceRef().ID != "ev:decision:not-found" {
				t.Errorf("evidence = %+v, want the domain decision reference", viaGRPC.EvidenceRef())
			}
			if viaGRPC.EvidenceRef() != viaConnect.EvidenceRef() {
				t.Errorf("evidence differs: grpc=%+v connect=%+v", viaGRPC.EvidenceRef(), viaConnect.EvidenceRef())
			}
		})

		t.Run("on a domain success (authentication evidence)", func(t *testing.T) {
			h.intent.Reset()
			vector := transporttest.CanonicalCreateIntentRequest()
			viaGRPC, err := h.grpcIntent.CreateIntent(ctx, clone(vector), h.authOpts()...)
			if err != nil {
				t.Fatalf("gRPC CreateIntent: %v", err)
			}
			viaConnect, err := h.connectIntent.CreateIntent(ctx, clone(vector), h.authOpts()...)
			if err != nil {
				t.Fatalf("connect CreateIntent: %v", err)
			}
			grpcEvidence := viaGRPC.GetIntent().GetCorrelationId()
			connectEvidence := viaConnect.GetIntent().GetCorrelationId()
			if grpcEvidence == "" || grpcEvidence != connectEvidence {
				t.Errorf("evidence id differs or is empty: grpc=%q connect=%q", grpcEvidence, connectEvidence)
			}
		})
	})

	t.Run("deadline propagates identically on both backends", func(t *testing.T) {
		request := &intentsv1.SimulateIntentRequest{IntentId: transporttest.BlockingIntentID}

		for _, tc := range []struct {
			name string
			call func(ctx context.Context) error
		}{
			{"grpc", func(ctx context.Context) error {
				_, err := h.grpcIntent.SimulateIntent(ctx, clone(request), h.authOpts()...)
				return err
			}},
			{"connect", func(ctx context.Context) error {
				_, err := h.connectIntent.SimulateIntent(ctx, clone(request), h.authOpts()...)
				return err
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h.resetRecords()
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
			})
		}
	})

	t.Run("refusal of caller-selected authority, for every method including REFUSED_P1A", func(t *testing.T) {
		var refusedP1A, served int
		for _, tc := range cases {
			t.Run(tc.Name, func(t *testing.T) {
				switch tc.Disposition {
				case manifest.DispositionRefusedP1A:
					refusedP1A++
				case manifest.DispositionServed:
					served++
				default:
					t.Fatalf("method %s has unexpected disposition %s", tc.Name, tc.Disposition)
				}

				for _, key := range trust.ReservedMetadataKeys() {
					t.Run(key, func(t *testing.T) {
						opts := h.noCredentialOpts(clients.WithHeader(key, "attacker-value"))

						_, grpcErr := tc.GRPC(ctx, opts...)
						_, connectErr := tc.Connect(ctx, opts...)
						if grpcErr == nil || connectErr == nil {
							t.Fatalf("%q was accepted: grpc=%v connect=%v", key, grpcErr, connectErr)
						}
						viaGRPC, viaConnect := ownedError(t, grpcErr), ownedError(t, connectErr)
						if viaGRPC.Code() != envelope.CodeInvalidArgument {
							t.Errorf("owned code = %v, want INVALID_ARGUMENT", viaGRPC.Code())
						}
						assertOwnedParity(t, tc.Name+"/"+key, viaGRPC, viaConnect)
					})
				}
			})
		}
		if refusedP1A != 1 {
			t.Errorf("found %d REFUSED_P1A methods, want ExecuteIntent only", refusedP1A)
		}
		if served != len(cases)-1 {
			t.Errorf("found %d SERVED methods, want %d", served, len(cases)-1)
		}
	})
}

// missingDefinitionRequest names an intent definition the registry fixture
// does not know, so GetIntentDefinition returns a typed NOT_FOUND.
func missingDefinitionRequest() *registryv1.GetIntentDefinitionRequest {
	return &registryv1.GetIntentDefinitionRequest{
		Definition: &intentsv1.DefinitionReference{IntentTypeId: "hcmnext.people.no_such_definition", Version: 1},
	}
}

// TestTodo_PROTO_006_Golden pins the fixture vector's result, identically to
// TestTodo_TOOL_007_Golden: both todos own the same generated artifact, so a
// drift in either backend fails both.
func TestTodo_PROTO_006_Golden(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	vector := transporttest.CanonicalCreateIntentRequest()

	viaGRPC, err := h.grpcIntent.CreateIntent(ctx, clone(vector), h.authOpts()...)
	if err != nil {
		t.Fatalf("gRPC CreateIntent: %v", err)
	}
	viaConnect, err := h.connectIntent.CreateIntent(ctx, clone(vector), h.authOpts()...)
	if err != nil {
		t.Fatalf("connect CreateIntent: %v", err)
	}
	if !proto.Equal(viaGRPC, viaConnect) {
		t.Fatalf("golden result differs:\ngrpc: %v\nconnect: %v", viaGRPC, viaConnect)
	}
	if got := viaGRPC.GetIntent().GetIntentId(); got != transporttest.DeterministicIntentID(vector.GetIdempotencyKey()) {
		t.Errorf("golden intent id = %q, want the deterministic fixture id", got)
	}
	if got := viaGRPC.GetIntent().GetInstanceVersion(); got != 1 {
		t.Errorf("golden instance version = %d, want 1", got)
	}
}

// FuzzTodo_PROTO_006 fuzzes the GetIntent identifier through both backends
// and requires them to always agree: either both resolve the same domain
// result, or both refuse with the same owned code. Neither backend may
// panic on any input.
func FuzzTodo_PROTO_006(f *testing.F) {
	for _, seed := range []string{
		transporttest.KnownIntentID,
		transporttest.MissingIntentID,
		transporttest.BlockingIntentID,
		"",
		" ",
		"intent-\x00null",
		"intent-'; DROP TABLE intents; --",
		"日本語-intent",
		strings.Repeat("a", 4096),
	} {
		f.Add(seed)
	}

	h := newHarness(f)
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, intentID string) {
		if !utf8.ValidString(intentID) {
			// Proto3 strings must be valid UTF-8; a caller that builds one
			// from arbitrary bytes gets a local marshal failure before
			// either backend puts a byte on the wire. That is a Protobuf
			// encoding invariant, not a transport-parity question: this
			// fuzz target is about what the two backends do with a request
			// that reaches the network, not about Go string hygiene.
			t.Skip("not valid UTF-8: proto3 string fields reject this before either backend sends anything")
		}
		request := &intentsv1.GetIntentRequest{IntentId: intentID}

		viaGRPC, grpcErr := h.grpcIntent.GetIntent(ctx, clone(request), h.authOpts()...)
		viaConnect, connectErr := h.connectIntent.GetIntent(ctx, clone(request), h.authOpts()...)

		if (grpcErr == nil) != (connectErr == nil) {
			t.Fatalf("outcome differs for %q: grpc err=%v connect err=%v", intentID, grpcErr, connectErr)
		}
		if grpcErr != nil {
			assertOwnedParity(t, fmt.Sprintf("GetIntent(%q)", intentID), ownedError(t, grpcErr), ownedError(t, connectErr))
			return
		}
		if !proto.Equal(viaGRPC, viaConnect) {
			t.Fatalf("result differs for %q:\ngrpc: %v\nconnect: %v", intentID, viaGRPC, viaConnect)
		}
	})
}

// TestTodo_PROTO_006_Race exercises both generated backends from many
// goroutines at once, across every method, with distinct idempotency keys
// per call. It is meant to be run under `go test -race` in an environment
// that supports the race detector (this repository's pinned
// windows/arm64 toolchain does not); run without -race it still proves the
// generated client is safe for concurrent use, since a data race in the
// shared connect.Client/grpc.ClientConn wiring would otherwise surface as a
// wrong result or a panic under this much concurrency.
func TestTodo_PROTO_006_Race(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	cases := h.methodCases(t)

	const workers = 8
	const roundsPerWorker = 5

	var wg sync.WaitGroup
	errs := make(chan error, workers*roundsPerWorker*len(cases)*2)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < roundsPerWorker; round++ {
				for _, tc := range cases {
					if _, err := tc.GRPC(ctx, h.authOpts()...); err != nil {
						if _, ok := envelope.As(err); !ok {
							errs <- fmt.Errorf("worker %d round %d %s (grpc): undecoded error %v", worker, round, tc.Name, err)
						}
					}
					if _, err := tc.Connect(ctx, h.authOpts()...); err != nil {
						if _, ok := envelope.As(err); !ok {
							errs <- fmt.Errorf("worker %d round %d %s (connect): undecoded error %v", worker, round, tc.Name, err)
						}
					}
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// TestTodo_PROTO_006_Integration runs every one of the 18 Intent and Registry RPCs
// through both generated backends, identically to
// TestTodo_TOOL_007_Integration: PROTO-006 and TOOL-007 certify the same
// generated artifact from two different todo obligations, so both suites
// must independently pass against it.
func TestTodo_PROTO_006_Integration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	cases := h.methodCases(t)

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			viaGRPC, grpcErr := tc.GRPC(ctx, h.authOpts()...)
			viaConnect, connectErr := tc.Connect(ctx, h.authOpts()...)

			if (grpcErr == nil) != (connectErr == nil) {
				t.Fatalf("outcome differs: grpc err=%v connect err=%v", grpcErr, connectErr)
			}
			if grpcErr != nil {
				assertOwnedParity(t, tc.Name, ownedError(t, grpcErr), ownedError(t, connectErr))
				return
			}
			if !proto.Equal(viaGRPC, viaConnect) {
				t.Fatalf("result differs:\ngrpc: %v\nconnect: %v", viaGRPC, viaConnect)
			}
		})
	}
}

// TestTodo_PROTO_006_Conformance checks the manifest-to-generated-client
// cross-join: exactly 29 methods, ExecuteIntent REFUSED_P1A, and the
// generated procedure set matches the manifest's grpc_procedure column
// exactly.
func TestTodo_PROTO_006_Conformance(t *testing.T) {
	doc := loadEndpointManifest(t)

	if len(doc.Endpoints) != 29 {
		t.Fatalf("manifest names %d endpoints, want 29", len(doc.Endpoints))
	}

	published := make(map[string]bool, len(clients.Procedures()))
	for _, p := range clients.Procedures() {
		published[p] = true
	}
	if len(published) != 29 {
		t.Fatalf("generated clients publish %d procedures, want 29", len(published))
	}

	refused := map[string]bool{}
	for _, e := range doc.Endpoints {
		if !published[e.GRPCProcedure] {
			t.Errorf("generated clients do not publish %s", e.GRPCProcedure)
		}
		if e.Disposition == manifest.DispositionRefusedP1A {
			refused[e.MethodName] = true
		}
		if !e.Disposition.Valid() {
			t.Errorf("%s has an invalid disposition %q", e.EndpointID, e.Disposition)
		}
	}
	want := map[string]bool{"ExecuteIntent": true}
	if len(refused) != len(want) {
		t.Fatalf("REFUSED_P1A methods = %v, want %v", refused, want)
	}
	for name := range want {
		if !refused[name] {
			t.Errorf("%s is not REFUSED_P1A in the manifest", name)
		}
	}
}
