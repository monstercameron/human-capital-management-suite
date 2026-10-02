package chatstore

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var chatscaleReportPath = flag.String("chatscale.report", "", "write SQL plans, timing percentiles and relation sizes as JSON")

// Recover literal read statements from the store rather than maintaining
// a second, drifting copy of production SQL in the performance harness.
func chatscaleLiteral(t *testing.T, file, function, method string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var query string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != method {
				return true
			}
			literal, ok := call.Args[1].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && (strings.HasPrefix(strings.TrimSpace(value), "SELECT") || strings.HasPrefix(strings.TrimSpace(value), "WITH") || strings.HasPrefix(strings.TrimSpace(value), "DELETE")) {
				if query == "" {
					query = value
				}
			}
			return true
		})
	}
	if query == "" {
		t.Fatalf("no literal %s in %s.%s", method, file, function)
	}
	return query
}

const chatscaleLegacyCounts = `WITH candidate AS (
            SELECT p.sequence, p.references_json, (p.sequence>COALESCE(c.last_sequence,0)) AS unseen
            FROM chat_post p
            JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id
                AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active'
            LEFT JOIN chat_cursor c ON c.tenant_id=p.tenant_id AND c.conversation_id=p.conversation_id
                AND c.home_tenant_id=$3 AND c.member_id=$4
            WHERE p.tenant_id=$1 AND p.conversation_id=$2
                AND (p.sequence>COALESCE(c.last_sequence,0) OR (p.revision>1 AND p.updated_at>c.updated_at))
                AND p.tombstoned=false AND NOT (p.author_home_tenant_id=$3 AND p.author_id=$4)
                AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))
            ORDER BY p.sequence DESC
            LIMIT $5
        ) SELECT count(*) FILTER (WHERE unseen), count(*) FILTER (
            WHERE references_json @> jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$3::text,'ID',$4::text))
        ) FROM candidate`

type chatscaleRead struct {
	name  string
	query string
	args  []any
}

func chatscaleEventSQL(t *testing.T) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "event_stream.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"OutboxKeyConversationID": OutboxKeyConversationID, "OutboxKeyTargetID": OutboxKeyTargetID}
	var value func(ast.Expr) (string, bool)
	value = func(e ast.Expr) (string, bool) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				s, err := strconv.Unquote(v.Value)
				return s, err == nil
			}
		case *ast.Ident:
			s, ok := values[v.Name]
			return s, ok
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				a, ok := value(v.X)
				b, ok2 := value(v.Y)
				return a + b, ok && ok2
			}
		case *ast.CallExpr:
			if fn, ok := v.Fun.(*ast.Ident); ok && fn.Name == "chatscaleTimelineSQL" && len(v.Args) == 2 {
				a, ok := value(v.Args[0])
				b, ok2 := value(v.Args[1])
				return chatscaleTimelineSQL(a, b), ok && ok2
			}
		}
		return "", false
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "readConversationEventPage" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			a, ok := n.(*ast.AssignStmt)
			if !ok || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
				return true
			}
			name, ok := a.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			if s, ok := value(a.Rhs[0]); ok {
				values[name.Name] = s
			}
			return true
		})
	}
	if values["query"] == "" {
		t.Fatal("cannot reconstruct production conversation event query")
	}
	return values["query"]
}

