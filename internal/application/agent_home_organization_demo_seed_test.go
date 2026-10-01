package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type demoHomeOrganizationFake struct {
	facts       map[string]string
	provisions  int
	lastTenant  values.TenantId
	source      string
	revision    int64
	effectiveAt time.Time
}

func (f *demoHomeOrganizationFake) CurrentHomeOrganization(_ context.Context, tenant values.TenantId, subject string) (string, error) {
	f.lastTenant = tenant
	organization, ok := f.facts[subject]
	if !ok {
		return "", ErrAgentHomeOrganizationUnavailable
	}
	return organization, nil
}

func (f *demoHomeOrganizationFake) ProvisionHomeOrganization(_ context.Context, tenant values.TenantId, subject, organization, source string, revision int64, at time.Time) error {
	f.lastTenant = tenant
	if current, ok := f.facts[subject]; ok && current != organization {
		return ErrAgentHomeOrganizationUnavailable
	}
	if f.facts == nil {
		f.facts = make(map[string]string)
	}
	f.facts[subject] = organization
	f.provisions++
	f.source, f.revision, f.effectiveAt = source, revision, at
	return nil
}

func TestTodo_AGENT2_029_DemoSeedUsesExplicitPlanUnitsAndReplays(t *testing.T) {
	clock := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	fake := &demoHomeOrganizationFake{}
	seed := &LocalDemoHomeOrganizationProvisioner{Directory: fake, Now: func() time.Time { return clock }}
	first, err := seed.Provision(context.Background(), demoworkforce.HarborCarePack.Key)
	if err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	if first.Created == 0 || first.Existing != 0 || fake.provisions != first.Created {
		t.Fatalf("first summary = %+v, provisions=%d", first, fake.provisions)
	}
	if fake.lastTenant != values.TenantId(demoworkforce.HarborCarePack.Key) || fake.source != localDemoHomeOrganizationSource || fake.revision != localDemoHomeOrganizationRevision || !fake.effectiveAt.Equal(clock) {
		t.Fatalf("provenance = tenant %q source %q revision %d at %s", fake.lastTenant, fake.source, fake.revision, fake.effectiveAt)
	}
	second, err := seed.Provision(context.Background(), demoworkforce.HarborCarePack.Key)
	if err != nil {
		t.Fatalf("replayed Provision: %v", err)
	}
	if second.Existing != first.Created || second.Created != 0 {
		t.Fatalf("replay summary = %+v, want %d existing", second, first.Created)
	}
}

func TestTodo_AGENT2_029_DemoSeedRejectsConflictingHomeOrganization(t *testing.T) {
	pack := demoworkforce.HarborCarePack
	workers, err := pack.Plan(uuid.NewSHA1(uuid.NameSpaceDNS, []byte(pack.Key)))
	if err != nil {
		t.Fatal(err)
	}
	var subject string
	for _, worker := range workers {
		if worker.Row.LifecycleStatus == "active" && worker.Row.WorkerType == "employee" {
			subject = worker.Row.WorkerKey
			break
		}
	}
	fake := &demoHomeOrganizationFake{facts: map[string]string{subject: "org:harborcare:wrong-unit"}}
	seed := &LocalDemoHomeOrganizationProvisioner{Directory: fake, Now: func() time.Time { return time.Unix(1, 0).UTC() }}
	if _, err := seed.Provision(context.Background(), pack.Key); !errors.Is(err, ErrLocalDemoHomeOrganizationProvisioning) {
		t.Fatalf("conflicting Provision error = %v, want local-demo rejection", err)
	}
	if fake.provisions != 0 {
		t.Fatalf("conflict wrote %d facts", fake.provisions)
	}
}

func TestTodo_AGENT2_029_DemoSeedRejectsUnknownTenant(t *testing.T) {
	seed := &LocalDemoHomeOrganizationProvisioner{Directory: &demoHomeOrganizationFake{}}
	if _, err := seed.Provision(context.Background(), "production"); !errors.Is(err, ErrLocalDemoHomeOrganizationProvisioning) {
		t.Fatalf("unknown tenant error = %v, want local-demo rejection", err)
	}
}

func TestTodo_AGENT2_029_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := pgstore.TenantID("harborcare-demo")
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'harborcare-demo','cell-local','HarborCare','ACTIVE',CURRENT_TIMESTAMP)`, tenantID)
	clock := func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }
	tenantUUID := func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) }
	seed := NewLocalDemoHomeOrganizationProvisioner(db.Conn, tenantUUID, clock)
	first, err := seed.Provision(context.Background(), "harborcare-demo")
	if err != nil || first.Created == 0 {
		t.Fatalf("first Provision = %+v, %v; want facts", first, err)
	}
	second, err := seed.Provision(context.Background(), "harborcare-demo")
	if err != nil || second.Existing != first.Created || second.Created != 0 {
		t.Fatalf("replayed Provision = %+v, %v; want %d existing", second, err, first.Created)
	}
	workers, err := demoworkforce.HarborCarePack.Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	var subject, wantOrganization string
	for _, worker := range workers {
		if worker.Row.LifecycleStatus == "active" && worker.Row.WorkerType == "employee" {
			subject = worker.Row.WorkerKey
			wantOrganization = "org:" + demoworkforce.HarborCarePack.Key + ":" + worker.Organization.Code
			break
		}
	}
	reader := NewAgentHomeOrganizationDirectoryDB(appRoleConnForPopulation(t, db), tenantUUID)
	got, err := reader.CurrentHomeOrganization(context.Background(), values.TenantId("harborcare-demo"), subject)
	if err != nil || got != wantOrganization {
		t.Fatalf("CurrentHomeOrganization = %q, %v; want %q", got, err, wantOrganization)
	}
}
