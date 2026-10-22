package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// chatgateOpenAuthority lets everybody read and answer and lets only "admin"
// build, as the gate service's own fixtures do.
type chatgateOpenAuthority struct{}

func (chatgateOpenAuthority) Check(_ context.Context, a chatgate.Actor, _ chatgate.Scope, p string) error {
	if (p == "admin" || p == "export" || p == "retention" || p == "agent_run") && a.Person != "admin" {
		return chatgate.ErrDenied
	}
	return nil
}
func (chatgateOpenAuthority) Policy(context.Context, chatgate.Scope) (chatgate.Policy, error) {
	return chatgate.Policy{Ceiling: "INTERNAL", Private: true}, nil
}
func (chatgateOpenAuthority) Facts(context.Context, chatgate.Actor, chatgate.Scope) (map[string]string, error) {
	return map[string]string{}, nil
}
func (chatgateOpenAuthority) Reference(context.Context, chatgate.Actor, chatgate.Scope, chatgate.Field, json.RawMessage) error {
	return nil
}

func chatgateServedFixture(t *testing.T) (*ChatgateApplication, *chatgate.Service) {
	t.Helper()
	svc := &chatgate.Service{Repository: chatgate.NewMemoryRepository(), Authority: chatgateOpenAuthority{}, Registry: chatgate.NewRegistry()}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "room"}
	admin := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant", Person: "admin"}, Key: "define"}
	d := chatgate.Definition{Mode: "automatic", Purpose: "Join the payroll discussion", Fields: []chatgate.Field{{ID: "office", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which office?", Purpose: "Place you", DataClass: "INTERNAL", Required: true, Options: []string{"Denver", "Austin"}, Visibility: chatgate.Visibility{Administrators: true, Members: true, Consumers: []string{chatgate.ConsumerSegments}}, RetentionDays: 30}}}
	if err := svc.Define(t.Context(), admin, d); err != nil {
		t.Fatal(err)
	}
	admin.Key, admin.ExpectedRevision = "publish", 1
	if _, err := svc.Publish(t.Context(), admin, chatgate.Version{Major: 1}); err != nil {
		t.Fatal(err)
	}
	return &ChatgateApplication{Clock: time.Now, Service: svc, Consumers: svc}, svc
}

func chatgateRevision(t *testing.T, svc *chatgate.Service) uint64 {
	t.Helper()
	g, err := svc.Get(t.Context(), chatgate.Actor{Tenant: "tenant", Person: "admin"}, chatgate.Scope{Tenant: "tenant", Conversation: "room"}, true)
	if err != nil {
		t.Fatal(err)
	}
	return g.Revision
}

// TestTodo_CHATGATE_009_Served: the gate's request carries the consumers: an
// administrator saves rules and reads them back with counts, a member's own page
// carries their labels with the reason, and a member cannot write rules.
func TestTodo_CHATGATE_009_Served(t *testing.T) {
	app, svc := chatgateServedFixture(t)
	admin, member := chatgateAs(t, "admin"), chatgateAs(t, "member")
	rules := []chatgate.SegmentRule{{ID: "rule-1", Field: "office", Value: "Denver", Label: "Denver crew", Segment: "Denver office"}}
	reply, err := app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "segments-save", Key: "rules", ExpectedRevision: chatgateRevision(t, svc), Rules: rules, Reason: "First rules"})
	if err != nil || reply.View == nil || reply.View.Segments == nil || reply.View.Segments.Rules.Version != 1 || len(reply.View.Segments.Rules.Rules) != 1 {
		t.Fatalf("saving rules: %+v %v", reply.View, err)
	}
	// A member answers and reads their own labels.
	reply, err = app.GateRequest(member, ChatgateRequest{Conversation: "room", Action: "submit", Key: "join", Version: "1.0.0", ExpectedRevision: chatgateRevision(t, svc), Answers: map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}})
	if err != nil || reply.View == nil || len(reply.View.MySegments) != 2 || reply.View.Segments != nil {
		t.Fatalf("a member's page: %+v %v", reply.View, err)
	}
	// The administrator reads counts and never people.
	reply, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "get"})
	if err != nil || reply.View.Segments == nil || len(reply.View.Segments.Segments) != 1 || reply.View.Segments.Segments[0].People != 1 || reply.View.Metrics == nil || reply.View.Metrics.Admitted != 1 {
		t.Fatalf("the administrator's page: %+v %v", reply.View, err)
	}
	// A member cannot write rules, change the levels or add somebody.
	for _, request := range []ChatgateRequest{{Action: "segments-save", Rules: rules, Reason: "mine"}, {Action: "levels-save", Addressing: true}, {Action: "override", Person: "friend", Reason: "friend"}} {
		request.Conversation, request.Key, request.ExpectedRevision = "room", "member-"+request.Action, chatgateRevision(t, svc)
		if _, err = app.GateRequest(member, request); !errors.Is(err, chatgate.ErrDenied) {
			t.Fatalf("a member's %s: %v", request.Action, err)
		}
	}
	// Levels, operating settings and the answer-by date are saved and read back.
	reply, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "levels-save", Key: "levels", ExpectedRevision: chatgateRevision(t, svc), Addressing: true, Restricted: true})
	if err != nil || !reply.View.Segments.Addressing || !reply.View.Segments.Restricted {
		t.Fatalf("levels: %+v %v", reply.View, err)
	}
	reply, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "ops-save", Key: "ops", ExpectedRevision: chatgateRevision(t, svc), ReviewAfterHours: 24, EscalateAfterHours: 72})
	if err != nil || reply.View.Ops == nil || reply.View.Ops.ReviewAfter != 24*time.Hour || reply.View.Ops.EscalateAfter != 72*time.Hour {
		t.Fatalf("ops: %+v %v", reply.View.Ops, err)
	}
	by := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	reply, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "existing-save", Key: "existing", ExpectedRevision: chatgateRevision(t, svc), ExistingMode: chatgate.ExistingAsk, AnswerBy: by})
	if err != nil || reply.View.Ops.Existing.Mode != chatgate.ExistingAsk || reply.View.Ops.Existing.AnswerBy.Format("2006-01-02") != by {
		t.Fatalf("existing members: %+v %v", reply.View.Ops, err)
	}
	if _, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "existing-save", Key: "bad-date", ExpectedRevision: chatgateRevision(t, svc), ExistingMode: chatgate.ExistingAsk, AnswerBy: "next week"}); !errors.Is(err, chatgate.ErrInvalid) {
		t.Fatalf("an unreadable date: %v", err)
	}
	// Who a message to a segment would reach, as the author asks it.
	reply, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "reach", Key: "reach", ExpectedRevision: chatgateRevision(t, svc), Segments: []string{"Denver office"}})
	reach, ok := reply.Result.(chatgate.Reach)
	if err != nil || !ok || reach.People != 1 || reach.Restricted {
		t.Fatalf("reach: %+v %v", reply.Result, err)
	}
	// The administrator's override: a person is added without answers, with a reason.
	reply, err = app.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "override", Key: "override", ExpectedRevision: chatgateRevision(t, svc), Person: "invited", Reason: "Invited by the director"})
	if err != nil || reply.View == nil || !svc.Repository.(*chatgate.MemoryRepository).Member(chatgate.Scope{Tenant: "tenant", Conversation: "room"}, "invited") {
		t.Fatalf("override: %v", err)
	}
	// With no consumers composed these actions are unavailable, never silent.
	bare := &ChatgateApplication{Clock: time.Now, Service: svc}
	if _, err = bare.GateRequest(admin, ChatgateRequest{Conversation: "room", Action: "segments-save", Key: "bare", ExpectedRevision: chatgateRevision(t, svc), Rules: rules, Reason: "x"}); !errors.Is(err, chatgate.ErrUnavailable) {
		t.Fatalf("no consumers: %v", err)
	}
}

