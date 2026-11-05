// TestTodo_SMB_BILL_001 covers SMB-BILL-001: explicit customer invoices and
// conserved receivable/payment allocations in the workorderbilling ownership
// boundary. Drafts never auto-create invoices; issuance consumes unique
// per-issuer/series numbers; collection state derives from signed,
// idempotent receivable events; restart preserves every balance.
package workorderbilling

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func smbBillManualLines(t *testing.T) []InvoiceLine {
	t.Helper()
	return []InvoiceLine{
		{ID: "line-1", Description: "Quarterly maintenance", Quantity: dec(t, "1.00", 2), UnitPrice: money(t, "100.00"), Amount: money(t, "100.00"), RateRef: "rate-card-2026", PolicyRef: "policy-std"},
	}
}

func smbBillDraftReq(id string, t *testing.T) DraftInvoiceRequest {
	t.Helper()
	return DraftInvoiceRequest{
		InvoiceID: id, IssuerEntity: "acme-llc", IssuerSeries: "ACME-2026", CustomerID: "cust-7",
		Authority: "local", Currency: "USD",
		Source: InvoiceSource{Kind: InvoiceSourceManual, Manual: ManualSource{RateRef: "rate-card-2026", PolicyRef: "policy-std", PolicyVersion: "v3", Evidence: "evidence:quote-accepted-9"}},
		Lines:  smbBillManualLines(t), TaxPolicyRef: "tax:standard-v1", IssueDate: "2026-10-01", DueDate: "2026-10-31",
	}
}

func smbBillFullCaps() ActorCaps {
	return ActorCaps{CanSubmit: true, CanApprove: true, CanIssue: true, CanVoid: true}
}

