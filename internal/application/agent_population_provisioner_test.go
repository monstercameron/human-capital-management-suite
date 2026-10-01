package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type populationProvisionDB struct{ tx *populationProvisionTx }

func (d *populationProvisionDB) Begin(context.Context) (dbport.Tx, error) { return d.tx, nil }

type populationProvisionTx struct {
	queries   int
	inserts   int
	current   [][]any
	commits   int
	rollbacks int
	failAfter int
}

func (t *populationProvisionTx) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	// tenancy.WithTenant establishes the RLS setting and is not a population
	// insert.
	// The SQL text is passed separately below so this fake only counts writes.
	if !strings.Contains(sql, "set_config") && !strings.Contains(sql, "pg_advisory_xact_lock") {
		t.inserts++
		if t.failAfter > 0 && t.inserts > t.failAfter {
			return 0, errors.New("injected population write failure")
		}
	}
	return 1, nil
}
func (t *populationProvisionTx) Query(_ context.Context, sql string, _ ...any) (dbport.Rows, error) {
	if strings.Contains(sql, "set_config") {
		return &populationRows{}, nil
	}
	t.queries++
	rows := t.current
	t.current = nil
	return &populationRows{rows: rows}, nil
}
func (t *populationProvisionTx) QueryRow(context.Context, string, ...any) dbport.Row {
	return populationRow{}
}
func (t *populationProvisionTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *populationProvisionTx) Rollback(context.Context) error { t.rollbacks++; return nil }

type populationRows struct {
	rows  [][]any
	index int
}

func (r *populationRows) Next() bool { return r.index < len(r.rows) }
func (r *populationRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i, target := range dest {
		switch p := target.(type) {
		case *string:
			*p = row[i].(string)
		case *int64:
			*p = row[i].(int64)
		case *bool:
			*p = row[i].(bool)
		}
	}
	r.index++
	return nil
}
func (r *populationRows) Err() error { return nil }
func (r *populationRows) Close()     {}

type populationRow struct{}

func (populationRow) Scan(...any) error { return dbport.ErrNoRows }

func TestTodo_AGENT2_028(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }
	t.Run("rejects non-demo tenant without a default population", func(t *testing.T) {
		p := NewLocalDemoPopulationProvisioner(&populationProvisionDB{tx: &populationProvisionTx{}}, func(values.TenantId) uuid.UUID { return uuid.New() }, clock)
		if _, err := p.Provision(context.Background(), "production"); !errors.Is(err, ErrLocalDemoPopulationProvisioning) {
			t.Fatalf("Provision error = %v, want local-demo rejection", err)
		}
	})
	t.Run("inserts and replays exact employee facts", func(t *testing.T) {
		// The full workforce plan is deterministic; this fake returns no prior
		// rows, proving that every active employee is offered exactly once.
		tx := &populationProvisionTx{}
		p := NewLocalDemoPopulationProvisioner(&populationProvisionDB{tx: tx}, func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceDNS, []byte("tenant")) }, clock)
		got, err := p.Provision(context.Background(), "harborcare-demo")
		if err != nil {
			t.Fatal(err)
		}
		if got.Created == 0 || got.Existing != 0 || tx.inserts != got.Created || tx.commits != 1 {
			t.Fatalf("summary = %+v, inserts=%d commits=%d", got, tx.inserts, tx.commits)
		}
	})
	t.Run("preserves an existing exact authority", func(t *testing.T) {
		tx := &populationProvisionTx{current: [][]any{{localDemoPopulationID, localDemoSource, int64(1), false, false, true}}}
		p := NewLocalDemoPopulationProvisioner(&populationProvisionDB{tx: tx}, func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceDNS, []byte("tenant")) }, clock)
		got, err := p.Provision(context.Background(), "harborcare-demo")
		if err != nil {
			t.Fatal(err)
		}
		if got.Existing != 1 || got.Created != 59 || tx.inserts != 59 {
			t.Fatalf("summary = %+v, inserts=%d", got, tx.inserts)
		}
	})
}

