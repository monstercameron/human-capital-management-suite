package tenant_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
)

var relocationKey = [32]byte{7, 7, 7, 7, 7}

// relocationSource returns a signed source placement for relocation tests.
func relocationSource(t *testing.T) tenant.Placement {
	t.Helper()
	p, err := tenant.Sign(tenant.Placement{
		Tenant: "tenant-relocate", Cell: "cell-east-1", Region: "us-east",
		ResidencyProfile: "us-only", IsolationTier: "dedicated", Epoch: 7,
	}, relocationKey)
	if err != nil {
		t.Fatalf("Sign source: %v", err)
	}
	return p
}

// relocationManifest covers every state plane the contract names:
// workflow, timer, signal, outbox, journal and key state.
func relocationManifest() []tenant.StreamRef {
	return []tenant.StreamRef{
		{Kind: "workflow", ID: "wf-001"},
		{Kind: "timer", ID: "timer-001"},
		{Kind: "signal", ID: "sig-001"},
		{Kind: "outbox", ID: "outbox-001"},
		{Kind: "journal", ID: "journal-001"},
		{Kind: "key", ID: "key-001"},
	}
}

func relocationTarget() tenant.RelocationTarget {
	return tenant.RelocationTarget{
		Cell: "cell-west-1", Region: "us-west",
		ResidencyProfile: "us-only", IsolationTier: "dedicated",
	}
}

// advanceRelocation records the four pre-cutover evidences in order.
func advanceRelocation(t *testing.T, plan *tenant.RelocationPlan) {
	t.Helper()
	for _, step := range []struct {
		phase  tenant.RelocationPhase
		digest string
		ref    string
	}{
		{tenant.RelocationFreeze, "sha256:freeze", "freeze-receipt-1"},
		{tenant.RelocationSnapshot, "sha256:snapshot", "snapshot-manifest-1"},
		{tenant.RelocationCopy, "sha256:copy", "copy-manifest-1"},
		{tenant.RelocationCatchUp, "sha256:catchup", "catchup-watermark-1"},
	} {
		if err := plan.RecordPhase(step.phase, step.digest, step.ref); err != nil {
			t.Fatalf("RecordPhase(%s): %v", step.phase, err)
		}
	}
}

func streamIDs(refs []tenant.StreamRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.Kind+":"+ref.ID)
	}
	return ids
}

// TestTodo_TENANT_005 is the PRIMARY contract: freeze, snapshot, copy,
// catch-up, cutover and reconciliation preserve tenant and stream identity
// while the signed epoch fences the old cell.
func TestTodo_TENANT_005(t *testing.T) {
	source := relocationSource(t)
	manifest := relocationManifest()
	plan, err := tenant.BeginRelocation("plan-1", source, relocationTarget(), manifest, relocationKey)
	if err != nil {
		t.Fatalf("BeginRelocation: %v", err)
	}
	if plan.TargetEpoch != source.Epoch+1 {
		t.Fatalf("TargetEpoch = %d, want %d", plan.TargetEpoch, source.Epoch+1)
	}
	advanceRelocation(t, &plan)
	moved, receipt, err := tenant.CutoverPlan(&plan, relocationKey)
	if err != nil {
		t.Fatalf("CutoverPlan: %v", err)
	}
	if err := tenant.Verify(moved, relocationKey); err != nil {
		t.Fatalf("cutover placement does not verify: %v", err)
	}
	if moved.Tenant != source.Tenant || moved.Cell != "cell-west-1" || moved.Epoch != source.Epoch+1 {
		t.Fatalf("cutover placement = %+v, want same tenant in cell-west-1 at epoch %d", moved, source.Epoch+1)
	}
	if receipt.Digest == "" {
		t.Fatal("cutover receipt has no digest")
	}
	reconciliation, err := tenant.ReconcilePlan(&plan, manifest)
	if err != nil {
		t.Fatalf("ReconcilePlan: %v", err)
	}
	if reconciliation.Digest == "" {
		t.Fatal("reconciliation receipt has no digest")
	}

	// The old cell no longer accepts writes once the epoch has moved.
	gate := tenant.RelocationGate{Current: moved, Key: relocationKey}
	if err := gate.CheckWrite(moved); err != nil {
		t.Fatalf("current placement rejected: %v", err)
	}
	if err := gate.CheckWrite(source); !errors.Is(err, tenant.ErrPlacementMismatch) {
		t.Fatalf("stale epoch write error = %v, want ErrPlacementMismatch", err)
	}
}

