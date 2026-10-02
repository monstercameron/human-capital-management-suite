package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUXServedAmbient serves the ambient surface through the assembly's own
// overlay, with real stores and the fake model of the ambient fixtures, and
// returns a caller that acts as a given person with a bearer credential.
func agentUXServedAmbient(t *testing.T, compose bool) (*AgentUXAmbientService, *agentUXAmbientTestModel, func(person, method, path string, body any) *httptest.ResponseRecorder) {
	t.Helper()
	service, model, _ := agentUXAmbientFixture(t)
	now := service.Now()
	people := map[string]*trust.Principal{}
	for _, person := range []string{"author", "luis", "peer"} {
		principal, err := localAgentDemoPrincipal(now, "tenant-a", person, "organization")
		if err != nil {
			t.Fatal(err)
		}
		people[person] = principal
	}
	admission := transport.Config{Now: func() time.Time { return now }, Verifier: trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		if principal, ok := people[credential.Token]; ok && credential.Scheme == "Bearer" {
			return principal, nil
		}
		return nil, trust.ErrNoCredential
	})}
	assembly := &agentServedAssembly{}
	if compose {
		store, ok := service.DB.(*chatstore.Store)
		if !ok {
			t.Fatal("the fixture's chat database is not the chat store")
		}
		assembly.agentUX.Ambient = composeAgentUXAmbientSurface(store, func(context.Context, string, string, string) error { return nil }, service.Now)
	}
	handler := assembly.Overlay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }), admission)
	call := func(person, method, path string, body any) *httptest.ResponseRecorder {
		var reader *bytes.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		r := httptest.NewRequest(method, "http://cell.test"+path, reader)
		if person != "" {
			r.Header.Set("Authorization", "Bearer "+person)
		}
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	return service, model, call
}

func agentUXServedSnapshot(t *testing.T, w *httptest.ResponseRecorder) ambientagents.Snapshot {
	t.Helper()
	var snapshot ambientagents.Snapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil {
		t.Fatalf("answer %d %s", w.Code, w.Body.String())
	}
	return snapshot
}

