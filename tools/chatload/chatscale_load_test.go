package chatload

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }
func testConfig(t *testing.T) Config {
	t.Helper()
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	return Config{DSN: os.Getenv("HCMNEXT_TEST_DATABASE_URL"), Root: root, Database: TestPrefix + "integration", Seed: 42, Posts: 1000, Tenants: 1, Channels: 300, Batch: 73, Repeats: 2, Concurrency: 2, Operations: 16}
}
func TestTodo_CHATSCALE_001(t *testing.T) {
	c := testConfig(t)
	if e := Command(context.Background(), []string{"-sizes", "bad"}, io.Discard, c.DSN); e == nil || !strings.Contains(e.Error(), "invalid syntax") {
		t.Fatalf("CLI accepted malformed size: %v", e)
	}
	for _, change := range []func(*Config){func(c *Config) { c.Database = "postgres" }, func(c *Config) { c.Database = TestPrefix + "bad;drop" }, func(c *Config) { c.DSN = "postgres://postgres@127.0.0.1:18532/postgres" }, func(c *Config) { c.DSN = "postgres://postgres@remote:18540/postgres" }, func(c *Config) { c.Posts = 3000001 }, func(c *Config) { c.Channels = 299 }, func(c *Config) { c.Batch = 0 }, func(c *Config) { c.Repeats = 1 }, func(c *Config) { c.Concurrency = 1001 }} {
		bad := c
		change(&bad)
		if bad.Validate() == nil {
			t.Fatalf("accepted unsafe config %+v", bad)
		}
	}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	seq1, seq2 := make([]int64, 300), make([]int64, 300)
	large, threads, edits, removals, mentions, attachments := 0, 0, 0, 0, 0, 0
	for i := int64(1); i <= 10000; i++ {
		a, b := generate(c, i, seq1), generate(c, i, seq2)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("seed is not deterministic")
		}
		if a.Channel < channelName(3) {
			large++
		}
		if a.Parent != "" {
			threads++
		}
		if a.Revision > 1 {
			edits++
		}
		if a.Removed {
			removals++
		}
		if strings.Contains(a.References, "PERSON_MENTION") {
			mentions++
		}
		if strings.Contains(a.References, "MEDIA") {
			attachments++
		}
	}
	if large < 6800 || large > 7600 || threads < 1900 || edits != 1000 || removals != 200 || mentions == 0 || attachments == 0 {
		t.Fatalf("wrong skew %d threads %d edits %d removals %d mentions %d attachments %d", large, threads, edits, removals, mentions, attachments)
	}
	t.Run("report", func(t *testing.T) {
		base := Measurement{Query: Query{File: "store.go", Line: 1, Function: "Counts", SQL: "WITH candidate"}, Channel: channelName(0), Bindings: []any{channelName(0)}, Timing: Percentiles{P95: 1, Samples: 2}, ReturnedRows: 1}
		small := base
		small.Channel, small.Bindings = channelName(299), []any{channelName(299)}
		large := base
		large.Timing.P95 = 20
		r := Report{Sizes: []SizeReport{{Posts: 1000, Queries: []Measurement{base, small}}, {Posts: 3000, Queries: []Measurement{large, small}}}}
		classify(&r)
		if len(r.Ranked) != 2 || r.Ranked[0].Proposal != "CHATSCALE-003" || r.Sizes[1].Queries[0].Degrades != "channel size correlated" {
			t.Fatalf("classification %+v", r)
		}
		dir := filepath.Join(c.Root, ".artifacts/lanes/agent-ui/chatscale/test-report")
		if e := os.MkdirAll(dir, 0755); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if e := WriteReport(dir, r); e != nil {
			t.Fatal(e)
		}
		data, e := os.ReadFile(filepath.Join(dir, ".artifacts/lanes/agent-ui/chatscale/report.json"))
		if e != nil {
			t.Fatal(e)
		}
		var decoded Report
		if e = json.Unmarshal(data, &decoded); e != nil || len(decoded.Sizes) != 2 {
			t.Fatalf("report round trip: %v", e)
		}
		if e = Command(context.Background(), []string{"-root", dir, "-refresh-report"}, io.Discard, ""); e != nil {
			t.Fatal(e)
		}
		data, e = os.ReadFile(filepath.Join(dir, ".artifacts/lanes/agent-ui/chatscale/report.json"))
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(data, &decoded); e != nil || len(decoded.Ranked) != 2 {
			t.Fatalf("refresh duplicated findings: %+v %v", decoded.Ranked, e)
		}
		r.Sizes[1].Queries[1].Bindings = base.Bindings
		r.Ranked = nil
		classify(&r)
		if r.Sizes[1].Queries[0].Degrades == "channel size correlated" {
			t.Fatal("identical bindings claimed channel-size evidence")
		}
		r.Sizes[1].Queries[0] = Measurement{Query: Query{Function: "searchChatContent"}, Error: "statement timeout (SQLSTATE 57014)"}
		r.Ranked = nil
		classify(&r)
		if len(r.Ranked) != 2 || r.Ranked[0].LowerBoundMS != 15000 || r.Ranked[0].P95MS != 0 || r.Ranked[0].Proposal != "Neither CHATSCALE-002 nor CHATSCALE-003" {
			t.Fatalf("timeout disappeared or fabricated latency: %+v", r.Ranked)
		}
	})
	snapshot, digest, e := snapshotSchema(c.Root)
	if e != nil {
		t.Fatal(e)
	}
	entries, e := fs.ReadDir(snapshot, ".")
	if e != nil || len(entries) < 17 || len(digest) != 64 {
		t.Fatalf("schema snapshot %v %s %v", len(entries), digest, e)
	}
	t.Run("snapshot_freezes_concurrent_edits", func(t *testing.T) {
		root := filepath.Join(c.Root, ".artifacts/lanes/agent-ui/chatscale/schema-fixture")
		dir := filepath.Join(root, "internal/data/chatstore/migrations")
		if e := os.MkdirAll(dir, 0755); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		name := filepath.Join(dir, "00001_chatscale_fixture.sql")
		if e := os.WriteFile(name, []byte("SELECT 1;"), 0600); e != nil {
			t.Fatal(e)
		}
		frozen, before, e := snapshotSchema(root)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(name, []byte("SELECT 2;"), 0600); e != nil {
			t.Fatal(e)
		}
		_, after, e := snapshotSchema(root)
		data, readErr := fs.ReadFile(frozen, "00001_chatscale_fixture.sql")
		if e != nil || readErr != nil || before == after || string(data) != "SELECT 1;" {
			t.Fatalf("snapshot followed concurrent edit: %s %s %q %v %v", before, after, data, e, readErr)
		}
	})
	qs, e := Inventory(c.Root)
	if e != nil {
		t.Fatal(e)
	}
	if len(qs) < 100 {
		t.Fatalf("inventory too small: %d", len(qs))
	}
	for _, name := range []string{"open_channel", "page_back", "sidebar_300", "search", "thread_open", "outbox_drain", "mark_read"} {
		if _, e := workloadQuery(qs, name); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range qs {
		if q.File == "" || q.Line < 1 || q.Function == "" {
			t.Fatalf("lost source provenance: %+v", q)
		}
	}
}
func TestTodo_CHATSCALE_001_Golden(t *testing.T) {
	got, e := json.Marshal(percentiles([]float64{9, 1, 5, 2, 3}))
	if e != nil {
		t.Fatal(e)
	}
	want := `{"p50_ms":3,"p95_ms":9,"p99_ms":9,"samples":5}`
	if string(got) != want {
		t.Fatalf("percentile golden: %s", got)
	}
	p := PlanNode{Node: "Limit", Rows: 2, Loops: 1, Plans: []PlanNode{{Node: "Index Scan", Rows: 2, Filtered: 8, Loops: 3}}}
	if n := rowsRead(p); n != 30 {
		t.Fatalf("scan rows %v", n)
	}
	if percentiles(nil).Samples != 0 {
		t.Fatal("empty sample count")
	}
	if binding("countScanLimit", 23, channelName(0)) != int64(5000) {
		t.Fatal("counter cap binding")
	}
	if binding("before", 23, channelName(0)) != int64(1<<31-1) {
		t.Fatal("cursor exceeds PostgreSQL int4")
	}
	if !reflect.DeepEqual(binding("r.Principal.TenantID", 25, channelName(0)), "t000") {
		t.Fatal("tenant binding")
	}
}
func TestTodo_CHATSCALE_001_Performance(t *testing.T) {
	// pgtest acquires the shared server without starting a process; the load
	// generator itself requires a separate guarded database, not a schema.
	_ = pgtest.NewEmpty(t)
	c := testConfig(t)
	ctx := context.Background()
	sentinel := errors.New("callback failure")
	e := WithDatabase(ctx, c, func(conn *pgx.Conn) error {
		if e := Migrate(ctx, c); e != nil {
			return e
		}
		if e := Load(ctx, conn, c); e != nil {
			return e
		}
		if e := Load(ctx, conn, c); e != nil {
			return e
		}
		var n int64
		if e := conn.QueryRow(ctx, "SELECT count(*) FROM chat_post").Scan(&n); e != nil {
			return e
		}
		var threads int64
		if e := conn.QueryRow(ctx, "SELECT count(*) FROM chat_post WHERE parent_id<>''").Scan(&threads); e != nil {
			return e
		}
		if threads != c.Posts/5 {
			t.Fatalf("thread share %d of %d", threads, c.Posts)
		}
		if n != c.Posts {
			t.Fatalf("resumption duplicates posts: %d", n)
		}
		c.Posts = 1200
		if e := Load(ctx, conn, c); e != nil {
			return e
		}
		if e := conn.QueryRow(ctx, "SELECT count(*) FROM chat_post").Scan(&n); e != nil {
			return e
		}
		if n != 1200 {
			t.Fatalf("resume target: %d", n)
		}
		bad := c
		bad.Seed++
		if Load(ctx, conn, bad) == nil {
			t.Fatal("resume accepted changed seed")
		}
		bad.Database = TestPrefix + "different"
		if Load(ctx, conn, bad) == nil {
			t.Fatal("wrong database connection accepted")
		}
		qs, e := Inventory(c.Root)
		if e != nil {
			return e
		}
		q, e := queryBy(qs, "Counts", "WITH candidate")
		if e != nil {
			return e
		}
		m := Measure(ctx, conn, q, channelName(0), 2)
		if m.Error != "" || m.Timing.Samples != 2 || m.Plan == nil || m.ReadRows == 0 {
			t.Fatalf("measurement failed: %+v", m)
		}
		resume := c
		resume.Seed++
		if e := WithResumedDatabase(ctx, resume, func(*pgx.Conn) error { t.Fatal("adopted a mismatched resume database"); return nil }); e == nil {
			t.Fatal("accepted wrong resume ownership")
		}
		worker, e := pgx.ConnectConfig(ctx, databaseConfig(c))
		if e != nil {
			return e
		}
		defer worker.Close(ctx)
		done := make(chan error, 1)
		go func() { _, e := worker.Exec(ctx, "SELECT pg_sleep(10)"); done <- e }()
		time.Sleep(30 * time.Millisecond)
		cancelled, e := CancelMeasurement(ctx, c)
		if e != nil {
			return e
		}
		if cancelled < 1 {
			t.Fatal("did not cancel own active statement")
		}
		var pgError *pgconn.PgError
		if err := <-done; !errors.As(err, &pgError) || pgError.Code != "57014" {
			t.Fatalf("query cancellation: %v", err)
		}
		status, e := Status(ctx, c)
		if e != nil {
			return e
		}
		if len(status) == 0 {
			t.Fatal("status omitted own database")
		}
		if !strings.Contains(strings.Join(status, "\n"), "committed_generated_posts=1196 database_bytes=") {
			t.Fatalf("status omitted committed progress: %v", status)
		}
		w, e := Mixed(ctx, c, qs)
		if e != nil {
			return e
		}
		for _, o := range w.Operations {
			if o.Errors != 0 || o.Timing.Samples != 2 {
				t.Fatalf("operation failed %+v", o)
			}
		}
		if e = conn.QueryRow(ctx, "SELECT count(*) FROM chat_post").Scan(&n); e != nil {
			return e
		}
		if n != 1202 {
			t.Fatalf("mixed send transaction state: %d", n)
		}
		if w.SendsPerSecond <= 0 {
			t.Fatal("no committed sends")
		}
		base, indexes, e := IndexCosts(ctx, conn, 2)
		if e != nil {
			return e
		}
		if base.Samples != 2 || len(indexes) < 5 {
			t.Fatalf("missing index evidence %v %+v", base, indexes)
		}
		if _, e = conn.Exec(ctx, "SET statement_timeout='1ms'"); e != nil {
			return e
		}
		if e = resetMaintenanceLimits(ctx, conn); e != nil {
			return e
		}
		if _, e = conn.Exec(ctx, "SELECT pg_sleep(0.02)"); e != nil {
			return e
		}
		for _, i := range indexes {
			if i.Error != "" || i.Timing.Samples != 2 {
				t.Fatalf("index failure %+v", i)
			}
		}
		r, e := relations(ctx, conn)
		if e != nil {
			return e
		}
		if len(r) < 12 {
			t.Fatal("missing relation sizes")
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	admin, e := pgx.Connect(ctx, c.DSN)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close(ctx)
	var exists bool
	if e = admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", c.Database).Scan(&exists); e != nil {
		t.Fatal(e)
	}
	r, e := Run(ctx, c, []int64{1000})
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range r.Sizes[0].Queries {
		if m.Error != "" && !strings.Contains(m.Error, "no unique or exclusion constraint matching the ON CONFLICT specification") {
			t.Fatalf("catalogue fixture failed: %s:%d %s %s SQL=%s bindings=%v", m.Query.File, m.Query.Line, m.Query.Function, m.Error, m.Query.SQL, m.Bindings)
		}
	}
	if len(r.Sizes) != 1 || len(r.Sizes[0].Queries) < 200 || len(r.Ranked) == 0 || r.Sizes[0].DatabaseBytes <= 0 {
		t.Fatalf("incomplete measured report %+v", r)
	}
	if exists {
		t.Fatal("database leaked after callback error")
	}
}
