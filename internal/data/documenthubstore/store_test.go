package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestTodo_HUB_001 is the PRIMARY test for HUB-001: the Knowledge document
// database must be isolated from chat and workflow storage. Document writes
// or semantic queries sharing chat/workflow credentials or storage capacity
// is the RED state this test pins.
func TestTodo_HUB_001(t *testing.T) {
	ctx := context.Background()

	if _, err := New(ctx, Config{}); err == nil {
		t.Fatal("empty document DSN accepted")
	}

	coreURL := "postgres://u:p@host:5432/core?sslmode=disable"
	if _, err := New(ctx, Config{DSN: coreURL, CoreDSN: coreURL}); !errors.Is(err, ErrIsolatedDatabase) {
		t.Fatalf("shared core database accepted: err=%v", err)
	}

	chatKeyword := "host=db port=5432 dbname=chat"
	if _, err := New(ctx, Config{DSN: chatKeyword, ChatDSN: "host=db port=5432 dbname=chat user=other"}); !errors.Is(err, ErrIsolatedDatabase) {
		t.Fatalf("shared chat database accepted across credential spellings: err=%v", err)
	}

	mixedCore := "host=host port=5432 dbname=core"
	if _, err := New(ctx, Config{DSN: coreURL, CoreDSN: mixedCore}); !errors.Is(err, ErrIsolatedDatabase) {
		t.Fatalf("shared core database accepted across URL/keyword spellings: err=%v", err)
	}

	if _, err := New(ctx, Config{DSN: "postgres://u:p@host:5432/docs", MaxConns: 2, MinConns: 9}); err == nil {
		t.Fatal("inverted pool bounds accepted")
	}

	got, err := withPoolSize("postgres://u:p@host:5432/docs?sslmode=disable", 12, 3)
	if err != nil || !strings.Contains(got, "&pool_max_conns=12") || !strings.Contains(got, "&pool_min_conns=3") {
		t.Fatalf("pool budget missing from DSN: %q err=%v", got, err)
	}
	got, err = withPoolSize("postgres://u:p@host:5432/docs?pool_max_conns=2", 16, 0)
	if err != nil || got != "postgres://u:p@host:5432/docs?pool_max_conns=2" {
		t.Fatalf("operator pool spelling overridden: %q err=%v", got, err)
	}
	if got, err = withPoolSize("postgres://u:p@host:5432/docs", 0, 0); err != nil || got != "postgres://u:p@host:5432/docs" {
		t.Fatalf("unset bounds rewrote DSN: %q err=%v", got, err)
	}
	if got, err = withPoolSize("host=db port=5432 dbname=docs", 8, 2); err != nil || got != "host=db port=5432 dbname=docs pool_max_conns=8 pool_min_conns=2" {
		t.Fatalf("keyword pool budget missing: %q err=%v", got, err)
	}
	if !sameDatabase("not a dsn", "not a dsn") || sameDatabase("not a dsn", "other") {
		t.Fatal("unparsable DSN fallback must be exact string equality")
	}
	if sameDatabase("postgres://u:p@a:5432/docs", "postgres://u:p@a:5433/docs") ||
		sameDatabase("postgres://u:p@a:5432/docs", "postgres://u:p@b:5432/docs") ||
		sameDatabase("postgres://u:p@a:5432/docs", "postgres://u:p@a:5432/other") {
		t.Fatal("distinct port, host or database compared equal")
	}

	var nilStore *Store
	nilStore.Close()
	(&Store{}).Close()
}
