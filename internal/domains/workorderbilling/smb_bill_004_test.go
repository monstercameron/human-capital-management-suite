// TestTodo_SMB_BILL_004 covers SMB-BILL-004: versioned tax treatment and
// invoice rounding through existing policy boundaries. Invoice issuance
// needs explicit treatment evidence; missing treatment never silently
// becomes zero tax. Snapshots pin profile, mode, rate and rounding at
// resolve time; new policy revisions never rewrite issued snapshots and
// corrections form separate adjustment chains.
package workorderbilling

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func bill004Dec(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func bill004Money(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func bill004At() string { return "2026-10-23T09:00:00Z" }

func bill004Caps() TaxCaps { return TaxCaps{CanResolve: true, CanApprove: true, CanCorrect: true} }

func bill004ActorCaps() ActorCaps {
	return ActorCaps{CanSubmit: true, CanApprove: true, CanIssue: true, CanVoid: true}
}

func bill004Profile() TaxProfile {
	return TaxProfile{
		ID: "TAX-US-TEST", Version: "2026.10", Mode: TaxExclusive, RatePercent: "7.5",
		Rounding: values.RoundingHalfUp, RoundingOrder: TaxRoundInvoice, DiscountBeforeTax: true,
		Currency: "USD", Scale: 2, Jurisdictions: []string{"US-CA"}, EvidenceRef: "legal-pack/2026.10",
		Applicable: true,
	}
}

func bill004Resolver(profiles ...TaxProfile) *StaticTaxResolver {
	resolver := NewStaticTaxResolver()
	for _, profile := range profiles {
		resolver.Publish("tenant-a", profile.ID, profile)
	}
	return resolver
}

func bill004Line(t *testing.T, id, amount string) InvoiceLine {
	t.Helper()
	return InvoiceLine{
		ID: id, Description: "Widget " + id, Quantity: bill004Dec(t, "1", 0),
		UnitPrice: bill004Money(t, amount), Amount: bill004Money(t, amount),
		RateRef: "rate-1", PolicyRef: "pol-1",
	}
}

func bill004Draft(t *testing.T, store *InvoiceStore, tenant, id string, lines []InvoiceLine) CustomerInvoice {
	t.Helper()
	inv, _, err := store.Draft(tenant, "clerk-1", "draft-"+id, DraftInvoiceRequest{
		InvoiceID: id, IssuerEntity: "ACME-LLC", IssuerSeries: "S1", CustomerID: "cust-1",
		Authority: LocalInvoiceAuthority, Source: InvoiceSource{
			Kind:   InvoiceSourceManual,
			Manual: ManualSource{RateRef: "rate-1", PolicyRef: "pol-1", PolicyVersion: "v1", Evidence: "ev-1"},
		},
		Currency: "USD", Lines: lines, TaxPolicyRef: "TAX-US-TEST",
		IssueDate: "2026-10-23", DueDate: "2026-11-23",
	})
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func bill004Ready(t *testing.T, store *InvoiceStore, tenant string, inv CustomerInvoice) CustomerInvoice {
	t.Helper()
	caps := bill004ActorCaps()
	next, _, err := store.SubmitForApproval(tenant, "clerk-1", caps, inv.ID, inv.Revision, "sub-"+inv.ID, bill004At())
	if err != nil {
		t.Fatal(err)
	}
	next, _, err = store.Approve(tenant, "mgr-1", caps, next.ID, next.Revision, "app-"+next.ID, bill004At())
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestTodo_SMB_BILL_004(t *testing.T) {
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	store.SetTaxIssueGate(taxes)
	inv := bill004Draft(t, store, "tenant-a", "inv-004-1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	snapshot, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-1", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != TaxResolved || snapshot.TotalTax != "7.50" || snapshot.Payable != "107.50" || snapshot.Subtotal != "100.00" {
		t.Fatalf("exclusive snapshot = %+v, want RESOLVED 100.00+7.50=107.50", snapshot)
	}
	if snapshot.ProfileVersion != "2026.10" || snapshot.Mode != TaxExclusive || snapshot.Digest == "" {
		t.Fatalf("snapshot pins = %+v, want profile/mode/digest pinned", snapshot)
	}
	ready := bill004Ready(t, store, "tenant-a", inv)
	issued, _, err := store.Issue("tenant-a", "clerk-1", bill004ActorCaps(), ready.ID, ready.Revision+1, "issue-1", bill004At())
	if err == nil {
		_ = issued
		t.Fatal("issue at wrong revision succeeded, want conflict before any tax decision")
	}
	current, err := store.Get("tenant-a", inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err = store.Issue("tenant-a", "clerk-1", bill004ActorCaps(), current.ID, current.Revision, "issue-1", bill004At())
	if err != nil {
		t.Fatal(err)
	}
	if issued.InvoiceNumber != "S1-000001" || issued.Status != InvoiceIssued {
		t.Fatalf("issued = %+v, want S1-000001 ISSUED", issued)
	}

	unresolved := bill004Draft(t, store, "tenant-a", "inv-004-2", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	pending, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), unresolved, ResolveTaxInput{
		Key: "resolve-2", InvoiceID: unresolved.ID, InvoiceRevision: unresolved.Revision, PolicyRef: "TAX-MISSING", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != TaxUnresolved || pending.TotalTax != "0.00" || pending.Note == "" {
		t.Fatalf("missing-policy snapshot = %+v, want UNRESOLVED with next action", pending)
	}
	ready2 := bill004Ready(t, store, "tenant-a", unresolved)
	if _, _, err := store.Issue("tenant-a", "clerk-1", bill004ActorCaps(), ready2.ID, ready2.Revision, "issue-2", bill004At()); !errors.Is(err, ErrTaxUnresolved) {
		t.Fatalf("unresolved issue err=%v, want tax-unresolved refusal, never USD 100 due", err)
	}
	after, err := store.Get("tenant-a", unresolved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.InvoiceNumber != "" || after.Status != InvoiceReady {
		t.Fatalf("blocked issue mutated invoice = %+v, want READY with no number consumed", after)
	}
}

func TestTodo_SMB_BILL_004_Unit(t *testing.T) {
	newStore := func(profile TaxProfile) (*InvoiceStore, *TaxStore) {
		store := NewInvoiceStore()
		taxes := NewTaxStore(bill004Resolver(profile))
		store.SetTaxIssueGate(taxes)
		return store, taxes
	}
	resolve := func(t *testing.T, taxes *TaxStore, inv CustomerInvoice, key, policy, discount string) (TaxSnapshot, error) {
		t.Helper()
		snapshot, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
			Key: key, InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: policy, Discount: discount, At: bill004At(),
		})
		return snapshot, err
	}

	inclusive := bill004Profile()
	inclusive.Mode = TaxInclusive
	inclusive.RoundingOrder = TaxRoundLine
	store, taxes := newStore(inclusive)
	inv := bill004Draft(t, store, "tenant-a", "inv-u-inc", []InvoiceLine{bill004Line(t, "L1", "107.50")})
	decomposed, err := resolve(t, taxes, inv, "resolve-inc", "TAX-US-TEST", "")
	if err != nil {
		t.Fatal(err)
	}
	if decomposed.Status != TaxResolved || decomposed.Subtotal != "100.00" || decomposed.TotalTax != "7.50" || decomposed.Payable != "107.50" {
		t.Fatalf("inclusive snapshot = %+v, want 100.00+7.50=107.50 decomposed", decomposed)
	}

	badRate := bill004Profile()
	badRate.RatePercent = "7.5%"
	store, taxes = newStore(badRate)
	inv = bill004Draft(t, store, "tenant-a", "inv-u-rate", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if _, err := resolve(t, taxes, inv, "resolve-rate", "TAX-US-TEST", ""); err == nil {
		t.Fatal("percent-signed rate resolved, want malformed-profile refusal")
	}
	if n := len(taxes.SnapshotsOf("tenant-a", inv.ID)); n != 0 {
		t.Fatalf("refused resolve stored %d snapshots, want zero", n)
	}

	negative := bill004Profile()
	negative.RatePercent = "-2"
	store, taxes = newStore(negative)
	inv = bill004Draft(t, store, "tenant-a", "inv-u-neg", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if _, err := resolve(t, taxes, inv, "resolve-neg", "TAX-US-TEST", ""); err == nil {
		t.Fatal("negative rate resolved, want refusal")
	}

	exemptBad := bill004Profile()
	exemptBad.Exempt = true
	exemptBad.RatePercent = "7.5"
	store, taxes = newStore(exemptBad)
	inv = bill004Draft(t, store, "tenant-a", "inv-u-exb", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if _, err := resolve(t, taxes, inv, "resolve-exb", "TAX-US-TEST", ""); err == nil {
		t.Fatal("exempt-with-rate resolved, want contradiction refusal")
	}

	exempt := bill004Profile()
	exempt.Exempt = true
	exempt.RatePercent = "0"
	exempt.ExemptReason = "resale certificate RC-9"
	store, taxes = newStore(exempt)
	inv = bill004Draft(t, store, "tenant-a", "inv-u-ex", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	clear, err := resolve(t, taxes, inv, "resolve-ex", "TAX-US-TEST", "")
	if err != nil {
		t.Fatal(err)
	}
	if clear.Status != TaxExempt || clear.TotalTax != "0.00" || clear.Payable != "100.00" {
		t.Fatalf("exempt snapshot = %+v, want EXEMPT zeros with evidence", clear)
	}

	na := bill004Profile()
	na.Applicable = false
	na.RatePercent = "0"
	store, taxes = newStore(na)
	inv = bill004Draft(t, store, "tenant-a", "inv-u-na", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	nap, err := resolve(t, taxes, inv, "resolve-na", "TAX-US-TEST", "")
	if err != nil {
		t.Fatal(err)
	}
	if nap.Status != TaxNotApplicable {
		t.Fatalf("inapplicable snapshot = %+v, want NOT_APPLICABLE", nap)
	}

	store, taxes = newStore(bill004Profile())
	inv = bill004Draft(t, store, "tenant-a", "inv-u-disc", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if _, err := resolve(t, taxes, inv, "resolve-disc", "TAX-US-TEST", "150.00"); err == nil {
		t.Fatal("over-subtotal discount resolved, want refusal")
	}
	discounted, err := resolve(t, taxes, inv, "resolve-disc2", "TAX-US-TEST", "10.00")
	if err != nil {
		t.Fatal(err)
	}
	if discounted.TotalTax != "6.75" || discounted.Payable != "96.75" {
		t.Fatalf("discounted snapshot = %+v, want (100-10)*7.5%% = 6.75 / 96.75", discounted)
	}

	mixed, _, err := store.Draft("tenant-a", "clerk-1", "draft-mixed", DraftInvoiceRequest{
		InvoiceID: "inv-u-mixed", IssuerEntity: "ACME-LLC", IssuerSeries: "S1", CustomerID: "cust-1",
		Authority: LocalInvoiceAuthority, Source: InvoiceSource{
			Kind:   InvoiceSourceManual,
			Manual: ManualSource{RateRef: "rate-1", PolicyRef: "pol-1", PolicyVersion: "v1", Evidence: "ev-1"},
		},
		Currency: "USD", Lines: []InvoiceLine{bill004Line(t, "L1", "100.00")}, TaxPolicyRef: "TAX-US-TEST",
		IssueDate: "2026-10-23", DueDate: "2026-11-23",
	})
	if err != nil {
		t.Fatal(err)
	}
	eur, err := values.NewMoney("10.00", "EUR", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	mixed.Lines = append(mixed.Lines, InvoiceLine{ID: "L2", Description: "Euro", Quantity: bill004Dec(t, "1", 0), UnitPrice: eur, Amount: eur, RateRef: "rate-1", PolicyRef: "pol-1"})
	if _, err := resolve(t, taxes, mixed, "resolve-mixed", "TAX-US-TEST", ""); err == nil {
		t.Fatal("mixed-currency invoice resolved, want refusal")
	}
}

func TestTodo_SMB_BILL_004_Property(t *testing.T) {
	profile := bill004Profile()
	profile.RoundingOrder = TaxRoundLine
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(profile))
	rng := rand.New(rand.NewSource(20261023))
	add := func(a, b string) string {
		x, err := values.NewDecimal(a, 2, values.RoundingExactRequired)
		if err != nil {
			t.Fatal(err)
		}
		y, err := values.NewDecimal(b, 2, values.RoundingExactRequired)
		if err != nil {
			t.Fatal(err)
		}
		z, err := x.Add(y)
		if err != nil {
			t.Fatal(err)
		}
		return z.String()
	}
	for i := 0; i < 50; i++ {
		n := 1 + rng.Intn(4)
		lines := make([]InvoiceLine, 0, n)
		for j := 0; j < n; j++ {
			cents := 1 + rng.Intn(99999)
			lines = append(lines, bill004Line(t, fmt.Sprintf("L%d", j), fmt.Sprintf("%d.%02d", cents/100, cents%100)))
		}
		id := fmt.Sprintf("inv-p-%d", i)
		inv := bill004Draft(t, store, "tenant-a", id, lines)
		snapshot, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
			Key: "resolve-" + id, InvoiceID: id, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := add(snapshot.Subtotal, snapshot.TotalTax); got != snapshot.Payable {
			t.Fatalf("case %d: %s + %s = %s, want payable %s", i, snapshot.Subtotal, snapshot.TotalTax, got, snapshot.Payable)
		}
		var taxSum, totalSum = "0.00", "0.00"
		for _, component := range snapshot.Components {
			if got := add(component.Basis, component.Tax); got != component.Total {
				t.Fatalf("case %d line %s: %s + %s = %s, want %s", i, component.LineID, component.Basis, component.Tax, got, component.Total)
			}
			taxSum = add(taxSum, component.Tax)
			totalSum = add(totalSum, component.Total)
		}
		if taxSum != snapshot.TotalTax || totalSum != snapshot.Payable {
			t.Fatalf("case %d: component sums %s/%s, want %s/%s", i, taxSum, totalSum, snapshot.TotalTax, snapshot.Payable)
		}
		if snapshot.Digest == "" || snapshot.Digest != taxes.SnapshotsOf("tenant-a", id)[0].Digest {
			t.Fatalf("case %d: digest unstable across reads", i)
		}
	}
}
func TestTodo_SMB_BILL_004_Integration(t *testing.T) {
	store := NewInvoiceStore()
	v1 := bill004Profile()
	resolver := bill004Resolver(v1)
	taxes := NewTaxStore(resolver)
	store.SetTaxIssueGate(taxes)
	inv := bill004Draft(t, store, "tenant-a", "inv-004-i1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	first, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-i1", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ready := bill004Ready(t, store, "tenant-a", inv)
	if _, _, err := store.Issue("tenant-a", "clerk-1", bill004ActorCaps(), ready.ID, ready.Revision, "issue-i1", bill004At()); err != nil {
		t.Fatal(err)
	}
	v2 := bill004Profile()
	v2.Version = "2026.11"
	v2.RatePercent = "8.5"
	resolver.Publish("tenant-a", v2.ID, v2)
	issued, err := store.Get("tenant-a", inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), issued, ResolveTaxInput{
		Key: "resolve-i2", InvoiceID: issued.ID, InvoiceRevision: issued.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ProfileVersion != "2026.11" || second.TotalTax != "8.50" {
		t.Fatalf("re-resolve = %+v, want v2 8.50 snapshot", second)
	}
	kept := taxes.SnapshotsOf("tenant-a", inv.ID)
	if len(kept) != 2 || kept[0].Digest != first.Digest || kept[0].TotalTax != "7.50" || kept[0].ProfileVersion != "2026.10" {
		t.Fatalf("issued snapshot rewritten = %+v, want v1 7.50 byte-preserved", kept)
	}
	v3 := bill004Profile()
	v3.Version = "2026.12"
	v3.RatePercent = "9.5"
	resolver.Publish("tenant-a", v3.ID, v3)
	adjustment, _, err := taxes.Correct(context.Background(), "tenant-a", "clerk-1", bill004Caps(), issued, CorrectTaxInput{
		Key: "correct-i1", InvoiceID: issued.ID, ExpectedChainLength: 0,
		Reason: "rate restated to 8.5%", Actor: "clerk-1", At: bill004At(),
		Resolve: &ResolveTaxInput{Key: "correct-i1-resolve", InvoiceID: issued.ID, InvoiceRevision: issued.Revision, PolicyRef: "TAX-US-TEST", At: bill004At()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if adjustment.DeltaTax != "1.00" || adjustment.DeltaPayable != "1.00" || adjustment.OriginalDigest != second.Digest {
		t.Fatalf("adjustment = %+v, want +1.00 delta against the v2 snapshot", adjustment)
	}
	chain := taxes.AdjustmentsOf("tenant-a", inv.ID)
	if len(chain) != 1 || chain[0].ID != adjustment.ID {
		t.Fatalf("adjustment chain = %+v, want the one recorded correction", chain)
	}
	latest, ok := taxes.SnapshotOf("tenant-a", inv.ID)
	if !ok || latest.Digest != adjustment.Corrected.Digest {
		t.Fatalf("latest snapshot = %+v, want the corrected resolution", latest)
	}

	manualInv := bill004Draft(t, store, "tenant-a", "inv-004-i2", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	manual, _, err := taxes.ResolveManual(context.Background(), "tenant-a", "clerk-1", bill004Caps(), manualInv, ManualTaxInput{
		Key: "manual-i1", InvoiceID: manualInv.ID, InvoiceRevision: manualInv.Revision,
		RatePercent: "7.5", Mode: TaxExclusive, Rounding: values.RoundingHalfUp, RoundingOrder: TaxRoundInvoice,
		Currency: "USD", Scale: 2, Reason: "tribal exemption review", Evidence: "ev-manual-1",
		Approved: false, At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Status != TaxUnresolved || !strings.Contains(manual.Note, "approval") {
		t.Fatalf("unapproved manual = %+v, want UNRESOLVED routed to approval", manual)
	}

	exported, err := taxes.Export()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewTaxStore(bill004Resolver(v1, v2))
	if err := restarted.Import(exported); err != nil {
		t.Fatal(err)
	}
	store.SetTaxIssueGate(restarted)
	if err := restarted.ValidateForIssue("tenant-a", issued, bill004At()); err != nil {
		t.Fatalf("restarted gate rejects the issued invoice: %v", err)
	}
}

func TestTodo_SMB_BILL_004_Security(t *testing.T) {
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	store.SetTaxIssueGate(taxes)
	inv := bill004Draft(t, store, "tenant-a", "inv-004-s1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if _, _, err := taxes.Resolve(context.Background(), "tenant-b", "intruder", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-x", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	}); !errors.Is(err, ErrTaxDenied) {
		t.Fatalf("cross-tenant resolve err=%v, want unresolved without inheriting tenant-a treatment", err)
	}
	denied := TaxCaps{}
	if _, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", denied, inv, ResolveTaxInput{
		Key: "resolve-d", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	}); !errors.Is(err, ErrTaxDenied) {
		t.Fatalf("denied-role resolve err=%v, want denial with no effect", err)
	}
	if _, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-stale", InvoiceID: inv.ID, InvoiceRevision: inv.Revision + 5, PolicyRef: "TAX-US-TEST", At: bill004At(),
	}); !errors.Is(err, ErrTaxConflict) {
		t.Fatalf("stale-revision resolve err=%v, want conflict", err)
	}
	if n := len(taxes.SnapshotsOf("tenant-a", inv.ID)); n != 0 {
		t.Fatalf("refused resolves stored %d snapshots, want zero", n)
	}
	other := bill004Draft(t, store, "tenant-a", "inv-004-s2", []InvoiceLine{bill004Line(t, "L1", "50.00")})
	snapshot, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), other, ResolveTaxInput{
		Key: "resolve-s2", InvoiceID: other.ID, InvoiceRevision: other.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := taxes.ValidateForIssue("tenant-a", other, bill004At()); err != nil {
		t.Fatalf("gate rejects resolved invoice: %v", err)
	}
	_ = snapshot
	if err := taxes.ValidateForIssue("tenant-a", inv, bill004At()); !errors.Is(err, ErrTaxUnresolved) {
		t.Fatalf("gate without snapshot err=%v, want unresolved, never a foreign snapshot", err)
	}
}

func TestTodo_SMB_BILL_004_Race(t *testing.T) {
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	inv := bill004Draft(t, store, "tenant-a", "inv-004-r1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
				Key: "resolve-race", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
			})
			if err == nil && snapshot.Payable == "107.50" {
				wins.Add(1)
			} else {
				t.Errorf("contended resolve err=%v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 8 {
		t.Fatalf("same-key contended resolves won %d, want 8 idempotent wins", wins.Load())
	}
	if n := len(taxes.SnapshotsOf("tenant-a", inv.ID)); n != 1 {
		t.Fatalf("contended resolve stored %d snapshots, want exactly 1", n)
	}

	var correctWins atomic.Int64
	var correctConflicts atomic.Int64
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, _, err := taxes.Correct(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, CorrectTaxInput{
				Key: fmt.Sprintf("correct-race-%d", slot), InvoiceID: inv.ID, ExpectedChainLength: 0,
				Reason: "race restatement", Actor: "clerk-1", At: bill004At(),
				Resolve: &ResolveTaxInput{Key: fmt.Sprintf("correct-race-%d-resolve", slot), InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At()},
			})
			if err == nil {
				correctWins.Add(1)
			} else if errors.Is(err, ErrTaxConflict) {
				correctConflicts.Add(1)
			} else {
				t.Errorf("contended correct err=%v, want win or conflict", err)
			}
		}(i)
	}
	wg.Wait()
	if correctWins.Load() != 1 || correctConflicts.Load() != 3 {
		t.Fatalf("contended corrections wins=%d conflicts=%d, want exactly 1 win", correctWins.Load(), correctConflicts.Load())
	}
}

func TestTodo_SMB_BILL_004_Fault(t *testing.T) {
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	store.SetTaxIssueGate(taxes)
	inv := bill004Draft(t, store, "tenant-a", "inv-004-f1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if _, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-f1", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", Discount: "999.00", At: bill004At(),
	}); err == nil {
		t.Fatal("over-discount resolve succeeded, want refusal with no effect")
	}
	if n := len(taxes.SnapshotsOf("tenant-a", inv.ID)); n != 0 {
		t.Fatalf("refused resolve stored %d snapshots, want zero", n)
	}
	snapshot, receipt, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-f2", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := taxes.Export()
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewTaxStore(bill004Resolver(bill004Profile()))
	if err := restarted.Import(exported); err != nil {
		t.Fatal(err)
	}
	replayed, receipt2, err := restarted.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-f2", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Digest != snapshot.Digest || receipt2.Payload != receipt.Payload {
		t.Fatal("lost-response replay returned a different result, want the durable original")
	}
	if _, _, err := restarted.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-f2", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", Discount: "5.00", At: bill004At(),
	}); !errors.Is(err, ErrTaxConflict) {
		t.Fatalf("changed-payload replay err=%v, want conflict", err)
	}

	expired := bill004Profile()
	expired.NotAfter = "2026-01-01T00:00:00Z"
	stale := NewTaxStore(bill004Resolver(expired))
	aged, _, err := stale.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-aged", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if aged.Status != TaxStale {
		t.Fatalf("expired-profile snapshot = %+v, want STALE", aged)
	}
}

func TestTodo_SMB_BILL_004_Browser(t *testing.T) {
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	inv := bill004Draft(t, store, "tenant-a", "inv-004-b1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	pending := ProjectTaxSection(inv, nil)
	if pending.DisplayTotal != "Tax pending" || !pending.Pending {
		t.Fatalf("missing snapshot section = %+v, want pending display, never $0", pending)
	}
	snapshot, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-b1", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	section := ProjectTaxSection(inv, &snapshot)
	if section.DisplayTotal != "USD 107.50" || section.Status != "RESOLVED" || len(section.Components) != 1 {
		t.Fatalf("resolved section = %+v, want USD 107.50 with one component", section)
	}
	detail := ProjectTreatmentDetail(snapshot)
	if detail.ProfileID != "TAX-US-TEST" || detail.ProfileVersion != "2026.10" || detail.Manual {
		t.Fatalf("treatment detail = %+v, want pinned profile, non-manual", detail)
	}
	second, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-b2", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", Discount: "10.00", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	recalc := ProjectRecalculation(snapshot, second)
	if recalc.DeltaPayable != "-10.75" || !recalc.TotalsChanged || !recalc.RequiresReapproval {
		t.Fatalf("recalculation = %+v, want -10.75 with reapproval", recalc)
	}
}

func TestTodo_SMB_BILL_004_Conformance(t *testing.T) {
	legacy := NewInvoiceStore()
	inv := bill004Draft(t, legacy, "tenant-a", "inv-004-c1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	ready := bill004Ready(t, legacy, "tenant-a", inv)
	issued, _, err := legacy.Issue("tenant-a", "clerk-1", bill004ActorCaps(), ready.ID, ready.Revision, "issue-c1", bill004At())
	if err != nil {
		t.Fatalf("nil-gate issue refused: %v (BILL-001 behavior must be preserved)", err)
	}
	if issued.InvoiceNumber != "S1-000001" {
		t.Fatalf("nil-gate number = %q, want S1-000001", issued.InvoiceNumber)
	}
	gated := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	gated.SetTaxIssueGate(taxes)
	inv2 := bill004Draft(t, gated, "tenant-a", "inv-004-c2", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	ready2 := bill004Ready(t, gated, "tenant-a", inv2)
	if _, _, err := gated.Issue("tenant-a", "clerk-1", bill004ActorCaps(), ready2.ID, ready2.Revision, "issue-c2", bill004At()); !errors.Is(err, ErrTaxUnresolved) {
		t.Fatalf("gateless-resolution issue err=%v, want unresolved refusal", err)
	}
}

func TestTodo_SMB_BILL_004_Mutation(t *testing.T) {
	store := NewInvoiceStore()
	zeroRate := bill004Profile()
	zeroRate.RatePercent = "0"
	taxes := NewTaxStore(bill004Resolver(zeroRate))
	store.SetTaxIssueGate(taxes)
	inv := bill004Draft(t, store, "tenant-a", "inv-004-m1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	silent, _, err := taxes.Resolve(context.Background(), "tenant-a", "clerk-1", bill004Caps(), inv, ResolveTaxInput{
		Key: "resolve-m1", InvoiceID: inv.ID, InvoiceRevision: inv.Revision, PolicyRef: "TAX-US-TEST", At: bill004At(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if silent.Status != TaxUnresolved {
		t.Fatalf("zero-rate-without-evidence = %+v, want UNRESOLVED, never silent zero tax", silent)
	}
	if err := taxes.ValidateForIssue("tenant-a", inv, bill004At()); !errors.Is(err, ErrTaxUnresolved) {
		t.Fatalf("silent-zero gate err=%v, want unresolved refusal", err)
	}

	foreign := bill004Draft(t, store, "tenant-b", "inv-004-m1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	if err := taxes.ValidateForIssue("tenant-b", foreign, bill004At()); !errors.Is(err, ErrTaxUnresolved) {
		t.Fatalf("cross-tenant gate err=%v, want unresolved without inheriting tenant-a treatment", err)
	}

	tampered := silent
	tampered.TotalTax = "0.01"
	if err := taxes.ValidateForIssue("tenant-a", inv, bill004At()); !errors.Is(err, ErrTaxUnresolved) {
		t.Fatalf("post-tamper gate err=%v, want unresolved (stored snapshot is untouched)", err)
	}
	exported, err := taxes.Export()
	if err != nil {
		t.Fatal(err)
	}
	needle := `"total_tax":"0.00"`
	if !strings.Contains(string(exported), needle) {
		t.Fatalf("snapshot JSON lacks %s, cannot tamper", needle)
	}
	forged := strings.Replace(string(exported), needle, `"total_tax":"0.01"`, 1)
	if err := NewTaxStore(bill004Resolver(bill004Profile())).Import([]byte(forged)); err == nil {
		t.Fatal("digest-tampered snapshot import succeeded, want refusal")
	}
	_ = tampered
}
func TestTodo_SMB_BILL_004_Preview(t *testing.T) {
	store := NewInvoiceStore()
	taxes := NewTaxStore(bill004Resolver(bill004Profile()))
	inv := bill004Draft(t, store, "tenant-a", "inv-004-v1", []InvoiceLine{bill004Line(t, "L1", "100.00")})
	preview, err := taxes.Preview(context.Background(), "tenant-a", inv, "TAX-US-TEST", "", bill004At())
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != TaxResolved || preview.DisplayTotal != "USD 107.50" {
		t.Fatalf("preview = %+v, want RESOLVED USD 107.50", preview)
	}
	if n := len(taxes.SnapshotsOf("tenant-a", inv.ID)); n != 0 {
		t.Fatalf("preview stored %d snapshots, want zero writes", n)
	}
	missing, err := taxes.Preview(context.Background(), "tenant-a", inv, "TAX-MISSING", "", bill004At())
	if err != nil {
		t.Fatal(err)
	}
	if missing.Status != TaxUnresolved || missing.DisplayTotal != "Tax pending" {
		t.Fatalf("missing preview = %+v, want pending display, never a total", missing)
	}
}
