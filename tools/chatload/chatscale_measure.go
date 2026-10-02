package chatload

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Percentiles struct {
	P50     float64 `json:"p50_ms"`
	P95     float64 `json:"p95_ms"`
	P99     float64 `json:"p99_ms"`
	Samples int     `json:"samples"`
}

func percentiles(xs []float64) Percentiles {
	ys := append([]float64(nil), xs...)
	sort.Float64s(ys)
	at := func(p float64) float64 {
		if len(ys) == 0 {
			return 0
		}
		return ys[max(0, int(math.Ceil(p*float64(len(ys))))-1)]
	}
	return Percentiles{at(.5), at(.95), at(.99), len(ys)}
}

type PlanNode struct {
	Node     string     `json:"Node Type"`
	Rows     float64    `json:"Actual Rows"`
	Loops    float64    `json:"Actual Loops"`
	Filtered float64    `json:"Rows Removed by Filter"`
	Recheck  float64    `json:"Rows Removed by Index Recheck"`
	Plans    []PlanNode `json:"Plans"`
}

func rowsRead(p PlanNode) float64 {
	n := 0.
	if strings.Contains(p.Node, "Scan") {
		n = (p.Rows + p.Filtered + p.Recheck) * p.Loops
	}
	for _, c := range p.Plans {
		n += rowsRead(c)
	}
	return n
}

type Measurement struct {
	Query           Query           `json:"query"`
	Channel         string          `json:"channel"`
	Timing          Percentiles     `json:"timing"`
	Plan            json.RawMessage `json:"plan,omitempty"`
	ReadRows        float64         `json:"scan_rows"`
	ReturnedRows    float64         `json:"returned_rows"`
	ReadPerReturned *float64        `json:"rows_read_per_row_returned"`
	Error           string          `json:"error,omitempty"`
	Bindings        []any           `json:"bindings,omitempty"`
	Degrades        string          `json:"degrades"`
}

