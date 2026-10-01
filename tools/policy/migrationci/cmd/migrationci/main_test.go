package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/migrationci"
)

type fakeMigrationConnector struct{ state *fakeMigrationConn }

func (c fakeMigrationConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c fakeMigrationConnector) Driver() driver.Driver                        { return fakeMigrationDriver{} }

type fakeMigrationDriver struct{}

func (fakeMigrationDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type fakeMigrationConn struct {
	lockAcquired bool
	lockErr      error
	releaseErr   error
	journalErr   error
	pingErr      error
	rows         [][]driver.Value
}

func (c *fakeMigrationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (c *fakeMigrationConn) Close() error { return nil }
func (c *fakeMigrationConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}
func (c *fakeMigrationConn) Ping(context.Context) error { return c.pingErr }

func (c *fakeMigrationConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "pg_try_advisory_lock") {
		if c.lockErr != nil {
			return nil, c.lockErr
		}
		return &fakeMigrationRows{columns: []string{"pg_try_advisory_lock"}, rows: [][]driver.Value{{c.lockAcquired}}}, nil
	}
	if c.journalErr != nil {
		return nil, c.journalErr
	}
	return &fakeMigrationRows{columns: []string{"migration_version", "migration_name", "checksum", "direction", "status", "rolled_back_at"}, rows: c.rows}, nil
}

func (c *fakeMigrationConn) ExecContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	return fakeMigrationResult{}, c.releaseErr
}

type fakeMigrationResult struct{}

func (fakeMigrationResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeMigrationResult) RowsAffected() (int64, error) { return 0, nil }

type fakeMigrationRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *fakeMigrationRows) Columns() []string { return r.columns }
func (r *fakeMigrationRows) Close() error      { return nil }
func (r *fakeMigrationRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func openFakeMigrationDB(t *testing.T, conn *fakeMigrationConn) *sql.DB {
	t.Helper()
	db := sql.OpenDB(fakeMigrationConnector{state: conn})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeMigration(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write migration %s: %v", name, err)
	}
}

func fixtureMigrations(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeMigration(t, dir, "00001_first.sql", "-- +goose Up\nCREATE TABLE first (id bigint PRIMARY KEY);\n")
	writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN note text;\n")
	return dir
}

// TestTodo_REV_034_01_Integration drives manifest pinning end to end against
// a fixture migrations directory and proves the CLI refuses to call a
// rehearsal successful without observed database evidence.
func TestTodo_REV_034_01_Integration(t *testing.T) {
	dir := fixtureMigrations(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("scan exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"Compatibility": "UNREVIEWED"`) {
		t.Fatalf("scan invented a compatibility claim:\n%s", stdout.String())
	}
	manifestPath := writeManifest(t, stdout.Bytes())
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("pinned manifest exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "manifest valid: 2 entries") {
		t.Fatalf("pinned manifest stdout does not report the valid manifest:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-manifest", manifestPath, "-rehearse"}, &stdout, &stderr); code != 1 {
		t.Fatalf("evidence-free rehearsal exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "database-url") {
		t.Fatalf("rehearsal without a disposable database URL was not refused clearly:\n%s", stderr.String())
	}
}

func writeManifest(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// TestTodo_REV_034_01_Fault proves altered, added, and missing migrations
// fail against pinned evidence instead of being silently rescanned.
func TestTodo_REV_034_01_Fault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir string)
		want   string
	}{
		{
			name: "altered bytes",
			change: func(t *testing.T, dir string) {
				writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN tampered text;\n")
			},
			want: "release digest differs from pinned manifest",
		},
		{
			name: "added migration",
			change: func(t *testing.T, dir string) {
				writeMigration(t, dir, "00003_third.sql", "-- +goose Up\nSELECT 1;\n")
			},
			want: "release digest differs from pinned manifest",
		},
		{
			name: "missing migration",
			change: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "00002_second.sql")); err != nil {
					t.Fatalf("remove migration: %v", err)
				}
			},
			want: "release digest differs from pinned manifest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixtureMigrations(t)
			var scan bytes.Buffer
			if code := run([]string{"-dir", dir, "-json"}, &scan, &bytes.Buffer{}); code != 0 {
				t.Fatalf("scan exit = %d, want 0", code)
			}
			manifestPath := writeManifest(t, scan.Bytes())
			tc.change(t, dir)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 1 {
				t.Fatalf("pinned manifest exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", stderr.String(), tc.want)
			}
		})
	}
}

// TestTodo_REV_034_01_Recovery proves a migration tree that failed the pinned
// manifest check passes again once the exact original bytes are restored.
func TestTodo_REV_034_01_Recovery(t *testing.T) {
	dir := fixtureMigrations(t)
	original := "-- +goose Up\nALTER TABLE first ADD COLUMN note text;\n"
	var scan bytes.Buffer
	if code := run([]string{"-dir", dir, "-json"}, &scan, &bytes.Buffer{}); code != 0 {
		t.Fatalf("scan exit = %d, want 0", code)
	}
	manifestPath := writeManifest(t, scan.Bytes())
	writeMigration(t, dir, "00002_second.sql", "-- +goose Up\nALTER TABLE first ADD COLUMN tampered text;\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 1 {
		t.Fatalf("tampered manifest check exit = %d, want 1", code)
	}
	writeMigration(t, dir, "00002_second.sql", original)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-dir", dir, "-manifest", manifestPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("restored manifest check exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "manifest valid: 2 entries") {
		t.Fatalf("restored check did not report valid manifest: %q", stdout.String())
	}
}

