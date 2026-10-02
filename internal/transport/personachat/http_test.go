package personachat

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type surfaceFixture struct {
	directory                               Directory
	progress                                Progress
	err                                     error
	retryID, retryKey                       string
	watchReads                              atomic.Int32
	denyAfter                               int32
	cancelID, cancelKey                     string
	feedbackID, feedbackReason, feedbackKey string
	feedbackHelpful                         bool
	undoID, undoKey                         string
}

func (s *surfaceFixture) Directory(context.Context, string) (Directory, error) {
	return s.directory, s.err
}
func (s *surfaceFixture) Progress(context.Context, string) (Progress, error) {
	if s.denyAfter > 0 && s.watchReads.Add(1) > s.denyAfter {
		return Progress{}, ErrDenied
	}
	return s.progress, s.err
}
func (s *surfaceFixture) Retry(_ context.Context, id, key string) (RetryResult, error) {
	s.retryID, s.retryKey = id, key
	return RetryResult{PostID: "fresh-post", ConversationID: "room"}, s.err
}
func (s *surfaceFixture) Cancel(_ context.Context, id, key string) (CancelResult, error) {
	s.cancelID, s.cancelKey = id, key
	return CancelResult{InvocationID: id, State: "CANCELLED"}, s.err
}
func (s *surfaceFixture) SubmitFeedback(_ context.Context, id string, helpful bool, reason, key string) (FeedbackResult, error) {
	s.feedbackID, s.feedbackHelpful, s.feedbackReason, s.feedbackKey = id, helpful, reason, key
	return FeedbackResult{InvocationID: id, Helpful: helpful, Reason: reason, Active: true}, s.err
}
func (s *surfaceFixture) UndoFeedback(_ context.Context, id, key string) (FeedbackResult, error) {
	s.undoID, s.undoKey = id, key
	return FeedbackResult{InvocationID: id}, s.err
}