func smbBillIssue(t *testing.T, store *InvoiceStore, tenant, id string) CustomerInvoice {
	t.Helper()
	caps := smbBillFullCaps()
	draft, _, err := store.Draft(tenant, "clerk-1", "draft-"+id, smbBillDraftReq(id, t))
	if err != nil {
		t.Fatal(err)
	}
	submitted, _, err := store.SubmitForApproval(tenant, "clerk-1", caps, draft.ID, draft.Revision, "submit-"+id, "2026-10-01T09:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := store.Approve(tenant, "manager-1", caps, draft.ID, submitted.Revision, "approve-"+id, "2026-10-01T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := store.Issue(tenant, "manager-1", caps, draft.ID, ready.Revision, "issue-"+id, "2026-10-01T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func smbBillEvent(tenant, invoice, id string, kind ReceivableEventKind, amount string, t *testing.T) ReceivableEvent {
	t.Helper()
	return ReceivableEvent{EventID: id, TenantID: tenant, InvoiceID: invoice, Kind: kind, Amount: money(t, amount), At: "2026-10-15T12:00:00Z", Actor: "cashier-1", Ref: "ref-" + id}
}

func TestTodo_SMB_BILL_001(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-1")
	if issued.Status != InvoiceIssued || issued.InvoiceNumber == "" || issued.Total.String() != "100.00 USD" {
		t.Fatalf("issued invoice = %+v, want ISSUED 100.00 USD with a number", issued)
	}
	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-1", "ev-credit-1", ReceivableCreditPosted, "10.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-1", "ev-receipt-1", ReceivableReceiptAllocated, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	projection, err := ledger.Explain("inv-1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "50.00 USD" || projection.Status != CollectionPartial {
		t.Fatalf("100 - 10 credit - 40 receipt = %s (%s), want 50.00 USD PARTIAL", projection.Remaining, projection.Status)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-1", "ev-credit-1", ReceivableCreditPosted, "10.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-1", "ev-receipt-1", ReceivableReceiptAllocated, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	projection, err = ledger.Explain("inv-1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "50.00 USD" {
		t.Fatalf("duplicate events changed remaining to %s, want 50.00 USD", projection.Remaining)
	}
	snapshot, err := ledger.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewReceivableLedger("tenant-a")
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	projection, err = restored.Explain("inv-1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "50.00 USD" || projection.Status != CollectionPartial {
		t.Fatalf("post-restart remaining = %s (%s), want 50.00 USD PARTIAL", projection.Remaining, projection.Status)
	}
}

func TestTodo_SMB_BILL_001_Unit(t *testing.T) {
	store := NewInvoiceStore()
	caps := smbBillFullCaps()
	draft, _, err := store.Draft("tenant-a", "clerk-1", "draft-u1", smbBillDraftReq("inv-u1", t))
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != InvoiceDraft || draft.Revision != 1 || draft.InvoiceNumber != "" {
		t.Fatalf("fresh draft = %+v, want DRAFT revision 1 with no number", draft)
	}
	if _, _, err := store.Issue("tenant-a", "manager-1", caps, draft.ID, draft.Revision, "issue-early", "t"); !errors.Is(err, ErrInvoiceDenied) {
		t.Fatalf("issue from DRAFT err=%v, want denial", err)
	}
	if _, _, err := store.Void("tenant-a", "manager-1", caps, draft.ID, draft.Revision, "void-early", "t", "not-issued"); !errors.Is(err, ErrInvoiceDenied) {
		t.Fatalf("void from DRAFT err=%v, want denial", err)
	}
	submitted, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, draft.ID, draft.Revision, "submit-u1", "t")
	if err != nil || submitted.Status != InvoiceApprovalPending {
		t.Fatalf("submit = %+v, err=%v", submitted, err)
	}
	if _, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, draft.ID, submitted.Revision, "submit-u1b", "t"); !errors.Is(err, ErrInvoiceDenied) {
		t.Fatalf("double submit err=%v, want denial", err)
	}
	ready, _, err := store.Approve("tenant-a", "manager-1", caps, draft.ID, submitted.Revision, "approve-u1", "t")
	if err != nil || ready.Status != InvoiceReady {
		t.Fatalf("approve = %+v, err=%v", ready, err)
	}
	issued, _, err := store.Issue("tenant-a", "manager-1", caps, draft.ID, ready.Revision, "issue-u1", "t")
	if err != nil || issued.Status != InvoiceIssued || issued.InvoiceNumber != "ACME-2026-000001" {
		t.Fatalf("issue = %+v, err=%v, want first series number", issued, err)
	}
	second := smbBillIssue(t, store, "tenant-a", "inv-u2")
	if second.InvoiceNumber != "ACME-2026-000002" {
		t.Fatalf("second issue number = %q, want sequence advance without reuse", second.InvoiceNumber)
	}
	if _, _, err := store.Void("tenant-a", "manager-1", caps, issued.ID, issued.Revision, "void-u1", "t", ""); !errors.Is(err, ErrRejected) {
		t.Fatalf("void without reason err=%v, want rejection", err)
	}
	voided, _, err := store.Void("tenant-a", "manager-1", caps, issued.ID, issued.Revision, "void-u1", "t", "duplicate billing")
	if err != nil || voided.Status != InvoiceVoided {
		t.Fatalf("void = %+v, err=%v", voided, err)
	}
	preserved, err := store.Get("tenant-a", issued.ID)
	if err != nil || preserved.Total.String() != "100.00 USD" || preserved.InvoiceNumber != "ACME-2026-000001" {
		t.Fatalf("void dropped the conserved artifact: %+v, err=%v", preserved, err)
	}

	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(second); err != nil {
		t.Fatal(err)
	}
	open, err := ledger.Explain("inv-u2", "2026-10-15")
	if err != nil || open.Status != CollectionOpen || open.Remaining.String() != "100.00 USD" {
		t.Fatalf("fresh projection = %+v, err=%v, want OPEN 100.00", open, err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-u2", "ev-overpay", ReceivableReceiptAllocated, "120.00", t)); err != nil {
		t.Fatal(err)
	}
	overpaid, err := ledger.Explain("inv-u2", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if overpaid.Remaining.String() != "0.00 USD" || overpaid.UnappliedCredit.String() != "20.00 USD" || overpaid.Status != CollectionPaid {
		t.Fatalf("overpayment = %+v, want 0 remaining with 20.00 unapplied credit", overpaid)
	}
	if err := ledger.Post(ReceivableEvent{EventID: "ev-dispute-1", TenantID: "tenant-a", InvoiceID: "inv-u2", Kind: ReceivableDisputeOpened, Amount: money(t, "0.00"), At: "t", Actor: "support-1"}); err != nil {
		t.Fatal(err)
	}
	disputed, err := ledger.Explain("inv-u2", "2026-10-15")
	if err != nil || disputed.Status != CollectionDisputed {
		t.Fatalf("dispute projection = %+v, err=%v, want DISPUTED", disputed, err)
	}
	if err := ledger.Post(ReceivableEvent{EventID: "ev-dispute-2", TenantID: "tenant-a", InvoiceID: "inv-u2", Kind: ReceivableDisputeResolved, Amount: money(t, "0.00"), At: "t", Actor: "support-1"}); err != nil {
		t.Fatal(err)
	}
	resolved, err := ledger.Explain("inv-u2", "2026-10-15")
	if err != nil || resolved.Status != CollectionPaid {
		t.Fatalf("resolved projection = %+v, err=%v, want PAID again", resolved, err)
	}

	third := smbBillIssue(t, store, "tenant-a", "inv-u3")
	ledger3 := NewReceivableLedger("tenant-a")
	if err := ledger3.Register(third); err != nil {
		t.Fatal(err)
	}
	overdue, err := ledger3.Explain("inv-u3", "2026-11-05")
	if err != nil || overdue.Status != CollectionOverdue {
		t.Fatalf("past-due projection = %+v, err=%v, want OVERDUE", overdue, err)
	}
}

func smbBillPermutations(n int) [][]int {
	var out [][]int
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	var generate func(k int)
	generate = func(k int) {
		if k == 1 {
			clone := append([]int(nil), order...)
			out = append(out, clone)
			return
		}
		for i := 0; i < k; i++ {
			generate(k - 1)
			if k%2 == 0 {
				order[i], order[k-1] = order[k-1], order[i]
			} else {
				order[0], order[k-1] = order[k-1], order[0]
			}
		}
	}
	generate(n)
	return out
}

func TestTodo_SMB_BILL_001_Property(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-p1")
	kinds := []struct {
		id     string
		kind   ReceivableEventKind
		amount string
	}{
		{"ev-p-credit", ReceivableCreditPosted, "10.00"},
		{"ev-p-receipt", ReceivableReceiptAllocated, "40.00"},
		{"ev-p-refund", ReceivableRefundAllocated, "5.00"},
		{"ev-p-reversal", ReceivableReversalPosted, "5.00"},
	}
	// Arrival order matters the way a real ledger works: a refund or
	// reversal against not-yet-settled money is refused, never silently
	// held. Every permutation must still converge to the same balances
	// once prerequisites arrive and the refused events are reposted.
	for i, perm := range smbBillPermutations(len(kinds)) {
		ledger := NewReceivableLedger("tenant-a")
		if err := ledger.Register(issued); err != nil {
			t.Fatal(err)
		}
		var deferred []int
		for _, slot := range perm {
			item := kinds[slot]
			if err := ledger.Post(smbBillEvent("tenant-a", "inv-p1", item.id, item.kind, item.amount, t)); err != nil {
				if item.kind != ReceivableRefundAllocated && item.kind != ReceivableReversalPosted {
					t.Fatalf("permutation %d refused independent event %s: %v", i, item.id, err)
				}
				deferred = append(deferred, slot)
			}
		}
		for _, slot := range deferred {
			item := kinds[slot]
			if err := ledger.Post(smbBillEvent("tenant-a", "inv-p1", item.id, item.kind, item.amount, t)); err != nil {
				t.Fatalf("permutation %d deferred event %s never applied: %v", i, item.id, err)
			}
		}
		projection, err := ledger.Explain("inv-p1", "2026-10-15")
		if err != nil {
			t.Fatal(err)
		}
		if projection.Remaining.String() != "60.00 USD" || projection.UnappliedCredit.String() != "0.00 USD" {
			t.Fatalf("permutation %d remaining = %s / %s, want 60.00 / 0.00", i, projection.Remaining, projection.UnappliedCredit)
		}
	}

	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 5; round++ {
		if err := ledger.Post(smbBillEvent("tenant-a", "inv-p1", "ev-p-retry", ReceivableReceiptAllocated, "40.00", t)); err != nil {
			t.Fatal(err)
		}
	}
	projection, err := ledger.Explain("inv-p1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Receipts.String() != "40.00 USD" || projection.Remaining.String() != "60.00 USD" {
		t.Fatalf("retried event applied %s, remaining %s; want single 40.00 / 60.00", projection.Receipts, projection.Remaining)
	}
	clash := smbBillEvent("tenant-a", "inv-p1", "ev-p-retry", ReceivableReceiptAllocated, "41.00", t)
	if err := ledger.Post(clash); !errors.Is(err, ErrInvoiceConflict) {
		t.Fatalf("reused event id with changed payload err=%v, want conflict", err)
	}
}

func TestTodo_SMB_BILL_001_Integration(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-i1")
	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-i1", "ev-i-receipt", ReceivableReceiptAllocated, "100.00", t)); err != nil {
		t.Fatal(err)
	}
	projection, err := ledger.Explain("inv-i1", "2026-10-15")
	if err != nil || projection.Status != CollectionPaid || projection.Remaining.String() != "0.00 USD" {
		t.Fatalf("settled projection = %+v, err=%v, want PAID 0.00", projection, err)
	}

	draft, err := Build(BuildRequest{ID: "bill-i9", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}})
	if err != nil {
		t.Fatal(err)
	}
	draft, err = draft.Transition("approve", "finance-1", "2026-10-01T10:00:00Z", "approve-i9", "")
	if err != nil {
		t.Fatal(err)
	}
	draft, err = draft.Transition("issue", "finance-1", "2026-10-01T11:00:00Z", "issue-i9", "external-invoice-ref")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindByDraft("tenant-a", draft.ID); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("issued BillingDraft auto-created invoice lookup err=%v, want not-found", err)
	}
	linkedReq := smbBillDraftReq("inv-i9", t)
	linkedReq.Source = InvoiceSource{Kind: InvoiceSourceWorkOrder, WorkOrderID: "wo-1", MigratedDraftID: draft.ID, MigrationEvidence: "evidence:migration-batch-3"}
	linked, _, err := store.Draft("tenant-a", "clerk-1", "draft-i9", linkedReq)
	if err != nil || linked.Source.MigratedDraftID != draft.ID {
		t.Fatalf("explicit migrated link = %+v, err=%v", linked, err)
	}
	unlinkedReq := smbBillDraftReq("inv-i10", t)
	unlinkedReq.Source = InvoiceSource{Kind: InvoiceSourceWorkOrder, WorkOrderID: "wo-1", MigratedDraftID: draft.ID}
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-i10", unlinkedReq); !errors.Is(err, ErrRejected) {
		t.Fatalf("migrated link without evidence err=%v, want rejection", err)
	}

	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewInvoiceStore()
	if err := restarted.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	same, err := restarted.Get("tenant-a", "inv-i1")
	if err != nil || same.InvoiceNumber != issued.InvoiceNumber || same.Digest != issued.Digest || same.Revision != issued.Revision {
		t.Fatalf("restarted record = %+v, err=%v; want the authoritative issued record", same, err)
	}
	replay, receipt, err := restarted.Issue("tenant-a", "manager-1", smbBillFullCaps(), "inv-i1", issued.Revision-1, "issue-inv-i1", "2026-10-01T11:00:00Z")
	if err != nil || receipt.Number != issued.InvoiceNumber || replay.InvoiceNumber != issued.InvoiceNumber {
		t.Fatalf("post-restart replay = %+v / %+v, err=%v; want the original receipt", replay, receipt, err)
	}
}

