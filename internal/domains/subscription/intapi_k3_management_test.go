package subscription

import (
	"errors"
	"testing"
)

func intapiK3SubscriptionRequest(id string) RevisionRequest {
	return RevisionRequest{SubscriptionID: id, Requester: "requester", Subscriber: Subscriber{PartnerRef: "partner-a"}, EventKinds: []EventKind{EventWorkerChanged}, DeclaredFields: map[EventKind][]string{EventWorkerChanged: {"worker.status"}}, DeliveryEndpointRef: "endpoint:webhook", DeliveryGuarantee: GuaranteeAtLeastOnce, TenantScope: "tenant-a"}
}

func TestTodo_INTAPI_013(t *testing.T) {
	management := NewManagement()
	draft, err := management.Create(intapiK3SubscriptionRequest("sub-1"))
	if err != nil || draft.State != StateDraft {
		t.Fatalf("create = %+v, err=%v", draft, err)
	}
	active, err := management.Resume("sub-1", "approver", "owner")
	if err != nil || active.State != StateActive || active.Revision != 2 {
		t.Fatalf("resume = %+v, err=%v", active, err)
	}
	paused, err := management.Pause("sub-1", "owner")
	if err != nil || paused.State != StatePaused {
		t.Fatalf("pause = %+v, err=%v", paused, err)
	}
	deleted, err := management.Delete("sub-1", "owner")
	if err != nil || deleted.State != StateRevoked {
		t.Fatalf("delete = %+v, err=%v", deleted, err)
	}
}

func TestTodo_INTAPI_013_Integration(t *testing.T) {
	feed := NewPullFeed()
	if _, err := feed.Append(PullEvent{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "sha256:1", Fields: map[string]string{"worker.status": "ACTIVE"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := feed.Append(PullEvent{TenantScope: "tenant-b", Kind: EventWorkerChanged, Digest: "sha256:2"}); err != nil {
		t.Fatal(err)
	}
	events, cursor, err := feed.Pull("tenant-a", 0, 10)
	if err != nil || len(events) != 1 || cursor != 1 || events[0].TenantScope != "tenant-a" {
		t.Fatalf("pull = %+v cursor=%d err=%v", events, cursor, err)
	}
}

func TestTodo_INTAPI_013_Security(t *testing.T) {
	management := NewManagement()
	if _, err := management.Create(intapiK3SubscriptionRequest("sub-security")); err != nil {
		t.Fatal(err)
	}
	if _, err := management.Resume("sub-security", "same", "same"); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("same actor approval = %v", err)
	}
	feed := NewPullFeed()
	if _, _, err := feed.Pull("tenant-a", 99, 1); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("forged cursor = %v", err)
	}
}

func TestTodo_INTAPI_013_Recovery(t *testing.T) {
	feed := NewPullFeed()
	for i := 0; i < 3; i++ {
		if _, err := feed.Append(PullEvent{TenantScope: "tenant-a", Kind: EventWorkerChanged, Digest: "event"}); err != nil {
			t.Fatal(err)
		}
	}
	first, cursor, err := feed.Pull("tenant-a", 0, 2)
	if err != nil || len(first) != 2 {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, next, err := feed.Pull("tenant-a", cursor, 2)
	if err != nil || len(second) != 1 || next <= cursor {
		t.Fatalf("recovery page = %+v cursor=%d next=%d err=%v", second, cursor, next, err)
	}
}
