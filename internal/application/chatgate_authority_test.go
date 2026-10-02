package application

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatgateWorld is a workspace for the gate authority: who has which workspace
// roles, who works in which team, and the channels with their members.
type chatgateWorld struct {
	roles       map[string][]string
	people      []chatgatePerson
	kinds       map[string]chat.ConversationKind
	members     map[string]map[string]chat.MembershipRole
	peopleError error
}

func (w *chatgateWorld) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, AuthorityRevision: 1, Roles: w.roles[subject]}, nil
}
func (w *chatgateWorld) GatePeople(context.Context, string) ([]chatgatePerson, error) {
	return w.people, w.peopleError
}
func (w *chatgateWorld) GetConversation(_ context.Context, tenant, id string) (chat.Conversation, error) {
	kind, ok := w.kinds[id]
	if !ok || tenant != "tenant" {
		return chat.Conversation{}, chat.ErrNotFound
	}
	return chat.Conversation{ID: id, TenantID: tenant, Kind: kind}, nil
}
func (w *chatgateWorld) GetMembership(_ context.Context, tenant, conversation, home, subject string) (chat.Membership, error) {
	role, ok := w.members[conversation][subject]
	if !ok || tenant != "tenant" || home != tenant {
		return chat.Membership{}, chat.ErrNotFound
	}
	joined := time.Now().Add(-time.Hour)
	return chat.Membership{ConversationID: conversation, TenantID: tenant, HomeTenantID: home, SubjectID: subject, Role: role, JoinedAt: &joined}, nil
}
func (w *chatgateWorld) ListMemberships(_ context.Context, _, conversation string, _ chat.Page) (chat.ListMembershipsResponse, error) {
	joined := time.Now().Add(-time.Hour)
	var out chat.ListMembershipsResponse
	for subject, role := range w.members[conversation] {
		out.Memberships = append(out.Memberships, chat.Membership{ConversationID: conversation, TenantID: "tenant", HomeTenantID: "tenant", SubjectID: subject, Role: role, JoinedAt: &joined})
	}
	return out, nil
}

// chatgateReadable is the chat service as a person reaches it: a public
// channel is readable by anyone, a private one by its members.
type chatgateReadable struct{ world *chatgateWorld }

