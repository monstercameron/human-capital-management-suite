package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_AGENTUX_053_GeneralHistory_Integration seeds #general with the dated
// history the demo shows and proves it is written once, names only a person who
// shares their birthday, carries no mention that could start an agent run, links
// its sources, and removes nothing that was already there.
func TestTodo_AGENTUX_053_GeneralHistory_Integration(t *testing.T) {
	fixture, _, _ := agentUXAmbientFixture(t)
	store := fixture.DB.(*chatstore.Store)
	ctx := t.Context()
	now := time.Date(2026, 10, 2, 13, 0, 0, 0, time.UTC)
	// luis's birthday was ten days ago and he shares it; peer's was three days
	// ago and peer does not: only luis may be named.
	birthday := now.AddDate(0, 0, -10)
	recent := now.AddDate(0, 0, -3)
	members := []localAgentDemoHistoryPerson{
		{Key: "author", Name: "Walt Brennan", Month: time.January, Day: 1, Shares: true},
		{Key: "luis", Name: "Luis Ortega", Month: birthday.Month(), Day: birthday.Day(), Shares: true},
		{Key: "peer", Name: "Priya Raman", Month: recent.Month(), Day: recent.Day(), Shares: false},
	}
	if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('old-test-question','tenant-a','general','author','tenant-a',1,'@Policy Helper does this still fail?')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deps := localAgentDemoHistoryDeps{Chat: store, Tenant: "tenant-a", General: "general", Admin: "author", OwnerName: "Walt Brennan", Members: members, Now: now,
		DocumentLink: func(_ context.Context, probe string) (string, string, error) {
			if probe == localAgentDemoHolidayProbe {
				return "doc-holiday", localAgentDemoHolidayTitle, nil
			}
			return "doc-pto", localAgentDemoPolicyTitle, nil
		}}
	created, err := ensureLocalAgentDemoGeneralHistory(ctx, deps)
	if err != nil || created != 8 {
		t.Fatalf("first run created %d posts: %v", created, err)
	}
	if again, err := ensureLocalAgentDemoGeneralHistory(ctx, deps); err != nil || again != 0 {
		t.Fatalf("a second run created %d posts: %v", again, err)
	}
	type row struct {
		id, author, body, parent string
		at                       time.Time
	}
	var rows []row
	if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		list, err := tx.Query(ctx, `SELECT id,author_id,body,COALESCE(parent_id,''),created_at FROM chat_post WHERE tenant_id='tenant-a' AND conversation_id='general' AND tombstoned=false ORDER BY created_at`)
		if err != nil {
			return err
		}
		defer list.Close()
		for list.Next() {
			var r row
			if err := list.Scan(&r.id, &r.author, &r.body, &r.parent, &r.at); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return list.Err()
	}); err != nil {
		t.Fatal(err)
	}
	byAuthor := map[string]int{}
	var oldKept bool
	ids := map[string]row{}
	for _, r := range rows {
		ids[r.id] = r
		byAuthor[r.author]++
		if r.id == "old-test-question" {
			oldKept = true
			continue
		}
		if strings.Contains(r.body, "@") || strings.Contains(r.body, "Priya Raman") || strings.Contains(r.body, "20") && strings.Contains(strings.ToLower(r.body), "birth year") {
			t.Fatalf("a seeded message carries a mention or names an opted-out person: %q", r.body)
		}
		if r.at.After(now) || r.at.Before(now.AddDate(0, 0, -45)) {
			t.Fatalf("a seeded message is dated %v", r.at)
		}
	}
	if !oldKept {
		t.Fatal("the history removed a post that was already there")
	}
	if byAuthor["policy-helper"] != 3 || byAuthor["assistant"] != 1 || byAuthor["birthday-buddy"] != 1 || byAuthor["luis"]+byAuthor["peer"] != 3 {
		t.Fatalf("authors %v", byAuthor)
	}
	linked, declined := 0, 0
	for _, r := range rows {
		if r.author == "policy-helper" {
			question, ok := ids[r.parent]
			if !ok || !question.at.Before(r.at) {
				t.Fatalf("an answer is not under its question: %+v", r)
			}
			if strings.Contains(r.body, "/workspace/app/docs?document=doc-pto") {
				linked++
			} else if strings.Contains(r.body, "can't answer that") {
				declined++
			}
		}
		if r.author == "assistant" {
			announcement, ok := chatui.DecodeAnnouncementMessageBody(r.body)
			if !ok || len(announcement.Sources) != 1 || announcement.Sources[0].Title != localAgentDemoHolidayTitle || !strings.Contains(announcement.Text, "Thanksgiving") {
				t.Fatalf("holiday announcement %+v ok=%t", announcement, ok)
			}
		}
		if r.author == "birthday-buddy" {
			announcement, ok := chatui.DecodeAnnouncementMessageBody(r.body)
			if !ok || announcement.Text != "Happy birthday, Luis Ortega! 🎂 Hope you have a wonderful day!" || !r.at.Equal(time.Date(birthday.Year(), birthday.Month(), birthday.Day(), 8, 0, 0, 0, time.UTC)) {
				t.Fatalf("birthday post %+v at %v ok=%t", announcement, r.at, ok)
			}
		}
	}
	if linked != 2 || declined != 1 {
		t.Fatalf("answers with a linked source=%d, declined with a reason=%d", linked, declined)
	}
	// With nobody who shares a recent birthday, no birthday is invented.
	for i := range members {
		members[i].Shares = false
	}
	if person, _, found := localAgentDemoLatestBirthday(members, now); found {
		t.Fatalf("named %+v who does not share a birthday", person)
	}
}
