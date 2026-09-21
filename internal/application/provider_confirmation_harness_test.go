package application

// The promotion 1.1.0 graph parks the committed run on the payroll and then
// the identity provider's confirmation before each observation. On the served
// test path no provider exists, so this harness plays one: it opens the run's
// open wait, receives the provider's signal from the provider's own source
// (the accepted-source check still applies; a test verifier stands in for the
// provider's signature), records the receipt the observation will read
// through the composed fake ProviderReceiptReader (Options.ProviderReceipts),
// and resumes the served cell from the matched receipt.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// fakeProviderReceipts is the served test path's ProviderReceiptReader: the
// receipts the harness's provider recorded, by change ref.
type fakeProviderReceipts struct {
	mu       sync.Mutex
	receipts map[string]promotionsteps.ProviderReceipt
}

func newFakeProviderReceipts() *fakeProviderReceipts {
	return &fakeProviderReceipts{receipts: map[string]promotionsteps.ProviderReceipt{}}
}

func (f *fakeProviderReceipts) LatestForChange(_ context.Context, _ dbport.Tx, _ uuid.UUID, changeRef string) (promotionsteps.ProviderReceipt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	receipt, ok := f.receipts[changeRef]
	return receipt, ok, nil
}

func (f *fakeProviderReceipts) record(changeRef string, receipt promotionsteps.ProviderReceipt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.receipts[changeRef] = receipt
}

// providerSignalVerifier stands in for the provider's signature check. The
// served intake binds its own attested verifier; this harness proves the
// park/receive/resume mechanics and the accepted-source binding.
type providerSignalVerifier struct{}

func (providerSignalVerifier) Verify(stepSignal.Signal) error { return nil }

// providerWait names one 1.1.0 provider-confirmation wait and the provider
// that answers it.
type providerWait struct {
	node, source string
	changeRef    func(string) string
}

var (
	payrollProviderWait = providerWait{promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll", promotionsteps.PayrollChangeRef}
	accessProviderWait  = providerWait{promotionexec.NodeAwaitAccessConfirmation, "hcmnext.integrations.iam", promotionsteps.AccessChangeRef}
)

// confirmProviders plays both providers confirming exactly the committed
// change, payroll then identity, so the served run proceeds to
// reconciliation and parks on the acknowledgement gate.
func (h *promoux015Harness) confirmProviders() {
	h.t.Helper()
	placement := h.committedPlacement(h.afterEffectiveDate()())
	h.confirmProvider(payrollProviderWait, promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeApplied, Details: map[string]string{
		promotionsteps.ProviderDetailAmount: placement.BasePay, promotionsteps.ProviderDetailCurrency: placement.Currency,
		promotionsteps.ProviderDetailEffectiveDate: h.effective,
	}})
	h.confirmProvider(accessProviderWait, promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeGranted, Details: map[string]string{
		promotionsteps.ProviderDetailJobCode: placement.JobCode, promotionsteps.ProviderDetailGrade: placement.Grade,
	}})
}

// confirmProvider receives one provider's signal against the run's open wait
// on w.node, records receipt as that provider's answer for the change the
// wait correlates, and resumes the served cell from the matched receipt.
func (h *promoux015Harness) confirmProvider(w providerWait, receipt promotionsteps.ProviderReceipt) intentapp.ExecutionResult {
	h.t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(demoworkforce.CompanyKey)
	var instanceID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT instance_id FROM workflow_signal_subscription WHERE tenant_id = $1 AND node_id = $2 AND subscription_state = 'OPEN'`,
		tenantID, w.node).Scan(&instanceID); err != nil {
		h.t.Fatalf("find the open %s wait: %v", w.node, err)
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		h.t.Fatalf("scope tenant: %v", err)
	}
	sub, err := (signals.Store{}).OpenSubscriptionForNode(ctx, tx, tenantID, instanceID, w.node)
	if err != nil {
		h.t.Fatalf("open %s wait: %v", w.node, err)
	}
	// The provider answers now: the wait's close window runs on the driver's
	// wall clock, not the business effective date.
	at := time.Now().UTC()
	received, err := (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
		Signal: stepSignal.Signal{
			Tenant: values.TenantId(tenantID.String()), Source: w.source,
			EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
			CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
			IdempotencyKey: "test:provider:" + w.node + ":" + sub.CorrelationValue,
			Payload:        []byte(`{"change_ref":"` + w.changeRef(sub.CorrelationValue) + `","outcome":"` + receipt.Outcome + `"}`),
			ReceivedAt:     values.NewInstant(at),
		},
		ReceivedAt: at,
	}, providerSignalVerifier{})
	if err != nil {
		h.t.Fatalf("receive the %s confirmation: %v", w.node, err)
	}
	accepted := false
	for _, disposition := range received.Dispositions {
		accepted = accepted || (disposition.SubscriptionID == sub.ID && disposition.Status == stepSignal.StatusAccepted)
	}
	if !accepted {
		h.t.Fatalf("%s confirmation = %+v, want one ACCEPTED disposition for the open wait", w.node, received)
	}
	if err := tx.Commit(ctx); err != nil {
		h.t.Fatalf("commit the %s confirmation: %v", w.node, err)
	}
	// The wait correlates on the proposal revision; the receipt is recorded
	// against the outbox effect identity of the same change.
	h.receipts.record(w.changeRef(sub.CorrelationValue), receipt)
	result, err := h.composed.Cell().ResumeMatchedSignal(intentapp.WithResumeTenant(ctx, h.cfg.Tenant),
		instanceID.String(), w.node, sub.NodeAttempt, received.SignalID.String(), sub.ID.String())
	if err != nil {
		h.t.Fatalf("resume from the %s confirmation: %v", w.node, err)
	}
	return result
}

// TestProviderConfirmationHarnessRecordsTheProvidersAnswer pins the fake
// reader the served harness composes: it answers only what a provider
// recorded, by change ref.
func TestProviderConfirmationHarnessRecordsTheProvidersAnswer(t *testing.T) {
	reader := newFakeProviderReceipts()
	if _, found, err := reader.LatestForChange(context.Background(), nil, uuid.Nil, "payroll:rev"); found || err != nil {
		t.Fatalf("unanswered change = %t, %v; want not found", found, err)
	}
	reader.record(payrollProviderWait.changeRef("rev"), promotionsteps.ProviderReceipt{Outcome: promotionsteps.ProviderOutcomeApplied})
	got, found, err := reader.LatestForChange(context.Background(), nil, uuid.Nil, "payroll:rev")
	if err != nil || !found || got.Outcome != promotionsteps.ProviderOutcomeApplied {
		t.Fatalf("recorded change = %+v, %t, %v", got, found, err)
	}
	if accessProviderWait.changeRef("rev") != "iam:rev" || accessProviderWait.source != "hcmnext.integrations.iam" {
		t.Fatalf("access wait = %+v", accessProviderWait)
	}
}
