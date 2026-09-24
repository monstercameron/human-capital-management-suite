package configparamstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_WF_DATA_037_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Config Param Store','ACTIVE',$3)`, tenant, "config-param-"+tenant.String(), time.Now().UTC())
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	store := New(db.Conn, func(key string) uuid.UUID {
		if key == tenant.String() {
			return tenant
		}
		return uuid.Nil
	})
	typ := workflow.ValueType{Kind: workflow.KindString}
	consumer := config.Consumer{Kind: config.ConsumerWorkflow, ID: "workflow.payroll"}
	snapshot, err := config.NewSnapshotWithDefinitions("active", "v5", nil, []config.ParameterDefinition{{Key: "payroll.currency", Type: typ, Classification: "TENANT", Owner: "payroll", Required: true, AllowedConsumers: []config.Consumer{consumer}}})
	if err != nil {
		t.Fatal(err)
	}
	path := config.ParameterScopePath{{Kind: config.ScopeTenant, ID: tenant.String()}, {Kind: config.ScopeCompany, ID: "company-1"}}
	firstChange := config.ParameterValueChange{Key: "payroll.currency", Scope: path[0], Environment: config.EnvironmentSandbox, Value: "TEST", ValueType: typ, Author: "admin-1", Reason: "sandbox fixture", RecordedAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), Path: path[:1]}
	first, err := store.AppendParameterRevision(context.Background(), tenant.String(), snapshot, firstChange)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.DefinitionVersion != "v5" {
		t.Fatalf("first revision = %+v", first)
	}
	company := firstChange
	company.Scope, company.Path = path[1], path
	company.Value, company.Author, company.Reason = "CAD", "admin-2", "company override"
	company.RecordedAt = firstChange.RecordedAt.Add(time.Minute)
	if _, err := store.AppendParameterRevision(context.Background(), tenant.String(), snapshot, company); err != nil {
		t.Fatal(err)
	}
	got, err := store.ResolveParameterValue(context.Background(), tenant.String(), snapshot, "payroll.currency", path, config.EnvironmentSandbox, consumer, typ)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "CAD" || got.Source != path[1] || got.Author != "admin-2" || len(got.Overridden) != 1 {
		t.Fatalf("resolved revision = %+v", got)
	}
	if _, err := store.ResolveParameterValue(context.Background(), tenant.String(), snapshot, "payroll.currency", path, config.EnvironmentProduction, consumer, typ); !errors.Is(err, config.ErrParameterValueMissing) {
		t.Fatalf("production fell back to sandbox: %v", err)
	}
	mutation := func(statement string) error {
		tx, err := db.Conn.Begin(context.Background())
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		_, err = tx.Exec(context.Background(), statement, tenant)
		return err
	}
	if err := mutation(`UPDATE tenant_parameter_value_revision SET value_text='tampered' WHERE tenant_id=$1`); err == nil {
		t.Fatal("UPDATE of append-only revision succeeded")
	}
	if err := mutation(`DELETE FROM tenant_parameter_value_revision WHERE tenant_id=$1`); err == nil {
		t.Fatal("DELETE of append-only revision succeeded")
	}

	other := uuid.New()
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM tenant_parameter_value_revision WHERE tenant_id=$1`, tenant).Scan(&visible); err != nil || visible != 2 {
		t.Fatalf("own tenant visible=%d err=%v", visible, err)
	}
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM tenant_parameter_value_revision WHERE tenant_id=$1`, other).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("foreign tenant visible=%d err=%v", visible, err)
	}
}

func TestTodo_WF_DATA_037_ConflictAndLocks(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Config Param Locks','ACTIVE',$3)`, tenant, "config-param-lock-"+tenant.String(), time.Now().UTC())
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	store := New(db.Conn, func(key string) uuid.UUID {
		if key == tenant.String() {
			return tenant
		}
		return uuid.Nil
	})
	typ := workflow.ValueType{Kind: workflow.KindString}
	definition := config.ParameterDefinition{Key: "payroll.currency", Type: typ, Classification: "TENANT", Owner: "payroll", AllowedConsumers: []config.Consumer{{Kind: config.ConsumerWorkflow, ID: "workflow.payroll"}}}
	snapshot, err := config.NewSnapshotWithDefinitions("active", "v5", nil, []config.ParameterDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	root := config.ParameterScope{Kind: config.ScopeTenant, ID: tenant.String()}
	company := config.ParameterScope{Kind: config.ScopeCompany, ID: "company-1"}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	change := config.ParameterValueChange{Key: definition.Key, Scope: root, Environment: config.EnvironmentProduction, Value: "USD", ValueType: typ, Author: "root", Reason: "central lock", RecordedAt: at, Locked: true, Path: config.ParameterScopePath{root}}
	if _, err := store.AppendParameterRevision(context.Background(), tenant.String(), snapshot, change); err != nil {
		t.Fatal(err)
	}
	child := change
	child.Scope, child.Path = company, config.ParameterScopePath{root, company}
	child.Value, child.Locked, child.ExpectedRevision = "CAD", false, 0
	child.RecordedAt = at.Add(time.Minute)
	if _, err := store.AppendParameterRevision(context.Background(), tenant.String(), snapshot, child); !errors.Is(err, config.ErrParameterScopeLocked) {
		t.Fatalf("descendant write error=%v, want ErrParameterScopeLocked", err)
	}
	change.ExpectedRevision = 3
	change.RecordedAt = at.Add(2 * time.Minute)
	if _, err := store.AppendParameterRevision(context.Background(), tenant.String(), snapshot, change); !errors.Is(err, config.ErrParameterRevisionConflict) {
		t.Fatalf("stale revision write error=%v, want ErrParameterRevisionConflict", err)
	}
}
