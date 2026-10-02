package chatload

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Relation struct {
	DeadBytes       int64  `json:"dead_tuple_bytes"`
	DeadBytesExact  bool   `json:"dead_bytes_exact"`
	DeadBytesNote   string `json:"dead_bytes_note,omitempty"`
	AutovacuumCount int64  `json:"autovacuum_count"`
	Name            string `json:"name"`
	TableBytes      int64  `json:"table_bytes"`
	IndexBytes      int64  `json:"index_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	LiveRows        int64  `json:"live_rows_estimate"`
	DeadRows        int64  `json:"dead_rows_estimate"`
}
type IndexCost struct {
	Table         string      `json:"table"`
	Baseline      Percentiles `json:"with_index"`
	Name          string      `json:"name"`
	Bytes         int64       `json:"bytes"`
	Timing        Percentiles `json:"insert_without_index"`
	MarginalP95MS float64     `json:"marginal_p95_ms"`
	Error         string      `json:"error,omitempty"`
}
type IndexSize struct {
	Table string `json:"table"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}
type SizeReport struct {
	Error         string        `json:"error,omitempty"`
	AllIndexes    []IndexSize   `json:"all_indexes"`
	Posts         int64         `json:"posts"`
	LoadSeconds   float64       `json:"load_seconds"`
	DatabaseBytes int64         `json:"database_bytes"`
	Queries       []Measurement `json:"queries"`
	Mixed         Workload      `json:"mixed"`
	Contention    Workload      `json:"send_contention"`
	Relations     []Relation    `json:"relations_before_vacuum"`
	AfterVacuum   []Relation    `json:"relations_after_vacuum"`
	VacuumMS      float64       `json:"vacuum_ms"`
	Indexes       []IndexCost   `json:"send_path_index_costs"`
	IndexedInsert Percentiles   `json:"indexed_insert"`
}
type Finding struct {
	Samples      int     `json:"completed_samples"`
	Error        string  `json:"error,omitempty"`
	LowerBoundMS float64 `json:"timeout_lower_bound_ms,omitempty"`
	Query        string  `json:"query"`
	P95MS        float64 `json:"p95_ms"`
	ReadRows     float64 `json:"scan_rows"`
	Proposal     string  `json:"proposal"`
	Reason       string  `json:"reason"`
}
type Report struct {
	GeneratorVersion string       `json:"generator_version"`
	SchemaSHA256     string       `json:"schema_sha256"`
	Seed             uint64       `json:"seed"`
	Tenants          int          `json:"tenants"`
	Channels         int          `json:"channels"`
	Repeats          int          `json:"repeats"`
	Concurrency      int          `json:"concurrency"`
	Fsync            string       `json:"fsync"`
	Server           string       `json:"server_version"`
	Limitations      []string     `json:"limitations"`
	Sizes            []SizeReport `json:"sizes"`
	Ranked           []Finding    `json:"ranked"`
}