func TestTodo_CICD_002_ObservedJournalRows(t *testing.T) {
	entry := migrationci.Entry{Version: 1, Name: "first", Checksum: strings.Repeat("a", 64), Direction: "UP"}
	state := stateFromJournalRows(true, true, []journalObservation{
		{entry: entry, status: "APPLIED"},
		{entry: entry, status: "APPLIED", rolledBack: true},
		{entry: entry, status: "FAILED"},
		{entry: migrationci.Entry{Version: 2, Direction: "DOWN"}, status: "APPLIED"},
	})
	if !state.JournalPresent || !state.LockHeld || !state.Dirty || len(state.Applied) != 1 {
		t.Fatalf("observed state = %+v, want lock, dirty, and one active UP entry", state)
	}
}

func TestTodo_CICD_002_ObservedDatabaseReadsJournalAndLock(t *testing.T) {
	entry := migrationci.Entry{Version: 1, Name: "first", Checksum: strings.Repeat("a", 64), Direction: "UP"}
	db := openFakeMigrationDB(t, &fakeMigrationConn{lockAcquired: true, rows: [][]driver.Value{
		{int64(1), "first", strings.Repeat("a", 64), "UP", "APPLIED", false},
		{int64(2), "second", strings.Repeat("b", 64), "UP", "APPLIED", true},
		{int64(3), "third", strings.Repeat("c", 64), "UP", "FAILED", false},
	}})
	state, err := observeDatabaseDB(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if state.LockHeld || !state.JournalPresent || !state.Dirty || len(state.Applied) != 1 || !reflect.DeepEqual(state.Applied[0], entry) {
		t.Fatalf("state = %+v, want released lock, present dirty journal and active entry", state)
	}
}

func TestTodo_CICD_002_ObservedDatabaseRefusesUnavailableEvidence(t *testing.T) {
	t.Run("journal table absent is a fresh database", func(t *testing.T) {
		db := openFakeMigrationDB(t, &fakeMigrationConn{lockAcquired: true, journalErr: &pgconn.PgError{Code: "42P01"}})
		state, err := observeDatabaseDB(context.Background(), db)
		if err != nil || state.JournalPresent || state.LockHeld {
			t.Fatalf("state=%+v err=%v, want fresh unlocked state", state, err)
		}
	})
	t.Run("held lock is returned as observed state", func(t *testing.T) {
		db := openFakeMigrationDB(t, &fakeMigrationConn{lockAcquired: false})
		state, err := observeDatabaseDB(context.Background(), db)
		if err != nil || !state.LockHeld {
			t.Fatalf("state=%+v err=%v, want lock-held refusal state", state, err)
		}
	})
	t.Run("lock query errors", func(t *testing.T) {
		db := openFakeMigrationDB(t, &fakeMigrationConn{lockErr: errors.New("lock unavailable")})
		if _, err := observeDatabaseDB(context.Background(), db); err == nil || !strings.Contains(err.Error(), "observe migration lock") {
			t.Fatalf("err=%v, want lock observation error", err)
		}
	})
	t.Run("journal query errors", func(t *testing.T) {
		db := openFakeMigrationDB(t, &fakeMigrationConn{lockAcquired: true, journalErr: errors.New("journal unavailable")})
		if _, err := observeDatabaseDB(context.Background(), db); err == nil || !strings.Contains(err.Error(), "read migration journal") {
			t.Fatalf("err=%v, want journal observation error", err)
		}
	})
	t.Run("unlock errors", func(t *testing.T) {
		db := openFakeMigrationDB(t, &fakeMigrationConn{lockAcquired: true, releaseErr: errors.New("unlock unavailable")})
		if _, err := observeDatabaseDB(context.Background(), db); err == nil || !strings.Contains(err.Error(), "release observed migration lock") {
			t.Fatalf("err=%v, want unlock observation error", err)
		}
	})
}

func TestTodo_CICD_002_RehearsalOutputAndDatabaseRefusal(t *testing.T) {
	result := migrationci.Result{Status: "REJECTED", Findings: []migrationci.Finding{{Code: "X", Field: "state", Detail: "blocked"}}}
	var textOut, textErr bytes.Buffer
	writeRehearsalResult(result, false, &textOut, &textErr)
	if !strings.Contains(textOut.String(), "X state: blocked") || textErr.Len() != 0 {
		t.Fatalf("text rehearsal output=%q err=%q", textOut.String(), textErr.String())
	}
	var jsonOut, jsonErr bytes.Buffer
	writeRehearsalResult(result, true, &jsonOut, &jsonErr)
	var decoded migrationci.Result
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil || decoded.Status != result.Status || jsonErr.Len() != 0 {
		t.Fatalf("json rehearsal output=%q err=%q decoded=%+v", jsonOut.String(), jsonErr.String(), decoded)
	}
	if err := rehearseDatabase(context.Background(), ".", "", &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("empty database URL was accepted")
	}
	if _, err := observeDatabase(context.Background(), "://invalid"); err == nil {
		t.Fatal("invalid database URL was accepted")
	}
	if isUndefinedTable(nil) {
		t.Fatal("nil PostgreSQL error classified as undefined table")
	}
	if !isUndefinedTable(&pgconn.PgError{Code: "42P01"}) {
		t.Fatal("undefined-table PostgreSQL error was not recognized")
	}
	if isUndefinedTable(errors.New("relation does not exist")) {
		t.Fatal("generic relation error was treated as PostgreSQL undefined-table evidence")
	}
}