// cutoverFixture runs the full relocation through cutover and returns the
// plan, the moved placement and its receipt.
func cutoverFixture(t *testing.T) (tenant.RelocationPlan, tenant.Placement, tenant.CutoverReceipt) {
	t.Helper()
	source := relocationSource(t)
	plan, err := tenant.BeginRelocation("plan-golden", source, relocationTarget(), relocationManifest(), relocationKey)
	if err != nil {
		t.Fatalf("BeginRelocation: %v", err)
	}
	advanceRelocation(t, &plan)
	moved, receipt, err := tenant.CutoverPlan(&plan, relocationKey)
	if err != nil {
		t.Fatalf("CutoverPlan: %v", err)
	}
	return plan, moved, receipt
}

// TestTodo_TENANT_005_Golden pins the cutover receipt bytes. Any serializer
// or field-order change that silently alters evidence identity fails here.
func TestTodo_TENANT_005_Golden(t *testing.T) {
	_, moved, receipt := cutoverFixture(t)
	if receipt.Digest == "" || receipt.FromDigest == "" || receipt.ToDigest == "" {
		t.Fatalf("receipt lacks digests: %+v", receipt)
	}
	if len(receipt.Streams) != len(tenant.RequiredStreamKinds) {
		t.Fatalf("receipt streams = %d, want %d", len(receipt.Streams), len(tenant.RequiredStreamKinds))
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "relocation_cutover.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(want)) {
		t.Fatalf("receipt bytes differ from golden:\n got %s\nwant %s", raw, want)
	}
	movedDigest, err := moved.Digest()
	if err != nil {
		t.Fatalf("moved Digest: %v", err)
	}
	if movedDigest != receipt.ToDigest {
		t.Fatalf("receipt ToDigest = %s, moved placement digest = %s", receipt.ToDigest, movedDigest)
	}
}

// TestTodo_TENANT_005_Race proves the epoch fence is deterministic under
// concurrent writers: every stale write is rejected and every current
// write is accepted, with no partial outcome.
func TestTodo_TENANT_005_Race(t *testing.T) {
	source := relocationSource(t)
	moved, err := tenant.Sign(tenant.Placement{
		Tenant: "tenant-relocate", Cell: "cell-west-1", Region: "us-west",
		ResidencyProfile: "us-only", IsolationTier: "dedicated", Epoch: source.Epoch + 1,
	}, relocationKey)
	if err != nil {
		t.Fatalf("Sign moved: %v", err)
	}
	gate := tenant.RelocationGate{Current: moved, Key: relocationKey}
	const writers = 16
	var accepted, rejected int64
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidate := moved
			if i%2 == 0 {
				candidate = source
			}
			if err := gate.CheckWrite(candidate); err != nil {
				if !errors.Is(err, tenant.ErrPlacementMismatch) {
					t.Errorf("writer %d error = %v, want ErrPlacementMismatch", i, err)
					return
				}
				atomic.AddInt64(&rejected, 1)
				return
			}
			atomic.AddInt64(&accepted, 1)
		}(i)
	}
	wg.Wait()
	if accepted != writers/2 || rejected != writers/2 {
		t.Fatalf("accepted=%d rejected=%d, want %d/%d", accepted, rejected, writers/2, writers/2)
	}
}

