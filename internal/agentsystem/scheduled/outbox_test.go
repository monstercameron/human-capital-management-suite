package scheduled

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type memoryOutbox struct {
	mu         sync.Mutex
	state      *ownerMemory
	deliveries map[string]Delivery
	receipts   map[string]Receipt
	failAck    bool
}

func (m *memoryOutbox) AppendWindow(_ context.Context, s Schedule, end values.Instant, deliveries []Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.Revision != m.state.state.Revision || s.Cursor.Compare(m.state.state.Cursor) != 0 {
		return ErrRevision
	}
	for _, delivery := range deliveries {
		bytes, err := json.Marshal(delivery)
		if err != nil {
			return err
		}
		var restored Delivery
		if err := json.Unmarshal(bytes, &restored); err != nil {
			return err
		}
		m.deliveries[delivery.Key] = restored
	}
	m.state.state.Cursor = end
	return nil
}
func (m *memoryOutbox) Pending(_ context.Context, tenant string, _ int) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Delivery
	for key, d := range m.deliveries {
		if _, ok := m.receipts[key]; !ok && d.TenantID == tenant {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
func (m *memoryOutbox) GetDelivery(_ context.Context, tenant, key string) (Delivery, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[key]
	return d, ok && d.TenantID == tenant, nil
}
func (m *memoryOutbox) Acknowledge(_ context.Context, tenant, key string, r Receipt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAck {
		m.failAck = false
		return errors.New("ack unavailable")
	}
	if prior, ok := m.receipts[key]; ok && prior != r {
		return ErrReceiptConflict
	}
	if m.deliveries[key].TenantID != tenant {
		return ErrReceiptConflict
	}
	m.receipts[key] = r
	return nil
}
func (m *memoryOutbox) LoadReceipt(_ context.Context, _ string, key string) (Receipt, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.receipts[key]
	return r, ok, nil
}

type frozenContext struct{}

func (frozenContext) BuildScheduleContext(_ context.Context, _ Schedule, _ schedule.Occurrence) (agentrun.ContextScope, error) {
	return agentrun.ContextScope{ID: "context", SnapshotID: "snapshot", Digest: testContextDigest}, nil
}
func TestTodo_AGENT_031_OutboxRecovery(t *testing.T) {
	ctx := context.Background()
	owner, state, authority, s := ownerFixture(t)
	s.State = StateActive
	s.Revision = 1
	s.Cursor = values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	s.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUpAll, MaxCatchUp: 10}
	state.state = s
	outbox := &memoryOutbox{state: state, deliveries: map[string]Delivery{}, receipts: map[string]Receipt{}, failAck: true}
	inbox, _ := testInbox()
	worker, err := NewOutboxWorker(owner, outbox, frozenContext{}, inbox)
	if err != nil {
		t.Fatal(err)
	}
	at := s.Cursor.Time().Add(24 * time.Hour)
	if err := worker.Plan(ctx, "tenant-7", "schedule-3", at); err != nil {
		t.Fatal(err)
	}
	if len(outbox.deliveries) != 1 || state.state.Cursor.Time() != at {
		t.Fatalf("window not committed: deliveries=%d cursor=%s", len(outbox.deliveries), state.state.Cursor)
	}
	pending, err := outbox.Pending(ctx, "tenant-7", 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	delivery := pending[0]
	if _, err := worker.Replay(ctx, "tenant-7", 10); err == nil {
		t.Fatal("ack failure disappeared")
	}
	if len(outbox.receipts) != 0 {
		t.Fatal("ack persisted despite failure")
	}
	// Restart preserves the exact request despite a changed process clock.
	restarted, err := NewOutboxWorker(owner, outbox, frozenContext{}, inbox)
	if err != nil {
		t.Fatal(err)
	}
	if done, err := restarted.Replay(ctx, "tenant-7", 10); err != nil || done != 1 {
		t.Fatalf("restart replay=%d err=%v", done, err)
	}
	receipt := outbox.receipts[delivery.Key]
	if receipt.Decision != agentrun.DecisionAccepted || receipt.RunRequestID == "" {
		t.Fatalf("receipt=%+v", receipt)
	}
	if done, err := worker.Replay(ctx, "tenant-7", 10); err != nil || done != 0 {
		t.Fatalf("duplicate replay=%d err=%v", done, err)
	}
	if key, err := worker.ResolveSourceKey(ctx, delivery.Request); err != nil || key != delivery.Key {
		t.Fatalf("source key=%q err=%v", key, err)
	}
	forged := delivery.Request
	forged.Purpose = "different"
	if err := worker.CheckRequest(ctx, forged); !errors.Is(err, ErrFiringRefused) {
		t.Fatalf("forged context=%v", err)
	}
	authority.revoked = true
	if err := worker.CheckRequest(ctx, delivery.Request); !errors.Is(err, ErrFiringRefused) {
		t.Fatalf("revocation after enqueue=%v", err)
	}
	authority.revoked = false
	state.state.State = StatePaused
	if err := worker.CheckRequest(ctx, delivery.Request); !errors.Is(err, ErrInactive) {
		t.Fatalf("pause after enqueue=%v", err)
	}
}
func TestTodo_AGENT_030_CodecAndDST(t *testing.T) {
	owner, _, _, s := ownerFixture(t)
	bytes, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored Schedule
	if err := json.Unmarshal(bytes, &restored); err != nil || ValidateSchedule(restored) != nil || restored.Trigger.Digest != s.Trigger.Digest {
		t.Fatalf("schedule round trip=%+v err=%v", restored, err)
	}
	s.Trigger.Definition.Source.Cron.Expression = "30 1 * * *"
	pub, err := schedule.Publish(schedule.NewRegistry(), s.Trigger.Definition, []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}})
	if err != nil {
		t.Fatal(err)
	}
	s.Trigger = pub
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	window := schedule.OccurrenceWindow{Start: values.NewInstant(at), End: values.NewInstant(at.Add(24 * time.Hour))}
	s.DST = "REJECT"
	if _, err := owner.Preview(context.Background(), Actor{"tenant-7", "owner"}, s, window); !errors.Is(err, schedule.ErrOccurrenceDST) {
		t.Fatalf("ambiguous rejected=%v", err)
	}
	s.DST = "EARLIER"
	earlier, err := owner.Preview(context.Background(), Actor{"tenant-7", "owner"}, s, window)
	if err != nil || len(earlier.Occurrences) != 1 {
		t.Fatalf("earlier=%+v err=%v", earlier, err)
	}
	s.DST = "LATER"
	later, err := owner.Preview(context.Background(), Actor{"tenant-7", "owner"}, s, window)
	if err != nil || len(later.Occurrences) != 1 || earlier.Digest == later.Digest || !earlier.Occurrences[0].At.Before(later.Occurrences[0].At) {
		t.Fatalf("later=%+v err=%v", later, err)
	}
	for _, result := range []schedule.OccurrenceResult{earlier, later} {
		occ := result.Occurrences[0]
		firing := Firing{Occurrence: occ, Target: *s.Trigger.Definition.AgentRun}
		source, _ := firing.SourceIdentity()
		delivery := Delivery{TenantID: "tenant-7", Key: source.Key, ScheduleID: "schedule-3", ControlRevision: 1, Firing: firing, Request: testRequest(firing), EnqueuedAt: at}
		bytes, err := json.Marshal(delivery)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Delivery
		if err := json.Unmarshal(bytes, &decoded); err != nil || decoded.Firing.Occurrence.ScheduledAt.Disambiguation() != occ.ScheduledAt.Disambiguation() || decoded.Firing.Occurrence.At.Compare(occ.At) != 0 {
			t.Fatalf("lost DST fold evidence=%+v err=%v", decoded, err)
		}
	}
}

