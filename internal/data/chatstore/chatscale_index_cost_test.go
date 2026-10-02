package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatscaleIndexCost struct {
	Index       string          `json:"index"`
	DDL         string          `json:"ddl"`
	SQL         string          `json:"write_sql"`
	Bytes       int64           `json:"index_bytes"`
	Before      []float64       `json:"before_execution_ms"`
	After       []float64       `json:"after_execution_ms"`
	MedianDelta float64         `json:"median_delta_ms"`
	Plan        json.RawMessage `json:"after_explain_analyze_buffers_wal"`
}

func chatscaleIndexWrite(table string) string {
	switch table {
	case "chat_post":
		return `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,parent_id,revision,references_json)
 SELECT 'chatscale-index-cost-'||g,$1,$2,'sender',$1,10000000+g,
 'Payroll follow-up '||repeat('Please check the operating notes. ',8),
 CASE WHEN g%5=0 THEN 'chatscale-p-0000000001' ELSE '' END,
 CASE WHEN g%17=0 THEN 2 ELSE 1 END,
 CASE WHEN g%13=0 THEN jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$1::text,'ID','chatscale-reader')) ELSE '[]'::jsonb END
 FROM generate_series(1,200) g RETURNING 1`
	case "chat_membership":
		return `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id)
 SELECT $1,$2,$1,'chatscale-index-cost-'||g FROM generate_series(1,200) g RETURNING 1`
	case "chat_reaction":
		return `INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji)
 SELECT $1,$1,'chatscale-p-0000000001','chatscale-reader','cost-'||g FROM generate_series(1,200) g WHERE $2<>'' RETURNING 1`
	case "chat_pin":
		return `INSERT INTO chat_pin(tenant_id,home_tenant_id,conversation_id,post_id,member_id)
 SELECT $1,$1,conversation_id,id,'chatscale-reader' FROM chat_post p WHERE p.tenant_id=$1 AND $2<>'' AND NOT p.tombstoned
 AND NOT EXISTS(SELECT 1 FROM chat_pin x WHERE x.tenant_id=p.tenant_id AND x.conversation_id=p.conversation_id AND x.post_id=p.id)
 ORDER BY p.id LIMIT 200 RETURNING 1`
	case "chat_outbox":
		return `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload)
 SELECT $1,'chatscale-index-cost-'||g,'post.created',jsonb_build_object('ConversationID',$2::text,'Value','payload') FROM generate_series(1,200) g RETURNING 1`
	case "chat_outbox_receipt":
		return `INSERT INTO chat_outbox_receipt(tenant_id,outbox_id)
 SELECT $1,o.id FROM chat_outbox o WHERE o.tenant_id=$1 AND $2<>''
 AND NOT EXISTS(SELECT 1 FROM chat_outbox_receipt r WHERE r.tenant_id=o.tenant_id AND r.outbox_id=o.id)
 ORDER BY o.id LIMIT 200 RETURNING 1`
	default:
		return ""
	}
}

func chatscaleWriteSamples(t *testing.T, s *Store, sql string) ([]float64, json.RawMessage) {
	t.Helper()
	ctx := context.Background()
	rollback := errors.New("rollback index-cost sample")
	var samples []float64
	var plan json.RawMessage
	for i := 0; i < 6; i++ {
		err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			var raw string
			if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) "+sql, chatscaleTenant, chatscaleRoom).Scan(&raw); err != nil {
				return err
			}
			var plans []chatscalePlan
			if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 || plans[0].Plan.Rows != 200 {
				return fmt.Errorf("write sample must insert 200 rows: %s err=%v", raw, err)
			}
			if i != 0 {
				samples = append(samples, plans[0].Execution)
				plan = json.RawMessage(raw)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
	}
	return samples, plan
}

func TestTodo_CHATSCALE_001_Performance_Indexes(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, chatscaleSize())
	chatscaleIndexes(t, s, false)
	var costs []chatscaleIndexCost
	for _, file := range []string{"migrations/00034_chatscale_hot_reads.sql", "migrations/00037_chatscale_receipt_retention.sql", "migrations/00038_chatscale_post_visibility.sql"} {
		raw, err := Migrations.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range strings.Split(strings.Split(string(raw), "-- +goose Down")[0], ";") {
			start := strings.Index(part, "CREATE INDEX CONCURRENTLY ")
			if start < 0 {
				continue
			}
			ddl := strings.TrimSpace(part[start:])
			words := strings.Fields(ddl)
			name, table := words[3], strings.Split(words[5], "(")[0]
			sql := chatscaleIndexWrite(table)
			if sql == "" {
				t.Fatalf("missing write workload for %s", table)
			}
			cost := chatscaleIndexCost{Index: name, DDL: ddl, SQL: sql}
			cost.Before, _ = chatscaleWriteSamples(t, s, sql)
			if err := s.execTenant(context.Background(), chatscaleTenant, strings.Replace(ddl, "CONCURRENTLY ", "", 1)); err != nil {
				t.Fatal(err)
			}
			cost.After, cost.Plan = chatscaleWriteSamples(t, s, sql)
			if err := s.pool.QueryRow(context.Background(), `SELECT pg_relation_size($1::regclass)`, name).Scan(&cost.Bytes); err != nil {
				t.Fatal(err)
			}
			cost.MedianDelta = chatscalePercentile(cost.After, .5) - chatscalePercentile(cost.Before, .5)
			t.Logf("INDEX COST %s bytes=%d 200-row insert median before=%.3fms after=%.3fms delta=%.3fms", name, cost.Bytes, chatscalePercentile(cost.Before, .5), chatscalePercentile(cost.After, .5), cost.MedianDelta)
			costs = append(costs, cost)
			if err := s.execTenant(context.Background(), chatscaleTenant, "DROP INDEX "+name); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(costs) != 11 {
		t.Fatalf("measured index costs=%d want 11", len(costs))
	}
	if *chatscaleReportPath != "" {
		raw, err := json.MarshalIndent(struct {
			Posts int                  `json:"posts"`
			Costs []chatscaleIndexCost `json:"index_costs"`
			Note  string               `json:"limits"`
		}{chatscaleSize(), costs, "Each index is enabled individually over the pre-existing index set. Five warm, rolled-back 200-row write batches per phase; WAL and trigger costs are in the plans. Deltas can be negative due to read-plan benefits and noise. This is not single-send latency or sustained write throughput."}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*chatscaleReportPath, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
