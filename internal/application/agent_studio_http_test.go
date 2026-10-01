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

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type studioHTTPActorFake struct{ err error }

func (f studioHTTPActorFake) AgentStudioActor(*http.Request, *trust.Principal) (agentmodel.ActorChain, error) {
	if f.err != nil {
		return agentmodel.ActorChain{}, f.err
	}
	return agentmodel.ActorChain{UserID: "user:owner", AgentVersion: "studio@1", InstallationID: "studio", TaskID: "studio-task", PlanRevision: "1", StepID: "suggest", DelegationGrantID: "grant"}, nil
}

type studioHTTPDraftsFake struct {
	snapshot AgentStudioDraftSnapshot
	calls    int
}

func (f *studioHTTPDraftsFake) CurrentAgentStudioDraft(context.Context, string) (AgentStudioDraftSnapshot, error) {
	f.calls++
	return f.snapshot, nil
}

func studioHTTPHandler(t *testing.T, authErr error, drafts *studioHTTPDraftsFake, executor *studioExecutorFake) http.Handler {
	t.Helper()
	service := &AgentStudioSuggestionService{Drafts: drafts, Profiles: personaProfileBuilderFake{}, Executor: executor, Authorizer: &personaAdminCommandAuthFake{err: authErr}}
	handler, err := NewAgentStudioHandler(AgentStudioHTTPConfig{Drafts: drafts, Generator: &AgentStudioModelGenerator{}, Apply: service, Actors: studioHTTPActorFake{}})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func studioHTTPRequest(t *testing.T, body string, path string) *http.Request {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	principal := personaDraftPrincipal(t, "tenant-a", "user:owner", now)
	request := httptest.NewRequest(http.MethodPost, AgentStudioPath+path, strings.NewReader(body))
	return request.WithContext(trust.WithPrincipal(request.Context(), principal))
}

func TestTodo_AGENT_051_HTTP_Security(t *testing.T) {
	profile := studioProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	drafts := &studioHTTPDraftsFake{snapshot: AgentStudioDraftSnapshot{PersonaID: "persona-a", Revision: 3, Version: agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 1, ContentDigest: sealed.Digest, Profile: studioProfileJSON(t, sealed.Profile)}, State: agentpersonastore.StateDraft}}
	unauthorized := studioHTTPHandler(t, errors.New("denied"), drafts, &studioExecutorFake{})
	response := httptest.NewRecorder()
	unauthorized.ServeHTTP(response, studioHTTPRequest(t, `{"persona_id":"persona-a","goal":"improve"}`, "suggest"))
	if response.Code != http.StatusForbidden || drafts.calls != 0 {
		t.Fatalf("unauthorized response=%d draft_calls=%d", response.Code, drafts.calls)
	}
}

func TestTodo_AGENT_051_HTTP_Conformance(t *testing.T) {
	profile := studioProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	drafts := &studioHTTPDraftsFake{snapshot: AgentStudioDraftSnapshot{PersonaID: "persona-a", Revision: 3, Version: agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 1, ContentDigest: sealed.Digest, Profile: studioProfileJSON(t, sealed.Profile)}, State: agentpersonastore.StateDraft}}
	executor := &studioExecutorFake{}
	handler := studioHTTPHandler(t, nil, drafts, executor)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, studioHTTPRequest(t, `{"persona_id":"persona-a","revision":3,"digest":"`+sealed.Digest+`","changes":[{"field":"purpose","value":"Answer policy and leave questions","reason":"clarifies job","uncertainty":0.1}]}`, "apply"))
	if response.Code != http.StatusCreated || executor.calls != 1 || executor.command.Action != PersonaAdminCreateVersion {
		t.Fatalf("apply response=%d calls=%d command=%+v body=%s", response.Code, executor.calls, executor.command, response.Body.String())
	}
	var receipt AgentStudioSuggestionReceipt
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil || receipt.Draft.Version != 2 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, studioHTTPRequest(t, `{"persona_id":"persona-a","revision":3,"digest":"`+sealed.Digest+`","changes":[],"unexpected":true}`, "apply"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field response=%d body=%s", response.Code, response.Body.String())
	}
}