func TestTodo_SMB_BILL_001_Security(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-s1")

	if _, err := store.Get("tenant-b", "inv-s1"); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("cross-tenant read err=%v, want neutral not-found", err)
	}
	if _, err := store.Get("tenant-a", "inv-guessed"); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("guessed id read err=%v, want the same neutral not-found", err)
	}
	caps := smbBillFullCaps()
	if _, _, err := store.Void("tenant-b", "intruder", caps, "inv-s1", issued.Revision, "void-x", "t", "takeover"); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("cross-tenant void err=%v, want neutral not-found", err)
	}
	if _, _, err := store.Void("tenant-a", "manager-1", caps, "inv-s1", issued.Revision+5, "void-stale", "t", "stale"); !errors.Is(err, ErrInvoiceConflict) {
		t.Fatalf("stale revision void err=%v, want conflict", err)
	}
	denied := ActorCaps{}
	if _, _, err := store.Approve("tenant-a", "clerk-1", denied, "inv-s1", issued.Revision, "approve-denied", "t"); !errors.Is(err, ErrInvoiceDenied) {
		t.Fatalf("unapproved role err=%v, want denial", err)
	}
	foreignReq := smbBillDraftReq("inv-s2", t)
	foreignReq.Authority = "external:acme-erp:v7"
	foreign, _, err := store.Draft("tenant-a", "clerk-1", "draft-s2", foreignReq)
	if err != nil {
		t.Fatal(err)
	}
	submitted, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, foreign.ID, foreign.Revision, "submit-s2", "t")
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := store.Approve("tenant-a", "manager-1", caps, foreign.ID, submitted.Revision, "approve-s2", "t")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Issue("tenant-a", "manager-1", caps, foreign.ID, ready.Revision, "issue-s2", "t"); !errors.Is(err, ErrInvoiceDenied) {
		t.Fatalf("external-master issue err=%v, want denial (observations only)", err)
	}
	mysteryReq := smbBillDraftReq("inv-s3", t)
	mysteryReq.Authority = "mystery"
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-s3", mysteryReq); !errors.Is(err, ErrRejected) {
		t.Fatalf("unknown authority draft err=%v, want rejection", err)
	}

	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	injected := smbBillEvent("tenant-b", "inv-s1", "ev-inject", ReceivableReceiptAllocated, "100.00", t)
	if err := ledger.Post(injected); !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("cross-tenant event err=%v, want neutral not-found", err)
	}
	projection, err := ledger.Explain("inv-s1", "2026-10-15")
	if err != nil || projection.Receipts.String() != "0.00 USD" {
		t.Fatalf("injection left receipts %s, want 0.00", projection.Receipts)
	}
}

