package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// violation returns the rule references an owned error carries.
func violations(t *testing.T, err error) []string {
	t.Helper()
	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error is not owned: %v", err)
	}
	out := make([]string, 0, len(owned.Detail().GetFieldViolations()))
	for _, v := range owned.Detail().GetFieldViolations() {
		out = append(out, v.GetFieldPath()+":"+v.GetRuleRef())
	}
	return out
}

// TestTodo_CHAT_009_CallerSuppliedPrincipalRefused proves the deprecated
// principal field is refused rather than ignored, on a representative RPC of
// each shape.
func TestTodo_CHAT_009_CallerSuppliedPrincipalRefused(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	forged := &chatv1.Principal{TenantId: "forged", SubjectId: "forged"}

	calls := map[string]func() error{
		"CreateConversation": func() error {
			_, err := s.CreateConversation(ctx, &chatv1.CreateConversationRequest{Principal: forged, IdempotencyKey: "k"})
			return err
		},
		"GetConversation": func() error {
			_, err := s.GetConversation(ctx, &chatv1.GetConversationRequest{Principal: forged, ConversationId: "c"})
			return err
		},
		"SendPost": func() error {
			_, err := s.SendPost(ctx, &chatv1.SendPostRequest{Principal: forged, ConversationId: "c", Body: "hi", IdempotencyKey: "k"})
			return err
		},
		"WatchConversation": func() error {
			_, _, err := s.watch(ctx, &chatv1.WatchConversationRequest{Principal: forged, ConversationId: "c"})
			return err
		},
	}
	for name, call := range calls {
		err := call()
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: code = %v, want InvalidArgument (err=%v)", name, status.Code(err), err)
		}
		got := violations(t, err)
		if len(got) != 1 || got[0] != "principal:"+ruleTrustedRequestBoundary {
			t.Fatalf("%s: violations = %v", name, got)
		}
	}
	// The same RPC without the forged field still works, so the refusal is the
	// field and not the call.
	if _, err := s.GetConversation(ctx, &chatv1.GetConversationRequest{ConversationId: "c"}); err != nil {
		t.Fatalf("clean request refused: %v", err)
	}
}

// TestTodo_CHAT_009_CallerSuppliedAttributionRefused proves neither send path
// lets a caller invent a post's provenance.
func TestTodo_CHAT_009_CallerSuppliedAttributionRefused(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	attr := &chatv1.SourceAttribution{TenantId: "other", ConversationId: "other", PostId: "p", OriginalAuthorId: "somebody"}
	if _, err := s.SendPost(ctx, &chatv1.SendPostRequest{ConversationId: "c", Body: "hi", IdempotencyKey: "k", SourceAttribution: attr}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("send attribution = %v", err)
	}
	if _, err := s.ForwardPost(ctx, &chatv1.ForwardPostRequest{SourceTenantId: "server", SourceConversationId: "c", SourcePostId: "p", DestinationTenantId: "server", DestinationConversationId: "c", IdempotencyKey: "k", SourceAttribution: attr}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("forward attribution = %v", err)
	}
	if v, err := s.ForwardPost(ctx, &chatv1.ForwardPostRequest{SourceTenantId: "server", SourceConversationId: "c", SourcePostId: "p", DestinationTenantId: "server", DestinationConversationId: "c", IdempotencyKey: "k"}); err != nil || v.GetPost().GetId() != "forwarded" {
		t.Fatalf("clean forward = %#v %v", v, err)
	}
}

// TestTodo_CHAT_018_PostBodyBounds pins both published send-path bounds.
func TestTodo_CHAT_018_PostBodyBounds(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"at the character bound", strings.Repeat("a", MaxPostBodyRunes), false},
		{"one character over", strings.Repeat("a", MaxPostBodyRunes+1), true},
		// Multibyte text hits the byte bound first: 1400 three-byte runes are
		// well inside MaxPostBodyRunes and well outside MaxPostBodyBytes.
		{"over the byte bound", strings.Repeat("あ", 1400), true},
		{"multibyte inside both bounds", strings.Repeat("あ", 1000), false},
	} {
		_, err := s.SendPost(ctx, &chatv1.SendPostRequest{ConversationId: "c", Body: tc.body, IdempotencyKey: "k"})
		refused := err != nil
		if refused != tc.want {
			t.Fatalf("%s: refused = %v (err=%v)", tc.name, refused, err)
		}
		if !refused {
			continue
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: code = %v", tc.name, status.Code(err))
		}
		if got := violations(t, err); len(got) != 1 || got[0] != "body:"+ruleBodyBound {
			t.Fatalf("%s: violations = %v", tc.name, got)
		}
	}
}