// TestTodo_TENANT_005_Security covers forged authority, retargeting after
// evidence, foreign tenants and wrong signing keys. Every one fails closed.
func TestTodo_TENANT_005_Security(t *testing.T) {
	source := relocationSource(t)

	t.Run("forged source signature", func(t *testing.T) {
		forged := source
		forged.Signature = strings.Repeat("0", 64)
		if _, err := tenant.BeginRelocation("plan-x", forged, relocationTarget(), relocationManifest(), relocationKey); !errors.Is(err, tenant.ErrInvalidSignature) {
			t.Fatalf("BeginRelocation error = %v, want ErrInvalidSignature", err)
		}
	})

	t.Run("wrong authority key", func(t *testing.T) {
		var wrongKey [32]byte
		wrongKey[0] = 9
		if _, err := tenant.BeginRelocation("plan-x", source, relocationTarget(), relocationManifest(), wrongKey); !errors.Is(err, tenant.ErrInvalidSignature) {
			t.Fatalf("BeginRelocation error = %v, want ErrInvalidSignature", err)
		}
	})

	t.Run("retarget after evidence", func(t *testing.T) {
		plan, err := tenant.BeginRelocation("plan-x", source, relocationTarget(), relocationManifest(), relocationKey)
		if err != nil {
			t.Fatal(err)
		}
		advanceRelocation(t, &plan)
		plan.Target.Cell = "cell-evil-1"
		if _, _, err := tenant.CutoverPlan(&plan, relocationKey); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("CutoverPlan error = %v, want ErrRelocationPhase", err)
		}
	})

	t.Run("foreign tenant write", func(t *testing.T) {
		_, moved, _ := cutoverFixture(t)
		foreign, err := tenant.Sign(tenant.Placement{
			Tenant: "tenant-other", Cell: "cell-west-1", Region: "us-west",
			ResidencyProfile: "us-only", IsolationTier: "dedicated", Epoch: moved.Epoch,
		}, relocationKey)
		if err != nil {
			t.Fatal(err)
		}
		gate := tenant.RelocationGate{Current: moved, Key: relocationKey}
		if err := gate.CheckWrite(foreign); !errors.Is(err, tenant.ErrPlacementMismatch) {
			t.Fatalf("foreign tenant error = %v, want ErrPlacementMismatch", err)
		}
	})

	t.Run("unsigned candidate write", func(t *testing.T) {
		_, moved, _ := cutoverFixture(t)
		bare := moved
		bare.Signature = ""
		gate := tenant.RelocationGate{Current: moved, Key: relocationKey}
		if err := gate.CheckWrite(bare); !errors.Is(err, tenant.ErrInvalidSignature) {
			t.Fatalf("unsigned write error = %v, want ErrInvalidSignature", err)
		}
	})
}

