package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_CHATLANG_006_Store(t *testing.T) {
	s, scope, _ := chatrenderDB(t)
	ctx := context.Background()
	tenant := scope.Tenant
	w, err := s.ChatlangWorkspace(ctx, tenant)
	if err != nil || w.Enabled || !w.ExternalAllowed || w.Budget() != chatlang.DefaultMonthlyBudgetMicros {
		t.Fatalf("a workspace that was never set has translation off: %+v %v", w, err)
	}
	set, err := s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Enabled: true, Languages: []string{"fr", "de"}, BudgetMicros: 2500, ExternalAllowed: false, Formality: map[string]string{"de": "formal"}})
	if err != nil || !set.Enabled || set.ExternalAllowed || set.BudgetMicros != 2500 || len(set.Languages) != 2 || set.Languages[0] != "de" || set.Formality["de"] != "formal" || set.Revision != 1 {
		t.Fatalf("workspace %+v %v", set, err)
	}
	if set, err = s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Enabled: false}); err != nil || set.Enabled || set.Revision != 2 {
		t.Fatalf("turning it off: %+v %v", set, err)
	}
	if _, err = s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Languages: []string{"xx"}}); !errors.Is(err, chatlang.ErrInvalid) {
		t.Fatal("an unsupported language was stored", err)
	}

	channel, err := s.ChatlangChannel(ctx, tenant, scope.Conversation)
	if err != nil || channel.Translation != chatlang.Inherit || channel.External != chatlang.ExternalInherit {
		t.Fatalf("channel default %+v %v", channel, err)
	}
	if err = s.PutChatlangChannel(ctx, tenant, scope.Conversation, "alice", chatlang.Channel{Translation: chatlang.Off, External: chatlang.ExternalBarred}); err != nil {
		t.Fatal(err)
	}
	if channel, err = s.ChatlangChannel(ctx, tenant, scope.Conversation); err != nil || channel.Translation != chatlang.Off || channel.External != chatlang.ExternalBarred {
		t.Fatalf("channel %+v %v", channel, err)
	}
	if err = s.PutChatlangChannel(ctx, tenant, "no-such-room", "alice", chatlang.Channel{Translation: chatlang.Off}); !errors.Is(err, chatlang.ErrInvalid) {
		t.Fatal("a setting for a channel that does not exist was stored", err)
	}
	if other, err := s.ChatlangChannel(ctx, "tenant-b", scope.Conversation); err != nil || other.Translation != chatlang.Inherit {
		t.Fatalf("another workspace saw this one's setting: %+v %v", other, err)
	}

	id, err := s.AddChatlangTerm(ctx, tenant, "alice", chatlang.Term{Source: "Acme Cloud"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddChatlangTerm(ctx, tenant, "alice", chatlang.Term{Source: "time off", Language: "de", Target: "Urlaub"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddChatlangTerm(ctx, tenant, "alice", chatlang.Term{Source: "bad", Language: "de"}); !errors.Is(err, chatlang.ErrInvalid) {
		t.Fatal("a translate term with no translation was stored", err)
	}
	glossary, entries, err := s.ChatlangGlossary(ctx, tenant)
	if err != nil || glossary.Version != 2 || len(entries) != 2 || entries[0].Source != "Acme Cloud" || entries[1].Target != "Urlaub" {
		t.Fatalf("glossary %+v %+v %v", glossary, entries, err)
	}
	if err = s.RemoveChatlangTerm(ctx, tenant, "alice", id); err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveChatlangTerm(ctx, tenant, "alice", id); !errors.Is(err, chatlang.ErrInvalid) {
		t.Fatal("a term was removed twice", err)
	}
	if glossary, _, err = s.ChatlangGlossary(ctx, tenant); err != nil || glossary.Version != 3 || len(glossary.Terms) != 1 {
		t.Fatalf("each change advances the version: %+v %v", glossary, err)
	}
	if w, _ = s.ChatlangWorkspace(ctx, tenant); w.Enabled || w.GlossaryVersion != 3 {
		t.Fatalf("a glossary change leaves the workspace switch alone: %+v", w)
	}

	// Usage lines are append-only, per tenant and per month.
	now := time.Now().UTC()
	for _, line := range []ChatlangUsage{{Tenant: tenant, Message: "m", Revision: 1, Language: "de", Provider: "p", Model: "m", CostMicros: 40, Outcome: "translated", At: now}, {Tenant: tenant, Message: "m", Revision: 1, Language: "de", Provider: "p", Model: "m", CostMicros: 2, Outcome: "discarded", At: now}, {Tenant: tenant, Message: "m", Revision: 1, Language: "de", Provider: "p", Model: "m", CostMicros: 900, Outcome: "translated", At: now.AddDate(0, -1, 0)}} {
		if err = s.RecordChatlangUsage(ctx, line); err != nil {
			t.Fatal(err)
		}
	}
	if spent, err := s.ChatlangSpent(ctx, tenant, now); err != nil || spent != 42 {
		t.Fatalf("this month's spend %d %v", spent, err)
	}
	if spent, _ := s.ChatlangSpent(ctx, "tenant-b", now); spent != 0 {
		t.Fatal("another workspace's spend")
	}
	if err = s.RecordChatlangUsage(ctx, ChatlangUsage{Tenant: tenant, Message: "m", Revision: 1, Language: "de", Provider: "p", Model: "m", CostMicros: -1, Outcome: "translated"}); !errors.Is(err, chatlang.ErrInvalid) {
		t.Fatal("a negative cost was recorded")
	}
	err = s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chatlang_usage SET cost_micros=0 WHERE tenant_id=$1`, tenant)
		return err
	})
	if err == nil {
		t.Fatal("a usage line was changed")
	}
	if n, _ := s.ChatlangUsageCount(ctx, tenant, "m"); n != 3 {
		t.Fatalf("usage lines %d", n)
	}
}

func TestTodo_CHATLANG_003_EagerRequests(t *testing.T) {
	s, scope, first := chatrenderDB(t)
	ctx := context.Background()
	tenant := scope.Tenant
	bob := RenderingScope{Principal: chat.Principal{TenantID: tenant, SubjectID: "bob"}, Tenant: tenant}
	jobs := func(post string) int {
		var n int
		err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM chatrender_job WHERE post_id=$1`, post).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	send := func(key, body string) chat.Post {
		p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: scope.Principal, TenantID: tenant, ConversationID: scope.Conversation, IdempotencyKey: key}, chat.Post{ID: key, TenantID: tenant, ConversationID: scope.Conversation, AuthorID: "alice", AuthorHomeTenantID: tenant, Body: body, Revision: 1})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	german := "Bitte lesen wir die Nachricht heute mit dem Team."
	english := chatrender.DefaultPreference("en")
	english.Translate = true
	if err := s.PutLanguageSettings(ctx, bob, english); err != nil {
		t.Fatal(err)
	}
	// The first message was sent before anything was set: nothing was requested.
	if jobs(first.ID) != 0 {
		t.Fatal("a job was requested with translation off")
	}
	step := func(name, want string, body string) {
		t.Helper()
		post := send(name, body)
		if got := jobs(post.ID); (want == "job") != (got == 1) || got > 1 {
			t.Fatalf("%s: %d jobs, want %s", name, got, want)
		}
	}
	step("workspace-off", "none", german)
	if _, err := s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Enabled: true, ExternalAllowed: true}); err != nil {
		t.Fatal(err)
	}
	step("on", "job", german)
	step("english-source", "none", "Please read the message with the team today.")
	step("no-language", "none", "ok")
	if err := s.PutChatlangChannel(ctx, tenant, scope.Conversation, "alice", chatlang.Channel{Translation: chatlang.Off}); err != nil {
		t.Fatal(err)
	}
	step("channel-off", "none", german)
	if err := s.PutChatlangChannel(ctx, tenant, scope.Conversation, "alice", chatlang.Channel{External: chatlang.ExternalBarred}); err != nil {
		t.Fatal(err)
	}
	step("barred", "none", german)
	if err := s.PutChatlangChannel(ctx, tenant, scope.Conversation, "alice", chatlang.Channel{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Enabled: true, ExternalAllowed: true, Languages: []string{"fr"}}); err != nil {
		t.Fatal(err)
	}
	step("language-not-offered", "none", german)
	if _, err := s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Enabled: true, ExternalAllowed: true, BudgetMicros: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordChatlangUsage(ctx, ChatlangUsage{Tenant: tenant, Message: "x", Revision: 1, Language: "en", Provider: "p", Model: "m", CostMicros: 10, Outcome: "translated"}); err != nil {
		t.Fatal(err)
	}
	step("budget-spent", "none", german)
	// A reader who has translation off is not requested for.
	if _, err := s.PutChatlangWorkspace(ctx, tenant, "alice", chatlang.Workspace{Enabled: true, ExternalAllowed: true}); err != nil {
		t.Fatal(err)
	}
	english.Translate = false
	if err := s.PutLanguageSettings(ctx, bob, english); err != nil {
		t.Fatal(err)
	}
	step("reader-off", "none", german)
}

