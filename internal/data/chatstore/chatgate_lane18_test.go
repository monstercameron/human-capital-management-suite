package chatstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATGATE_005_Integration_Summaries: the list of gates a person may
// know of holds the gates in force of public channels and of the channels they
// are in, with the count of questions and the stated purposes, and tells a
// member when the questions changed since they answered. It holds nothing of
// a private channel they are not in, of a paused gate, of another workspace or
// of anybody's answers.
func TestTodo_CHATGATE_005_Integration_Summaries(t *testing.T) {
	store, service, command := chatgateFixture(t)
	ctx := t.Context()
	repo := service.Repository.(*GateRepository)
	scope := command.Scope

	// One gate in force on a public channel: anyone in the workspace sees it.
	if gated, err := repo.GateInForce(ctx, scope); err != nil || !gated {
		t.Fatalf("in force: %v %v", gated, err)
	}
	if gated, err := repo.GateInForce(ctx, chatgate.Scope{Tenant: "tenant-a", Conversation: "no-such-channel"}); err != nil || gated {
		t.Fatalf("a channel with no gate: %v %v", gated, err)
	}
	if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		payload, _ := json.Marshal(teamPayload{Purpose: "Talk about the product"})
		_, err := tx.Exec(ctx, `INSERT INTO chat_channel_widget(tenant_id,conversation_id,kind,revision,pinned,payload_json) VALUES('tenant-a','gated','TEAM',2,false,$1)`, payload)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if purpose, err := repo.GatePurpose(ctx, scope); err != nil || purpose != "Talk about the product" {
		t.Fatalf("purpose=%q err=%v", purpose, err)
	}
	list, err := repo.GateSummaries(ctx, "tenant-a", "stranger")
	if err != nil || len(list) != 1 {
		t.Fatalf("a non-member's list: %+v %v", list, err)
	}
	got := list[0]
	if got.Conversation != "gated" || got.Questions != 1 || got.Purpose != "Join discussion" || got.ChannelPurpose != "Talk about the product" || got.Mode != "automatic" || got.Member || got.AnswerAgain || !got.AnswerBy.IsZero() {
		t.Fatalf("summary=%+v", got)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{"Introduction", "Welcome", "owner", "intro"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(secret)) {
			t.Fatalf("the summary carries %q: %s", secret, raw)
		}
	}
	if other, err := repo.GateSummaries(ctx, "tenant-b", "stranger"); err != nil || len(other) != 0 {
		t.Fatalf("another workspace's list: %+v %v", other, err)
	}

	// A private channel's gate is listed for its members only.
	seedConversationRow(t, store, Conversation{ID: "private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", Role: "manager", State: "active"}, {MemberID: "insider", Role: "member", State: "active"}})
	private := chatgate.Command{Scope: chatgate.Scope{Tenant: "tenant-a", Conversation: "private"}, Actor: command.Actor, Key: "define-private"}
	d := chatgate.Definition{Mode: "review", Purpose: "Private matters", Fields: []chatgate.Field{{ID: "why", Kind: "short_text", KindVersion: "1.0.0", Label: "Why", Purpose: "Context", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30}}}
	if err = service.Define(ctx, private, d); err != nil {
		t.Fatal(err)
	}
	private.Key, private.ExpectedRevision = "publish-private", 1
	if _, err = service.Publish(ctx, private, chatgate.Version{Major: 1}); err != nil {
		t.Fatal(err)
	}
	if list, err = repo.GateSummaries(ctx, "tenant-a", "stranger"); err != nil || len(list) != 1 {
		t.Fatalf("an outsider sees a private channel's gate: %+v %v", list, err)
	}
	if list, err = repo.GateSummaries(ctx, "tenant-a", "insider"); err != nil || len(list) != 2 {
		t.Fatalf("a member's list: %+v %v", list, err)
	}

	// A member answers; then a major version asks them to answer again by a date.
	answer := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant-a", Person: "joiner"}, Key: "submit", ExpectedRevision: 2}
	if _, err = service.Submit(ctx, answer, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"hello"`)}); err != nil {
		t.Fatal(err)
	}
	find := func(person string) GateSummary {
		t.Helper()
		rows, err := repo.GateSummaries(ctx, "tenant-a", person)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.Conversation == "gated" {
				return row
			}
		}
		t.Fatalf("%s does not see the gate: %+v", person, rows)
		return GateSummary{}
	}
	if row := find("joiner"); !row.Member || row.AnswerAgain {
		t.Fatalf("a member whose answers are current: %+v", row)
	}
	next := chatgate.Definition{Mode: "automatic", Purpose: "Join discussion", Fields: []chatgate.Field{
		{ID: "intro", Kind: "short_text", KindVersion: "1.0.0", Label: "Introduction", Purpose: "Welcome", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30},
		{ID: "team", Kind: "short_text", KindVersion: "1.0.0", Label: "Team", Purpose: "Routing", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30}}}
	command.Key, command.ExpectedRevision = "define-2", 3
	if err = service.Define(ctx, command, next); err != nil {
		t.Fatal(err)
	}
	command.Key, command.ExpectedRevision = "publish-2", 4
	if _, err = service.Publish(ctx, command, chatgate.Version{Major: 2}); err != nil {
		t.Fatal(err)
	}
	row := find("joiner")
	want := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, 30)
	if !row.AnswerAgain || row.Questions != 2 || !row.AnswerBy.Equal(want) {
		t.Fatalf("after a major version: %+v, want answer again by %s", row, want)
	}
	// Somebody who is not in the channel is not told to answer "again".
	if row = find("stranger"); row.AnswerAgain || !row.AnswerBy.IsZero() || row.Questions != 2 {
		t.Fatalf("a non-member after a major version: %+v", row)
	}
	// A paused gate is not in force and not listed.
	command.Key, command.ExpectedRevision = "pause", 5
	if err = service.Lifecycle(ctx, command, "paused"); err != nil {
		t.Fatal(err)
	}
	if gated, err := repo.GateInForce(ctx, scope); err != nil || gated {
		t.Fatalf("a paused gate is in force: %v %v", gated, err)
	}
	if list, err = repo.GateSummaries(ctx, "tenant-a", "joiner"); err != nil || len(list) != 0 {
		t.Fatalf("a paused gate is listed: %+v %v", list, err)
	}
	if _, err = repo.GateSummaries(ctx, "tenant-a", ""); err == nil {
		t.Fatal("a list for nobody was read")
	}
}
