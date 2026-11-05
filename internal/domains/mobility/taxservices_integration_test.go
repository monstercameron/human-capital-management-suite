package mobility_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/mobility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// taxMessageNames pins the protobuf message each delegated contract serves.
// The vendor transport (WF-EXT-022) implements these messages; the contract
// only names them.
func taxMessageNames(service mobility.TaxService) (request, response string) {
	base := "hcmnext.mobility.tax.v1."
	switch service {
	case mobility.TaxShadowPayroll:
		return base + "ShadowPayrollRequest", base + "ShadowPayrollResult"
	case mobility.TaxHypothetical:
		return base + "HypotheticalTaxRequest", base + "HypotheticalTaxResult"
	case mobility.TaxEqualization:
		return base + "EqualizationSettlementRequest", base + "EqualizationSettlementResult"
	case mobility.TaxGrossUp:
		return base + "GrossUpRequest", base + "GrossUpResult"
	default:
		return "", ""
	}
}

func taxDefinition(t *testing.T, contract mobility.DelegatedContract) capability.Definition {
	t.Helper()
	request, response := taxMessageNames(contract.Service)
	if request == "" {
		t.Fatalf("service %s names no protobuf messages", contract.Service)
	}
	schema := func(id, full string) capability.SchemaRef {
		return capability.SchemaRef{SchemaID: id, Version: 1, ProtobufFullName: full}
	}
	return capability.Definition{
		ID: mobility.TaxContractID(contract.Service), Version: 1, OwnerDomain: "mobility",
		RequestSchema: schema(contract.RequestSchema, request), ResponseSchema: schema(contract.ResponseSchema, response),
		ErrorSchema:          schema("mobility.tax.error/v1", "hcmnext.mobility.tax.v1.TaxError"),
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: append([]string(nil), contract.DataDomains...)},
		RiskClass:            "DELEGATED_VENDOR_COMPUTE",
		IdempotencyPolicyRef: "mobility.tax.vendor_round_trip",
		AuthZScopeRef:        "scope:mobility.tax.read",
		LegalBasisRef:        "legal.mobility.tax.observe.v1",
		EntitlementRef:       "entitlement.mobility.tax.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "TestTodo_WF_CAP_018",
	}
}

type taxEvidenceSink struct{ n int }

func (s *taxEvidenceSink) RecordInvocation(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocationTx(ctx, evt)
}

func (s *taxEvidenceSink) RecordInvocationTx(_ context.Context, _ capability.InvocationEvidence) (string, error) {
	s.n++
	return "ev-tax", nil
}

// stubVendor is the WF-EXT-022 seam in miniature: deterministic receipts and
// one pinned annual statement.
type stubVendor struct {
	statement mobility.SettlementStatement
	submitted int
}

func (v *stubVendor) SubmitHypotheticalRun(_ context.Context, req mobility.HypotheticalRunRequest) (mobility.VendorRunReceipt, error) {
	v.submitted++
	return mobility.VendorRunReceipt{RequestKey: req.IdempotencyKey, RunRef: "vendor-run:" + req.IdempotencyKey}, nil
}

func (v *stubVendor) ObserveSettlement(_ context.Context, assignmentRef string, taxYear int) (mobility.SettlementStatement, error) {
	if assignmentRef != v.statement.AssignmentRef || taxYear != v.statement.TaxYear {
		return mobility.SettlementStatement{}, errors.New("unknown settlement")
	}
	return v.statement, nil
}

func mustIntegrationMoney(t *testing.T, text, currency string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, currency, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("money %s %s: %v", text, currency, err)
	}
	return m
}