func TestTodo_CHATLANG_003_PermanentFailure(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	target := chatrenderTarget(scope, p)
	if err := s.RequestRendering(ctx, scope, target); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRendering(ctx, scope.Tenant, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := s.ChatlangJobFacts(ctx, scope.Tenant, p.ID, p.Revision)
	if err != nil || facts.Conversation != scope.Conversation || facts.Author != "alice" || facts.Body != p.Body || facts.Direct {
		t.Fatalf("job facts %+v %v", facts, err)
	}
	if _, err = s.ChatlangJobFacts(ctx, scope.Tenant, p.ID, p.Revision+1); !errors.Is(err, chatrender.ErrDenied) {
		t.Fatal("a superseded revision answered", err)
	}
	if err = s.FailRenderingPermanently(ctx, job, "checks failed"); err != nil {
		t.Fatal(err)
	}
	if err = s.FailRenderingPermanently(ctx, job, "again"); !errors.Is(err, chatrender.ErrLease) {
		t.Fatal("a job ended twice", err)
	}
	if _, err = s.ClaimRendering(ctx, scope.Tenant, time.Minute); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatal("a permanently failed job was claimed again", err)
	}
	available, err := s.RenderingAvailabilityForTarget(ctx, scope, p.ID, chatrender.AsWritten, "en")
	if err != nil || available.Pending || !available.Failed {
		t.Fatalf("availability %+v %v", available, err)
	}
	bodies, err := s.ChatlangPreviousBodies(ctx, scope.Tenant, p.ID, 3)
	if err != nil || len(bodies) != 0 {
		t.Fatalf("previous bodies of the first post %v %v", bodies, err)
	}
}

