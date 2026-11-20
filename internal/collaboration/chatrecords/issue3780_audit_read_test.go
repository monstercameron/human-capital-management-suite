package chatrecords

import (
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"
)

type denyAuth struct{}

func (denyAuth) Authorize(context.Context, string, string, string, string) (string, error) {
	return "", ErrUnauthorized
}

// onlyAuth allows exactly one action name.
type onlyAuth struct{ action string }

func (a onlyAuth) Authorize(_ context.Context, _, _, action, _ string) (string, error) {
	if action != a.action {
		return "", ErrUnauthorized
	}
	return "policy:v1", nil
}

func seedAudit(t *testing.T) *Service {
	t.Helper()
	tick := int64(1700000000)
	s := &Service{Repo: NewMemoryRepository(), Auth: allowAuth{}, Clock: func() time.Time { tick += 3600; return time.Unix(tick, 0).UTC() }}
	ctx := context.Background()
	add := func(tenant, actor, conv, record, action, reason string) {
		t.Helper()
		if _, err := s.AppendRecord(ctx, actor, Record{TenantID: tenant, ConversationID: conv, RecordID: record, Kind: KindPost, Revision: 1}, action, reason); err != nil {
			t.Fatal(err)
		}
	}
	add("t1", "ana", "c1", "post:1", "chat.post.send", "chat.post.send")
	add("t1", "bob", "c1", "post:2", "moderation.remove", "off topic")
	add("t1", "bob", "c2", "post:3", "chat.app.install", "chat.app.install")
	add("t1", "cy", "c2", "post:4", "moderation.original_view", "review 12")
	add("t2", "zed", "c9", "post:9", "chat.post.send", "other tenant")
	return s
}

func TestIssue3780_ListAuditFiltersAndPages(t *testing.T) {
	s := seedAudit(t)
	ctx := context.Background()
	page, err := s.ListAudit(ctx, "admin", "t1", AuditFilter{}, "", 2)
	if err != nil || len(page.Entries) != 2 || page.NextCursor == "" {
		t.Fatalf("first page = %+v err=%v", page, err)
	}
	if page.Entries[0].Action != "moderation.original_view" || page.Entries[1].Action != "chat.app.install" {
		t.Fatalf("newest first expected: %+v", page.Entries)
	}
	next, err := s.ListAudit(ctx, "admin", "t1", AuditFilter{}, page.NextCursor, 2)
	if err != nil || len(next.Entries) != 2 || next.NextCursor != "" || next.Entries[1].ActorID != "ana" {
		t.Fatalf("second page = %+v err=%v", next, err)
	}
	for name, tc := range map[string]struct {
		f    AuditFilter
		want int
	}{
		"actor":        {AuditFilter{ActorID: "bob"}, 2},
		"action group": {AuditFilter{Action: "moderation"}, 2},
		"exact action": {AuditFilter{Action: "chat.app.install"}, 1},
		"conversation": {AuditFilter{ConversationID: "c2"}, 2},
		"combined":     {AuditFilter{ActorID: "bob", ConversationID: "c1"}, 1},
		"time range":   {AuditFilter{From: time.Unix(1700000000+14400, 0), To: time.Unix(1700000000+21601, 0)}, 2},
		"no match":     {AuditFilter{ActorID: "nobody"}, 0},
	} {
		got, err := s.ListAudit(ctx, "admin", "t1", tc.f, "", 50)
		if err != nil || len(got.Entries) != tc.want {
			t.Errorf("%s: %d entries, want %d (err=%v)", name, len(got.Entries), tc.want, err)
		}
	}
}

func TestIssue3780_ListAuditIsTenantScopedAndCursorBound_Security(t *testing.T) {
	s := seedAudit(t)
	ctx := context.Background()
	other, err := s.ListAudit(ctx, "admin", "t2", AuditFilter{}, "", 50)
	if err != nil || len(other.Entries) != 1 || other.Entries[0].ActorID != "zed" {
		t.Fatalf("tenant t2 sees %+v err=%v", other, err)
	}
	page, _ := s.ListAudit(ctx, "admin", "t1", AuditFilter{}, "", 1)
	if _, err = s.ListAudit(ctx, "admin", "t2", AuditFilter{}, page.NextCursor, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a cursor from another tenant must be refused, got %v", err)
	}
	if _, err = s.ListAudit(ctx, "admin", "t1", AuditFilter{}, "not-a-cursor", 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("garbage cursor must be refused, got %v", err)
	}
	if _, err = s.ListAudit(ctx, "admin", "t1", AuditFilter{From: time.Unix(20, 0), To: time.Unix(10, 0)}, "", 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("inverted time range must be refused, got %v", err)
	}
}

