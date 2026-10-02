package chatstore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatscaleRelation struct {
	Name            string   `json:"name"`
	HeapBytes       int64    `json:"heap_bytes"`
	IndexBytes      int64    `json:"index_bytes"`
	TotalBytes      int64    `json:"total_bytes"`
	LiveEstimate    int64    `json:"live_rows_estimate"`
	DeadEstimate    int64    `json:"dead_rows_estimate"`
	VacuumCount     int64    `json:"vacuum_count"`
	AutoVacuumCount int64    `json:"autovacuum_count"`
	Options         []string `json:"storage_options"`
}

func chatscaleRelations(t *testing.T, s *Store) []chatscaleRelation {
	t.Helper()
	var out []chatscaleRelation
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT c.relname,pg_relation_size(c.oid),pg_indexes_size(c.oid),pg_total_relation_size(c.oid),
 COALESCE(st.n_live_tup,0),COALESCE(st.n_dead_tup,0),COALESCE(st.vacuum_count,0),COALESCE(st.autovacuum_count,0),COALESCE(c.reloptions,'{}'::text[])
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_stat_user_tables st ON st.relid=c.oid
 WHERE n.nspname=current_schema() AND c.relkind='r' AND (c.relname LIKE 'chat_%' OR c.relname LIKE 'chatscale_%') ORDER BY c.relname`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v chatscaleRelation
			if err := rows.Scan(&v.Name, &v.HeapBytes, &v.IndexBytes, &v.TotalBytes, &v.LiveEstimate, &v.DeadEstimate, &v.VacuumCount, &v.AutoVacuumCount, &v.Options); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

type chatscaleWorkloadResult struct {
	Ratio      map[string]int       `json:"ratio_percent"`
	Operations int                  `json:"operations"`
	ElapsedMS  float64              `json:"elapsed_ms"`
	Timings    map[string][]float64 `json:"timings_ms"`
	Pruned     int64                `json:"outbox_pruned"`
	PruneMS    float64              `json:"outbox_prune_ms"`
}

func chatscaleWorkload(t *testing.T, s *Store, phase string) chatscaleWorkloadResult {
	t.Helper()
	ctx := context.Background()
	a := NewAdapter(s)
	r := NewRecipientStateStore(s)
	p := chat.Principal{TenantID: chatscaleTenant, SubjectID: chatscaleReader}
	out := chatscaleWorkloadResult{Ratio: map[string]int{"open-channel": 30, "page-back": 20, "send": 15, "mark-read": 10, "sidebar": 10, "search": 5, "thread": 5, "outbox": 5}, Operations: 100, Timings: map[string][]float64{}}
	startAll := time.Now()
	for i := 0; i < 100; i++ {
		start := time.Now()
		name := ""
		var err error
		// Permute the slots so sends, reads and counts are mixed rather than runs
		// of one operation. The proportions are deterministic across fixture sizes.
		slot := (i * 37) % 100
		switch {
		case slot < 30:
			name = "open-channel"
			var page chat.ListPostsResponse
			page, err = a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 50}, chat.PostWindow{Descending: true})
			if err == nil && len(page.Posts) != 50 {
				err = fmt.Errorf("open returned %d posts", len(page.Posts))
			}
		case slot < 50:
			name = "page-back"
			var page chat.ListPostsResponse
			page, err = a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 50}, chat.PostWindow{Descending: true, BeforeSequence: uint64(chatscaleSize() * 3 / 5 / 8 / 4)})
			if err == nil && len(page.Posts) != 50 {
				err = fmt.Errorf("page-back returned %d posts", len(page.Posts))
			}
		case slot < 65:
			name = "send"
			_, err = s.sendPostRaw(ctx, SendRequest{TenantID: chatscaleTenant, ConversationID: chatscaleRoom, AuthorID: chatscaleReader, ClientKey: fmt.Sprintf("chatscale-mixed-%s-%d", phase, i), Body: "Payroll follow-up", References: []byte("[]")})
		case slot < 75:
			name = "mark-read"
			var state chat.ReadState
			state, err = a.GetReadState(ctx, chatscaleTenant, chatscaleRoom, chatscaleTenant, chatscaleReader)
			if err == nil {
				_, err = a.PutReadState(ctx, state, state.Revision)
			}
		case slot < 85:
			name = "sidebar"
			_, err = r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, chatscaleRooms(200))
		case slot < 90:
			name = "search"
			_, err = a.Search(ctx, chat.SearchRequest{Principal: p, TenantID: chatscaleTenant, ConversationID: chatscaleRoom, Query: "payroll", Page: chat.Page{PageSize: 20}})
		case slot < 95:
			name = "thread"
			var snapshot chat.ThreadSnapshot
			snapshot, err = a.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: chatscaleTenant, SubjectID: "chatscale-u-1"}, TenantID: chatscaleTenant, ConversationID: chatscaleRoom, ThreadID: "chatscale-p-0000000001", InvokingPostID: "chatscale-p-0000000001", Limit: 50})
			if err == nil && (len(snapshot.Posts) != 2 || snapshot.Posts[0].ID != "chatscale-p-0000000001" || snapshot.Posts[1].ID != "chatscale-p-0000000005") {
				err = fmt.Errorf("thread snapshot lost its root or reply: %+v", snapshot.Posts)
			}
		default:
			name = "outbox"
			_, err = s.PendingOutboxAfter(ctx, chatscaleTenant, int64(chatscaleSize()*4/5), 500)
		}
		if err != nil {
			t.Fatalf("mixed %s/%s operation %d: %v", phase, name, i, err)
		}
		out.Timings[name] = append(out.Timings[name], float64(time.Since(start).Microseconds())/1000)
	}
	out.ElapsedMS = float64(time.Since(startAll).Microseconds()) / 1000
	start := time.Now()
	n, err := s.PruneOutbox(ctx, chatscaleTenant, time.Now(), 500)
	if err != nil || n != 500 {
		t.Fatalf("bounded outbox prune=%d err=%v", n, err)
	}
	out.Pruned = n
	out.PruneMS = float64(time.Since(start).Microseconds()) / 1000
	t.Logf("WORKLOAD %s operations=%d time=%.3fms prune=%d prune-time=%.3fms", phase, out.Operations, out.ElapsedMS, out.Pruned, out.PruneMS)
	return out
}

func TestTodo_CHATSCALE_001_Workload(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	// Use the small shape test's available depth. The actual volume run invokes
	// this same workload after the read plans have been measured.
	result := chatscaleWorkload(t, s, "shape")
	total := 0
	for name, percent := range result.Ratio {
		total += percent
		if len(result.Timings[name]) != percent {
			t.Fatalf("%s operations=%d want %d", name, len(result.Timings[name]), percent)
		}
	}
	if total != 100 || result.Pruned != 500 {
		t.Fatalf("workload ratio=%d prune=%d", total, result.Pruned)
	}
	relations := chatscaleRelations(t, s)
	if len(relations) < 10 {
		t.Fatalf("missing relation size inventory: %d", len(relations))
	}
	raw, err := json.Marshal(relations)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RELATIONS %s", raw)
}
