package chatresource

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type apiService struct {
	chatcore.ConversationService
	sent          []chatcore.SendPostRequest
	read          []chatcore.ListPostsRequest
	readErr       error
	got           []chatcore.GetConversationRequest
	getMethod     string
	getVerified   bool
	watch         []chatcore.WatchConversationRequest
	watchMethod   string
	watchVerified bool
	watchEvents   chan chatcore.WatchEvent
	watchFailures chan error
	watchErr      error
	watchDelay    time.Duration
	sendErr       error
	getErr        error
}

func (s *apiService) WatchConversationWithErrors(ctx context.Context, r chatcore.WatchConversationRequest) (<-chan chatcore.WatchEvent, <-chan error, error) {
	if s.watchDelay > 0 {
		select {
		case <-time.After(s.watchDelay):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	s.watch = append(s.watch, r)
	if invocation, ok := transport.InvocationFromContext(ctx); ok {
		s.watchMethod = invocation.Method()
	}
	if verified, ok := trust.FromContext(ctx); ok {
		s.watchVerified = verified.Subject() == r.Principal.SubjectID && verified.Tenant().String() == r.TenantID
	}
	if s.watchErr != nil {
		return nil, nil, s.watchErr
	}
	if s.watchEvents == nil {
		s.watchEvents = make(chan chatcore.WatchEvent)
	}
	if s.watchFailures == nil {
		s.watchFailures = make(chan error)
	}
	return s.watchEvents, s.watchFailures, nil
}

func (s *apiService) GetConversation(ctx context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	s.got = append(s.got, r)
	if invocation, ok := transport.InvocationFromContext(ctx); ok {
		s.getMethod = invocation.Method()
	}
	if verified, ok := trust.FromContext(ctx); ok {
		s.getVerified = verified.Subject() == r.Principal.SubjectID && verified.Tenant().String() == r.TenantID
	}
	if s.getErr != nil {
		return chatcore.Conversation{}, s.getErr
	}
	at := time.Date(2026, time.September, 22, 12, 34, 56, 0, time.UTC)
	return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Kind: chatcore.PublicChannel, Name: "Operations", OwnerID: "owner-a", Revision: 7, MemberCount: 3, LastActivityAt: &at}, nil
}

func (s *apiService) SendPost(_ context.Context, r chatcore.SendPostRequest) (chatcore.Post, error) {
	s.sent = append(s.sent, r)
	if s.sendErr != nil {
		return chatcore.Post{}, s.sendErr
	}
	return chatcore.Post{ID: "post-1", TenantID: r.TenantID, ConversationID: r.ConversationID, AuthorID: r.Principal.SubjectID, Body: r.Body, Sequence: 3, Revision: 1}, nil
}

func TestTodo_CHAT_009_RateBoundary(t *testing.T) {
	s := &apiService{sendErr: chatadmission.ErrOverloaded}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindIntegration), s).ServeHTTP(w, apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", `{"body":"hello"}`))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" || len(s.sent) != 1 {
		t.Fatalf("status=%d retry=%q calls=%d", w.Code, w.Header().Get("Retry-After"), len(s.sent))
	}
}
func (s *apiService) ListPosts(_ context.Context, r chatcore.ListPostsRequest) (chatcore.ListPostsResponse, error) {
	s.read = append(s.read, r)
	if s.readErr != nil {
		return chatcore.ListPostsResponse{}, s.readErr
	}
	return chatcore.ListPostsResponse{Posts: []chatcore.Post{{ID: "post-1", Body: "hello"}}, NextCursor: "next"}, nil
}

func apiConfig(t *testing.T, kind trust.SubjectKind) transport.Config {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "agent-a", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })}
}

func apiRequest(method, url, body string) *http.Request {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer opaque-machine-token")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "unique-1")
	return r
}

