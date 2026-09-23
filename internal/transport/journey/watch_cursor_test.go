package journey_test

// This file is PROTO-007's half of the watch stream: the resumable cursor
// WatchJourney issues per message, the resume it validates before the stream
// opens, the typed status a mid-stream revocation ends with, and the bound
// on how much the handler holds while a consumer is not reading.
//
// watch_test.go already covers the change feed itself (what is emitted and
// when); nothing here re-asserts that. What is asserted here is only the
// stream's position contract, which is a different claim and has a different
// failure mode: a feed that emits the right details in the right order is
// still broken if a reconnect silently restarts it at one.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
)

// testCursorKey signs the cursors these tests read back. It is 32 bytes,
// [streaming.MinKeySize], and authenticates nothing outside this process.
var testCursorKey = []byte("journey-watch-cursor-key-32bytes")

// foreignCursorKey stands for some other deployment's signing key, so a
// cursor minted under it is a forgery from this service's point of view even
// though it is perfectly well formed.
var foreignCursorKey = []byte("a-different-deployments-key-32byt")

// cursorWatchDeps is [watchDeps] with cursor issuance turned on and the
// clock pinned, so a test decides for itself whether a cursor has expired
// rather than racing the wall clock.
func cursorWatchDeps(engine *fakeEngine, now func() time.Time) journey.Dependencies {
	return journey.Dependencies{
		Engine:       engine,
		PollInterval: testPollInterval,
		CursorKey:    testCursorKey,
		CursorTTL:    5 * time.Minute,
		Now:          now,
	}
}

// cursorClock is the pinned instant the cursor tests mint and verify at.
var cursorClock = time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)

func fixedNow() func() time.Time { return func() time.Time { return cursorClock } }

// mustSigner builds the verifier a test reads an issued cursor back with.
func mustCursorSigner(t *testing.T, key []byte) streaming.Signer {
	t.Helper()
	signer, err := streaming.NewSigner(key)
	if err != nil {
		t.Fatalf("streaming.NewSigner: %v", err)
	}
	return signer
}

// setInspectErr points the engine at a canned refusal while a stream is
// open, under the engine's own lock, which is how a revocation that happens
// mid-flight is staged.
func (f *fakeEngine) setInspectErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectErr = err
}

// ---------------------------------------------------------------------------
// Issuance
// ---------------------------------------------------------------------------

// TestWatchIssuesASignedTenantBoundCursorPerMessage is the issuance half of
// the contract. Every message carries its own sequence and its own cursor,
// the sequence counts from one with no gap, and each cursor verifies under
// the composed key for exactly the caller's admitted tenant, this journey's
// stream, and the sequence printed beside it.
//
// The tenant is checked against the credential's tenant rather than against
// anything the request carried, because that binding is the whole reason the
// cursor is signed: a cursor a client could re-scope would resume another
// tenant's stream.
func TestWatchIssuesASignedTenantBoundCursorPerMessage(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, cursorWatchDeps(engine, fixedNow())))
	signer := mustCursorSigner(t, testCursorKey)

	watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})

	var tracker streaming.OrderTracker
	first := watch.recv(5 * time.Second)
	if first.GetSequence() != 1 {
		t.Fatalf("the opening message carries sequence %d, want 1", first.GetSequence())
	}
	if err := tracker.Accept(first.GetSequence()); err != nil {
		t.Fatalf("Accept(%d): %v", first.GetSequence(), err)
	}
	assertCursorNames(t, signer, first.GetCursor(), fixtureTenant, fixtureIntentID, 1)

	engine.setDetail(changedDetail())
	second := watch.recv(5 * time.Second)
	if err := tracker.Accept(second.GetSequence()); err != nil {
		t.Fatalf("the second message left a gap or repeated: %v", err)
	}
	if second.GetSequence() != 2 {
		t.Fatalf("the second message carries sequence %d, want 2", second.GetSequence())
	}
	assertCursorNames(t, signer, second.GetCursor(), fixtureTenant, fixtureIntentID, 2)
	if second.GetCursor() == first.GetCursor() {
		t.Fatal("two messages carry the same cursor; a cursor names one position, not a stream")
	}
}

