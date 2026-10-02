package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var c001Rollback = errors.New("chatscale001: roll back the measurement")

// c001Stmt is one statement the product issues, with the arguments it would
// bind. Prepare runs inside the measurement transaction before the EXPLAIN, to
// put the data in the state the statement is meant to meet; the transaction is
// always rolled back.
type c001Stmt struct {
	Group, Name string
	SQL         string
	Args        []any
	Prepare     func(ctx context.Context, tx dbport.Tx) error
}

type c001Result struct {
	Phase     string  `json:"phase"`
	Group     string  `json:"group"`
	Name      string  `json:"name"`
	MedianMS  float64 `json:"median_ms"`
	MaxMS     float64 `json:"max_ms"`
	PlanMS    float64 `json:"planning_ms"`
	TriggerMS float64 `json:"trigger_ms"`
	Rows      int64   `json:"rows_returned"`
	Scanned   int64   `json:"rows_scanned"`
	Hit       int64   `json:"buffers_hit"`
	Read      int64   `json:"buffers_read"`
	Dirtied   int64   `json:"buffers_dirtied"`
	Shape     string  `json:"plan_shape"`
	Samples   int     `json:"samples"`
	Error     string  `json:"error,omitempty"`
}

type c001Size struct {
	Phase      string  `json:"phase"`
	Table      string  `json:"table"`
	Rows       int64   `json:"rows_estimate"`
	HeapMB     float64 `json:"heap_mb"`
	IndexMB    float64 `json:"index_mb"`
	TotalMB    float64 `json:"total_mb"`
	Partitions int     `json:"partitions"`
}

type c001IndexSize struct {
	Phase string  `json:"phase"`
	Index string  `json:"index"`
	MB    float64 `json:"mb"`
}

