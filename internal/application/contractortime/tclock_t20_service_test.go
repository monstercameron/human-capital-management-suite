package contractortime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/agencytime"
	ct "github.com/monstercameron/human-capital-management-suite/internal/domains/contractortime"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type t20Auth struct {
	err   error
	calls []Capability
}

func (a *t20Auth) Authorize(_ context.Context, _ *trust.Principal, tenant, worker string, cap Capability) error {
	if tenant == "" || worker == "" {
		return errors.New("scope was not supplied")
	}
	a.calls = append(a.calls, cap)
	return a.err
}

type t20Dispatcher struct {
	observation DeliveryObservation
	calls       int
}

func (d *t20Dispatcher) Dispatch(_ context.Context, _ string, _ string, _ DeliveryPayload, _ string) (DeliveryObservation, error) {
	d.calls++
	return d.observation, nil
}

type t20Bindings struct {
	dispatcher Dispatcher
	tenant     string
	kind       DeliveryKind
	ref        string
}

func (b *t20Bindings) Resolve(_ context.Context, tenant string, kind DeliveryKind, ref string) (Dispatcher, error) {
	b.tenant, b.kind, b.ref = tenant, kind, ref
	return b.dispatcher, nil
}

type t20Store struct {
	records  map[string]DeliveryRecord
	receipts []Receipt
}

func (s *t20Store) Create(_ context.Context, tenant string, r DeliveryRecord) (DeliveryRecord, error) {
	if s.records == nil {
		s.records = map[string]DeliveryRecord{}
	}
	if old, ok := s.records[r.ID]; ok {
		return old, nil
	}
	r.TenantID = tenant
	s.records[r.ID] = r
	return r, nil
}

func (s *t20Store) RecordReceipt(_ context.Context, tenant string, receipt Receipt) (DeliveryRecord, error) {
	r, ok := s.records[receipt.DestinationID]
	if !ok || r.TenantID != tenant {
		return DeliveryRecord{}, errors.New("destination not found")
	}
	s.receipts = append(s.receipts, receipt)
	if receipt.Status == StatusSent || r.Status == StatusDraft {
		r.Status = receipt.Status
	}
	if receipt.Status != StatusSent {
		r.Status, r.ReceiptRef, r.Reason = receipt.Status, receipt.ReceiptRef, receipt.Reason
	}
	r.Revision++
	s.records[r.ID] = r
	return r, nil
}

func t20Principal(t *testing.T, tenant string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: "host-manager", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-t20", IssuedAt: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 28, 13, 0, 0, 0, time.UTC), CredentialDigest: "cred-t20",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func t20HourlyEngagement(t *testing.T) ct.Engagement {
	t.Helper()
	money, err := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	rate, err := values.NewMoneyRate(money, "HOUR")
	if err != nil {
		t.Fatal(err)
	}
	return ct.Engagement{ID: "eng-1", WorkerRef: "worker-1", SOWRef: "sow-1", Pricing: ct.PricingHourly, Currency: "USD", AmountScale: 2, Rounding: values.RoundingHalfEven, RateCard: map[string]values.Rate{"STD": rate}, Tax: ct.TaxTreatment{Kind: ct.TaxNone}, SelfBilling: true}
}

func t20Service(d Dispatcher) (Service, *t20Auth, *t20Store, *t20Bindings) {
	auth, store := &t20Auth{}, &t20Store{}
	binding := &t20Bindings{dispatcher: d}
	return Service{Auth: auth, Store: store, Bindings: binding, Clock: func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }}, auth, store, binding
}

