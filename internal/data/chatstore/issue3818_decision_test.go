package chatstore

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// TestIssue3818_RequestStateAndDecisionNotice_Integration drives the real
// service over the real repository: an applicant to a review-mode channel sees
// their own request waiting in the list of gates (and nobody else's), is told
// by a private notice when it is decided, sees the reason on the list when it
// is declined, and another workspace sees none of it.
func TestIssue3818_RequestStateAndDecisionNotice_Integration(t *testing.T) {
	store, service, command := chatgateFixture(t)
	ctx := t.Context()
	repo := service.Repository.(*GateRepository)
	service.Decisions = repo
	adapter := &Adapter{Store: store}
	moderated := NewModeratedAdapter(adapter)
	seedConversationRow(t, store, Conversation{ID: "review-room", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}})
	scope := chatgate.Scope{Tenant: "tenant-a", Conversation: "review-room"}
	owner := chatgate.Command{Scope: scope, Actor: command.Actor, Key: "define"}
	d := chatgate.Definition{Mode: "review", Purpose: "Review room", Fields: []chatgate.Field{{ID: "why", Kind: "short_text", KindVersion: "1.0.0", Label: "Why", Purpose: "Context", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30}}}
	if err := service.Define(ctx, owner, d); err != nil {
		t.Fatal(err)
	}
	owner.Key, owner.ExpectedRevision = "publish", 1
	if _, err := service.Publish(ctx, owner, chatgate.Version{Major: 1}); err != nil {
		t.Fatal(err)
	}
	find := func(person string) GateSummary {
		t.Helper()
		rows, err := repo.GateSummaries(ctx, "tenant-a", person)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Conversation == "review-room" {
				return row
			}
		}
		t.Fatalf("%s does not see the gate: %+v", person, rows)
		return GateSummary{}
	}
	notices := func(subject string) []chat.ModerationNotice {
		t.Helper()
		out, err := moderated.ModerationNotices(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if row := find("linh"); row.Request != "" {
		t.Fatalf("a person who has not asked has a request: %+v", row)
	}

	apply := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant-a", Person: "linh"}, Key: "submit", ExpectedRevision: 2}
	sub, err := service.Submit(ctx, apply, "1.0.0", map[string]json.RawMessage{"why": json.RawMessage(`"I work in sales"`)})
	if err != nil || sub.Status != "review" {
		t.Fatalf("submit: %+v %v", sub, err)
	}
	if row := find("linh"); row.Request != "review" || row.RequestReason != "" || row.Member {
		t.Fatalf("applicant's own request: %+v", row)
	}
	if row := find("someone-else"); row.Request != "" {
		t.Fatalf("another person sees linh's request: %+v", row)
	}
	if rows := notices("linh"); len(rows) != 0 {
		t.Fatalf("told before a decision: %+v", rows)
	}

	revision := func() uint64 {
		t.Helper()
		g, err := service.Get(ctx, command.Actor, scope, true)
		if err != nil {
			t.Fatal(err)
		}
		return g.Revision
	}
	decide := chatgate.Command{Scope: scope, Actor: command.Actor, Key: "decline", ExpectedRevision: revision()}
	if _, err = service.Review(ctx, decide, sub.ID, sub.Revision, false, "Sales has its own channel"); err != nil {
		t.Fatal(err)
	}
	got := notices("linh")
	if len(got) != 1 || got[0].Outcome != chat.ModerationOutcomeGateDeclined || got[0].ConversationID != "review-room" || got[0].Reason != "Sales has its own channel" {
		t.Fatalf("decline notice: %+v", got)
	}
	if row := find("linh"); row.Request != "declined" || row.RequestReason != "Sales has its own channel" {
		t.Fatalf("declined request on the list: %+v", row)
	}
	// Replaying the notice is the same notice; nobody else is told.
	if err = repo.NotifyDecision(ctx, chatgate.DecisionNotice{Scope: scope, SubmissionID: sub.ID, Revision: sub.Revision + 1, Person: "linh", Reason: "Sales has its own channel"}); err != nil {
		t.Fatal(err)
	}
	if got = notices("linh"); len(got) != 1 {
		t.Fatalf("replay raised another notice: %+v", got)
	}
	for _, other := range []string{"owner", "someone-else"} {
		if rows := notices(other); len(rows) != 0 {
			t.Fatalf("%s was told of linh's decision: %+v", other, rows)
		}
	}

	// She answers again and is let in.
	apply.Key, apply.ExpectedRevision = "submit-2", revision()
	again, err := service.Submit(ctx, apply, "1.0.0", map[string]json.RawMessage{"why": json.RawMessage(`"Sales asked me to"`)})
	if err != nil {
		t.Fatal(err)
	}
	decide.Key, decide.ExpectedRevision = "admit", revision()
	if _, err = service.Review(ctx, decide, again.ID, again.Revision, true, "Welcome"); err != nil {
		t.Fatal(err)
	}
	if row := find("linh"); row.Request != "" || !row.Member {
		t.Fatalf("admitted member keeps a request state: %+v", row)
	}
	got = notices("linh")
	if len(got) != 2 {
		t.Fatalf("notices after admission: %+v", got)
	}
	admitted := 0
	for _, n := range got {
		if n.Outcome == chat.ModerationOutcomeGateAdmitted && n.ConversationID == "review-room" {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("admission notice missing: %+v", got)
	}

	// Another workspace's channel of the same id tells nobody here, and a
	// notice with no person is refused.
	if err = repo.NotifyDecision(ctx, chatgate.DecisionNotice{Scope: chatgate.Scope{Tenant: "tenant-b", Conversation: "review-room"}, SubmissionID: "x", Revision: 2, Person: "linh", Admitted: true}); err != nil {
		t.Fatal(err)
	}
	if got = notices("linh"); len(got) != 2 {
		t.Fatalf("another workspace changed the notices: %+v", got)
	}
	if err = repo.NotifyDecision(ctx, chatgate.DecisionNotice{Scope: scope, SubmissionID: "x", Revision: 2}); err == nil {
		t.Fatal("a notice for nobody was accepted")
	}
}
