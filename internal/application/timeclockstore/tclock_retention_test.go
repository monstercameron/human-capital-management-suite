package timeclockstore

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

func TestTodo_TCLOCK_017(t *testing.T) {
	policy := clockservice.DefaultTimeRecordRetentionPolicy()
	adapter, err := NewRetentionAdapter(policy)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	decl := clockservice.TimeRecordDeclaration{TenantID: "tenant-a", RecordID: "punch-1", WorkerRef: "worker-a", Custodian: "timekeeping", Jurisdiction: "US-FEDERAL", RecordClass: clockservice.TimeRecordClassPunch, RecordedAt: at, CutoffAt: at, ArchiveAcknowledged: true}
	decision, err := adapter.Evaluate(decl, at.AddDate(2, 0, 0))
	if err != nil || !decision.DispositionAllowed || decision.Rule.MinimumDays != 730 {
		t.Fatalf("adapter decision=%+v err=%v", decision, err)
	}
}

func TestTodo_TCLOCK_017_Golden(t *testing.T) {
	adapter, err := NewRetentionAdapter(clockservice.DefaultTimeRecordRetentionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	decl := clockservice.TimeRecordDeclaration{TenantID: "tenant-a", RecordID: "photo-1", WorkerRef: "worker-a", Custodian: "privacy", Jurisdiction: "US-FEDERAL", RecordClass: clockservice.TimeRecordClassPhotoEvidence, RecordedAt: at, CutoffAt: at, ArchiveAcknowledged: true}
	decision, err := adapter.EvaluateRetention(decl, at.AddDate(1, 0, 0))
	if err != nil || decision.Rule.MinimumDays != 30 || !decision.DispositionAllowed || decision.Digest == "" {
		t.Fatalf("golden adapter decision=%+v err=%v", decision, err)
	}
}

func TestTodo_TCLOCK_017_Security(t *testing.T) {
	if _, err := NewRetentionAdapter(clockservice.TimeRecordRetentionPolicy{}); err == nil {
		t.Fatal("invalid policy was accepted")
	}
	adapter, err := NewRetentionAdapter(clockservice.DefaultTimeRecordRetentionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	decl := clockservice.TimeRecordDeclaration{TenantID: "tenant-a", RecordID: "tc-1", WorkerRef: "worker-a", Custodian: "timekeeping", Jurisdiction: "US-FEDERAL", RecordClass: clockservice.TimeRecordClassTimecard, RecordedAt: at, CutoffAt: at, ArchiveAcknowledged: true, ActiveLegalHoldRefs: []string{"hold-1"}}
	decision, err := adapter.Evaluate(decl, at.AddDate(10, 0, 0))
	if err != nil || decision.DispositionAllowed || !decision.LegalHoldBlocked {
		t.Fatalf("hold bypassed by adapter: decision=%+v err=%v", decision, err)
	}
}

func TestTodo_TCLOCK_017_Property(t *testing.T) {
	policy := clockservice.DefaultTimeRecordRetentionPolicy()
	policy, err := policy.WithRule(clockservice.TimeRecordRetentionRule{RecordClass: clockservice.TimeRecordClassTimecard, Jurisdiction: "EU-DE", MinimumDays: 3650, AuthorityRef: "EU-MEMBER-STATE"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRetentionAdapter(policy)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	decl := clockservice.TimeRecordDeclaration{TenantID: "tenant-a", RecordID: "tc-eu", WorkerRef: "worker-a", Custodian: "timekeeping", Jurisdiction: "EU-DE", RecordClass: clockservice.TimeRecordClassTimecard, RecordedAt: at, CutoffAt: at, ArchiveAcknowledged: true}
	decision, err := adapter.Evaluate(decl, at.AddDate(5, 0, 0))
	if err != nil || decision.DispositionAllowed || decision.Rule.MinimumDays != 3650 {
		t.Fatalf("long jurisdiction was not honored: decision=%+v err=%v", decision, err)
	}
}
