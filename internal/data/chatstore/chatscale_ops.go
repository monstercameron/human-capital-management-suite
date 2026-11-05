package chatstore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ChatscaleBudgets carries the p95 budgets CHATSCALE-001 GREEN sets for the
// later scale todos: open a channel under 50 ms, page back under 50 ms at any
// depth, send under 30 ms, sidebar counts under 80 ms for a person in 300
// channels, all at the 95th percentile at the full target. The operations
// view reports these beside the live figures so an operator reads every
// number against the same thresholds the budgets tests enforce.
type ChatscaleBudgets struct {
	OpenChannelP95MS float64 `json:"open_channel_p95_ms"`
	PageBackP95MS    float64 `json:"page_back_p95_ms"`
	SendP95MS        float64 `json:"send_p95_ms"`
	Sidebar300P95MS  float64 `json:"sidebar_300_p95_ms"`
}

// DefaultChatscaleBudgets returns the CHATSCALE-001 budget thresholds in
// milliseconds. It mirrors the budget_limits_ms of chatscale_series.json;
// changing a budget here without changing the budgets tests and the series
// would silently move the goalposts.
func DefaultChatscaleBudgets() ChatscaleBudgets {
	return ChatscaleBudgets{OpenChannelP95MS: 50, PageBackP95MS: 50, SendP95MS: 30, Sidebar300P95MS: 80}
}

// strictest returns the smallest positive budget: the flag line for slow
// queries, which cannot be attributed to one budget each.
func (b ChatscaleBudgets) strictest() float64 {
	out := 0.0
	for _, v := range []float64{b.OpenChannelP95MS, b.PageBackP95MS, b.SendP95MS, b.Sidebar300P95MS} {
		if v > 0 && (out == 0 || v < out) {
			out = v
		}
	}
	return out
}

