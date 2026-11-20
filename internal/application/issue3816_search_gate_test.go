package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type issue3816Authority struct{}

func (issue3816Authority) Check(_ context.Context, p chatgate.Actor, _ chatgate.Scope, action string) error {
	if (action == "admin" || action == "export" || action == "install") && p.Person != "admin" {
		return chatgate.ErrDenied
	}
	return nil
}
func (issue3816Authority) Policy(context.Context, chatgate.Scope) (chatgate.Policy, error) {
	return chatgate.Policy{Ceiling: "INTERNAL"}, nil
}
func (issue3816Authority) Facts(context.Context, chatgate.Actor, chatgate.Scope) (map[string]string, error) {
	return map[string]string{}, nil
}
func (issue3816Authority) Reference(context.Context, chatgate.Actor, chatgate.Scope, chatgate.Field, json.RawMessage) error {
	return nil
}

// issue3816FakeSummaries stands in for the gate repository's list: a person is
// listed only for the gates they may know of.
type issue3816FakeSummaries map[string][]chatstore.GateSummary

func (s issue3816FakeSummaries) GateSummaries(_ context.Context, _, person string) ([]chatstore.GateSummary, error) {
	return s[person], nil
}

var issue3816Port ChatSearchGatePort

func issue3816Fixture(t *testing.T) *chatsearch.Registry {
	t.Helper()
	service := &chatgate.Service{Repository: chatgate.NewMemoryRepository(), Authority: issue3816Authority{}, Registry: chatgate.NewRegistry(), Clock: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }}
	c := chatgate.Command{Scope: chatgate.Scope{Tenant: "tenant", Conversation: "room"}, Actor: chatgate.Actor{Tenant: "tenant", Person: "admin"}, Key: "define"}
	d := chatgate.Definition{Mode: "review", Purpose: "Keep the room focused", Fields: []chatgate.Field{
		{ID: "why", Kind: "long_text", KindVersion: "1.0.0", Label: "Why do you want to join?", Help: "A sentence is enough", Purpose: "Review", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30},
	}}
	if err := service.Define(t.Context(), c, d); err != nil {
		t.Fatal(err)
	}
	c.Key, c.ExpectedRevision = "publish", 1
	if _, err := service.Publish(t.Context(), c, chatgate.Version{Major: 1}); err != nil {
		t.Fatal(err)
	}
	apply := c
	apply.Actor.Person, apply.Key, apply.ExpectedRevision = "linh", "submit", 2
	if _, err := service.Submit(t.Context(), apply, "1.0.0", map[string]json.RawMessage{"why": json.RawMessage(`"I mentor payroll newcomers"`)}); err != nil {
		t.Fatal(err)
	}
	summaries := issue3816FakeSummaries{
		"linh":     {{Conversation: "room", Questions: 1, Mode: "review", Request: "review"}},
		"admin":    {{Conversation: "room", Questions: 1, Mode: "review", Member: true}},
		"member2":  {{Conversation: "room", Questions: 1, Mode: "review", Member: true}},
		"outsider": {},
	}
	registry := chatsearch.NewRegistry()
	issue3816Port = chatSearchGates{Summaries: summaries, Service: service}
	if err := RegisterChatSearchGates(registry, issue3816Port); err != nil {
		t.Fatal(err)
	}
	return registry
}

func issue3816Search(t *testing.T, r *chatsearch.Registry, person, query string, kind chatsearch.Kind) []chatsearch.Row {
	t.Helper()
	got, err := r.Search(t.Context(), chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: person}, Query: query, Filters: chatsearch.Filters{Kind: kind}, At: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	var rows []chatsearch.Row
	for _, g := range got.Groups {
		rows = append(rows, g.Rows...)
	}
	return rows
}

func TestIssue3816_QuestionWordingFindsAGateQuestion(t *testing.T) {
	r := issue3816Fixture(t)
	for _, person := range []string{"linh", "member2"} {
		rows := issue3816Search(t, r, person, "why do you want", chatsearch.GateQuestion)
		if len(rows) != 1 || rows[0].Kind != chatsearch.GateQuestion || rows[0].Target.ConversationID != "room" || rows[0].Private {
			t.Fatalf("%s: gate question rows %+v", person, rows)
		}
	}
	if rows := issue3816Search(t, r, "linh", "sentence is enough", chatsearch.GateQuestion); len(rows) != 1 {
		t.Fatalf("help text not found: %+v", rows)
	}
	if rows := issue3816Search(t, r, "linh", "unrelated words", chatsearch.GateQuestion); len(rows) != 0 {
		t.Fatalf("unrelated query matched: %+v", rows)
	}
}

