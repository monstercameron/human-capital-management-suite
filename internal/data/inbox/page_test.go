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

func TestTodo_NAAS_003(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "naas-pages")
	conn := appConn(t, db)
	ctx := context.Background()
	instance := uuid.New()
	store := inbox.Store{}
	var ids []uuid.UUID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for n := 0; n < 11; n++ {
			purpose := "APPROVAL"
			if n%2 == 0 {
				purpose = "TASK"
			}
			r, err := store.PublishWorkflow(ctx, tx, inbox.WorkflowNotice{TenantID: tenant, WorkItemID: uuid.New(), InstanceID: instance, SubjectRef: "owner", Purpose: purpose, CorrelationID: "corr", AudienceDigest: "resolution", CreatedAt: fixedInstant})
			if err != nil {
				return err
			}
			ids = append(ids, r.InboxRecordID)
		}
		return nil
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		seen := map[uuid.UUID]bool{}
		q := inbox.PageQuery{Limit: 3}
		var prior uuid.UUID
		for {
			p, err := store.ListPage(ctx, tx, tenant, "owner", q)
			if err != nil {
				return err
			}
			if len(p.Records) > 3 {
				t.Fatal("unbounded page")
			}
			for _, r := range p.Records {
				if seen[r.InboxRecordID] {
					t.Fatal("duplicate page row")
				}
				if prior != uuid.Nil && prior.String() <= r.InboxRecordID.String() {
					t.Fatal("unstable equal-timestamp ordering")
				}
				seen[r.InboxRecordID] = true
				prior = r.InboxRecordID
			}
			if p.Next == nil {
				break
			}
			q.After = p.Next
		}
		if len(seen) != 11 {
			t.Fatalf("got %d rows", len(seen))
		}
		if err := store.MarkRead(ctx, tx, tenant, "owner", ids[0], 1, fixedInstant); err != nil {
			return err
		}
		if err := store.Pin(ctx, tx, tenant, "owner", ids[0], true, 2, fixedInstant); err != nil {
			return err
		}
		pinned := true
		p, err := store.ListPage(ctx, tx, tenant, "owner", inbox.PageQuery{ReadState: inbox.Read, Pinned: &pinned, CreatedFrom: fixedInstant, CreatedBefore: fixedInstant.Add(time.Second)})
		if err != nil {
			return err
		}
		if len(p.Records) != 1 || p.Records[0].InboxRecordID != ids[0] || p.Records[0].Version != 3 || !p.Records[0].Pinned {
			t.Fatalf("filtered page=%+v", p)
		}
		if err := store.Archive(ctx, tx, tenant, "owner", ids[0], true, 3, fixedInstant); err != nil {
			return err
		}
		archived, err := store.ListPage(ctx, tx, tenant, "owner", inbox.PageQuery{Archived: true})
		if err != nil {
			return err
		}
		if len(archived.Records) != 1 || !archived.Records[0].Archived {
			t.Fatal("archive filter failed")
		}
		unpinned := false
		active, err := store.ListPage(ctx, tx, tenant, "owner", inbox.PageQuery{Limit: 200, Pinned: &unpinned, ReadState: inbox.Unread})
		if err != nil {
			return err
		}
		if len(active.Records) != 10 || active.Next != nil {
			t.Fatalf("active unpinned/unread filter: %+v", active)
		}
		missing, err := store.WorkflowNoticesPage(ctx, tx, tenant, "owner", inbox.WorkflowPageQuery{InstanceID: uuid.New()})
		if err != nil {
			return err
		}
		if len(missing.Records) != 0 || missing.Next != nil {
			t.Fatal("unrelated workflow leaked into filtered page")
		}
		wq := inbox.WorkflowPageQuery{PageQuery: inbox.PageQuery{Limit: 2}, Purpose: "APPROVAL", InstanceID: instance}
		count := 0
		for {
			p, err := store.WorkflowNoticesPage(ctx, tx, tenant, "owner", wq)
			if err != nil {
				return err
			}
			for _, r := range p.Records {
				if r.Purpose != "APPROVAL" || r.InstanceID != instance {
					t.Fatal("workflow filter failed")
				}
			}
			count += len(p.Records)
			if p.Next == nil {
				break
			}
			wq.After = p.Next
		}
		if count != 5 {
			t.Fatalf("approval count=%d", count)
		}
		empty, err := store.ListPage(ctx, tx, tenant, "owner", inbox.PageQuery{CreatedBefore: fixedInstant})
		if err != nil {
			return err
		}
		if len(empty.Records) != 0 || empty.Next != nil {
			t.Fatal("exclusive upper bound failed")
		}
		return nil
	})
}

func TestTodo_NAAS_003_Security(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "naas-security")
	other := insertTenant(t, db, "naas-other")
	conn := appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (inbox.Store{}).PublishWorkflow(context.Background(), tx, inbox.WorkflowNotice{TenantID: tenant, WorkItemID: uuid.New(), InstanceID: uuid.New(), SubjectRef: "owner", Purpose: "TASK", CorrelationID: "corr", AudienceDigest: "resolution", CreatedAt: fixedInstant})
		return err
	})
	for _, tc := range []struct {
		scope, query uuid.UUID
		subject      string
	}{{tenant, tenant, "stranger"}, {other, tenant, "owner"}, {tenant, other, "owner"}} {
		inTenantTx(t, conn, tc.scope, func(tx dbport.Tx) error {
			p, err := (inbox.Store{}).ListPage(context.Background(), tx, tc.query, tc.subject, inbox.PageQuery{})
			if err != nil {
				return err
			}
			w, err := (inbox.Store{}).WorkflowNoticesPage(context.Background(), tx, tc.query, tc.subject, inbox.WorkflowPageQuery{})
			if err != nil {
				return err
			}
			if len(p.Records)+len(w.Records) != 0 || p.Next != nil || w.Next != nil {
				t.Fatal("cross-recipient/tenant data leaked")
			}
			return nil
		})
	}
}

func TestTodo_NAAS_003_Invalid(t *testing.T) {
	for _, q := range []inbox.PageQuery{{Limit: -1}, {Limit: 201}, {ReadState: "injected"}, {CreatedFrom: fixedInstant, CreatedBefore: fixedInstant}, {After: &inbox.Position{}}, {After: &inbox.Position{CreatedAt: fixedInstant}}} {
		if _, err := (inbox.Store{}).ListPage(context.Background(), nil, uuid.New(), "owner", q); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("invalid query accepted: %+v %v", q, err)
		}
	}
	for _, tc := range []struct {
		tenant  uuid.UUID
		subject string
	}{{uuid.Nil, "owner"}, {uuid.New(), " "}} {
		if _, err := (inbox.Store{}).ListPage(context.Background(), nil, tc.tenant, tc.subject, inbox.PageQuery{}); !errors.Is(err, inbox.ErrInvalid) {
			t.Fatalf("invalid scope: %v", err)
		}
	}
	if _, err := (inbox.Store{}).WorkflowNoticesPage(context.Background(), nil, uuid.New(), "owner", inbox.WorkflowPageQuery{Purpose: "NOTICE"}); !errors.Is(err, inbox.ErrInvalid) {
		t.Fatalf("invalid purpose: %v", err)
	}
}
