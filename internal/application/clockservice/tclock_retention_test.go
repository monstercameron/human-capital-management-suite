package clockservice

import (
	"testing"
	"time"
)

func retentionDeclaration(class TimeRecordClass) TimeRecordDeclaration {
	at := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	return TimeRecordDeclaration{TenantID: "tenant-a", RecordID: string(class) + "-1", WorkerRef: "worker-a", Custodian: "timekeeping", Jurisdiction: "US-FEDERAL", RecordClass: class, RecordedAt: at, CutoffAt: at, ArchiveAcknowledged: true}
}

func TestTodo_TCLOCK_017(t *testing.T) {
	policy := DefaultTimeRecordRetentionPolicy()
	for _, tc := range []struct {
		class TimeRecordClass
		days  int
	}{
		{TimeRecordClassPunch, 730},
		{TimeRecordClassTimecard, 730},
		{TimeRecordClassPayrollLinkedTimecard, 1095},
		{TimeRecordClassAttestation, 730},
		{TimeRecordClassPhotoEvidence, 30},
		{TimeRecordClassLocationEvidence, 90},
		{TimeRecordClassDeviceLog, 365},
	} {
		rule, err := policy.RuleFor(tc.class, "US-FEDERAL")
		if err != nil || rule.MinimumDays != tc.days {
			t.Fatalf("%s rule=%+v err=%v", tc.class, rule, err)
		}
	}
	decl := retentionDeclaration(TimeRecordClassTimecard)
	before, err := EvaluateTimeRecordRetention(policy, decl, decl.CutoffAt.Add(730*24*time.Hour).Add(-time.Hour))
	if err != nil || before.DispositionAllowed || before.Status != TimeRecordRetentionBlocked {
		t.Fatalf("before minimum decision=%+v err=%v", before, err)
	}
	after, err := EvaluateTimeRecordRetention(policy, decl, decl.CutoffAt.AddDate(2, 0, 0))
	if err != nil || !after.DispositionAllowed || after.Status != TimeRecordRetentionEligible {
		t.Fatalf("after minimum decision=%+v err=%v", after, err)
	}
}

func TestTodo_TCLOCK_017_Golden(t *testing.T) {
	policy := DefaultTimeRecordRetentionPolicy()
	decl := retentionDeclaration(TimeRecordClassPayrollLinkedTimecard)
	decision, err := EvaluateTimeRecordRetention(policy, decl, decl.CutoffAt.AddDate(3, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Rule.MinimumDays != 1095 || decision.Rule.AuthorityRef != "FLSA-29-CFR-516-PAYROLL" || decision.Status != TimeRecordRetentionEligible || !decision.DispositionAllowed || decision.Digest == "" {
		t.Fatalf("golden decision=%+v", decision)
	}
}

func TestTodo_TCLOCK_017_Security(t *testing.T) {
	policy := DefaultTimeRecordRetentionPolicy()
	decl := retentionDeclaration(TimeRecordClassTimecard)
	decl.ActiveLegalHoldRefs = []string{"hold-1"}
	decision, err := EvaluateTimeRecordRetention(policy, decl, decl.CutoffAt.AddDate(10, 0, 0))
	if err != nil || decision.DispositionAllowed || !decision.LegalHoldBlocked || decision.Status != TimeRecordRetentionBlocked {
		t.Fatalf("held decision=%+v err=%v", decision, err)
	}
	decl.Jurisdiction = "EU-DE"
	if _, err := EvaluateTimeRecordRetention(policy, decl, time.Now().UTC()); err == nil {
		t.Fatal("unregistered jurisdiction was accepted")
	}
	weakened, err := policy.WithRule(TimeRecordRetentionRule{RecordClass: TimeRecordClassTimecard, Jurisdiction: "US-CA", MinimumDays: 365, AuthorityRef: "state"})
	if err == nil || weakened.Rules != nil {
		t.Fatalf("weakened jurisdiction rule accepted: policy=%+v err=%v", weakened, err)
	}
}

func TestTodo_TCLOCK_017_Property(t *testing.T) {
	policy := DefaultTimeRecordRetentionPolicy()
	statePolicy, err := policy.WithRule(TimeRecordRetentionRule{RecordClass: TimeRecordClassTimecard, Jurisdiction: "US-CA", MinimumDays: 1460, AuthorityRef: "STATE-RETENTION"})
	if err != nil {
		t.Fatal(err)
	}
	decl := retentionDeclaration(TimeRecordClassTimecard)
	decl.Jurisdiction = "US-CA"
	decision, err := EvaluateTimeRecordRetention(statePolicy, decl, decl.CutoffAt.AddDate(3, 0, 0))
	if err != nil || decision.DispositionAllowed || decision.Rule.MinimumDays != 1460 {
		t.Fatalf("longer jurisdiction rule=%+v err=%v", decision, err)
	}
	if _, err := policy.WithRule(TimeRecordRetentionRule{}); err == nil {
		t.Fatal("empty retention rule was accepted")
	}
}
