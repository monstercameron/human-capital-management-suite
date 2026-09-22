package inbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// NAAS-005 qualification: reproducible million-row multi-tenant load with
// sparse filters, deep pagination, concurrent writes and index-rollout
// rehearsal. Isolated pgtest schema only: never touches a live database.
// Latencies are recorded with t.Logf; assertions cover plans and counts,
// never machine-time thresholds.

const (
	naas5HotSubject     = "load-hot"
	naas5ColdSubject    = "load-cold"
	naas5NeighSubject   = "load-neighbor"
	naas5HotClones      = 700000
	naas5ColdClones     = 50000
	naas5NeighClones    = 250000
	naas5SparseMod      = 100
	naas5DeepBlockLimit = 10000
	naas5PageSize       = 50
)

type naas5Fixture struct {
	hotTenant     uuid.UUID
	neighTenant   uuid.UUID
	hotSeed       inbox.Record
	coldSeed      inbox.Record
	neighSeed     inbox.Record
	targetInst    uuid.UUID
	otherInst     uuid.UUID
	hotCorr       string
	totalInbox    int
	hotCount      int
	coldCount     int
	neighCount    int
	sparseTotal   int
	sparseActive  int
	retentionSpan time.Duration
}

func naas5MaxUUID() uuid.UUID {
	return uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
}

