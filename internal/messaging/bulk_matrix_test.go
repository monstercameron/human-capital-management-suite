package messaging

import (
	"sync"
	"testing"
)

func msg012Campaign(t *testing.T) BulkCampaign {
	t.Helper()
	campaign, err := ScheduleBulk(BulkScheduleRequest{
		TenantID: "tenant-1", CampaignID: "camp-1", Intent: bulkTestIntent(),
		Audience: []string{"alice", "bob", "carol", "dave"}, Exclusions: []string{"dave"}, BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("ScheduleBulk: %v", err)
	}
	return campaign
}

// TestTodo_MSG_012_Race: concurrent recordings serialize without losing
// or duplicating outcomes.
func TestTodo_MSG_012_Race(t *testing.T) {
	campaign := msg012Campaign(t)
	ledger := NewBulkLedger()
	recipients := []string{"alice", "bob", "carol"}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				r := recipients[(n+i)%len(recipients)]
				_ = ledger.Record(campaign, BulkResult{Recipient: r, Status: BulkSent, Attempt: 1})
			}
		}(g)
	}
	wg.Wait()
	report, err := ledger.Reconcile(campaign)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report.Sent != 3 {
		t.Fatalf("report=%+v, want exactly the 3 frozen recipients sent", report)
	}
}

// TestTodo_MSG_012_Integration: a campaign over an accepted intent runs
// batches to completion with every outcome preserved through pause,
// resume and kill.
func TestTodo_MSG_012_Integration(t *testing.T) {
	campaign := msg012Campaign(t)
	ledger := NewBulkLedger()
	// Batch one.
	if err := ledger.Record(campaign, BulkResult{Recipient: "alice", Status: BulkSent, Attempt: 1}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	ledger.Pause(campaign.CampaignID)
	if err := ledger.Record(campaign, BulkResult{Recipient: "bob", Status: BulkSent, Attempt: 1}); err == nil {
		t.Fatal("recording while paused accepted")
	}
	ledger.Resume(campaign.CampaignID)
	if err := ledger.Record(campaign, BulkResult{Recipient: "bob", Status: BulkFailed, Attempt: 1, Failure: "provider timeout"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Batch two resumes from the pending cursor.
	report, err := ledger.Reconcile(campaign)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(report.Pending) != 1 || report.Pending[0] != "carol" {
		t.Fatalf("pending=%q, want the unrecorded cursor", report.Pending)
	}
	if err := ledger.Record(campaign, BulkResult{Recipient: "carol", Status: BulkSent, Attempt: 2}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	ledger.Kill(campaign.CampaignID)
	if err := ledger.Record(campaign, BulkResult{Recipient: "carol", Status: BulkSent, Attempt: 3}); err == nil {
		t.Fatal("recording after kill accepted")
	}
	report, err = ledger.Reconcile(campaign)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report.Sent != 2 || report.Failed != 1 || report.Complete {
		t.Fatalf("report=%+v, want 2 sent, 1 failed, killed campaigns never complete", report)
	}
}

// TestTodo_MSG_012_Fault: intent, audience, tenant and recipient faults
// fail closed with typed errors.
func TestTodo_MSG_012_Fault(t *testing.T) {
	intent := bulkTestIntent()
	badIntent := intent
	badIntent.Purpose = "SPAM"
	if _, err := ScheduleBulk(BulkScheduleRequest{TenantID: "tenant-1", CampaignID: "c", Intent: badIntent, Audience: []string{"a"}, BatchSize: 1}); err == nil {
		t.Fatal("invalid intent scheduled")
	}
	mismatched := intent
	mismatched.TenantID = "other-tenant"
	if _, err := ScheduleBulk(BulkScheduleRequest{TenantID: "tenant-1", CampaignID: "c", Intent: mismatched, Audience: []string{"a"}, BatchSize: 1}); err == nil {
		t.Fatal("cross-tenant intent scheduled")
	}
	for _, audience := range [][]string{nil, {}, {"alice", ""}, {"alice", "bob", "alice"}} {
		if _, err := ScheduleBulk(BulkScheduleRequest{TenantID: "tenant-1", CampaignID: "c", Intent: intent, Audience: audience, BatchSize: 1}); err == nil {
			t.Fatalf("audience %q scheduled", audience)
		}
	}
	allExcluded, err := ScheduleBulk(BulkScheduleRequest{TenantID: "tenant-1", CampaignID: "c", Intent: intent, Audience: []string{"a"}, Exclusions: []string{"a"}, BatchSize: 1})
	if err == nil {
		_ = allExcluded
		t.Fatal("fully excluded audience scheduled")
	}
	if _, err := PreviewBulk(BulkPreviewRequest{TenantID: "tenant-1", Intent: intent, Candidates: []string{"a"}, RatePerMinute: 0}); err == nil {
		t.Fatal("zero rate previewed")
	}
	campaign := msg012Campaign(t)
	ledger := NewBulkLedger()
	if err := ledger.Record(campaign, BulkResult{Recipient: "mallory", Status: BulkSent}); err == nil {
		t.Fatal("outside-audience recipient recorded")
	}
	if err := ledger.Record(campaign, BulkResult{Recipient: "alice", Status: BulkFailed}); err == nil {
		t.Fatal("causeless failure recorded")
	}
	if err := ledger.Record(campaign, BulkResult{Recipient: "", Status: BulkSent}); err == nil {
		t.Fatal("empty recipient recorded")
	}
}
