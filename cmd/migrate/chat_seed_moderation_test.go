package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

const moderationSeedTenant = "ironridge-demo"

// moderationSeededDatabase runs the full small chat seed, which ends with the
// moderation history, and hands back the seeder to read it with.
func moderationSeededDatabase(t *testing.T) (*chatstore.Store, *pgtest.DB, *moderationSeeder, time.Time) {
	t.Helper()
	store, db := chatSeedStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	var out bytes.Buffer
	opts := chatSeedOptions{Tenant: moderationSeedTenant, Scale: "small", MediaRoot: t.TempDir(), AssetDir: "../../internal/humanwork/workspace/assets", Now: now}
	if err := runChatSeedCommand(ctx, store, nil, opts, &out); err != nil {
		t.Fatalf("chat seed: %v", err)
	}
	if !strings.Contains(out.String(), "seeded the moderation history for "+moderationSeedTenant) {
		t.Fatalf("the full chat seed did not add the moderation history: %q", out.String())
	}
	classifier, err := application.NewLocalDevPersonaPostClassifier(store, true)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newModerationSeeder(ctx, store, moderationSeedTenant, now, classifier)
	if err != nil {
		t.Fatal(err)
	}
	return store, db, s, now
}

// queue reads the Moderation queue as one person: the open items, then the
// resolved ones.
func (s *moderationSeeder) queue(t *testing.T, subject string, at time.Time) (open, closed []chatcore.ModerationItem) {
	t.Helper()
	ctx, principal, err := s.as(subject)
	if err != nil {
		t.Fatal(err)
	}
	service := s.moderation(at)
	if open, err = service.Queue(ctx, principal, s.tenant, ""); err != nil {
		t.Fatalf("open queue of %s: %v", subject, err)
	}
	if closed, err = service.Queue(ctx, principal, s.tenant, "state:closed"); err != nil {
		t.Fatalf("resolved queue of %s: %v", subject, err)
	}
	return open, closed
}

func moderationItems(items []chatcore.ModerationItem, keep func(chatcore.ModerationItem) bool) []chatcore.ModerationItem {
	var out []chatcore.ModerationItem
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return out
}

func inOfficeHours(t *testing.T, what string, at time.Time, office *time.Location, now time.Time) {
	t.Helper()
	local := at.In(office)
	since := local.Sub(time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office))
	if since < chatSeedWorkdayStart || since > chatSeedWorkdayEnd+time.Minute {
		t.Errorf("%s is at %s, outside office hours in %s", what, local.Format("Mon 15:04:05"), office)
	}
	if at.After(now) || at.Before(now.Add(-15*24*time.Hour)) {
		t.Errorf("%s is at %s, outside the last two weeks", what, at.Format(time.RFC3339))
	}
}

