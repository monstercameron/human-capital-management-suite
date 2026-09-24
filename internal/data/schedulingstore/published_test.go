package schedulingstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schedulingstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func snapshot(revision, payloadText string, publicationDigest string) schedulingstore.PublishedScheduleSnapshot {
	value := schedulingstore.PublishedScheduleSnapshot{
		Revision: revision, ApprovedDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PublicationDigest: publicationDigest, ProblemDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		RuleRevision: "rules-r1", RuleDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
	if err := schedulingstore.BindPublishedSchedulePayload(&value, json.RawMessage(payloadText)); err != nil {
		panic(err)
	}
	return value
}

func TestTodo_REV_045_03_StoreRestartCAS(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	tenantKey := tenantID.String()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenantKey, tenantKey)
	tenant := values.TenantId(tenantKey)
	first := schedulingstore.New(db.Conn)
	second := schedulingstore.New(db.NewConn(t))
	initial := snapshot("revision-1", `{"approved":{"assignments":[]},"publication":{"digest":"one"},"problem":{},"rules":[]}`, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	fence, err := first.PublishScheduleSnapshot(context.Background(), tenant, "schedule-1", "", 0, initial)
	if err != nil || fence != 1 {
		t.Fatalf("initial publish fence=%d err=%v; want 1", fence, err)
	}
	loaded, loadedFence, err := second.LoadPublishedSchedule(context.Background(), tenant, "schedule-1")
	if err != nil || loadedFence != 1 || loaded.Revision != initial.Revision || loaded.PayloadDigest != initial.PayloadDigest || string(loaded.Payload) != string(initial.Payload) {
		t.Fatalf("new store instance loaded snapshot=%+v fence=%d err=%v", loaded, loadedFence, err)
	}
	next := snapshot("revision-2", `{"approved":{"assignments":[{"id":"a1"}]},"publication":{"digest":"two"},"problem":{},"rules":[]}`, "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	nextFence, err := first.PublishScheduleSnapshot(context.Background(), tenant, "schedule-1", initial.PublicationDigest, loadedFence, next)
	if err != nil || nextFence != 2 {
		t.Fatalf("successor publish fence=%d err=%v; want 2", nextFence, err)
	}
	if _, err := second.PublishScheduleSnapshot(context.Background(), tenant, "schedule-1", initial.PublicationDigest, loadedFence, snapshot("revision-stale", `{"approved":{},"publication":{},"problem":{},"rules":[]}`, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")); !errors.Is(err, schedulingstore.ErrVersionConflict) {
		t.Fatalf("stale publisher error=%v; want CAS conflict", err)
	}
	current, currentFence, err := second.LoadPublishedSchedule(context.Background(), tenant, "schedule-1")
	if err != nil || currentFence != 2 || current.Revision != "revision-2" {
		t.Fatalf("current snapshot=%+v fence=%d err=%v; want revision-2 at fence 2", current, currentFence, err)
	}
}

func TestTodo_REV_045_03_StoreClaimPersistsSuccessorAtomically(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	tenantKey := tenantID.String()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenantKey, tenantKey)
	tenant := values.TenantId(tenantKey)
	first := schedulingstore.New(db.Conn)
	second := schedulingstore.New(db.NewConn(t))
	initial := snapshot("revision-1", `{"approved":{},"publication":{},"problem":{},"rules":[]}`, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := first.PublishScheduleSnapshot(context.Background(), tenant, "schedule-claim", "", 0, initial); err != nil {
		t.Fatal(err)
	}
	offer, err := first.CreateShiftOffer(context.Background(), tenant, "schedule-claim", 1, schedopt.ShiftOffer{
		ID: "offer-1", Kind: schedopt.ShiftOfferOpenClaim, AssignmentID: "a-1", PublicationDigest: initial.PublicationDigest,
		FencingToken: 1, State: schedopt.ShiftOfferActive, Reason: "pickup",
	})
	if err != nil {
		t.Fatal(err)
	}
	successor := snapshot("revision-2", `{"approved":{"assignments":[{"id":"a-1"}]},"publication":{},"problem":{},"rules":[]}`, "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	fence, err := second.ClaimShiftOfferWithSnapshot(context.Background(), tenant, "schedule-claim", offer.ID, offer.FencingToken, successor)
	if err != nil || fence != 3 {
		t.Fatalf("claim successor fence=%d err=%v; want 3", fence, err)
	}
	loaded, loadedFence, err := first.LoadPublishedSchedule(context.Background(), tenant, "schedule-claim")
	if err != nil || loadedFence != 3 || loaded.Revision != successor.Revision {
		t.Fatalf("successor after store restart=%+v fence=%d err=%v", loaded, loadedFence, err)
	}
	closed, err := first.LoadShiftOffer(context.Background(), tenant, "schedule-claim", offer.ID)
	if err != nil || closed.State != schedopt.ShiftOfferClaimed {
		t.Fatalf("offer after atomic claim state=%s err=%v", closed.State, err)
	}
}