func TestTodo_AGENTP_019_HTTPDirectoryAndBoundaryErrors(t *testing.T) {
	surface := &surfaceFixture{directory: Directory{Personas: []Profile{{Reference: Reference{Kind: "AGENT_MENTION", ID: "agent:coach", TenantID: "tenant", ConversationID: "room"}, Skills: []Skill{{Name: "Read policy", Tier: "T0"}}}}}}
	w := httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", Path+"?conversation_id=room", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"agent:coach"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("directory=%d %s", w.Code, w.Body.String())
	}
	for _, test := range []struct {
		err    error
		status int
	}{{ErrUnauthenticated, 401}, {ErrDenied, 403}, {ErrInvalid, 400}, {ErrConflict, 409}, {errors.New("private database error"), 503}} {
		surface.err = test.err
		w = httptest.NewRecorder()
		Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", Path+"?conversation_id=room", nil))
		if w.Code != test.status || strings.Contains(w.Body.String(), "database") {
			t.Fatalf("error=%d %s", w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	Handler{}.ServeHTTP(w, httptest.NewRequest("GET", Path, nil))
	if w.Code != 503 {
		t.Fatalf("nil surface=%d", w.Code)
	}
	w = httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("PUT", Path, nil))
	if w.Code != 404 {
		t.Fatalf("unsupported=%d", w.Code)
	}
}

func TestTodo_AGENTP_020_HTTPProgressAndRealRetryPort(t *testing.T) {
	surface := &surfaceFixture{progress: Progress{Invocations: []Invocation{{InvocationID: "invocation", Status: "FAILED", FailureCode: "MODEL_UNAVAILABLE", Retryable: true}}}}
	handler := Handler{Surface: surface}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", Path+"/invocations?conversation_id=room", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"failure_code":"MODEL_UNAVAILABLE"`) {
		t.Fatalf("progress=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", Path+"/invocations/invocation/retry", strings.NewReader(`{"idempotency_key":"click-unique"}`)))
	if w.Code != 200 || surface.retryID != "invocation" || surface.retryKey != "click-unique" || !strings.Contains(w.Body.String(), `"post_id":"fresh-post"`) {
		t.Fatalf("retry=%d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"tenant_id":"forged"}`, `broken`, strings.Repeat("x", 2048)} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", Path+"/invocations/invocation/retry", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("malformed=%d", w.Code)
		}
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", Path+"/invocations/a/b/retry", strings.NewReader(`{}`)))
	if w.Code != 400 {
		t.Fatalf("path=%d", w.Code)
	}
}

func TestTodo_AGENTP_020_StreamStopsWhenApplicationRevokesAccess(t *testing.T) {
	surface := &surfaceFixture{progress: Progress{Invocations: []Invocation{{InvocationID: "invocation", Status: "RUNNING"}}}, denyAfter: 2}
	server := httptest.NewServer(Handler{Surface: surface, WatchInterval: time.Millisecond})
	defer server.Close()
	response, err := http.Get(server.URL + Path + "/invocations?conversation_id=room&watch=1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("headers=%v", response.Header)
	}
	reader := bufio.NewReader(response.Body)
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "event: invocations") || !strings.Contains(string(body), ": keepalive") || !strings.Contains(string(body), "event: unavailable") || surface.watchReads.Load() != 3 {
		t.Fatalf("stream=%q reads=%d", body, surface.watchReads.Load())
	}
	surface.err, surface.denyAfter = ErrDenied, 0
	w := httptest.NewRecorder()
	Handler{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", Path+"/invocations?watch=1", nil))
	if w.Code != 403 {
		t.Fatalf("first denial=%d", w.Code)
	}
}

func TestAgentUXR5Srv_CancelAndFeedbackHTTP(t *testing.T) {
	surface := &surfaceFixture{}
	handler := Handler{Surface: surface}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path+"/invocations/run-1/cancel", strings.NewReader(`{"idempotency_key":"cancel-click-1"}`)))
	if w.Code != http.StatusOK || surface.cancelID != "run-1" || surface.cancelKey != "cancel-click-1" || !strings.Contains(w.Body.String(), `"state":"CANCELLED"`) {
		t.Fatalf("cancel=%d body=%s fixture=%+v", w.Code, w.Body.String(), surface)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path+"/invocations/run-1/feedback", strings.NewReader(`{"helpful":false,"reason":"The cited policy is outdated.","idempotency_key":"feedback-click-1"}`)))
	if w.Code != http.StatusOK || surface.feedbackID != "run-1" || surface.feedbackHelpful || surface.feedbackReason != "The cited policy is outdated." || surface.feedbackKey != "feedback-click-1" || !strings.Contains(w.Body.String(), `"active":true`) {
		t.Fatalf("feedback=%d body=%s fixture=%+v", w.Code, w.Body.String(), surface)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path+"/invocations/run-1/feedback/undo", strings.NewReader(`{"idempotency_key":"undo-click-1"}`)))
	if w.Code != http.StatusOK || surface.undoID != "run-1" || surface.undoKey != "undo-click-1" || !strings.Contains(w.Body.String(), `"active":false`) {
		t.Fatalf("undo=%d body=%s fixture=%+v", w.Code, w.Body.String(), surface)
	}

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, Path+"/invocations/a/b/cancel", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPost, Path+"/invocations/run-1/feedback", strings.NewReader(`{"helpful":true,"reason":"`+strings.Repeat("x", 501)+`","idempotency_key":"feedback-click-2"}`)),
		httptest.NewRequest(http.MethodPost, Path+"/invocations/run-1/feedback", strings.NewReader(`{"helpful":true,"unknown":true}`)),
	} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid action %s = %d %s", request.URL.Path, w.Code, w.Body.String())
		}
	}

	surface.err = &FinalStateConflict{State: "COMPLETED"}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Path+"/invocations/run-1/cancel", strings.NewReader(`{"idempotency_key":"cancel-click-2"}`)))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"state":"COMPLETED"`) {
		t.Fatalf("terminal cancel=%d %s", w.Code, w.Body.String())
	}
}