// TestTodo_CHATSEED_002 checks that every kind of moderation item is there after
// the seed, with who raised it, who wrote it, where, why and when.
func TestTodo_CHATSEED_002(t *testing.T) {
	_, db, s, now := moderationSeededDatabase(t)
	ctx := context.Background()
	open, closed := s.queue(t, s.admin, now)
	byConversation := func(name string) func(chatcore.ModerationItem) bool {
		return func(i chatcore.ModerationItem) bool { return i.ConversationName == name }
	}

	// Open: one harassment report with its note, two spam reports of one message,
	// one message held by a channel word list, one report in a private channel.
	if len(open) != 5 {
		t.Fatalf("open items = %d, want 5: %+v", len(open), open)
	}
	harassment := moderationItems(open, func(i chatcore.ModerationItem) bool { return strings.HasPrefix(i.Reason, "harassment — ") })
	if len(harassment) != 1 || harassment[0].Kind != "report" || harassment[0].ReporterID == "" || harassment[0].AuthorID == "" || harassment[0].ReporterID == harassment[0].AuthorID ||
		harassment[0].ConversationName != "people-ops" || harassment[0].Message.Body != modHarassment.Body || !strings.Contains(harassment[0].Reason, "not the first time") || !harassment[0].CanRemove {
		t.Fatalf("the harassment report: %+v", harassment)
	}
	spam := moderationItems(open, func(i chatcore.ModerationItem) bool { return strings.HasPrefix(i.Reason, "spam") })
	if len(spam) != 2 || spam[0].PostID != spam[1].PostID || spam[0].ReporterID == spam[1].ReporterID || spam[0].ConversationName != "general" || spam[0].Message.Body != modSpam.Body {
		t.Fatalf("the spam reports: %+v", spam)
	}
	held := moderationItems(open, func(i chatcore.ModerationItem) bool { return i.Kind == "filter" })
	if len(held) != 1 || held[0].Rule != "profanity 1.1.0" || held[0].ConversationName != "engineering" || held[0].Message.Body != modHeld.Body || held[0].AuthorID == "" {
		t.Fatalf("the message held by the channel word list: %+v", held)
	}
	private := moderationItems(open, byConversation("leadership-private"))
	if len(private) != 1 || private[0].Kind != "report" || !strings.HasPrefix(private[0].Reason, "sensitive_information") || private[0].Message.Body != modPrivate.Body {
		t.Fatalf("the private channel report: %+v", private)
	}

	// Resolved: a dismissed report, a removal, a restored removal with its
	// appeal, a released filter hit; each says who decided and why.
	dismissed := moderationItems(closed, func(i chatcore.ModerationItem) bool { return i.Kind == "report" && i.Decision == "dismiss" })
	if len(dismissed) != 1 || dismissed[0].DecidedBy != s.admin || !strings.Contains(dismissed[0].DecisionReason, "Blunt feedback") || dismissed[0].Message.Body != modDismissed.Body {
		t.Fatalf("the dismissed report: %+v", dismissed)
	}
	removals := moderationItems(closed, func(i chatcore.ModerationItem) bool { return i.Kind == "removal" })
	if len(removals) != 2 {
		t.Fatalf("removals = %d, want the removed message and the restored one: %+v", len(removals), removals)
	}
	var removed, restored chatcore.ModerationItem
	for _, r := range removals {
		if r.Message.Deleted {
			removed = r
		} else {
			restored = r
		}
	}
	if removed.Decision != "remove" || removed.DecidedBy != s.admin || !strings.HasPrefix(removed.Reason, "sensitive_information") || removed.Message.Body != modRemoved.Body || !removed.CanRestore {
		t.Fatalf("the removed message: %+v", removed)
	}
	if restored.Decision != "restore" || restored.Message.Body != modRestored.Body || !strings.HasPrefix(restored.Reason, "policy_violation") {
		t.Fatalf("the restored message: %+v", restored)
	}
	appeals := moderationItems(closed, func(i chatcore.ModerationItem) bool { return i.Kind == "appeal" })
	if len(appeals) != 1 || appeals[0].Decision != "restore" || appeals[0].ReporterID != restored.AuthorID || !strings.Contains(appeals[0].DecisionReason, "not aimed at a person") {
		t.Fatalf("the appeal: %+v", appeals)
	}
	released := moderationItems(closed, func(i chatcore.ModerationItem) bool { return i.Kind == "filter" })
	if len(released) != 1 || released[0].Decision != "dismiss" || released[0].Message.Body != modReleased.Body || released[0].Rule != held[0].Rule {
		t.Fatalf("the released filter hit: %+v", released)
	}

	// Both decisions on the restored message are recorded, with who and why.
	var removeActor, restoreActor string
	if err := db.SQL.QueryRowContext(ctx, `SELECT (SELECT actor_id FROM chat_moderation_action WHERE case_id=$1 AND action='remove'), (SELECT actor_id FROM chat_moderation_action WHERE case_id=$1 AND action='restore')`, "removal:"+restored.PostID).Scan(&removeActor, &restoreActor); err != nil {
		t.Fatalf("the restored message's decisions: %v", err)
	}
	if removeActor != s.admin || restoreActor != s.admin {
		t.Fatalf("decisions recorded for %q and %q, want the administrator", removeActor, restoreActor)
	}

	// The two filters exist, and the workspace one told the managers of #general
	// about the message that mentioned a client's code name.
	defs, err := s.filters.Definitions(ctx, s.tenant)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, d := range defs {
		found[d.ID] = d.Action + "/" + d.Authority
	}
	if found[chatSeedModerationNotifyRule] != "notify/workspace" || found[chatSeedModerationChannelRule] != "flag/workspace" {
		t.Fatalf("custom filters = %v", found)
	}
	hits, err := s.filters.Hits(ctx, s.tenant)
	if err != nil {
		t.Fatal(err)
	}
	notified := 0
	for _, h := range hits {
		if h.Hit.RuleID == chatSeedModerationNotifyRule && h.Hit.Action == "notify" && !h.Hit.DryRun {
			notified++
		}
	}
	if notified != 1 {
		t.Fatalf("notify hits = %d, want 1 (%d hits)", notified, len(hits))
	}
	var notices int
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_moderation_notice n WHERE n.outcome=$1 AND n.reason='Client code names' AND EXISTS (SELECT 1 FROM chat_membership m WHERE m.conversation_id=$2 AND m.member_id=n.subject_id AND m.role='MANAGER')`, chatcore.ModerationOutcomeFilterNotify, s.convs["general"]).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices == 0 {
		t.Fatal("no manager of #general was told about the client code name")
	}

	// The identifiers are derived from the tenant and a fixed key, so a fresh
	// database always has the same reports.
	for _, key := range []string{modHarassment.Key, modSpam.Key + ":1", modSpam.Key + ":2", modPrivate.Key, modDismissed.Key} {
		id := moderationReportID(moderationSeedTenant, key)
		var n int
		if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM chat_moderation_report WHERE tenant_id=$1 AND report_id=$2`, moderationSeedTenant, id).Scan(&n); err != nil || n != 1 {
			t.Fatalf("report %s (%s) = %d, %v", key, id, n, err)
		}
	}

	// Office hours of the last two weeks: the messages, the reports and the decisions.
	rows, err := db.SQL.QueryContext(ctx, `SELECT 'message '||i.client_key, p.created_at FROM chat_post p JOIN chat_idempotency i ON i.post_id=p.id AND i.tenant_id=p.tenant_id WHERE i.client_key LIKE 'seed-moderation:%' UNION ALL SELECT 'report '||report_id, created_at FROM chat_moderation_report UNION ALL SELECT 'decision '||case_id||' '||action, at_time FROM chat_moderation_action`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var what string
		var at time.Time
		if err = rows.Scan(&what, &at); err != nil {
			t.Fatal(err)
		}
		inOfficeHours(t, what, at, s.office, now)
		seen++
	}
	if seen < 18 {
		t.Fatalf("checked %d dated records, want the messages, reports and decisions", seen)
	}
}

