package chatstore

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The search source builds its SQL from a catalog plus two suffixes. Recover
// those suffixes from the source, as the other reads recover their literals.
// This records the initial visible-message window; authority calls remain the
// separately timed real SearchChatContent call in the volume test.
func chatscaleSearchPlan(t *testing.T, s *Store, kind chatsearch.Kind) chatscaleMetric {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "chatsearch_source.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var window string
	ast.Inspect(f, func(n ast.Node) bool {
		literal, ok := n.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		v, err := strconv.Unquote(literal.Value)
		if err != nil {
			return true
		}
		if strings.HasPrefix(v, " AND ($13::text[] IS NULL OR true) AND ($8::timestamptz") {
			window = v
		}
		return true
	})
	if window == "" {
		t.Fatal("served search SQL changed; update its measurement builder")
	}
	catalog, err := chatsearchCatalog(kind)
	if err != nil {
		t.Fatal(err)
	}
	sql := chatsearchPosts + catalog + `) SELECT payload FROM catalog WHERE ($5='' OR payload->>'ID'=$5)` + window
	args := []any{chatscaleTenant, chatscaleTenant, chatscaleReader, chatscaleRooms(200), "", true, "", nil, "", string(kind), 100, chatsearchLikePatterns("payroll"), []string{}}
	metric := chatscaleMetric{Phase: "after", Read: "served-" + strings.ToLower(string(kind)) + "-search", SQL: sql, Args: args, Samples: 1}
	err = s.RunTenantTx(context.Background(), chatscaleTenant, func(tx dbport.Tx) error {
		var raw string
		if err := tx.QueryRow(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
			return err
		}
		var plans []chatscalePlan
		if err := json.Unmarshal([]byte(raw), &plans); err != nil {
			return err
		}
		if len(plans) != 1 || plans[0].Plan.Rows == 0 {
			t.Fatalf("served search plan returned no fixture matches: %s", raw)
		}
		metric.Plan = json.RawMessage(raw)
		metric.P50 = plans[0].Execution
		metric.P95 = plans[0].Execution
		metric.P99 = plans[0].Execution
		metric.RowsReadPerReturned = plans[0].Plan.scannedRows() / plans[0].Plan.Rows
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SEARCH PLAN %s time=%.3fms rows-read/returned=%.2f\n%s", kind, metric.P50, metric.RowsReadPerReturned, metric.Plan)
	return metric
}

func TestTodo_CHATSCALE_001_SearchPlans(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread} {
		metric := chatscaleSearchPlan(t, s, kind)
		if metric.Samples != 1 || len(metric.Plan) == 0 || metric.P50 <= 0 {
			t.Fatalf("missing measured search plan: %+v", metric)
		}
	}
}
