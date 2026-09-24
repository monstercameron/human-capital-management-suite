package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type rev014History struct{ exists bool }

func (h rev014History) FieldHistoryAt(_ context.Context, q dataops.HistoryQuery) (dataops.HistorySet, error) {
	watermark, _ := values.NewSequenceRevision("worker-history", 1)
	return dataops.HistorySet{Subject: q.Subject, Exists: h.exists, Watermark: watermark}, nil
}

func rev014Request(t *testing.T, authorization dataops.Authorization) dataops.ExplainFieldHistoryRequest {
	t.Helper()
	tenant := values.TenantId("acme")
	effective, err := values.ParseLocalDate("2026-06-15")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse(time.RFC3339, "2026-08-31T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	instant := values.NewInstant(parsed)
	known, err := values.NewKnownAt(instant)
	if err != nil {
		t.Fatal(err)
	}
	return dataops.ExplainFieldHistoryRequest{
		Tenant:        tenant,
		Subject:       values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-0000-0000-000000000001"},
		Fields:        []dataops.FieldID{"person.name", "compensation.base"},
		AsOfEffective: effective,
		AsKnownAt:     known,
		Authorization: authorization,
	}
}

func rev014Authorization() dataops.Authorization {
	return dataops.Authorization{
		PolicyVersion: "policy/1", Purpose: "dataops.debug", SubjectDisclosable: true,
		Fields: map[dataops.FieldID]dataops.Ruling{
			"person.name":       {Effect: dataops.EffectAllow},
			"compensation.base": {Effect: dataops.EffectDeny, Reason: "compensation_restricted"},
		},
	}
}

func TestTodo_REV_014_01(t *testing.T) {
	history := rev014History{exists: true}
	h := &domainHandlers{history: history}
	got, err := h.handlerFor(dataops.ExplainFieldHistoryOperation)(context.Background(), rev014Request(t, rev014Authorization()))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if _, ok := got.(dataops.HistoryExplanation); !ok {
		t.Fatalf("handler returned %T, want dataops.HistoryExplanation", got)
	}
	registry, err := newCapabilityRegistry(h)
	if err != nil {
		t.Fatalf("newCapabilityRegistry: %v", err)
	}
	if _, ok := registry.Lookup(capability.Key{ID: dataops.ExplainFieldHistoryOperation, Version: 1}); !ok {
		t.Fatal("effective-date debugger is absent from the app capability registry")
	}
}

func TestTodo_REV_014_01_Security(t *testing.T) {
	h := &domainHandlers{history: rev014History{exists: true}}
	got, err := h.explainFieldHistory(context.Background(), rev014Request(t, rev014Authorization()))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result := got.(dataops.HistoryExplanation)
	timeline, ok := result.Timeline("compensation.base")
	if !ok || timeline.Access != dataops.AccessDenied || timeline.DenialReason != "compensation_restricted" || len(timeline.Versions) != 0 {
		t.Fatalf("restricted field redaction changed: %+v", timeline)
	}
}

func TestTodo_REV_014_01_Golden(t *testing.T) {
	ctx := context.Background()
	history := rev014History{exists: true}
	req := rev014Request(t, rev014Authorization())
	want, err := dataops.ExplainFieldHistory(ctx, history, req)
	if err != nil {
		t.Fatalf("direct library call: %v", err)
	}
	gotAny, err := (&domainHandlers{history: history}).explainFieldHistory(ctx, req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := gotAny.(dataops.HistoryExplanation)
	if !bytes.Equal(got.Canonical(), want.Canonical()) {
		t.Fatalf("handler canonical explanation differs from direct library result\n got: %s\nwant: %s", got.Canonical(), want.Canonical())
	}
}