func (r chatgateReadable) GetConversation(_ context.Context, request chat.GetConversationRequest) (chat.Conversation, error) {
	kind, ok := r.world.kinds[request.ConversationID]
	if !ok {
		return chat.Conversation{}, chat.ErrNotFound
	}
	if _, member := r.world.members[request.ConversationID][request.Principal.SubjectID]; kind != chat.PublicChannel && !member {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{ID: request.ConversationID, TenantID: request.TenantID, Kind: kind}, nil
}

func chatgateWorldFixture() (*chatgateWorld, *ChatgateAuthority) {
	world := &chatgateWorld{
		roles: map[string][]string{"admin": {"hcm_admin"}, "manager": {"employee"}, "member": {"employee"}, "applicant": {"employee"}},
		people: []chatgatePerson{
			{Subject: "admin", Name: "Ada Admin", Team: "People", Location: "Berlin", Active: true},
			{Subject: "manager", Name: "Mia Manager", Team: "Payroll", Location: "Austin", Active: true},
			{Subject: "member", Name: "Max Member", Team: "Payroll", Location: "Austin", Active: true},
			{Subject: "applicant", Name: "Alex Applicant", Team: "Finance", Location: "Lisbon", Active: true},
			{Subject: "gone", Name: "Greta Gone", Team: "Legal", Location: "Oslo", Active: false},
		},
		kinds: map[string]chat.ConversationKind{"public": chat.PublicChannel, "private": chat.PrivateChannel, "dm": chat.Direct},
		members: map[string]map[string]chat.MembershipRole{
			"public":  {"manager": chat.Manager, "member": chat.Member},
			"private": {"manager": chat.Manager, "member": chat.Member},
			"dm":      {"manager": chat.Member, "member": chat.Member},
		},
	}
	return world, &ChatgateAuthority{Roles: world, Readable: chatgateReadable{world}, Store: world, People: world, Now: time.Now}
}

func chatgateAs(t *testing.T, subject string) context.Context {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "gate", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:gate"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(t.Context(), p)
}

// TestTodo_CHATGATE_006_Security_Authority: who the production authority lets build,
// review, read as an administrator and export: a channel's managers and a
// workspace administrator who can read the channel, and nobody else.
func TestTodo_CHATGATE_006_Security_Authority(t *testing.T) {
	_, authority := chatgateWorldFixture()
	scope := func(conversation string) chatgate.Scope {
		return chatgate.Scope{Tenant: "tenant", Conversation: conversation}
	}
	for _, permission := range []string{"admin", "export", "retention"} {
		for _, tc := range []struct {
			person, conversation string
			want                 bool
		}{
			{"manager", "public", true}, {"manager", "private", true},
			{"admin", "public", true},
			// An administrator who is not in a private channel does not administer
			// its gate: its answers are as private as the channel.
			{"admin", "private", false},
			{"member", "public", false}, {"member", "private", false},
			{"applicant", "public", false}, {"applicant", "private", false},
			// A gate is a channel's; a direct conversation has none.
			{"manager", "dm", false}, {"admin", "dm", false},
			{"manager", "unknown", false},
		} {
			err := authority.Check(chatgateAs(t, tc.person), chatgate.Actor{Tenant: "tenant", Person: tc.person}, scope(tc.conversation), permission)
			if (err == nil) != tc.want {
				t.Errorf("%s: %s in %s: %v, want allowed=%v", permission, tc.person, tc.conversation, err, tc.want)
			}
		}
	}
	// The roles are the signed-in person's: asked about somebody else, with the
	// asker's session, the authority says no.
	if err := authority.Check(chatgateAs(t, "member"), chatgate.Actor{Tenant: "tenant", Person: "admin"}, scope("public"), "admin"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an administrator's authority was borrowed: %v", err)
	}
	if err := authority.Check(t.Context(), chatgate.Actor{Tenant: "tenant", Person: "admin"}, scope("public"), "admin"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an administrator with no session administers: %v", err)
	}
	// Another workspace, and a permission nothing here grants.
	if err := authority.Check(chatgateAs(t, "manager"), chatgate.Actor{Tenant: "other", Person: "manager"}, scope("public"), "admin"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("another workspace's actor: %v", err)
	}
	for _, permission := range []string{"consumer:roles", "install", "anything"} {
		if err := authority.Check(chatgateAs(t, "manager"), chatgate.Actor{Tenant: "tenant", Person: "manager"}, scope("public"), permission); !errors.Is(err, chatgate.ErrDenied) {
			t.Errorf("permission %q is granted: %v", permission, err)
		}
	}
	// The privacy report is the workspace administrator's alone.
	all := chatgate.Scope{Tenant: "tenant", Conversation: "*"}
	if err := authority.Check(chatgateAs(t, "admin"), chatgate.Actor{Tenant: "tenant", Person: "admin"}, all, "privacy_report"); err != nil {
		t.Fatalf("the administrator's privacy report: %v", err)
	}
	if err := authority.Check(chatgateAs(t, "manager"), chatgate.Actor{Tenant: "tenant", Person: "manager"}, all, "privacy_report"); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("a manager's privacy report: %v", err)
	}
	// What a gate may ask, and how a private channel starts.
	if policy, err := authority.Policy(t.Context(), scope("public")); err != nil || policy.Private || policy.Ceiling != "INTERNAL" {
		t.Fatalf("public policy: %+v %v", policy, err)
	}
	if policy, err := authority.Policy(t.Context(), scope("private")); err != nil || !policy.Private {
		t.Fatalf("private policy: %+v %v", policy, err)
	}
	if _, err := authority.Policy(t.Context(), scope("dm")); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("a direct conversation has a gate policy: %v", err)
	}
	if _, err := authority.Policy(t.Context(), scope("unknown")); !errors.Is(err, chatgate.ErrNotFound) {
		t.Fatalf("an unknown conversation: %v", err)
	}
}