type c001Writes struct {
	Phase string  `json:"phase"`
	Sends int     `json:"sends"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	Max   float64 `json:"max_ms"`
}

type c001Report struct {
	Date       string          `json:"date"`
	Stages     []int           `json:"stages"`
	Channels   int             `json:"channels"`
	Members    int             `json:"members"`
	LoadSecond map[string]int  `json:"load_seconds"`
	Results    []c001Result    `json:"results"`
	Sizes      []c001Size      `json:"sizes"`
	Indexes    []c001IndexSize `json:"post_indexes"`
	Writes     []c001Writes    `json:"send_loop"`
	Notes      []string        `json:"notes"`
}

func c001Num(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func c001Str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func c001Children(node map[string]any) []map[string]any {
	var out []map[string]any
	list, _ := node["Plans"].([]any)
	for _, child := range list {
		if m, ok := child.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// c001Shape writes a plan as one line: node, relation and index, children in
// parentheses. An Append over partitions shows how many were left after pruning.
func c001Shape(node map[string]any) string {
	kind := c001Str(node, "Node Type")
	label := kind
	if idx := c001Str(node, "Index Name"); idx != "" {
		label += " " + idx
	} else if rel := c001Str(node, "Relation Name"); rel != "" {
		label += " " + rel
	} else if cte := c001Str(node, "CTE Name"); cte != "" {
		label += " " + cte
	}
	children := c001Children(node)
	if kind == "Append" || kind == "Merge Append" {
		label += fmt.Sprintf("[%d scanned, %d pruned]", len(children), int(c001Num(node, "Subplans Removed")))
		if len(children) > 1 {
			children = children[:1]
		}
	}
	if len(children) == 0 {
		return label
	}
	parts := make([]string, 0, len(children))
	for _, child := range children {
		parts = append(parts, c001Shape(child))
	}
	return label + "(" + strings.Join(parts, ", ") + ")"
}

// c001Scanned counts the rows the scan nodes read: those returned plus those a
// filter removed, over every loop.
func c001Scanned(node map[string]any) int64 {
	var n float64
	if strings.Contains(c001Str(node, "Node Type"), "Scan") && c001Str(node, "Relation Name") != "" {
		n = (c001Num(node, "Actual Rows") + c001Num(node, "Rows Removed by Filter")) * c001Num(node, "Actual Loops")
	}
	for _, child := range c001Children(node) {
		n += float64(c001Scanned(child))
	}
	return int64(n)
}

func c001ParseExplain(raw string) (c001Result, error) {
	var docs []map[string]any
	if err := json.Unmarshal([]byte(raw), &docs); err != nil || len(docs) != 1 {
		return c001Result{}, fmt.Errorf("explain output: %v", err)
	}
	doc := docs[0]
	plan, _ := doc["Plan"].(map[string]any)
	if plan == nil {
		return c001Result{}, errors.New("explain output has no plan")
	}
	r := c001Result{
		MedianMS: c001Num(doc, "Execution Time"), PlanMS: c001Num(doc, "Planning Time"),
		Rows: int64(c001Num(plan, "Actual Rows")), Scanned: c001Scanned(plan),
		Hit: int64(c001Num(plan, "Shared Hit Blocks")), Read: int64(c001Num(plan, "Shared Read Blocks")),
		Dirtied: int64(c001Num(plan, "Shared Dirtied Blocks")), Shape: c001Shape(plan),
	}
	if triggers, ok := doc["Triggers"].([]any); ok {
		for _, item := range triggers {
			if m, ok := item.(map[string]any); ok {
				r.TriggerMS += c001Num(m, "Time")
			}
		}
	}
	return r, nil
}

// c001Measure runs a statement once to warm it, then up to samples times, each in
// a transaction that is rolled back. A statement slower than two seconds is
// sampled twice only, so one pathological read cannot hold the shared server.
func c001Measure(t *testing.T, s *Store, phase string, stmt c001Stmt, samples int) c001Result {
	t.Helper()
	ctx := context.Background()
	res := c001Result{Phase: phase, Group: stmt.Group, Name: stmt.Name}
	var runs []c001Result
	for i := 0; i <= samples; i++ {
		var one c001Result
		err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='90s'`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, chatscaleTenant, chatscaleReader); err != nil {
				return err
			}
			if stmt.Prepare != nil {
				if err := stmt.Prepare(ctx, tx); err != nil {
					return err
				}
			}
			var raw string
			if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+stmt.SQL, stmt.Args...).Scan(&raw); err != nil {
				return err
			}
			var perr error
			one, perr = c001ParseExplain(raw)
			if perr != nil {
				return perr
			}
			return c001Rollback
		})
		if err != nil && !errors.Is(err, c001Rollback) {
			res.Error = err.Error()
			return res
		}
		if i == 0 {
			if one.MedianMS > 2000 && samples > 2 {
				samples = 2
			}
			continue
		}
		runs = append(runs, one)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].MedianMS < runs[j].MedianMS })
	mid := runs[len(runs)/2]
	res.MedianMS, res.MaxMS = mid.MedianMS, runs[len(runs)-1].MedianMS
	res.PlanMS, res.TriggerMS, res.Rows, res.Scanned = mid.PlanMS, mid.TriggerMS, mid.Rows, mid.Scanned
	res.Hit, res.Read, res.Dirtied, res.Shape, res.Samples = mid.Hit, mid.Read, mid.Dirtied, mid.Shape, len(runs)
	return res
}

func c001Scalar[T any](t *testing.T, s *Store, sql string, args ...any) T {
	t.Helper()
	var out T
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, sql, args...).Scan(&out)
	}); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	return out
}