// assertCursorNames decodes token under signer and asserts the position it
// names, which is the only thing a caller is ever entitled to learn from it
// and the only thing a test should assert about its bytes.
func assertCursorNames(t *testing.T, signer streaming.Signer, token, tenant, intentID string, seq uint64) {
	t.Helper()
	if token == "" {
		t.Fatal("a composition with a signing key issued an empty cursor")
	}
	streamID := journey.WatchStreamIDForTest(intentID)
	c, err := signer.Decode(token, cursorClock, tenant, streamID)
	if err != nil {
		t.Fatalf("the issued cursor does not verify for tenant %q stream %q: %v", tenant, streamID, err)
	}
	if c.Sequence != seq {
		t.Fatalf("the cursor names sequence %d, but the message beside it says %d", c.Sequence, seq)
	}
	if !c.ExpiresAt.After(cursorClock) {
		t.Fatalf("the issued cursor is already expired at issue time: %v", c.ExpiresAt)
	}
}

// TestWatchIssuesNoCursorWhenNoSigningKeyIsComposed pins the documented
// disabled composition: a nil (or too-short) key leaves the response's
// cursor empty and its sequence zero, and the feed itself is unaffected.
// This is what lets a process composed before a key is provisioned answer
// WatchJourney exactly as it did before the fields existed.
func TestWatchIssuesNoCursorWhenNoSigningKeyIsComposed(t *testing.T) {
	for name, key := range map[string][]byte{
		"nil key":      nil,
		"too short":    []byte("short"),
		"one byte shy": make([]byte, streaming.MinKeySize-1),
	} {
		t.Run(name, func(t *testing.T) {
			engine := newFakeEngine()
			deps := journey.Dependencies{Engine: engine, PollInterval: testPollInterval, CursorKey: key}
			if journey.CursorEnabledForTest(deps) {
				t.Fatalf("a %s must leave cursor issuance disabled", name)
			}
			client := dialJourneyClient(startTestServer(t, deps))
			watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})

			msg := watch.recv(5 * time.Second)
			if msg.GetCursor() != "" {
				t.Fatalf("cursor = %q, want empty when no signing key is composed", msg.GetCursor())
			}
			if msg.GetSequence() != 0 {
				t.Fatalf("sequence = %d, want 0 when no signing key is composed", msg.GetSequence())
			}
			if msg.GetDetail().GetDetailDigest() == "" {
				t.Fatal("the feed itself must be unaffected by cursor issuance being off")
			}
		})
	}
}

// TestWatchIgnoresAResumeCursorWhenNoSigningKeyIsComposed is the other half
// of the disabled composition, and the one with a security argument behind
// it: with no key there is nothing to verify against, so a presented cursor
// is ignored rather than refused *or* honoured. Honouring it would let a
// caller choose its own sequence; refusing it would break a client that
// legitimately holds a cursor from before the key was removed.
func TestWatchIgnoresAResumeCursorWhenNoSigningKeyIsComposed(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, PollInterval: testPollInterval}))

	foreign := mintCursor(t, foreignCursorKey, fixtureTenant, fixtureIntentID, 900)
	watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{
		IntentId:     fixtureIntentID,
		ResumeCursor: foreign,
	})
	msg := watch.recv(5 * time.Second)
	if msg.GetSequence() != 0 || msg.GetCursor() != "" {
		t.Fatalf("a keyless composition answered sequence=%d cursor=%q; it must number nothing",
			msg.GetSequence(), msg.GetCursor())
	}
}

// ---------------------------------------------------------------------------
// Resume
// ---------------------------------------------------------------------------

