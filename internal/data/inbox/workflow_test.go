package inbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_NAAS_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workflow-notice")
	conn := appConn(t, db)
	ctx := context.Background()
	notice := inbox.WorkflowNotice{TenantID: tenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: "principal:approver", Purpose: "APPROVAL", CorrelationID: "corr-test", AudienceDigest: digestOf("authorized-resolution"), CreatedAt: fixedInstant}
	var first inbox.Record
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		first, err = (inbox.Store{}).PublishWorkflow(ctx, tx, notice)
		if err != nil {
			return err
		}
		again, err := (inbox.Store{}).PublishWorkflow(ctx, tx, notice)
		if err == nil && first.InboxRecordID != again.InboxRecordID {
			t.Fatal("retry minted another notification")
		}
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		rows, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, notice.SubjectRef, 20)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].WorkItemID != notice.WorkItemID || rows[0].ReadState != inbox.Unread || rows[0].CorrelationID != "corr-test" {
			t.Fatalf("notices = %+v", rows)
		}
		wrong, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, "principal:other", 20)
		if err != nil {
			return err
		}
		if len(wrong) != 0 {
			t.Fatal("another recipient saw notification")
		}
		return (inbox.Store{}).MarkRead(ctx, tx, tenant, notice.SubjectRef, first.InboxRecordID, first.Version, fixedInstant)
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		replayed, err := (inbox.Store{}).PublishWorkflow(ctx, tx, notice)
		if err == nil && (replayed.ReadState != inbox.Read || replayed.Version != 2) {
			t.Fatalf("retry reset read state: %+v", replayed)
		}
		return err
	})
	// A caller rollback leaves neither an inbox record nor orphan message metadata.
	conflict := notice
	conflict.InstanceID = uuid.New()
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (inbox.Store{}).PublishWorkflow(ctx, tx, conflict)
		return err
	}); !errors.Is(err, inbox.ErrInvalid) {
		t.Fatalf("conflicting replay: %v", err)
	}
	rolledBack := notice
	rolledBack.WorkItemID = uuid.New()
	want := errors.New("rollback")
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		if _, err := (inbox.Store{}).PublishWorkflow(ctx, tx, rolledBack); err != nil {
			return err
		}
		return want
	}); !errors.Is(err, want) {
		t.Fatal(err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		rows, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, notice.SubjectRef, 20)
		if len(rows) != 1 {
			t.Fatalf("rollback leaked rows: %+v", rows)
		}
		rolledBackID := uuid.NewSHA1(tenant, []byte("workflow-notice/v1\x00"+rolledBack.WorkItemID.String()+"\x00"+rolledBack.SubjectRef))
		var orphanCount int
		if scanErr := tx.QueryRow(ctx, `SELECT count(*) FROM message_intent WHERE tenant_id=$1 AND message_intent_id=$2`, tenant, rolledBackID).Scan(&orphanCount); scanErr != nil {
			return scanErr
		}
		if orphanCount != 0 {
			t.Fatal("rollback left orphan message metadata")
		}
		return err
	})
}

func TestTodo_NAAS_001_Race(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "workflow-notice-concurrency")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	notice := inbox.WorkflowNotice{TenantID: tenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: "principal:owner", Purpose: "TASK", CorrelationID: "corr-concurrent", AudienceDigest: digestOf("resolution"), CreatedAt: fixedInstant}
	start := make(chan struct{})
	errs := make(chan error, 4)
	for range 4 {
		conn := appConn(t, db)
		go func() {
			<-start
			tx, err := conn.Begin(ctx)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err = (inbox.Store{}).PublishWorkflow(ctx, tx, notice); err == nil {
				err = tx.Commit(ctx)
			}
			errs <- err
		}()
	}
	close(start)
	for range 4 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	conn := appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		rows, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, notice.SubjectRef, 100)
		if len(rows) != 1 || rows[0].Purpose != "TASK" {
			t.Fatalf("concurrent publication: %+v", rows)
		}
		return err
	})
}

func TestTodo_NAAS_001_Invalid(t *testing.T) {
	if _, err := (inbox.Store{}).PublishWorkflow(context.Background(), nil, inbox.WorkflowNotice{}); !errors.Is(err, inbox.ErrInvalid) {
		t.Fatalf("empty input: %v", err)
	}
	for _, limit := range []int{0, -1, 101} {
		if _, err := (inbox.Store{}).WorkflowNotices(context.Background(), nil, uuid.New(), "principal:test", limit); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
}