func TestTodo_AGENT2_028_Security(t *testing.T) {
	tx := &populationProvisionTx{current: [][]any{{"contractors", "directory/admin", int64(4), false, false, true}}}
	p := NewLocalDemoPopulationProvisioner(&populationProvisionDB{tx: tx}, func(values.TenantId) uuid.UUID { return uuid.New() }, func() time.Time { return time.Now() })
	if _, err := p.Provision(context.Background(), "harborcare-demo"); !errors.Is(err, ErrLocalDemoPopulationProvisioning) {
		t.Fatalf("conflicting authority error = %v", err)
	}
	if tx.inserts != 0 {
		t.Fatalf("conflict inserted %d rows", tx.inserts)
	}
}

func TestTodo_AGENT2_028_Security_CrossTenant(t *testing.T) {
	db := pgtest.New(t)
	tenantA := pgstore.TenantID("harborcare-demo")
	tenantB := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'harborcare-demo','cell-local','HarborCare','ACTIVE',CURRENT_TIMESTAMP),($2,'foreign-demo','cell-local','Foreign','ACTIVE',CURRENT_TIMESTAMP)`, tenantA, tenantB)
	provisioner := NewLocalDemoPopulationProvisioner(db.Conn, tenantKeyMapper[values.TenantId](pgstore.TenantID), func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	if _, err := provisioner.Provision(context.Background(), "harborcare-demo"); err != nil {
		t.Fatal(err)
	}
	workers, err := demoworkforce.HarborCarePack.Plan(tenantA)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewAgentDirectoryDB(appRoleConnForPopulation(t, db), func(values.TenantId) uuid.UUID { return tenantB })
	if _, err := reader.CurrentPopulation(context.Background(), values.TenantId("foreign-demo"), workers[0].Row.WorkerKey); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("foreign tenant population = %v; want unavailable", err)
	}
}

func TestTodo_AGENT2_028_Recovery_WindowConflictsFailClosed(t *testing.T) {
	db := pgtest.New(t)
	tenantID := pgstore.TenantID("harborcare-demo")
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'harborcare-demo','cell-local','HarborCare','ACTIVE',CURRENT_TIMESTAMP)`, tenantID)
	workers, err := demoworkforce.HarborCarePack.Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,'contractors','directory',9,CURRENT_TIMESTAMP + interval '1 day')`, tenantID, uuid.New(), workers[0].Row.WorkerKey)
	exactID := uuid.NewSHA1(localDemoPopulationNamespace, []byte(tenantID.String()+"\x00"+workers[1].Row.WorkerKey))
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from,effective_until) VALUES ($1,$2,$3,$4,$5,1,CURRENT_TIMESTAMP - interval '2 days',CURRENT_TIMESTAMP - interval '1 day')`, tenantID, exactID, workers[1].Row.WorkerKey, localDemoPopulationID, localDemoSource)
	provisioner := NewLocalDemoPopulationProvisioner(db.Conn, tenantKeyMapper[values.TenantId](pgstore.TenantID), func() time.Time { return time.Now().UTC() })
	if _, err := provisioner.Provision(context.Background(), "harborcare-demo"); !errors.Is(err, ErrLocalDemoPopulationProvisioning) {
		t.Fatalf("future authority Provision error = %v; want fail closed", err)
	}
	db.Exec(t, `UPDATE agent_current_population SET revoked_at=CURRENT_TIMESTAMP,revoked_by='test',revocation_reason='window regression' WHERE tenant_id=$1 AND subject_ref=$2`, tenantID, workers[0].Row.WorkerKey)
	if _, err := provisioner.Provision(context.Background(), "harborcare-demo"); !errors.Is(err, ErrLocalDemoPopulationProvisioning) {
		t.Fatalf("expired exact authority Provision error = %v; want fail closed", err)
	}
}