func chatscaleReads(t *testing.T, posts int, before bool) []chatscaleRead {
	t.Helper()
	common := []any{chatscaleTenant, chatscaleRoom, chatscaleTenant, chatscaleReader}
	first := append(append([]any{}, common...), int64(math.MaxInt64), 51)
	deep := append(append([]any{}, common...), int64(posts*3/5/8/4), 51)
	reactions := chatscaleLiteral(t, "contracts_adapter.go", "ListReactions", "Query")
	pins := chatscaleLiteral(t, "contracts_adapter.go", "ListPins", "Query")
	counts := chatscaleLegacyCounts
	search := chatscaleLiteral(t, "contracts_adapter.go", "Search", "Query")
	reads := []chatscaleRead{
		{"conversation-list", listConversationsJoined, []any{chatscaleTenant, chatscaleReader, chatscaleTenant, "", 201}},
		{"unread-and-mentions", counts, append(append([]any{}, common...), countScanLimit)},
		{"first-page", listPostsBackward, first},
		{"deep-page", listPostsBackward, deep},
		{"thread", listPostsSelect + ` AND (p.id=$5 OR p.parent_id=$5) ORDER BY p.sequence LIMIT $6`, append(append([]any{}, common...), "chatscale-p-0000000001", 51)},
		{"reactions", reactions, []any{chatscaleTenant, chatscaleRoom, "chatscale-p-0000000042", chatscaleTenant, chatscaleReader, "", "", "", 201}},
		{"pins", pins, []any{chatscaleTenant, chatscaleRoom}},
		{"saved-list", `SELECT ` + chatsaveColumns + ` FROM chat_saved_item WHERE tenant_id=$1 AND home_tenant_id=$1 AND person_id=$2 ORDER BY created_at DESC,host_tenant_id DESC,conversation_id DESC,post_id DESC LIMIT 51`, []any{chatscaleTenant, chatscaleReader}},
		{"legacy-search", search, []any{chatscaleTenant, chatscaleReader, chatscaleTenant, "", "", "payroll", time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), "", 51}},
		{"outbox-drain", chatscaleLiteral(t, "store.go", "PendingOutboxAfter", "Query"), []any{chatscaleTenant, 500, int64(posts * 4 / 5)}},
	}
	if before {
		reads[0].query = strings.Replace(reads[0].query, lastActivitySubquery, `(SELECT max(lp.created_at) FROM chat_post lp WHERE lp.tenant_id=c.tenant_id AND lp.conversation_id=c.id AND lp.tombstoned=false)`, 1)
		reads[6].query = chatscaleLegacyPinsSQL
		reads[9].query = strings.Replace(reads[9].query, " AND r.outbox_id>$3", "", 1)
	} else {
		reads[1].query = chatscaleCachedSQL
		reads[1].args = []any{chatscaleTenant, chatscaleTenant, chatscaleReader, []string{chatscaleRoom}, countScanLimit}
	}
	metadata := getConversationRow
	if before {
		metadata = strings.Replace(metadata, lastActivitySubquery, `(SELECT max(lp.created_at) FROM chat_post lp WHERE lp.tenant_id=c.tenant_id AND lp.conversation_id=c.id AND lp.tombstoned=false)`, 1)
	}
	reads = append(reads,
		chatscaleRead{"conversation-metadata", metadata, []any{chatscaleTenant, chatscaleRoom}},
		chatscaleRead{"memberships", chatscaleLiteral(t, "contracts_adapter.go", "ListMemberships", "Query"), []any{chatscaleTenant, chatscaleRoom, "", "", 201}},
		chatscaleRead{"conversation-events", chatscaleEventSQL(t), []any{chatscaleTenant, chatscaleRoom, int64(posts * 3 / 5 / 2), 100, false, chatscaleTenant, chatscaleReader, "FULL_HISTORY", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}},
	)
	if before {
		events := &reads[len(reads)-1].query
		*events = strings.Replace(*events, "FROM ((", "FROM (", 1)
		*events = strings.Replace(*events, " ORDER BY id LIMIT $4) UNION ALL (", " UNION ALL ", 1)
		*events = strings.Replace(*events, " ORDER BY id LIMIT $4)) AS timeline", ") AS timeline", 1)
	}
	return reads
}

type chatscaleNode struct {
	Kind     string          `json:"Node Type"`
	Relation string          `json:"Relation Name"`
	Rows     float64         `json:"Actual Rows"`
	Loops    float64         `json:"Actual Loops"`
	Removed  float64         `json:"Rows Removed by Filter"`
	Shared   int             `json:"Shared Hit Blocks"`
	Reads    int             `json:"Shared Read Blocks"`
	Children []chatscaleNode `json:"Plans"`
}

func (n chatscaleNode) scannedRows() float64 {
	var rows float64
	if n.Relation != "" {
		rows = (n.Rows + n.Removed) * n.Loops
	}
	for _, child := range n.Children {
		rows += child.scannedRows()
	}
	return rows
}

type chatscalePlan struct {
	Execution float64       `json:"Execution Time"`
	Plan      chatscaleNode `json:"Plan"`
}

type chatscaleMetric struct {
	Phase               string          `json:"phase"`
	Read                string          `json:"read"`
	SQL                 string          `json:"sql"`
	Args                []any           `json:"args"`
	P50                 float64         `json:"p50_ms"`
	P95                 float64         `json:"p95_ms"`
	P99                 float64         `json:"p99_ms"`
	Samples             int             `json:"samples"`
	RowsReadPerReturned float64         `json:"rows_read_per_returned"`
	Plan                json.RawMessage `json:"explain_analyze_buffers"`
}

func chatscalePercentile(samples []float64, p float64) float64 {
	sort.Float64s(samples)
	return samples[max(0, int(math.Ceil(float64(len(samples))*p))-1)]
}

