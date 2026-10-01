package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type homeOrganizationDBFake struct{ tx *homeOrganizationTxFake }

func (d homeOrganizationDBFake) Begin(context.Context) (dbport.Tx, error) { return d.tx, nil }

type homeOrganizationTxFake struct {
	rows       dbport.Rows
	queries    []string
	args       [][]any
	execCounts []int64
	row        dbport.Row
}

func (t *homeOrganizationTxFake) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	t.queries = append(t.queries, sql)
	t.args = append(t.args, args)
	if len(t.execCounts) == 0 {
		return 1, nil
	}
	count := t.execCounts[0]
	t.execCounts = t.execCounts[1:]
	return count, nil
}
func (t *homeOrganizationTxFake) Query(_ context.Context, sql string, args ...any) (dbport.Rows, error) {
	t.queries = append(t.queries, sql)
	t.args = append(t.args, args)
	if t.rows == nil {
		return nil, errors.New("unexpected query")
	}
	rows := t.rows
	t.rows = nil
	return rows, nil
}

func (t *homeOrganizationTxFake) QueryRow(context.Context, string, ...any) dbport.Row {
	if t.row != nil {
		return t.row
	}
	return homeOrganizationRowFake{}
}
func (t *homeOrganizationTxFake) Commit(context.Context) error   { return nil }
func (t *homeOrganizationTxFake) Rollback(context.Context) error { return nil }

type homeOrganizationRowsFake struct {
	rows  [][]any
	index int
}

func (r *homeOrganizationRowsFake) Next() bool { return r.index < len(r.rows) }
func (r *homeOrganizationRowsFake) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i := range dest {
		if p, ok := dest[i].(*string); ok {
			*p = row[i].(string)
		}
	}
	r.index++
	return nil
}
func (r *homeOrganizationRowsFake) Err() error { return nil }
func (r *homeOrganizationRowsFake) Close()     {}

type homeOrganizationRowFake struct{}

func (homeOrganizationRowFake) Scan(...any) error { return dbport.ErrNoRows }

type homeOrganizationFactRowFake struct {
	id                   uuid.UUID
	organization, source string
	revision             int64
	effective            time.Time
}

func (r homeOrganizationFactRowFake) Scan(dest ...any) error {
	*(dest[0].(*uuid.UUID)) = r.id
	*(dest[1].(*string)) = r.organization
	*(dest[2].(*string)) = r.source
	*(dest[3].(*int64)) = r.revision
	*(dest[4].(*time.Time)) = r.effective
	return nil
}

func homeOrganizationReader(rows ...[]any) *AgentHomeOrganizationDirectoryDB {
	return NewAgentHomeOrganizationDirectoryDB(homeOrganizationDBFake{tx: &homeOrganizationTxFake{rows: &homeOrganizationRowsFake{rows: rows}}}, func(values.TenantId) uuid.UUID {
		return uuid.MustParse("00000000-0000-4000-8000-000000000001")
	})
}

func TestTodo_AGENT2_029(t *testing.T) {
	d := homeOrganizationReader([]any{"org:harborcare:care-operations"})
	got, err := d.CurrentHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1")
	if err != nil || got != "org:harborcare:care-operations" {
		t.Fatalf("CurrentHomeOrganization = %q, %v", got, err)
	}
}

func TestTodo_AGENT2_029_UnavailableWhenAbsentOrAmbiguous(t *testing.T) {
	for name, rows := range map[string][][]any{"absent": nil, "ambiguous": {{"org-a"}, {"org-b"}}} {
		t.Run(name, func(t *testing.T) {
			d := homeOrganizationReader(rows...)
			_, err := d.CurrentHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1")
			if !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
				t.Fatalf("error = %v, want ErrAgentHomeOrganizationUnavailable", err)
			}
		})
	}
}

func TestTodo_AGENT2_029_RejectsIncompleteAndNeverUsesRoleVisibility(t *testing.T) {
	d := homeOrganizationReader()
	if _, err := d.CurrentHomeOrganization(context.Background(), values.TenantId("tenant-a"), " "); !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("blank subject error = %v", err)
	}
	if err := d.ProvisionHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "", "source", 1, time.Unix(1, 0)); !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("blank organization error = %v", err)
	}
	if err := d.RevokeHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "admin", "reason", time.Time{}); !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("zero revocation time error = %v", err)
	}
	if err := d.ProvisionHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "org-a", "source", 1, time.Now().UTC().Add(time.Hour)); !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("future effective time error = %v", err)
	}
	if err := d.RevokeHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "admin", "reason", time.Now().UTC().Add(time.Hour)); !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("future revocation time error = %v", err)
	}
}

func TestTodo_AGENT2_029_ProvisionAndRevokeAreExplicitRevisionedOperations(t *testing.T) {
	tx := &homeOrganizationTxFake{rows: &homeOrganizationRowsFake{}}
	d := NewAgentHomeOrganizationDirectoryDB(homeOrganizationDBFake{tx: tx}, func(values.TenantId) uuid.UUID {
		return uuid.MustParse("00000000-0000-4000-8000-000000000001")
	})
	at := time.Unix(100, 0).UTC()
	if err := d.ProvisionHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "org-a", "workforce-plan:v1", 1, at); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := d.RevokeHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "admin", "employment ended", at.Add(time.Hour)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(tx.queries) != 5 {
		t.Fatalf("queries = %d, want tenant scope plus revision lock, provision insert and revoke update", len(tx.queries))
	}
}