func relations(ctx context.Context, conn *pgx.Conn) ([]Relation, error) {
	rows, e := conn.Query(ctx, `SELECT relname,pg_table_size(relid),pg_indexes_size(relid),pg_total_relation_size(relid),n_live_tup,n_dead_tup,autovacuum_count FROM pg_stat_user_tables WHERE relname LIKE 'chat_%' ORDER BY pg_total_relation_size(relid) DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Relation
	for rows.Next() {
		var r Relation
		if e = rows.Scan(&r.Name, &r.TableBytes, &r.IndexBytes, &r.TotalBytes, &r.LiveRows, &r.DeadRows, &r.AutovacuumCount); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	var available bool
	e = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name='pgstattuple')").Scan(&available)
	if e != nil {
		return nil, e
	}
	if available {
		_, e = conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgstattuple")
		available = e == nil
	}
	for i := range out {
		r := &out[i]
		if available {
			e = conn.QueryRow(ctx, "SELECT dead_tuple_len FROM pgstattuple($1::regclass)", pgx.Identifier{r.Name}.Sanitize()).Scan(&r.DeadBytes)
			r.DeadBytesExact = e == nil
		}
		if !r.DeadBytesExact {
			r.DeadBytesNote = "pgstattuple unavailable; approximate bytes from relation size and dead/live row estimates"
			if r.LiveRows+r.DeadRows > 0 {
				r.DeadBytes = r.TableBytes * r.DeadRows / (r.LiveRows + r.DeadRows)
			}
		}
	}
	return out, nil
}

// IndexCosts removes one index only inside a rolled-back transaction in the
// disposable database. It measures marginal insert executor cost, not causal
// committed-send throughput; WAL, caches and constraints remain confounders.
func IndexCosts(ctx context.Context, conn *pgx.Conn, repeats int) (Percentiles, []IndexCost, error) {
	if e := guardConnection(ctx, conn); e != nil {
		return Percentiles{}, nil, e
	}
	q := Query{SQL: `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json) VALUES('index-probe','t000','c000000','reader','t000',9000000000,'Payroll review onboarding benefits policy','[{"Kind":"PERSON_MENTION","TenantID":"t000","ID":"reader"}]') RETURNING id`}
	base := Measure(ctx, conn, q, channelName(0), repeats)
	if base.Error != "" {
		return base.Timing, nil, fmt.Errorf("index baseline: %s", base.Error)
	}
	rows, e := conn.Query(ctx, `SELECT t.relname,i.relname,pg_relation_size(i.oid),coalesce(c.conname,'') FROM pg_index x JOIN pg_class t ON t.oid=x.indrelid JOIN pg_class i ON i.oid=x.indexrelid LEFT JOIN pg_constraint c ON c.conindid=i.oid AND c.conrelid=x.indrelid AND c.contype IN ('p','u') WHERE t.relname IN ('chat_conversation','chat_post','chat_post_revision','chat_outbox','chat_audit_event','chat_record_inventory') ORDER BY t.relname,i.relname`)
	if e != nil {
		return base.Timing, nil, e
	}
	type index struct {
		table, name, con string
		bytes            int64
	}
	var indexes []index
	for rows.Next() {
		var i index
		if e = rows.Scan(&i.table, &i.name, &i.bytes, &i.con); e != nil {
			rows.Close()
			return base.Timing, nil, e
		}
		indexes = append(indexes, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return base.Timing, nil, e
	}
	var out []IndexCost
	for _, idx := range indexes {
		probe := indexProbe(idx.table, q)
		baseline := Measure(ctx, conn, probe, channelName(0), repeats)
		if baseline.Error != "" {
			return base.Timing, nil, fmt.Errorf("%s index baseline: %s", idx.table, baseline.Error)
		}
		r := IndexCost{Table: idx.table, Name: idx.name, Bytes: idx.bytes, Baseline: baseline.Timing}
		tx, e := conn.Begin(ctx)
		if e != nil {
			return base.Timing, nil, e
		}
		ddl := "DROP INDEX " + pgx.Identifier{idx.name}.Sanitize() + " CASCADE"
		if idx.con != "" {
			ddl = "ALTER TABLE " + pgx.Identifier{idx.table}.Sanitize() + " DROP CONSTRAINT " + pgx.Identifier{idx.con}.Sanitize() + " CASCADE"
		}
		_, e = tx.Exec(ctx, ddl)
		var xs []float64
		if e == nil {
			for n := 0; n < repeats; n++ {
				_, e = tx.Exec(ctx, "SAVEPOINT index_sample")
				if e != nil {
					break
				}
				var raw []byte
				e = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+probe.SQL).Scan(&raw)
				if e != nil {
					break
				}
				var p []struct {
					MS float64 `json:"Execution Time"`
				}
				e = json.Unmarshal(raw, &p)
				if e != nil {
					break
				}
				xs = append(xs, p[0].MS)
				_, e = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT index_sample")
				if e != nil {
					break
				}
			}
		}
		if e != nil {
			r.Error = e.Error()
		}
		if e = tx.Rollback(ctx); e != nil {
			return base.Timing, nil, e
		}
		r.Timing = percentiles(xs)
		r.MarginalP95MS = baseline.Timing.P95 - r.Timing.P95
		out = append(out, r)
	}
	return base.Timing, out, nil
}
func Run(ctx context.Context, c Config, sizes []int64) (Report, error) {
	report := Report{GeneratorVersion: "chatscale-20261001-v3", Seed: c.Seed, Tenants: c.Tenants, Channels: c.Channels, Repeats: c.Repeats, Concurrency: c.Concurrency, Limitations: []string{"Compact fixture: short hexadecimal synthetic IDs and 32-34 byte high-hit-rate messages keep the three-million-post corpus within the laptop disk budget. Real UUIDs, large bodies, rich audit evidence and event payloads require additional capacity; these figures are not production storage forecasts. The administrative test connection can bypass RLS; application-role plans remain unverified.", "Empirical tail percentiles have limited precision with small sample counts; p95 and p99 equal the maximum when fewer than 20/100 samples are collected. These are discovery figures, not full-target qualification.",
		"Laptop run: 100 thousand, 1 million and 3 million posts only; 500 million, 10 million and 100 million not run. Full target requires an isolated host with sufficient SSD capacity, production fsync/WAL settings and 1,000 connection capacity.", "200 tenants and 100,000 channels by default (500 memberships per tenant); 20 percent of total posts are threads. Four fixed roots plus first posts in each occupied small channel. The full 500-million-post shape remains unverified.", "Bodies intentionally share payroll/onboarding terms to exercise a high-hit-rate search. Post counts are initial loaded counts; committed benchmark sends add a small number of rows. Query SQL and migrations are captured at start; the schema digest pins all sizes to one snapshot. Concurrent lane edits may shift current source line numbers.",
		"Scan rows count tuple visits across scan nodes, including filter/recheck rows and loops; index and heap visits may count the same logical row twice. Zero-row ratios are undefined.",
		"Warm-cache executor EXPLAIN percentiles include instrumentation, not network or application authorization. Writes rollback; source constraints and failed fixture bindings are reported, never counted as successes.", "Mixed sends reproduce the database transaction (membership, route fence, counter, post, revision, outbox, audit and inventory), not the application transport. Retention EXPLAIN rolls back; vacuum is real on disposable database.", "Index ablation covers every index on the six send-path tables, rolls back DDL and writes, and measures marginal executor p95 only; negative marginal values indicate noise. Independent committed throughput per index is not inferred.", "Lock observer counts all database lock waiters; dedicated send-only phase isolates conversation-counter and tenant audit contention. Laptop concurrency is configurable; 1,000 senders not run.", "Peripheral feature tables are empty; timings over empty tables are not volume evidence. Snapshot catalogue includes current wave files and unresolved dynamic call sites explicitly."}}
	schema, digest, e := snapshotSchema(c.Root)
	if e != nil {
		return report, e
	}
	c.schema = schema
	report.SchemaSHA256 = digest
	qs, e := Inventory(c.Root)
	if e != nil {
		return report, e
	}
	if len(qs) == 0 {
		return report, fmt.Errorf("no chat query call sites found")
	}
	if c.Resume && len(sizes) != 1 {
		return report, fmt.Errorf("resume accepts one size")
	}
	requestedDatabase := c.Database
	for _, size := range sizes {
		c.Posts = size
		c.Database = requestedDatabase
		if c.Database == "" {
			c.Database = TestPrefix + fmt.Sprintf("%d_%d", time.Now().UnixNano(), size)
		}
		if requestedDatabase != "" && len(sizes) != 1 {
			return report, fmt.Errorf("named database accepts one size")
		}
		s := SizeReport{Posts: size}
		stage := "migrations"
		fmt.Printf("loading %d posts in isolated test database %s\n", size, c.Database)
		withDatabase := WithDatabase
		if c.Resume {
			withDatabase = WithResumedDatabase
		}
		e = withDatabase(ctx, c, func(conn *pgx.Conn) error {
			if err := Migrate(ctx, c); err != nil {
				return err
			}
			if err := conn.QueryRow(ctx, "SELECT current_setting('fsync'),current_setting('server_version')").Scan(&report.Fsync, &report.Server); err != nil {
				return err
			}
			began := time.Now()
			stage = "load"
			if err := Load(ctx, conn, c); err != nil {
				return err
			}
			s.LoadSeconds = time.Since(began).Seconds()
			stage = "query catalogue"
			fmt.Printf("loaded %d in %.1fs; measuring %d query variants\n", size, s.LoadSeconds, len(qs))
			for qi, q := range qs {
				if qi%50 == 0 {
					fmt.Printf("%d posts: query %d/%d (%s :%d)\n", size, qi+1, len(qs), q.File, q.Line)
				}
				for _, channel := range []string{channelName(0), channelName(299)} {
					s.Queries = append(s.Queries, Measure(ctx, conn, q, channel, c.Repeats))
				}
			}
			var err error
			stage = "mixed workload"
			fmt.Printf("%d posts: mixed workload\n", size)
			s.Mixed, err = Mixed(ctx, c, qs)
			if err != nil {
				return err
			}
			contention := c
			stage = "send contention"
			contention.Operations = max(c.Concurrency*8, 80)
			s.Contention, err = mixed(ctx, contention, qs, []string{"send"})
			if err != nil {
				return err
			}
			stage = "index costs"
			if err = resetMaintenanceLimits(ctx, conn); err != nil {
				return err
			}
			s.IndexedInsert, s.Indexes, err = IndexCosts(ctx, conn, c.Repeats)
			if err != nil {
				return err
			}
			stage = "statistics"
			if err = resetMaintenanceLimits(ctx, conn); err != nil {
				return err
			}
			if _, err = conn.Exec(ctx, "SELECT pg_stat_force_next_flush()"); err != nil {
				return err
			}
			s.AllIndexes, err = indexSizes(ctx, conn)
			if err != nil {
				return err
			}
			s.Relations, err = relations(ctx, conn)
			if err != nil {
				return err
			}
			began = time.Now()
			stage = "vacuum"
			fmt.Printf("%d posts: vacuum\n", size)
			if _, err = conn.Exec(ctx, "VACUUM (ANALYZE)"); err != nil {
				return err
			}
			s.VacuumMS = float64(time.Since(began).Microseconds()) / 1000
			stage = "post-vacuum statistics"
			s.AfterVacuum, err = relations(ctx, conn)
			if err != nil {
				return err
			}
			return conn.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&s.DatabaseBytes)
		})
		if e != nil {
			s.Error = fmt.Sprintf("%s: %v", stage, e)
			report.Sizes = append(report.Sizes, s)
			classify(&report)
			return report, fmt.Errorf("%d posts %s: %w", size, stage, e)
		}
		report.Sizes = append(report.Sizes, s)
		fmt.Printf("measured %d; database dropped (%.2f GiB peak)\n", size, float64(s.DatabaseBytes)/(1<<30))
	}
	classify(&report)
	return report, nil
}

func resetMaintenanceLimits(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, "SELECT set_config('statement_timeout','0',false),set_config('lock_timeout','0',false)")
	return err
}
func classify(r *Report) {
	if len(r.Sizes) == 0 {
		return
	}
	first := r.Sizes[0]
	last := &r.Sizes[len(r.Sizes)-1]
	for si := range r.Sizes {
		for mi := range r.Sizes[si].Queries {
			m := &r.Sizes[si].Queries[mi]
			m.Degrades = "insufficient successful nonempty samples"
			if m.Error != "" || m.Timing.Samples == 0 {
				continue
			}
			base := first.Queries[mi]
			m.Degrades = "neither observed (warm-cache laptop range)"
			if base.Timing.P95 > 0 && m.Timing.P95 > base.Timing.P95*2 && m.Timing.P95-base.Timing.P95 > .5 {
				m.Degrades = "table/history growth correlated; partition vs channel not isolated"
			}
			if m.Channel == channelName(0) && mi+1 < len(r.Sizes[si].Queries) {
				small := r.Sizes[si].Queries[mi+1]
				if small.Error == "" && !reflect.DeepEqual(m.Bindings, small.Bindings) && m.Timing.P95 > small.Timing.P95*2 && m.Timing.P95-small.Timing.P95 > .5 {
					m.Degrades = "channel size correlated"
				}
			}
			if m.ReturnedRows == 0 {
				m.Degrades += "; zero returned rows, not representative"
			}
		}
	}
	for _, m := range last.Queries {
		timedOut := strings.Contains(m.Error, "statement timeout")
		if (m.Error != "" && !timedOut) || (m.Timing.Samples == 0 && m.Error == "") {
			continue
		}
		proposal := "CHATSCALE-002"
		reason := "Access path, covering or partial indexes; validate using the captured plan."
		if strings.Contains(m.Query.SQL, "lastActivity") || m.Query.Function == "Counts" || strings.Contains(m.Query.SQL, "chat_outbox") || strings.Contains(m.Query.SQL, "max(lp.created_at)") {
			proposal = "CHATSCALE-003"
			reason = "Maintained conversation head, recipient state, or bounded outbox/receipt retention."
		}
		if strings.Contains(strings.ToLower(m.Query.Function), "searchchatcontent") {
			proposal = "Neither CHATSCALE-002 nor CHATSCALE-003"
			reason = "Materialized visible history plus position/JSON filters needs indexed extension-search access paths; partitioning or unread projections alone do not cover it."
		}
		f := Finding{Samples: m.Timing.Samples, Query: fmt.Sprintf("%s:%d %s [%s]", m.Query.File, m.Query.Line, m.Query.Function, m.Channel), P95MS: m.Timing.P95, ReadRows: m.ReadRows, Proposal: proposal, Reason: reason, Error: m.Error}
		if timedOut {
			f.LowerBoundMS = 15000
			f.Reason = "Statement exceeded the 15-second measurement budget; successful samples are censored. " + f.Reason
		}
		r.Ranked = append(r.Ranked, f)
	}
	sort.SliceStable(r.Ranked, func(i, j int) bool {
		return max(r.Ranked[i].P95MS, r.Ranked[i].LowerBoundMS) > max(r.Ranked[j].P95MS, r.Ranked[j].LowerBoundMS)
	})
}

// RefreshReport rebuilds classifications from saved measurements without
// loading another corpus or changing any measured plan or percentile.
func RefreshReport(root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".artifacts/lanes/agent-ui/chatscale/report.json"))
	if err != nil {
		return err
	}
	var r Report
	if err = json.Unmarshal(data, &r); err != nil {
		return err
	}
	r.Ranked = nil
	classify(&r)
	return WriteReport(root, r)
}

// WriteReport keeps all disposable artifacts inside the one authorized lane root.
func WriteReport(root string, r Report) error {
	dir := filepath.Join(root, ".artifacts/lanes/agent-ui/chatscale")
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	data, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "report.json"), data, 0600); e != nil {
		return e
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CHATSCALE-001 measurement report\nSeed %d; tenants %d; channels %d; repeats %d; concurrency %d; PostgreSQL %s; fsync=%s\n", r.Seed, r.Tenants, r.Channels, r.Repeats, r.Concurrency, r.Server, r.Fsync)
	fmt.Fprintf(&b, "Schema SHA256 %s\n", r.SchemaSHA256)
	fmt.Fprintf(&b, "Generator %s\n", r.GeneratorVersion)
	for _, l := range r.Limitations {
		fmt.Fprintln(&b, l)
	}
	for _, s := range r.Sizes {
		fmt.Fprintf(&b, "\n%d posts, %.2f GiB, COPY %.1fs, vacuum %.1fms\n", s.Posts, float64(s.DatabaseBytes)/(1<<30), s.LoadSeconds, s.VacuumMS)
		for _, m := range s.Queries {
			fmt.Fprintf(&b, "%s:%d %s [%s] p50=%.3f p95=%.3f p99=%.3fms scans=%.0f returned=%.0f %s ERROR=%s\n", m.Query.File, m.Query.Line, m.Query.Function, m.Channel, m.Timing.P50, m.Timing.P95, m.Timing.P99, m.ReadRows, m.ReturnedRows, m.Degrades, m.Error)
			if m.ReadPerReturned != nil {
				fmt.Fprintf(&b, "rows-read/returned %.4f\n", *m.ReadPerReturned)
			} else {
				fmt.Fprintln(&b, "rows-read/returned undefined (zero rows)")
			}
			if len(m.Plan) > 0 {
				fmt.Fprintf(&b, "EXPLAIN %s\n", m.Plan)
			}
		}
		for _, rel := range s.Relations {
			fmt.Fprintf(&b, "table %s heap+toast=%d index=%d total=%d live_estimate=%d dead_estimate=%d bytes\n", rel.Name, rel.TableBytes, rel.IndexBytes, rel.TotalBytes, rel.LiveRows, rel.DeadRows)
			fmt.Fprintf(&b, "dead tuple bytes=%d exact=%t autovacuum=%d %s\n", rel.DeadBytes, rel.DeadBytesExact, rel.AutovacuumCount, rel.DeadBytesNote)
		}
		for _, index := range s.Indexes {
			fmt.Fprintf(&b, "send-path index %s bytes=%d without-index p95=%.3f marginal=%.3fms ERROR=%s\n", index.Name, index.Bytes, index.Timing.P95, index.MarginalP95MS, index.Error)
		}
		for _, o := range s.Mixed.Operations {
			fmt.Fprintf(&b, "mixed %s p50=%.3f p95=%.3f p99=%.3fms errors=%d %s\n", o.Name, o.Timing.P50, o.Timing.P95, o.Timing.P99, o.Errors, o.Error)
		}
		fmt.Fprintf(&b, "mixed sends/s %.2f; send-only sends/s %.2f; lock max waiters %d age %.1fms\n", s.Mixed.SendsPerSecond, s.Contention.SendsPerSecond, s.Contention.MaxLockWaiters, s.Contention.LockWaitAgeMS)
	}
	fmt.Fprintln(&b, "\nBudgets at full target (unverified): open <50ms, page-back <50ms, send <30ms, sidebar of 300 <80ms, all p95.\nRanked queries at largest measured size:")
	for i, f := range r.Ranked {
		if f.LowerBoundMS > 0 && f.Samples == 0 {
			fmt.Fprintf(&b, "%d. timeout >=%.0fms (no completed samples) %s -> %s: %s ERROR=%s\n", i+1, f.LowerBoundMS, f.Query, f.Proposal, f.Reason, f.Error)
		} else if f.LowerBoundMS > 0 {
			fmt.Fprintf(&b, "%d. timeout >=%.0fms (completed-sample p95 %.3fms) %s -> %s: %s ERROR=%s\n", i+1, f.LowerBoundMS, f.P95MS, f.Query, f.Proposal, f.Reason, f.Error)
		} else {
			fmt.Fprintf(&b, "%d. %.3fms %s -> %s: %s\n", i+1, f.P95MS, f.Query, f.Proposal, f.Reason)
		}
	}
	return os.WriteFile(filepath.Join(dir, "report.txt"), []byte(b.String()), 0600)
}

func indexProbe(table string, post Query) Query {
	var sql string
	switch table {
	case "chat_post":
		return post
	case "chat_conversation":
		sql = `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id='t000' AND id='c000000' RETURNING event_sequence,post_sequence`
	case "chat_post_revision":
		sql = `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body) VALUES('t000','index-probe',1,'reader','Payroll review onboarding') RETURNING id`
	case "chat_outbox":
		sql = `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES('t000','index-probe','post.created','{"ConversationID":"c000000","TargetID":"index-probe"}') RETURNING id`
	case "chat_audit_event":
		sql = `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES('t000','index-probe',9000000000,'reader','post.created','chat','index-probe',0,'post.created','fixture',now(),'fixture') RETURNING event_id`
	case "chat_record_inventory":
		sql = `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES('t000','index-probe','c000000','POST','index-probe',1,now()) RETURNING record_id`
	}
	return Query{SQL: sql}
}

func indexSizes(ctx context.Context, conn *pgx.Conn) ([]IndexSize, error) {
	rows, e := conn.Query(ctx, `SELECT relname,indexrelname,pg_relation_size(indexrelid) FROM pg_stat_user_indexes WHERE relname LIKE 'chat_%' ORDER BY relname,indexrelname`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []IndexSize
	for rows.Next() {
		var i IndexSize
		if e = rows.Scan(&i.Table, &i.Name, &i.Bytes); e != nil {
			return nil, e
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