// TestTodo_WTIME_011 proves approved contractor time is priced from the SOW
// and reaches a connector as an invoice, never an employee payroll record.
func TestTodo_WTIME_011(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Status: StatusAccepted, ExternalRef: "ap-42"}}
	svc, _, store, binding := t20Service(dispatcher)
	got, err := svc.InvoiceContractor(context.Background(), t20Principal(t, "tenant-a"), ContractorInvoiceRequest{
		Build:     ct.BuildInvoiceRequest{Engagement: t20HourlyEngagement(t), Lines: []ct.ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-28", Minutes: 60, RateCode: "STD", RevisionDigest: "approval-1"}}},
		SourceRef: "period-1", SourceRevision: 1, BindingRef: "ap-connector", ApprovalRevision: "approval-1", IdempotencyKey: "invoice-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Invoice.Total.String() != "100.00 USD" || got.Record.Status != StatusAccepted || got.Observation.ExternalRef != "ap-42" {
		t.Fatalf("invoice result = %+v", got)
	}
	if got.Record.Kind != KindContractorInvoice || binding.kind != KindContractorInvoice || len(store.receipts) != 2 {
		t.Fatalf("delivery routing = record=%+v binding=%+v receipts=%d", got.Record, binding, len(store.receipts))
	}
}

func TestTodo_WTIME_011_Integration(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Status: StatusAccepted}}
	svc, _, _, _ := t20Service(dispatcher)
	principal := t20Principal(t, "tenant-a")
	req := ContractorInvoiceRequest{Build: ct.BuildInvoiceRequest{Engagement: t20HourlyEngagement(t), Lines: []ct.ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-28", Minutes: 120, RateCode: "STD", RevisionDigest: "approval-1"}}}, SourceRef: "period-1", SourceRevision: 1, BindingRef: "ap", ApprovalRevision: "approval-1", IdempotencyKey: "once"}
	if _, err := svc.InvoiceContractor(context.Background(), principal, req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InvoiceContractor(context.Background(), principal, req); err != nil {
		t.Fatal(err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("connector calls on replay = %d, want 1", dispatcher.calls)
	}
}

func TestTodo_WTIME_011_Security(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Status: StatusAccepted}}
	svc, auth, store, _ := t20Service(dispatcher)
	auth.err = errors.New("denied")
	_, err := svc.InvoiceContractor(context.Background(), t20Principal(t, "tenant-a"), ContractorInvoiceRequest{
		Build:     ct.BuildInvoiceRequest{Engagement: t20HourlyEngagement(t), Lines: []ct.ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-28", Minutes: 60, RateCode: "STD", RevisionDigest: "approval-1"}}},
		SourceRef: "period-1", SourceRevision: 1, BindingRef: "ap", ApprovalRevision: "approval-1", IdempotencyKey: "denied",
	})
	if err == nil || dispatcher.calls != 0 || len(store.records) != 0 {
		t.Fatalf("unauthorized contractor flow err=%v calls=%d records=%d", err, dispatcher.calls, len(store.records))
	}

	auth.err = nil
	bad := t20HourlyEngagement(t)
	_, err = svc.InvoiceContractor(context.Background(), t20Principal(t, "tenant-a"), ContractorInvoiceRequest{
		Build:     ct.BuildInvoiceRequest{Engagement: bad, Lines: []ct.ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-28", Minutes: 60, RateCode: "NOT-IN-SOW", RevisionDigest: "approval-2"}}},
		SourceRef: "period-2", SourceRevision: 1, BindingRef: "ap", ApprovalRevision: "approval-2", IdempotencyKey: "bad-rate",
	})
	if !errors.Is(err, ct.ErrRejected) || dispatcher.calls != 0 {
		t.Fatalf("out-of-SOW rate err=%v calls=%d", err, dispatcher.calls)
	}
}

func TestTodo_WTIME_011_Golden(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Status: StatusAccepted}}
	svc, _, _, _ := t20Service(dispatcher)
	got, err := svc.InvoiceContractor(context.Background(), t20Principal(t, "tenant-a"), ContractorInvoiceRequest{
		Build:     ct.BuildInvoiceRequest{ID: "invoice-golden", Engagement: t20HourlyEngagement(t), Lines: []ct.ApprovedLine{{WorkerRef: "worker-1", Date: "2026-09-28", Minutes: 90, RateCode: "STD", RevisionDigest: "approval-1"}}},
		SourceRef: "period-golden", SourceRevision: 7, BindingRef: "ap", ApprovalRevision: "approval-1", IdempotencyKey: "golden",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Invoice.Total.String() != "150.00 USD" || got.Record.PayloadDigest != got.Invoice.Digest || len(got.Record.Payload) == 0 {
		t.Fatalf("golden invoice = %+v", got)
	}
}

func t20AgencyRequest(weeks int) AgencyDeliveryRequest {
	return AgencyDeliveryRequest{Build: agencytime.BuildExportRequest{Assignment: agencytime.Assignment{ID: "assignment-1", WorkerRef: "worker-2", AgencyRef: "agency-1", HostEntityRef: "host-1", WorkOrderRef: "wo-1", WorksiteRef: "site-1", Role: "warehouse"}, Lines: []agencytime.ApprovedLine{{WorkerRef: "worker-2", Date: "2026-09-28", Minutes: 480, RevisionDigest: "approval-2"}}}, SourceRef: "period-agency-1", SourceRevision: 3, BindingRef: "vms-fieldglass", ApprovalRevision: "approval-2", QualifyingWeeks: weeks, IdempotencyKey: "agency-1"}
}

func TestTodo_WTIME_012(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Status: StatusAccepted, ExternalRef: "vms-99"}}
	svc, auth, _, binding := t20Service(dispatcher)
	got, err := svc.ApproveAndDeliverAgency(context.Background(), t20Principal(t, "tenant-a"), t20AgencyRequest(12))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Parity.Applies || got.Record.Status != StatusAccepted || binding.tenant != "tenant-a" || len(auth.calls) != 2 {
		t.Fatalf("agency result=%+v auth=%v binding=%+v", got, auth.calls, binding)
	}
}

func TestTodo_WTIME_012_Integration(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Raw: []byte(`{"status":"REJECTED","reason":"worker not recognized"}`)}}
	svc, _, store, _ := t20Service(dispatcher)
	got, err := svc.ApproveAndDeliverAgency(context.Background(), t20Principal(t, "tenant-a"), t20AgencyRequest(11))
	if !errors.Is(err, ErrDeliveryRejected) || got.Observation.Reason != "worker not recognized" || got.Record.Status != StatusRejected {
		t.Fatalf("agency rejection result=%+v err=%v", got, err)
	}
	if len(store.receipts) != 2 || store.receipts[1].Reason == "" {
		t.Fatalf("rejection receipts=%+v", store.receipts)
	}
}

func TestTodo_WTIME_012_Security(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Status: StatusAccepted}}
	svc, auth, store, _ := t20Service(dispatcher)
	auth.err = errors.New("host manager denied")
	_, err := svc.ApproveAndDeliverAgency(context.Background(), t20Principal(t, "tenant-a"), t20AgencyRequest(12))
	if err == nil || dispatcher.calls != 0 || len(store.records) != 0 {
		t.Fatalf("unauthorized agency flow err=%v calls=%d records=%d", err, dispatcher.calls, len(store.records))
	}
}

func TestTodo_WTIME_012_Fault(t *testing.T) {
	dispatcher := &t20Dispatcher{observation: DeliveryObservation{Raw: []byte("not-json")}}
	svc, _, store, _ := t20Service(dispatcher)
	got, err := svc.ApproveAndDeliverAgency(context.Background(), t20Principal(t, "tenant-a"), t20AgencyRequest(12))
	if !errors.Is(err, ErrDeliveryMalformed) || got.Observation.RawDigest == "" || got.Record.Status != StatusMalformed {
		t.Fatalf("malformed agency response result=%+v err=%v", got, err)
	}
	if len(store.receipts) != 2 {
		t.Fatalf("malformed response was not recorded: %+v", store.receipts)
	}
}
