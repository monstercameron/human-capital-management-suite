package application

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestTodo_REV_055_03(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("start composed serve role: %v", err)
	}
	if composed.Cell() == nil || composed.Cell().Health == nil {
		t.Fatal("serve composition did not install a shared health server")
	}
	waitHealthStatus(t, composed.HTTPAddr(), "/healthz", http.StatusOK, time.Second)
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusServiceUnavailable, time.Second)
}

func TestTodo_REV_055_03_Integration(t *testing.T) {
	interval := 60 * time.Millisecond
	var currentConfig atomic.Value
	currentConfig.Store("config-ready")
	currentConfigFingerprint := func() (string, error) { return currentConfig.Load().(string), nil }
	composed, pool, _ := composeServeWithDatabase(t, interval, nil, currentConfigFingerprint)
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("start composed serve role: %v", err)
	}
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusOK, time.Second)
	waitHealthStatus(t, composed.HTTPAddr(), "/healthz", http.StatusOK, time.Second)
	if pool == nil {
		t.Fatal("integration composition omitted the real PostgreSQL pool")
	}
	currentConfig.Store("config-drift")
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusServiceUnavailable, interval+time.Second)
	currentConfig.Store("config-ready")
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusOK, interval+time.Second)

	wantSchemaFingerprint, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE schema_release SET artifact_digest='schema-drift'`); err != nil {
		t.Fatalf("simulate schema fingerprint mismatch: %v", err)
	}
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusServiceUnavailable, interval+time.Second)
	if _, err := pool.Exec(context.Background(), `UPDATE schema_release SET artifact_digest=$1`, wantSchemaFingerprint); err != nil {
		t.Fatalf("restore schema fingerprint: %v", err)
	}
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusOK, interval+time.Second)
}

func TestTodo_REV_055_03_Recovery(t *testing.T) {
	interval := 50 * time.Millisecond
	var connected atomic.Bool
	connected.Store(true)
	var pool *pgxadapter.Pool
	ping := func(ctx context.Context) error {
		if !connected.Load() {
			return errors.New("connection refused")
		}
		return pool.Ping(ctx)
	}
	composed, openedPool, _ := composeServeWithDatabase(t, interval, ping, func() (string, error) { return "config-ready", nil })
	pool = openedPool
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("start composed serve role: %v", err)
	}
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusOK, time.Second)
	connected.Store(false)
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusServiceUnavailable, interval+time.Second)
	connected.Store(true)
	waitHealthStatus(t, composed.HTTPAddr(), "/readyz", http.StatusOK, interval+time.Second)
}

func composeServeWithDatabase(t *testing.T, interval time.Duration, ping func(context.Context) error, currentConfigFingerprint func() (string, error)) (*App, *pgxadapter.Pool, *pgtest.DB) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	artifactFingerprint, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO schema_release (
		release_id, release_version, artifact_digest, digest_algorithm, source_digest,
		tool_version, compatibility_class, owner, reversible, trusted_time_source
	) VALUES ($1,$2,$3,'sha256',$3,'test','FULL','test',true,'test')`, uuid.New(), "rev-055-03-test", artifactFingerprint); err != nil {
		t.Fatalf("record expected schema fingerprint: %v", err)
	}
	cfg := stubServeConfig()
	cfg.DatabaseURL = db.URL
	cfg.ConfigFingerprint = "config-ready"
	options := Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{}), WithHealthPoolPing(ping, interval, 200*time.Millisecond), WithCurrentHealthConfigFingerprint(currentConfigFingerprint))
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Logger: discardLogger{}, Identity: "rev-055-03", Options: options})
	if err != nil {
		t.Fatalf("compose serve against isolated database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("stop composed serve role: %v", err)
		}
	})
	return composed, pool, db
}

func checkHealthStatus(t *testing.T, addr, path string) int {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + addr + path)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	return response.StatusCode
}

func waitHealthStatus(t *testing.T, addr, path string, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if got := checkHealthStatus(t, addr, path); got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s did not reach status %d within %s", path, want, within)
}