// TestTodo_WF_CAP_018_Integration binds the four DELEGATED contracts to a
// real capability registry and drives the schedule-triggered run plus the
// annual settlement observation and reconciliation through the governed
// gateway. Vendor computation stays behind the VendorPort seam that
// WF-EXT-022's connector bindings will implement.
func TestTodo_WF_CAP_018_Integration(t *testing.T) {
	vendor := &stubVendor{statement: mobility.SettlementStatement{
		AssignmentRef: "assign-9", TaxYear: 2026, VendorRef: "vendor:tax-provider/acme-tax", StatementRef: "stmt-2026-9",
		Lines: []mobility.SettlementLine{
			{Jurisdiction: "DE-BE", Kind: mobility.SettlementActual, Amount: mustIntegrationMoney(t, "12000.00", "EUR")},
		},
	}}
	registry := capability.NewRegistry()
	contracts := mobility.DelegatedContracts(vendor.statement.VendorRef)
	for _, contract := range contracts {
		contract := contract
		def := taxDefinition(t, contract)
		var handler capability.Handler
		switch contract.Service {
		case mobility.TaxEqualization:
			handler = func(_ context.Context, payload any) (any, error) {
				req, ok := payload.(mobility.SettlementStatement)
				if !ok {
					return nil, errors.New("equalization handler wants the observed statement frame")
				}
				return vendor.ObserveSettlement(context.Background(), req.AssignmentRef, req.TaxYear)
			}
		default:
			handler = func(_ context.Context, payload any) (any, error) {
				req, ok := payload.(mobility.HypotheticalRunRequest)
				if !ok {
					return nil, errors.New("tax handler wants a hypothetical run request")
				}
				return vendor.SubmitHypotheticalRun(context.Background(), req)
			}
		}
		if err := registry.Register(def, handler); err != nil {
			t.Fatalf("register %s: %v", def.ID, err)
		}
	}
	if got := len(registry.List()); got != 4 {
		t.Fatalf("registry holds %d tax capabilities, want 4", got)
	}

	anchor, err := values.ParseLocalDate("2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	periodStart, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	periodEnd, err := values.ParseLocalDate("2026-01-31")
	if err != nil {
		t.Fatal(err)
	}
	schedule := mobility.HypotheticalRunSchedule{AssignmentRef: "assign-9", Cadence: mobility.TaxMonthly, Anchor: anchor, PeriodStart: periodStart, PeriodEnd: periodEnd, VendorRef: vendor.statement.VendorRef}
	due, err := values.ParseLocalDate("2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	run, err := schedule.RunRequest(due)
	if err != nil {
		t.Fatalf("RunRequest: %v", err)
	}
	again, err := schedule.RunRequest(due)
	if err != nil || again.IdempotencyKey != run.IdempotencyKey {
		t.Fatalf("retried trigger changed the idempotency key: %+v %+v %v", run, again, err)
	}

	sink := &taxEvidenceSink{}
	gateway := capability.NewGateway(registry, sink)
	auth := capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:mobility.tax.read"}, Reason: "test", SubjectRef: "test:mobility", Tenant: "acme"}
	res, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: capability.Key{ID: mobility.TaxContractID(mobility.TaxHypothetical), Version: 1},
		Payload:    run, Authorization: auth,
	})
	if err != nil {
		t.Fatalf("gateway invoke hypothetical run: %v", err)
	}
	receipt, ok := res.Response.(mobility.VendorRunReceipt)
	if !ok || receipt.RequestKey != run.IdempotencyKey || !strings.HasPrefix(receipt.RunRef, "vendor-run:") {
		t.Fatalf("receipt = %+v", res.Response)
	}

	observed, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    capability.Key{ID: mobility.TaxContractID(mobility.TaxEqualization), Version: 1},
		Payload:       mobility.SettlementStatement{AssignmentRef: "assign-9", TaxYear: 2026},
		Authorization: auth,
	})
	if err != nil {
		t.Fatalf("gateway invoke settlement observation: %v", err)
	}
	statement, ok := observed.Response.(mobility.SettlementStatement)
	if !ok {
		t.Fatalf("settlement response is %T", observed.Response)
	}
	rec, err := mobility.ReconcileSettlement([]mobility.PostedShadowAmount{
		{AssignmentRef: "assign-9", TaxYear: 2026, Jurisdiction: "DE-BE", Amount: mustIntegrationMoney(t, "12000.00", "EUR"), PostingRef: "pay-9"},
	}, statement)
	if err != nil {
		t.Fatalf("ReconcileSettlement: %v", err)
	}
	if !rec.Balanced {
		t.Fatalf("reconciliation = %+v, want balanced", rec)
	}
	if sink.n != 2 {
		t.Fatalf("evidence records = %d, want 2 (run submit + settlement observe)", sink.n)
	}
}