func binding(expr string, oid uint32, channel string) any {
	lower := strings.ToLower(expr)
	switch oid {
	case 16:
		return !strings.Contains(lower, "tomb") && !strings.Contains(lower, "deleted")
	case 20, 21, 23, 26:
		if strings.Contains(lower, "limit") || strings.Contains(lower, "pagesize") {
			if strings.Contains(lower, "countscan") {
				return int64(5000)
			}
			return int64(51)
		}
		if strings.Contains(lower, "before") || strings.Contains(lower, "bound") {
			return int64(1<<31 - 1)
		}
		if strings.Contains(lower, "after") || strings.Contains(lower, "offset") {
			return int64(0)
		}
		if strings.Contains(lower, "countscan") {
			return int64(5000)
		}
		if lower == "limit+1" {
			return int64(51)
		}
		return int64(1)
	case 700, 701, 1700:
		return float64(1)
	case 1114, 1184, 1082:
		return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	case 114, 3802:
		return "[]"
	case 1009:
		return []string{"reader"}
	case 1016:
		return []int64{1}
	case 1007:
		return []int32{1}
	case 17:
		return []byte("fixture")
	}
	switch {
	case strings.Contains(lower, "cursor") || lower == "after":
		return ""
	case strings.Contains(lower, "tenant") || lower == "t" || lower == "tid" || lower == "home" || lower == "host":
		return "t000"
	case strings.Contains(lower, "conversation") || lower == "cid":
		return channel
	case strings.Contains(lower, "root") || strings.Contains(lower, "thread") || strings.Contains(lower, "parent"):
		return "root-" + channel
	case strings.Contains(lower, "post") || lower == "id":
		return "root-" + channel
	case strings.Contains(lower, "version"):
		return "1.0.0"
	case strings.Contains(lower, "digest"):
		return "sha256:" + strings.Repeat("a", 64)
	case strings.Contains(lower, "classification"):
		return "PUBLIC"
	case strings.Contains(lower, "tone"):
		return "as-written"
	case strings.Contains(lower, "permission"):
		return "Remove messages"
	case strings.Contains(lower, "operation"):
		return "CREATE"
	case strings.Contains(lower, "reasoncode"):
		return "policy_violation"
	case strings.Contains(lower, "body"):
		return "Payroll review and onboarding update"
	case strings.Contains(lower, "query") || strings.Contains(lower, "term") || strings.Contains(lower, "search"):
		return "payroll"
	case strings.Contains(lower, "emoji"):
		return "thumbsup"
	case strings.Contains(lower, "consumer"):
		return "fixture"
	case strings.Contains(lower, "kind"):
		return "PUBLIC_CHANNEL"
	case strings.Contains(lower, "role"):
		return "member"
	case strings.Contains(lower, "state"):
		return "active"
	case strings.Contains(lower, "visibility") || lower == "history":
		return "FULL_HISTORY"
	case strings.Contains(lower, "json") || strings.Contains(lower, "payload") || strings.Contains(lower, "refs"):
		return "[]"
	default:
		return "reader"
	}
}
func arguments(ctx context.Context, conn *pgx.Conn, q Query, channel string) ([]any, error) {
	d, e := conn.Prepare(ctx, "", q.SQL)
	if e != nil {
		return nil, e
	}
	out := make([]any, len(d.ParamOIDs))
	for i, oid := range d.ParamOIDs {
		expr := ""
		if i < len(q.Arguments) {
			expr = q.Arguments[i]
		}
		out[i] = binding(expr, oid, channel)
		column := insertColumn(q.SQL, i+1)
		if column != "" {
			out[i] = binding(column, oid, channel)
			switch column {
			case "sharer_kind":
				out[i] = "human"
			case "id", "event_id", "record_id", "client_key":
				if oid == 25 {
					out[i] = "measurement-probe"
				}
			case "sequence", "stream_offset":
				out[i] = int64(9000000000)
			case "revision":
				out[i] = int64(2)
			case "payload_json", "layout", "value":
				out[i] = "{}"
			case "kind":
				if strings.Contains(q.SQL, "chat_channel_widget") {
					out[i] = "TEAM"
				}
			case "reason_code":
				out[i] = "policy_violation"
			case "data_class":
				out[i] = "PUBLIC"
			case "vote_home_tenant_id", "vote_subject_id":
				out[i] = nil
			case "prior_option_id", "option_id":
				if strings.Contains(q.SQL, "chat_channel_poll_revision") {
					out[i] = ""
				}
			case "expires_at":
				out[i] = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			}
		}
		if q.Function == "chatstateWrite" {
			if expr == "status.Status" {
				out[i] = "LOCKED"
			}
			if expr == "status.Until" {
				out[i] = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
			}
		}
		if q.Function == "createConversation" && strings.Contains(q.SQL, "INSERT INTO chat_membership") && strings.Contains(strings.ToLower(expr), "subject") {
			out[i] = "measurement-reader"
		}
		if oid == 1009 {
			if strings.Contains(strings.ToLower(expr), "conversation") || expr == "allowed" {
				out[i] = []string{channel}
			}
			if expr == "strings.Fields(q.Query)" {
				out[i] = []string{"payroll"}
			}
		}
		if q.Function == "GetConversation" && i == 1 {
			out[i] = channel
		}
		if q.Function == "Search" {
			if expr == "r.ConversationID" || expr == "r.AuthorID" || expr == "channelCursor" || expr == "messageID" {
				out[i] = ""
			}
		}
		if strings.HasPrefix(q.Function, "SearchChatContent") {
			switch expr {
			case "id", "f.Person", "f.Before", "f.After", "f.On", "edge":
				out[i] = ""
			case "f.File", "f.Link", "f.Reactions", "f.Threads", "f.MentionsMe", "f.Agent", "f.Mine", "f.Voice":
				out[i] = false
			}
		}
		if expr == "time.Now().UTC()" || expr == "now" || expr == "at" {
			if oid == 1186 {
				out[i] = "0 seconds"
			}
		}
	}
	return out, nil
}
func explainOnce(ctx context.Context, conn *pgx.Conn, q Query, args []any) (raw json.RawMessage, ms float64, err error) {
	tx, e := conn.Begin(ctx)
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id','t000',true),set_config('statement_timeout','15000',true),set_config('lock_timeout','3000',true)"); e != nil {
		return nil, 0, e
	}
	if strings.HasPrefix(q.SQL, "INSERT INTO chat_channel_poll_vote") {
		if _, e = tx.Exec(ctx, `INSERT INTO chat_channel_poll(tenant_id,conversation_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, args[0], args[1]); e != nil {
			return nil, 0, e
		}
	}
	if strings.HasPrefix(q.SQL, "INSERT INTO chat_public_audience_principal") {
		if _, e = tx.Exec(ctx, `INSERT INTO chat_public_audience_policy(tenant_id,conversation_id,revision,classification) VALUES($1,$2,1,'PUBLIC') ON CONFLICT DO NOTHING`, args[0], args[1]); e != nil {
			return nil, 0, e
		}
	}
	e = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+q.SQL, args...).Scan(&raw)
	if e != nil {
		return nil, 0, e
	}
	var plan []struct {
		Execution float64 `json:"Execution Time"`
	}
	if e = json.Unmarshal(raw, &plan); e != nil {
		return nil, 0, e
	}
	return raw, plan[0].Execution, nil
}

// Measure records executor percentiles. Writes are explained in rolled-back
// transactions; their latency is not a committed send's end-to-end latency.
func Measure(ctx context.Context, conn *pgx.Conn, q Query, channel string, repeats int) Measurement {
	m := Measurement{Query: q, Channel: channel, Degrades: "pending cross-size comparison"}
	if e := guardConnection(ctx, conn); e != nil {
		m.Error = e.Error()
		return m
	}
	if q.Unresolved != "" {
		m.Error = "unresolved dynamic SQL: " + q.Unresolved
		return m
	}
	args, e := arguments(ctx, conn, q, channel)
	if e != nil {
		m.Error = e.Error()
		return m
	}
	m.Bindings = args
	xs := []float64{}
	readOnly := !regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE)\b`).MatchString(q.SQL)
	if readOnly {
		if _, e = conn.Exec(ctx, "SELECT set_config('hcmnext.tenant_id','t000',false),set_config('statement_timeout','15000',false),set_config('lock_timeout','3000',false)"); e != nil {
			m.Error = e.Error()
			return m
		}
	}
	for i := 0; i < repeats; i++ {
		var raw json.RawMessage
		var ms float64
		var e error
		if readOnly {
			e = conn.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+q.SQL, args...).Scan(&raw)
			if e == nil {
				var rows []struct {
					MS float64 `json:"Execution Time"`
				}
				e = json.Unmarshal(raw, &rows)
				if e == nil {
					ms = rows[0].MS
				}
			}
		} else {
			raw, ms, e = explainOnce(ctx, conn, q, args)
		}
		if e != nil {
			m.Error = e.Error()
			break
		}
		if i == 0 {
			m.Plan = raw
		}
		xs = append(xs, ms)
	}
	m.Timing = percentiles(xs)
	if len(m.Plan) > 0 {
		var plans []struct {
			Plan PlanNode `json:"Plan"`
		}
		if e = json.Unmarshal(m.Plan, &plans); e == nil && len(plans) > 0 {
			m.ReadRows = rowsRead(plans[0].Plan)
			m.ReturnedRows = plans[0].Plan.Rows * plans[0].Plan.Loops
			if m.ReturnedRows > 0 {
				v := m.ReadRows / m.ReturnedRows
				m.ReadPerReturned = &v
			}
		}
	}
	return m
}
func replay(ctx context.Context, conn *pgx.Conn, q Query, args []any) error {
	rows, e := conn.Query(ctx, q.SQL, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		if _, e = rows.Values(); e != nil {
			return e
		}
	}
	return rows.Err()
}