// TestWatchResumesExactlyAfterThePresentedSequence is the reconnect claim in
// process: a second stream opened with the cursor the first stream last
// delivered continues the numbering rather than restarting it, and an
// [streaming.OrderTracker] fed by the client across both streams accepts
// every sequence - which is precisely "no duplicate and no gap".
//
// since_digest is presented alongside, as a real client reconnecting would,
// so the assertion is about the sequence the resumed stream continues at and
// not about the detail it happens to re-send.
func TestWatchResumesExactlyAfterThePresentedSequence(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, cursorWatchDeps(engine, fixedNow())))

	var tracker streaming.OrderTracker

	firstCtx, endFirst := context.WithCancel(testContext(t))
	watch := openWatch(t, firstCtx, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	opening := watch.recv(5 * time.Second)
	if err := tracker.Accept(opening.GetSequence()); err != nil {
		t.Fatalf("Accept(%d): %v", opening.GetSequence(), err)
	}
	engine.setDetail(changedDetail())
	changed := watch.recv(5 * time.Second)
	if err := tracker.Accept(changed.GetSequence()); err != nil {
		t.Fatalf("Accept(%d): %v", changed.GetSequence(), err)
	}
	held := changed.GetDetail().GetDetailDigest()
	endFirst()

	// The reconnect. Nothing about the new stream except the cursor tells it
	// where the old one stopped.
	resumed := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{
		IntentId:     fixtureIntentID,
		SinceDigest:  held,
		ResumeCursor: changed.GetCursor(),
	})
	// Nothing has changed since the reconnect, and the client says it holds
	// the current digest, so the resumed stream is silent until it does.
	resumed.silent(4 * testPollInterval)

	third := fixtureDetail()
	third.Approver = "approver-nakamura"
	engine.setDetail(third)

	next := resumed.recv(5 * time.Second)
	if err := tracker.Accept(next.GetSequence()); err != nil {
		t.Fatalf("the resumed stream duplicated or skipped a sequence: %v", err)
	}
	if want := changed.GetSequence() + 1; next.GetSequence() != want {
		t.Fatalf("the resumed stream continued at sequence %d, want %d (exactly after the presented cursor)",
			next.GetSequence(), want)
	}
	signer := mustCursorSigner(t, testCursorKey)
	assertCursorNames(t, signer, next.GetCursor(), fixtureTenant, fixtureIntentID, next.GetSequence())
}

// retiredCursorKey is the pre-rotation signing key for the rotation test.
// It authenticates nothing outside this process.
var retiredCursorKey = []byte("journey-retired-cursor-key-32byte")