// c001Statements builds the hot statements from the store's own SQL: the
// constants and literals are read from the package and from the Go source, so
// the tool cannot drift from what the server issues.
func c001Statements(t *testing.T, s *Store, channels int) []c001Stmt {
	t.Helper()
	hot, backlog, small := c001Channel(0), c001Channel(7), c001Channel(channels-20)
	hotMax := c001Scalar[int64](t, s, `SELECT post_sequence FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, chatscaleTenant, hot)
	smallMax := c001Scalar[int64](t, s, `SELECT post_sequence FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, chatscaleTenant, small)
	root := c001Scalar[string](t, s, `SELECT parent_id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND parent_id<>'' ORDER BY sequence DESC LIMIT 1`, chatscaleTenant, hot)
	reacted := c001Scalar[string](t, s, `SELECT id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND sequence=$3`, chatscaleTenant, hot, hotMax/2/7*7)
	modAt := c001Scalar[time.Time](t, s, `SELECT updated_at FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND sequence=$3`, chatscaleTenant, hot, hotMax/2)
	modBy := c001Scalar[string](t, s, `SELECT author_id FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND sequence=$3`, chatscaleTenant, hot, hotMax/2)
	afterOutbox := c001Scalar[int64](t, s, `SELECT COALESCE(max(outbox_id),0) FROM chat_outbox_receipt WHERE tenant_id=$1`, chatscaleTenant)
	all := make([]string, channels)
	for i := range all {
		all[i] = c001Channel(i)
	}
	prior := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	common := func(room string) []any { return []any{chatscaleTenant, room, chatscaleTenant, chatscaleReader} }
	with := func(base []any, extra ...any) []any { return append(append([]any{}, base...), extra...) }
	search := chatscaleLiteral(t, "contracts_adapter.go", "Search", "Query")
	sidebarArgs := []any{chatscaleTenant, chatscaleTenant, chatscaleReader, all, countScanLimit}
	sidebarCold := func(ctx context.Context, tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chatscale_read_state WHERE tenant_id=$1`, chatscaleTenant)
		return err
	}
	// Ten busy channels each take three sends after the badges were last
	// computed, through the same fence and insert the send path uses, so the
	// triggers of the schema under test decide what the badges must do.
	sidebarAfterSends := func(ctx context.Context, tx dbport.Tx) error {
		for _, room := range all[:10] {
			for k := 0; k < 3; k++ {
				var ev, rev, seq int64
				if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence,settings_revision,post_sequence`, chatscaleTenant, room).Scan(&ev, &rev, &seq); err != nil {
					return err
				}
				if err := tx.QueryRow(ctx, chatscaleInsertPostSQL, fmt.Sprintf("chatscale-burst-%s-%d-%d", room, k, time.Now().UnixNano()), chatscaleTenant, room, "chatscale-u-1", chatscaleTenant, "Payroll review: burst", "", []byte("[]"), nil, "", seq, nil).Scan(new(string), new(string), new(string), new(string), new(string), new(int64), new(string), new(int64), new(bool), new(time.Time), new(time.Time), new(string), new([]byte), new([]byte)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	rewritten := strings.Replace(search, "($4='' OR p.conversation_id=$4)", "($4='' OR p.conversation_id||''=$4)", 1)
	capped := []any{chatscaleTenant, chatscaleTenant, chatscaleReader, all, 100}
	sidebarAfter := c001Stmt{Group: "sidebar", Name: "counts, ten busy channels took three sends each", SQL: chatscaleCachedSQL, Args: sidebarArgs, Prepare: sidebarAfterSends}
	return []c001Stmt{
		{Group: "history", Name: "open hot channel", SQL: listPostsBackward, Args: with(common(hot), int64(math.MaxInt64), 51)},
		{Group: "history", Name: "page back, middle of hot channel", SQL: listPostsBackward, Args: with(common(hot), hotMax/2, 51)},
		{Group: "history", Name: "page back, start of hot channel", SQL: listPostsBackward, Args: with(common(hot), int64(60), 51)},
		{Group: "history", Name: "open small channel", SQL: listPostsBackward, Args: with(common(small), smallMax+1, 51)},
		{Group: "history", Name: "forward from middle (around a message)", SQL: listPostsForward, Args: with(common(hot), hotMax/2, 51)},
		{Group: "thread", Name: "thread read", SQL: listPostsSelect + ` AND (p.id=$5 OR p.parent_id=$5) ORDER BY p.sequence LIMIT $6`, Args: with(common(hot), root, 51)},
		{Group: "unread", Name: "rebuild one channel (served path), caught up", SQL: chatscaleRebuildSQL, Args: []any{chatscaleTenant, chatscaleTenant, chatscaleReader, []string{hot}, countScanLimit}},
		{Group: "unread", Name: "rebuild one channel (served path), backlog over the limit", SQL: chatscaleRebuildSQL, Args: []any{chatscaleTenant, chatscaleTenant, chatscaleReader, []string{backlog}, countScanLimit}},
		{Group: "unread", Name: "legacy per-channel scan (oracle), caught up", SQL: chatscaleLegacyCounts, Args: with(common(hot), countScanLimit)},
		{Group: "sidebar", Name: "conversation list, person in every channel", SQL: listConversationsJoined, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, "", channels + 1}},
		{Group: "sidebar", Name: "counts, every row cached and current", SQL: chatscaleCachedSQL, Args: sidebarArgs},
		sidebarAfter,
		{Group: "sidebar", Name: "counts, nothing cached (first open)", SQL: chatscaleCachedSQL, Args: sidebarArgs, Prepare: sidebarCold},
		{Group: "sidebar", Name: "counts, nothing cached, display cap 100 not 5000", SQL: chatscaleCachedSQL, Args: capped, Prepare: sidebarCold},
		{Group: "readstate", Name: "mark read: highest visible sequence", SQL: `SELECT COALESCE((SELECT max(p.sequence) FROM chat_post p WHERE p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))),0) FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' FOR UPDATE OF m`, Args: common(hot)},
		{Group: "readstate", Name: "mark read: cursor advance", SQL: chatReadAdvanceSQL, Args: []any{hotMax, chatscaleTenant, hot, chatscaleTenant, chatscaleReader, int64(1)}},
		{Group: "send", Name: "sequence fence on the conversation row", SQL: `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence,settings_revision,post_sequence`, Args: []any{chatscaleTenant, hot}},
		{Group: "send", Name: "insert post and revision (with triggers)", SQL: chatscaleInsertPostSQL, Args: []any{"chatscale-send-probe", chatscaleTenant, hot, "chatscale-u-1", chatscaleTenant, "probe", "", []byte("[]"), nil, "", hotMax + 1, nil}},
		{Group: "search", Name: "common term, all channels", SQL: search, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, "", "", "payroll", prior, "", 51}},
		{Group: "search", Name: "rare term (1 in 997), all channels", SQL: search, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, "", "", "zebrafish", prior, "", 51}},
		{Group: "search", Name: "rare term, one channel", SQL: search, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, hot, "", "zebrafish", prior, "", 51}},
		{Group: "search", Name: "rare term, one channel, rewritten (channel test not indexable)", SQL: rewritten, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, hot, "", "zebrafish", prior, "", 51}},
		{Group: "search", Name: "common term, one channel", SQL: search, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, hot, "", "payroll", prior, "", 51}},
		{Group: "search", Name: "common term, one channel, rewritten (channel test not indexable)", SQL: rewritten, Args: []any{chatscaleTenant, chatscaleReader, chatscaleTenant, hot, "", "payroll", prior, "", 51}},
		{Group: "reactions", Name: "reactions on a post", SQL: chatscaleLiteral(t, "contracts_adapter.go", "ListReactions", "Query"), Args: []any{chatscaleTenant, hot, reacted, chatscaleTenant, chatscaleReader, "", "", "", 201}},
		{Group: "pins", Name: "pins in a channel", SQL: chatscaleLiteral(t, "contracts_adapter.go", "ListPins", "Query"), Args: []any{chatscaleTenant, hot}},
		{Group: "members", Name: "member list of a full channel", SQL: chatscaleLiteral(t, "contracts_adapter.go", "ListMemberships", "Query"), Args: []any{chatscaleTenant, hot, "", "", 201}},
		{Group: "events", Name: "conversation event page", SQL: chatscaleEventSQL(t), Args: []any{chatscaleTenant, hot, hotMax / 2, 100, false, chatscaleTenant, chatscaleReader, "FULL_HISTORY", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}},
		// The moderation queue finds the post behind a filter hit by author and time
		// (chatmod005_queue.go, filterHitSelect); chat_post_touched serves it.
		{Group: "moderation", Name: "post behind a filter hit (author and updated_at window)", SQL: `SELECT p.id FROM chat_post p WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.author_id=$3
 AND p.updated_at>=$4::timestamptz-interval '5 seconds' AND p.updated_at<$4::timestamptz+interval '2 minutes'
 ORDER BY abs(extract(epoch FROM p.updated_at-$4::timestamptz)),p.sequence LIMIT 1`, Args: []any{chatscaleTenant, hot, modBy, modAt}},
		{Group: "outbox", Name: "drain a batch of 500 undelivered events", SQL: chatscaleLiteral(t, "store.go", "PendingOutboxAfter", "Query"), Args: []any{chatscaleTenant, 500, afterOutbox}},
	}
}

