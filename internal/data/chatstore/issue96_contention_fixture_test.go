package chatstore

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// Issue #96: the contention test's independent lock connection must come from
// the isolated fixture server and its role, never from the machine's ambient
// localhost:5432 listener or login name.
func TestIssue96_ContentionConnectionUsesTheIsolatedFixture_Integration(t *testing.T) {
	_, schema := chatFixture(t)
	raw := pgtest.ServerURL()
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		t.Fatalf("fixture server URL %q: %v", raw, err)
	}
	if external := os.Getenv(pgtest.EnvDatabaseURL); external == "" {
		if u.Port() == "5432" || u.Port() == "" {
			t.Fatalf("fixture server uses the ambient default port: %s", u.Host)
		}
		if u.User == nil || u.User.Username() == "" {
			t.Fatalf("fixture server URL names no explicit role")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := pgxadapter.Connect(ctx, raw, map[string]string{"search_path": schema})
	if err != nil {
		t.Fatalf("connect to the fixture server: %v", err)
	}
	defer conn.Close(context.Background())
	var current string
	if err := conn.QueryRow(ctx, `SELECT current_schema()`).Scan(&current); err != nil || current != schema {
		t.Fatalf("current_schema=%q err=%v want %q", current, err, schema)
	}
}

// The contention test itself must take its lock connection from the fixture
// server, not from an ambient default (host, port or login role).
func TestIssue96_ContentionTestSourceHasNoAmbientConnection(t *testing.T) {
	src, err := os.ReadFile("chatscale_contention_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "pgtest.ServerURL()") {
		t.Fatalf("the contention test no longer connects through pgtest.ServerURL()")
	}
	for _, ambient := range []string{"localhost", "127.0.0.1", ":5432", "os.Getenv(\"USER", "user.Current"} {
		if strings.Contains(text, ambient) {
			t.Fatalf("the contention test names an ambient connection detail %q", ambient)
		}
	}
}