func TestTodo_AGENT2_029_ProvisionRefusesSourceOwnershipChange(t *testing.T) {
	tenantID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	tx := &homeOrganizationTxFake{
		rows: &homeOrganizationRowsFake{},
		row:  homeOrganizationFactRowFake{id: uuid.New(), organization: "org-a", source: "source-a", revision: 1, effective: time.Unix(100, 0).UTC()},
	}
	d := NewAgentHomeOrganizationDirectoryDB(homeOrganizationDBFake{tx: tx}, func(values.TenantId) uuid.UUID { return tenantID })
	err := d.ProvisionHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1", "org-b", "source-b", 2, time.Unix(200, 0).UTC())
	if !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("source ownership change error = %v, want unavailable", err)
	}
	if len(tx.queries) != 2 {
		t.Fatalf("queries = %d, want tenant scope plus advisory lock", len(tx.queries))
	}
	for _, args := range tx.args {
		if len(args) == 0 {
			continue
		}
		if got, ok := args[0].(uuid.UUID); ok && got != tenantID {
			t.Fatalf("query tenant = %v, want %v", got, tenantID)
		}
	}
}

func TestTodo_AGENT2_029_Race_CurrentReadsRemainTenantScoped(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := homeOrganizationReader([]any{"org-a"})
			got, err := d.CurrentHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1")
			if err != nil || got != "org-a" {
				errs <- errors.New("concurrent home-organization read was not deterministic")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTodo_AGENT2_029_Security_InvalidTenantFailsClosed(t *testing.T) {
	d := homeOrganizationReader([]any{"org-a"})
	_, err := d.CurrentHomeOrganization(context.Background(), values.TenantId(" "), "worker-1")
	if !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("invalid tenant error = %v, want ErrAgentHomeOrganizationUnavailable", err)
	}
	d = NewAgentHomeOrganizationDirectoryDB(homeOrganizationDBFake{tx: &homeOrganizationTxFake{rows: &homeOrganizationRowsFake{rows: [][]any{{"org-a"}}}}}, func(values.TenantId) uuid.UUID { return uuid.Nil })
	if _, err := d.CurrentHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1"); !errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
		t.Fatalf("nil tenant UUID error = %v, want ErrAgentHomeOrganizationUnavailable", err)
	}
}

func TestTodo_AGENT2_029_Integration_QueryCarriesTenantAndSubject(t *testing.T) {
	tenantID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	tx := &homeOrganizationTxFake{rows: &homeOrganizationRowsFake{rows: [][]any{{"org-a"}}}}
	d := NewAgentHomeOrganizationDirectoryDB(homeOrganizationDBFake{tx: tx}, func(values.TenantId) uuid.UUID { return tenantID })
	if _, err := d.CurrentHomeOrganization(context.Background(), values.TenantId("tenant-a"), "worker-1"); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 2 {
		t.Fatalf("captured argument sets = %d, want tenant setup and scoped query", len(tx.args))
	}
	queryArgs := tx.args[1]
	if len(queryArgs) != 2 || queryArgs[0] != tenantID || queryArgs[1] != "worker-1" {
		t.Fatalf("scoped query args = %#v, want tenant UUID and subject", queryArgs)
	}
}

func TestTodo_AGENT2_029_Race_IntegrationConcurrentProvisionAndRead(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	tenantKey := values.TenantId("home-org-race")
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, string(tenantKey), string(tenantKey))
	when := time.Now().UTC().Add(-time.Minute)
	results := make(chan error, 2)
	for _, source := range []string{"workforce-plan:v1", "other-source:v1"} {
		go func(source string) {
			conn := appRoleConnForPopulation(t, db)
			d := NewAgentHomeOrganizationDirectoryDB(conn, func(values.TenantId) uuid.UUID { return tenantID })
			results <- d.ProvisionHomeOrganization(context.Background(), tenantKey, "worker-1", "org-a", source, 1, when)
		}(source)
	}
	succeeded, refused := 0, 0
	for range 2 {
		if err := <-results; err == nil {
			succeeded++
		} else if errors.Is(err, ErrAgentHomeOrganizationUnavailable) {
			refused++
		} else {
			t.Fatalf("concurrent provision: %v", err)
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("concurrent provision outcomes = succeeded %d, refused %d; want one of each", succeeded, refused)
	}
	reader := NewAgentHomeOrganizationDirectoryDB(appRoleConnForPopulation(t, db), func(values.TenantId) uuid.UUID { return tenantID })
	got, err := reader.CurrentHomeOrganization(context.Background(), tenantKey, "worker-1")
	if err != nil || got != "org-a" {
		t.Fatalf("current home organization = %q, %v; want org-a", got, err)
	}
}
