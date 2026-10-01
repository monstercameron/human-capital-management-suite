package agenttriggerstore

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type nativeKeys struct{ store *Store }

func (n nativeKeys) ResolveSourceKey(ctx context.Context, r agentrun.Request) (string, error) {
	d, found, err := n.store.GetDelivery(ctx, r.Source.TenantID, r.Source.Ref)
	if err != nil || !found {
		return "", scheduled.ErrInvalidFiring
	}
	return d.Key, nil
}

type executionAuthority struct{}

func (executionAuthority) Recheck(context.Context, string, string) error { return nil }

// The owner lock encloses real execution metadata transitions on the ordinary
// pool. Concurrent source controls use a separate bounded fence pool.
func testScheduleExecutionFence(t *testing.T, database *agentstore.Store, store *Store, tenant uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, policy := range []schedule.OverlapPolicy{schedule.OverlapSkip, schedule.OverlapQueue, schedule.OverlapRefuse} {
		t.Run("fence-"+string(policy), func(t *testing.T) {
			s := scheduleFixture(t)
			s.Trigger.Definition.ID = "fence-" + string(policy)
			s.Trigger.Definition.Source.Cron.Expression = "*/5 * * * *"
			s.Trigger.Definition.Overlap = policy
			pub, err := schedule.Publish(schedule.NewRegistry(), s.Trigger.Definition, []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}})
			if err != nil {
				t.Fatal(err)
			}
			s.Trigger = pub
			if err := store.Save(ctx, s, 0, scheduled.Audit{TenantID: "tenant-a", ScheduleID: pub.Definition.ID, ActorID: "reviewer", Revision: 1, Action: scheduled.ActionPublish, At: s.Cursor.Time()}); err != nil {
				t.Fatal(err)
			}
			repository, err := agentrunstore.NewAdmissionRepositoryWithSourceResolver(database, tenant, values.TenantId("tenant-a"), nativeKeys{store})
			if err != nil {
				t.Fatal(err)
			}
			at := s.Cursor.Time().Add(10 * time.Minute)
			admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Store: repository, Authority: currentAuthority{}, Now: func() time.Time { return at }})
			if err != nil {
				t.Fatal(err)
			}
			owner, _ := scheduled.NewOwner(store, sourceAuthority{})
			worker, _ := scheduled.NewOutboxWorker(owner, store, contextBuilder{}, admission)
			if err := worker.Plan(ctx, "tenant-a", pub.Definition.ID, at); err != nil {
				t.Fatal(err)
			}
			pending, err := store.Pending(ctx, "tenant-a", 100)
			if err != nil {
				t.Fatal(err)
			}
			var deliveries []scheduled.Delivery
			for _, d := range pending {
				if d.ScheduleID == pub.Definition.ID {
					deliveries = append(deliveries, d)
				}
			}
			if len(deliveries) != 2 {
				t.Fatalf("deliveries=%d", len(deliveries))
			}
			// The same enqueue time has a stable source-key tie break.
			sort.Slice(deliveries, func(i, j int) bool { return deliveries[i].Key < deliveries[j].Key })
			runStore, _ := agentrunstate.New(database, func(string) uuid.UUID { return tenant })
			tenantStore, _ := runStore.ForTenant("tenant-a")
			executions, _ := runstate.New(tenantStore, executionAuthority{})
			runs := make([]runstate.Run, 2)
			for i, d := range deliveries {
				record, _, err := admission.Admit(ctx, d.Request)
				if err != nil {
					t.Fatal(err)
				}
				runs[i], err = executions.Start(ctx, record)
				if err != nil {
					t.Fatal(err)
				}
			}
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i := range deliveries {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, errs[i] = store.WithExecutionFence(ctx, "tenant-a", deliveries[i].Key, func(policyErr error) (runstate.Run, error) {
						if policyErr != nil {
							return runstate.Run{}, policyErr
						}
						return executions.Claim(ctx, runs[i].ID, "worker", at, time.Minute)
					})
				}(i)
			}
			wg.Wait()
			want := scheduled.ErrOverlapRefused
			if policy == schedule.OverlapSkip {
				want = scheduled.ErrOverlapSkipped
			}
			if policy == schedule.OverlapQueue {
				want = scheduled.ErrOverlapQueued
			}
			if errs[0] != nil || !errors.Is(errs[1], want) {
				t.Fatalf("claim errors=%v want=%v", errs, want)
			}
			first, err := tenantStore.Get(ctx, runs[0].ID)
			if err != nil || first.State != runstate.StateRunning {
				t.Fatalf("first=%+v err=%v", first, err)
			}
			second, err := tenantStore.Get(ctx, runs[1].ID)
			if err != nil || second.State != runstate.StateReady || second.Lease != nil {
				t.Fatalf("overlap acquired lease=%+v err=%v", second, err)
			}
			// Bounded source queues reject a batch before cursor advancement.
			current, _, _ := store.Load(ctx, "tenant-a", pub.Definition.ID)
			tooMany := make([]scheduled.Delivery, 11)
			if err := store.AppendWindow(ctx, current, values.NewInstant(at.Add(time.Minute)), tooMany); !errors.Is(err, schedule.ErrStorm) {
				t.Fatalf("unbounded batch=%v", err)
			}
			current.State, current.Revision = scheduled.StatePaused, 2
			if err := store.Save(ctx, current, 1, scheduled.Audit{TenantID: "tenant-a", ScheduleID: pub.Definition.ID, ActorID: "owner", Revision: 2, Action: scheduled.ActionPause, At: at}); err != nil {
				t.Fatal(err)
			}
			_, err = store.WithExecutionFence(ctx, "tenant-a", deliveries[1].Key, func(e error) (runstate.Run, error) { return runstate.Run{}, e })
			if !errors.Is(err, scheduled.ErrInactive) {
				t.Fatalf("paused fence=%v", err)
			}
		})
	}
	if _, err := store.WithExecutionFence(ctx, "tenant-b", "missing", nil); !errors.Is(err, scheduled.ErrAuthority) {
		t.Fatalf("tenant fence=%v", err)
	}
	if _, err := store.WithExecutionFence(ctx, "tenant-a", "missing", nil); !errors.Is(err, scheduled.ErrInvalidFiring) {
		t.Fatalf("nil claim=%v", err)
	}
	if _, err := store.WithExecutionFence(ctx, "tenant-a", "missing", func(error) (runstate.Run, error) { return runstate.Run{}, nil }); !errors.Is(err, scheduled.ErrInvalidFiring) {
		t.Fatalf("missing firing=%v", err)
	}
}
