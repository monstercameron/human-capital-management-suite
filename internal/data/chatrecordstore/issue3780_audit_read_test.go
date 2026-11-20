package chatrecordstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
)

func TestIssue3780_StoreListAudit_Integration(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	base := time.Unix(1700000000, 0).UTC()
	add := func(tenant, actor, conv, record, action string, n int) {
		t.Helper()
		e := chatrecords.AuditEvent{TenantID: tenant, ActorID: actor, Action: action, TargetType: "chat", TargetID: record, PriorRevision: 1, Reason: action, PolicyEvidence: "p", At: base.Add(time.Duration(n) * time.Hour)}
		r := chatrecords.Record{TenantID: tenant, ConversationID: conv, RecordID: record, Kind: chatrecords.KindPost, SourceID: record, Revision: 1, CreatedAt: base}
		if _, err := s.AppendEvent(ctx, r, e, chatrecords.OutboxEvent{}); err != nil {
			t.Fatal(err)
		}
	}
	add("t1", "ana", "c1", "post:1", "chat.post.send", 1)
	add("t1", "bob", "c1", "post:2", "chat.post.delete", 2)
	add("t1", "bob", "c2", "post:3", "chat.app.install", 3)
	add("t2", "zed", "c1", "post:1", "chat.post.send", 4)
	at := base.Add(5 * time.Hour)
	action := chatrecords.CaseAction{CaseID: "case-1", Action: "remove", ActorID: "mod", Reason: "off topic", EvidenceRef: "evidence://1", At: at}
	if _, err := s.PutCaseActionAudited(ctx, "t1", "c2", action, chatrecords.AuditEvent{TenantID: "t1", ActorID: "mod", Action: "moderation.remove", TargetType: "chat_post", TargetID: "raw-post-9", PriorRevision: 3, Reason: "off topic", PolicyEvidence: "p", At: at}); err != nil {
		t.Fatal(err)
	}

	list := func(tenant string, q chatrecords.AuditQuery) []chatrecords.AuditEvent {
		t.Helper()
		if q.Limit == 0 {
			q.Limit = 50
		}
		got, err := s.ListAudit(ctx, tenant, q)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	all := list("t1", chatrecords.AuditQuery{})
	if len(all) != 4 || all[0].Action != "moderation.remove" || all[3].Action != "chat.post.send" {
		t.Fatalf("t1 newest first = %+v", all)
	}
	if got := list("t1", chatrecords.AuditQuery{Before: all[1].Sequence, Limit: 1}); len(got) != 1 || got[0].Sequence != all[2].Sequence {
		t.Fatalf("keyset page = %+v", got)
	}
	cases := map[string]struct {
		f    chatrecords.AuditFilter
		want int
	}{
		"actor":                            {chatrecords.AuditFilter{ActorID: "bob"}, 2},
		"action family":                    {chatrecords.AuditFilter{Action: "chat.post"}, 2},
		"action exact":                     {chatrecords.AuditFilter{Action: "chat.app.install"}, 1},
		"family not substring":             {chatrecords.AuditFilter{Action: "chat.po"}, 0},
		"conversation c1":                  {chatrecords.AuditFilter{ConversationID: "c1"}, 2},
		"conversation c2 incl. moderation": {chatrecords.AuditFilter{ConversationID: "c2"}, 2},
		"time range":                       {chatrecords.AuditFilter{From: base.Add(2 * time.Hour), To: base.Add(4 * time.Hour)}, 2},
		"like wildcard literal":            {chatrecords.AuditFilter{ActorID: "%"}, 0},
	}
	for name, tc := range cases {
		if got := list("t1", chatrecords.AuditQuery{AuditFilter: tc.f}); len(got) != tc.want {
			t.Errorf("%s: %d rows, want %d: %+v", name, len(got), tc.want, got)
		}
	}
	// Another tenant's rows never appear, whatever the filter says.
	for _, e := range list("t2", chatrecords.AuditQuery{}) {
		if e.TenantID != "t2" || e.ActorID != "zed" {
			t.Fatalf("tenant leak: %+v", e)
		}
	}
	if got := list("t2", chatrecords.AuditQuery{AuditFilter: chatrecords.AuditFilter{ActorID: "ana"}}); len(got) != 0 {
		t.Fatalf("t2 reads t1 actor: %+v", got)
	}
}

func TestIssue3780_ServiceOverStore_Integration(t *testing.T) {
	s := newStore(t)
	svc := &chatrecords.Service{Repo: s, Auth: allowAll{}, Clock: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	ctx := context.Background()
	if _, err := svc.AppendRecord(ctx, "ana", chatrecords.Record{TenantID: "t1", ConversationID: "c1", RecordID: "post:1", Kind: chatrecords.KindPost, Revision: 1}, "chat.post.send", "chat.post.send"); err != nil {
		t.Fatal(err)
	}
	body, n, err := svc.ExportAuditCSV(ctx, "admin", "t1", chatrecords.AuditFilter{})
	if err != nil || n != 1 || len(body) == 0 {
		t.Fatalf("export n=%d err=%v", n, err)
	}
	page, err := svc.ListAudit(ctx, "admin", "t1", chatrecords.AuditFilter{Action: "audit"}, "", 10)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ActorID != "admin" {
		t.Fatalf("export not in the durable log: %+v err=%v", page, err)
	}
}

type allowAll struct{}

func (allowAll) Authorize(context.Context, string, string, string, string) (string, error) {
	return "policy:v1", nil
}
