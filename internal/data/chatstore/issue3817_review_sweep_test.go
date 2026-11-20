package chatstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

// TestIssue3817_SubmitAndSweepTellManagers_Integration drives the real service
// over the real repository: a review-mode answer tells the channel's manager and
// nobody else; the sweep tells them again after the deadline, once, and the
// applicant never learns of their own request. Nested store work (the notice is
// written while the sweep's own transaction is open) must not deadlock.
func TestIssue3817_SubmitAndSweepTellManagers_Integration(t *testing.T) {
	adapter := adapterDB(t)
	store := adapter.Store
	moderated := NewModeratedAdapter(adapter)
	chatmodChannel(t, moderated, "gated", "gated", chat.PrivateChannel, false, "owner", "bystander")
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repo := &GateRepository{Store: store}
	service := &chatgate.Service{Repository: repo, Registry: chatgate.NewRegistry(), Authority: chatgateAuthority{}, Notifier: repo, Clock: func() time.Time { return now }}
	scope := chatgate.Scope{Tenant: "tenant-a", Conversation: "gated"}
	owner := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant-a", Person: "owner"}, Key: "define"}
	d := chatgate.Definition{Mode: "review", Purpose: "Join discussion", Fields: []chatgate.Field{{ID: "intro", Kind: "short_text", KindVersion: "1.0.0", Label: "Introduction", Purpose: "Welcome", DataClass: "INTERNAL", Required: true, Visibility: chatgate.Visibility{Administrators: true}, RetentionDays: 30}}}
	if e := service.Define(t.Context(), owner, d); e != nil {
		t.Fatal(e)
	}
	owner.Key, owner.ExpectedRevision = "publish", 1
	if _, e := service.Publish(t.Context(), owner, chatgate.Version{Major: 1}); e != nil {
		t.Fatal(e)
	}
	owner.Key, owner.ExpectedRevision = "deadlines", 2
	if e := service.SetQueueDeadlines(t.Context(), owner, time.Hour, 3*time.Hour); e != nil {
		t.Fatal(e)
	}
	notices := func(subject string) []chat.ModerationNotice {
		t.Helper()
		out, err := moderated.ModerationNotices(t.Context(), chat.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	applicant := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant-a", Person: "worker"}, Key: "submit", ExpectedRevision: 3}
	sub, e := service.Submit(t.Context(), applicant, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello, a private answer"`)})
	if e != nil || sub.Status != "review" {
		t.Fatalf("submission %+v %v", sub, e)
	}
	got := notices("owner")
	if len(got) != 1 || got[0].Outcome != chat.ModerationOutcomeGateReview || got[0].ConversationID != "gated" || got[0].Reason != "" {
		t.Fatalf("manager notices after submit: %+v", got)
	}
	for _, other := range []string{"worker", "bystander"} {
		if rows := notices(other); len(rows) != 0 {
			t.Fatalf("%s was told of the request: %+v", other, rows)
		}
	}
	// A replay of the same request is the same notice.
	if _, e = service.Submit(t.Context(), applicant, "1.0.0", map[string]json.RawMessage{"intro": json.RawMessage(`"Hello, a private answer"`)}); e != nil {
		t.Fatal(e)
	}
	if got = notices("owner"); len(got) != 1 {
		t.Fatalf("a replay raised a second notice: %+v", got)
	}

	sweeper := chatgate.Actor{Tenant: "tenant-a", Person: "sweeper"}
	if o, es, e := service.SweepQueue(t.Context(), sweeper, scope); e != nil || o != 0 || es != 0 {
		t.Fatalf("early sweep %d %d %v", o, es, e)
	}
	now = now.Add(90 * time.Minute)
	if o, es, e := service.SweepQueue(t.Context(), sweeper, scope); e != nil || o != 1 || es != 0 {
		t.Fatalf("overdue sweep %d %d %v", o, es, e)
	}
	if o, es, e := service.SweepQueue(t.Context(), sweeper, scope); e != nil || o != 0 || es != 0 {
		t.Fatalf("a second sweep repeated the notice %d %d %v", o, es, e)
	}
	now = now.Add(2 * time.Hour)
	if o, es, e := service.SweepQueue(t.Context(), sweeper, scope); e != nil || o != 0 || es != 1 {
		t.Fatalf("escalation sweep %d %d %v", o, es, e)
	}
	got = notices("owner")
	seen := map[string]int{}
	for _, n := range got {
		seen[n.Outcome]++
		if n.Reason != "" || n.PostID != "" {
			t.Fatalf("a notice carries content: %+v", n)
		}
	}
	if len(got) != 3 || seen[chat.ModerationOutcomeGateReview] != 1 || seen[chat.ModerationOutcomeGateOverdue] != 1 || seen[chat.ModerationOutcomeGateEscalated] != 1 {
		t.Fatalf("manager should hold one notice per level: %+v", got)
	}
	if rows := notices("worker"); len(rows) != 0 {
		t.Fatalf("the applicant was told of their own request: %+v", rows)
	}
	// Another workspace sees none of it, even for a person of the same name.
	other, err := moderated.ModerationNotices(t.Context(), chat.Principal{TenantID: "tenant-b", SubjectID: "owner"}, "tenant-b")
	if err != nil || len(other) != 0 {
		t.Fatalf("tenant-b read tenant-a's notices: %+v %v", other, err)
	}
}