func TestIssue3780_ListAuditRefusesBeforeReading_Security(t *testing.T) {
	s := seedAudit(t)
	s.Auth = denyAuth{}
	if _, err := s.ListAudit(context.Background(), "intruder", "t1", AuditFilter{}, "", 10); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err=%v", err)
	}
	s.Auth = onlyAuth{action: "audit.read"}
	if _, err := s.ListAudit(context.Background(), "reader", "t1", AuditFilter{}, "", 10); err != nil {
		t.Fatalf("the audit.read action must be what is asked: %v", err)
	}
	if _, _, err := s.ExportAuditCSV(context.Background(), "reader", "t1", AuditFilter{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("read permission must not allow export: %v", err)
	}
}

func TestIssue3780_ListAuditEntriesAreBodyFree(t *testing.T) {
	s := seedAudit(t)
	page, _ := s.ListAudit(context.Background(), "admin", "t1", AuditFilter{}, "", 50)
	for _, e := range page.Entries {
		if e.EventID == "" || e.ActorID == "" || e.Action == "" || e.TargetID == "" || e.At.IsZero() {
			t.Fatalf("incomplete entry %+v", e)
		}
	}
	long := strings.Repeat("x", 2000)
	if got := auditReasonForView(long); len([]rune(got)) > auditReasonLimit+1 {
		t.Fatalf("reason not bounded: %d", len(got))
	}
}

func TestIssue3780_ExportAuditCSVIsAuditedAndSafe(t *testing.T) {
	s := seedAudit(t)
	ctx := context.Background()
	if _, err := s.AppendRecord(ctx, "evil", Record{TenantID: "t1", ConversationID: "c1", RecordID: "post:7", Kind: KindPost, Revision: 1}, "chat.post.send", "=HYPERLINK(\"http://x\")"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Repo.Events(ctx, "t1")
	body, n, err := s.ExportAuditCSV(ctx, "admin", "t1", AuditFilter{})
	if err != nil || n != len(before) {
		t.Fatalf("n=%d want %d err=%v", n, len(before), err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil || len(rows) != len(before)+1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	for _, row := range rows[1:] {
		for _, cell := range row {
			if cell != "" && strings.ContainsAny(cell[:1], "=+-@\t\r") {
				t.Fatalf("cell %q can run as a formula", cell)
			}
		}
	}
	if !strings.Contains(string(body), "'=HYPERLINK") {
		t.Fatalf("formula text must be neutralised, not dropped: %s", body)
	}
	after, _ := s.Repo.Events(ctx, "t1")
	if len(after) != len(before)+1 {
		t.Fatalf("the export itself must be audited: %d -> %d", len(before), len(after))
	}
	last := after[len(after)-1]
	if last.Action != "audit.export" || last.ActorID != "admin" || !strings.Contains(last.Reason, "5") {
		t.Fatalf("export audit event = %+v", last)
	}
	page, _ := s.ListAudit(ctx, "admin", "t1", AuditFilter{Action: "audit.export"}, "", 10)
	if len(page.Entries) != 1 {
		t.Fatalf("export must be readable in the log: %+v", page.Entries)
	}
}

func TestIssue3780_ExportFailsClosedWhenItCannotBeAudited_Security(t *testing.T) {
	s := seedAudit(t)
	s.Repo = failingAppend{s.Repo.(*MemoryRepository)}
	body, _, err := s.ExportAuditCSV(context.Background(), "admin", "t1", AuditFilter{})
	if err == nil || len(body) != 0 {
		t.Fatalf("an export that cannot be recorded must not be handed over: err=%v bytes=%d", err, len(body))
	}
}

type failingAppend struct{ *MemoryRepository }

func (failingAppend) Append(context.Context, Record, AuditEvent, OutboxEvent) error {
	return errors.New("down")
}