type Operation struct {
	Name   string      `json:"name"`
	Timing Percentiles `json:"timing"`
	Errors int         `json:"errors"`
	Error  string      `json:"first_error,omitempty"`
}
type Workload struct {
	Ratio           string      `json:"ratio"`
	Concurrency     int         `json:"concurrency"`
	Operations      []Operation `json:"operations"`
	ElapsedSeconds  float64     `json:"elapsed_seconds"`
	SendsPerSecond  float64     `json:"committed_sends_per_second"`
	LockSamples     int         `json:"conversation_lock_samples"`
	LockWaitSamples int         `json:"conversation_lock_wait_samples"`
	MaxLockWaiters  int         `json:"max_lock_waiters"`
	LockWaitAgeMS   float64     `json:"max_observed_lock_wait_age_ms"`
}

func workloadQuery(qs []Query, name string) (Query, error) {
	switch name {
	case "open_channel", "page_back":
		return queryBy(qs, "ListPosts", "m.state='active'")
	case "sidebar_300":
		return queryBy(qs, "Counts", "WITH candidate")
	case "search":
		return queryBy(qs, "Search", "SELECT p.id")
	case "thread_open":
		return queryBy(qs, "readBackgroundThreadSnapshot", "p.parent_id")
	case "outbox_drain":
		return queryBy(qs, "PendingOutboxAfter", "SELECT")
	case "mark_read":
		return queryBy(qs, "PutReadState", "INSERT")
	}
	return Query{}, fmt.Errorf("unknown workload %s", name)
}

func insertColumn(sql string, param int) string {
	match := regexp.MustCompile(`(?is)INSERT\s+INTO\s+\w+\s*\(([^)]+)\)\s*VALUES\s*\(([^)]+)\)`).FindStringSubmatch(sql)
	if len(match) != 3 {
		return ""
	}
	columns, values := strings.Split(match[1], ","), strings.Split(match[2], ",")
	for i, v := range values {
		v = strings.TrimSpace(v)
		if v == "$"+strconv.Itoa(param) && i < len(columns) {
			return strings.ToLower(strings.TrimSpace(columns[i]))
		}
	}
	return ""
}