// TestWatchResumesACursorMintedUnderTheRetiredKey is INTAPI-006's
// end-to-end rotation proof: a cursor the server issued before the page
// key rotated still resumes after it, because the retired key verifies
// while a rotation is in progress.
func TestWatchResumesACursorMintedUnderTheRetiredKey(t *testing.T) {
	engine := newFakeEngine()
	preRotation := cursorWatchDeps(engine, fixedNow())
	preRotation.CursorKey = retiredCursorKey
	preClient := dialJourneyClient(startTestServer(t, preRotation))

	firstCtx, endFirst := context.WithCancel(testContext(t))
	watch := openWatch(t, firstCtx, preClient, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	opening := watch.recv(5 * time.Second)
	if opening.GetSequence() != 1 {
		t.Fatalf("the opening message carries sequence %d, want 1", opening.GetSequence())
	}
	held := opening.GetDetail().GetDetailDigest()
	endFirst()

	// The rotation: the server now mints under the active key and verifies
	// the retired one.
	postRotation := cursorWatchDeps(engine, fixedNow())
	postRotation.PreviousCursorKey = retiredCursorKey
	postClient := dialJourneyClient(startTestServer(t, postRotation))

	resumed := openWatch(t, testContext(t), postClient, &journeyv1.WatchJourneyRequest{
		IntentId:     fixtureIntentID,
		SinceDigest:  held,
		ResumeCursor: opening.GetCursor(),
	})
	resumed.silent(4 * testPollInterval)

	third := fixtureDetail()
	third.Approver = "approver-nakamura"
	engine.setDetail(third)

	next := resumed.recv(5 * time.Second)
	if next.GetSequence() != 2 {
		t.Fatalf("the resumed stream continued at sequence %d, want 2 (exactly after the retired-key cursor)", next.GetSequence())
	}
	signer := mustCursorSigner(t, testCursorKey)
	assertCursorNames(t, signer, next.GetCursor(), fixtureTenant, fixtureIntentID, next.GetSequence())
}

// TestWatchWithoutAResumeCursorStartsANewNumberedStream is the control for
// the test above: reconnecting *without* the cursor is not a resume, and the
// server says so by numbering from one again rather than by guessing where
// the client had got to. A server that silently continued would be inventing
// continuity it cannot prove.
func TestWatchWithoutAResumeCursorStartsANewNumberedStream(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, cursorWatchDeps(engine, fixedNow())))

	firstCtx, endFirst := context.WithCancel(testContext(t))
	watch := openWatch(t, firstCtx, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	watch.recv(5 * time.Second)
	endFirst()

	fresh := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	if got := fresh.recv(5 * time.Second).GetSequence(); got != 1 {
		t.Fatalf("a stream opened with no resume cursor started at sequence %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Refusal
// ---------------------------------------------------------------------------

// mintCursor forges a cursor for arbitrary tenant, journey and sequence
// under an arbitrary key. Every argument is deliberately a parameter: what
// the refusal tests need is precisely the ability to present a cursor this
// server would never have issued.
func mintCursor(t *testing.T, key []byte, tenant, intentID string, seq uint64) string {
	t.Helper()
	return mintCursorExpiring(t, key, tenant, intentID, seq, cursorClock.Add(time.Hour))
}

func mintCursorExpiring(t *testing.T, key []byte, tenant, intentID string, seq uint64, expires time.Time) string {
	t.Helper()
	signer := mustCursorSigner(t, key)
	token, err := signer.Encode(streaming.Cursor{
		Tenant:    tenant,
		StreamID:  journey.WatchStreamIDForTest(intentID),
		Sequence:  seq,
		ExpiresAt: expires,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return token
}

// TestWatchRefusesAForgedExpiredOrForeignResumeCursor is the security claim.
// Each refusal is decided before the stream sends anything, so the client
// receives a typed status and never an opening emission it would have to be
// told to discard, and each carries its own condition and reason so a
// legitimate client can tell "reopen without a cursor" from "stop".
func TestWatchRefusesAForgedExpiredOrForeignResumeCursor(t *testing.T) {
	cases := []struct {
		name       string
		cursor     func(t *testing.T) string
		wantCode   envelope.Code
		wantReason string
	}{
		{
			name: "minted under another key",
			cursor: func(t *testing.T) string {
				return mintCursor(t, foreignCursorKey, fixtureTenant, fixtureIntentID, 4)
			},
			wantCode:   envelope.CodePermissionDenied,
			wantReason: "journey.watch.resume_cursor_forged",
		},
		{
			name: "tampered after issue",
			cursor: func(t *testing.T) string {
				token := mintCursor(t, testCursorKey, fixtureTenant, fixtureIntentID, 4)
				return token[:len(token)-1] + differentLastByte(token)
			},
			wantCode:   envelope.CodePermissionDenied,
			wantReason: "journey.watch.resume_cursor_forged",
		},
		{
			name: "another tenant's stream",
			cursor: func(t *testing.T) string {
				return mintCursor(t, testCursorKey, "someone-elses-tenant", fixtureIntentID, 4)
			},
			wantCode:   envelope.CodePermissionDenied,
			wantReason: "journey.watch.resume_cursor_foreign",
		},
		{
			name: "another journey in the same tenant",
			cursor: func(t *testing.T) string {
				return mintCursor(t, testCursorKey, fixtureTenant, "intent-somebody-elses", 4)
			},
			wantCode:   envelope.CodePermissionDenied,
			wantReason: "journey.watch.resume_cursor_foreign",
		},
		{
			name: "expired",
			cursor: func(t *testing.T) string {
				return mintCursorExpiring(t, testCursorKey, fixtureTenant, fixtureIntentID, 4,
					cursorClock.Add(-time.Second))
			},
			wantCode:   envelope.CodeFailedPrecondition,
			wantReason: "journey.watch.resume_cursor_expired",
		},
		{
			name:       "not a cursor at all",
			cursor:     func(*testing.T) string { return "certainly-not-a-cursor" },
			wantCode:   envelope.CodeInvalidArgument,
			wantReason: "journey.watch.resume_cursor_malformed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newFakeEngine()
			client := dialJourneyClient(startTestServer(t, cursorWatchDeps(engine, fixedNow())))

			watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{
				IntentId:     fixtureIntentID,
				ResumeCursor: tc.cursor(t),
			})
			msg, err := watch.next(5 * time.Second)
			if err == nil {
				t.Fatalf("the stream delivered a message (sequence %d) instead of refusing the cursor",
					msg.GetSequence())
			}
			owned := assertOwnedCode(t, err, tc.wantCode)
			if owned.ReasonRef() != tc.wantReason {
				t.Fatalf("ReasonRef() = %q, want %q", owned.ReasonRef(), tc.wantReason)
			}
			if owned.CorrelationID() == "" {
				t.Fatal("a refused cursor must carry a correlation id")
			}
			if owned.EvidenceRef().ID == "" {
				t.Fatal("a refused cursor must carry the authentication evidence reference")
			}
			// The refusal happens before the engine is read at all: a caller
			// presenting somebody else's cursor learns nothing about whether
			// the journey it names exists.
			if got := engine.inspectCount(); got != 0 {
				t.Fatalf("the engine was read %d times behind a refused cursor; it must be read none", got)
			}
		})
	}
}

// differentLastByte returns a byte that is not token's last one, so
// truncating and appending it produces a one-character tamper rather than a
// coincidental no-op.
func differentLastByte(token string) string {
	if len(token) == 0 || token[len(token)-1] != 'A' {
		return "A"
	}
	return "B"
}

// ---------------------------------------------------------------------------
// Revocation
// ---------------------------------------------------------------------------

// TestWatchTerminatesWithATypedStatusWhenAuthorizationIsRevoked pins the
// mid-stream half of the authorization contract. The stream opened for a
// caller the engine allowed; a later poll is refused; the stream ends
// PERMISSION_DENIED under its own reason, distinct from the open-time
// denial, so a revocation is never read as "this caller could never watch
// this journey".
func TestWatchTerminatesWithATypedStatusWhenAuthorizationIsRevoked(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, cursorWatchDeps(engine, fixedNow())))

	watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	watch.recv(5 * time.Second)

	engine.setInspectErr(workspace.ErrDenied)

	msg, err := watch.next(5 * time.Second)
	if err == nil {
		t.Fatalf("the stream kept delivering (sequence %d) after authorization was revoked", msg.GetSequence())
	}
	owned := assertOwnedCode(t, err, envelope.CodePermissionDenied)
	if owned.ReasonRef() != "journey.watch.revoked" {
		t.Fatalf("ReasonRef() = %q, want journey.watch.revoked", owned.ReasonRef())
	}
}

// TestWatchOpenTimeDenialIsNotReportedAsARevocation is the control: a caller
// the engine refuses on the very first read never had an authorized stream,
// so the refusal keeps the ordinary open-time reason. The two reasons are
// what make the pair of events distinguishable at all.
func TestWatchOpenTimeDenialIsNotReportedAsARevocation(t *testing.T) {
	engine := newFakeEngine()
	engine.setInspectErr(workspace.ErrDenied)
	client := dialJourneyClient(startTestServer(t, cursorWatchDeps(engine, fixedNow())))

	watch := openWatch(t, testContext(t), client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
	_, err := watch.next(5 * time.Second)
	owned := assertOwnedCode(t, err, envelope.CodePermissionDenied)
	if owned.ReasonRef() == "journey.watch.revoked" {
		t.Fatal("a caller denied on the opening read was reported as a mid-stream revocation")
	}
	if owned.ReasonRef() != "journey.watch.denied" {
		t.Fatalf("ReasonRef() = %q, want journey.watch.denied", owned.ReasonRef())
	}
}

// ---------------------------------------------------------------------------
// Bounded buffering
// ---------------------------------------------------------------------------

// blockedStream is a WatchJourney server stream whose Send never returns
// until the test releases it. It stands in for a consumer that has stopped
// reading.
type blockedStream struct {
	ctx     context.Context
	release chan struct{}

	mu   sync.Mutex
	sent []*journeyv1.WatchJourneyResponse
}

func (s *blockedStream) Send(msg *journeyv1.WatchJourneyResponse) error {
	s.mu.Lock()
	s.sent = append(s.sent, msg)
	s.mu.Unlock()
	select {
	case <-s.release:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *blockedStream) sendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *blockedStream) Context() context.Context     { return s.ctx }
func (s *blockedStream) SetHeader(metadata.MD) error  { return nil }
func (s *blockedStream) SendHeader(metadata.MD) error { return nil }
func (s *blockedStream) SetTrailer(metadata.MD)       {}
func (s *blockedStream) SendMsg(any) error            { return nil }
func (s *blockedStream) RecvMsg(any) error            { return nil }

// churningEngine answers a different detail on every read, so the watch
// handler has something new to send on every single poll. Without that a
// "the handler stopped producing" assertion would be satisfied trivially by
// a journey that simply never changed.
type churningEngine struct {
	*fakeEngine

	mu    sync.Mutex
	reads int
}

func (e *churningEngine) Inspect(context.Context, string) (workspace.JourneyDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.reads++
	d := fixtureDetail()
	d.Approver = fmt.Sprintf("approver-%d", e.reads)
	return d, nil
}

func (e *churningEngine) readCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reads
}

// TestWatchHoldsAtMostOneMessageWhileASendIsBlocked is the backpressure and
// bounded-resource claim for this handler, stated as the property that
// actually matters: a consumer that stops reading stops the producer.
//
// The handler mints and sends on the same goroutine that polls, so a Send
// that does not return means no further poll, no further mint, and nothing
// queued behind the stalled consumer - exactly one message is outstanding,
// no matter how long the stall lasts or how fast the journey underneath is
// changing. That bound is structural rather than configured, which is why it
// is asserted here rather than as a capacity number.
func TestWatchHoldsAtMostOneMessageWhileASendIsBlocked(t *testing.T) {
	engine := &churningEngine{fakeEngine: newFakeEngine()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	admitted, _, admitErr := transport.Admit(ctx, transport.Config{Verifier: fakeVerifier{}}, transport.AdmissionRequest{
		Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: {"Bearer " + fixtureManagerToken}},
		Method:   "/hcmnext.journey.v1.JourneyService/WatchJourney",
		Kind:     transport.KindGRPC,
	})
	if admitErr != nil {
		t.Fatalf("admit: %v", admitErr)
	}

	stream := &blockedStream{ctx: admitted, release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- journey.WatchJourneyForTest(
			journey.Dependencies{Engine: engine, PollInterval: time.Millisecond, CursorKey: testCursorKey, Now: fixedNow()},
			&journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID},
			stream,
		)
	}()

	// Wait for the one outstanding send, then let the journey churn far past
	// any plausible poll count while nobody is reading.
	waitFor(t, 5*time.Second, func() bool { return stream.sendCount() == 1 })
	readsWhileBlocked := engine.readCount()
	time.Sleep(200 * time.Millisecond) // ~200 poll intervals

	if got := stream.sendCount(); got != 1 {
		t.Fatalf("%d messages were produced behind a blocked consumer, want exactly 1", got)
	}
	if got := engine.readCount(); got != readsWhileBlocked {
		t.Fatalf("the engine was read %d more times while a send was blocked; a stalled consumer must stall the producer",
			got-readsWhileBlocked)
	}

	// Releasing the send lets the loop run again, which proves the stall was
	// backpressure and not a handler that had simply died.
	close(stream.release)
	waitFor(t, 5*time.Second, func() bool { return stream.sendCount() > 1 })
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("the handler ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after its stream context was cancelled")
	}
}