func TestTodo_AGENT_031_ReplayContinuesAfterStaleOccurrence(t *testing.T) {
	ctx := context.Background()
	owner, state, _, s := ownerFixture(t)
	s.State, s.Revision = StateActive, 1
	s.Cursor = values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	s.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUpAll, MaxCatchUp: 10}
	state.state = s
	outbox := &memoryOutbox{state: state, deliveries: map[string]Delivery{}, receipts: map[string]Receipt{}}
	inbox, _ := testInbox()
	worker, _ := NewOutboxWorker(owner, outbox, frozenContext{}, inbox)
	if err := worker.Plan(ctx, "tenant-7", "schedule-3", s.Cursor.Time().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	pending, _ := outbox.Pending(ctx, "tenant-7", 10)
	if len(pending) != 2 {
		t.Fatalf("pending=%d", len(pending))
	}
	stale := pending[0]
	stale.Firing.Occurrence.Trigger.Version = "obsolete"
	outbox.deliveries[stale.Key] = stale
	if done, err := worker.Replay(ctx, "tenant-7", 10); done != 1 || !errors.Is(err, ErrInactive) {
		t.Fatalf("done=%d err=%v", done, err)
	}
	remaining, _ := outbox.Pending(ctx, "tenant-7", 10)
	if len(remaining) != 1 || remaining[0].Key != stale.Key || remaining[0].Request.Deadline != stale.Request.Deadline {
		t.Fatalf("unresolved evidence=%+v", remaining)
	}
}

func TestTodo_AGENT_030_CalendarPins(t *testing.T) {
	owner, _, _, s := ownerFixture(t)
	date, _ := values.NewLocalDate(2026, time.November, 1)
	clock, _ := values.NewLocalTime(9, 0, 0, 0)
	ref := values.CalendarRef{Ref: "calendar", Version: "4"}
	s.Trigger.Definition.Source = schedule.TriggerSource{Kind: schedule.SourceCalendar, Calendar: schedule.CalendarSource{CalendarRef: ref, Cutoff: cycle.CutoffRule{PhaseID: "digest", NominalDate: date, NominalTime: clock, JurisdictionRef: "US-NY", Adjustment: cycle.AdjustmentNextBusinessDay}}}
	pub, err := schedule.Publish(schedule.NewRegistry(), s.Trigger.Definition, []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}})
	if err != nil {
		t.Fatal(err)
	}
	s.Trigger = pub
	s.Calendar = cycle.TenantCalendar{Calendar: ref, Zone: s.Zone, WorkingWeekdays: map[time.Weekday]bool{time.Monday: true, time.Tuesday: true, time.Wednesday: true, time.Thursday: true, time.Friday: true}}
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var restored Schedule
	if err := json.Unmarshal(encoded, &restored); err != nil || ValidateSchedule(restored) != nil {
		t.Fatalf("calendar decode=%+v err=%v", restored, err)
	}
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	preview, err := owner.Preview(context.Background(), Actor{"tenant-7", "owner"}, restored, schedule.OccurrenceWindow{Start: values.NewInstant(at), End: values.NewInstant(at.Add(72 * time.Hour))})
	if err != nil || len(preview.Occurrences) != 4 || preview.Occurrences[0].At.Time() != time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC) {
		t.Fatalf("calendar preview=%+v err=%v", preview, err)
	}
	restored.Calendar.Calendar.Version = "5"
	if !errors.Is(ValidateSchedule(restored), ErrInvalidSchedule) {
		t.Fatal("changed calendar revision accepted")
	}
}