func TestTodo_SMB_BILL_001_Race(t *testing.T) {
	store := NewInvoiceStore()
	caps := smbBillFullCaps()
	const invoices = 8
	numbers := make([]string, invoices)
	reqs := make([]DraftInvoiceRequest, invoices)
	for i := 0; i < invoices; i++ {
		reqs[i] = smbBillDraftReq("inv-r"+string(rune('a'+i)), t)
	}
	var wg sync.WaitGroup
	for i := 0; i < invoices; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			id := "inv-r" + string(rune('a'+slot))
			draft, _, err := store.Draft("tenant-a", "clerk-1", "draft-"+id, reqs[slot])
			if err != nil {
				t.Errorf("draft %s: %v", id, err)
				return
			}
			submitted, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, id, draft.Revision, "submit-"+id, "t")
			if err != nil {
				t.Errorf("submit %s: %v", id, err)
				return
			}
			ready, _, err := store.Approve("tenant-a", "manager-1", caps, id, submitted.Revision, "approve-"+id, "t")
			if err != nil {
				t.Errorf("approve %s: %v", id, err)
				return
			}
			issued, _, err := store.Issue("tenant-a", "manager-1", caps, id, ready.Revision, "issue-"+id, "t")
			if err != nil {
				t.Errorf("issue %s: %v", id, err)
				return
			}
			numbers[slot] = issued.InvoiceNumber
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, number := range numbers {
		if number == "" || seen[number] {
			t.Fatalf("race numbers = %v, want 8 unique consumed numbers", numbers)
		}
		seen[number] = true
	}

	contended := smbBillDraftReq("inv-race-1", t)
	draft, _, err := store.Draft("tenant-a", "clerk-1", "draft-race-1", contended)
	if err != nil {
		t.Fatal(err)
	}
	submitted, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, draft.ID, draft.Revision, "submit-race-1", "t")
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := store.Approve("tenant-a", "manager-1", caps, draft.ID, submitted.Revision, "approve-race-1", "t")
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int64
	var conflicts atomic.Int64
	for i := 0; i < invoices; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, _, err := store.Issue("tenant-a", "manager-1", caps, draft.ID, ready.Revision, "issue-race-1-try-"+string(rune('0'+slot)), "t")
			if err == nil {
				wins.Add(1)
			} else if errors.Is(err, ErrInvoiceConflict) {
				conflicts.Add(1)
			} else {
				t.Errorf("contended issue err=%v, want success or conflict", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 || conflicts.Load() != invoices-1 {
		t.Fatalf("contended issue wins=%d conflicts=%d, want 1 win and %d conflicts", wins.Load(), conflicts.Load(), invoices-1)
	}
	final, err := store.Get("tenant-a", "inv-race-1")
	if err != nil || final.Revision != ready.Revision+1 {
		t.Fatalf("contended issue left %+v, err=%v; want one issued artifact", final, err)
	}

	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(final); err != nil {
		t.Fatal(err)
	}
	raceEvent := smbBillEvent("tenant-a", "inv-race-1", "ev-race-receipt", ReceivableReceiptAllocated, "40.00", t)
	for i := 0; i < invoices; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ledger.Post(raceEvent); err != nil {
				t.Errorf("concurrent duplicate event: %v", err)
			}
		}()
	}
	wg.Wait()
	projection, err := ledger.Explain("inv-race-1", "2026-10-15")
	if err != nil || projection.Receipts.String() != "40.00 USD" || projection.Remaining.String() != "60.00 USD" {
		t.Fatalf("concurrent duplicates left receipts %s remaining %s, want 40.00 / 60.00", projection.Receipts, projection.Remaining)
	}
}

