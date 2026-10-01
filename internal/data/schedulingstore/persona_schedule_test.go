package schedulingstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/schedulingstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func personaWorkerSnapshot(t *testing.T, revision string, worker values.EntityRef, publicationDigest string) schedulingstore.PublishedScheduleSnapshot {
	t.Helper()
	data, err := json.Marshal(struct {
		Schema  string `json:"schema"`
		Workers []struct {
			Worker values.EntityRef `json:"worker"`
		} `json:"workers"`
	}{Schema: "persona-work-schedule/v1", Workers: []struct {
		Worker values.EntityRef `json:"worker"`
	}{{Worker: worker}}})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot(revision, string(data), publicationDigest)
}

func TestTodo_AGENTP_023_NativeSchedule_StoreIntegration(t *testing.T) {
	db := pgtest.New(t)
	tenant := values.TenantId("persona-schedule-" + uuid.NewString())
	insertTenant(t, db, tenant.String())
	foreignTenant := values.TenantId("persona-schedule-foreign-" + uuid.NewString())
	insertTenant(t, db, foreignTenant.String())
	worker := ref(tenant, "candidate")
	otherWorker := ref(tenant, "candidate")
	first := schedulingstore.New(db.Conn)
	second := schedulingstore.New(db.NewConn(t))
	initial := personaWorkerSnapshot(t, "revision-1", worker, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := first.PublishScheduleSnapshot(context.Background(), tenant, "schedule-current", "", 0, initial); err != nil {
		t.Fatal(err)
	}
	id, loaded, fence, err := second.LoadPersonaPublishedScheduleForWorker(context.Background(), tenant, worker)
	if err != nil || id != "schedule-current" || fence != 1 || loaded.PayloadDigest != initial.PayloadDigest {
		t.Fatalf("current persisted worker schedule id=%s fence=%d snapshot=%+v err=%v", id, fence, loaded, err)
	}
	if _, _, _, err := first.LoadPersonaPublishedScheduleForWorker(context.Background(), tenant, otherWorker); !errors.Is(err, schedulingstore.ErrNotFound) {
		t.Fatalf("unassigned worker lookup=%v; want absent", err)
	}
	if _, _, _, err := first.LoadPersonaPublishedScheduleForWorker(context.Background(), foreignTenant, worker); !errors.Is(err, schedulingstore.ErrInvalid) {
		t.Fatalf("cross tenant worker lookup=%v; want invalid", err)
	}
	foreignWorker := worker
	foreignWorker.Tenant = foreignTenant
	if _, _, _, err := first.LoadPersonaPublishedScheduleForWorker(context.Background(), foreignTenant, foreignWorker); !errors.Is(err, schedulingstore.ErrNotFound) {
		t.Fatalf("foreign tenant lookup leaked schedule: %v", err)
	}
	next := personaWorkerSnapshot(t, "revision-2", otherWorker, "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if _, err := first.PublishScheduleSnapshot(context.Background(), tenant, "schedule-current", initial.PublicationDigest, 1, next); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := second.LoadPersonaPublishedScheduleForWorker(context.Background(), tenant, worker); !errors.Is(err, schedulingstore.ErrNotFound) {
		t.Fatalf("superseded worker membership remained visible: %v", err)
	}
	id, loaded, fence, err = second.LoadPersonaPublishedScheduleForWorker(context.Background(), tenant, otherWorker)
	if err != nil || id != "schedule-current" || fence != 2 || loaded.Revision != "revision-2" {
		t.Fatalf("advanced current worker membership id=%s fence=%d revision=%s err=%v", id, fence, loaded.Revision, err)
	}
	duplicate := personaWorkerSnapshot(t, "duplicate-revision", otherWorker, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if _, err := first.PublishScheduleSnapshot(context.Background(), tenant, "schedule-ambiguous", "", 0, duplicate); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := second.LoadPersonaPublishedScheduleForWorker(context.Background(), tenant, otherWorker); !errors.Is(err, schedulingstore.ErrInvalid) {
		t.Fatalf("ambiguous current worker schedules arbitrarily selected: %v", err)
	}
}