func c001Sizes(t *testing.T, s *Store, phase string, report *c001Report) {
	t.Helper()
	ctx := context.Background()
	err := s.RunTx(ctx, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `WITH leaf AS (
 SELECT c.oid AS root,c.oid AS relid FROM pg_class c WHERE c.relkind='r'
 UNION ALL SELECT c.oid,t.relid FROM pg_class c,LATERAL pg_partition_tree(c.oid) t WHERE c.relkind='p' AND t.isleaf)
 SELECT c.relname,COALESCE(sum(pg_relation_size(l.relid)),0)::bigint,COALESCE(sum(pg_indexes_size(l.relid)),0)::bigint,
 COALESCE(sum(pg_total_relation_size(l.relid)),0)::bigint,COALESCE(sum((SELECT reltuples FROM pg_class x WHERE x.oid=l.relid)),0)::bigint,
 CASE WHEN c.relkind='p' THEN count(*) ELSE 0 END::int
 FROM pg_class c JOIN leaf l ON l.root=c.oid WHERE c.relnamespace=current_schema()::regnamespace AND NOT c.relispartition
 AND c.relname IN ('chat_post','chat_post_revision','chat_reaction','chat_pin','chat_cursor','chat_membership','chat_conversation','chat_outbox','chat_outbox_receipt','chatscale_read_state')
 GROUP BY c.relname,c.relkind ORDER BY 4 DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var z c001Size
			var heap, idx, total int64
			z.Phase = phase
			if err := rows.Scan(&z.Table, &heap, &idx, &total, &z.Rows, &z.Partitions); err != nil {
				return err
			}
			z.HeapMB, z.IndexMB, z.TotalMB = float64(heap)/1048576, float64(idx)/1048576, float64(total)/1048576
			report.Sizes = append(report.Sizes, z)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows2, err := tx.Query(ctx, `WITH leaf AS (
 SELECT ic.oid AS root,ic.oid AS relid FROM pg_class ic WHERE ic.relkind='i'
 UNION ALL SELECT ic.oid,t.relid FROM pg_class ic,LATERAL pg_partition_tree(ic.oid) t WHERE ic.relkind='I' AND t.isleaf)
 SELECT ic.relname,COALESCE(sum(pg_relation_size(l.relid)),0)::bigint FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid
 JOIN leaf l ON l.root=ic.oid WHERE i.indrelid='chat_post'::regclass GROUP BY ic.relname ORDER BY 2 DESC`)
		if err != nil {
			return err
		}
		defer rows2.Close()
		for rows2.Next() {
			z := c001IndexSize{Phase: phase}
			var b int64
			if err := rows2.Scan(&z.Index, &b); err != nil {
				return err
			}
			z.MB = float64(b) / 1048576
			report.Indexes = append(report.Indexes, z)
		}
		return rows2.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// c001SendLoop commits n real sends one at a time (sequence fence, then post and
// revision, with every trigger), a hundred into the busiest channel and the rest
// spread over small ones, and reports the wall time per send. It is the write
// cost of the indexes the table carries; the posts stay in the schema.
func c001SendLoop(t *testing.T, s *Store, phase string, n, channels int) c001Writes {
	t.Helper()
	ctx := context.Background()
	var ms []float64
	for i := 0; i < n; i++ {
		room := c001Channel(0)
		if i%2 == 1 {
			room = c001Channel(channels/2 + i%(channels/2))
		}
		start := time.Now()
		err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			var ev, rev, seq int64
			if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence,settings_revision,post_sequence`, chatscaleTenant, room).Scan(&ev, &rev, &seq); err != nil {
				return err
			}
			var id string
			return tx.QueryRow(ctx, chatscaleInsertPostSQL, fmt.Sprintf("chatscale-send-%s-%d", phase, i), chatscaleTenant, room, "chatscale-u-1", chatscaleTenant, "Payroll review: a new message", "", []byte("[]"), nil, "", seq, nil).Scan(&id, new(string), new(string), new(string), new(string), new(int64), new(string), new(int64), new(bool), new(time.Time), new(time.Time), new(string), new([]byte), new([]byte))
		})
		if err != nil {
			t.Fatalf("send loop: %v", err)
		}
		ms = append(ms, float64(time.Since(start).Microseconds())/1000)
	}
	sort.Float64s(ms)
	return c001Writes{Phase: phase, Sends: n, P50: ms[len(ms)/2], P95: ms[len(ms)*95/100], Max: ms[len(ms)-1]}
}

