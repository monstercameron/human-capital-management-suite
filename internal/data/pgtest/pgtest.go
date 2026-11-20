// Package pgtest runs the authoritative PostgreSQL server that the data-plane
// tests execute against (owner: data plane; phase: P1A).
//
// # How a server is obtained
//
// If HCMNEXT_TEST_DATABASE_URL is set, that server is used as-is and nothing is
// downloaded or started. This is the escape hatch for CI, for a shared
// development server, and for any machine where running an embedded server is
// undesirable.
//
// Otherwise one embedded PostgreSQL server is started per `go test` process, on
// a free localhost port, from binaries cached outside the repository. The cache
// directory is HCMNEXT_TEST_PG_CACHE, or <user cache dir>/hcm-next/embedded-postgres.
//
// # Windows on ARM64
//
// The zonky binary repository publishes no windows/arm64 build. Windows on ARM64
// runs x64 binaries through its emulator, so this package downloads the
// windows/amd64 jar itself and places the archive in the cache under the exact
// name embedded-postgres derives for this machine. embedded-postgres then finds
// the archive, skips its own download, and extracts and runs it. This is the only
// available override: Config exposes CachePath, BinariesPath and
// BinaryRepositoryURL but keeps VersionStrategy unexported.
//
// The extracted binaries live in each process's own temporary runtime directory
// rather than a shared one, because on Windows embedded-postgres looks for
// "bin/pg_ctl" without the .exe suffix and therefore re-extracts on every start.
//
// # Isolation
//
// Every test gets its own PostgreSQL schema, created fresh and dropped on
// cleanup, with search_path pinned on every connection. Migrations run inside
// that schema, including Goose's own version table, so tests may call
// t.Parallel() and cannot observe each other's rows.
//
// # Connection handles
//
// A test gets a database/sql handle (which Goose drives, and which pools) and
// connections through internal/data/dbport - the same port production code
// takes - for statements that need an explicit transaction. pgxpool is
// deliberately not used: its puddle dependency is absent from the module
// requirements, and this package may not edit them.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// Environment variables understood by this harness.
const (
	// EnvDatabaseURL points at an already-running PostgreSQL server. When set,
	// no server is downloaded or started.
	EnvDatabaseURL = "HCMNEXT_TEST_DATABASE_URL"
	// EnvCacheDir overrides where PostgreSQL binaries are cached.
	EnvCacheDir = "HCMNEXT_TEST_PG_CACHE"
	// EnvKeepSchema keeps per-test schemas after the run, for debugging.
	EnvKeepSchema = "HCMNEXT_TEST_KEEP_SCHEMA"
)

var (
	serverOnce sync.Once
	serverErr  error

	serverMu   sync.Mutex
	serverURL  string
	serverStop func() error

	adminMu   sync.Mutex
	adminConn *pgxadapter.Conn
)

// RunMain runs the package's tests and shuts down the PostgreSQL server
// afterwards. Every data-plane test package calls it from TestMain:
//
//	func TestMain(m *testing.M) { pgtest.RunMain(m) }
//
// The server starts on the first test that asks for a database, not here. Tests
// that need no database - golden vectors, fuzz targets over pure functions - run
// without one, and `go test -fuzz` workers do not each start their own server.
func RunMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	code := m.Run()
	stopServer()
	return code
}

// ensureServer starts the shared server once per test process.
func ensureServer() error {
	serverOnce.Do(func() { serverErr = startServer() })
	return serverErr
}

// ServerURL returns the administrative connection URL of the running server.
func ServerURL() string {
	serverMu.Lock()
	defer serverMu.Unlock()
	return serverURL
}

func startServer() error {
	serverMu.Lock()
	if external := os.Getenv(EnvDatabaseURL); external != "" {
		serverURL = external
	} else {
		url, stop, err := startEmbedded()
		if err != nil {
			serverMu.Unlock()
			return err
		}
		serverURL = url
		serverStop = stop
	}
	url := serverURL
	serverMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgxadapter.Connect(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", redact(url), err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close(ctx)
		return fmt.Errorf("ping %s: %w", redact(url), err)
	}

	adminMu.Lock()
	adminConn = conn
	adminMu.Unlock()
	return nil
}

func stopServer() {
	adminMu.Lock()
	if adminConn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = adminConn.Close(ctx)
		cancel()
		adminConn = nil
	}
	adminMu.Unlock()

	serverMu.Lock()
	defer serverMu.Unlock()
	if serverStop != nil {
		if err := serverStop(); err != nil {
			fmt.Fprintf(os.Stderr, "pgtest: stopping embedded server: %v\n", err)
		}
		serverStop = nil
	}
}

