package journey

import (
	"errors"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Watch bounds. They are constants rather than wire fields because a change
// feed whose polling rate and ceiling are chosen per request is a change
// feed nobody can reason about from either side. [Dependencies.PollInterval]
// exists so a test can shorten the interval; nothing on the wire can.
const (
	// defaultWatchPollInterval is how often the watch re-reads the engine.
	// 750ms is fast enough that a human clicking "approve" in one tab sees
	// the other tab update within a beat, and slow enough that a page left
	// open costs the engine roughly one read per second.
	defaultWatchPollInterval = 750 * time.Millisecond
	// watchMaxLifetime is the ceiling on one stream. A client wanting to
	// watch for longer opens another; nothing in this process holds a stream
	// open indefinitely. Reaching it is not an error: the stream ends with
	// OK, and the client reconnects carrying the digest it now holds.
	//
	// The request's own gRPC deadline and the shared server deadline cap
	// (internal/transport.Config.MaxDeadline, capped by the interceptor
	// before this handler runs) still apply, and whichever bound is nearest
	// is the one that ends the stream.
	watchMaxLifetime = 15 * time.Minute
	// defaultCursorTTL bounds how long a cursor WatchJourney issues stays
	// presentable, when [Dependencies.CursorTTL] names none. It comfortably
	// outlives one poll cycle at the default interval and one reconnect
	// attempt, without outliving [watchMaxLifetime] by so much that a cursor
	// from one stream ceiling is still valid two ceilings later.
	defaultCursorTTL = 5 * time.Minute
)

// watchStreamIDPrefix namespaces the stream ids this service mints cursors
// for, so a cursor issued by WatchJourney can never verify against a stream
// some other service in this process names by the same bare id.
const watchStreamIDPrefix = "journey/"

// watchStreamID is the stream identity a WatchJourney cursor is bound to.
// One journey is one stream: reconnecting to the same intent id continues
// that stream, and a cursor for a different intent id is foreign to it even
// for the same tenant and the same caller.
func watchStreamID(intentID string) string { return watchStreamIDPrefix + intentID }

// cursorSigner returns the signer that mints and verifies WatchJourney's
// cursors, and whether cursor issuance is enabled at all.
//
// Disabled is the documented composition, not a failure: a key shorter than
// [streaming.MinKeySize] - nil included - leaves the response's cursor empty
// and its sequence zero, and makes any resume_cursor a caller presents a
// no-op. That is safe precisely because no cursor was ever issued under the
// absent key, so there is nothing a caller could be replaying, and it is
// what lets a process composed before an operator provisions a signing key
// answer WatchJourney exactly as it did before the field existed.
func (d Dependencies) cursorSigner() (streaming.Signer, bool) {
	if len(d.CursorKey) < streaming.MinKeySize {
		return streaming.Signer{}, false
	}
	signer, err := streaming.NewSignerWithPrevious(d.CursorKey, d.PreviousCursorKey)
	if err != nil {
		// Unreachable: the constructor refuses only a short active key,
		// which the length check above already excluded, or a short
		// retired key, which rotation never configures. Fail closed anyway
		// rather than issue cursors under a signer whose construction
		// complained.
		return streaming.Signer{}, false
	}
	return signer, true
}

// watchProducer builds the cursor producer for one watch stream, or returns
// nil when this composition issues no cursors.
//
// The producer is bound to the caller's own tenant - the one admission
// already derived onto the invocation, never a value the request carries -
// and to this journey's stream id, which is what makes a cursor minted for
// one tenant's stream unusable on another's.
func (s *server) watchProducer(inv *transport.Invocation, intentID string) *streaming.Producer[struct{}] {
	signer, ok := s.deps.cursorSigner()
	if !ok {
		return nil
	}
	return streaming.NewProducer[struct{}](signer, inv.TenantID(), watchStreamID(intentID),
		s.deps.cursorTTL(), s.deps.nowFunc())
}

// resumeCursorError projects a refused resume cursor onto the owned error
// model. Every one of these is decided before the stream sends anything, so
// a caller presenting a bad cursor gets a refusal instead of a stream that
// silently restarted at one - which is the whole reason the cursor is signed
// rather than being a bare sequence number the client could pick.
//
// The four refusals are separate conditions because they call for different
// client behaviour, and collapsing them would tell a legitimate client
// nothing it can act on:
//
//	ErrCursorForged    -> PERMISSION_DENIED  (stop; this token was not issued here)
//	ErrCursorForeign   -> PERMISSION_DENIED  (stop; it names another tenant or journey)
//	ErrCursorExpired   -> FAILED_PRECONDITION (reopen with no cursor)
//	ErrCursorMalformed -> INVALID_ARGUMENT   (the field is not a cursor at all)
//
// None of them says which of the cursor's fields disagreed: a caller that
// did not receive the cursor from this server learns only that it was
// refused.
func resumeCursorError(err error, principal *trust.Principal, inv *transport.Invocation) *envelope.Error {
	var out *envelope.Error
	switch {
	case errors.Is(err, streaming.ErrCursorForged):
		out = envelope.New(envelope.CodePermissionDenied,
			"journey.watch.resume_cursor_forged",
			"the resume cursor was not issued by this service")
	case errors.Is(err, streaming.ErrCursorForeign):
		out = envelope.New(envelope.CodePermissionDenied,
			"journey.watch.resume_cursor_foreign",
			"the resume cursor belongs to another tenant or another journey")
	case errors.Is(err, streaming.ErrCursorExpired):
		out = envelope.New(envelope.CodeFailedPrecondition,
			"journey.watch.resume_cursor_expired",
			"the resume cursor has expired; open the stream again without one")
	default:
		out = envelope.New(envelope.CodeInvalidArgument,
			"journey.watch.resume_cursor_malformed",
			"the resume cursor is not a cursor this service issued").
			WithViolation("resume_cursor", "the value is not a well-formed stream cursor",
				"streaming.ErrCursorMalformed")
	}
	if inv != nil {
		out = out.WithCorrelation(inv.RequestID())
	}
	if principal != nil {
		out = out.WithEvidence(evidence(principal))
	}
	return out
}

// revokedError is the typed status an already-open watch ends with when a
// poll is refused that an earlier poll allowed: the caller's authorization
// to keep receiving this stream was revoked mid-flight.
//
// It is PERMISSION_DENIED, like an open-time denial, but under its own
// reason so the two are never confused - a stream that ended because the
// caller lost access midway is a different operational event from one that
// was never allowed to open, and only the first says anything about a
// revocation having happened. [streaming.Terminate] supplies the typed
// cause, kept as the nested diagnostic no transport surface renders.
func revokedError(cause error, principal *trust.Principal, inv *transport.Invocation) *envelope.Error {
	out := envelope.New(envelope.CodePermissionDenied,
		"journey.watch.revoked",
		"authorization for this stream was revoked; the stream is closed").
		WithDiagnostic(streaming.Terminate(cause))
	if inv != nil {
		out = out.WithCorrelation(inv.RequestID())
	}
	if principal != nil {
		out = out.WithEvidence(evidence(principal))
	}
	return out
}

// sendWatch stamps the next stream position onto resp and sends it.
//
// When cursors are enabled the message carries the sequence the producer
// just advanced to and a cursor freshly signed for exactly that sequence, so
// the pair a client holds after any message is always self-consistent: the
// cursor names the sequence printed beside it. When they are disabled the
// message goes out with sequence zero and no cursor, which is what the proto
// declares that composition emits.
//
// A minting failure ends the stream rather than sending a message with no
// position on it. A client that received an unnumbered message in the middle
// of a numbered run could not tell a gap from a server that simply stopped
// numbering.
func sendWatch(
	stream journeyv1.JourneyService_WatchJourneyServer,
	producer *streaming.Producer[struct{}],
	detail *journeyv1.JourneyDetail,
	principal *trust.Principal,
	inv *transport.Invocation,
) error {
	resp := &journeyv1.WatchJourneyResponse{Detail: detail}
	if producer != nil {
		chunk, err := producer.Next(struct{}{}, false)
		if err != nil {
			return ownedError(err, principal, inv, "watch")
		}
		resp.Sequence = chunk.Sequence
		resp.Cursor = chunk.Cursor
	}
	return stream.Send(resp)
}

// WatchJourney is the server-streaming change feed described on the RPC in
// schema/proto/hcmnext/journey/v1/journey_service.proto. READ_ONLY: it
// performs exactly the read InspectJourney performs, repeatedly.
//
// It emits the current detail once immediately, unless the client's
// since_digest says it already holds exactly that detail, and thereafter one
// message per change: it polls workspace.JourneyEngine.Inspect every
// [Dependencies.PollInterval] and sends only when the detail digest differs
// from the last digest it sent. An unchanged journey produces no traffic.
//
// Each message it sends also carries its position in a resumable stream -
// WatchJourneyResponse.sequence and the opaque, signed
// WatchJourneyResponse.cursor - when [Dependencies.CursorKey] names a
// signing key, and carries neither when it does not. The cursor is bound to
// the caller's admitted tenant and to this journey's stream id and expires
// after [Dependencies.CursorTTL]; presenting it back as
// WatchJourneyRequest.resume_cursor makes the next stream continue the
// sequence instead of restarting it, which is what lets a client that
// reconnected prove for itself that it missed nothing
// (internal/transport/streaming, PROTO-007). A forged, expired or foreign
// cursor is refused before the stream opens ([resumeCursorError]), and an
// authorization lost while the stream is open ends it under its own typed
// status ([revokedError]) rather than under the open-time denial.
//
// Resumption is deliberately two independent halves. resume_cursor
// authenticates and positions the stream; since_digest decides which detail
// the opening emission may skip. A client that reconnects carrying both
// resumes a numbered run and is not re-sent the detail it already holds; a
// client carrying only the digest still gets the older, cursor-free
// behaviour unchanged.
//
// It starts no goroutine. Everything happens on the stream's own handler
// goroutine, driven by one ticker and one timer that are both stopped before
// returning, so a client that disconnects leaves nothing behind: grpc-go
// cancels the stream context, the select observes it, and the handler
// returns. Returning nil there is correct rather than lax - the client that
// cancelled already knows why, and the shared stream interceptor
// (internal/transport/grpcserver.StreamInterceptor) projects the context's
// own error onto the record it emits, so an abandoned stream is not logged
// as a successful one.
//
// Its buffering bound is one message, and it is structural rather than
// configured: the handler mints a chunk and sends it synchronously on the
// same goroutine that polls, so a consumer that stops reading blocks the
// send, which blocks the poll loop, which mints nothing further. Nothing
// queues behind a slow client - grpc-go's own flow control is the only
// window in play, and this handler adds no second one. That is why it uses
// [streaming.Producer] and not [streaming.Buffer]: the Buffer exists for a
// producer that computes ahead of its consumer, and this one cannot.
//
// The request reaches this handler without admission's strict structural
// validation or its trusted-field overwrite: grpc-go decodes a stream's
// request message inside the generated handler, after every interceptor has
// run, so grpcserver.StreamInterceptor admits with no message (see
// grpcserver's admit). That is safe for this request and this request only
// because it carries nothing trusted-field derivation would overwrite and
// nothing validation would refuse that the engine does not refuse itself:
// intent_id is resolved by the engine under the admitted principal (an
// unknown or foreign id is NOT_FOUND), and since_digest is only ever
// compared against a digest this server computed. A streaming request that
// ever carries an initiator, a tenant or any other server-derived field
// needs its own derivation here before it is used.
func (s *server) WatchJourney(req *journeyv1.WatchJourneyRequest, stream journeyv1.JourneyService_WatchJourneyServer) error {
	ctx := stream.Context()
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return ctxErr
	}
	eng, depErr := s.engine(principal, inv, "watch")
	if depErr != nil {
		return depErr
	}
	// Resolved once for the life of the stream: a caller's diagnostics
	// authority is a fact about who they are, not about which poll this is.
	diagAuthorized := s.diagnosticsAuthorized(ctx, principal)

	intentID := req.GetIntentId()
	// sent is the digest the client is known to hold: what it told us it
	// holds to begin with, and thereafter whatever this stream last sent.
	// Every send decision is one comparison against it.
	sent := req.GetSinceDigest()

	// The resume cursor is validated before the stream sends anything at
	// all, and before the engine is read: a caller presenting a cursor for
	// somebody else's tenant or somebody else's journey is refused rather
	// than served an opening emission it then has to be told to discard.
	producer := s.watchProducer(inv, intentID)
	if producer != nil {
		if err := producer.Resume(req.GetResumeCursor()); err != nil {
			return resumeCursorError(err, principal, inv)
		}
	}

	// The first read happens before any waiting. A client that holds
	// nothing, or that holds a digest the engine has already moved past,
	// must not be made to wait for a change that already happened.
	detail, err := eng.Inspect(ctx, intentID)
	if err != nil {
		return ownedError(err, principal, inv, "watch")
	}
	if current := toDetail(detail, diagAuthorized); current.GetDetailDigest() != sent {
		if sendErr := sendWatch(stream, producer, current, principal, inv); sendErr != nil {
			return sendErr
		}
		sent = current.GetDetailDigest()
	}

	ceiling := time.NewTimer(watchMaxLifetime)
	defer ceiling.Stop()
	ticker := time.NewTicker(s.deps.pollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ceiling.C:
			// The declared ceiling, reached. The feed was healthy the whole
			// time, so this is a completed stream, not a failed one.
			return nil
		case <-ticker.C:
			detail, err = eng.Inspect(ctx, intentID)
			if err != nil {
				// A poll that lost the race with the client's own cancel
				// reads a cancelled context and fails for that reason
				// alone; the stream ended because the client went away,
				// which the interceptor already records from the context,
				// not because the engine refused.
				if ctx.Err() != nil {
					return nil
				}
				// A denial on a poll the stream already passed once is a
				// revocation: the caller was authorized when the stream
				// opened and is not any more. It ends the stream under its
				// own typed status rather than the open-time denial, so a
				// mid-stream loss of authorization is never read as "this
				// caller could never watch this journey".
				if errors.Is(err, workspace.ErrDenied) {
					return revokedError(err, principal, inv)
				}
				return ownedError(err, principal, inv, "watch")
			}
			next := toDetail(detail, diagAuthorized)
			if next.GetDetailDigest() == sent {
				continue
			}
			if sendErr := sendWatch(stream, producer, next, principal, inv); sendErr != nil {
				// The client went away mid-send. grpc-go already knows;
				// handing it its own transport error back is the honest
				// answer and never becomes an owned refusal, which would
				// claim the journey was at fault.
				return sendErr
			}
			sent = next.GetDetailDigest()
		}
	}
}