// c001ApplyCandidate runs a candidate migration file in the measurement schema
// only. Statements are separated by lines holding "-- @@" and run in one
// transaction, so a failure leaves the schema as it was.
func c001ApplyCandidate(t *testing.T, s *Store, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	start := time.Now()
	err = s.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL work_mem='128MB'`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL maintenance_work_mem='256MB'`); err != nil {
			return err
		}
		for i, stmt := range strings.Split(string(raw), "-- @@") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("statement %d: %w", i, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("candidate %s: %v", filepath.Base(path), err)
	}
	t.Logf("candidate %s applied in %.1fs", filepath.Base(path), time.Since(start).Seconds())
}

func c001Print(t *testing.T, results []c001Result) {
	t.Helper()
	for _, r := range results {
		if r.Error != "" {
			t.Logf("S7ROW | %s | %s / %s | ERROR %s", r.Phase, r.Group, r.Name, r.Error)
			continue
		}
		t.Logf("S7ROW | %s | %s / %s | med %.2f ms | max %.2f ms | plan %.2f ms | trig %.2f ms | rows %d | scanned %d | hit %d | read %d | dirtied %d | %s",
			r.Phase, r.Group, r.Name, r.MedianMS, r.MaxMS, r.PlanMS, r.TriggerMS, r.Rows, r.Scanned, r.Hit, r.Read, r.Dirtied, r.Shape)
	}
}