func TestTodo_CHAT_009_ResourcePostAndRead(t *testing.T) {
	s := &apiService{}
	h := NewHandler(apiConfig(t, trust.SubjectKindAgent), s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", `{"body":"hello","parent_id":"source-post"}`))
	if w.Code != http.StatusCreated || len(s.sent) != 1 || s.sent[0].TenantID != "tenant-a" || s.sent[0].ConversationID != "channel-a" || s.sent[0].Principal.SubjectID != "agent-a" || s.sent[0].IdempotencyKey != "unique-1" || s.sent[0].ParentID != "source-post" || !bytes.Contains(w.Body.Bytes(), []byte(`"author_id":"agent-a"`)) {
		t.Fatalf("post response=%d %s sent=%+v", w.Code, w.Body.String(), s.sent)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/posts?page_size=2", ""))
	if w.Code != http.StatusOK || len(s.read) != 1 || s.read[0].Page.PageSize != 2 || s.read[0].ConversationID != "channel-a" || !bytes.Contains(w.Body.Bytes(), []byte(`"next_cursor":"next"`)) {
		t.Fatalf("read response=%d %s read=%+v", w.Code, w.Body.String(), s.read)
	}
}

func TestTodo_CHAT_009_Security_ResourceRejectsHumanAndMalformedBody(t *testing.T) {
	s := &apiService{}
	h := NewHandler(apiConfig(t, trust.SubjectKindHuman), s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", `{"body":"hello"}`))
	if w.Code != http.StatusForbidden || len(s.sent) != 0 {
		t.Fatalf("human post=%d sent=%d", w.Code, len(s.sent))
	}
	h = NewHandler(apiConfig(t, trust.SubjectKindAgent), s)
	for name, body := range map[string]string{
		"unknown authority": `{"body":"hello","tenant_id":"other"}`,
		"trailing object":   `{"body":"hello"}{}`,
		"oversized tail":    `{"body":"hello"}` + strings.Repeat(" ", 9000),
		"oversized body":    `{"body":"` + strings.Repeat("x", 4100) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", body))
			if w.Code != http.StatusBadRequest || len(s.sent) != 0 {
				t.Fatalf("status=%d sent=%d", w.Code, len(s.sent))
			}
		})
	}
	w = httptest.NewRecorder()
	r := apiRequest(http.MethodPost, "/v1/conversations/channel-a/posts", `{"body":"hello"}`)
	r.Header.Set("Content-Type", "application/jsonX")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType || len(s.sent) != 0 {
		t.Fatalf("content type status=%d sent=%d", w.Code, len(s.sent))
	}
}

func TestTodo_CHAT_009_Conformance_ConversationMetadata(t *testing.T) {
	s := &apiService{}
	h := NewHandler(apiConfig(t, trust.SubjectKindAgent), s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a", ""))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if len(s.got) != 1 || s.got[0].TenantID != "tenant-a" || s.got[0].Principal.TenantID != "tenant-a" || s.got[0].Principal.SubjectID != "agent-a" || s.got[0].ConversationID != "channel-a" || s.getMethod != "/hcmnext.chat.v1.ConversationService/GetConversation" || !s.getVerified {
		t.Fatalf("get calls=%+v", s.got)
	}
	want := `{"id":"channel-a","tenant_id":"tenant-a","kind":"PUBLIC_CHANNEL","name":"Operations","owner_id":"owner-a","revision":7,"archived":false,"joined":false,"member_count":3,"last_activity_at":"2026-09-22T12:34:56Z"}` + "\n"
	if w.Body.String() != want || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("body=%q want=%q", w.Body.String(), want)
	}
}

func TestTodo_CHAT_009_Security_ConversationMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   trust.SubjectKind
		url    string
		err    error
		status int
		code   string
		calls  int
	}{
		{"invalid id", trust.SubjectKindAgent, "/v1/conversations/" + strings.Repeat("x", 201), nil, http.StatusBadRequest, "chat.invalid_request", 0},
		{"revoked installation", trust.SubjectKindIntegration, "/v1/conversations/channel-a", chatcore.ErrPermissionDenied, http.StatusNotFound, "chat.not_found", 1},
		{"foreign tenant hidden", trust.SubjectKindAgent, "/v1/conversations/channel-a", chatcore.ErrNotFound, http.StatusNotFound, "chat.not_found", 1},
		{"human", trust.SubjectKindHuman, "/v1/conversations/channel-a", nil, http.StatusForbidden, "chat.machine_identity_required", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &apiService{getErr: tc.err}
			w := httptest.NewRecorder()
			NewHandler(apiConfig(t, tc.kind), s).ServeHTTP(w, apiRequest(http.MethodGet, tc.url, ""))
			if w.Code != tc.status || len(s.got) != tc.calls || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != `{"error":{"code":"`+tc.code+`"}}`+"\n" {
				t.Fatalf("status=%d calls=%d body=%q headers=%v", w.Code, len(s.got), w.Body.String(), w.Header())
			}
		})
	}
}