// withAdmin runs one administrative statement. The administrative connection is
// a single pgx connection, so access to it is serialized.
func withAdmin(ctx context.Context, fn func(dbport.Conn) error) error {
	adminMu.Lock()
	defer adminMu.Unlock()
	if adminConn == nil {
		return fmt.Errorf("no server; the test package must call pgtest.RunMain from TestMain")
	}
	if adminConn.IsClosed() {
		if err := reconnectAdminLocked(ctx); err != nil {
			return err
		}
	}
	err := fn(adminConn)
	if err == nil || !adminConn.IsClosed() {
		return err
	}
	// pgx closes a connection whose statement was interrupted by its
	// context. One schema drop that overran its deadline under load must not
	// turn into "conn closed" for every later CREATE SCHEMA in the package,
	// so reconnect, and retry only when the caller's context is still live
	// (retrying with a spent context would close the new connection too).
	if rerr := reconnectAdminLocked(ctx); rerr != nil {
		return fmt.Errorf("%w (and reconnecting the admin connection failed: %v)", err, rerr)
	}
	if ctx.Err() != nil {
		return err
	}
	return fn(adminConn)
}

// reconnectAdminLocked replaces the shared admin connection. adminMu must be
// held.
func reconnectAdminLocked(ctx context.Context) error {
	serverMu.Lock()
	url := serverURL
	serverMu.Unlock()
	if url == "" {
		return fmt.Errorf("no server URL to reconnect the admin connection")
	}
	connectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	conn, err := pgxadapter.Connect(connectCtx, url, nil)
	if err != nil {
		return fmt.Errorf("reconnect admin connection to %s: %w", redact(url), err)
	}
	adminConn = conn
	return nil
}

func startEmbedded() (string, func() error, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", nil, err
	}
	platform := resolvePlatform(runtime.GOOS, runtime.GOARCH)

	// The binaries are extracted into this process's own runtime directory, not a
	// shared one: on Windows embedded-postgres looks for "bin/pg_ctl" without the
	// .exe suffix, never finds it, and re-extracts on every start. Sharing that
	// directory across concurrent test processes would have them overwrite each
	// other's running server.
	// Earlier test processes that died before their stop function ran leave
	// their runtime directories behind; reclaim the abandoned ones first.
	sweepStaleRuntimes(os.TempDir(), staleRuntimeAge, time.Now())
	runtimePath, err := createRuntimeParentDir(runtime.GOOS)
	if err != nil {
		return "", nil, fmt.Errorf("create runtime directory: %w", err)
	}

	start := func() (string, func() error, error) {
		port, err := freePort()
		if err != nil {
			return "", nil, err
		}
		config := embeddedpostgres.DefaultConfig().
			Version(postgresVersion).
			Port(port).
			CachePath(dir).
			RuntimePath(runtimePath).
			DataPath(filepath.Join(runtimePath, "data")).
			BinaryRepositoryURL(binaryRepository).
			// initdb on Windows otherwise picks a WIN1252 cluster, which would
			// mangle the UTF-8 text the canonical digest is computed over.
			Locale("C").
			Encoding("UTF8").
			StartTimeout(3 * time.Minute).
			Logger(os.Stderr).
			// Durability is exercised by the production configuration, not by
			// every unit test; these settings only make the test server quick.
			StartParameters(map[string]string{
				"fsync":              "off",
				"full_page_writes":   "off",
				"synchronous_commit": "off",
				"max_connections":    "200",
				"lock_timeout":       "60s",
			})

		server := embeddedpostgres.NewDatabase(config)
		if err := server.Start(); err != nil {
			return "", nil, err
		}
		stop := func() error {
			stopErr := server.Stop()
			// Windows keeps the just-stopped server's DLLs mapped for a moment,
			// so the runtime directory needs a few attempts before it will go.
			removeErr := removeWithRetry(runtimePath)
			if stopErr != nil {
				return stopErr
			}
			return removeErr
		}
		url := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
		return url, stop, nil
	}

	// Only the download is shared, so only the download is serialized across
	// concurrently running test processes.
	if err := withDirectoryLock(dir, func() error {
		_, archiveErr := ensureArchive(dir, platform)
		return archiveErr
	}); err != nil {
		_ = os.RemoveAll(runtimePath)
		return "", nil, err
	}

	url, stop, err := start()
	if err != nil {
		_ = os.RemoveAll(runtimePath)
		return "", nil, fmt.Errorf("start embedded postgres: %w", err)
	}
	return url, stop, nil
}

// DB is one isolated database handle: a private schema on the shared server,
// with the migration tree already applied unless NewEmpty was used.
type DB struct {
	// SQL is the database/sql handle Goose drives. Every connection it opens has
	// search_path pinned to this test's schema.
	SQL *sql.DB
	// Conn is a connection on the same schema, exposed through
	// internal/data/dbport, for statements that want an explicit transaction. It
	// is not safe for concurrent use; call NewConn for an independent
	// connection.
	Conn *pgxadapter.Conn
	// Schema is the PostgreSQL schema owned by this test.
	Schema string
	// URL is the server connection URL. It carries no schema; the schema is set
	// as a runtime parameter on each connection.
	URL string

	provider *goose.Provider
}