// TestTodo_CHATSEED_002_Integration runs the step again, as the subcommand and as
// part of a second seed call, and asserts that nothing changes; then reads the
// result the way the product does: the queue for an administrator and for an
// ordinary member, and the conversations as a reader sees them.
func TestTodo_CHATSEED_002_Integration(t *testing.T) {
	store, db, s, now := moderationSeededDatabase(t)
	ctx := context.Background()
	counts := func() string {
		var parts []string
		for _, table := range []string{"chat_post", "chat_moderation_report", "chat_admin_removal", "chat_moderation_notice", "chat_moderation_action", "chat_filter_version", "chat_filter_enablement", "chat_filter_hit", "chat_filter_hit_review", "chat_audit_event", "chat_outbox", "chat_post_revision", "chat_membership"} {
			var n int
			if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
				t.Fatalf("%s: %v", table, err)
			}
			parts = append(parts, fmt.Sprintf("%s=%d", table, n))
		}
		return strings.Join(parts, " ")
	}
	before := counts()
	for run := 1; run <= 2; run++ {
		var out bytes.Buffer
		if err := runChatSeedModerationCommand(ctx, store, nil, chatSeedOptions{Tenant: moderationSeedTenant, Now: now}, &out); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if !strings.Contains(out.String(), " 0 added,") {
			t.Fatalf("run %d added rows: %q", run, out.String())
		}
	}
	// A later clock changes nothing either: the messages, reports and decisions
	// are found by their keys, not by their dates.
	if _, err := seedChatModeration(ctx, store, moderationSeedTenant, now.Add(36*time.Hour), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if after := counts(); after != before {
		t.Fatalf("a repeated run changed the rows:\nbefore %s\nafter  %s", before, after)
	}

	// An administrator reads everything; an ordinary member reads nothing.
	open, closed := s.queue(t, s.admin, now)
	if len(open) != 5 || len(closed) != 5 {
		t.Fatalf("the administrator's queue: %d open, %d resolved", len(open), len(closed))
	}
	for _, subject := range []string{s.member("general", 7), s.member("engineering", 6), s.member("leadership-private", 3)} {
		if open, closed = s.queue(t, subject, now); len(open)+len(closed) != 0 {
			t.Fatalf("%s reads moderation items: %+v %+v", subject, open, closed)
		}
	}
	summary, err := func() (chatcore.ModerationSummary, error) {
		ctx, principal, err := s.as(s.admin)
		if err != nil {
			return chatcore.ModerationSummary{}, err
		}
		return s.moderation(now).Summary(ctx, principal, s.tenant)
	}()
	if err != nil || !summary.Moderator || summary.Open != 5 {
		t.Fatalf("the administrator's count: %+v %v", summary, err)
	}

	// A reader sees the removed message as a notice, and the restored one as text.
	reader := s.member("general", 7)
	readerCtx, readerPrincipal, err := s.as(reader)
	if err != nil {
		t.Fatal(err)
	}
	read := func(room string) map[string]chatcore.Post {
		page, listErr := s.moderated.ListPosts(readerCtx, readerPrincipal, s.tenant, s.convs[room], 0, chatcore.Page{PageSize: 200}, chatcore.PostWindow{Descending: true})
		if listErr != nil {
			t.Fatalf("reader of %s: %v", room, listErr)
		}
		out := map[string]chatcore.Post{}
		for _, p := range page.Posts {
			out[p.ID] = p
		}
		return out
	}
	removed, err := s.send(modRemoved)
	if err != nil {
		t.Fatal(err)
	}
	if p := read("general")[removed.ID]; !p.Deleted || p.Body != chatcore.RemovedByAdministrator || strings.Contains(p.Body, "303-555") {
		t.Fatalf("a reader of the removed message sees %+v", p)
	}
	restoredPost, err := s.send(modRestored)
	if err != nil {
		t.Fatal(err)
	}
	engineeringReader := s.member("engineering", 6)
	readerCtx, readerPrincipal, err = s.as(engineeringReader)
	if err != nil {
		t.Fatal(err)
	}
	if p := read("engineering")[restoredPost.ID]; p.Deleted || p.Body != modRestored.Body {
		t.Fatalf("a reader of the restored message sees %+v", p)
	}

	// The author of the removed message was told why, privately, and the author of
	// the restored one was told twice: removed, then restored.
	told, err := s.notices(removed.AuthorID, removed)
	if err != nil || len(told) != 1 || told[0].Outcome != "remove" || !strings.HasPrefix(told[0].Reason, "sensitive_information") {
		t.Fatalf("the removed message's author was told %+v %v", told, err)
	}
	told, err = s.notices(restoredPost.AuthorID, restoredPost)
	outcomes := map[string]bool{}
	for _, n := range told {
		outcomes[n.Outcome] = true
	}
	if err != nil || !outcomes["remove"] || !outcomes["restore"] {
		t.Fatalf("the restored message's author was told %+v %v", told, err)
	}
}
