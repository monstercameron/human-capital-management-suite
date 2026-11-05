package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
)

func smbBill002CardFixture(t *testing.T) ([]workorderbilling.BillSourceView, *workorderbilling.BillPreview) {
	t.Helper()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	registry := workorderbilling.NewSourceClaimRegistry(func() time.Time { return now })
	op := workorderbilling.BillOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "biller-1", BillingAllowed: true}
	if err := registry.RegisterRate(op, workorderbilling.BillRateInput{Ref: "rate:USD90/hour", Currency: "USD", Amount: "90", Unit: "hour"}); err != nil {
		t.Fatal(err)
	}
	source, err := registry.RegisterSource(op, workorderbilling.BillSourceInput{
		Owner: "JOB007", RootID: "alloc-7", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: workorderbilling.BillKindTime, UnitDimension: "time", Unit: "minute",
		ApprovedQty: "120", RateRef: "rate:USD90/hour",
		PricingMode: workorderbilling.PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-7", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-21T15:04:05Z",
		Description: "Field labor, Oct 21", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Claim(op, workorderbilling.BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "30",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: workorderbilling.BillClaimIssue,
	}, "card-claim"); err != nil {
		t.Fatal(err)
	}
	odd, err := registry.RegisterSource(op, workorderbilling.BillSourceInput{
		Owner: "JOB007", RootID: "alloc-odd", Revision: 1,
		WorkOrderID: "wo-1", JobRef: "job-9",
		Kind: workorderbilling.BillKindTime, UnitDimension: "time", Unit: "minute",
		ApprovedQty: "7", RateRef: "rate:USD90/hour",
		PricingMode: workorderbilling.PricingUnitPrice, BillableUnit: "hour",
		EvidenceRef: "alloc-appr-9", ApprovedBy: "supervisor-2", ApprovedAt: "2026-10-21T15:04:05Z",
		Description: "Call-out, Oct 21", ServiceDate: "2026-10-21",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = odd
	views, err := registry.QuerySources(op, "wo-1",
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := registry.PreviewClaim(op, workorderbilling.BillClaimInput{
		FamilyID: source.FamilyID, Revision: 1, Qty: "90",
		PolicyID: "contract-a", PolicyVersion: "v1", Kind: workorderbilling.BillClaimIssue,
	})
	if err != nil {
		t.Fatal(err)
	}
	return views, &preview
}

// TestTodo_SMB_BILL_002_Browser renders the Job Billing "Add approved work"
// drawer the way the billing surface serves it: eligible sources with
// selection plus quantity entry, needs-attention rows with exact reasons,
// the server totals preview, the availability-change notice and the
// review-draft link, localized across en-US, de-DE and ar RTL.
func TestTodo_SMB_BILL_002_Browser(t *testing.T) {
	views, preview := smbBill002CardFixture(t)

	markup, err := ui.RenderToString(SMBBillClaimsCard(views, preview, false, "/workspace/app/billing/drafts/draft-2", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Add approved work", "Eligible (1)", "Needs attention (1)",
		"Field labor, Oct 21", "2026-10-21", "120 minute", "30 minute", "90 minute",
		"USD90/hour", "Call-out, Oct 21", "no exact billable conversion",
		"Preview: 1.5 hour = 135.00 USD", "Review draft", "/workspace/app/billing/drafts/draft-2",
		"type=\"checkbox\"", "Quantity to bill", "tabIndex=\"-1\"",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("claims card missing %q", want)
		}
	}
	if strings.Contains(markup, "Availability changed") {
		t.Errorf("steady card shows the conflict notice")
	}

	conflicted, err := ui.RenderToString(SMBBillClaimsCard(views, preview, true, "", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conflicted, "Availability changed") || !strings.Contains(conflicted, "selections were kept") {
		t.Errorf("conflict card misses the retention notice")
	}

	german, err := ui.RenderToString(SMBBillClaimsCard(views, preview, false, "", "de-DE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Genehmigte Arbeit", "Zulässig (1)", "Erfordert Aufmerksamkeit (1)", "Vorschau: 1.5 hour = 135.00 USD"} {
		if !strings.Contains(german, want) {
			t.Errorf("German claims card missing %q", want)
		}
	}

	rtl, err := ui.RenderToString(SMBBillClaimsCard(views, preview, false, "", "ar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dir=\"rtl\"", "lang=\"ar\"", "إضافة عمل معتمد", "مؤهل (1)"} {
		if !strings.Contains(rtl, want) {
			t.Errorf("Arabic claims card missing %q", want)
		}
	}

	empty, err := ui.RenderToString(SMBBillClaimsCard(nil, nil, false, "", "en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No approved sources", "Select sources to preview"} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty card missing %q", want)
		}
	}
}
