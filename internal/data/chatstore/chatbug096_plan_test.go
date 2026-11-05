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

// chatbug096IndexName is the trigram index on post bodies the served
// message/thread search needs (blocked-needs-migration): the prefilter
// matches infixes ("%word%"), which the tsvector index cannot serve.
const chatbug096IndexName = "chat_post_body_trgm"

// TestTodo_CHATBUG_096_PlanUsesIndex: message text search uses an index. On
// 50,000 posts the served message-search SQL answers a rare word through the
// post-body trigram index, not a sequential scan. (Needs a migration adding
// pg_trgm and the index; this test is its executable spec.)
func TestTodo_CHATBUG_096_PlanUsesIndex(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 50000)
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
	catalog, err := chatsearchCatalog(chatsearch.Message)
	if err != nil {
		t.Fatal(err)
	}
	sql := chatsearchPosts + catalog + `) SELECT payload FROM catalog WHERE ($5='' OR payload->>'ID'=$5)` + window
	// A word carried by exactly one post: selective enough that only an index
	// answers it without reading the table.
	words := []string{"%49999%"}
	args := []any{chatscaleTenant, chatscaleTenant, chatscaleReader, chatscaleRooms(200), "", true, "", nil, "", string(chatsearch.Message), 100, words, []string{}}
	var raw string
	err = s.RunTenantTx(context.Background(), chatscaleTenant, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	var plans []chatscalePlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("no plan: %s %v", raw, err)
	}
	if plans[0].Plan.Rows == 0 {
		t.Fatalf("the rare word matched nothing, the plan proves nothing: %s", raw)
	}
	var seqscan, indexed bool
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if node["Node Type"] == "Seq Scan" && node["Relation Name"] == "chat_post" {
			seqscan = true
		}
		if name, _ := node["Index Name"].(string); name == chatbug096IndexName {
			indexed = true
		}
		plans, _ := node["Plans"].([]any)
		for _, child := range plans {
			if m, ok := child.(map[string]any); ok {
				walk(m)
			}
		}
	}
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("no plan: %s %v", raw, err)
	}
	walk(decoded[0]["Plan"].(map[string]any))
	if seqscan || !indexed {
		t.Fatalf("50,000-post message search seqscan=%v index=%v, want the %s index: %s", seqscan, indexed, chatbug096IndexName, raw)
	}
}
