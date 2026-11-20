package chatrecords

import (
	"context"
	"errors"
	"testing"
	"time"
)

type issue3778Repo struct {
	*retentionMemory
	listed int
}

func (r *issue3778Repo) List(ctx context.Context, tenant string) ([]Record, error) {
	r.listed++
	return r.retentionMemory.MemoryRepository.List(ctx, tenant)
}

func issue3778Service(t *testing.T, policy RetentionPolicy, auth Authorizer) (*Service, *issue3778Repo, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	repo := &issue3778Repo{retentionMemory: &retentionMemory{MemoryRepository: NewMemoryRepository()}}
	s := &Service{Repo: repo, Auth: auth, Clock: func() time.Time { return now }}
	if policy.Mode != "" {
		policy.TenantID = "t"
		if _, err := s.PutRetentionPolicy(context.Background(), "admin", policy, 0); err != nil {
			t.Fatal(err)
		}
	}
	old := now.AddDate(0, 0, -30)
	put := func(r Record) {
		r.TenantID, r.ConversationID, r.Revision = "t", "c", 1
		if r.CreatedAt.IsZero() {
			r.CreatedAt = old
		}
		if err := repo.MemoryRepository.Append(context.Background(), r, AuditEvent{TenantID: "t", EventID: "e-" + r.RecordID, Action: "x"}, OutboxEvent{ID: "e-" + r.RecordID, EventID: "e-" + r.RecordID}); err != nil {
			t.Fatal(err)
		}
	}
	released := now.AddDate(0, 0, -1)
	for _, h := range []Hold{{TenantID: "t", HoldID: "h1", Reason: "matter", PlacedBy: "legal"}, {TenantID: "t", HoldID: "h2", Reason: "done", PlacedBy: "legal", ReleasedAt: &released}} {
		if err := repo.MemoryRepository.PutHold(context.Background(), h); err != nil {
			t.Fatal(err)
		}
	}
	put(Record{RecordID: "p1", Kind: KindPost})
	put(Record{RecordID: "d1", Kind: KindDerived, DerivedFrom: "p1"})
	put(Record{RecordID: "e1", Kind: KindEdit})
	put(Record{RecordID: "f1", Kind: KindFile, HoldIDs: []string{"h1"}})
	put(Record{RecordID: "f2", Kind: KindFile, HoldIDs: []string{"h2"}})
	put(Record{RecordID: "f3", Kind: KindFile, HoldIDs: []string{"ghost"}})
	put(Record{RecordID: "n1", Kind: KindPost, CreatedAt: now.AddDate(0, 0, -1)})
	put(Record{RecordID: "o1", Kind: KindDerived, DerivedFrom: "missing"})
	put(Record{RecordID: "x1", Kind: KindPost, Disposition: "DISPOSED"})
	return s, repo, now
}

func issue3778Count(report RetentionPreviewReport, kind Kind) int {
	for _, c := range report.WouldRemove {
		if c.Kind == kind {
			return c.Count
		}
	}
	return 0
}

func TestIssue3778_PreviewCountsByKindAndSkipsHeldAndUnknown(t *testing.T) {
	cutoff := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
	s, repo, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionDate, BeforeDate: cutoff}, allowAuth{})
	report, err := s.PreviewRetention(context.Background(), "admin", "t")
	if err != nil {
		t.Fatal(err)
	}
	if !report.PolicyConfigured || report.Mode != RetentionDate || report.Scanned != 8 {
		t.Fatalf("report=%+v", report)
	}
	for kind, want := range map[Kind]int{KindPost: 1, KindDerived: 1, KindEdit: 1, KindFile: 1} {
		if got := issue3778Count(report, kind); got != want {
			t.Errorf("would remove %s = %d, want %d (%+v)", kind, got, want, report.WouldRemove)
		}
	}
	if report.WouldRemoveTotal != 4 || report.HeldSkipped != 2 || report.UnknownSkipped != 1 || report.UsageUnknown {
		t.Fatalf("totals wrong: %+v", report)
	}
	// A preview reads and never writes: nothing was disposed, no event was added.
	events, _ := repo.MemoryRepository.Events(context.Background(), "t")
	records, _ := repo.MemoryRepository.List(context.Background(), "t")
	if len(events) != 9 || len(records) != 9 {
		t.Fatalf("preview changed state: events=%d records=%d", len(events), len(records))
	}
	for _, r := range records {
		if r.RecordID != "x1" && r.Disposition != "" {
			t.Fatalf("%s was disposed by a preview", r.RecordID)
		}
	}
}