// TestTodo_TENANT_005_Recovery proves rollback restores the prior placement
// safely: same tenant and cell, a strictly greater epoch, fenced target,
// preserved stream manifest, and no second rollback or late reconcile.
func TestTodo_TENANT_005_Recovery(t *testing.T) {
	source := relocationSource(t)
	plan, _, receipt := cutoverFixture(t)
	restored, rollback, err := tenant.RollbackPlan(&plan, relocationKey)
	if err != nil {
		t.Fatalf("RollbackPlan: %v", err)
	}
	if err := tenant.Verify(restored, relocationKey); err != nil {
		t.Fatalf("restored placement does not verify: %v", err)
	}
	if restored.Tenant != source.Tenant || restored.Cell != source.Cell {
		t.Fatalf("restored = %+v, want tenant %s back in %s", restored, source.Tenant, source.Cell)
	}
	if restored.Epoch != receipt.ToEpoch+1 {
		t.Fatalf("restored epoch = %d, want %d (past the fenced target)", restored.Epoch, receipt.ToEpoch+1)
	}
	if rollback.RestoredCell != source.Cell || rollback.Epoch != restored.Epoch || rollback.Digest == "" {
		t.Fatalf("rollback receipt = %+v, want restoration evidence", rollback)
	}
	if tenant.SortedStreamKinds(plan.Streams)[0] != "journal" {
		t.Fatalf("stream manifest changed by rollback: %+v", plan.Streams)
	}

	gate := tenant.RelocationGate{Current: restored, Key: relocationKey}
	for name, stale := range map[string]tenant.Placement{"source": source} {
		if err := gate.CheckWrite(stale); !errors.Is(err, tenant.ErrPlacementMismatch) {
			t.Fatalf("%s epoch write error = %v, want ErrPlacementMismatch", name, err)
		}
	}
	if err := gate.CheckWrite(restored); err != nil {
		t.Fatalf("restored placement rejected: %v", err)
	}

	if _, _, err := tenant.RollbackPlan(&plan, relocationKey); !errors.Is(err, tenant.ErrRelocationPhase) {
		t.Fatalf("second rollback error = %v, want ErrRelocationPhase", err)
	}
	if _, err := tenant.ReconcilePlan(&plan, relocationManifest()); !errors.Is(err, tenant.ErrRelocationPhase) {
		t.Fatalf("reconcile after rollback error = %v, want ErrRelocationPhase", err)
	}

	// Rollback before cutover still fences forward: same cell, next epoch.
	plan2, err := tenant.BeginRelocation("plan-early", source, relocationTarget(), relocationManifest(), relocationKey)
	if err != nil {
		t.Fatal(err)
	}
	early, _, err := tenant.RollbackPlan(&plan2, relocationKey)
	if err != nil {
		t.Fatalf("early RollbackPlan: %v", err)
	}
	if early.Cell != source.Cell || early.Epoch != source.Epoch+1 {
		t.Fatalf("early rollback = %+v, want source cell at epoch %d", early, source.Epoch+1)
	}
}