// TestTodo_CHAT_018_HandlerBoundsRequestBody proves the Connect projection
// refuses an oversized body instead of buffering it.
func TestTodo_CHAT_018_HandlerBoundsRequestBody(t *testing.T) {
	h := NewHandler(Dependencies{Service: transportChatFake{}})
	body := `{"body":"` + strings.Repeat("a", MaxRequestBytes+16) + `"}`
	r := httptest.NewRequest(http.MethodPost, chatv1.ConversationService_SendPost_FullMethodName, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	// connect answers a body past WithReadMaxBytes with RESOURCE_EXHAUSTED,
	// which is HTTP 429 through its own status mapping.
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("oversized body status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "resource_exhausted") {
		t.Fatalf("oversized body was not refused for its size: %s", w.Body.String())
	}
}

// TestTodo_CHAT_018_TransportErrorTable pins the whole projection, including
// the two mappings that were wrong: an unclassified failure is an internal,
// non-retryable condition rather than a retryable UNAVAILABLE, and
// ErrAlreadyExists keeps its own code instead of collapsing into ABORTED.
func TestTodo_CHAT_018_TransportErrorTable(t *testing.T) {
	unknown := errors.New("a programming fault nobody classified")
	for _, tc := range []struct {
		in        error
		want      codes.Code
		retryable bool
	}{
		{chatcore.ErrInvalidArgument, codes.InvalidArgument, false},
		{chatcore.ErrUnauthenticated, codes.Unauthenticated, false},
		{chatcore.ErrPermissionDenied, codes.PermissionDenied, false},
		{chatcore.ErrNotFound, codes.NotFound, false},
		{chatcore.ErrAlreadyExists, codes.AlreadyExists, false},
		{chatcore.ErrConflict, codes.Aborted, true},
		{chatcore.ErrUnavailable, codes.Unavailable, true},
		{context.Canceled, codes.Unavailable, true},
		{context.DeadlineExceeded, codes.DeadlineExceeded, false},
		{unknown, codes.Internal, false},
		{fmt.Errorf("wrapped: %w", chatcore.ErrNotFound), codes.NotFound, false},
	} {
		err := callErr(tc.in)
		if got := status.Code(err); got != tc.want {
			t.Errorf("callErr(%v) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		owned, ok := envelope.As(err)
		if !ok {
			t.Errorf("callErr(%v) is not owned", tc.in)
			continue
		}
		if owned.Retryable() != tc.retryable {
			t.Errorf("callErr(%v) retryable = %v, want %v", tc.in, owned.Retryable(), tc.retryable)
		}
	}
	if callErr(nil) != nil {
		t.Fatal("callErr(nil) is not nil")
	}
	// An already-owned error passes through untouched.
	owned := envelope.New(envelope.CodeFailedPrecondition, "chat.test", "precondition")
	if got := callErr(owned); got != error(owned) {
		t.Fatalf("owned error rewritten: %v", got)
	}
}

// TestTodo_CHAT_018_WatchAfterSequenceIsACursor proves the field that always
// failed now names a starting position, that it travels as itself rather than
// disguised as a signed resume cursor, and that naming two is refused.
func TestTodo_CHAT_018_WatchAfterSequenceIsACursor(t *testing.T) {
	for _, tc := range []struct {
		after      uint64
		resume     string
		wantAfter  uint64
		wantResume string
		fails      bool
	}{
		{0, "", 0, "", false},
		{0, "opaque", 0, "opaque", false},
		{41, "", 41, "", false},
		{41, "opaque", 0, "", true},
	} {
		after, resume, err := watchStart(tc.after, tc.resume)
		if tc.fails {
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("watchStart(%d,%q) = %v, want InvalidArgument", tc.after, tc.resume, err)
			}
			if v := violations(t, err); len(v) != 1 || v[0] != "after_sequence:"+ruleCursorConflict {
				t.Fatalf("violations = %v", v)
			}
			continue
		}
		if err != nil || after != tc.wantAfter || resume != tc.wantResume {
			t.Fatalf("watchStart(%d,%q) = %d, %q, %v", tc.after, tc.resume, after, resume, err)
		}
	}

	// End to end: after_sequence reaches the core as AfterSequence. Rendering it
	// as a decimal string in ResumeCursor is what made the stream reject it as an
	// unverifiable signed token, which is the failure this pins shut.
	rec := &watchRecorder{transportChatFake: &transportChatFake{}}
	s := &server{deps: Dependencies{Service: rec}}
	if _, _, err := s.watch(admittedChatContext(t), &chatv1.WatchConversationRequest{ConversationId: "c", AfterSequence: 7}); err != nil {
		t.Fatal(err)
	}
	if rec.got.AfterSequence != 7 || rec.got.ResumeCursor != "" {
		t.Fatalf("core saw %+v", rec.got)
	}
}

// TestTodo_CHAT_018_FailedWatchSubscribeIsClassified proves a failed subscribe
// leaves this transport owned and named. It used to leave unowned, so the edge
// coerced it into transport.unclassified_failure and the request log recorded
// nothing about the cause.
func TestTodo_CHAT_018_FailedWatchSubscribeIsClassified(t *testing.T) {
	rec := &watchRecorder{transportChatFake: &transportChatFake{}, err: chatcore.ErrInvalidArgument}
	s := &server{deps: Dependencies{Service: rec}}
	_, _, err := s.watch(admittedChatContext(t), &chatv1.WatchConversationRequest{ConversationId: "c"})
	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("watch error is unowned: %v", err)
	}
	if owned.Code() != envelope.CodeInvalidArgument || owned.ReasonRef() != reasonInvalidArgument {
		t.Fatalf("watch error = %v/%v", owned.Code(), owned.ReasonRef())
	}
}

type watchRecorder struct {
	*transportChatFake
	got    chatcore.WatchConversationRequest
	events []chatcore.WatchEvent
	// open, when non-nil, is a channel the recorder returns without ever
	// closing, so a test can observe what the stream loop does with a producer
	// that outlives the caller.
	open chan chatcore.WatchEvent
	// err, when non-nil, is the subscribe failure the recorder answers with.
	err error
}

func (w *watchRecorder) WatchConversation(_ context.Context, r chatcore.WatchConversationRequest) (<-chan chatcore.WatchEvent, error) {
	w.got = r
	if w.err != nil {
		return nil, w.err
	}
	if w.open != nil {
		return w.open, nil
	}
	ch := make(chan chatcore.WatchEvent, len(w.events))
	for _, e := range w.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// TestTodo_CHAT_018_StreamStopsOnCancelAndDrains proves the stream loop no
// longer blocks on a channel nobody reads: a cancelled caller ends the stream,
// and the producer is drained so its goroutine can finish.
func TestTodo_CHAT_018_StreamStopsOnCancelAndDrains(t *testing.T) {
	open := make(chan chatcore.WatchEvent)
	rec := &watchRecorder{transportChatFake: &transportChatFake{}, open: open}
	s := &server{deps: Dependencies{Service: rec}}

	ctx, cancel := context.WithCancel(admittedChatContext(t))
	done := make(chan error, 1)
	go func() {
		done <- s.stream(ctx, &chatv1.WatchConversationRequest{ConversationId: "c"}, func(*chatv1.WatchConversationResponse) error { return nil })
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled stream = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled stream did not return")
	}
	// The drain is what makes this send succeed: before it, a producer still
	// holding the channel would block here forever.
	select {
	case open <- chatcore.WatchEvent{}:
	case <-time.After(5 * time.Second):
		t.Fatal("producer was not drained after cancellation")
	}
	close(open)
}

// TestTodo_CHAT_018_StreamDrainsAfterSendFailure covers the other exit: the
// client went away mid-send.
func TestTodo_CHAT_018_StreamDrainsAfterSendFailure(t *testing.T) {
	open := make(chan chatcore.WatchEvent, 1)
	open <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: 1}}
	rec := &watchRecorder{transportChatFake: &transportChatFake{}, open: open}
	s := &server{deps: Dependencies{Service: rec}}

	sendErr := errors.New("client went away")
	err := s.stream(admittedChatContext(t), &chatv1.WatchConversationRequest{ConversationId: "c"}, func(*chatv1.WatchConversationResponse) error { return sendErr })
	// The failure is owned and named now rather than handed back raw, so the
	// request log can tell a client that hung up from a broken subscribe. The
	// send error itself is kept as the unprojected diagnostic.
	owned, ok := envelope.As(err)
	if !ok || owned.ReasonRef() != reasonStreamSend {
		t.Fatalf("stream = %v, want an owned send failure", err)
	}
	if diagnostic, has := owned.Diagnostic(allowDiagnostics{}); !has || !errors.Is(diagnostic, sendErr) {
		t.Fatalf("diagnostic = %v, want the send error", diagnostic)
	}
	select {
	case open <- chatcore.WatchEvent{}:
	case <-time.After(5 * time.Second):
		t.Fatal("producer was not drained after a send failure")
	}
	close(open)
}

