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

func TestTodo_NAAS_001_StatusIntegration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "status-contract")
	otherTenant := insertTenant(t, db, "status-other")
	conn := appConn(t, db)
	ctx := context.Background()
	store := inbox.Store{}
	notice := inbox.WorkflowStatusNotice{TenantID: tenant, InstanceID: uuid.New(), SubjectRef: "requester",
		Event: inbox.StatusInReview, EventRef: "approval-1", CorrelationID: "status-correlation", AudienceDigest: "resolved", CreatedAt: fixedInstant}
	var first inbox.Record
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		first, err = store.PublishWorkflowStatus(ctx, tx, notice)
		if err != nil {
			return err
		}
		return store.MarkRead(ctx, tx, tenant, notice.SubjectRef, first.InboxRecordID, first.Version, fixedInstant)
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		again, err := store.PublishWorkflowStatus(ctx, tx, notice)
		if err != nil {
			return err
		}
		if again.InboxRecordID != first.InboxRecordID || again.ReadState != inbox.Read || again.Version != 2 {
			t.Fatalf("status replay reset recipient state: %+v", again)
		}
		finished := notice
		finished.Event, finished.EventRef, finished.CreatedAt = inbox.StatusFinished, "end-complete", fixedInstant.Add(time.Second)
		if _, err := store.PublishWorkflowStatus(ctx, tx, finished); err != nil {
			return err
		}
		rows, err := store.WorkflowStatusNotices(ctx, tx, tenant, notice.SubjectRef, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Event != inbox.StatusFinished || rows[0].EventRef != "end-complete" || rows[0].CorrelationID != notice.CorrelationID || rows[0].InstanceID != notice.InstanceID {
			t.Fatalf("bounded ordered status feed lost linkage: %+v", rows)
		}
		if err := store.Archive(ctx, tx, tenant, notice.SubjectRef, rows[0].InboxRecordID, true, rows[0].Version, fixedInstant.Add(2*time.Second)); err != nil {
			return err
		}
		rows, err = store.WorkflowStatusNotices(ctx, tx, tenant, notice.SubjectRef, 100)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].InboxRecordID != first.InboxRecordID || rows[0].ReadState != inbox.Read {
			t.Fatalf("archive membership or read state incorrect: %+v", rows)
		}
		wrong, err := store.WorkflowStatusNotices(ctx, tx, tenant, "other", 100)
		if len(wrong) != 0 {
			t.Fatalf("recipient leak: %+v", wrong)
		}
		return err
	})
	inTenantTx(t, conn, otherTenant, func(tx dbport.Tx) error {
		rows, err := store.WorkflowStatusNotices(ctx, tx, tenant, notice.SubjectRef, 100)
		if len(rows) != 0 {
			t.Fatalf("tenant RLS leak: %+v", rows)
		}
		return err
	})
	rollback := errors.New("rollback publication")
	notice.EventRef = "approval-2"
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.PublishWorkflowStatus(ctx, tx, notice); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("rollback: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		rows, err := store.WorkflowStatusNotices(ctx, tx, tenant, notice.SubjectRef, 100)
		if len(rows) != 1 || rows[0].EventRef != "approval-1" {
			t.Fatalf("rolled-back status became visible: %+v", rows)
		}
		return err
	})
}

func TestTodo_NAAS_001_StatusInvalid(t *testing.T) {
	ctx := context.Background()
	store := inbox.Store{}
	valid := inbox.WorkflowStatusNotice{TenantID: uuid.New(), InstanceID: uuid.New(), SubjectRef: "requester",
		Event: inbox.StatusInReview, EventRef: "item", CorrelationID: "correlation", AudienceDigest: "resolved", CreatedAt: fixedInstant}
	for _, breakNotice := range []func(*inbox.WorkflowStatusNotice){
		func(n *inbox.WorkflowStatusNotice) { n.TenantID = uuid.Nil },
		func(n *inbox.WorkflowStatusNotice) { n.InstanceID = uuid.Nil },
		func(n *inbox.WorkflowStatusNotice) { n.SubjectRef = "" },
		func(n *inbox.WorkflowStatusNotice) { n.Event = "APPROVED" },
		func(n *inbox.WorkflowStatusNotice) { n.EventRef = "" },
		func(n *inbox.WorkflowStatusNotice) { n.CorrelationID = "" },
		func(n *inbox.WorkflowStatusNotice) { n.AudienceDigest = "" },
		func(n *inbox.WorkflowStatusNotice) { n.CreatedAt = time.Time{} },
	} {
		n := valid
		breakNotice(&n)
		if _, err := store.PublishWorkflowStatus(ctx, nil, n); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("invalid status reached storage: %v", err)
		}
	}
	for _, limit := range []int{-1, 0, 101} {
		if _, err := store.WorkflowStatusNotices(ctx, nil, valid.TenantID, valid.SubjectRef, limit); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("invalid limit %d reached storage: %v", limit, err)
		}
	}
	if _, err := store.WorkflowStatusNotices(ctx, nil, uuid.Nil, valid.SubjectRef, 1); !errors.Is(err, inbox.ErrInvalid) {
		t.Fatalf("missing tenant reached storage: %v", err)
	}
}