func TestTodo_AGENT2_028_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := pgstore.TenantID("harborcare-demo")
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'harborcare-demo','cell-local','HarborCare','ACTIVE',CURRENT_TIMESTAMP)`, tenantID)
	provisioner := NewLocalDemoPopulationProvisioner(db.Conn, tenantKeyMapper[values.TenantId](pgstore.TenantID), func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	summary, err := provisioner.Provision(context.Background(), "harborcare-demo")
	if err != nil || summary.Created == 0 {
		t.Fatalf("Provision = %+v, %v; want seeded facts", summary, err)
	}
	workers, err := demoworkforce.HarborCarePack.Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO access_role (tenant_id,role_id,version,name,updated_by) VALUES ($1,'manager',1,'Manager','test')`, tenantID)
	for _, worker := range workers {
		db.Exec(t, `INSERT INTO worker_access_role_set (tenant_id,worker_ref,version,updated_by) VALUES ($1,$2,1,'test')`, tenantID, worker.Row.WorkerKey)
		db.Exec(t, `INSERT INTO worker_access_role_assignment (tenant_id,worker_ref,role_id) VALUES ($1,$2,'manager')`, tenantID, worker.Row.WorkerKey)
		db.Exec(t, `INSERT INTO agent_current_home_organization (tenant_id,fact_id,subject_ref,organization_scope_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,$4,'test',1,timestamptz '2026-09-29T12:00:00Z')`, tenantID, uuid.New(), worker.Row.WorkerKey, "org:harborcare:"+worker.Organization.Code)
	}
	reader := NewAgentDirectoryDB(appRoleConnForPopulation(t, db), tenantKeyMapper[values.TenantId](pgstore.TenantID))
	population, err := reader.CurrentPopulation(context.Background(), values.TenantId("harborcare-demo"), workers[0].Row.WorkerKey)
	if err != nil || population != localDemoPopulationID {
		t.Fatalf("CurrentPopulation = %q, %v; want %q", population, err, localDemoPopulationID)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("harborcare-demo"), Subject: "invoker", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	audience := NewPersonaAudienceDirectoryDB(reader)
	for _, worker := range workers {
		facts, audienceErr := audience.ResolvePersonaAudienceMember(trust.WithPrincipal(context.Background(), principal), "harborcare-demo", worker.Row.WorkerKey)
		if audienceErr != nil || len(facts.Populations) != 1 || facts.Populations[0] != localDemoPopulationID {
			t.Fatalf("audience facts for %s = %+v, %v", worker.Row.WorkerKey, facts, audienceErr)
		}
	}
}

func TestTodo_AGENT2_028_Recovery(t *testing.T) {
	failedTx := &populationProvisionTx{failAfter: 1}
	failed := NewLocalDemoPopulationProvisioner(&populationProvisionDB{tx: failedTx}, func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceDNS, []byte("recovery")) }, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	if _, err := failed.Provision(context.Background(), "harborcare-demo"); err == nil || failedTx.commits != 0 || failedTx.rollbacks == 0 {
		t.Fatalf("injected failure = %v, commits=%d rollbacks=%d; want rollback without commit", err, failedTx.commits, failedTx.rollbacks)
	}
	retryTx := &populationProvisionTx{}
	retry := NewLocalDemoPopulationProvisioner(&populationProvisionDB{tx: retryTx}, func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceDNS, []byte("recovery")) }, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	if summary, err := retry.Provision(context.Background(), "harborcare-demo"); err != nil || summary.Created == 0 {
		t.Fatalf("retry after rollback = %+v, %v; want successful seed", summary, err)
	}
	db := pgtest.New(t)
	tenantID := pgstore.TenantID("harborcare-demo")
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'harborcare-demo','cell-local','HarborCare','ACTIVE',CURRENT_TIMESTAMP)`, tenantID)
	newProvisioner := func() *LocalDemoPopulationProvisioner {
		return NewLocalDemoPopulationProvisioner(db.Conn, tenantKeyMapper[values.TenantId](pgstore.TenantID), func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	}
	first, err := newProvisioner().Provision(context.Background(), "harborcare-demo")
	if err != nil || first.Created == 0 {
		t.Fatalf("first Provision = %+v, %v", first, err)
	}
	second, err := newProvisioner().Provision(context.Background(), "harborcare-demo")
	if err != nil || second.Existing != first.Created {
		t.Fatalf("replayed Provision = %+v, %v; want %d existing", second, err, first.Created)
	}
	workers, err := demoworkforce.HarborCarePack.Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,'contractors','directory',9,CURRENT_TIMESTAMP)`, tenantID, uuid.New(), workers[1].Row.WorkerKey)
	if _, err := newProvisioner().Provision(context.Background(), "harborcare-demo"); !errors.Is(err, ErrLocalDemoPopulationProvisioning) {
		t.Fatalf("conflicting restart Provision error = %v, want fail-closed conflict", err)
	}
}
