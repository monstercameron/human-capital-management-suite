package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type remindPeople map[string][]string

func (p remindPeople) ListSavedReminderPeople(_ context.Context, tenant string) ([]string, error) {
	return p[tenant], nil
}

type remindDispatcher struct {
	mu    sync.Mutex
	calls []chat.Principal
	hosts []string
	fail  map[string]bool
}

func (d *remindDispatcher) DispatchSavedReminders(_ context.Context, p chat.Principal, tenant string, sink chat.SavedReminderSink) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, p)
	d.hosts = append(d.hosts, tenant)
	if d.fail[p.SubjectID] {
		return errors.New("boom")
	}
	if sink == nil {
		return chat.ErrUnavailable
	}
	return nil
}

type nopSavedDelivery struct{}

func (nopSavedDelivery) DeliverPrivateSavedNotice(context.Context, PrivateSavedNotice) error {
	return nil
}

// TestIssue3735_SweepRemindsEachPersonOfEachTenant: the scheduler reaches every
// person with a due reminder in every served workspace, as that person and in
// that person's workspace, and one failing person does not stop the others.
func TestIssue3735_SweepRemindsEachPersonOfEachTenant(t *testing.T) {
	people := remindPeople{"tenant-a": {"ana", "bo"}, "tenant-b": {"cy"}}
	d := &remindDispatcher{fail: map[string]bool{"ana": true}}
	log := &sweepLog{}
	got := sweepSavedReminders(context.Background(), people, d, SavedReminderNotifications{Delivery: nopSavedDelivery{}}, []string{"tenant-a", "tenant-b"}, log)
	if got != 2 || len(d.calls) != 3 || log.n != 1 {
		t.Fatalf("reminded=%d calls=%+v logged=%d", got, d.calls, log.n)
	}
	for i, p := range d.calls {
		if p.TenantID != d.hosts[i] || p.SubjectID == "" {
			t.Errorf("call %d acted outside its own workspace: %+v host %s", i, p, d.hosts[i])
		}
	}
}

type recordingNotices struct{ got []string }

func (r *recordingNotices) NotifySavedReminder(_ context.Context, n chatstoreSavedReminderNotice) error {
	r.got = append(r.got, n.Home+"/"+n.Person+"/"+n.Host+"/"+n.Key)
	return nil
}

// TestIssue3735_DeliveryWritesOnlyToTheOwner: the product's delivery hands the
// store the owner's own workspace and person; with no store it refuses instead
// of acknowledging, so the due time stays pending.
func TestIssue3735_DeliveryWritesOnlyToTheOwner(t *testing.T) {
	rec := &recordingNotices{}
	due := time.Unix(900, 0)
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "ana"}
	item := chat.SavedItem{TenantID: "tenant-a", HomeTenantID: "tenant-a", PersonID: "ana", ConversationID: "room", PostID: "post", DueAt: &due, Note: "private", Post: &chat.Post{Body: "secret words"}}
	sink := SavedReminderNotifications{Delivery: savedReminderDelivery{Notices: rec}}
	if err := sink.NotifySaved(context.Background(), p, item, "saved:k"); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0] != "tenant-a/ana/tenant-a/saved:k" {
		t.Fatalf("written=%v", rec.got)
	}
	var none *recordingNotices
	if err := (SavedReminderNotifications{Delivery: savedReminderDelivery{Notices: none}}).NotifySaved(context.Background(), p, item, "saved:k"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("no store acknowledged: %v", err)
	}
}

// TestIssue3735_WorkloadRunsAndStops: the served workload sweeps at once, and
// stops with its context.
func TestIssue3735_WorkloadRunsAndStops(t *testing.T) {
	d := &remindDispatcher{}
	w := savedReminderWorkload(remindPeople{"tenant-a": {"ana"}}, d, SavedReminderNotifications{Delivery: nopSavedDelivery{}}, []string{"tenant-a"}, nil, time.Hour)
	if w == nil || w.Name != "chat-saved-reminders" {
		t.Fatalf("workload=%+v", w)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.After(5 * time.Second)
	for {
		d.mu.Lock()
		n := len(d.calls)
		d.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no reminder swept")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workload did not stop")
	}
	if savedReminderWorkload(nil, d, SavedReminderNotifications{}, []string{"tenant-a"}, nil, time.Hour) != nil || savedReminderWorkload(remindPeople{}, d, SavedReminderNotifications{Delivery: nopSavedDelivery{}}, []string{" bad tenant "}, nil, time.Hour) != nil {
		t.Fatal("workload built without its ports or with a bad tenant")
	}
}