func TestTodo_CHAT_009_Conformance_EventPull(t *testing.T) {
	events := make(chan chatcore.WatchEvent, 2)
	events <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: 8, Revision: 2, Post: &chatcore.Post{ID: "post-8", Body: "hello"}}, ResumeCursor: "signed-8"}
	events <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostEdited, Sequence: 9, Revision: 3, Post: &chatcore.Post{ID: "post-8", Body: "updated"}}, ResumeCursor: "signed-9"}
	s := &apiService{watchEvents: events, watchFailures: make(chan error)}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindIntegration), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?resume_cursor=signed-7&max_events=2&wait_ms=100", ""))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	if len(s.watch) != 1 || s.watch[0].TenantID != "tenant-a" || s.watch[0].Principal.SubjectID != "agent-a" || s.watch[0].ConversationID != "channel-a" || s.watch[0].AfterSequence != 0 || s.watch[0].ResumeCursor != "signed-7" || s.watchMethod != "/hcmnext.chat.v1.ConversationService/WatchConversation" || !s.watchVerified || len(s.got) != 1 {
		t.Fatalf("watch=%+v method=%q verified=%t", s.watch, s.watchMethod, s.watchVerified)
	}
	var body struct {
		Events []map[string]any `json:"events"`
		Cursor string           `json:"resume_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Events) != 2 || body.Cursor != "signed-9" || body.Events[0]["kind"] != "POST_CREATED" || body.Events[1]["kind"] != "POST_EDITED" {
		t.Fatalf("body=%s err=%v", w.Body.String(), err)
	}
	post, ok := body.Events[0]["post"].(map[string]any)
	if !ok || post["id"] != "post-8" || post["body"] != "hello" {
		t.Fatalf("post=%v", body.Events[0]["post"])
	}
}

func TestTodo_CHAT_009_Security_EventPull(t *testing.T) {
	for _, tc := range []struct {
		name   string
		url    string
		status int
		code   string
	}{
		{"long id", "/v1/conversations/" + strings.Repeat("x", 201) + "/events", http.StatusBadRequest, "chat.invalid_request"},
		{"bad cursor", "/v1/conversations/channel-a/events?resume_cursor=" + strings.Repeat("x", 2049), http.StatusBadRequest, "chat.invalid_cursor"},
		{"after sequence rejected", "/v1/conversations/channel-a/events?after_sequence=1", http.StatusBadRequest, "chat.invalid_cursor"},
		{"zero offset rejected", "/v1/conversations/channel-a/events?after_sequence=0", http.StatusBadRequest, "chat.invalid_cursor"},
		{"too many", "/v1/conversations/channel-a/events?max_events=51", http.StatusBadRequest, "chat.invalid_request"},
		{"too long", "/v1/conversations/channel-a/events?wait_ms=5001", http.StatusBadRequest, "chat.invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &apiService{}
			w := httptest.NewRecorder()
			NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, tc.url, ""))
			if w.Code != tc.status || len(s.watch) != 0 || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != `{"error":{"code":"`+tc.code+`"}}`+"\n" {
				t.Fatalf("status=%d calls=%d body=%q", w.Code, len(s.watch), w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"revoked", chatcore.ErrPermissionDenied, http.StatusNotFound, "chat.not_found"},
		{"foreign tenant", chatcore.ErrNotFound, http.StatusNotFound, "chat.not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &apiService{watchErr: tc.err}
			w := httptest.NewRecorder()
			NewHandler(apiConfig(t, trust.SubjectKindIntegration), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?resume_cursor=signed", ""))
			if w.Code != tc.status || len(s.watch) != 1 || s.watch[0].ResumeCursor != "signed" || w.Body.String() != `{"error":{"code":"`+tc.code+`"}}`+"\n" {
				t.Fatalf("status=%d calls=%d body=%q", w.Code, len(s.watch), w.Body.String())
			}
		})
	}
	{
		s := &apiService{watchEvents: make(chan chatcore.WatchEvent), watchFailures: make(chan error, 1)}
		s.watchFailures <- chatcore.ErrPermissionDenied
		w := httptest.NewRecorder()
		NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?wait_ms=100", ""))
		if w.Code != http.StatusNotFound || w.Body.String() != "{\"error\":{\"code\":\"chat.not_found\"}}\n" {
			t.Fatalf("terminal revoke=%d %s", w.Code, w.Body.String())
		}
	}
	{
		events := make(chan chatcore.WatchEvent, 1)
		events <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: 1}, ResumeCursor: "signed-1"}
		close(events)
		failures := make(chan error, 1)
		failures <- chatcore.ErrPermissionDenied
		close(failures)
		s := &apiService{watchEvents: events, watchFailures: failures}
		w := httptest.NewRecorder()
		NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events", ""))
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "signed-1") {
			t.Fatalf("mid-page revoke=%d %s", w.Code, w.Body.String())
		}
	}
}

func TestTodo_CHAT_009_EventPullBound(t *testing.T) {
	events := make(chan chatcore.WatchEvent, 51)
	for i := 1; i <= 51; i++ {
		events <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: uint64(i)}, ResumeCursor: "signed-" + strconv.Itoa(i)}
	}
	s := &apiService{watchEvents: events, watchFailures: make(chan error)}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?wait_ms=100", ""))
	var body struct {
		Events []json.RawMessage `json:"events"`
		Cursor string            `json:"resume_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK || len(body.Events) != 50 || body.Cursor != "signed-50" {
		t.Fatalf("status=%d count=%d cursor=%q err=%v", w.Code, len(body.Events), body.Cursor, err)
	}
}

