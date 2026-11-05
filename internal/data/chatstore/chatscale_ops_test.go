package chatstore

import (
	"context"
	"encoding/json"
	"testing"
)

// TestTodo_CHATSCALE_003_OpsView is the RED test for the CHATSCALE-003 GREEN
// clause "slow-query and table-growth figures exposed on the operations view
// with thresholds from the budgets": the store reports per-table growth
// figures for the chat tables, slow-query figures ranked against the
// CHATSCALE-001 p95 budgets, and the budget thresholds themselves.
func TestTodo_CHATSCALE_003_OpsView(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)

	view, err := s.ChatscaleOpsView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	budgets := view.Budgets
	if budgets.OpenChannelP95MS != 50 || budgets.PageBackP95MS != 50 || budgets.SendP95MS != 30 || budgets.Sidebar300P95MS != 80 {
		t.Fatalf("budgets=%+v want the CHATSCALE-001 p95 limits 50/50/30/80ms", budgets)
	}
	if view.GeneratedAt.IsZero() {
		t.Fatal("operations view carries no generation time")
	}
	byTable := map[string]ChatscaleTableGrowth{}
	for _, table := range view.Tables {
		byTable[table.Table] = table
	}
	for _, want := range []string{"chat_post", "chat_outbox", "chat_outbox_receipt", "chatscale_read_state", "chat_membership", "chat_conversation"} {
		if _, ok := byTable[want]; !ok {
			t.Fatalf("table-growth figures omit %s (tables=%d)", want, len(view.Tables))
		}
	}
	// The seed fills posts, the outbox and memberships; those tables must
	// report positive sizes. chatscale_read_state stays empty until the
	// sidebar cache is exercised, and an empty table has no heap pages.
	for _, want := range []string{"chat_post", "chat_outbox", "chat_outbox_receipt", "chat_membership", "chat_conversation"} {
		if got := byTable[want]; got.TotalBytes <= 0 || got.HeapBytes <= 0 {
			t.Fatalf("%s figures=%+v: seeded tables must report positive sizes", want, got)
		}
	}
	posts := byTable["chat_post"]
	if posts.TotalBytes < posts.HeapBytes+posts.IndexBytes {
		t.Fatalf("chat_post total=%d below heap+index=%d+%d", posts.TotalBytes, posts.HeapBytes, posts.IndexBytes)
	}
	slow := view.SlowQueries
	if slow.Available {
		// pg_stat_statements is installed: every reported statement carries
		// the strictest served budget as its flag line, ranked slowest first.
		for i, q := range slow.Queries {
			if q.Calls < 1 || q.MeanMS < 0 || q.MaxMS < q.MeanMS {
				t.Fatalf("slow query %d=%+v has impossible counters", i, q)
			}
			if q.BudgetMS != budgets.SendP95MS {
				t.Fatalf("slow query %d budget=%.1f want strictest served budget %.1f", i, q.BudgetMS, budgets.SendP95MS)
			}
			if q.OverBudget != (q.MeanMS > q.BudgetMS) {
				t.Fatalf("slow query %d over=%v inconsistent with mean=%.3f budget=%.1f", i, q.OverBudget, q.MeanMS, q.BudgetMS)
			}
			if i > 0 && q.MeanMS > slow.Queries[i-1].MeanMS {
				t.Fatal("slow queries are not ranked slowest first")
			}
		}
	} else if slow.Reason == "" {
		t.Fatal("slow-query figures unavailable with no reason recorded")
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ChatscaleOpsView
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Tables) != len(view.Tables) {
		t.Fatalf("operations view does not survive a JSON round trip: %v", err)
	}
}

// TestChatscaleSlowQueryThresholds pins the pure ranking/threshold step: the
// slowest statements come first, each is flagged against the strictest served
// budget, and the list is capped.
func TestChatscaleSlowQueryThresholds(t *testing.T) {
	rows := []chatscaleSlowQueryRow{
		{query: "SELECT 1", calls: 10, meanMS: 1.0, maxMS: 2.0},
		{query: "SELECT * FROM chat_post", calls: 3, meanMS: 45.5, maxMS: 120.0},
		{query: "SELECT * FROM chat_outbox", calls: 7, meanMS: 30.0, maxMS: 31.0},
	}
	got := rankChatscaleSlowQueries(rows, DefaultChatscaleBudgets(), 2)
	if len(got) != 2 {
		t.Fatalf("ranked=%d want the 2 slowest", len(got))
	}
	if got[0].MeanMS != 45.5 || !got[0].OverBudget {
		t.Fatalf("slowest=%+v want mean 45.5 flagged over the 30ms send budget", got[0])
	}
	if got[1].MeanMS != 30.0 || got[1].OverBudget {
		t.Fatalf("second=%+v want mean 30.0 exactly at budget, not over", got[1])
	}
	if got[0].BudgetMS != 30 || got[0].Calls != 3 || got[0].MaxMS != 120.0 {
		t.Fatalf("slowest=%+v lost its counters or budget line", got[0])
	}
}