// ChatscaleTableGrowth is one chat table's growth figure: heap, index and
// total bytes from pg_class (exact), live/dead row estimates and vacuum
// counters from pg_stat_user_tables. The JSON names match the series
// report's relations entries so the two can be read side by side.
type ChatscaleTableGrowth struct {
	Table           string `json:"table"`
	HeapBytes       int64  `json:"heap_bytes"`
	IndexBytes      int64  `json:"index_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	LiveRows        int64  `json:"live_rows_estimate"`
	DeadRows        int64  `json:"dead_rows_estimate"`
	VacuumCount     int64  `json:"vacuum_count"`
	AutovacuumCount int64  `json:"autovacuum_count"`
}

// ChatscaleSlowQuery is one chat statement observed by pg_stat_statements,
// ranked against the strictest served budget. pg_stat_statements reports
// means, not percentiles, so OverBudget compares the mean execution time:
// a statement whose mean already exceeds the p95 budget has no headroom left.
type ChatscaleSlowQuery struct {
	Query      string  `json:"query"`
	Calls      int64   `json:"calls"`
	MeanMS     float64 `json:"mean_ms"`
	MaxMS      float64 `json:"max_ms"`
	BudgetMS   float64 `json:"budget_ms"`
	OverBudget bool    `json:"over_budget"`
}

// ChatscaleSlowQuerySection is the view's slow-query figure. Available is
// false with a Reason when pg_stat_statements is not installed: the view
// reports the gap explicitly rather than an empty list that reads as "no
// slow queries".
type ChatscaleSlowQuerySection struct {
	Available bool                 `json:"available"`
	Reason    string               `json:"reason,omitempty"`
	Queries   []ChatscaleSlowQuery `json:"queries,omitempty"`
}

// ChatscaleOpsView is the CHATSCALE-003 operations figure: table-growth
// figures for the chat tables, slow-query figures ranked against the
// budgets, and the budget thresholds themselves.
type ChatscaleOpsView struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	Budgets     ChatscaleBudgets          `json:"budgets"`
	Tables      []ChatscaleTableGrowth    `json:"tables"`
	SlowQueries ChatscaleSlowQuerySection `json:"slow_queries"`
}

// chatscaleOpsSlowQueryLimit caps the ranked slow-query list: an operations
// view shows the worst offenders, not the whole statement catalog.
const chatscaleOpsSlowQueryLimit = 20

// chatscaleOpsQueryTextLimit caps one reported statement's text so a single
// enormous query cannot dominate the view's JSON.
const chatscaleOpsQueryTextLimit = 512

const chatscaleOpsTableGrowthSQL = `SELECT c.relname,pg_relation_size(c.oid),pg_indexes_size(c.oid),pg_total_relation_size(c.oid),
COALESCE(st.n_live_tup,0),COALESCE(st.n_dead_tup,0),COALESCE(st.vacuum_count,0),COALESCE(st.autovacuum_count,0)
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_stat_user_tables st ON st.relid=c.oid
WHERE n.nspname=current_schema() AND c.relkind='r'
AND (c.relname LIKE 'chat\_%' ESCAPE '\' OR c.relname LIKE 'chatscale\_%' ESCAPE '\') ORDER BY c.relname`

const chatscaleOpsSlowQuerySQL = `SELECT query,calls,mean_exec_time,max_exec_time FROM pg_stat_statements
WHERE (query ILIKE '%chat\_%' ESCAPE '\' OR query ILIKE '%chatscale\_%' ESCAPE '\')
AND query NOT ILIKE '%pg\_stat\_%' ESCAPE '\' AND query NOT ILIKE '%pg\_class%' ESCAPE '\'
AND query NOT ILIKE '%pg\_extension%' ESCAPE '\'
ORDER BY mean_exec_time DESC LIMIT $1`

// ChatscaleOpsView collects the operations figures for the chat tables:
// growth figures for every chat_* and chatscale_* table in the store's
// schema, and the slowest chat statements from pg_stat_statements ranked
// against the CHATSCALE-001 budgets. The collector is read-only; it never
// analyzes, vacuums or resets statistics.
func (s *Store) ChatscaleOpsView(ctx context.Context) (ChatscaleOpsView, error) {
	if s == nil {
		return ChatscaleOpsView{}, errors.New("chat store: operations view unavailable")
	}
	budgets := DefaultChatscaleBudgets()
	view := ChatscaleOpsView{GeneratedAt: time.Now().UTC(), Budgets: budgets}
	err := s.RunTx(ctx, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, chatscaleOpsTableGrowthSQL)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table ChatscaleTableGrowth
			if err := rows.Scan(&table.Table, &table.HeapBytes, &table.IndexBytes, &table.TotalBytes, &table.LiveRows, &table.DeadRows, &table.VacuumCount, &table.AutovacuumCount); err != nil {
				return err
			}
			view.Tables = append(view.Tables, table)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		var installed int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname='pg_stat_statements'`).Scan(&installed); err != nil {
			return err
		}
		if installed == 0 {
			view.SlowQueries = ChatscaleSlowQuerySection{Available: false, Reason: "pg_stat_statements is not installed on this database"}
			return nil
		}
		slow, err := tx.Query(ctx, chatscaleOpsSlowQuerySQL, chatscaleOpsSlowQueryLimit)
		if err != nil {
			return err
		}
		defer slow.Close()
		var found []chatscaleSlowQueryRow
		for slow.Next() {
			var row chatscaleSlowQueryRow
			if err := slow.Scan(&row.query, &row.calls, &row.meanMS, &row.maxMS); err != nil {
				return err
			}
			found = append(found, row)
		}
		if err := slow.Err(); err != nil {
			return err
		}
		view.SlowQueries = ChatscaleSlowQuerySection{Available: true, Queries: rankChatscaleSlowQueries(found, budgets, chatscaleOpsSlowQueryLimit)}
		return nil
	})
	if err != nil {
		return ChatscaleOpsView{}, err
	}
	return view, nil
}

// chatscaleSlowQueryRow is one pg_stat_statements row before ranking.
type chatscaleSlowQueryRow struct {
	query  string
	calls  int64
	meanMS float64
	maxMS  float64
}

// rankChatscaleSlowQueries orders statements slowest first, flags each
// against the strictest served budget, caps the list, and truncates
// statement text to the view's display limit.
func rankChatscaleSlowQueries(rows []chatscaleSlowQueryRow, budgets ChatscaleBudgets, limit int) []ChatscaleSlowQuery {
	ordered := append([]chatscaleSlowQueryRow(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].meanMS > ordered[j].meanMS })
	if limit >= 0 && len(ordered) > limit {
		ordered = ordered[:limit]
	}
	budget := budgets.strictest()
	out := make([]ChatscaleSlowQuery, 0, len(ordered))
	for _, row := range ordered {
		out = append(out, ChatscaleSlowQuery{
			Query:      truncateChatscaleQuery(row.query),
			Calls:      row.calls,
			MeanMS:     row.meanMS,
			MaxMS:      row.maxMS,
			BudgetMS:   budget,
			OverBudget: row.meanMS > budget,
		})
	}
	return out
}

func truncateChatscaleQuery(query string) string {
	runes := []rune(query)
	if len(runes) <= chatscaleOpsQueryTextLimit {
		return query
	}
	return string(runes[:chatscaleOpsQueryTextLimit])
}
