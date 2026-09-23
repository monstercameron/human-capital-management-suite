package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportchat "github.com/monstercameron/human-capital-management-suite/internal/transport/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// chatOverlayService answers the two RPCs the overlay tests reach, and records
// that it was reached at all.
type chatOverlayService struct {
	chatcore.ConversationService
	reached int
}

func (s *chatOverlayService) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	s.reached++
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: chatcore.Group, Revision: 1}, nil
}
func (s *chatOverlayService) SendPost(_ context.Context, r chatcore.SendPostRequest) (chatcore.Post, error) {
	s.reached++
	return chatcore.Post{ID: "p", ConversationID: r.ConversationID, TenantID: r.TenantID, AuthorID: r.Principal.SubjectID, Body: r.Body, Sequence: 1, Revision: 1}, nil
}

// chatOverlayFixture mounts the real chat HTTP overlay behind a 404 edge and
// returns the server, a valid bearer token and the service the overlay calls.
func chatOverlayFixture(t *testing.T) (*httptest.Server, string, *chatOverlayService) {
	t.Helper()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "chat-overlay", nil)
	service := &chatOverlayService{}
	edge := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	server := httptest.NewServer(chatHTTPOverlay(edge, cfg, service))
	t.Cleanup(server.Close)
	return server, token, service
}

// TestTodo_CHAT_009_OverlayAdmitsWithTheDecodedMessage is the regression for
// the overlay calling transport.Admit with no Message.
//
// Without the message, admission skipped per-method structural validation and
// the trusted-context overwrite entirely, so the chat HTTP projection was the
// one surface in the process where a request's shape was never checked. Each
// case below fails if the message does not reach admission.
func TestTodo_CHAT_009_OverlayAdmitsWithTheDecodedMessage(t *testing.T) {
	server, token, service := chatOverlayFixture(t)
	get := connect.NewClient[chatv1.GetConversationRequest, chatv1.GetConversationResponse](
		server.Client(), server.URL+chatv1.ConversationService_GetConversation_FullMethodName, connect.WithProtoJSON(),
	)
	send := connect.NewClient[chatv1.SendPostRequest, chatv1.SendPostResponse](
		server.Client(), server.URL+chatv1.ConversationService_SendPost_FullMethodName, connect.WithProtoJSON(),
	)
	authorized := func(msg any) {
		switch r := msg.(type) {
		case *connect.Request[chatv1.GetConversationRequest]:
			r.Header().Set(transport.AuthorizationMetadataKey, token)
		case *connect.Request[chatv1.SendPostRequest]:
			r.Header().Set(transport.AuthorizationMetadataKey, token)
		}
	}

	// A well-formed request still works, so the overlay did not simply start
	// refusing everything.
	ok := connect.NewRequest(&chatv1.GetConversationRequest{ConversationId: "c"})
	authorized(ok)
	if _, err := get.CallUnary(context.Background(), ok); err != nil {
		t.Fatalf("clean request: %v", err)
	}
	if service.reached != 1 {
		t.Fatalf("service reached %d times", service.reached)
	}

	// A caller-supplied trusted field is refused on a chat RPC.
	forged := connect.NewRequest(&chatv1.GetConversationRequest{ConversationId: "c", Principal: &chatv1.Principal{TenantId: "forged", SubjectId: "forged"}})
	authorized(forged)
	if _, err := get.CallUnary(context.Background(), forged); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("forged principal = %v (code %v)", err, connect.CodeOf(err))
	}

	// And the shared structural validation runs: a string past the transport's
	// own bound is refused before the handler, which is only possible if the
	// decoded message reached transport.Admit.
	oversized := connect.NewRequest(&chatv1.SendPostRequest{ConversationId: "c", IdempotencyKey: "k", Body: strings.Repeat("a", transportchat.MaxPostBodyBytes+1)})
	authorized(oversized)
	if _, err := send.CallUnary(context.Background(), oversized); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("oversized body = %v (code %v)", err, connect.CodeOf(err))
	}
	if service.reached != 1 {
		t.Fatalf("a refused request reached the service (%d calls)", service.reached)
	}
}

// TestTodo_CHAT_009_OverlayRefusesUnauthenticatedBeforeDecoding keeps the
// ordering the canonical edge has: an anonymous caller is refused by
// authentication, never by a validation message that would tell it whether its
// body was well formed.
func TestTodo_CHAT_009_OverlayRefusesUnauthenticatedBeforeDecoding(t *testing.T) {
	server, _, service := chatOverlayFixture(t)
	get := connect.NewClient[chatv1.GetConversationRequest, chatv1.GetConversationResponse](
		server.Client(), server.URL+chatv1.ConversationService_GetConversation_FullMethodName, connect.WithProtoJSON(),
	)
	// Malformed and unauthenticated at once: the answer must be the
	// authentication one.
	_, err := get.CallUnary(context.Background(), connect.NewRequest(&chatv1.GetConversationRequest{
		Principal: &chatv1.Principal{TenantId: "forged"},
	}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous request = %v (code %v)", err, connect.CodeOf(err))
	}
	if service.reached != 0 {
		t.Fatal("an unauthenticated request reached the service")
	}
}

// TestTodo_CHAT_009_OverlayLeavesOtherRoutesAlone proves the overlay still
// hands everything outside the chat prefix to the edge it wraps, and that a
// nil service leaves the edge untouched.
func TestTodo_CHAT_009_OverlayLeavesOtherRoutesAlone(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "chat-overlay", nil)
	edge := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })

	if got := chatHTTPOverlay(edge, cfg, nil); got == nil {
		t.Fatal("nil service produced a nil handler")
	} else {
		w := httptest.NewRecorder()
		got.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/anything", nil))
		if w.Code != http.StatusTeapot {
			t.Fatalf("nil service replaced the edge: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	chatHTTPOverlay(edge, cfg, &chatOverlayService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/anything", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("non-chat route = %d, want the wrapped edge", w.Code)
	}
}