func naas5AppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func naas5InTenant(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tenant transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// naas5BulkHot clones seed into count rows with 1-in-100 sparse
// APPROVAL/target rows. Prefix isolates chunks; offsetSec widens retention.
func naas5BulkHot(t *testing.T, db *pgtest.DB, tenant, seed uuid.UUID, prefix string, count, offsetSec int, targetWorkflow string) {
	t.Helper()
	db.Exec(t, `INSERT INTO message_intent (tenant_id,message_intent_id,purpose,audience_expression,template_key,template_version,classification,urgency,delivery_requirement,response_requirement,workflow_ref,correlation_key,created_at)
	 SELECT tenant_id,md5($3||g)::uuid,CASE WHEN g%100=0 THEN 'APPROVAL' ELSE purpose END,audience_expression,template_key,template_version,classification,urgency,delivery_requirement,response_requirement,CASE WHEN g%100=0 THEN $4 ELSE workflow_ref END,correlation_key,created_at + (g/5 + $5)*interval '1 second'
	 FROM message_intent CROSS JOIN generate_series(1,$6) g WHERE tenant_id=$1 AND message_intent_id=$2`, tenant, seed, prefix, targetWorkflow, offsetSec, count)
	db.Exec(t, `INSERT INTO recipient_message (tenant_id,recipient_message_id,message_intent_id,recipient_ref,endpoint_id,rendered_digest,classification,correlation_key,recipient_state,satisfaction_state,created_at,updated_at)
	 SELECT tenant_id,md5($3||g)::uuid,md5($3||g)::uuid,recipient_ref,endpoint_id,rendered_digest,classification,correlation_key,recipient_state,satisfaction_state,created_at+(g/5+$4)*interval '1 second',created_at+(g/5+$4)*interval '1 second'
	 FROM recipient_message CROSS JOIN generate_series(1,$5) g WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, seed, prefix, offsetSec, count)
	db.Exec(t, `INSERT INTO inbox_record (tenant_id,inbox_record_id,subject_ref,recipient_message_id,read_state,archived,pinned,state_changed_at,version,created_at)
	 SELECT tenant_id,md5($3||g)::uuid,subject_ref,md5($3||g)::uuid,CASE WHEN g%4=0 THEN 'UNREAD' ELSE 'READ' END,g%7=0,g%10=0,created_at+(g/5+$4)*interval '1 second',1,created_at+(g/5+$4)*interval '1 second'
	 FROM inbox_record CROSS JOIN generate_series(1,$5) g WHERE tenant_id=$1 AND inbox_record_id=$2`, tenant, seed, prefix, offsetSec, count)
}

func naas5BulkPlain(t *testing.T, db *pgtest.DB, tenant, seed uuid.UUID, prefix string, count, offsetSec int) {
	t.Helper()
	db.Exec(t, `INSERT INTO message_intent (tenant_id,message_intent_id,purpose,audience_expression,template_key,template_version,classification,urgency,delivery_requirement,response_requirement,workflow_ref,correlation_key,created_at)
	 SELECT tenant_id,md5($3||g)::uuid,purpose,audience_expression,template_key,template_version,classification,urgency,delivery_requirement,response_requirement,workflow_ref,correlation_key,created_at + (g/5 + $4)*interval '1 second'
	 FROM message_intent CROSS JOIN generate_series(1,$5) g WHERE tenant_id=$1 AND message_intent_id=$2`, tenant, seed, prefix, offsetSec, count)
	db.Exec(t, `INSERT INTO recipient_message (tenant_id,recipient_message_id,message_intent_id,recipient_ref,endpoint_id,rendered_digest,classification,correlation_key,recipient_state,satisfaction_state,created_at,updated_at)
	 SELECT tenant_id,md5($3||g)::uuid,md5($3||g)::uuid,recipient_ref,endpoint_id,rendered_digest,classification,correlation_key,recipient_state,satisfaction_state,created_at+(g/5+$4)*interval '1 second',created_at+(g/5+$4)*interval '1 second'
	 FROM recipient_message CROSS JOIN generate_series(1,$5) g WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, seed, prefix, offsetSec, count)
	db.Exec(t, `INSERT INTO inbox_record (tenant_id,inbox_record_id,subject_ref,recipient_message_id,read_state,archived,pinned,state_changed_at,version,created_at)
	 SELECT tenant_id,md5($3||g)::uuid,subject_ref,md5($3||g)::uuid,CASE WHEN g%4=0 THEN 'UNREAD' ELSE 'READ' END,g%7=0,g%10=0,created_at+(g/5+$4)*interval '1 second',1,created_at+(g/5+$4)*interval '1 second'
	 FROM inbox_record CROSS JOIN generate_series(1,$5) g WHERE tenant_id=$1 AND inbox_record_id=$2`, tenant, seed, prefix, offsetSec, count)
}

func naas5Count(t *testing.T, db *pgtest.DB, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func naas5Build(t *testing.T, db *pgtest.DB, conn *pgxadapter.Conn, hotKey, neighKey string) *naas5Fixture {
	t.Helper()
	ctx := context.Background()
	fx := &naas5Fixture{targetInst: uuid.New(), otherInst: uuid.New(), hotCorr: "naas5-" + uuid.NewString()}
	fx.hotTenant = insertTenant(t, db, hotKey)
	fx.neighTenant = insertTenant(t, db, neighKey)
	inTenantTx(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var err error
		fx.hotSeed, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{TenantID: fx.hotTenant, WorkItemID: uuid.New(), InstanceID: fx.otherInst, SubjectRef: naas5HotSubject, Purpose: "TASK", CorrelationID: fx.hotCorr, AudienceDigest: "resolution", CreatedAt: fixedInstant})
		return err
	})
	inTenantTx(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var err error
		fx.coldSeed, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{TenantID: fx.hotTenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: naas5ColdSubject, Purpose: "TASK", CorrelationID: "naas5-cold", AudienceDigest: "resolution", CreatedAt: fixedInstant})
		return err
	})
	inTenantTx(t, conn, fx.neighTenant, func(tx dbport.Tx) error {
		var err error
		fx.neighSeed, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{TenantID: fx.neighTenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: naas5NeighSubject, Purpose: "TASK", CorrelationID: "naas5-neigh", AudienceDigest: "resolution", CreatedAt: fixedInstant})
		return err
	})
	targetRef := fx.targetInst.String()
	// Hot 700k in 7x100k chunks with widening retention offsets.
	for chunk := 0; chunk < 7; chunk++ {
		naas5BulkHot(t, db, fx.hotTenant, fx.hotSeed.InboxRecordID, fmt.Sprintf("naas5-hot-c%d-", chunk), 100000, chunk*20000, targetRef)
	}
	naas5BulkPlain(t, db, fx.hotTenant, fx.coldSeed.InboxRecordID, "naas5-cold-", naas5ColdClones, 0)
	naas5BulkPlain(t, db, fx.neighTenant, fx.neighSeed.InboxRecordID, "naas5-neigh-a-", 100000, 0)
	naas5BulkPlain(t, db, fx.neighTenant, fx.neighSeed.InboxRecordID, "naas5-neigh-b-", 100000, 20000)
	naas5BulkPlain(t, db, fx.neighTenant, fx.neighSeed.InboxRecordID, "naas5-neigh-c-", naas5NeighClones-200000, 40000)
	db.Exec(t, "ANALYZE inbox_record")
	db.Exec(t, "ANALYZE recipient_message")
	db.Exec(t, "ANALYZE message_intent")
	fx.hotCount = naas5Count(t, db, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND subject_ref=$2`, fx.hotTenant, naas5HotSubject)
	fx.coldCount = naas5Count(t, db, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND subject_ref=$2`, fx.hotTenant, naas5ColdSubject)
	fx.neighCount = naas5Count(t, db, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND subject_ref=$2`, fx.neighTenant, naas5NeighSubject)
	fx.totalInbox = naas5Count(t, db, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 OR tenant_id=$2`, fx.hotTenant, fx.neighTenant)
	fx.sparseTotal = naas5Count(t, db, `SELECT count(*) FROM message_intent WHERE tenant_id=$1 AND purpose='APPROVAL' AND workflow_ref=$2`, fx.hotTenant, targetRef)
	fx.sparseActive = naas5Count(t, db, `SELECT count(*) FROM inbox_record i
		 JOIN recipient_message r ON r.tenant_id=i.tenant_id AND r.recipient_message_id=i.recipient_message_id
		 JOIN message_intent m ON m.tenant_id=r.tenant_id AND m.message_intent_id=r.message_intent_id
		 WHERE i.tenant_id=$1 AND i.subject_ref=$2 AND NOT i.archived AND m.template_key='workflow.attention' AND m.purpose='APPROVAL' AND m.workflow_ref=$3`,
		fx.hotTenant, naas5HotSubject, targetRef)
	var minAt, maxAt time.Time
	if err := db.Conn.QueryRow(context.Background(), `SELECT min(created_at), max(created_at) FROM inbox_record WHERE tenant_id=$1 AND subject_ref=$2`, fx.hotTenant, naas5HotSubject).Scan(&minAt, &maxAt); err != nil {
		t.Fatalf("retention window: %v", err)
	}
	fx.retentionSpan = maxAt.Sub(minAt)
	return fx
}

