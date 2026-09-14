package messaging

import (
	"testing"
	"time"
)

// MSG-012 RED: governed bulk communications before bulk.go exists.
func TestTodo_MSG_012(t *testing.T) {
	intent := bulkTestIntent()
	audience := []string{"alice", "bob", "carol", "dave"}
	preview, err := PreviewBulk(BulkPreviewRequest{
		TenantID: "tenant-1", Intent: intent, Candidates: audience,
		Exclusions: []string{"dave"}, CostPerRecipientCents: 2, RatePerMinute: 60,
	})
	if err != nil {
		t.Fatalf("PreviewBulk: %v", err)
	}
	if preview.Recipients != 3 || preview.Excluded != 1 || preview.EstimatedCostCents != 6 {
		t.Fatalf("preview=%+v, want 3 recipients, 1 exclusion, cost 6", preview)
	}

	campaign, err := ScheduleBulk(BulkScheduleRequest{
		TenantID: "tenant-1", CampaignID: "camp-1", Intent: intent,
		Audience: audience, Exclusions: []string{"dave"}, BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("ScheduleBulk: %v", err)
	}
	// RED: duplicate recipients are refused, not silently deduped.
	if _, err := ScheduleBulk(BulkScheduleRequest{
		TenantID: "tenant-1", CampaignID: "camp-dupe", Intent: intent,
		Audience: []string{"alice", "alice"}, BatchSize: 2,
	}); err == nil {
		t.Fatal("duplicate recipients accepted")
	}

	ledger := NewBulkLedger()
	if err := ledger.Record(campaign, BulkResult{Recipient: "alice", Status: BulkSent}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := ledger.Record(campaign, BulkResult{Recipient: "bob", Status: BulkFailed, Failure: "provider timeout"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// RED: aggregate success must not hide the failure.
	report, err := ledger.Reconcile(campaign)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if report.Sent != 1 || report.Failed != 1 || len(report.Failures) != 1 || len(report.Pending) != 1 {
		t.Fatalf("report=%+v, want 1 sent, 1 failed, 1 pending", report)
	}
	// RED: the frozen audience rejects a mutated resubmission.
	mutated := campaign
	mutated.Audience.Digest = "sha256:tampered"
	if err := ledger.Record(mutated, BulkResult{Recipient: "carol", Status: BulkSent}); err == nil {
		t.Fatal("mutated audience accepted")
	}
}

func bulkTestIntent() MessageIntent {
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	return MessageIntent{
		IntentID: "intent-bulk-1", TenantID: "tenant-1", OrganizationScope: "org-1",
		Purpose: PurposeNotice, AudienceExpression: "all-managers", AudienceResolutionPolicy: "frozen-snapshot",
		TemplateRef: "tmpl-1", ParametersRef: "params-1",
		Classification: "internal", Urgency: "routine", DeliveryRequirement: RequirementDelivered,
		ReplyMode: ReplyNotAllowed, CorrelationID: "corr-1", ExpiresAt: future,
	}
}
