package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// agentux059Store is the invocation store as the served surface uses it: it
// takes a rating, removes it, and answers what rating the person holds.
type agentux059Store struct {
	*agentUXR5SrvInvocationActions
	failing error
}

func (s *agentux059Store) SubmitAnswerFeedback(ctx context.Context, tenant, person, invocation string, helpful bool, reason, key string, at time.Time) (agentinvocationstore.AnswerFeedback, error) {
	if s.failing != nil {
		return agentinvocationstore.AnswerFeedback{}, s.failing
	}
	return s.agentUXR5SrvInvocationActions.SubmitAnswerFeedback(ctx, tenant, person, invocation, helpful, reason, key, at)
}

func (s *agentux059Store) ListAnswerFeedback(_ context.Context, tenant, person, _ string) (map[string]agentinvocationstore.AnswerFeedback, error) {
	held := map[string]agentinvocationstore.AnswerFeedback{}
	if s.feedback.Active && s.feedback.PersonID == person && tenant == s.invocation.TenantID {
		held[s.feedback.InvocationID] = s.feedback
	}
	return held, nil
}

func agentux059Call(t *testing.T, ctx context.Context, surface personachat.Surface, method, path, body string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	response := httptest.NewRecorder()
	personachat.Handler{Surface: surface}.ServeHTTP(response, request)
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("%s %s answered %d %q", method, path, response.Code, response.Body.String())
	}
	return response.Code, decoded
}

// The served handler takes a rating, shows it in the activity a reload reads,
// takes its removal, and stops a run: the routes the page calls reach the
// surface, with no model involved.
func TestTodo_AGENTUX_059_Integration(t *testing.T) {
	surface, ctx, invocation, _ := agentUXR5SrvActionSurface(t, runstate.StateCompleted)
	store := &agentux059Store{agentUXR5SrvInvocationActions: surface.Invocations.(*agentUXR5SrvInvocationActions)}
	surface.Invocations = store
	base := personachat.Path + "/invocations/" + invocation.ID
	rated := func() string {
		t.Helper()
		code, activity := agentux059Call(t, ctx, surface, http.MethodGet, personachat.Path+"/invocations?conversation_id=channel-a", "")
		rows, _ := activity["invocations"].([]any)
		if code != http.StatusOK || len(rows) != 1 {
			t.Fatalf("activity = %d %v", code, activity)
		}
		feedback, _ := rows[0].(map[string]any)["feedback"].(string)
		return feedback
	}

	if got := rated(); got != "" {
		t.Fatalf("an unrated answer is sent as %q", got)
	}
	code, result := agentux059Call(t, ctx, surface, http.MethodPost, base+"/feedback", `{"helpful":true,"reason":"","idempotency_key":"rate-key-0001"}`)
	if code != http.StatusOK || result["active"] != true || result["helpful"] != true {
		t.Fatalf("rating = %d %v", code, result)
	}
	if got := rated(); got != "helpful" {
		t.Fatalf("after Helpful a reload reads %q", got)
	}
	code, result = agentux059Call(t, ctx, surface, http.MethodPost, base+"/feedback", `{"helpful":false,"reason":"","idempotency_key":"rate-key-0002"}`)
	if got := rated(); code != http.StatusOK || got != "not-right" {
		t.Fatalf("changing the rating = %d %v, a reload reads %q", code, result, got)
	}
	code, result = agentux059Call(t, ctx, surface, http.MethodPost, base+"/feedback/undo", `{"idempotency_key":"undo-key-0001"}`)
	if code != http.StatusOK || result["active"] != false {
		t.Fatalf("removing the rating = %d %v", code, result)
	}
	if got := rated(); got != "" {
		t.Fatalf("after the rating was removed a reload reads %q", got)
	}

	// The store refuses or is down: the page is told, with the status it maps to
	// "not saved", and nothing is stored.
	for _, tc := range []struct {
		err  error
		code int
	}{{agentinvocationstore.ErrConflict, http.StatusConflict}, {errors.New("store is down"), http.StatusServiceUnavailable}, {agentinvocationstore.ErrNotFound, http.StatusForbidden}} {
		store.failing = tc.err
		if code, _ = agentux059Call(t, ctx, surface, http.MethodPost, base+"/feedback", `{"helpful":true,"reason":"","idempotency_key":"rate-key-0003"}`); code != tc.code {
			t.Fatalf("%v answered %d, want %d", tc.err, code, tc.code)
		}
		if got := rated(); got != "" {
			t.Fatalf("%v: a refused rating is read back as %q", tc.err, got)
		}
	}

	// Stop reaches the run: a run at work is stopped; one that is over says so.
	if code, result = agentux059Call(t, ctx, surface, http.MethodPost, base+"/cancel", `{"idempotency_key":"stop-key-0001"}`); code != http.StatusConflict || result["state"] != string(runstate.StateCompleted) {
		t.Fatalf("stopping a finished run = %d %v", code, result)
	}
	running, runningCtx, runningInvocation, runs := agentUXR5SrvActionSurface(t, runstate.StateRunning)
	if code, result = agentux059Call(t, runningCtx, running, http.MethodPost, personachat.Path+"/invocations/"+runningInvocation.ID+"/cancel", `{"idempotency_key":"stop-key-0002"}`); code != http.StatusOK || result["state"] != string(runstate.StateCancelled) {
		t.Fatalf("stopping a run at work = %d %v", code, result)
	}
	runID, _ := personaSurfaceRunID(runningInvocation)
	if run, err := runs.Get(runningCtx, runID); err != nil || run.State != runstate.StateCancelled {
		t.Fatalf("the run was not stopped: %+v %v", run, err)
	}
}