func TestIssue3816_Security_QuestionsStayWithThoseWhoMayKnowTheGate(t *testing.T) {
	r := issue3816Fixture(t)
	if rows := issue3816Search(t, r, "outsider", "why do you want", chatsearch.GateQuestion); len(rows) != 0 {
		t.Fatalf("a person who may not know of the gate found its question: %+v", rows)
	}
	other := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant-b", HomeTenantID: "tenant", PersonID: "linh"}, Query: "why do you want", Filters: chatsearch.Filters{Kind: chatsearch.GateQuestion}, At: time.Now().UTC()}
	if got, err := r.Search(t.Context(), other); err != nil || len(got.Groups) != 0 {
		t.Fatalf("question crossed tenants: %+v %v", got, err)
	}
}

func TestIssue3816_OwnAnswerTextFindsAGateAnswerForTheOwnerOnly(t *testing.T) {
	r := issue3816Fixture(t)
	rows := issue3816Search(t, r, "linh", "payroll newcomers", chatsearch.GateAnswer)
	if len(rows) != 1 || rows[0].Kind != chatsearch.GateAnswer || !rows[0].Private || rows[0].OwnerID != "linh" || rows[0].Target.ConversationID != "room" {
		t.Fatalf("own answer rows %+v", rows)
	}
	if rows[0].Text != "Why do you want to join?: I mentor payroll newcomers" {
		t.Fatalf("answer row text %q", rows[0].Text)
	}
	// The question's wording alone does not turn up the answer.
	if rows := issue3816Search(t, r, "linh", "why do you want", chatsearch.GateAnswer); len(rows) != 0 {
		t.Fatalf("answer found by its question: %+v", rows)
	}
}

func TestIssue3816_Security_NobodyElseFindsSomeonesAnswer(t *testing.T) {
	r := issue3816Fixture(t)
	for _, person := range []string{"admin", "member2", "outsider"} {
		if rows := issue3816Search(t, r, person, "payroll newcomers", chatsearch.GateAnswer); len(rows) != 0 {
			t.Fatalf("%s found another person's answer: %+v", person, rows)
		}
		// Nor does an unfiltered search show it.
		for _, row := range issue3816Search(t, r, person, "payroll newcomers", "") {
			if row.Kind == chatsearch.GateAnswer {
				t.Fatalf("%s: answer in an unfiltered search: %+v", person, row)
			}
		}
	}
	cross := chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant-b", HomeTenantID: "tenant", PersonID: "linh"}, Query: "payroll", Filters: chatsearch.Filters{Kind: chatsearch.GateAnswer}, At: time.Now().UTC()}
	if got, err := r.Search(t.Context(), cross); err != nil || len(got.Groups) != 0 {
		t.Fatalf("answer crossed tenants: %+v %v", got, err)
	}
}

func TestIssue3816_AnswerRowsAreRecheckedWhenOpened(t *testing.T) {
	r := issue3816Fixture(t)
	rows := issue3816Search(t, r, "linh", "payroll newcomers", chatsearch.GateAnswer)
	if len(rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	_, source := issue3816Sources(issue3816Port)
	if ok, err := source.CanOpen(t.Context(), chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "linh"}, rows[0]); err != nil || !ok {
		t.Fatalf("owner cannot open own answer: %v %v", ok, err)
	}
	if ok, _ := source.CanOpen(t.Context(), chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "member2"}, rows[0]); ok {
		t.Fatal("another person opened the answer")
	}
}

func TestIssue3816_NilPortIsRefused(t *testing.T) {
	if err := RegisterChatSearchGates(chatsearch.NewRegistry(), nil); err == nil {
		t.Fatal("nil port accepted")
	}
}

// issue3816DenyDiscover refuses the discovery check for one person, as the real
// authority does for somebody who may not see a gate, while the list of gates
// still names it.
type issue3816DenyDiscover struct {
	issue3816Authority
	person string
}

func (a issue3816DenyDiscover) Check(ctx context.Context, p chatgate.Actor, s chatgate.Scope, action string) error {
	if p.Person == a.person && (action == "discover" || action == "read_answers") {
		return chatgate.ErrDenied
	}
	return a.issue3816Authority.Check(ctx, p, s, action)
}

func TestIssue3816_Security_TheGateOwnDiscoveryCheckDecidesNotTheList(t *testing.T) {
	r := issue3816Fixture(t)
	service := issue3816Port.(chatSearchGates).Service
	service.Authority = issue3816DenyDiscover{person: "linh"}
	// The list still names the room for linh, yet the gate refuses them: no
	// question and no answer is returned, and the refusal is not an error.
	for _, kind := range []chatsearch.Kind{chatsearch.GateQuestion, chatsearch.GateAnswer} {
		if rows := issue3816Search(t, r, "linh", "payroll why", kind); len(rows) != 0 {
			t.Fatalf("%s found past the gate's own refusal: %+v", kind, rows)
		}
	}
	// Somebody the gate still admits keeps finding the question.
	if rows := issue3816Search(t, r, "member2", "why do you want", chatsearch.GateQuestion); len(rows) != 1 {
		t.Fatalf("the refusal of one person hid the question from another: %+v", rows)
	}
}
