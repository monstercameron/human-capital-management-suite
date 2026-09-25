package workorderreport

import (
	"bytes"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/reportrender"
)

func allowedRequest() Request {
	return Request{
		Scope:         Scope{TenantID: "tenant-a", ProjectID: "project-1", WorkOrderID: "wo-1"},
		Definition:    Definition{ID: "field-daily", Version: 3, Digest: "published-v3-digest", Kind: DailyField},
		Authorization: Authorization{Principal: Principal{ID: "supervisor-1", Tenant: "tenant-a", Purpose: "daily-report"}, Allow: func(Principal, Scope, string) bool { return true }},
		Sources: []Source{
			{Name: "work-log", Watermark: Watermark{Source: "work-log", Value: "wl-9"}, Complete: true, Entries: []Entry{{TenantID: "tenant-a", ProjectID: "project-1", WorkOrderID: "wo-1", Source: "work-log", ID: "2", Fields: map[string]string{"hours": "3"}}}},
			{Name: "progress", Watermark: Watermark{Source: "progress", Value: "p-8"}, Complete: true, Entries: []Entry{{TenantID: "tenant-a", ProjectID: "project-1", WorkOrderID: "wo-1", Source: "progress", ID: "1", Fields: map[string]string{"accepted_quantity": "12", "unit": "m"}}}},
			{Name: "cost", Watermark: Watermark{Source: "cost", Value: "c-7"}, Complete: true},
			{Name: "request", Watermark: Watermark{Source: "request", Value: "r-6"}, Complete: true},
		},
	}
}

func TestGeneratePinsDefinitionScopeAuthorizationAndDeterministicExportInput(t *testing.T) {
	got, err := Generate(allowedRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusComplete || got.Definition.Version != 3 || got.Definition.Digest != "published-v3-digest" || got.PrincipalID != "supervisor-1" || got.Scope.WorkOrderID != "wo-1" {
		t.Fatalf("report not pinned and scoped: %+v", got)
	}
	first, err := reportrender.Execute(got.RenderInput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reportrender.Execute(got.RenderInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.EvidenceDigest != second.EvidenceDigest || first.WatermarkText != "work-order-daily-field=cost=c-7,progress=p-8,request=r-6,work-log=wl-9" {
		t.Fatalf("nondeterministic or unwatermarked: %+v", first)
	}
	a, err := first.Export("json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes, b.Bytes) || a.Digest != b.Digest {
		t.Fatal("export bytes are not deterministic")
	}
}

func TestGenerateMissingAndStaleInputsAreVisible(t *testing.T) {
	req := allowedRequest()
	req.Sources = req.Sources[:3]
	req.ExpectedWatermarks = []Watermark{{Source: "progress", Value: "older"}}
	got, err := Generate(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusStale || len(got.MissingSources) != 1 || got.MissingSources[0] != "request" || len(got.StaleSources) != 1 || got.StaleSources[0] != "progress" {
		t.Fatalf("missing or stale sources hidden: %+v", got)
	}
	req.ExpectedWatermarks = nil
	got, err = Generate(req)
	if err != nil || got.Status != StatusPartial {
		t.Fatalf("incomplete report status = %q, %v", got.Status, err)
	}
}

func TestGenerateRechecksCurrentAuthorizationAndRejectsCrossScopeRows(t *testing.T) {
	req := allowedRequest()
	req.Authorization.Allow = func(Principal, Scope, string) bool { return false }
	if _, err := Generate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked reader accepted: %v", err)
	}
	req = allowedRequest()
	req.Sources[0].Entries[0].TenantID = "tenant-b"
	if _, err := Generate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("cross-tenant source row accepted: %v", err)
	}
}

func TestGenerateRejectsUnboundedOrMalformedInputs(t *testing.T) {
	req := allowedRequest()
	req.Sources = append(req.Sources, req.Sources[0])
	if _, err := Generate(req); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate source accepted: %v", err)
	}
	req = allowedRequest()
	entries := make([]Entry, MaxRecords+1)
	for i := range entries {
		entries[i] = Entry{TenantID: "tenant-a", ProjectID: "project-1", WorkOrderID: "wo-1", Source: "work-log", ID: "x"}
	}
	req.Sources[0].Entries = entries
	if _, err := Generate(req); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized input accepted: %v", err)
	}
	req = allowedRequest()
	req.Authorization.Allow = nil
	if _, err := Generate(req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("nil policy did not fail closed: %v", err)
	}
}