// TestTodo_CHAT_018_StreamCeilingIsDeclared keeps the lifetime ceiling from
// quietly disappearing, and pins it to the same value journey publishes.
func TestTodo_CHAT_018_StreamCeilingIsDeclared(t *testing.T) {
	if watchMaxLifetime != 15*time.Minute {
		t.Fatalf("watchMaxLifetime = %v, want 15m", watchMaxLifetime)
	}
}

// TestTodo_CHAT_018_WatchThroughConnectHandler drives the real server-stream
// handler the HTTP projection registers, rather than the unexported watch.
func TestTodo_CHAT_018_WatchThroughConnectHandler(t *testing.T) {
	admitted := admittedChatContext(t)
	inner := NewHandler(Dependencies{Service: transportChatFake{}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(srv.Close)

	client := connect.NewClient[chatv1.WatchConversationRequest, chatv1.WatchConversationResponse](
		srv.Client(), srv.URL+chatv1.ConversationService_WatchConversation_FullMethodName, connect.WithProtoJSON(),
	)
	stream, err := client.CallServerStream(context.Background(), connect.NewRequest(&chatv1.WatchConversationRequest{TenantId: "server", ConversationId: "c"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	if !stream.Receive() {
		t.Fatalf("no event received: %v", stream.Err())
	}
	if got := stream.Msg(); got.GetResumeCursor() != "cs1" || got.GetEvent().GetPost().GetId() != "p" {
		t.Fatalf("event = %+v", got)
	}
	if stream.Receive() {
		t.Fatalf("stream did not end: %+v", stream.Msg())
	}
	if stream.Err() != nil {
		t.Fatalf("stream ended with %v", stream.Err())
	}
}

// TestTodo_CHAT_018_WatchThroughConnectHandlerRefusesForgedPrincipal proves the
// refusal reaches a real client over the streaming path too.
func TestTodo_CHAT_018_WatchThroughConnectHandlerRefusesForgedPrincipal(t *testing.T) {
	admitted := admittedChatContext(t)
	inner := NewHandler(Dependencies{Service: transportChatFake{}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(admitted))
	}))
	t.Cleanup(srv.Close)

	client := connect.NewClient[chatv1.WatchConversationRequest, chatv1.WatchConversationResponse](
		srv.Client(), srv.URL+chatv1.ConversationService_WatchConversation_FullMethodName, connect.WithProtoJSON(),
	)
	stream, err := client.CallServerStream(context.Background(), connect.NewRequest(&chatv1.WatchConversationRequest{
		TenantId: "server", ConversationId: "c", Principal: &chatv1.Principal{TenantId: "forged"},
	}))
	if err == nil {
		defer func() { _ = stream.Close() }()
		if stream.Receive() {
			t.Fatal("forged principal accepted on the stream")
		}
		err = stream.Err()
	}
	if err == nil {
		t.Fatal("forged principal accepted on the stream")
	}
	// The bare projection hands the owned error back as it is; projecting it
	// onto a connect code is the composition root's job, and
	// TestTodo_CHAT_009_OverlayAdmitsWithTheDecodedMessage in internal/transport/cell
	// asserts that end of it.
	if !strings.Contains(err.Error(), reasonCallerSelectedAuthority) {
		t.Fatalf("stream refusal = %v", err)
	}
}

// TestTodo_CHAT_051_OutboundProjectionCarriesHomeTenants is the golden round
// trip for the fields the converters used to drop. Every one of them is the
// home tenant of a person in a conversation hosted by another company, so
// dropping them made a cross-company post look local.
func TestTodo_CHAT_051_OutboundProjectionCarriesHomeTenants(t *testing.T) {
	created := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

	post := chatcore.Post{
		ID: "p", ConversationID: "c", TenantID: "host", AuthorID: "u", AuthorHomeTenantID: "consumer",
		Body: "hello", Sequence: 4, Revision: 2, ParentID: "root", CreatedAt: created,
		References:        []chatcore.Reference{{Kind: chatcore.PersonMention, TenantID: "consumer", ID: "u2", Display: "Two"}},
		SourceAttribution: &chatcore.SourceAttribution{TenantID: "origin", ConversationID: "oc", PostID: "op", PostRevision: 3, OriginalAuthorID: "author"},
	}
	gotPost := post2(post)
	if gotPost.GetAuthorHomeTenantId() != "consumer" {
		t.Fatalf("post.author_home_tenant_id = %q", gotPost.GetAuthorHomeTenantId())
	}
	if gotPost.GetSourceAttribution().GetOriginalAuthorId() != "author" {
		t.Fatalf("attribution.original_author_id = %q", gotPost.GetSourceAttribution().GetOriginalAuthorId())
	}
	if back := sourceAttributionValue(gotPost.GetSourceAttribution()); back != *post.SourceAttribution {
		t.Fatalf("attribution round trip = %+v, want %+v", back, *post.SourceAttribution)
	}

	if got := reaction(chatcore.Reaction{ConversationID: "c", PostID: "p", TenantID: "host", HomeTenantID: "consumer", SubjectID: "u", Emoji: "+1", CreatedAt: created}); got.GetHomeTenantId() != "consumer" {
		t.Fatalf("reaction.home_tenant_id = %q", got.GetHomeTenantId())
	} else if back := reactionIn(got); back.HomeTenantID != "consumer" {
		t.Fatalf("reaction round trip = %+v", back)
	}

	if got := pin(chatcore.Pin{ConversationID: "c", PostID: "p", TenantID: "host", PinnedBy: "u", PinnedByHomeTenantID: "consumer", Revision: 1, CreatedAt: created}); got.GetPinnedByHomeTenantId() != "consumer" {
		t.Fatalf("pin.pinned_by_home_tenant_id = %q", got.GetPinnedByHomeTenantId())
	} else if back := pinIn(got); back.PinnedByHomeTenantID != "consumer" {
		t.Fatalf("pin round trip = %+v", back)
	}

	if got := readState(chatcore.ReadState{ConversationID: "c", TenantID: "host", SubjectID: "u", HomeTenantID: "consumer", LastReadSequence: 9, Revision: 1}); got.GetHomeTenantId() != "consumer" {
		t.Fatalf("read_state.home_tenant_id = %q", got.GetHomeTenantId())
	} else if back := readStateIn(got); back.HomeTenantID != "consumer" {
		t.Fatalf("read state round trip = %+v", back)
	}

	if got := prefs(chatcore.NotificationPreferences{ConversationID: "c", TenantID: "host", SubjectID: "u", HomeTenantID: "consumer", Muted: true, Revision: 1}); got.GetHomeTenantId() != "consumer" {
		t.Fatalf("preferences.home_tenant_id = %q", got.GetHomeTenantId())
	} else if back := prefsIn(got); back.HomeTenantID != "consumer" {
		t.Fatalf("preferences round trip = %+v", back)
	}
}

// post2 exists only so the golden test reads as a projection call; post is the
// converter under test.
func post2(v chatcore.Post) *chatv1.Post { return post(v) }

// TestTodo_CHAT_051_EnumProjectionsAreExhaustive replaces the concatenated
// enum-name lookups. Every core value must project to a named member, and an
// unknown value must project to UNSPECIFIED rather than to whatever a map miss
// happens to return.
func TestTodo_CHAT_051_EnumProjectionsAreExhaustive(t *testing.T) {
	for _, v := range []chatcore.ConversationKind{chatcore.PublicChannel, chatcore.PrivateChannel, chatcore.Direct, chatcore.Group} {
		if got := kindOut(v); got == chatv1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED {
			t.Fatalf("kindOut(%q) is unspecified", v)
		} else if kind(got) != v {
			t.Fatalf("kind round trip for %q = %q", v, kind(got))
		}
	}
	if kindOut("NOT_A_KIND") != chatv1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED {
		t.Fatal("unknown kind did not project to unspecified")
	}
	for _, v := range []chatcore.MembershipRole{chatcore.Member, chatcore.Manager} {
		if got := roleOut(v); got == chatv1.MembershipRole_MEMBERSHIP_ROLE_UNSPECIFIED || role(got) != v {
			t.Fatalf("roleOut(%q) = %v", v, got)
		}
	}
	if roleOut("NOT_A_ROLE") != chatv1.MembershipRole_MEMBERSHIP_ROLE_UNSPECIFIED {
		t.Fatal("unknown role did not project to unspecified")
	}
	// FROM_JOIN and FULL_HISTORY are exactly the two the concatenated lookup
	// got wrong: the core spells them differently from the proto, so the old
	// projection silently answered UNSPECIFIED for both.
	for _, v := range []chatcore.ReadHistoryFrom{chatcore.NoHistory, chatcore.FromJoin, chatcore.FullHistory} {
		if got := historyOut(v); got == chatv1.ReadHistoryFrom_READ_HISTORY_FROM_UNSPECIFIED || history(got) != v {
			t.Fatalf("historyOut(%q) = %v", v, got)
		}
	}
	if historyOut("NOT_A_VISIBILITY") != chatv1.ReadHistoryFrom_READ_HISTORY_FROM_UNSPECIFIED {
		t.Fatal("unknown visibility did not project to unspecified")
	}
	for _, v := range []chatcore.ConversationEventKind{chatcore.PostCreated, chatcore.PostEdited, chatcore.PostDeleted, chatcore.MembershipChanged, chatcore.ConversationUpdated, chatcore.ReactionChanged, chatcore.PinChanged} {
		if got := eventKindOut(v); got == chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_UNSPECIFIED {
			t.Fatalf("eventKindOut(%q) is unspecified", v)
		}
	}
	if eventKindOut("NOT_AN_EVENT") != chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_UNSPECIFIED {
		t.Fatal("unknown event kind did not project to unspecified")
	}
	for _, v := range []chatcore.ReferenceKind{chatcore.PersonMention, chatcore.AgentMention, chatcore.ConversationMention} {
		if got := referenceKindOut(v); got == chatv1.ReferenceKind_REFERENCE_KIND_UNSPECIFIED || referenceKind(got) != v {
			t.Fatalf("referenceKindOut(%q) = %v", v, got)
		}
	}
	if referenceKindOut("NOT_A_REFERENCE") != chatv1.ReferenceKind_REFERENCE_KIND_UNSPECIFIED {
		t.Fatal("unknown reference kind did not project to unspecified")
	}
}

type bulkService struct {
	*transportChatFake
	pins       int
	candidates int
}

func (b *bulkService) ListPins(context.Context, chatcore.ListPinsRequest) ([]chatcore.Pin, error) {
	out := make([]chatcore.Pin, b.pins)
	for i := range out {
		out[i] = chatcore.Pin{ConversationID: "c", PostID: "p", TenantID: "server"}
	}
	return out, nil
}
func (b *bulkService) SuggestReferences(context.Context, chatcore.SuggestReferencesRequest) ([]chatcore.ReferenceCandidate, error) {
	out := make([]chatcore.ReferenceCandidate, b.candidates)
	for i := range out {
		out[i] = chatcore.ReferenceCandidate{Reference: chatcore.Reference{Kind: chatcore.PersonMention, TenantID: "home", ID: "u"}, Eligible: true}
	}
	return out, nil
}

// TestTodo_CHAT_018_UnboundedResponsesAreClamped covers the two RPCs whose wire
// contract carries no page size.
func TestTodo_CHAT_018_UnboundedResponsesAreClamped(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: &bulkService{transportChatFake: &transportChatFake{}, pins: MaxListPinsPageSize + 25, candidates: MaxReferenceCandidates + 25}}}
	v, err := s.ListPins(ctx, &chatv1.ListPinsRequest{TenantId: "server", ConversationId: "c"})
	if err != nil || len(v.GetPins()) != MaxListPinsPageSize {
		t.Fatalf("pins = %d, want %d (%v)", len(v.GetPins()), MaxListPinsPageSize, err)
	}
	c, err := s.ResolveReferences(ctx, &chatv1.ResolveReferencesRequest{TenantId: "server", ConversationId: "c", Kind: chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, Query: "u"})
	if err != nil || len(c.GetCandidates()) != MaxReferenceCandidates {
		t.Fatalf("candidates = %d, want %d (%v)", len(c.GetCandidates()), MaxReferenceCandidates, err)
	}
}