// TestTodo_TENANT_005_Fault covers every malformed or out-of-order input:
// each one fails closed with a typed sentinel.
func TestTodo_TENANT_005_Fault(t *testing.T) {
	source := relocationSource(t)
	manifest := relocationManifest()

	t.Run("nil plans", func(t *testing.T) {
		if _, _, err := tenant.CutoverPlan(nil, relocationKey); !errors.Is(err, tenant.ErrInvalidRelocation) {
			t.Fatalf("CutoverPlan(nil) = %v, want ErrInvalidRelocation", err)
		}
		if _, err := tenant.ReconcilePlan(nil, manifest); !errors.Is(err, tenant.ErrInvalidRelocation) {
			t.Fatalf("ReconcilePlan(nil) = %v, want ErrInvalidRelocation", err)
		}
		if _, _, err := tenant.RollbackPlan(nil, relocationKey); !errors.Is(err, tenant.ErrInvalidRelocation) {
			t.Fatalf("RollbackPlan(nil) = %v, want ErrInvalidRelocation", err)
		}
		var nilPlan *tenant.RelocationPlan
		if err := nilPlan.RecordPhase(tenant.RelocationFreeze, "d", "r"); !errors.Is(err, tenant.ErrInvalidRelocation) {
			t.Fatalf("RecordPhase(nil) = %v, want ErrInvalidRelocation", err)
		}
	})

	t.Run("begin faults", func(t *testing.T) {
		unsigned := source
		unsigned.Signature = ""
		dropped := manifest[:5]
		duplicated := append(append([]tenant.StreamRef(nil), manifest...), tenant.StreamRef{Kind: "key", ID: "key-002"})
		sameCell := relocationTarget()
		sameCell.Cell = source.Cell
		for name, args := range map[string]struct {
			id      string
			current tenant.Placement
			target  tenant.RelocationTarget
			streams []tenant.StreamRef
		}{
			"empty plan id":   {id: "", current: source, target: relocationTarget(), streams: manifest},
			"unsigned source": {id: "p", current: unsigned, target: relocationTarget(), streams: manifest},
			"empty target":    {id: "p", current: source, target: tenant.RelocationTarget{}, streams: manifest},
			"same cell":       {id: "p", current: source, target: sameCell, streams: manifest},
			"empty manifest":  {id: "p", current: source, target: relocationTarget(), streams: nil},
			"missing plane":   {id: "p", current: source, target: relocationTarget(), streams: dropped},
			"duplicate plane": {id: "p", current: source, target: relocationTarget(), streams: duplicated},
		} {
			t.Run(name, func(t *testing.T) {
				if _, err := tenant.BeginRelocation(args.id, args.current, args.target, args.streams, relocationKey); !errors.Is(err, tenant.ErrInvalidRelocation) && !errors.Is(err, tenant.ErrInvalidSignature) {
					t.Fatalf("BeginRelocation = %v, want ErrInvalidRelocation or ErrInvalidSignature", err)
				}
			})
		}
		if _, err := tenant.BeginRelocation("p", source, relocationTarget(), dropped, relocationKey); err == nil || !strings.Contains(err.Error(), `"key"`) {
			t.Fatalf("missing-plane error = %v, want it to name the key plane", err)
		}
	})

	t.Run("phase order and evidence", func(t *testing.T) {
		plan, err := tenant.BeginRelocation("plan-o", source, relocationTarget(), manifest, relocationKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.RecordPhase(tenant.RelocationCopy, "d", "r"); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("skipped phase error = %v, want ErrRelocationPhase", err)
		}
		if err := plan.RecordPhase(tenant.RelocationFreeze, "", ""); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("empty evidence error = %v, want ErrRelocationPhase", err)
		}
		if err := plan.RecordPhase(tenant.RelocationFreeze, "sha256:freeze", "freeze-1"); err != nil {
			t.Fatal(err)
		}
		if err := plan.RecordPhase(tenant.RelocationFreeze, "sha256:freeze", "freeze-1"); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("repeated phase error = %v, want ErrRelocationPhase", err)
		}
		if _, _, err := tenant.CutoverPlan(&plan, relocationKey); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("early cutover error = %v, want ErrRelocationPhase", err)
		}
	})

	t.Run("reconcile faults", func(t *testing.T) {
		plan, err := tenant.BeginRelocation("plan-r", source, relocationTarget(), manifest, relocationKey)
		if err != nil {
			t.Fatal(err)
		}
		advanceRelocation(t, &plan)
		if _, err := tenant.ReconcilePlan(&plan, manifest); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("reconcile before cutover = %v, want ErrRelocationPhase", err)
		}
		if _, _, err := tenant.CutoverPlan(&plan, relocationKey); err != nil {
			t.Fatal(err)
		}
		missing := manifest[:5]
		if _, err := tenant.ReconcilePlan(&plan, missing); err == nil || !strings.Contains(err.Error(), `"key"`) {
			t.Fatalf("missing stream error = %v, want it to name the key plane", err)
		}
		swapped := append([]tenant.StreamRef(nil), manifest...)
		swapped[0].ID = "wf-999"
		if _, err := tenant.ReconcilePlan(&plan, swapped); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("swapped stream error = %v, want ErrRelocationPhase", err)
		}
		extra := append(append([]tenant.StreamRef(nil), manifest...), tenant.StreamRef{Kind: "search", ID: "s-1"})
		if _, err := tenant.ReconcilePlan(&plan, extra); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("extra stream error = %v, want ErrRelocationPhase", err)
		}
		if _, err := tenant.ReconcilePlan(&plan, manifest); err != nil {
			t.Fatalf("exact reconcile: %v", err)
		}
		if err := plan.RecordPhase(tenant.RelocationFreeze, "d", "r"); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("phase after complete = %v, want ErrRelocationPhase", err)
		}
		if _, _, err := tenant.CutoverPlan(&plan, relocationKey); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("cutover after complete = %v, want ErrRelocationPhase", err)
		}
		if _, _, err := tenant.RollbackPlan(&plan, relocationKey); !errors.Is(err, tenant.ErrRelocationPhase) {
			t.Fatalf("rollback after complete = %v, want ErrRelocationPhase", err)
		}
	})
}
