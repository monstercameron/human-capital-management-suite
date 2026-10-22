package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/tools/chatload"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }
func commandArgs(t *testing.T, args ...string) {
	t.Helper()
	previousArgs := os.Args
	os.Args = append([]string{"chatload"}, args...)
	t.Cleanup(func() { os.Args = previousArgs })
}
func TestTodo_CHATSCALE_001(t *testing.T) {
	commandArgs(t, "-sizes", "not-a-number")
	if e := run(); e == nil || !strings.Contains(e.Error(), "invalid syntax") {
		t.Fatalf("malformed target accepted: %v", e)
	}
	if code := entry(); code != 1 {
		t.Fatalf("malformed target exit code %d", code)
	}
}
func TestTodo_CHATSCALE_001_Performance(t *testing.T) {
	_ = pgtest.NewEmpty(t)
	commandArgs(t, "-status", "-channels", "300", "-tenants", "1")
	dsn := os.Getenv("HCMNEXT_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres@127.0.0.1:18540/postgres?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("no shared test PostgreSQL at the guarded address: %v", err)
	}
	_ = conn.Close(context.Background())
	c := chatload.Config{DSN: dsn, Database: chatload.TestPrefix + fmt.Sprintf("command_%d", time.Now().UnixNano()), Seed: 1, Posts: 5, Tenants: 1, Channels: 300, Batch: 5, Repeats: 2, Concurrency: 1, Operations: 8}
	e := chatload.WithDatabase(context.Background(), c, func(*chatload.Database) error {
		r, w, e := os.Pipe()
		if e != nil {
			return e
		}
		previous := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = previous }()
		if code := entry(); code != 0 {
			t.Fatalf("status command exit %d", code)
		}
		_ = w.Close()
		output, e := io.ReadAll(r)
		_ = r.Close()
		if e != nil {
			return e
		}
		if !strings.Contains(string(output), c.Database) {
			t.Fatalf("status command omitted fixture: %s", output)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
