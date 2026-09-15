package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type fixedQuarantine struct {
	policy      string
	quarantined bool
	err         error
	digests     []string
}

func (q *fixedQuarantine) LiveInstancePolicy(_ context.Context, _ dbport.Conn, digest string) (string, bool, error) {
	q.digests = append(q.digests, digest)
	return q.policy, q.quarantined, q.err
}

func TestApplyQuarantineDispositionsWithoutAPause(t *testing.T) {
	f := newWfrun028Fixture(t, "quarantine-dispositions")
	run := runContext{start: f.start, selection: runtime.WorkflowSelection{Plan: f.plan}, instanceID: f.instanceID}
	ctx := context.Background()

	if v, err := (&Driver{}).applyQuarantine(ctx, nil, run, 7, f.at); err != nil || v != 7 {
		t.Fatalf("no quarantine port = %d, %v", v, err)
	}
	for _, q := range []*fixedQuarantine{{}, {policy: QuarantineContinue, quarantined: true}} {
		d := &Driver{opts: Options{Quarantine: q}}
		if v, err := d.applyQuarantine(ctx, nil, run, 7, f.at); err != nil || v != 7 {
			t.Fatalf("policy %+v = %d, %v; want the advancement to proceed", q, v, err)
		}
		if len(q.digests) != 1 || q.digests[0] != f.plan.Digest() {
			t.Fatalf("the port was asked about %v, want the pinned plan digest", q.digests)
		}
	}
	for _, policy := range []string{QuarantineBlock, "UNDECLARED"} {
		d := &Driver{opts: Options{Quarantine: &fixedQuarantine{policy: policy, quarantined: true}}}
		if _, err := d.applyQuarantine(ctx, nil, run, 7, f.at); !errors.Is(err, ErrVersionQuarantined) {
			t.Fatalf("policy %s = %v, want ErrVersionQuarantined", policy, err)
		}
	}
	boom := errors.New("registry unavailable")
	if _, err := (&Driver{opts: Options{Quarantine: &fixedQuarantine{err: boom}}}).applyQuarantine(ctx, nil, run, 7, f.at); !errors.Is(err, boom) {
		t.Fatalf("a failing port = %v, want its error", err)
	}
}

// TestTodo_WF_RUN_009_LiveInstancePause proves a live instance of a version
// quarantined with a PAUSE disposition stops at its next advancement: the
// driver requests the pause in the advance transaction, the instance is
// PAUSED at its safe point, the run reports StatusPaused and nothing advanced.
// A BLOCK disposition refuses the advancement and leaves the instance as it
// was.
func TestTodo_WF_RUN_009_LiveInstancePause(t *testing.T) {
	f := newWfrun028Fixture(t, "quarantine-pause")
	driver := func(q VersionQuarantine) *Driver {
		d, err := New(Options{
			DB: f.conn, Steps: work006EndRunner{}, Terminal: work006Terminal{},
			Items: workitem.Store{}, Guard: idempotency.PostgresStore{},
			Retention:  idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
			Clock:      func() time.Time { return f.at.Add(time.Minute) },
			Quarantine: q,
		})
		if err != nil {
			t.Fatalf("New driver: %v", err)
		}
		return d
	}
	load := func() runtime.Instance {
		var inst runtime.Instance
		work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
			var err error
			inst, err = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
			return err
		})
		return inst
	}

	if _, err := driver(&fixedQuarantine{policy: QuarantineBlock, quarantined: true}).Resume(context.Background(), f.resumeRequest()); !errors.Is(err, ErrVersionQuarantined) {
		t.Fatalf("Resume under BLOCK = %v, want ErrVersionQuarantined", err)
	}
	if inst := load(); inst.InstanceVersion != f.instanceVersion || inst.RuntimeStatus != runtime.InstanceWaiting {
		t.Fatalf("a blocked resume moved the instance to %s at %d", inst.RuntimeStatus, inst.InstanceVersion)
	}

	result, err := driver(&fixedQuarantine{policy: QuarantinePause, quarantined: true}).Resume(context.Background(), f.resumeRequest())
	if err != nil || result.Status != StatusPaused || len(result.Advances) != 0 {
		t.Fatalf("Resume under PAUSE = %+v, %v; want StatusPaused with no advancement", result, err)
	}
	if inst := load(); inst.RuntimeStatus != runtime.InstancePaused || inst.InstanceVersion != result.InstanceVersion {
		t.Fatalf("instance is %s at %d, want PAUSED at %d", inst.RuntimeStatus, inst.InstanceVersion, result.InstanceVersion)
	}
}