// TestTodo_AGENTUX_066_Served proves the ambient agents are on the page's
// server and not only in the library: the read answers each person what that
// person may see, the conversation's administrator (and nobody else) turns
// "Reads every message here" on and off and the switch is enforced before any
// read, a member's own opt-out is kept, and a cell that has not composed the
// surface still answers 200 with nothing in it instead of "not found".
func TestTodo_AGENTUX_066_Served(t *testing.T) {
	service, model, call := agentUXServedAmbient(t, true)
	ctx := t.Context()
	agentUXAmbientPost(t, service, "self", "author", "I'll send the deck")
	agentUXAmbientPost(t, service, "public", "luis", "Can someone book the room?")
	for _, post := range []string{"self", "public"} {
		if err := service.ProcessMessage(ctx, "tenant-a", "general", post, "task-catcher", "UTC"); err != nil {
			t.Fatal(err)
		}
	}
	path := ambientagents.Path + "?conversation=general"

	if w := call("", http.MethodGet, path, nil); w.Code == http.StatusOK || w.Code == http.StatusNotFound || w.Code == http.StatusTeapot {
		t.Fatalf("an unauthenticated read was %d, want a refusal from admission", w.Code)
	}
	author := agentUXServedSnapshot(t, call("author", http.MethodGet, path, nil))
	peer := agentUXServedSnapshot(t, call("peer", http.MethodGet, path, nil))
	if len(author.Cards) != 2 || len(peer.Cards) != 1 || peer.Cards[0].Scope != "PUBLIC" || peer.Cards[0].Source != "public" {
		t.Fatalf("author sees %d cards, peer sees %+v: a private offer must reach only its person", len(author.Cards), peer.Cards)
	}
	if !author.Manages || peer.Manages || len(peer.Grants) != 2 || !peer.Grants[0].Enabled {
		t.Fatalf("manages author=%v peer=%v grants=%+v", author.Manages, peer.Manages, peer.Grants)
	}
	for _, card := range peer.Cards {
		if card.SourceHref != "/workspace/app/chat?conversation=general&message=public" {
			t.Fatalf("the card links to %q", card.SourceHref)
		}
	}

	// Nothing is added until a person presses Add.
	if tasks, err := service.ListTasks(ctx, "tenant-a", "author"); err != nil || len(tasks) != 0 {
		t.Fatalf("a task was added by the agent: %+v %v", tasks, err)
	}
	var private chatCard
	for _, card := range author.Cards {
		if card.Scope == "PRIVATE" {
			private = chatCard{ID: card.ID, Revision: card.Revision}
		}
	}
	if w := call("peer", http.MethodPost, ambientagents.Path+"/control", ambientagents.Command{Conversation: "general", ID: private.ID, Action: "ADD", ExpectedRevision: private.Revision}); w.Code != http.StatusForbidden {
		t.Fatalf("a peer pressed Add on another person's private card: %d", w.Code)
	}
	if w := call("author", http.MethodPost, ambientagents.Path+"/control", ambientagents.Command{Conversation: "general", ID: private.ID, Action: "ADD", ExpectedRevision: private.Revision}); w.Code != http.StatusOK {
		t.Fatalf("Add by the person: %d %s", w.Code, w.Body.String())
	}
	if tasks, err := service.ListTasks(ctx, "tenant-a", "author"); err != nil || len(tasks) != 1 || tasks[0].Source != "self" {
		t.Fatalf("own tasks %+v %v", tasks, err)
	}

	// Only the conversation's administrator switches "Reads every message here".
	off := ambientagents.GrantCommand{Conversation: "general", Agent: "task-catcher", Enabled: false}
	if w := call("peer", http.MethodPost, ambientagents.Path+"/grant", off); w.Code != http.StatusForbidden {
		t.Fatalf("a member switched an agent off: %d", w.Code)
	}
	snapshot := agentUXServedSnapshot(t, call("author", http.MethodPost, ambientagents.Path+"/grant", off))
	for _, grant := range snapshot.Grants {
		if grant.Enabled == (grant.Agent == "task-catcher") {
			t.Fatalf("grants after switching only task-catcher off: %+v", snapshot.Grants)
		}
	}
	calls := model.calls
	agentUXAmbientPost(t, service, "after-off", "author", "I'll book the travel")
	if err := service.ProcessMessage(ctx, "tenant-a", "general", "after-off", "task-catcher", "UTC"); err != nil || model.calls != calls {
		t.Fatalf("an agent turned off still read the message (model calls %d -> %d, err %v)", calls, model.calls, err)
	}
	on := ambientagents.GrantCommand{Conversation: "general", Agent: "task-catcher", Enabled: true}
	snapshot = agentUXServedSnapshot(t, call("author", http.MethodPost, ambientagents.Path+"/grant", on))
	for _, grant := range snapshot.Grants {
		if !grant.Enabled {
			t.Fatalf("%s is still off after switching it on", grant.Agent)
		}
	}

	// A member's own opt-out is kept for that member alone.
	snapshot = agentUXServedSnapshot(t, call("peer", http.MethodPost, ambientagents.Path+"/opt-out", ambientagents.OptOutCommand{Conversation: "general", OptOut: true}))
	if !snapshot.OptOut || agentUXServedSnapshot(t, call("luis", http.MethodGet, path, nil)).OptOut {
		t.Fatal("the opt-out was lost or applied to someone else")
	}
}

type chatCard struct {
	ID       string
	Revision uint64
}

