package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

type publishProbe struct {
	tenant uuid.UUID
	called bool
}

func (p *publishProbe) Run(_ context.Context, tenant uuid.UUID) Snapshot {
	p.tenant, p.called = tenant, true
	return Snapshot{SchemaVersion: SnapshotSchemaVersion, Tenant: tenant, State: StateDegraded, Digest: "sha256:health"}
}

type publishTenant struct {
	tenant uuid.UUID
	allow  bool
}

func (r publishTenant) ResolveHealthTenant(*http.Request) (uuid.UUID, bool) { return r.tenant, r.allow }

func TestTodo_REV_032_02(t *testing.T) {
	tenant := uuid.New()
	probe := &publishProbe{}
	handler := EndpointHandler(probe, publishTenant{tenant: tenant, allow: true})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/ops/store-health", nil))
	if rec.Code != 200 || !probe.called || probe.tenant != tenant {
		t.Fatalf("health endpoint status=%d called=%v tenant=%s", rec.Code, probe.called, probe.tenant)
	}
	var got Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.State != StateDegraded || got.Tenant != tenant || got.Digest != "sha256:health" {
		t.Fatalf("published snapshot=%+v decode error=%v", got, err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", rec.Header().Get("Cache-Control"))
	}
}

func TestTodo_REV_032_02_Integration(t *testing.T) {
	tenant := uuid.New()
	probe := &publishProbe{}
	handler := EndpointHandler(probe, publishTenant{tenant: tenant, allow: true})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/ops/store-health", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode served health response: %v", err)
	}
	if body["tenant"] != tenant.String() || body["state"] != string(StateDegraded) {
		t.Fatalf("served response tenant/state=%v/%v", body["tenant"], body["state"])
	}

	deniedProbe := &publishProbe{}
	denied := EndpointHandler(deniedProbe, publishTenant{tenant: tenant, allow: false})
	deniedRec := httptest.NewRecorder()
	denied.ServeHTTP(deniedRec, httptest.NewRequest("GET", "/admin/ops/store-health", nil))
	if deniedRec.Code != 403 || deniedProbe.called {
		t.Fatalf("unauthorized response=%d probe-called=%v", deniedRec.Code, deniedProbe.called)
	}
}

func TestTodo_REV_032_02_Security(t *testing.T) {
	authorizedTenant := uuid.New()
	attackerTenant := uuid.New()
	probe := &publishProbe{}
	handler := EndpointHandler(probe, publishTenant{tenant: authorizedTenant, allow: true})
	rec := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/admin/ops/store-health?tenant_id="+attackerTenant.String(), nil)
	handler.ServeHTTP(rec, request)
	if rec.Code != 200 || probe.tenant != authorizedTenant {
		t.Fatalf("caller tenant override status=%d probed=%s, want authorized tenant %s", rec.Code, probe.tenant, authorizedTenant)
	}
}