func c001Phase(t *testing.T, s *Store, phase string, channels, samples int, report *c001Report) {
	t.Helper()
	c001Maintain(t, s)
	c001VerifyCounts(t, s, channels)
	c001ExplainCold(t, s, channels)
	c001ExplainTriggers(t, s, channels)
	// Warm the badge rows the way a first sidebar open does, so "every row
	// cached" measures a hit and not the rebuild.
	all := make([]string, channels)
	for i := range all {
		all[i] = c001Channel(i)
	}
	if _, err := NewRecipientStateStore(s).ChatscaleSidebarCounts(context.Background(), chatscaleTenant, chatscaleTenant, chatscaleReader, all); err != nil {
		t.Fatal(err)
	}
	var results []c001Result
	for _, stmt := range c001Statements(t, s, channels) {
		results = append(results, c001Measure(t, s, phase, stmt, samples))
	}
	report.Results = append(report.Results, results...)
	c001Print(t, results)
	c001Sizes(t, s, phase, report)
	report.Writes = append(report.Writes, c001SendLoop(t, s, phase, c001EnvInt("CHATSCALE001_SENDS", 100), channels))
	w := report.Writes[len(report.Writes)-1]
	t.Logf("S7SEND | %s | %d sends | p50 %.2f ms | p95 %.2f ms | max %.2f ms", phase, w.Sends, w.P50, w.P95, w.Max)
}

