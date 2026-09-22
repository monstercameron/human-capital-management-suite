package inbox_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// Isolated PostgreSQL fixture only: never benchmarks or drops indexes in the
// live development database. Assert plan shape, not machine-dependent latency.
func TestTodo_NAAS_003_Performance(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "naas-volume")
	conn := appConn(t, db)
	ctx := context.Background()
	var seed inbox.Record
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		seed, err = (inbox.Store{}).PublishWorkflow(ctx, tx, inbox.WorkflowNotice{TenantID: tenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: "volume-owner", Purpose: "APPROVAL", CorrelationID: "volume", AudienceDigest: "resolution", CreatedAt: fixedInstant})
		return err
	})
	// Set-based population includes old/read/archived/pinned rows and timestamp
	// ties. Every notification retains its real FK chain and RLS policies.
	db.Exec(t, `INSERT INTO message_intent (tenant_id,message_intent_id,purpose,audience_expression,template_key,template_version,classification,urgency,delivery_requirement,response_requirement,workflow_ref,correlation_key,created_at)
	 SELECT tenant_id,md5('volume-'||g)::uuid,purpose,audience_expression,template_key,template_version,classification,urgency,delivery_requirement,response_requirement,workflow_ref,correlation_key,created_at + (g/5)*interval '1 second'
	 FROM message_intent CROSS JOIN generate_series(1,100000) g WHERE tenant_id=$1 AND message_intent_id=$2`, tenant, seed.RecipientMessageID)
	db.Exec(t, `INSERT INTO recipient_message (tenant_id,recipient_message_id,message_intent_id,recipient_ref,endpoint_id,rendered_digest,classification,correlation_key,recipient_state,satisfaction_state,created_at,updated_at)
	 SELECT tenant_id,md5('volume-'||g)::uuid,md5('volume-'||g)::uuid,recipient_ref,endpoint_id,rendered_digest,classification,correlation_key,recipient_state,satisfaction_state,created_at+(g/5)*interval '1 second',created_at+(g/5)*interval '1 second'
	 FROM recipient_message CROSS JOIN generate_series(1,100000) g WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, seed.RecipientMessageID)
	db.Exec(t, `INSERT INTO inbox_record (tenant_id,inbox_record_id,subject_ref,recipient_message_id,read_state,archived,pinned,state_changed_at,version,created_at)
	 SELECT tenant_id,md5('volume-'||g)::uuid,subject_ref,md5('volume-'||g)::uuid,CASE WHEN g%4=0 THEN 'UNREAD' ELSE 'READ' END,g%7=0,g%10=0,created_at+(g/5)*interval '1 second',1,created_at+(g/5)*interval '1 second'
	 FROM inbox_record CROSS JOIN generate_series(1,100000) g WHERE tenant_id=$1 AND inbox_record_id=$2`, tenant, seed.InboxRecordID)
	// Reproduce the pre-migration plan in this disposable schema.
	db.Exec(t, "DROP INDEX inbox_record_feed")
	db.Exec(t, "DROP INDEX inbox_record_read_feed")
	db.Exec(t, "DROP INDEX inbox_record_pinned_feed")
	db.Exec(t, "ANALYZE inbox_record")
	db.Exec(t, "ANALYZE recipient_message")
	db.Exec(t, "ANALYZE message_intent")
	measure := func(label string, q inbox.PageQuery) {
		t.Helper()
		samples := make([]time.Duration, 0, 25)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			for n := 0; n < 26; n++ {
				start := time.Now()
				p, err := (inbox.Store{}).ListPage(ctx, tx, tenant, "volume-owner", q)
				if err != nil {
					return err
				}
				if len(p.Records) != 50 || p.Next == nil {
					t.Fatalf("volume page is incomplete: %d", len(p.Records))
				}
				if n > 0 {
					samples = append(samples, time.Since(start))
				}
			}
			return nil
		})
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("%s rows=100001 page=50 n=25 p50=%s p95=%s", label, samples[12], samples[23])
	}
	measure("baseline-no-feed-index", inbox.PageQuery{Limit: 50})
	db.Exec(t, "CREATE INDEX inbox_record_feed ON inbox_record (tenant_id,subject_ref,archived,created_at DESC,inbox_record_id DESC)")
	db.Exec(t, "CREATE INDEX inbox_record_read_feed ON inbox_record (tenant_id,subject_ref,archived,read_state,created_at DESC,inbox_record_id DESC)")
	db.Exec(t, "CREATE INDEX inbox_record_pinned_feed ON inbox_record (tenant_id,subject_ref,archived,created_at DESC,inbox_record_id DESC) WHERE pinned")
	db.Exec(t, "ANALYZE inbox_record")
	measure("indexed-first-page", inbox.PageQuery{Limit: 50})
	measure("indexed-deep-page", inbox.PageQuery{Limit: 50, After: &inbox.Position{CreatedAt: fixedInstant.Add(2000 * time.Second), RecordID: uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")}})
	measure("indexed-unread", inbox.PageQuery{Limit: 50, ReadState: inbox.Unread})
	pinned := true
	measure("indexed-pinned", inbox.PageQuery{Limit: 50, Pinned: &pinned})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var plan string
		if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT inbox_record_id FROM inbox_record
		 WHERE tenant_id=$1 AND subject_ref=$2 AND NOT archived AND read_state='UNREAD'
		 AND (created_at,inbox_record_id)<($3,$4) ORDER BY created_at DESC,inbox_record_id DESC LIMIT 51`, tenant, "volume-owner", fixedInstant.Add(2000*time.Second), uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")).Scan(&plan); err != nil {
			return err
		}
		if !strings.Contains(plan, "inbox_record_read_feed") || strings.Contains(plan, `"Node Type": "Sort"`) || strings.Contains(plan, `"Node Type": "Seq Scan"`) {
			t.Fatalf("non-seek plan: %s", plan)
		}
		var parsed []struct {
			Plan struct {
				Rows   int `json:"Actual Rows"`
				Blocks int `json:"Shared Hit Blocks"`
			} `json:"Plan"`
		}
		if err := json.Unmarshal([]byte(plan), &parsed); err != nil {
			return err
		}
		if len(parsed) != 1 || parsed[0].Plan.Rows != 51 || parsed[0].Plan.Blocks > 1000 {
			t.Fatalf("unbounded plan: %s", plan)
		}
		t.Logf("deep-unread-plan rows=%d shared_hit_blocks=%d", parsed[0].Plan.Rows, parsed[0].Plan.Blocks)
		start := time.Now()
		p, err := (inbox.Store{}).WorkflowNoticesPage(ctx, tx, tenant, "volume-owner", inbox.WorkflowPageQuery{PageQuery: inbox.PageQuery{Limit: 50}, Purpose: "APPROVAL"})
		if err != nil {
			return err
		}
		if len(p.Records) != 50 || p.Next == nil {
			t.Fatal("workflow volume page incomplete")
		}
		t.Logf("joined-workflow-page=%s rows=%d", time.Since(start), len(p.Records))
		return nil
	})
}
