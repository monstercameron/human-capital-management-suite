package chatstore

import (
	"strings"
	"testing"
)

// TestTodo_CHAT_003_PoolSize proves Config.MaxConns/MinConns reach the pool in
// both DSN spellings, that an operator's own spelling is preserved, and that an
// inverted pair is refused instead of silently reordered.
func TestTodo_CHAT_003_PoolSize(t *testing.T) {
	got, err := withPoolSize("postgres://u:p@host:5432/chat?sslmode=disable", 12, 3)
	if err != nil || !strings.Contains(got, "&pool_max_conns=12") || !strings.Contains(got, "&pool_min_conns=3") {
		t.Fatalf("url dsn=%q err=%v", got, err)
	}
	got, err = withPoolSize("postgres://u:p@host:5432/chat", 4, 0)
	if err != nil || got != "postgres://u:p@host:5432/chat?pool_max_conns=4" {
		t.Fatalf("url without query=%q err=%v", got, err)
	}
	got, err = withPoolSize("host=db port=5432 dbname=chat", 8, 2)
	if err != nil || got != "host=db port=5432 dbname=chat pool_max_conns=8 pool_min_conns=2" {
		t.Fatalf("keyword dsn=%q err=%v", got, err)
	}
	got, err = withPoolSize("postgres://u:p@host:5432/chat?pool_max_conns=2", 16, 0)
	if err != nil || got != "postgres://u:p@host:5432/chat?pool_max_conns=2" {
		t.Fatalf("operator spelling overridden: %q err=%v", got, err)
	}
	if got, err = withPoolSize("postgres://u:p@host:5432/chat", 0, 0); err != nil || got != "postgres://u:p@host:5432/chat" {
		t.Fatalf("unset bounds=%q err=%v", got, err)
	}
	if _, err = withPoolSize("postgres://u:p@host:5432/chat", 2, 9); err == nil {
		t.Fatal("MinConns above MaxConns accepted")
	}
}