// TestTodo_CHATGATE_013_Served: a limit is a plain 429 the page can name, a
// reserved label is refused with its own code, and restricted visibility outside
// a private channel is refused with its own code.
func TestTodo_CHATGATE_013_Served(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{chatgate.ErrLimited, 429, "too_many_requests"}, {chatgate.ErrReservedLabel, 422, "reserved_label"}, {chatgate.ErrRestricted, 422, "restricted_not_allowed"}} {
		surface := &chatgateHTTPFixture{err: test.err}
		w := httptest.NewRecorder()
		ChatgateHTTP{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", ChatgatePath+"?conversation=room", nil))
		var reply struct{ Error string }
		if e := json.Unmarshal(w.Body.Bytes(), &reply); e != nil || w.Code != test.status || reply.Error != test.code {
			t.Fatalf("typed error %d %s", w.Code, w.Body.String())
		}
	}
	// Six submissions in a minute: the sixth is limited, through the page's request.
	app, svc := chatgateServedFixture(t)
	member := chatgateAs(t, "member")
	var err error
	for i := 0; i <= chatgate.MaxSubmissionsPerPersonPerMinute; i++ {
		_, err = app.GateRequest(member, ChatgateRequest{Conversation: "room", Action: "submit", Key: "again-" + string(rune('a'+i)), Version: "1.0.0", ExpectedRevision: chatgateRevision(t, svc), Answers: map[string]json.RawMessage{"office": json.RawMessage(`"Denver"`)}})
	}
	if !errors.Is(err, chatgate.ErrLimited) {
		t.Fatalf("the sixth submission in a minute: %v", err)
	}
}