func TestIssue3778_PreviewByAgeAndNothingDue(t *testing.T) {
	s, _, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionAge, AgeDays: 90}, allowAuth{})
	report, err := s.PreviewRetention(context.Background(), "admin", "t")
	if err != nil || report.WouldRemoveTotal != 0 || report.HeldSkipped != 0 || report.UnknownSkipped != 0 || report.Scanned != 8 {
		t.Fatalf("nothing is 90 days old: %+v err=%v", report, err)
	}
}

func TestIssue3778_PreviewNeverPromisesAStorageBudgetItCannotMeasure(t *testing.T) {
	s, _, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionBudget, BudgetBytes: 1}, allowAuth{})
	report, err := s.PreviewRetention(context.Background(), "admin", "t")
	if err != nil || !report.UsageUnknown || report.WouldRemoveTotal != 0 || report.Mode != RetentionBudget {
		t.Fatalf("a budget without byte accounting must remove nothing: %+v err=%v", report, err)
	}
}

func TestIssue3778_PreviewWithoutAPolicyReportsNotConfigured(t *testing.T) {
	s, _, _ := issue3778Service(t, RetentionPolicy{}, allowAuth{})
	report, err := s.PreviewRetention(context.Background(), "admin", "t")
	if err != nil || report.PolicyConfigured || report.WouldRemoveTotal != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestIssue3778_PreviewRefusedBeforeAnyRead_Security(t *testing.T) {
	s, repo, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionAge, AgeDays: 7}, allowAuth{})
	s.Auth = denyRetention{}
	repo.listed = 0
	if _, err := s.PreviewRetention(context.Background(), "intruder", "t"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err=%v", err)
	}
	if repo.listed != 0 {
		t.Fatal("the inventory was read before the caller was authorised")
	}
}

func TestIssue3778_PreviewIsTenantScoped_Security(t *testing.T) {
	s, _, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionAge, AgeDays: 7}, allowAuth{})
	report, err := s.PreviewRetention(context.Background(), "admin", "other")
	if err != nil || report.PolicyConfigured || report.Scanned != 0 {
		t.Fatalf("another tenant's records leaked into the preview: %+v err=%v", report, err)
	}
}

// A hold placed on a whole conversation (#3779) covers every record in it, so
// the preview must skip them even when no record carries the hold id itself.
func TestIssue3778_PreviewSkipsRecordsUnderAConversationHold_Security(t *testing.T) {
	cutoff := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -10)
	s, repo, _ := issue3778Service(t, RetentionPolicy{Mode: RetentionDate, BeforeDate: cutoff}, allowAuth{})
	before, err := s.PreviewRetention(context.Background(), "admin", "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MemoryRepository.PutHold(context.Background(), Hold{TenantID: "t", HoldID: "hc", Reason: "matter", PlacedBy: "legal", ConversationID: "c"}); err != nil {
		t.Fatal(err)
	}
	after, err := s.PreviewRetention(context.Background(), "admin", "t")
	if err != nil {
		t.Fatal(err)
	}
	if before.WouldRemoveTotal == 0 || after.WouldRemoveTotal != 0 || after.HeldSkipped < before.WouldRemoveTotal {
		t.Fatalf("a conversation hold did not stop the preview: before=%+v after=%+v", before, after)
	}
}
