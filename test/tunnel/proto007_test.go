package tunnel_test

// PROTO-007's integration evidence: the resumable-stream contract
// (internal/transport/streaming) as it behaves through the real
// gRPC-over-WebSocket tunnel, plus the long-operation polling contract
// exercised in process beside it.
//
// stream_test.go already proves a server stream survives the tunnel at all.
// What is added here is the claim a stream over a websocket actually needs:
// that the socket can go away mid-stream and the client can come back and
// continue the same numbered run, with no message delivered twice and none
// missed. A reconnect is not an exceptional path for a browser-resident
// client - it is what happens every time a laptop lid closes - and a change
// feed that silently restarts its numbering on reconnect gives a client no
// way to know whether it missed anything while it was gone.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// tunnelCursorKey signs the stream cursors this suite reads back. It is
// [streaming.MinKeySize] bytes and authenticates nothing outside the test
// process.
var tunnelCursorKey = []byte("tunnel-proto007-cursor-key-32byt")

// tunnelWatchPoll is the interval the cursor cell's watch polls at, short so
// a change crosses the socket in milliseconds rather than in the production
// beat. It changes nothing about the handler except how often it looks.
const tunnelWatchPoll = 25 * time.Millisecond

// newCursorCell is [newCellWith] with one difference that cannot be
// expressed through it: the journey service is registered with a cursor
// signing key, so WatchJourney issues the resumable cursor PROTO-007 is
// about.
//
// It composes its own *grpc.Server rather than calling
// transportcell.NewGRPCServer because that constructor deliberately holds no
// opinion about cursor signing - a key is an operator's secret, not a
// composition constant - and this suite has to supply one. Everything else
// is the composition cmd/hcmnext runs: the same grpcserver.NewServer, so the
// same non-optional interceptor chain in both cardinalities, and the same
// transportcell.NewEdgeHandlerWithTunnel mounting the same bridge on the
// same mux.
func newCursorCell(t *testing.T, engine workspace.JourneyEngine) *cell {
	t.Helper()

	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := pgstore.New(pool, pgstore.WithCellID(testCellID),
		pgstore.WithClock(func() time.Time { return baseTime }))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), testTenant); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      testSigningKey,
		Issuer:   testIssuer,
		Audience: testAudience,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}

	composed, err := app.NewCell(app.CellConfig{
		Store:       store,
		RoleAccess:  bootstrapTestRoleAccess(t, pool, testTenant),
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Now:         func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}
	composed.Journey = engine

	grpcServer, err := grpcserver.NewServer(grpcserver.Options{
		Config:   composed.Config,
		Intent:   composed.Service,
		Registry: composed.Service,
	})
	if err != nil {
		t.Fatalf("grpcserver.NewServer: %v", err)
	}
	transportjourney.Register(grpcServer, transportjourney.Dependencies{
		Engine:       engine,
		RoleAccess:   composed.RoleAccess,
		PollInterval: tunnelWatchPoll,
		CursorKey:    tunnelCursorKey,
		CursorTTL:    5 * time.Minute,
	})
	t.Cleanup(grpcServer.Stop)

	handler, err := transportcell.NewEdgeHandlerWithTunnel(composed, grpcServer)
	if err != nil {
		t.Fatalf("edge handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              "user-0191f3c4",
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-tunnel-cursor",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cell{
		t:      t,
		url:    server.URL,
		host:   trimScheme(server.URL),
		client: client,
		token:  "Bearer " + token,
	}
}

// trimScheme is the host form the tunnel dialer needs, spelled once here so
// this file does not restate the "http://" prefix in two places.
func trimScheme(url string) string {
	const prefix = "http://"
	if len(url) > len(prefix) && url[:len(prefix)] == prefix {
		return url[len(prefix):]
	}
	return url
}

// advancingJourneyEngine is [fakeJourneyEngine] with an approver a test can
// move as many times as it likes, so a stream has more than the one change
// stream_test.go needs and a reconnect has something to arrive after.
type advancingJourneyEngine struct {
	*fakeJourneyEngine
}

func newAdvancingJourneyEngine() *advancingJourneyEngine {
	return &advancingJourneyEngine{fakeJourneyEngine: newFakeJourneyEngine()}
}

// moveTo names a new approver, which changes the detail digest and is
// therefore one emission on every open watch.
func (e *advancingJourneyEngine) moveTo(approver string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detail.Approver = approver
}

// TestTodo_PROTO_007_Integration is PROTO-007's integration evidence.
func TestTodo_PROTO_007_Integration(t *testing.T) {
	t.Run("a reconnect through the tunnel resumes with no duplicate and no gap", func(t *testing.T) {
		testTunnelReconnectResumesExactly(t)
	})
	t.Run("the long-operation polling contract holds in process", func(t *testing.T) {
		testLongOperationPollingContract(t)
	})
}

// testTunnelReconnectResumesExactly drives the reconnect claim end to end:
// two websockets, two streams, one numbered run.
//
// The client behaves the way a browser-resident one has to. It tracks every
// sequence it is delivered through a [streaming.OrderTracker] - the same
// check the server-side contract is written against - so "no duplicate and
// no gap" is asserted by the mechanism rather than by counting messages by
// hand. It holds the last cursor and the last digest, drops the socket
// without warning, dials a fresh one, and presents both.
//
// The two are doing different jobs and both are needed. The cursor proves to
// the server that this client is continuing that stream and tells it where
// to number from; the digest tells the server which detail the client
// already has, so the reconnect is not answered with a re-send of it. A
// client presenting only the cursor would resume the numbering and be handed
// content it already had; one presenting only the digest would get correct
// content in a run that silently restarted at one.
func testTunnelReconnectResumesExactly(t *testing.T) {
	engine := newAdvancingJourneyEngine()
	c := newCursorCell(t, engine)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	signer, err := streaming.NewSigner(tunnelCursorKey)
	if err != nil {
		t.Fatalf("streaming.NewSigner: %v", err)
	}
	// The stream id WatchJourney binds its cursors to. It is restated here
	// rather than imported because internal/transport/journey exposes it only
	// to its own tests; a drift between the two is caught immediately, since
	// every Decode below would fail with ErrCursorForeign.
	streamID := "journey/" + tunnelJourneyIntentID

	var tracker streaming.OrderTracker
	var heldCursor, heldDigest string

	// take reads one message off a stream, records its sequence through the
	// tracker (which is what refuses a duplicate or a gap) and remembers the
	// position the client now holds.
	take := func(events <-chan watchEvent, what string) *journeyv1.WatchJourneyResponse {
		t.Helper()
		e := nextWatchEvent(t, events, 20*time.Second)
		if e.err != nil {
			t.Fatalf("%s never arrived: %v", what, e.err)
		}
		if err := tracker.Accept(e.msg.GetSequence()); err != nil {
			t.Fatalf("%s broke the run at sequence %d: %v", what, e.msg.GetSequence(), err)
		}
		cursor, err := signer.Decode(e.msg.GetCursor(), baseTime, testTenant, streamID)
		if err != nil {
			t.Fatalf("%s carries a cursor that does not verify for this tenant and stream: %v", what, err)
		}
		if cursor.Sequence != e.msg.GetSequence() {
			t.Fatalf("%s: the cursor names sequence %d but the message says %d",
				what, cursor.Sequence, e.msg.GetSequence())
		}
		heldCursor = e.msg.GetCursor()
		heldDigest = e.msg.GetDetail().GetDetailDigest()
		return e.msg
	}

	// --- first socket ------------------------------------------------------
	firstConn := c.dial(ctx, c.authorizedUpgrade())
	firstCtx, dropSocket := context.WithCancel(
		metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token))

	firstStream, err := journeyv1.NewJourneyServiceClient(firstConn).
		WatchJourney(firstCtx, &journeyv1.WatchJourneyRequest{IntentId: tunnelJourneyIntentID})
	if err != nil {
		t.Fatalf("open WatchJourney over the tunnel: %v", err)
	}
	firstEvents := drain(firstStream)

	opening := take(firstEvents, "the opening emission")
	if opening.GetSequence() != 1 {
		t.Fatalf("the opening emission carries sequence %d, want 1", opening.GetSequence())
	}

	engine.moveTo("approver-kim")
	second := take(firstEvents, "the first change")
	if second.GetSequence() != 2 {
		t.Fatalf("the first change carries sequence %d, want 2", second.GetSequence())
	}

	// The connection goes away mid-stream, exactly as a closed lid or a lost
	// network would take it, with the client holding sequence 2.
	dropSocket()
	_ = firstConn.Close()

	// --- reconnect ---------------------------------------------------------
	engine.moveTo("approver-nakamura")

	secondConn := c.dial(ctx, c.authorizedUpgrade())
	resumedCtx, endSecond := context.WithCancel(
		metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token))
	defer endSecond()

	resumedStream, err := journeyv1.NewJourneyServiceClient(secondConn).
		WatchJourney(resumedCtx, &journeyv1.WatchJourneyRequest{
			IntentId:     tunnelJourneyIntentID,
			SinceDigest:  heldDigest,
			ResumeCursor: heldCursor,
		})
	if err != nil {
		t.Fatalf("reopen WatchJourney over a fresh socket: %v", err)
	}
	resumedEvents := drain(resumedStream)

	third := take(resumedEvents, "the change that happened while the client was away")
	if third.GetSequence() != 3 {
		t.Fatalf("the resumed stream numbered its first message %d, want 3 - a reconnect must continue the run",
			third.GetSequence())
	}
	if third.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL {
		t.Fatalf("the resumed stream delivered stage %v", third.GetDetail().GetJourney().GetStage())
	}

	// Nothing further changed, so the resumed stream stays silent rather than
	// re-sending anything the client already holds. This is the "no
	// duplicate" half stated as an absence, which no assertion about a
	// message that did arrive can establish.
	select {
	case e, ok := <-resumedEvents:
		if !ok {
			t.Fatal("the resumed stream's reader ended while the journey was merely unchanged")
		}
		if e.err != nil {
			t.Fatalf("the resumed stream ended while the journey was merely unchanged: %v", e.err)
		}
		t.Fatalf("the resumed stream re-sent a message the client already held: sequence %d digest %q",
			e.msg.GetSequence(), e.msg.GetDetail().GetDetailDigest())
	case <-time.After(10 * tunnelWatchPoll):
	}

	// One more change, to prove the resumed stream is a live feed and not a
	// single catch-up message.
	engine.moveTo("approver-okafor")
	fourth := take(resumedEvents, "a change after the resume")
	if fourth.GetSequence() != 4 {
		t.Fatalf("the resumed stream's second message is sequence %d, want 4", fourth.GetSequence())
	}

	if last, ok := tracker.Last(); !ok || last != 4 {
		t.Fatalf("the client tracked through sequence %d across two sockets, want 4", last)
	}
}