// TestTodo_CHATGATE_005_Security_Authority: who may see and answer a gate, and what the
// directory tells them. A public channel's questions are for anyone in the
// workspace; a private channel's for its members; nobody outside learns who
// else applied.
func TestTodo_CHATGATE_005_Security_Authority(t *testing.T) {
	world, authority := chatgateWorldFixture()
	check := func(person, conversation, permission string) error {
		return authority.Check(chatgateAs(t, person), chatgate.Actor{Tenant: "tenant", Person: person}, chatgate.Scope{Tenant: "tenant", Conversation: conversation}, permission)
	}
	for _, permission := range []string{"discover", "eligible"} {
		for _, tc := range []struct {
			person, conversation string
			want                 bool
		}{
			{"applicant", "public", true}, {"member", "public", true}, {"admin", "public", true},
			{"applicant", "private", false}, {"admin", "private", false},
			{"member", "private", true}, {"manager", "private", true},
			{"member", "dm", false}, {"applicant", "unknown", false},
		} {
			if err := check(tc.person, tc.conversation, permission); (err == nil) != tc.want {
				t.Errorf("%s: %s in %s: %v, want allowed=%v", permission, tc.person, tc.conversation, err, tc.want)
			}
		}
	}
	// A member-visible answer is for the people in the channel.
	if err := check("member", "public", chatgate.PermissionMember); err != nil {
		t.Fatalf("a member is not a member: %v", err)
	}
	if err := check("applicant", "public", chatgate.PermissionMember); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an applicant counts as a member: %v", err)
	}
	// A person's own submission is theirs wherever they stand with the channel.
	for _, permission := range []string{"self", "read_answers"} {
		if err := check("applicant", "private", permission); err != nil {
			t.Errorf("%s for one's own answers: %v", permission, err)
		}
	}

	// Facts come from the people directory: a team is the organisation unit.
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "public"}
	facts, err := authority.Facts(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, scope)
	if err != nil || facts["team"] != "Finance" || facts["location"] != "Lisbon" || facts["person"] != "applicant" {
		t.Fatalf("facts=%v err=%v", facts, err)
	}
	// A sample person's facts are read for the administrator who tries a rule.
	if facts, err = authority.Facts(chatgateAs(t, "manager"), chatgate.Actor{Tenant: "tenant", Person: "member"}, scope); err != nil || facts["team"] != "Payroll" {
		t.Fatalf("a sample person's facts=%v err=%v", facts, err)
	}
	// Somebody the directory does not hold, or who has left, has no facts.
	for _, person := range []string{"stranger", "gone"} {
		if facts, err = authority.Facts(chatgateAs(t, "manager"), chatgate.Actor{Tenant: "tenant", Person: person}, scope); err != nil || len(facts) != 0 {
			t.Fatalf("%s has facts %v (%v)", person, facts, err)
		}
	}
	// An answer that names a person, a team or a place must name a real one.
	raw := func(value string) json.RawMessage { b, _ := json.Marshal(value); return b }
	for _, tc := range []struct {
		kind, value string
		want        bool
	}{
		{"person", "member", true}, {"person", "stranger", false}, {"person", "gone", false},
		{"team", "Payroll", true}, {"team", "Legal", false}, {"team", "Nowhere", false},
		{"location", "Lisbon", true}, {"location", "Oslo", false},
		{"team", "", true},
	} {
		err := authority.Reference(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, scope, chatgate.Field{ID: "q", Kind: tc.kind}, raw(tc.value))
		if (err == nil) != tc.want {
			t.Errorf("%s %q: %v, want accepted=%v", tc.kind, tc.value, err, tc.want)
		}
		var field chatgate.FieldError
		if err != nil && (!errors.As(err, &field) || field.Field != "q") {
			t.Errorf("%s %q: the refusal does not name its question: %v", tc.kind, tc.value, err)
		}
	}
	if err = authority.Reference(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, scope, chatgate.Field{ID: "ack", Kind: "acknowledgement", DocumentID: "doc"}, json.RawMessage("true")); err == nil {
		t.Fatal("an acknowledgement of a document nothing checked was accepted")
	}

	// The directory: choices for everyone who may see the form, names only for
	// the people who administer it.
	choices, names, _, err := authority.GateDirectory(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, scope)
	if err != nil || len(choices["team"]) != 3 || len(choices["person"]) != 4 || len(choices["location"]) != 3 {
		t.Fatalf("an applicant's choices=%v err=%v", choices, err)
	}
	if len(names) != 1 || names["applicant"] != "Alex Applicant" {
		t.Fatalf("an applicant was given other people's names: %v", names)
	}
	for _, list := range choices {
		for _, choice := range list {
			if choice.ID == "gone" || choice.Label == "Legal" || choice.Label == "Oslo" {
				t.Fatalf("a person who left is offered as a choice: %+v", choice)
			}
		}
	}
	if choices["team"][0].Label != "Finance" || choices["team"][2].Label != "People" {
		t.Fatalf("teams are not in order: %+v", choices["team"])
	}
	_, names, _, err = authority.GateDirectory(chatgateAs(t, "manager"), chatgate.Actor{Tenant: "tenant", Person: "manager"}, scope)
	if err != nil || names["applicant"] != "Alex Applicant" || names["gone"] != "Greta Gone" {
		t.Fatalf("an administrator's names=%v err=%v", names, err)
	}
	// A private channel's directory is not for someone outside it.
	private := chatgate.Scope{Tenant: "tenant", Conversation: "private"}
	if _, _, _, err = authority.GateDirectory(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, private); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an outsider read a private channel's gate directory: %v", err)
	}
	// Who an applicant is waiting for: the channel's managers, by name.
	reviewers, err := authority.GateReviewers(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, scope)
	if err != nil || len(reviewers) != 1 || reviewers[0] != "Mia Manager" {
		t.Fatalf("reviewers=%v err=%v", reviewers, err)
	}
	if _, err = authority.GateReviewers(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, private); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("an outsider read a private channel's reviewers: %v", err)
	}
	// A directory that cannot be read fails the read; it is not an empty one.
	world.peopleError = errors.New("database is away")
	if _, err = authority.Facts(chatgateAs(t, "applicant"), chatgate.Actor{Tenant: "tenant", Person: "applicant"}, scope); !errors.Is(err, chatgate.ErrUnavailable) {
		t.Fatalf("facts from a directory that is away: %v", err)
	}
	// No worker records, no authority: the gate service stays uncomposed.
	if got := newChatgateAuthority(chatfilterHTTPFacts{}, chatgateReadable{world}, world, nil, time.Now); got != nil {
		t.Fatal("an authority was built without a people directory")
	}
	if got := newChatgateAuthority(currentRoleChatFacts{}, chatgateReadable{world}, world, nil, time.Now); got != nil {
		t.Fatal("an authority was built from facts with no worker records")
	}
	if got := newChatgateAuthority(world, chatgateReadable{world}, world, nil, time.Now); got == nil {
		t.Fatal("no authority was built from a people directory")
	}
}