func TestChatscale001_Volume(t *testing.T) {
	if os.Getenv("HCMNEXT_CHATSCALE001") == "" {
		t.Skip("set HCMNEXT_CHATSCALE001=1 to load the CHATSCALE-001 volume and measure it")
	}
	channels, members, samples := c001EnvInt("CHATSCALE001_CHANNELS", 300), c001EnvInt("CHATSCALE001_MEMBERS", 2000), c001EnvInt("CHATSCALE001_SAMPLES", 5)
	stages := c001Stages(t)
	if channels > 300 || channels < 30 {
		t.Fatal("CHATSCALE001_CHANNELS must be 30 to 300: one sidebar read serves at most 300 channels")
	}
	s, schema := chatFixture(t)
	report := &c001Report{Date: time.Now().Format(time.RFC3339), Stages: stages, Channels: channels, Members: members, LoadSecond: map[string]int{}}
	write := func() {
		out := c001Env("CHATSCALE001_OUT", filepath.Join("..", "..", "..", ".artifacts", "tmp", "S7", "c001-report.json"))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Logf("report directory: %v", err)
			return
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(out, data, 0o600); err != nil {
			t.Logf("report file: %v", err)
		}
	}
	defer write()
	t.Logf("S7SCHEMA %s", schema)
	c001Run(t, s, `ALTER TABLE chat_post DISABLE TRIGGER chatstate_post_fence`)
	c001SeedStatic(t, s, channels, members)
	previous := 0
	for _, stage := range stages {
		start := time.Now()
		c001SeedPosts(t, s, previous, stage, channels, members, 50000)
		report.LoadSecond[fmt.Sprint(stage)] = int(time.Since(start).Seconds())
		t.Logf("S7LOAD | %d posts loaded in %.0fs", stage, time.Since(start).Seconds())
		previous = stage
		c001Run(t, s, `ALTER TABLE chat_post ENABLE TRIGGER chatstate_post_fence`)
		c001Phase(t, s, fmt.Sprintf("%d posts", stage), channels, samples, report)
		c001Run(t, s, `ALTER TABLE chat_post DISABLE TRIGGER chatstate_post_fence`)
	}
	c001Run(t, s, `ALTER TABLE chat_post ENABLE TRIGGER chatstate_post_fence`)
	for _, path := range strings.Split(c001Env("CHATSCALE001_CANDIDATES", ""), ",") {
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		c001ApplyCandidate(t, s, path)
		c001Phase(t, s, fmt.Sprintf("%d posts + %s", previous, strings.TrimSuffix(filepath.Base(path), ".sql")), channels, samples, report)
	}
	report.Notes = []string{
		"One tenant, one connection at a time, warm cache, no concurrent writers; times are server execution time from EXPLAIN (ANALYZE, BUFFERS).",
		"The test database role is a superuser, so row-level security policies are not applied to these plans.",
		"Each statement runs once to warm it and then the stated number of times, each in a rolled-back transaction; the median run is reported.",
	}
}

// The plan summary must name the relation, the index and the pruning of a
// partitioned scan, because the report is read for exactly that.
func TestChatscale001_PlanShape(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Limit","Actual Rows":50,"Plans":[{"Node Type":"Append","Subplans Removed":15,"Plans":[
 {"Node Type":"Index Scan","Index Name":"chat_post_p3_tenant_id_conversation_id_sequence_idx","Relation Name":"chat_post_p3","Actual Rows":50,"Rows Removed by Filter":4,"Actual Loops":1}]}]},
 "Planning Time":0.2,"Execution Time":1.5,"Triggers":[{"Time":0.5},{"Time":0.25}]}]`
	got, err := c001ParseExplain(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Limit(Append[1 scanned, 15 pruned](Index Scan chat_post_p3_tenant_id_conversation_id_sequence_idx))"; got.Shape != want {
		t.Fatalf("shape %q want %q", got.Shape, want)
	}
	if got.Scanned != 54 || got.TriggerMS != 0.75 || got.MedianMS != 1.5 || got.Rows != 50 {
		t.Fatalf("parsed %+v", got)
	}
	if _, err := c001ParseExplain(`[]`); err == nil {
		t.Fatal("an empty explain document must be an error")
	}
}
