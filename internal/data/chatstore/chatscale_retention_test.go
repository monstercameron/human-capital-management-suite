package chatstore

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func chatscaleRetentionPlan(t *testing.T, s *Store, phase string) chatscaleMetric {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenant(ctx, tx, chatscaleTenant); err != nil {
		t.Fatal(err)
	}
	sql := chatscaleLiteral(t, "store.go", "PruneOutbox", "Exec") + " RETURNING o.id"
	args := []any{chatscaleTenant, time.Now(), 500}
	var raw string
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plans []chatscalePlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 || plans[0].Plan.Rows != 500 {
		t.Fatalf("retention must delete its bounded batch: %s err=%v", raw, err)
	}
	// Roll back the measured deletion so before and after prune the same rows.
	metric := chatscaleMetric{Phase: phase, Read: "outbox-retention", SQL: sql, Args: args,
		P50: plans[0].Execution, P95: plans[0].Execution, P99: plans[0].Execution,
		Samples: 1, RowsReadPerReturned: plans[0].Plan.scannedRows() / 500, Plan: json.RawMessage(raw)}
	t.Logf("RETENTION %s events=500 time=%.3fms\n%s", phase, metric.P50, metric.Plan)
	return metric
}

func TestTodo_CHATSCALE_002_Integration_Retention(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, chatscaleSize())
	if err := s.execTenant(context.Background(), chatscaleTenant, `DROP INDEX chatscale_receipt_parent`); err != nil {
		t.Fatal(err)
	}
	before := chatscaleRetentionPlan(t, s, "before")
	if err := s.execTenant(context.Background(), chatscaleTenant, `CREATE INDEX chatscale_receipt_parent ON chat_outbox_receipt(outbox_id)`); err != nil {
		t.Fatal(err)
	}
	after := chatscaleRetentionPlan(t, s, "after")
	n, err := s.PruneOutbox(context.Background(), chatscaleTenant, time.Now(), 500)
	if err != nil || n != 500 {
		t.Fatalf("retention committed batch=%d err=%v", n, err)
	}
	var receipts int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM chat_outbox_receipt`).Scan(&receipts); err != nil || receipts != chatscaleSize()*4/5-500 {
		t.Fatalf("cascade receipts=%d want %d err=%v", receipts, chatscaleSize()*4/5-500, err)
	}
	if *chatscaleReportPath != "" {
		raw, err := json.MarshalIndent(struct {
			Posts   int               `json:"posts"`
			Metrics []chatscaleMetric `json:"metrics"`
			Limits  []string          `json:"limits"`
		}{chatscaleSize(), []chatscaleMetric{before, after}, []string{"One ANALYZE observation per phase, including foreign-key trigger time; repeated p50/p95/p99 fields are not percentile estimates. Both measurements roll back the same bounded 500-event deletion."}}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(*chatscaleReportPath, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