func TestTodo_SMB_BILL_001_Fault(t *testing.T) {
	store := NewInvoiceStore()
	caps := smbBillFullCaps()
	draft, _, err := store.Draft("tenant-a", "clerk-1", "draft-f1", smbBillDraftReq("inv-f1", t))
	if err != nil {
		t.Fatal(err)
	}
	submitted, _, err := store.SubmitForApproval("tenant-a", "clerk-1", caps, draft.ID, draft.Revision, "submit-f1", "t")
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := store.Approve("tenant-a", "manager-1", caps, draft.ID, submitted.Revision, "approve-f1", "t")
	if err != nil {
		t.Fatal(err)
	}
	committed, receipt, err := store.Issue("tenant-a", "manager-1", caps, draft.ID, ready.Revision, "issue-f1", "2026-10-01T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	// Crash after domain commit before the reply reaches the caller: the only
	// durable facts are the snapshot. Restore and replay the same key.
	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	recovered := NewInvoiceStore()
	if err := recovered.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	replayed, replayReceipt, err := recovered.Issue("tenant-a", "manager-1", caps, draft.ID, ready.Revision, "issue-f1", "2026-10-01T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if replayReceipt.Number != receipt.Number || replayed.InvoiceNumber != committed.InvoiceNumber || replayed.Revision != committed.Revision {
		t.Fatalf("crash replay = %+v / %+v, want the one committed effect %+v", replayed, replayReceipt, committed)
	}
	next := smbBillIssue(t, recovered, "tenant-a", "inv-f2")
	if next.InvoiceNumber == committed.InvoiceNumber {
		t.Fatalf("recovery reused number %q", next.InvoiceNumber)
	}

	clashReq := smbBillDraftReq("inv-f3", t)
	if _, _, err := recovered.Draft("tenant-a", "clerk-1", "draft-f1", clashReq); !errors.Is(err, ErrInvoiceConflict) {
		t.Fatalf("reused key with changed payload err=%v, want conflict, never invented success", err)
	}
}

func TestTodo_SMB_BILL_001_Browser(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-b1")
	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	projection, err := ledger.Explain("inv-b1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	view := ProjectInvoice(issued, projection.Status, "", "")
	if view.Badges.Invoice != "ISSUED" || view.Badges.Collection != "OPEN" {
		t.Fatalf("badges = %+v, want ISSUED invoice with OPEN collection", view.Badges)
	}
	if view.Badges.Delivery != "not-observed" || view.Badges.AccountingSync != "not-synced" {
		t.Fatalf("badges = %+v, want independent not-observed delivery and not-synced accounting", view.Badges)
	}
	if view.AuthorityCTA != "" {
		t.Fatalf("local invoice offers CTA %q, want none", view.AuthorityCTA)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-b1", "ev-b-receipt", ReceivableReceiptAllocated, "100.00", t)); err != nil {
		t.Fatal(err)
	}
	settled, err := ledger.Explain("inv-b1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	paidView := ProjectInvoice(issued, settled.Status, "delivered", "acked")
	if paidView.Badges.Invoice != "ISSUED" || paidView.Badges.Collection != "PAID" || paidView.Badges.Delivery != "delivered" || paidView.Badges.AccountingSync != "acked" {
		t.Fatalf("settled badges = %+v, want independently labeled states", paidView.Badges)
	}

	foreignReq := smbBillDraftReq("inv-b2", t)
	foreignReq.Authority = "external:acme-erp:v7"
	foreign, _, err := store.Draft("tenant-a", "clerk-1", "draft-b2", foreignReq)
	if err != nil {
		t.Fatal(err)
	}
	foreignView := ProjectInvoice(foreign, CollectionOpen, "", "")
	if foreignView.AuthorityCTA != "Configure invoicing authority" || foreignView.SettingsLink == "" {
		t.Fatalf("foreign invoice view = %+v, want authority CTA with settings link", foreignView)
	}
}

func TestTodo_SMB_BILL_001_Conformance(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-c1")
	if err := issued.Validate(); err != nil {
		t.Fatalf("issued invoice fails Validate: %v", err)
	}
	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewInvoiceStore()
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	same, err := restored.Get("tenant-a", "inv-c1")
	if err != nil || same.Digest != issued.Digest {
		t.Fatalf("snapshot round-trip changed the conserved artifact: %+v, err=%v", same, err)
	}

	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	raw := []struct {
		id     string
		kind   ReceivableEventKind
		amount string
	}{
		{"ev-c-1", ReceivableCreditPosted, "10.00"},
		{"ev-c-2", ReceivableReceiptAllocated, "40.00"},
		{"ev-c-3", ReceivableRefundAllocated, "5.00"},
		{"ev-c-4", ReceivableReversalPosted, "5.00"},
	}
	var credits, receipts, refunds, reversals int64
	for _, item := range raw {
		if err := ledger.Post(smbBillEvent("tenant-a", "inv-c1", item.id, item.kind, item.amount, t)); err != nil {
			t.Fatal(err)
		}
		var cents int64
		for _, ch := range strings.Replace(item.amount, ".", "", 1) {
			cents = cents*10 + int64(ch-'0')
		}
		switch item.kind {
		case ReceivableCreditPosted:
			credits += cents
		case ReceivableReceiptAllocated:
			receipts += cents
		case ReceivableRefundAllocated:
			refunds += cents
		case ReceivableReversalPosted:
			reversals += cents
		}
	}
	projection, err := ledger.Explain("inv-c1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	wantNet := 10000 - credits - receipts + refunds + reversals
	var gotRemaining int64
	for _, ch := range strings.Replace(strings.TrimSuffix(projection.Remaining.String(), " USD"), ".", "", 1) {
		gotRemaining = gotRemaining*10 + int64(ch-'0')
	}
	if wantNet != gotRemaining {
		t.Fatalf("independent recomputation = %d cents, Explain = %d cents", wantNet, gotRemaining)
	}

	draft, err := Build(BuildRequest{ID: "bill-c1", WorkOrderID: "wo-1", Policy: policy(t), Sources: []Source{accepted()}})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Total.String() != "37.50 USD" || draft.Status != Draft {
		t.Fatalf("existing draft semantics changed: %+v", draft)
	}
}

func TestTodo_SMB_BILL_001_Mutation(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-m1")

	tampered := issued
	tampered.Total = money(t, "999.99")
	if err := tampered.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("mutated total err=%v, want rejection", err)
	}
	tampered = issued
	tampered.Lines = append([]InvoiceLine(nil), issued.Lines...)
	tampered.Lines[0].Amount = money(t, "1.00")
	if err := tampered.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("mutated line err=%v, want rejection", err)
	}
	tampered = issued
	tampered.Currency = "EUR"
	if err := tampered.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("mutated currency err=%v, want rejection", err)
	}
	tampered = issued
	tampered.Digest = issued.Digest + "00"
	if err := tampered.Validate(); !errors.Is(err, ErrRejected) {
		t.Fatalf("mutated digest err=%v, want rejection", err)
	}

	noEvidence := smbBillDraftReq("inv-m2", t)
	noEvidence.Source.Manual.Evidence = ""
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-m2", noEvidence); !errors.Is(err, ErrRejected) {
		t.Fatalf("manual source without evidence err=%v, want rejection", err)
	}
	fabricated := smbBillDraftReq("inv-m3", t)
	fabricated.Source.WorkOrderID = "wo-invented"
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-m3", fabricated); !errors.Is(err, ErrRejected) {
		t.Fatalf("fabricated work order on manual invoice err=%v, want rejection", err)
	}
	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-m1", "ev-m-credit", ReceivableCreditPosted, "100.01", t)); !errors.Is(err, ErrRejected) {
		t.Fatalf("credit above obligation err=%v, want rejection", err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-m1", "ev-m-refund", ReceivableRefundAllocated, "1.00", t)); !errors.Is(err, ErrRejected) {
		t.Fatalf("refund above settled receipts err=%v, want rejection", err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-m1", "ev-m-receipt", ReceivableReceiptAllocated, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-m1", "ev-m-reversal", ReceivableReversalPosted, "40.01", t)); !errors.Is(err, ErrRejected) {
		t.Fatalf("reversal above net receipts err=%v, want rejection", err)
	}
	eur, err := values.NewMoney("1.00", "EUR", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(ReceivableEvent{EventID: "ev-m-fx", TenantID: "tenant-a", InvoiceID: "inv-m1", Kind: ReceivableReceiptAllocated, Amount: eur, At: "t", Actor: "cashier-1"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("cross-currency event err=%v, want rejection", err)
	}
	if err := ledger.Post(ReceivableEvent{EventID: "ev-m-zero", TenantID: "tenant-a", InvoiceID: "inv-m1", Kind: ReceivableReceiptAllocated, Amount: money(t, "0.00"), At: "t", Actor: "cashier-1"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("zero-amount receipt err=%v, want rejection", err)
	}
	if err := ledger.Post(ReceivableEvent{EventID: "ev-m-dispute-money", TenantID: "tenant-a", InvoiceID: "inv-m1", Kind: ReceivableDisputeOpened, Amount: money(t, "5.00"), At: "t", Actor: "support-1"}); !errors.Is(err, ErrRejected) {
		t.Fatalf("money-carrying dispute err=%v, want rejection", err)
	}

	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(string(snapshot), "100.00", "999.99", 1)
	if err := NewInvoiceStore().Import([]byte(forged)); err == nil {
		t.Fatal("tampered snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_BILL_001_RefundReversalSymmetry(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-sym-1")
	ledger := NewReceivableLedger("tenant-a")
	if err := ledger.Register(issued); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-sym-1", "ev-sym-r1", ReceivableReceiptAllocated, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-sym-1", "ev-sym-v1", ReceivableReversalPosted, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Post(smbBillEvent("tenant-a", "inv-sym-1", "ev-sym-f1", ReceivableRefundAllocated, "40.00", t)); !errors.Is(err, ErrRejected) {
		t.Fatalf("refund after full reversal err=%v, want rejection (net settled is zero)", err)
	}
	projection, err := ledger.Explain("inv-sym-1", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "100.00 USD" {
		t.Fatalf("reversed receipt left remaining %s, want 100.00 USD", projection.Remaining)
	}

	second := smbBillIssue(t, store, "tenant-a", "inv-sym-2")
	ledger2 := NewReceivableLedger("tenant-a")
	if err := ledger2.Register(second); err != nil {
		t.Fatal(err)
	}
	if err := ledger2.Post(smbBillEvent("tenant-a", "inv-sym-2", "ev-sym-r2", ReceivableReceiptAllocated, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger2.Post(smbBillEvent("tenant-a", "inv-sym-2", "ev-sym-f2", ReceivableRefundAllocated, "40.00", t)); err != nil {
		t.Fatal(err)
	}
	if err := ledger2.Post(smbBillEvent("tenant-a", "inv-sym-2", "ev-sym-v2", ReceivableReversalPosted, "40.00", t)); !errors.Is(err, ErrRejected) {
		t.Fatalf("reversal after full refund err=%v, want rejection (net settled is zero)", err)
	}
	projection2, err := ledger2.Explain("inv-sym-2", "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}
	if projection2.Remaining.String() != "100.00 USD" {
		t.Fatalf("refunded receipt left remaining %s, want 100.00 USD", projection2.Remaining)
	}
}

func smbBillSnapshotMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func smbBillImportMap(t *testing.T, store *InvoiceStore, decoded map[string]any) error {
	t.Helper()
	raw, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return store.Import(raw)
}

func TestTodo_SMB_BILL_001_NumberRegressionRefused(t *testing.T) {
	store := NewInvoiceStore()
	first := smbBillIssue(t, store, "tenant-a", "inv-n1")
	second := smbBillIssue(t, store, "tenant-a", "inv-n2")
	if first.InvoiceNumber != "ACME-2026-000001" || second.InvoiceNumber != "ACME-2026-000002" {
		t.Fatalf("numbers = %q/%q, want 000001/000002", first.InvoiceNumber, second.InvoiceNumber)
	}
	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	decoded := smbBillSnapshotMap(t, snapshot)
	numbers := decoded["numbers"].(map[string]any)
	if len(numbers) == 0 {
		t.Fatal("exported snapshot carries no series counters")
	}
	for key := range numbers {
		numbers[key] = float64(0)
	}
	if err := smbBillImportMap(t, NewInvoiceStore(), decoded); err == nil {
		t.Fatal("regressed-counter snapshot import succeeded, want refusal")
	}

	honest := NewInvoiceStore()
	if err := honest.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	third := smbBillIssue(t, honest, "tenant-a", "inv-n3")
	if third.InvoiceNumber != "ACME-2026-000003" {
		t.Fatalf("post-restore issue number = %q, want 000003 without reuse", third.InvoiceNumber)
	}
}

func TestTodo_SMB_BILL_001_LifecycleTamperRefused(t *testing.T) {
	store := NewInvoiceStore()
	issued := smbBillIssue(t, store, "tenant-a", "inv-lt-1")
	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	flipped := smbBillSnapshotMap(t, snapshot)
	for _, raw := range flipped["invoices"].([]any) {
		inv := raw.(map[string]any)
		if inv["ID"] == issued.ID {
			inv["Status"] = string(InvoiceVoided)
		}
	}
	if err := smbBillImportMap(t, NewInvoiceStore(), flipped); err == nil {
		t.Fatal("status-flipped snapshot import succeeded, want refusal")
	}
	reset := smbBillSnapshotMap(t, snapshot)
	for _, raw := range reset["invoices"].([]any) {
		inv := raw.(map[string]any)
		if inv["ID"] == issued.ID {
			inv["Revision"] = float64(1)
		}
	}
	if err := smbBillImportMap(t, NewInvoiceStore(), reset); err == nil {
		t.Fatal("revision-reset snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_BILL_001_ReceiptBindingRefused(t *testing.T) {
	store := NewInvoiceStore()
	smbBillIssue(t, store, "tenant-a", "inv-rb-1")
	snapshot, err := store.Export()
	if err != nil {
		t.Fatal(err)
	}
	decoded := smbBillSnapshotMap(t, snapshot)
	receipts := decoded["receipts"].([]any)
	if len(receipts) == 0 {
		t.Fatal("exported snapshot carries no receipts")
	}
	receipts[0].(map[string]any)["key"] = "tenant-a\x00some-other-key"
	if err := smbBillImportMap(t, NewInvoiceStore(), decoded); err == nil {
		t.Fatal("misfiled-receipt snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_BILL_001_LineAndDateGuards(t *testing.T) {
	store := NewInvoiceStore()
	mismatched := smbBillDraftReq("inv-g1", t)
	mismatched.Lines[0].Quantity = dec(t, "2.00", 2)
	mismatched.Lines[0].UnitPrice = money(t, "100.00")
	mismatched.Lines[0].Amount = money(t, "0.01")
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-g1", mismatched); !errors.Is(err, ErrRejected) {
		t.Fatalf("quantity x price mismatch err=%v, want rejection", err)
	}
	negative := smbBillDraftReq("inv-g2", t)
	negative.Lines[0].Quantity = values.Decimal{}
	negative.Lines[0].UnitPrice = values.Money{}
	negated, err := money(t, "5.00").Neg()
	if err != nil {
		t.Fatal(err)
	}
	negative.Lines[0].Amount = negated
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-g2", negative); !errors.Is(err, ErrRejected) {
		t.Fatalf("negative line amount err=%v, want rejection", err)
	}
	badDue := smbBillDraftReq("inv-g3", t)
	badDue.DueDate = "not-a-date"
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-g3", badDue); !errors.Is(err, ErrRejected) {
		t.Fatalf("free-text due date err=%v, want rejection", err)
	}
	backwards := smbBillDraftReq("inv-g4", t)
	backwards.IssueDate = "2026-10-31"
	backwards.DueDate = "2026-10-01"
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-g4", backwards); !errors.Is(err, ErrRejected) {
		t.Fatalf("due-before-issue err=%v, want rejection", err)
	}
	manual := smbBillDraftReq("inv-g5", t)
	manual.Lines[0].Quantity = values.Decimal{}
	manual.Lines[0].UnitPrice = values.Money{}
	if _, _, err := store.Draft("tenant-a", "clerk-1", "draft-g5", manual); err != nil {
		t.Fatalf("manual-evidence line without quantity/price refused: %v", err)
	}
}