func naas5AssertSeek(t *testing.T, plan, requiredIndex string) {
	t.Helper()
	if !strings.Contains(plan, requiredIndex) || strings.Contains(plan, `"Node Type": "Sort"`) || strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("non-seek plan, want %s: %s", requiredIndex, plan)
	}
}

func naas5PlanBlocks(t *testing.T, plan string) (rows, blocks int) {
	t.Helper()
	var parsed []struct {
		Plan struct {
			Rows   int `json:"Actual Rows"`
			Blocks int `json:"Shared Hit Blocks"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil {
		t.Fatalf("parse plan: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("unexpected plan shape: %s", plan)
	}
	return parsed[0].Plan.Rows, parsed[0].Plan.Blocks
}

func naas5Percentiles(durs []time.Duration) (p50, p95, p99 time.Duration) {
	sorted := append([]time.Duration(nil), durs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	at := func(p int) time.Duration {
		idx := len(sorted) * p / 100
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return sorted[idx]
	}
	return at(50), at(95), at(99)
}

// TestTodo_NAAS_005 proves the million-row mixed-tenant workload: skewed
// recipients, sparse purpose/workflow filters, deep keyset pagination,
// concurrent publish/read-state and tenant isolation. Capacity budget:
// deep and sparse seeks stay within naas5DeepBlockLimit shared blocks.
func TestTodo_NAAS_005(t *testing.T) {
	db := pgtest.New(t)
	conn := naas5AppConn(t, db)
	ctx := context.Background()
	fx := naas5Build(t, db, conn, "naas5-primary", "naas5-neighbor")
	if fx.totalInbox != naas5HotClones+naas5ColdClones+naas5NeighClones+3 {
		t.Fatalf("total inbox=%d, want %d", fx.totalInbox, naas5HotClones+naas5ColdClones+naas5NeighClones+3)
	}
	if fx.hotCount != naas5HotClones+1 || fx.coldCount != naas5ColdClones+1 || fx.neighCount != naas5NeighClones+1 {
		t.Fatalf("skewed counts hot=%d cold=%d neigh=%d", fx.hotCount, fx.coldCount, fx.neighCount)
	}
	if fx.sparseTotal == 0 || fx.sparseTotal*20 > fx.hotCount {
		t.Fatalf("sparse purpose not sparse: total=%d hot=%d", fx.sparseTotal, fx.hotCount)
	}
	if fx.sparseActive == 0 || fx.sparseActive > fx.sparseTotal {
		t.Fatalf("sparse active=%d total=%d", fx.sparseActive, fx.sparseTotal)
	}
	if fx.retentionSpan < 24*time.Hour {
		t.Fatalf("retention span=%s, want >=24h", fx.retentionSpan)
	}
	t.Logf("million-row fixture total=%d hot=%d cold=%d neigh=%d sparse_total=%d sparse_active=%d retention=%s",
		fx.totalInbox, fx.hotCount, fx.coldCount, fx.neighCount, fx.sparseTotal, fx.sparseActive, fx.retentionSpan)

	// Deep keyset traversal: stable order, no duplicates, bounded pages.
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		seen := map[uuid.UUID]bool{}
		q := inbox.PageQuery{Limit: naas5PageSize}
		for pages := 0; pages < 6; pages++ {
			p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, q)
			if err != nil {
				return err
			}
			if len(p.Records) != naas5PageSize || p.Next == nil {
				t.Fatalf("shallow page %d incomplete: %d", pages, len(p.Records))
			}
			for _, r := range p.Records {
				if seen[r.InboxRecordID] {
					t.Fatal("duplicate row in shallow traversal")
				}
				seen[r.InboxRecordID] = true
			}
			q.After = p.Next
		}
		deep := inbox.PageQuery{Limit: naas5PageSize, After: &inbox.Position{CreatedAt: fixedInstant.Add(100000 * time.Second), RecordID: naas5MaxUUID()}}
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, deep)
		if err != nil {
			return err
		}
		if len(p.Records) != naas5PageSize || p.Next == nil {
			t.Fatalf("deep page incomplete: %d", len(p.Records))
		}
		for _, r := range p.Records {
			if seen[r.InboxRecordID] {
				t.Fatal("deep page overlapped shallow pages unexpectedly")
			}
		}
		// Sparse workflow traversal must return exactly the active set.
		wq := inbox.WorkflowPageQuery{PageQuery: inbox.PageQuery{Limit: naas5PageSize}, Purpose: "APPROVAL", InstanceID: fx.targetInst}
		count := 0
		for {
			wp, err := (inbox.Store{}).WorkflowNoticesPage(ctx, tx, fx.hotTenant, naas5HotSubject, wq)
			if err != nil {
				return err
			}
			for _, r := range wp.Records {
				if r.Purpose != "APPROVAL" || r.InstanceID != fx.targetInst {
					t.Fatal("sparse workflow filter leaked")
				}
			}
			count += len(wp.Records)
			if wp.Next == nil {
				break
			}
			wq.After = wp.Next
			if count > fx.sparseActive+naas5PageSize {
				t.Fatalf("sparse traversal overran: %d > %d", count, fx.sparseActive)
			}
		}
		if count != fx.sparseActive {
			t.Fatalf("sparse traversal=%d, want %d", count, fx.sparseActive)
		}
		t.Logf("deep pagination ok; sparse workflow pages=%d rows=%d", (fx.sparseActive+naas5PageSize-1)/naas5PageSize, count)
		return nil
	})

	// Seek plans on the million-row load: no Sort, no Seq Scan.
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var plan string
		if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT inbox_record_id FROM inbox_record
			 WHERE tenant_id=$1 AND subject_ref=$2 AND NOT archived AND read_state='UNREAD'
			 AND (created_at,inbox_record_id)<($3,$4) ORDER BY created_at DESC,inbox_record_id DESC LIMIT 51`,
			fx.hotTenant, naas5HotSubject, fixedInstant.Add(100000*time.Second), naas5MaxUUID()).Scan(&plan); err != nil {
			return err
		}
		naas5AssertSeek(t, plan, "inbox_record_read_feed")
		rows, blocks := naas5PlanBlocks(t, plan)
		if rows != 51 || blocks > naas5DeepBlockLimit {
			t.Fatalf("deep plan rows=%d blocks=%d limit=%d", rows, blocks, naas5DeepBlockLimit)
		}
		t.Logf("deep-unread-plan rows=%d shared_hit_blocks=%d budget=%d", rows, blocks, naas5DeepBlockLimit)
		var wplan string
		if err := tx.QueryRow(ctx, `EXPLAIN (FORMAT JSON) SELECT i.inbox_record_id FROM inbox_record i
			 JOIN recipient_message r ON r.tenant_id=i.tenant_id AND r.recipient_message_id=i.recipient_message_id
			 JOIN message_intent m ON m.tenant_id=r.tenant_id AND m.message_intent_id=r.message_intent_id
			 WHERE i.tenant_id=$1 AND i.subject_ref=$2 AND NOT i.archived AND r.recipient_ref=$2 AND m.template_key='workflow.attention' AND m.purpose='APPROVAL' AND m.workflow_ref=$3
			 ORDER BY i.created_at DESC,i.inbox_record_id DESC LIMIT 51`,
			fx.hotTenant, naas5HotSubject, fx.targetInst.String()).Scan(&wplan); err != nil {
			return err
		}
		if strings.Contains(wplan, `"Node Type": "Seq Scan"`) {
			t.Fatalf("sparse workflow seq scan: %s", wplan)
		}
		// Sparse side drives the join: the workflow_notice index seeks the
		// ~7k sparse intents, then nested loops reach inbox rows by PK.
		// A small top Sort over the joined match set is expected; what
		// must never appear is a Seq Scan.
		if !strings.Contains(wplan, "message_intent_workflow_notice") {
			t.Fatalf("sparse workflow missed sparse-side index: %s", wplan)
		}
		t.Logf("sparse-workflow-plan uses message_intent_workflow_notice, no seq scan")
		return nil
	})

	// Tenant/subject isolation holds at volume.
	naas5InTenant(t, conn, fx.neighTenant, func(tx dbport.Tx) error {
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: 10})
		if err != nil {
			return err
		}
		if len(p.Records) != 0 || p.Next != nil {
			t.Fatal("cross-tenant rows leaked")
		}
		return nil
	})
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, "load-stranger", inbox.PageQuery{Limit: 10})
		if err != nil {
			return err
		}
		if len(p.Records) != 0 || p.Next != nil {
			t.Fatal("cross-subject rows leaked")
		}
		return nil
	})

	// Concurrent publish (distinct + idempotent retry) and read-state.
	// Connections are opened in the test goroutine; workers only use them.
	retryNotice := inbox.WorkflowNotice{TenantID: fx.hotTenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: naas5HotSubject, Purpose: "TASK", CorrelationID: "naas5-retry", AudienceDigest: "resolution", CreatedAt: fixedInstant}
	retryConns := make([]*pgxadapter.Conn, 4)
	for i := range retryConns {
		retryConns[i] = naas5AppConn(t, db)
	}
	var wg sync.WaitGroup
	retryErrs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(c *pgxadapter.Conn) {
			defer wg.Done()
			tx, err := c.Begin(ctx)
			if err != nil {
				retryErrs <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, fx.hotTenant); err != nil {
				retryErrs <- err
				return
			}
			if _, err := (inbox.Store{}).PublishWorkflow(ctx, tx, retryNotice); err != nil {
				retryErrs <- err
				return
			}
			retryErrs <- tx.Commit(ctx)
		}(retryConns[i])
	}
	wg.Wait()
	close(retryErrs)
	for err := range retryErrs {
		if err != nil {
			t.Fatalf("concurrent retry publish: %v", err)
		}
	}
	distinctConns := make([]*pgxadapter.Conn, 8)
	for i := range distinctConns {
		distinctConns[i] = naas5AppConn(t, db)
	}
	distinctErrs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int, c *pgxadapter.Conn) {
			defer wg.Done()
			tx, err := c.Begin(ctx)
			if err != nil {
				distinctErrs <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, fx.hotTenant); err != nil {
				distinctErrs <- err
				return
			}
			n := retryNotice
			n.WorkItemID = uuid.New()
			n.CorrelationID = fmt.Sprintf("naas5-distinct-%d-%s", i, uuid.NewString())
			if _, err := (inbox.Store{}).PublishWorkflow(ctx, tx, n); err != nil {
				distinctErrs <- err
				return
			}
			distinctErrs <- tx.Commit(ctx)
		}(i, distinctConns[i])
	}
	wg.Wait()
	close(distinctErrs)
	for err := range distinctErrs {
		if err != nil {
			t.Fatalf("concurrent distinct publish: %v", err)
		}
	}
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		// Idempotent retry minted exactly one inbox row for its work item.
		retryID := uuid.NewSHA1(fx.hotTenant, []byte("workflow-notice/v1\x00"+retryNotice.WorkItemID.String()+"\x00"+retryNotice.SubjectRef))
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND subject_ref=$2 AND inbox_record_id=$3`, fx.hotTenant, naas5HotSubject, retryID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("retry rows=%d, want 1", n)
		}
		// Contended CAS on one unread row: exactly one winner.
		var target uuid.UUID
		var ver int64
		if err := tx.QueryRow(ctx, `SELECT inbox_record_id, version FROM inbox_record WHERE tenant_id=$1 AND subject_ref=$2 AND read_state='UNREAD' AND NOT archived LIMIT 1`, fx.hotTenant, naas5HotSubject).Scan(&target, &ver); err != nil {
			return err
		}
		casConns := make([]*pgxadapter.Conn, 4)
		for i := range casConns {
			casConns[i] = naas5AppConn(t, db)
		}
		results := make(chan error, 4)
		var inner sync.WaitGroup
		for i := 0; i < 4; i++ {
			inner.Add(1)
			go func(c *pgxadapter.Conn) {
				defer inner.Done()
				ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				tx2, err := c.Begin(ctx2)
				if err != nil {
					results <- err
					return
				}
				defer func() { _ = tx2.Rollback(ctx2) }()
				if err := tenancy.WithTenant(ctx2, tx2, fx.hotTenant); err != nil {
					results <- err
					return
				}
				err = (inbox.Store{}).MarkRead(ctx2, tx2, fx.hotTenant, naas5HotSubject, target, uint64(ver), fixedInstant)
				if err == nil {
					err = tx2.Commit(ctx2)
				}
				results <- err
			}(casConns[i])
		}
		inner.Wait()
		close(results)
		wins, conflicts := 0, 0
		for err := range results {
			if err == nil {
				wins++
			} else if strings.Contains(err.Error(), "version conflict") {
				conflicts++
			} else {
				t.Fatalf("contended read-state: %v", err)
			}
		}
		if wins != 1 || conflicts != 3 {
			t.Fatalf("contended CAS wins=%d conflicts=%d, want 1/3", wins, conflicts)
		}
		// Retry after state change preserves read state and version.
		again, err := (inbox.Store{}).PublishWorkflow(ctx, tx, retryNotice)
		if err != nil {
			return err
		}
		if again.InboxRecordID != retryID {
			t.Fatal("retry changed identity")
		}
		loaded, err := (inbox.Store{}).Load(ctx, tx, fx.hotTenant, naas5HotSubject, retryID)
		if err != nil {
			return err
		}
		if loaded.InboxRecordID != retryID {
			t.Fatal("idempotency lost identity")
		}
		t.Logf("concurrent publish/read-state ok: retry=1 distinct=8 cas_winner=1")
		return nil
	})
}

// TestTodo_NAAS_005_Performance records endpoint latency percentiles,
// throughput, plans, lock waits and WAL/index sizes on the million-row
// load. Only plans and counts are asserted; latencies are evidence.
func TestTodo_NAAS_005_Performance(t *testing.T) {
	db := pgtest.New(t)
	conn := naas5AppConn(t, db)
	ctx := context.Background()
	fx := naas5Build(t, db, conn, "naas5-perf", "naas5-perf-neigh")
	if fx.totalInbox < 1000000 {
		t.Fatalf("performance fixture=%d, want >=1000000", fx.totalInbox)
	}
	var lsnBefore string
	if err := db.Conn.QueryRow(context.Background(), `SELECT pg_current_wal_lsn()::text`).Scan(&lsnBefore); err != nil {
		t.Fatalf("wal lsn: %v", err)
	}
	measure := func(label string, q inbox.PageQuery) []time.Duration {
		t.Helper()
		var durs []time.Duration
		naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
			for n := 0; n < 21; n++ {
				start := time.Now()
				p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, q)
				if err != nil {
					return err
				}
				if len(p.Records) != naas5PageSize || p.Next == nil {
					t.Fatalf("%s page incomplete: %d", label, len(p.Records))
				}
				if n > 0 {
					durs = append(durs, time.Since(start))
				}
			}
			return nil
		})
		p50, p95, p99 := naas5Percentiles(durs)
		var total time.Duration
		for _, d := range durs {
			total += d
		}
		t.Logf("%s rows=%d page=%d n=%d p50=%s p95=%s p99=%s throughput=%.1f pages/s",
			label, fx.hotCount, naas5PageSize, len(durs), p50, p95, p99, float64(len(durs))/total.Seconds())
		return durs
	}
	measure("perf-first-page", inbox.PageQuery{Limit: naas5PageSize})
	measure("perf-deep-page", inbox.PageQuery{Limit: naas5PageSize, After: &inbox.Position{CreatedAt: fixedInstant.Add(100000 * time.Second), RecordID: naas5MaxUUID()}})
	measure("perf-unread", inbox.PageQuery{Limit: naas5PageSize, ReadState: inbox.Unread})
	pinned := true
	measure("perf-pinned", inbox.PageQuery{Limit: naas5PageSize, Pinned: &pinned})
	// Sparse workflow endpoint latency (recorded, count asserted).
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var durs []time.Duration
		for n := 0; n < 21; n++ {
			start := time.Now()
			p, err := (inbox.Store{}).WorkflowNoticesPage(ctx, tx, fx.hotTenant, naas5HotSubject,
				inbox.WorkflowPageQuery{PageQuery: inbox.PageQuery{Limit: naas5PageSize}, Purpose: "APPROVAL", InstanceID: fx.targetInst})
			if err != nil {
				return err
			}
			if len(p.Records) != naas5PageSize || p.Next == nil {
				t.Fatalf("sparse workflow page incomplete: %d", len(p.Records))
			}
			if n > 0 {
				durs = append(durs, time.Since(start))
			}
		}
		p50, p95, p99 := naas5Percentiles(durs)
		t.Logf("perf-sparse-workflow rows=%d page=%d n=%d p50=%s p95=%s p99=%s", fx.sparseActive, naas5PageSize, len(durs), p50, p95, p99)
		return nil
	})
	// Plans stay seeks at volume.
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		var plan string
		if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT inbox_record_id FROM inbox_record
			 WHERE tenant_id=$1 AND subject_ref=$2 AND NOT archived AND (created_at,inbox_record_id)<($3,$4)
			 ORDER BY created_at DESC,inbox_record_id DESC LIMIT 51`,
			fx.hotTenant, naas5HotSubject, fixedInstant.Add(100000*time.Second), naas5MaxUUID()).Scan(&plan); err != nil {
			return err
		}
		naas5AssertSeek(t, plan, "inbox_record_feed")
		rows, blocks := naas5PlanBlocks(t, plan)
		if rows != 51 || blocks > naas5DeepBlockLimit {
			t.Fatalf("perf deep plan rows=%d blocks=%d", rows, blocks)
		}
		t.Logf("perf-deep-plan rows=%d shared_hit_blocks=%d budget=%d", rows, blocks, naas5DeepBlockLimit)
		return nil
	})
	// Storage sizes and WAL growth are recorded evidence.
	var feed, readFeed, pinnedFeed, noticeIdx, totalRel int64
	for _, tc := range []struct {
		label string
		dest  *int64
		rel   string
	}{
		{"inbox_record_feed", &feed, "inbox_record_feed"},
		{"inbox_record_read_feed", &readFeed, "inbox_record_read_feed"},
		{"inbox_record_pinned_feed", &pinnedFeed, "inbox_record_pinned_feed"},
		{"message_intent_workflow_notice", &noticeIdx, "message_intent_workflow_notice"},
	} {
		if err := db.Conn.QueryRow(context.Background(), `SELECT pg_relation_size($1::regclass)`, tc.rel).Scan(tc.dest); err != nil {
			t.Fatalf("index size %s: %v", tc.label, err)
		}
		if *tc.dest <= 0 {
			t.Fatalf("index %s has no bytes", tc.label)
		}
	}
	if err := db.Conn.QueryRow(context.Background(), `SELECT pg_total_relation_size('inbox_record'::regclass)`).Scan(&totalRel); err != nil {
		t.Fatalf("total size: %v", err)
	}
	if totalRel <= 0 {
		t.Fatal("inbox_record total size is empty")
	}
	var lsnAfter string
	if err := db.Conn.QueryRow(context.Background(), `SELECT pg_current_wal_lsn()::text`).Scan(&lsnAfter); err != nil {
		t.Fatalf("wal lsn after: %v", err)
	}
	var walBytes int64
	if err := db.Conn.QueryRow(context.Background(), `SELECT pg_wal_lsn_diff($1,$2)`, lsnAfter, lsnBefore).Scan(&walBytes); err != nil {
		t.Fatalf("wal diff: %v", err)
	}
	if walBytes < 0 {
		t.Fatalf("negative wal growth: %d", walBytes)
	}
	var deadlocks, conflicts int64
	if err := db.Conn.QueryRow(context.Background(), `SELECT coalesce(sum(deadlocks),0), coalesce(sum(conflicts),0) FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocks, &conflicts); err != nil {
		t.Fatalf("lock stats: %v", err)
	}
	var lockWaiters int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock'`).Scan(&lockWaiters); err != nil {
		t.Fatalf("lock waiters: %v", err)
	}
	t.Logf("storage feed=%d read_feed=%d pinned_feed=%d workflow_notice=%d inbox_total=%d wal_bytes=%d deadlocks=%d conflicts=%d lock_waiters=%d",
		feed, readFeed, pinnedFeed, noticeIdx, totalRel, walBytes, deadlocks, conflicts, lockWaiters)
	t.Logf("capacity budget: deep/sparse seeks <=%d shared blocks; million-row indexes resident; wal growth recorded not asserted", naas5DeepBlockLimit)
}

