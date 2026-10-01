package timestore

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixture migrates a fresh schema and opens a store on it. Concern files
// share it; each test owns its tenant ids.
func fixture(t *testing.T) *Store {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestStoreRejectsInvalidConfigAndTenantlessTransactions(t *testing.T) {
	if _, err := New(context.Background(), Config{Schema: "hcmnext_time"}); err == nil {
		t.Fatal("empty DSN accepted")
	}
	if _, err := New(context.Background(), Config{DSN: "postgres://u:p@h/db", Schema: "1bad"}); err == nil {
		t.Fatal("invalid schema accepted")
	}
	if _, err := New(context.Background(), Config{DSN: "postgres://u:p@h/db", CoreDSN: "postgres://u:p@h/db", Schema: "hcmnext_time"}); !errors.Is(err, ErrCoreCredential) {
		t.Fatalf("shared core credential: %v", err)
	}
	if _, err := withPoolSize("postgres://h/db", 1, 2); err == nil {
		t.Fatal("min conns above max accepted")
	}
	if got, _ := withPoolSize("host=h", 4, 1); got != "host=h pool_max_conns=4 pool_min_conns=1" {
		t.Fatalf("keyword pool size = %q", got)
	}
	var nilStore *Store
	if err := nilStore.RunTenantTx(context.Background(), "t", func(dbport.Tx) error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store tx: %v", err)
	}
	nilStore.Close()
}

func TestStoreMigratesAndScopesTransactionsToTenant(t *testing.T) {
	s := fixture(t)
	if err := s.RunTenantTx(context.Background(), " ", func(dbport.Tx) error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank tenant: %v", err)
	}
	var got string
	err := s.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT current_setting('hcmnext.tenant_id', true)").Scan(&got)
	})
	if err != nil || got != "tenant-a" {
		t.Fatalf("tenant setting = %q, %v", got, err)
	}
}