// A cell that has not composed the ambient surface (no chat database, or an
// older assembly) still answers the read with 200 and an empty list: before the
// assembly claimed the path, the request was handed to the base handler and
// Chat saw "not found" for every conversation it opened.
func TestTodo_AGENTUX_066_ServedAbsent(t *testing.T) {
	_, _, call := agentUXServedAmbient(t, false)
	w := call("author", http.MethodGet, ambientagents.Path+"?conversation=general", nil)
	snapshot := agentUXServedSnapshot(t, w)
	if len(snapshot.Cards) != 0 || len(snapshot.Grants) != 0 || snapshot.Manages {
		t.Fatalf("an uncomposed cell answered %+v", snapshot)
	}
	if w := call("author", http.MethodPost, ambientagents.Path+"/grant", ambientagents.GrantCommand{Conversation: "general", Agent: "task-catcher", Enabled: true}); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a command to an uncomposed cell was %d, want unavailable", w.Code)
	}
}

// TestTodo_AGENTUX_053_ServedBirthdayPreference proves the person's "share my
// birthday" preference is on the served assembly: it is read and saved for the
// person the bearer credential names and for nobody else, a stale save is a
// conflict, an anonymous call is refused by admission, and a cell that has not
// composed it hands the path on as before.
func TestTodo_AGENTUX_053_ServedBirthdayPreference(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	principal, err := localAgentDemoPrincipal(now, localAgentDemoTenant, "ir-002-person", "organization")
	if err != nil {
		t.Fatal(err)
	}
	admission := transport.Config{Now: func() time.Time { return now }, Verifier: trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		if credential.Scheme == "Bearer" && credential.Token == "person" {
			return principal, nil
		}
		return nil, trust.ErrNoCredential
	})}
	store := &agentuxDemoBirthdayProfileFixture{}
	assembly := &agentServedAssembly{}
	assembly.agentUX.Birthday = AgentUXDemoBirthdayProfile{Store: store, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.MustParse("00000000-0000-0000-0000-0000000000a1") }, Now: func() time.Time { return now }}
	fell := 0
	handler := assembly.Overlay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fell++; w.WriteHeader(http.StatusTeapot) }), admission)
	do := func(method, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://cell.test"+agentdemo.BirthdayPreferencePath, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := do(http.MethodGet, "", ""); w.Code == http.StatusOK || w.Code == http.StatusTeapot || fell != 0 {
		t.Fatalf("an anonymous read was %d", w.Code)
	}
	read := do(http.MethodGet, "person", "")
	var preference agentdemo.BirthdayPreference
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &preference) != nil || !preference.ShareBirthday || store.worker != "ir-002-person" {
		t.Fatalf("read %d %s worker=%q", read.Code, read.Body.String(), store.worker)
	}
	if w := do(http.MethodPost, "person", `{"share_birthday":false,"revision":0}`); w.Code != http.StatusOK || store.pref.Share || store.worker != "ir-002-person" {
		t.Fatalf("opt out %d %s", w.Code, w.Body.String())
	}
	if w := do(http.MethodPost, "person", `{"share_birthday":true,"revision":0}`); w.Code != http.StatusConflict {
		t.Fatalf("a stale save was %d, want a conflict", w.Code)
	}
	if w := do(http.MethodPost, "person", `{"worker":"someone-else","share_birthday":false,"revision":1}`); w.Code != http.StatusBadRequest || store.worker != "ir-002-person" {
		t.Fatalf("a body naming another person was %d", w.Code)
	}

	// Not composed: the path is handed on, exactly as before.
	bare := (&agentServedAssembly{}).Overlay(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fell++; w.WriteHeader(http.StatusTeapot) }), admission)
	r := httptest.NewRequest(http.MethodGet, "http://cell.test"+agentdemo.BirthdayPreferencePath, nil)
	r.Header.Set("Authorization", "Bearer person")
	w := httptest.NewRecorder()
	bare.ServeHTTP(w, r)
	if w.Code == http.StatusOK {
		t.Fatalf("an uncomposed cell answered the preference with %d", w.Code)
	}
}