type chatgateDocuments struct {
	version  string
	readable bool
	err      error
	asked    chat.AgentDocumentSource
}

func (d *chatgateDocuments) ResolveAgentDocumentSource(_ context.Context, _ chat.Principal, _, _ string, source chat.AgentDocumentSource) (chat.AgentDocumentSource, error) {
	d.asked = source
	if d.err != nil {
		return source, d.err
	}
	source.VersionID, source.Readable = d.version, d.readable
	return source, nil
}

// TestTodo_CHATGATE_005_Acknowledgement: an acknowledgement is accepted when the
// document hub lets the person read the exact version the question pins, and is
// refused with no hub, an unreadable or different version, or a failing hub.
func TestTodo_CHATGATE_005_Acknowledgement(t *testing.T) {
	_, authority := chatgateWorldFixture()
	field := chatgate.Field{ID: "policy", Kind: "acknowledgement", DocumentID: "doc-1", DocumentVersion: "ver-2"}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "public"}
	actor := chatgate.Actor{Tenant: "tenant", Person: "applicant"}
	answer := json.RawMessage(`true`)
	refused := func(label string) {
		t.Helper()
		err := authority.Reference(t.Context(), actor, scope, field, answer)
		var fe chatgate.FieldError
		if !errors.As(err, &fe) || fe.Field != "policy" || !errors.Is(err, chatgate.ErrDenied) {
			t.Fatalf("%s: %v", label, err)
		}
	}
	refused("no hub bound")
	hub := &chatgateDocuments{version: "ver-2", readable: true}
	authority.Documents = hub
	if err := authority.Reference(t.Context(), actor, scope, field, answer); err != nil {
		t.Fatalf("a readable pinned version: %v", err)
	}
	if hub.asked.DocumentID != "doc-1" || hub.asked.VersionID != "ver-2" {
		t.Fatalf("the hub was asked about %+v", hub.asked)
	}
	hub.readable = false
	refused("a version the person may not read")
	hub.readable, hub.version = true, "ver-3"
	refused("a different version")
	hub.version, hub.err = "ver-2", errors.New("hub down")
	refused("a failing hub")
	hub.err = nil
	unpinned := field
	unpinned.DocumentVersion = ""
	if err := authority.Reference(t.Context(), actor, scope, unpinned, answer); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an acknowledgement with no pinned version: %v", err)
	}
	_ = strings.TrimSpace
}