// TestTodo_CHATLANG_006_Integration: a translation is derived. When the
// original is removed the translation goes with it, in the same transaction, and
// the original's own revision ledger (what a hold preserves) is untouched.
func TestTodo_CHATLANG_006_Integration(t *testing.T) {
	s, scope, p := chatrenderDB(t)
	ctx := context.Background()
	rendering := chatrenderProduce(t, s.Store, scope, p)
	count := func(query string) int {
		var n int
		err := s.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, query, scope.Tenant, p.ID).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT count(*) FROM chatrender_rendering WHERE tenant_id=$1 AND post_id=$2`) != 1 || rendering.Text == "" {
		t.Fatal("the rendering was not stored")
	}
	if _, err := s.Store.RevisePostCAS(ctx, scope.Tenant, scope.Conversation, p.ID, "alice", "", int64(p.Revision), true); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT count(*) FROM chatrender_rendering WHERE tenant_id=$1 AND post_id=$2`) != 0 || count(`SELECT count(*) FROM chatrender_job WHERE tenant_id=$1 AND post_id=$2`) != 0 {
		t.Fatal("a removed message kept its translation")
	}
	if count(`SELECT count(*) FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2 AND body<>''`) != 1 {
		t.Fatal("the original's revision ledger lost the written text")
	}
}