func chatscaleMeasure(t *testing.T, s *Store, label string, reads []chatscaleRead) []chatscaleMetric {
	t.Helper()
	var metrics []chatscaleMetric
	for _, read := range reads {
		t.Run(label+"/"+read.name, func(t *testing.T) {
			var samples []float64
			metric := chatscaleMetric{Phase: label, Read: read.name, SQL: read.query, Args: read.args}
			err := s.RunTenantTx(context.Background(), chatscaleTenant, func(tx dbport.Tx) error {
				if _, err := tx.Exec(context.Background(), `SELECT set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, chatscaleTenant, chatscaleReader); err != nil {
					return err
				}
				for i := 0; i < 21; i++ {
					var raw string
					if err := tx.QueryRow(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+read.query, read.args...).Scan(&raw); err != nil {
						return err
					}
					var plans []chatscalePlan
					if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
						return fmt.Errorf("invalid explain: %v", err)
					}
					if i == 1 {
						t.Logf("SQL %s\n%s\nARGS %v\nPLAN %s", read.name, read.query, read.args, raw)
						metric.Plan = json.RawMessage(raw)
						metric.RowsReadPerReturned = plans[0].Plan.scannedRows() / math.Max(1, plans[0].Plan.Rows)
					}
					if i > 0 {
						samples = append(samples, plans[0].Execution)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			metric.P50 = chatscalePercentile(samples, .5)
			metric.P95 = chatscalePercentile(samples, .95)
			metric.P99 = chatscalePercentile(samples, .99)
			metric.Samples = len(samples)
			metrics = append(metrics, metric)
			t.Logf("TIMING %s %s p50=%.3fms p95=%.3fms p99=%.3fms samples=%d rows-read/returned=%.2f", label, read.name, metric.P50, metric.P95, metric.P99, metric.Samples, metric.RowsReadPerReturned)
			if label == "after" && (read.name == "first-page" || read.name == "deep-page") && metric.P95 >= 50 {
				t.Fatalf("history p95 %.3fms exceeds 50ms", metric.P95)
			}
		})
	}
	return metrics
}

func chatscaleIndexes(t *testing.T, s *Store, create bool) {
	t.Helper()
	raw, err := Migrations.ReadFile("migrations/00034_chatscale_hot_reads.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(string(raw), "-- +goose Down")[0]
	for _, statement := range strings.Split(up, ";") {
		start := strings.Index(statement, "CREATE INDEX CONCURRENTLY ")
		if start < 0 {
			continue
		}
		sql := strings.TrimSpace(statement[start:])
		name := strings.Fields(sql)[3]
		if create {
			sql = strings.Replace(sql, "CONCURRENTLY ", "", 1)
		} else {
			sql = "DROP INDEX " + name
		}
		if err := s.RunTx(context.Background(), func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), sql); return err }); err != nil {
			t.Fatal(err)
		}
	}
	sql := "DROP INDEX chatscale_receipt_parent"
	if create {
		sql = "CREATE INDEX chatscale_receipt_parent ON chat_outbox_receipt(outbox_id)"
	}
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), sql); return err }); err != nil {
		t.Fatal(err)
	}
	sql = "DROP INDEX chatscale_post_visibility"
	if create {
		sql = "CREATE INDEX chatscale_post_visibility ON chat_post(tenant_id,conversation_id,id) WHERE NOT tombstoned"
	}
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), sql); return err }); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CHATSCALE_001_Performance(t *testing.T) {
	s, resumed := chatscaleVolumeFixture(t)
	if !resumed {
		chatscaleSeed(t, s, chatscaleSize())
	}
	vacuumMS := chatscaleVacuum(t, s)
	// Only this test's fresh schema is changed; no review database is used.
	chatscaleIndexes(t, s, false)
	metrics := chatscaleMeasure(t, s, "before", chatscaleReads(t, chatscaleSize(), true))
	metrics = append(metrics, chatscaleRetentionPlan(t, s, "before"))
	chatscaleIndexes(t, s, true)
	metrics = append(metrics, chatscaleMeasure(t, s, "after", chatscaleReads(t, chatscaleSize(), false))...)
	metrics = append(metrics, chatscaleRetentionPlan(t, s, "after"))
	// The extension search is the served source; measure it as well as the
	// legacy service read. Its authority is deterministic and incurs no network.
	ctx := context.Background()
	q := chatsearch.Request{Actor: chatsearch.Actor{TenantID: chatscaleTenant, HomeTenantID: chatscaleTenant, PersonID: chatscaleReader}, Query: "payroll", At: time.Now(), Limit: 20}
	authority := func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }
	start := time.Now()
	rows, err := s.SearchChatContent(ctx, q, chatsearch.Message, authority, "")
	if err != nil || len(rows) < 20 || len(rows) > 21 {
		t.Fatalf("served search rows=%d err=%v", len(rows), err)
	}
	t.Logf("STORE served-search %.3fms rows=%d", float64(time.Since(start).Microseconds())/1000, len(rows))
	a := NewAdapter(s)
	p := chat.Principal{TenantID: chatscaleTenant, SubjectID: chatscaleReader}
	start = time.Now()
	page, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 50}, chat.PostWindow{Descending: true})
	if err != nil || len(page.Posts) != 50 || page.NextCursor == "" {
		t.Fatalf("first page: rows=%d cursor=%q err=%v", len(page.Posts), page.NextCursor, err)
	}
	t.Logf("STORE first-page %.3fms rows=%d", float64(time.Since(start).Microseconds())/1000, len(page.Posts))
	start = time.Now()
	counts, err := NewRecipientStateStore(s).Counts(ctx, chatrecipient.Identity{HostTenantID: chatscaleTenant, HomeTenantID: chatscaleTenant, SubjectID: chatscaleReader, ConversationID: chatscaleRoom})
	if err != nil || counts.Unread == 0 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	t.Logf("STORE counts %.3fms unread=%d mentions=%d", float64(time.Since(start).Microseconds())/1000, counts.Unread, counts.Mentions)
	metrics = append(metrics, chatscaleSearchPlan(t, s, chatsearch.Message), chatscaleSearchPlan(t, s, chatsearch.Thread))
	chatscaleHistoryBudget(t, s)
	chatscaleSidebarBudget(t, s)
	workload := chatscaleWorkload(t, s, fmt.Sprintf("indexed-%d", time.Now().UnixNano()))
	if *chatscaleReportPath != "" {
		data, err := json.MarshalIndent(struct {
			Posts         int                     `json:"posts"`
			Conversations int                     `json:"conversations"`
			Members       int                     `json:"distinct_members"`
			VacuumMS      float64                 `json:"fixture_vacuum_ms"`
			Metrics       []chatscaleMetric       `json:"metrics"`
			Relations     []chatscaleRelation     `json:"relations"`
			Workload      chatscaleWorkloadResult `json:"workload"`
			Limits        []string                `json:"limits"`
		}{chatscaleSize(), 200, 2000, vacuumMS, metrics, chatscaleRelations(t, s), workload, []string{
			"One tenant; this lane's target is two million posts. This does not prove the planning target of 500 million posts across 200 tenants.",
			"Twenty measured warm executions after one warmup; p99 is the maximum of these twenty samples. Server execution time excludes transport and authorization outside the store.",
			"Before drops only chatscale indexes in the isolated fixture, uses the original activity/count/outbox SQL, and retains all pre-existing indexes. After includes cached heads and counts.",
			"Dead/live tuple and vacuum counts are PostgreSQL statistics estimates, not a measured long-running autovacuum experiment.",
			"An explicit VACUUM ANALYZE of posts, pins and reactions precedes both phases. Its elapsed time is recorded; it models maintained history rather than an empty post-load visibility map.",
			"Native send timing includes pool waits and sequence fences. The separate contention test uses 1000 senders; no per-index write-amplification experiment is claimed here.",
			"Search authority is deterministic in this fixture; the separate search lane owns production text matching and its index.",
			"Served message/thread search plans have one ANALYZE execution each; their p50/p95/p99 fields repeat that single observation and are not percentile estimates.",
			"Retention has one ANALYZE execution per phase including cascade-trigger time; both phases roll back the same 500-event deletion. Its repeated percentile fields are not percentile estimates.",
			"The seeded shape has 200 conversations. Native sidebar-budget checks add 100 empty conversations; mixed writes run after read measurements and budget checks.",
		}}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*chatscaleReportPath, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_CHATSCALE_001_Golden(t *testing.T) {
	if got := chatscalePercentile([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, .95); got != 19 {
		t.Fatalf("nearest-rank p95=%v want 19", got)
	}
	for _, sql := range []string{listPostsForward, listPostsBackward} {
		if strings.Contains(strings.ToUpper(sql), "OFFSET") || strings.Contains(strings.ToUpper(sql), "COUNT(") || !strings.Contains(sql, "p.sequence") {
			t.Fatalf("history read lost its bounded sequence keyset: %s", sql)
		}
	}
	got := listPostsBackward[strings.Index(listPostsBackward, " FROM "):]
	want := ` FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND p.sequence<$5 ORDER BY p.sequence DESC LIMIT $6`
	if got != want {
		t.Fatalf("history boundary SQL golden changed:\n%q\nwant:\n%q", got, want)
	}
}