// TestTodo_CHATGATE_006_Export: the export of every answer names people and
// questions as an administrator reads them and stays safe to open.
func TestTodo_CHATGATE_006_Export(t *testing.T) {
	raw := "person,field,answer\napplicant,team,Finance\napplicant,skills,\"[\"\"go\"\",\"\"sql\"\"]\"\nstranger,team,'=cmd\nmember,note,\"two\nlines\"\n"
	got, err := chatgateReadableCSV(raw, map[string]string{"applicant": "Alex Applicant", "member": "=Max"}, map[string]string{"team": "Which team are you in?", "skills": "Skills", "note": "+Notes"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(got)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"person", "question", "answer"},
		{"Alex Applicant", "Which team are you in?", "Finance"},
		{"Alex Applicant", "Skills", "go; sql"},
		// No name is known: the identifier stays. The service's guard stays too.
		{"stranger", "Which team are you in?", "'=cmd"},
		// A name and a question that would run as a formula are written as text.
		{"'=Max", "'+Notes", "two\nlines"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows=%q", rows)
	}
	for i := range want {
		if strings.Join(rows[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d: %q, want %q", i, rows[i], want[i])
		}
	}
	for _, broken := range []string{"", "person,field,answer\nonly,two\n"} {
		if _, err = chatgateReadableCSV(broken, nil, nil); err == nil {
			t.Errorf("a broken export %q was rewritten", broken)
		}
	}
}

// TestTodo_CHATGATE_005_Integration_Served: the gate service with the product's
// own authority on the real chat store. A manager builds and publishes a gate
// whose rule admits two teams; a person of one of them answers and is in the
// channel; a person of another waits for review; nobody outside reads a
// member-visible answer; a channel with no gate is joined as it always was.
func TestTodo_CHATGATE_005_Integration_Served(t *testing.T) {
	runtime, core, store := chatattach001Served(t)
	ctx := t.Context()
	for _, c := range []chat.Conversation{{ID: "gated", TenantID: "tenant", Kind: chat.PublicChannel, Name: "payroll-help", OwnerID: "manager", Revision: 1}, {ID: "open", TenantID: "tenant", Kind: chat.PublicChannel, Name: "open", OwnerID: "manager", Revision: 1}} {
		if _, err := store.CreateConversation(ctx, c, []chat.Membership{{TenantID: "tenant", ConversationID: c.ID, HomeTenantID: "tenant", SubjectID: "manager", Role: chat.Manager, HistoryVisibility: chat.FullHistory}}, "create-"+c.ID); err != nil {
			t.Fatal(err)
		}
	}
	world, _ := chatgateWorldFixture()
	repository := &chatstore.GateRepository{Store: runtime.store, Membership: store.GateMembership}
	authority := newChatgateAuthority(world, core, store, repository, time.Now)
	if authority == nil {
		t.Fatal("no authority")
	}
	gates := &chatgate.Service{Repository: repository, Authority: authority, Registry: chatgate.NewRegistry()}
	join := ChatgateJoinAdapter{Service: gates, InForce: repository.GateInForce}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "gated"}
	actor := func(person string) chatgate.Actor { return chatgate.Actor{Tenant: "tenant", Person: person} }
	command := func(person, key string, revision uint64) chatgate.Command {
		return chatgate.Command{Scope: scope, Actor: actor(person), Key: key, ExpectedRevision: revision}
	}

	// Before any gate: nothing is in force, and joining is not asked about.
	if gated, err := repository.GateInForce(ctx, scope); err != nil || gated {
		t.Fatalf("a channel with no gate is gated: %v %v", gated, err)
	}
	guest := chat.Membership{TenantID: "tenant", ConversationID: "open", HomeTenantID: "partner", SubjectID: "guest"}
	if err := join.CheckMembership(ctx, chat.Principal{}, guest); err != nil {
		t.Fatalf("a guest's add to an ungated channel was refused by the gate port: %v", err)
	}

	definition := chatgate.Definition{Mode: "rule", Purpose: "Payroll questions from the teams that run payroll",
		Fields: []chatgate.Field{
			{ID: "why", Kind: "short_text", KindVersion: "1.0.0", Label: "What do you need help with?", Purpose: "So the right person answers", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true, Members: true}, RetentionDays: 30},
			{ID: "private", Kind: "short_text", KindVersion: "1.0.0", Label: "Anything else for the managers?", Purpose: "Context", DataClass: "INTERNAL", Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30},
		},
		Rules: []chatgate.Rule{{When: chatgate.Expression{Operator: "in", Fact: "team", Values: []string{"Payroll", "Finance"}}, Outcome: "admitted", Reason: "Your team runs payroll"}}}
	// Only a manager builds; a member and an administrator of the workspace who
	// is in a public channel's workspace may too, a plain member may not.
	if err := gates.Define(chatgateAs(t, "member"), command("member", "define", 0), definition); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("a member built a gate: %v", err)
	}
	if err := gates.Define(chatgateAs(t, "manager"), command("manager", "define", 0), definition); err != nil {
		t.Fatalf("a manager could not build a gate: %v", err)
	}
	if _, err := gates.Publish(chatgateAs(t, "manager"), command("manager", "publish", 1), chatgate.Version{Major: 1}); err != nil {
		t.Fatal(err)
	}
	if gated, err := repository.GateInForce(ctx, scope); err != nil || !gated {
		t.Fatalf("a published gate is not in force: %v %v", gated, err)
	}
	// Joining without answering is refused, for the person and for a guest.
	plain := chat.Membership{TenantID: "tenant", ConversationID: "gated", HomeTenantID: "tenant", SubjectID: "applicant"}
	if err := join.CheckMembership(chatgateAs(t, "applicant"), chat.Principal{TenantID: "tenant", SubjectID: "applicant"}, plain); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("joining a gated channel without answering: %v", err)
	}
	gatedGuest := guest
	gatedGuest.ConversationID = "gated"
	if err := join.CheckMembership(ctx, chat.Principal{}, gatedGuest); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a guest's add to a gated channel: %v", err)
	}

	answers := func(why string) map[string]json.RawMessage {
		b, _ := json.Marshal(why)
		return map[string]json.RawMessage{"why": b, "private": json.RawMessage(`"for managers only"`)}
	}
	// Finance is in the rule's list: admitted at once, and in the channel.
	admitted, err := gates.Submit(chatgateAs(t, "applicant"), command("applicant", "submit-applicant", 2), "1.0.0", answers("A payslip question"))
	if err != nil || admitted.Status != "admitted" || admitted.Reason != "Your team runs payroll" {
		t.Fatalf("a Finance person's request: %+v %v", admitted, err)
	}
	if m, err := store.GetMembership(ctx, "tenant", "gated", "tenant", "applicant"); err != nil || m.LeftAt != nil {
		t.Fatalf("the admitted person is not in the channel: %+v %v", m, err)
	}
	if err = join.CheckMembership(chatgateAs(t, "applicant"), chat.Principal{TenantID: "tenant", SubjectID: "applicant"}, plain); err != nil {
		t.Fatalf("the admitted person's membership is refused: %v", err)
	}
	// People is not: the request waits for a person, and they are not in.
	waiting, err := gates.Submit(chatgateAs(t, "admin"), command("admin", "submit-admin", 3), "1.0.0", answers("Curious"))
	if err != nil || waiting.Status != "review" {
		t.Fatalf("a People person's request: %+v %v", waiting, err)
	}
	if _, err = store.GetMembership(ctx, "tenant", "gated", "tenant", "admin"); err == nil {
		t.Fatal("a person waiting for review is in the channel")
	}
	// The manager admits them with a reason.
	if reviewed, err := gates.Review(chatgateAs(t, "manager"), command("manager", "review-admin", 4), waiting.ID, waiting.Revision, true, "Welcome"); err != nil || reviewed.Status != "admitted" {
		t.Fatalf("review: %+v %v", reviewed, err)
	}

	// Reading answers: one's own in full; a member reads another's member-visible
	// field only; someone outside reads nothing of another person's.
	read := func(reader, person string) (map[string]json.RawMessage, error) {
		return gates.ReadAnswers(chatgateAs(t, reader), chatgate.ReadRequest{Actor: actor(reader), Scope: scope, Person: person, Purpose: "test"})
	}
	if own, err := read("applicant", "applicant"); err != nil || len(own) != 2 {
		t.Fatalf("one's own answers: %v %v", own, err)
	}
	if theirs, err := read("applicant", "admin"); err != nil || len(theirs) != 1 || theirs["private"] != nil {
		t.Fatalf("a member's view of another member's answers: %v %v", theirs, err)
	}
	if outside, err := read("member", "applicant"); err != nil || len(outside) != 0 {
		t.Fatalf("someone outside the channel read another person's answers: %v %v", outside, err)
	}
	if manager, err := read("manager", "applicant"); err != nil || len(manager) != 2 {
		t.Fatalf("a manager's view: %v %v", manager, err)
	}
	// The export is the managers'.
	if !gates.CanExport(chatgateAs(t, "manager"), actor("manager"), scope) || gates.CanExport(chatgateAs(t, "applicant"), actor("applicant"), scope) {
		t.Fatal("export is not the managers' alone")
	}
	app := &ChatgateApplication{Service: gates, Directory: authority}
	reply, err := app.GateRequest(chatgateAs(t, "manager"), ChatgateRequest{Conversation: "gated", Action: "export"})
	if err != nil {
		t.Fatal(err)
	}
	export, _ := reply.Result.(string)
	if !strings.HasPrefix(export, "person,question,answer\n") || !strings.Contains(export, "Alex Applicant,What do you need help with?,A payslip question") || strings.Contains(export, "applicant,why") {
		t.Fatalf("the export is not readable: %q", export)
	}
	if _, err = app.GateRequest(chatgateAs(t, "applicant"), ChatgateRequest{Conversation: "gated", Action: "export"}); !errors.Is(err, chatgate.ErrDenied) {
		t.Fatalf("a member exported: %v", err)
	}
	// The applicant's own view names who decides and never the rules.
	view, err := app.GateRequest(chatgateAs(t, "member"), ChatgateRequest{Conversation: "gated", Action: "get", Locale: "en-US"})
	if err != nil || view.View == nil || view.View.Administrator || len(view.View.Reviewers) != 1 || view.View.Reviewers[0] != "Mia Manager" {
		t.Fatalf("an applicant's view: %+v %v", view.View, err)
	}
	for _, d := range view.View.Gate.Versions {
		if len(d.Rules) != 0 {
			t.Fatal("an applicant was shown the rules")
		}
	}
	if len(view.View.Directory["team"]) == 0 || len(view.View.Names) > 1 {
		t.Fatalf("an applicant's directory: %v names=%v", view.View.Directory, view.View.Names)
	}

	// The list of gates: what Browse says before anything is pressed. It is a
	// read by GET, for no one conversation, and it is unavailable (not empty)
	// where nothing reads it.
	if _, err = app.GateRequest(chatgateAs(t, "member"), ChatgateRequest{Action: chatgateBrowseAction}); !errors.Is(err, chatgate.ErrUnavailable) {
		t.Fatalf("the list with no reader: %v", err)
	}
	app.Summaries = repository
	surface := integrate2GateSurface{ChatgateSurface: app, Routes: chatgateNoRoutes{}}
	listed, err := surface.GateRequest(chatgateAs(t, "member"), ChatgateRequest{Action: chatgateBrowseAction})
	if err != nil {
		t.Fatalf("the list through the served surface: %v", err)
	}
	summaries, _ := listed.Result.([]chatstore.GateSummary)
	if len(summaries) != 1 || summaries[0].Conversation != "gated" || summaries[0].Questions != 2 || summaries[0].Mode != "rule" || summaries[0].Member {
		t.Fatalf("the list: %+v", listed.Result)
	}
	get := httptest.NewRequest(http.MethodGet, ChatgatePath+"?list=1", nil).WithContext(chatgateAs(t, "member"))
	w := httptest.NewRecorder()
	ChatgateHTTP{Surface: surface}.ServeHTTP(w, get)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"conversation":"gated"`) || !strings.Contains(w.Body.String(), `"questions":2`) || strings.Contains(w.Body.String(), "Your team runs payroll") {
		t.Fatalf("GET ?list=1: %d %s", w.Code, w.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, ChatgatePath, strings.NewReader(`{"Action":"browse"}`)).WithContext(chatgateAs(t, "member"))
	w = httptest.NewRecorder()
	ChatgateHTTP{Surface: surface}.ServeHTTP(w, post)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("the list by POST: %d %s", w.Code, w.Body.String())
	}
	// A read of one gate still needs its conversation.
	w = httptest.NewRecorder()
	ChatgateHTTP{Surface: surface}.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ChatgatePath, nil).WithContext(chatgateAs(t, "member")))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET with no conversation: %d", w.Code)
	}
}

// chatgateNoRoutes fails every lease: the list of gates must not ask for one.
type chatgateNoRoutes struct{}

func (chatgateNoRoutes) ChatWriteContext(context.Context, string, string) (context.Context, error) {
	return nil, chat.ErrUnavailable
}