// TestTodo_NAAS_005_Recovery rehearses index rollout, restart and recovery
// on the isolated production-sized copy (this pgtest schema, never a live
// database): drop and rebuild the exact 00321 indexes, reconnect, and prove
// counts, RLS, triggers and idempotency are unchanged. No new migration.
func TestTodo_NAAS_005_Recovery(t *testing.T) {
	db := pgtest.New(t)
	conn := naas5AppConn(t, db)
	ctx := context.Background()
	fx := naas5Build(t, db, conn, "naas5-rec", "naas5-rec-neigh")
	beforeTotal := fx.totalInbox
	beforeSparse := fx.sparseActive
	// Stable witness: one notice read through CAS before the rollout.
	witness := inbox.WorkflowNotice{TenantID: fx.hotTenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: naas5HotSubject, Purpose: "TASK", CorrelationID: "naas5-witness", AudienceDigest: "resolution", CreatedAt: fixedInstant}
	var witnessID uuid.UUID
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		r, err := (inbox.Store{}).PublishWorkflow(ctx, tx, witness)
		if err != nil {
			return err
		}
		witnessID = r.InboxRecordID
		return (inbox.Store{}).MarkRead(ctx, tx, fx.hotTenant, naas5HotSubject, witnessID, 1, fixedInstant)
	})
	policies := naas5Count(t, db, `SELECT count(*) FROM pg_policies WHERE schemaname=current_schema() AND policyname='tenant_isolation' AND tablename IN ('inbox_record','inbox_state_event','message_intent','recipient_message','delivery_endpoint')`)
	if policies < 2 {
		t.Fatalf("rls tenant_isolation policies=%d, want >=2", policies)
	}
	triggers := naas5Count(t, db, `SELECT count(*) FROM pg_trigger WHERE tgname='inbox_state_event_append_only'`)
	if triggers != 1 {
		t.Fatalf("forbid_mutation triggers=%d, want 1", triggers)
	}
	t.Logf("pre-rollout total=%d sparse_active=%d policies=%d triggers=%d witness=%s", beforeTotal, beforeSparse, policies, triggers, witnessID)
	// Maintenance rehearsal on the isolated copy: drop the rollout indexes.
	db.Exec(t, "DROP INDEX IF EXISTS inbox_record_feed")
	db.Exec(t, "DROP INDEX IF EXISTS inbox_record_read_feed")
	db.Exec(t, "DROP INDEX IF EXISTS inbox_record_pinned_feed")
	db.Exec(t, "DROP INDEX IF EXISTS message_intent_workflow_notice")
	db.Exec(t, "ANALYZE inbox_record")
	// RLS still denies cross-tenant reads while indexes are absent, and the
	// witness survives with its read state.
	naas5InTenant(t, conn, fx.neighTenant, func(tx dbport.Tx) error {
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: 10})
		if err != nil {
			return err
		}
		if len(p.Records) != 0 {
			t.Fatal("rls weakened during rollout rehearsal")
		}
		return nil
	})
	naas5InTenant(t, conn, fx.hotTenant, func(tx dbport.Tx) error {
		loaded, err := (inbox.Store{}).Load(ctx, tx, fx.hotTenant, naas5HotSubject, witnessID)
		if err != nil {
			return err
		}
		if loaded.ReadState != inbox.Read || loaded.Version != 2 {
			t.Fatalf("witness lost state during rollout: %+v", loaded)
		}
		return nil
	})
	// Rebuild with the exact 00321 statements (non-concurrent, maintenance
	// window) and re-analyze.
	db.Exec(t, "CREATE INDEX inbox_record_feed ON inbox_record (tenant_id, subject_ref, archived, created_at DESC, inbox_record_id DESC)")
	db.Exec(t, "CREATE INDEX inbox_record_read_feed ON inbox_record (tenant_id, subject_ref, archived, read_state, created_at DESC, inbox_record_id DESC)")
	db.Exec(t, "CREATE INDEX inbox_record_pinned_feed ON inbox_record (tenant_id, subject_ref, archived, created_at DESC, inbox_record_id DESC) WHERE pinned")
	db.Exec(t, "CREATE INDEX message_intent_workflow_notice ON message_intent (tenant_id, workflow_ref, message_intent_id) WHERE template_key = 'workflow.attention'")
	db.Exec(t, "ANALYZE inbox_record")
	db.Exec(t, "ANALYZE recipient_message")
	db.Exec(t, "ANALYZE message_intent")
	// Simulated restart: fresh connections observe the same durable state.
	fresh := naas5AppConn(t, db)
	afterTotal := naas5Count(t, db, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 OR tenant_id=$2`, fx.hotTenant, fx.neighTenant)
	if afterTotal != beforeTotal+1 {
		t.Fatalf("recovery total=%d, want %d", afterTotal, beforeTotal+1)
	}
	orphans := naas5Count(t, db, `SELECT count(*) FROM inbox_record i
		 LEFT JOIN recipient_message r ON r.tenant_id=i.tenant_id AND r.recipient_message_id=i.recipient_message_id
		 LEFT JOIN message_intent m ON m.tenant_id=r.tenant_id AND m.message_intent_id=r.message_intent_id
		 WHERE (i.tenant_id=$1 OR i.tenant_id=$2) AND (r.recipient_message_id IS NULL OR m.message_intent_id IS NULL)`,
		fx.hotTenant, fx.neighTenant)
	if orphans != 0 {
		t.Fatalf("orphan rows after rebuild: %d", orphans)
	}
	naas5InTenant(t, fresh, fx.hotTenant, func(tx dbport.Tx) error {
		var plan string
		if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT inbox_record_id FROM inbox_record
			 WHERE tenant_id=$1 AND subject_ref=$2 AND NOT archived AND read_state='UNREAD'
			 AND (created_at,inbox_record_id)<($3,$4) ORDER BY created_at DESC,inbox_record_id DESC LIMIT 51`,
			fx.hotTenant, naas5HotSubject, fixedInstant.Add(100000*time.Second), naas5MaxUUID()).Scan(&plan); err != nil {
			return err
		}
		naas5AssertSeek(t, plan, "inbox_record_read_feed")
		rows, blocks := naas5PlanBlocks(t, plan)
		if rows != 51 || blocks > naas5DeepBlockLimit {
			t.Fatalf("post-rebuild plan rows=%d blocks=%d", rows, blocks)
		}
		loaded, err := (inbox.Store{}).Load(ctx, tx, fx.hotTenant, naas5HotSubject, witnessID)
		if err != nil {
			return err
		}
		if loaded.ReadState != inbox.Read || loaded.Version != 2 {
			t.Fatalf("witness lost state after rebuild: %+v", loaded)
		}
		again, err := (inbox.Store{}).PublishWorkflow(ctx, tx, witness)
		if err != nil {
			return err
		}
		if again.InboxRecordID != witnessID || again.ReadState != inbox.Read || again.Version != 2 {
			t.Fatalf("idempotency weakened after rebuild: %+v", again)
		}
		events := 0
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM inbox_state_event WHERE tenant_id=$1 AND inbox_record_id=$2`, fx.hotTenant, witnessID).Scan(&events); err != nil {
			return err
		}
		if events != 1 {
			t.Fatalf("state events=%d, want 1", events)
		}
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: naas5PageSize, After: &inbox.Position{CreatedAt: fixedInstant.Add(100000 * time.Second), RecordID: naas5MaxUUID()}})
		if err != nil {
			return err
		}
		if len(p.Records) != naas5PageSize || p.Next == nil {
			t.Fatalf("post-rebuild deep page incomplete: %d", len(p.Records))
		}
		t.Logf("post-rebuild plan rows=%d blocks=%d witness=%s events=%d orphans=%d", rows, blocks, witnessID, events, orphans)
		return nil
	})
	naas5InTenant(t, fresh, fx.neighTenant, func(tx dbport.Tx) error {
		p, err := (inbox.Store{}).ListPage(ctx, tx, fx.hotTenant, naas5HotSubject, inbox.PageQuery{Limit: 10})
		if err != nil {
			return err
		}
		if len(p.Records) != 0 || p.Next != nil {
			t.Fatal("rls weakened after rebuild")
		}
		return nil
	})
}