// New returns an isolated schema with every migration applied.
// schemaDropTimeout bounds the DROP SCHEMA ... CASCADE that ends every test.
const schemaDropTimeout = 5 * time.Minute

func New(t *testing.T) *DB {
	t.Helper()
	db := NewEmpty(t)
	if _, err := db.Provider(t).Up(context.Background()); err != nil {
		t.Fatalf("apply migrations to schema %s: %v", db.Schema, err)
	}
	return db
}

// NewEmpty returns an isolated, empty schema. Callers drive Goose themselves.
func NewEmpty(t *testing.T) *DB {
	t.Helper()

	if err := ensureServer(); err != nil {
		t.Fatalf("pgtest: cannot obtain a PostgreSQL server: %v; "+
			"set %s to point at an existing server to bypass the embedded one",
			err, EnvDatabaseURL)
	}

	serverMu.Lock()
	url := serverURL
	serverMu.Unlock()

	schema := schemaName()
	ctx := context.Background()
	err := withAdmin(ctx, func(conn dbport.Conn) error {
		_, execErr := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", quoteIdentifier(schema)))
		return execErr
	})
	if err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		if os.Getenv(EnvKeepSchema) != "" {
			t.Logf("pgtest: keeping schema %s", schema)
			return
		}
		// Dropping a fully migrated schema cascades through several hundred
		// relations; under a loaded machine (a pre-commit hook running five
		// database packages while another session's hook runs) that has
		// taken over a minute, and an interrupted drop closes the shared
		// admin connection. Give it room.
		dropCtx, cancel := context.WithTimeout(context.Background(), schemaDropTimeout)
		defer cancel()
		dropErr := withAdmin(dropCtx, func(conn dbport.Conn) error {
			_, execErr := conn.Exec(dropCtx, fmt.Sprintf("DROP SCHEMA %s CASCADE", quoteIdentifier(schema)))
			return execErr
		})
		if dropErr != nil {
			t.Errorf("drop schema %s: %v", schema, dropErr)
		}
	})

	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse %s: %v", redact(url), err)
	}
	// Pinning search_path on the connection - rather than qualifying every
	// statement - is what makes the migration tree schema-relative and therefore
	// safe to apply many times in parallel inside one server.
	connCfg.RuntimeParams["search_path"] = schema

	sqlDB := stdlib.OpenDB(*connCfg)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sql handle for %s: %v", schema, err)
		}
	})

	db := &DB{
		SQL:    sqlDB,
		Schema: schema,
		URL:    url,
	}
	db.Conn = db.NewConn(t)
	return db
}

// NewConn opens an independent connection on this test's schema. Concurrency
// tests use it to hold genuinely separate sessions and transactions.
func (d *DB) NewConn(t *testing.T) *pgxadapter.Conn {
	t.Helper()

	serverMu.Lock()
	url := serverURL
	serverMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := pgxadapter.Connect(ctx, url, map[string]string{"search_path": d.Schema})
	if err != nil {
		t.Fatalf("connect to schema %s: %v", d.Schema, err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	})
	return conn
}

// Provider returns the Goose provider bound to this schema. It is created once
// per DB so that up, down and status all address the same version table.
func (d *DB) Provider(t *testing.T) *goose.Provider {
	t.Helper()
	if d.provider != nil {
		return d.provider
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		d.SQL,
		migrations.FS,
		goose.WithVerbose(false),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		t.Fatalf("create goose provider for schema %s: %v", d.Schema, err)
	}
	d.provider = provider
	return provider
}

// Exec runs a statement against the isolated schema and fails the test on error.
func (d *DB) Exec(t *testing.T, sqlText string, args ...any) {
	t.Helper()
	if _, err := d.Conn.Exec(context.Background(), sqlText, args...); err != nil {
		t.Fatalf("exec %q: %v", firstLine(sqlText), err)
	}
}

// ExecErr runs a statement and returns its error, if any. Negative fixtures use
// it to prove that a constraint or trigger rejects the write.
func (d *DB) ExecErr(sqlText string, args ...any) error {
	_, err := d.Conn.Exec(context.Background(), sqlText, args...)
	return err
}

// QueryRow forwards to the connection bound to this schema.
func (d *DB) QueryRow(ctx context.Context, sqlText string, args ...any) dbport.Row {
	return d.Conn.QueryRow(ctx, sqlText, args...)
}

func schemaName() string {
	return "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		return s[:idx] + " ..."
	}
	return s
}

// redact removes the password from a connection URL before it reaches a log.
func redact(url string) string {
	at := strings.LastIndex(url, "@")
	scheme := strings.Index(url, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return url
	}
	return url[:scheme+3] + "***" + url[at:]
}