// waitFor polls cond to a deadline rather than sleeping a fixed interval, so
// a slow machine makes the test slower rather than flaky.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition was still false after %v", d)
}

// ---------------------------------------------------------------------------
// Composition knobs
// ---------------------------------------------------------------------------

// TestCursorTTLIsAComposedKnobWithACompiledInDefault mirrors the poll
// interval's own test: the lifetime of an issued cursor is a property of the
// composition, never of the request, and a composition that names none gets
// a default that comfortably outlives one reconnect without outliving the
// stream ceiling several times over.
func TestCursorTTLIsAComposedKnobWithACompiledInDefault(t *testing.T) {
	_, maxLifetime := journey.WatchBoundsForTest()

	defaultTTL := journey.EffectiveCursorTTLForTest(journey.Dependencies{})
	if defaultTTL <= 0 {
		t.Fatalf("the default cursor TTL is %v; it must be positive", defaultTTL)
	}
	if defaultTTL >= maxLifetime {
		t.Fatalf("the default cursor TTL %v must be under the stream ceiling %v", defaultTTL, maxLifetime)
	}
	if got := journey.EffectiveCursorTTLForTest(journey.Dependencies{CursorTTL: 90 * time.Second}); got != 90*time.Second {
		t.Fatalf("a composed cursor TTL of 90s was reported as %v", got)
	}
	if got := journey.EffectiveCursorTTLForTest(journey.Dependencies{CursorTTL: -time.Second}); got != defaultTTL {
		t.Fatalf("a negative cursor TTL was reported as %v, want the default %v", got, defaultTTL)
	}
	if !journey.CursorEnabledForTest(journey.Dependencies{CursorKey: testCursorKey}) {
		t.Fatal("a key of exactly streaming.MinKeySize bytes must enable cursor issuance")
	}
}
