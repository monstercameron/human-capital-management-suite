package timeclockstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

func TestTodo_WTIME004_PunchObservationLookupFailsClosedWithoutStore(t *testing.T) {
	_, found, err := (ObservationAdapter{}).LookupPunchObservation(context.Background(), "tenant", "WORKER_SELF", "key")
	if found || !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestTodo_WTIME004_PunchObservationLookupUsesDurableIdentity(t *testing.T) {
	store, tenant := adapterFixture(t)
	a := ObservationAdapter{Store: store}
	at := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	_, _, err := store.AppendObservation(context.Background(), tenant, timestore.ObservationRow{ID: "old-observation", TenantID: tenant, WorkerRef: "worker", AssignmentRef: "assignment", Source: "WORKER_SELF", EventType: "OUT", IdempotencyKey: "old-key", Digest: "old-digest", OccurredAt: at, ReceivedAt: at.Add(time.Second), Payload: []byte(`{"session_id":"old-session","state":"CLOSED"}`)})
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := a.LookupPunchObservation(context.Background(), tenant, "WORKER_SELF", "old-key")
	if err != nil || !found || got.ID != "old-observation" || got.WorkerRef != "worker" || !jsonEqual(got.Payload, `{"session_id":"old-session","state":"CLOSED"}`) {
		t.Fatalf("got=%+v found=%v err=%v", got, found, err)
	}
	if _, found, err := a.LookupPunchObservation(context.Background(), tenant, "WORKER_SELF", "missing"); err != nil || found {
		t.Fatalf("missing found=%v err=%v", found, err)
	}
	if _, found, err := a.LookupPunchObservation(context.Background(), tenant, "OTHER_SOURCE", "old-key"); err != nil || found {
		t.Fatalf("wrong source found=%v err=%v", found, err)
	}
	if _, found, err := a.LookupPunchObservation(context.Background(), "other-tenant", "WORKER_SELF", "old-key"); err != nil || found {
		t.Fatalf("foreign tenant found=%v err=%v", found, err)
	}
}

// jsonEqual compares JSON semantically: jsonb storage normalises key order and spacing.
func jsonEqual(got []byte, want string) bool {
	var a, b any
	if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
		return false
	}
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	return string(ga) == string(gb)
}