// testLongOperationPollingContract exercises PROTO-007's sibling contract:
// the answer a long-running operation gives to a poll, and the discipline a
// poller owes it back. It runs in process rather than over the tunnel
// because it is a contract about successive independent requests, not about
// one open stream - a transport that can carry a unary RPC (which
// tunnel_test.go already proves this one can) carries it by construction.
//
// Four things are asserted per poll: the operation id is stable across the
// whole run, the state is one of the declared ones, the cursor a poll hands
// back verifies for exactly this caller's tenant and this operation, and the
// retry-after obeys the declared bound - positive and inside [Min, Max]
// while the operation is running, and exactly zero once it is terminal,
// because a poller must stop rather than keep asking a finished operation
// how it is doing.
func testLongOperationPollingContract(t *testing.T) {
	signer, err := streaming.NewSigner(tunnelCursorKey)
	if err != nil {
		t.Fatalf("streaming.NewSigner: %v", err)
	}
	const operationID = "op-promotion-execute-7c31"
	bound := streaming.RetryAfterBound{Min: 250 * time.Millisecond, Max: 5 * time.Second}
	now := baseTime

	// The states one execution walks through, and what the server would like
	// the poller to wait between them - deliberately including one value
	// under the floor and one over the ceiling, so the clamp is doing work.
	steps := []struct {
		state   streaming.OperationState
		suggest time.Duration
	}{
		{streaming.OperationPending, 10 * time.Millisecond}, // under the floor
		{streaming.OperationRunning, time.Second},
		{streaming.OperationRunning, time.Hour}, // over the ceiling
		{streaming.OperationSucceeded, 0},
	}

	var seq uint64
	for i, step := range steps {
		seq++
		cursor, err := signer.Encode(streaming.Cursor{
			Tenant:    testTenant,
			StreamID:  "operation/" + operationID,
			Sequence:  seq,
			ExpiresAt: now.Add(10 * time.Minute),
		})
		if err != nil {
			t.Fatalf("poll %d: mint cursor: %v", i, err)
		}
		op := streaming.Operation{ID: operationID, State: step.state, Cursor: cursor}
		if !step.state.Terminal() {
			op.RetryAfter = bound.Clamp(step.suggest)
		}

		if err := op.Validate(); err != nil {
			t.Fatalf("poll %d (%s) is not a well-formed answer: %v", i, step.state, err)
		}
		if op.ID != operationID {
			t.Fatalf("poll %d answered for operation %q, want %q", i, op.ID, operationID)
		}

		decoded, err := signer.Decode(op.Cursor, now, testTenant, "operation/"+operationID)
		if err != nil {
			t.Fatalf("poll %d: the cursor does not verify for this tenant and operation: %v", i, err)
		}
		if decoded.Sequence != seq {
			t.Fatalf("poll %d: cursor names sequence %d, want %d", i, decoded.Sequence, seq)
		}

		switch {
		case step.state.Terminal():
			if op.RetryAfter != 0 {
				t.Fatalf("poll %d is terminal (%s) but asks to be polled again in %v",
					i, step.state, op.RetryAfter)
			}
		default:
			if op.RetryAfter < bound.Min || op.RetryAfter > bound.Max {
				t.Fatalf("poll %d retry-after %v is outside the declared bound [%v, %v]",
					i, op.RetryAfter, bound.Min, bound.Max)
			}
		}
	}

	// A cursor from this operation never resumes another one, which is what
	// stops an operation id from being a bearer token for whatever else the
	// same signer protects.
	token, err := signer.Encode(streaming.Cursor{
		Tenant: testTenant, StreamID: "operation/" + operationID, Sequence: 1,
		ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := signer.Decode(token, now, testTenant, "operation/op-somebody-elses"); err != streaming.ErrCursorForeign {
		t.Fatalf("an operation cursor verified against another operation: %v", err)
	}
	if _, err := signer.Decode(token, now, "someone-elses-tenant", "operation/"+operationID); err != streaming.ErrCursorForeign {
		t.Fatalf("an operation cursor verified for another tenant: %v", err)
	}
	if _, err := signer.Decode(token, now.Add(time.Hour), testTenant, "operation/"+operationID); err != streaming.ErrCursorExpired {
		t.Fatalf("an operation cursor outlived its expiry: %v", err)
	}
}