func TestTodo_CHAT_009_Security_EventPullHidesNonPostPayload(t *testing.T) {
	events := make(chan chatcore.WatchEvent, 1)
	events <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.MembershipChanged, Sequence: 9, Membership: &chatcore.Membership{SubjectID: "private-person"}}, ResumeCursor: "signed-9"}
	s := &apiService{watchEvents: events, watchFailures: make(chan error)}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?wait_ms=1", ""))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "private-person") || w.Body.String() != "{\"events\":[],\"resume_cursor\":\"signed-9\"}\n" {
		t.Fatalf("hidden event status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestTodo_CHAT_009_EventPullTimeout(t *testing.T) {
	s := &apiService{}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?wait_ms=1", ""))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != "{\"events\":[],\"resume_cursor\":\"\"}\n" {
		t.Fatalf("timeout=%d %s", w.Code, w.Body.String())
	}
}

func TestTodo_CHAT_009_EventPullWaitStartsAfterSubscription(t *testing.T) {
	s := &apiService{watchDelay: 20 * time.Millisecond}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?wait_ms=1", ""))
	if w.Code != http.StatusOK || len(s.watch) != 1 || len(s.got) != 1 || w.Body.String() != "{\"events\":[],\"resume_cursor\":\"\"}\n" {
		t.Fatalf("delayed setup status=%d watch=%d recheck=%d body=%q", w.Code, len(s.watch), len(s.got), w.Body.String())
	}
}

func TestTodo_CHAT_009_Security_EventPullFinalRecheck(t *testing.T) {
	events := make(chan chatcore.WatchEvent, 1)
	events <- chatcore.WatchEvent{Event: chatcore.ConversationEvent{Kind: chatcore.PostCreated, Sequence: 8, Post: &chatcore.Post{ID: "secret-post", Body: "secret"}}, ResumeCursor: "signed-secret"}
	s := &apiService{watchEvents: events, watchFailures: make(chan error), getErr: chatcore.ErrPermissionDenied}
	w := httptest.NewRecorder()
	NewHandler(apiConfig(t, trust.SubjectKindAgent), s).ServeHTTP(w, apiRequest(http.MethodGet, "/v1/conversations/channel-a/events?wait_ms=1", ""))
	if w.Code != http.StatusNotFound || len(s.watch) != 1 || len(s.got) != 1 || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != "{\"error\":{\"code\":\"chat.not_found\"}}\n" {
		t.Fatalf("recheck status=%d watch=%d get=%d body=%q", w.Code, len(s.watch), len(s.got), w.Body.String())
	}
}

func TestTodo_CHAT_009_Security_ResourceExistenceIndistinguishable(t *testing.T) {
	for _, path := range []string{"/v1/conversations/channel-a", "/v1/conversations/channel-a/posts", "/v1/conversations/channel-a/events?wait_ms=1"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			if method == http.MethodPost && !strings.HasSuffix(path, "/posts") {
				continue
			}
			for _, denial := range []error{chatcore.ErrPermissionDenied, chatcore.ErrNotFound} {
				s := &apiService{getErr: denial, readErr: denial, sendErr: denial, watchErr: denial}
				w := httptest.NewRecorder()
				NewHandler(apiConfig(t, trust.SubjectKindIntegration), s).ServeHTTP(w, apiRequest(method, path, `{"body":"hello"}`))
				if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" || w.Body.String() != "{\"error\":{\"code\":\"chat.not_found\"}}\n" {
					t.Fatalf("%s %s denial=%v status=%d body=%q", method, path, denial, w.Code, w.Body.String())
				}
			}
		}
	}
}
