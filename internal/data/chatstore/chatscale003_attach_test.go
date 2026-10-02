package chatstore

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestChatscale003_Attach runs the CHATSCALE-001 statements against a schema the
// volume tool left behind (HCMNEXT_TEST_KEEP_SCHEMA=1), so a statement can be
// tuned at a million posts without loading them again. It needs
// CHATSCALE003_SCHEMA and the usual test database URL, and does nothing otherwise.
//
//	CHATSCALE003_SCHEMA=t_...     the kept schema
//	CHATSCALE003_APPLY=a.sql      a candidate (statements split by "-- @@") to apply first
//	CHATSCALE003_ONLY=first open  measure only statements whose name contains this
//	CHATSCALE003_EXPLAIN=1        also print the plan of the first-open read
//	CHATSCALE003_DROP=1           drop the schema when done (=only: drop it and measure nothing)
func TestChatscale003_Attach(t *testing.T) {
	schema := os.Getenv("CHATSCALE003_SCHEMA")
	if schema == "" {
		t.Skip("set CHATSCALE003_SCHEMA to attach to a kept volume schema")
	}
	u, err := url.Parse(os.Getenv("HCMNEXT_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := New(context.Background(), Config{DSN: u.String(), CoreDSN: "postgres://core:pw@127.0.0.1:1/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if os.Getenv("CHATSCALE003_DROP") == "only" {
		if _, err := s.pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schema)); err != nil {
			t.Fatal(err)
		}
		return
	}
	if path := os.Getenv("CHATSCALE003_APPLY"); path != "" {
		c001ApplyCandidate(t, s, path)
	}
	channels := c001EnvInt("CHATSCALE001_CHANNELS", 300)
	c001Maintain(t, s)
	c001ExplainCold(t, s, channels)
	only := os.Getenv("CHATSCALE003_ONLY")
	var results []c001Result
	for _, stmt := range c001Statements(t, s, channels) {
		if only != "" && !strings.Contains(stmt.Group+" / "+stmt.Name, only) {
			continue
		}
		results = append(results, c001Measure(t, s, "attached", stmt, c001EnvInt("CHATSCALE001_SAMPLES", 5)))
	}
	c001Print(t, results)
	if os.Getenv("CHATSCALE003_DROP") != "" {
		if _, err := s.pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schema)); err != nil {
			t.Fatal(err)
		}
	}
}
