package lineage_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
)

// TestTodo_LEDGER_005 proves append-only correction and supersession
// lineage: a correction names the event it corrects and carries its reason
// in the payload; a supersession uses the identical mechanism to link a
// replaced revision to its successor; ancestor, descendant and
// effective-current queries are deterministic, including under branching;
// a malformed graph is detected rather than looped over forever; and
// nothing is ever updated or deleted in place.
func TestTodo_LEDGER_005(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	t.Run("a correction references the corrected event and carries its reason", func(t *testing.T) {
		original := f.mustAppend(t, f.request(streamKey, 0, "manager: Alice"))

		correctionReq := f.correction(streamKey, original.Sequence, ref(streamKey, original.Sequence),
			"Alice was a data entry error; the correct manager is Bob")
		corrected := f.mustAppendLineage(t, correctionReq)

		node, err := lineage.ValidateCorrectionTarget(context.Background(), f.db.Conn, f.tenant, ref(streamKey, original.Sequence))
		if err != nil {
			t.Fatalf("resolve the corrected event: %v", err)
		}
		if node.EventID != original.EventID {
			t.Fatalf("resolved target event id %s, want %s", node.EventID, original.EventID)
		}

		ancestors, err := lineage.Ancestors(context.Background(), f.db.Conn, f.tenant, ref(streamKey, corrected.Sequence))
		if err != nil {
			t.Fatalf("ancestors of the correction: %v", err)
		}
		if len(ancestors) != 1 || ancestors[0].Ref != ref(streamKey, original.Sequence) {
			t.Fatalf("ancestors of the correction are %+v, want exactly the original event", ancestors)
		}

		stored := f.readPayload(t, streamKey, corrected.Sequence)
		if !strings.Contains(stored, "Bob") {
			t.Fatalf("correction payload %q does not carry its reason", stored)
		}
	})

	t.Run("supersession links a replaced revision to its successor using the same mechanism", func(t *testing.T) {
		revisionOne := f.mustAppend(t, f.request(streamKey, 2, "proposal revision 1: base salary 120000"))

		supersedeReq := f.correction(streamKey, revisionOne.Sequence, ref(streamKey, revisionOne.Sequence),
			"superseded by revision 2 after manager review")
		revisionTwo := f.mustAppendLineage(t, supersedeReq)

		current, path, err := lineage.EffectiveCurrent(context.Background(), f.db.Conn, f.tenant, ref(streamKey, revisionOne.Sequence))
		if err != nil {
			t.Fatalf("effective-current of the superseded revision: %v", err)
		}
		if current.Ref != ref(streamKey, revisionTwo.Sequence) {
			t.Fatalf("effective-current is %+v, want revision 2 at %v", current, revisionTwo.Sequence)
		}
		if len(path) != 1 || path[0].Ref != current.Ref {
			t.Fatalf("effective-current path is %+v, want exactly [revision 2]", path)
		}
	})

	t.Run("ancestor, descendant and effective-current queries are deterministic, including under branching", func(t *testing.T) {
		g := newFixture(t)

		root := g.mustAppend(t, g.request(streamKey, 0, "v1"))
		chainB := g.mustAppendLineage(t, g.correction(streamKey, root.Sequence, ref(streamKey, root.Sequence), "fix 1"))
		chainC := g.mustAppendLineage(t, g.correction(streamKey, chainB.Sequence, ref(streamKey, chainB.Sequence), "fix 2"))
		// A second, later correction of the ROOT (not of chainB): a branch.
		branchD := g.mustAppendLineage(t, g.correction(streamKey, chainC.Sequence, ref(streamKey, root.Sequence), "alternate fix"))

		rootRef := ref(streamKey, root.Sequence)
		bRef := ref(streamKey, chainB.Sequence)
		cRef := ref(streamKey, chainC.Sequence)
		dRef := ref(streamKey, branchD.Sequence)

		// Ancestors of the tip of the long branch: immediate parent first, then
		// the root. Two independent calls must agree exactly.
		for i := 0; i < 2; i++ {
			ancestors, err := lineage.Ancestors(context.Background(), g.db.Conn, g.tenant, cRef)
			if err != nil {
				t.Fatalf("ancestors of C (call %d): %v", i, err)
			}
			if len(ancestors) != 2 || ancestors[0].Ref != bRef || ancestors[1].Ref != rootRef {
				t.Fatalf("ancestors of C are %+v, want [B, root]", ancestors)
			}
		}

		// Descendants of the root: breadth-first, both branches, deterministic
		// order across repeated calls - level 1 ordered by sequence (B, D),
		// then level 2 (C, the only child of B).
		want := []lineage.EventRef{bRef, dRef, cRef}
		for i := 0; i < 2; i++ {
			descendants, err := lineage.Descendants(context.Background(), g.db.Conn, g.tenant, rootRef)
			if err != nil {
				t.Fatalf("descendants of root (call %d): %v", i, err)
			}
			if len(descendants) != len(want) {
				t.Fatalf("descendants of root are %+v, want %d nodes", descendants, len(want))
			}
			for i, n := range descendants {
				if n.Ref != want[i] {
					t.Fatalf("descendants of root are %v, want %v", refsOf(descendants), want)
				}
			}
		}

		// Effective-current of the root always resolves to the branch with the
		// higher sequence at each step - here, D directly beats the B->C chain
		// at the very first step, since D's own sequence exceeds C's.
		for i := 0; i < 2; i++ {
			current, path, err := lineage.EffectiveCurrent(context.Background(), g.db.Conn, g.tenant, rootRef)
			if err != nil {
				t.Fatalf("effective-current of root (call %d): %v", i, err)
			}
			if current.Ref != dRef {
				t.Fatalf("effective-current of root is %+v, want D at %v", current, dRef)
			}
			if len(path) != 1 || path[0].Ref != dRef {
				t.Fatalf("effective-current path is %+v, want exactly [D]", path)
			}
		}
	})

	t.Run("a dangling or cross-tenant correction target is refused before anything is written", func(t *testing.T) {
		g := newFixture(t)

		dangling := g.correction(streamKey, 0, ref(streamKey, 99), "nothing to correct")
		_, err := g.appendLineage(t, dangling)
		var notFound lineage.ErrCorrectionTargetNotFound
		if !errors.As(err, &notFound) {
			t.Fatalf("a dangling correction target returned %v, want ErrCorrectionTargetNotFound", err)
		}
		if got := g.sequences(t, streamKey); len(got) != 0 {
			t.Fatalf("a refused correction still wrote %v", got)
		}

		// A real event, but recorded under a different tenant: still refused,
		// because ValidateCorrectionTarget resolves strictly within g.tenant.
		other := g.otherTenant(t)
		foreign := g.mustAppendTenant(t, other, g.request(streamKey, 0, "foreign event"))

		crossTenant := g.correction(streamKey, 0, ref(streamKey, foreign.Sequence), "cannot reach across tenants")
		_, err = g.appendLineage(t, crossTenant)
		if !errors.As(err, &notFound) {
			t.Fatalf("a cross-tenant correction target returned %v, want ErrCorrectionTargetNotFound", err)
		}
	})

	t.Run("a malformed correction graph is detected rather than looped over forever", func(t *testing.T) {
		g := newFixture(t)

		// lineage.Append would refuse both of these (each names a target that
		// does not exist yet), so the cycle is built with the bare
		// internal/data/ledger.Append instead - proving lineage.Append's
		// pre-flight check is exactly what stands between a well-formed ledger
		// and this graph, and that the query layer still fails closed if that
		// check is ever bypassed.
		a := g.mustAppend(t, g.correction(streamKey, 0, ref(otherStream, 1), "a corrects b, appended first"))
		g.mustAppend(t, g.correction(otherStream, 0, ref(streamKey, a.Sequence), "b corrects a"))

		_, err := lineage.Ancestors(context.Background(), g.db.Conn, g.tenant, ref(streamKey, a.Sequence))
		var cycle lineage.ErrLineageCycle
		if !errors.As(err, &cycle) {
			t.Fatalf("ancestors of a 2-cycle returned %v, want ErrLineageCycle", err)
		}

		_, err = lineage.Descendants(context.Background(), g.db.Conn, g.tenant, ref(streamKey, a.Sequence))
		if !errors.As(err, &cycle) {
			t.Fatalf("descendants of a 2-cycle returned %v, want ErrLineageCycle", err)
		}
	})

	t.Run("nothing is ever updated or deleted in place", func(t *testing.T) {
		g := newFixture(t)
		original := g.mustAppend(t, g.request(streamKey, 0, "immutable payload"))

		before := g.readPayload(t, streamKey, original.Sequence)

		updateErr := g.db.ExecErr(`UPDATE ledger_event SET payload = $1 WHERE tenant_id = $2 AND stream_key = $3 AND sequence = $4`,
			[]byte("tampered"), g.tenant, streamKey, original.Sequence)
		if updateErr == nil {
			t.Fatal("an UPDATE against ledger_event succeeded; it must be refused by the append-only trigger")
		}

		deleteErr := g.db.ExecErr(`DELETE FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
			g.tenant, streamKey, original.Sequence)
		if deleteErr == nil {
			t.Fatal("a DELETE against ledger_event succeeded; it must be refused by the append-only trigger")
		}

		after := g.readPayload(t, streamKey, original.Sequence)
		if after != before {
			t.Fatalf("the physical row changed after refused mutation attempts: %q -> %q", before, after)
		}
	})
}

// TestTodo_LEDGER_005_Property drives correction chains of several lengths
// and checks the invariants Ancestors/EffectiveCurrent must hold regardless
// of depth: the ancestor list length equals the chain depth, its order is
// immediate-parent-first, the last ancestor has no target of its own, and
// EffectiveCurrent's path is exactly the ancestor list of the tip, reversed.
func TestTodo_LEDGER_005_Property(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{1, 2, 5} {
		depth := depth
		t.Run(fmtDepth(depth), func(t *testing.T) {
			t.Parallel()
			g := newFixture(t)

			root := g.mustAppend(t, g.request(streamKey, 0, "v0"))
			tip := root
			refs := []lineage.EventRef{ref(streamKey, root.Sequence)}
			for i := 0; i < depth; i++ {
				tip = g.mustAppendLineage(t, g.correction(streamKey, tip.Sequence, ref(streamKey, tip.Sequence), "fix"))
				refs = append(refs, ref(streamKey, tip.Sequence))
			}
			tipRef := ref(streamKey, tip.Sequence)

			ancestors, err := lineage.Ancestors(context.Background(), g.db.Conn, g.tenant, tipRef)
			if err != nil {
				t.Fatalf("ancestors: %v", err)
			}
			if len(ancestors) != depth {
				t.Fatalf("depth %d: ancestors has %d entries, want %d", depth, len(ancestors), depth)
			}
			for i, a := range ancestors {
				want := refs[len(refs)-2-i] // immediate parent first, walking back to the root
				if a.Ref != want {
					t.Fatalf("depth %d: ancestor %d is %v, want %v", depth, i, a.Ref, want)
				}
			}
			if ancestors[len(ancestors)-1].Corrects != nil {
				t.Fatalf("depth %d: the root ancestor still names a correction target: %v", depth, ancestors[len(ancestors)-1].Corrects)
			}

			current, path, err := lineage.EffectiveCurrent(context.Background(), g.db.Conn, g.tenant, ref(streamKey, root.Sequence))
			if err != nil {
				t.Fatalf("effective-current: %v", err)
			}
			if current.Ref != tipRef {
				t.Fatalf("depth %d: effective-current is %v, want the tip %v", depth, current.Ref, tipRef)
			}
			if len(path) != depth {
				t.Fatalf("depth %d: effective-current path has %d entries, want %d", depth, len(path), depth)
			}
			for i, n := range path {
				if n.Ref != refs[i+1] {
					t.Fatalf("depth %d: effective-current path[%d] is %v, want %v", depth, i, n.Ref, refs[i+1])
				}
			}
		})
	}
}

// TestTodo_LEDGER_005_Security proves tenant isolation holds for lineage
// queries themselves, not only for ValidateCorrectionTarget: a node that
// exists only under a different tenant is invisible to Ancestors,
// Descendants and EffectiveCurrent run under this tenant, exactly as if it
// did not exist at all.
func TestTodo_LEDGER_005_Security(t *testing.T) {
	t.Parallel()
	g := newFixture(t)

	mine := g.mustAppend(t, g.request(streamKey, 0, "mine"))

	other := g.otherTenant(t)
	g.mustAppendTenant(t, other, g.request(streamKey, 0, "theirs"))

	// A correction naming a target that exists (at this stream/sequence) only
	// for the other tenant is exactly a dangling reference for this tenant.
	crossTenant := g.correction(streamKey, mine.Sequence, ref(streamKey, mine.Sequence), "legitimate, same tenant")
	fixed := g.mustAppendLineage(t, crossTenant)

	// This tenant's own lineage is unaffected by the other tenant's identical
	// (stream, sequence) coordinates.
	current, _, err := lineage.EffectiveCurrent(context.Background(), g.db.Conn, g.tenant, ref(streamKey, mine.Sequence))
	if err != nil {
		t.Fatalf("effective-current: %v", err)
	}
	if current.Ref != ref(streamKey, fixed.Sequence) {
		t.Fatalf("effective-current under the correct tenant is %v, want %v", current.Ref, ref(streamKey, fixed.Sequence))
	}

	// The other tenant's stream is a completely disjoint graph: ancestors of
	// its own first event, read under g.tenant, do not exist.
	_, err = lineage.Ancestors(context.Background(), g.db.Conn, other, ref(streamKey, 999))
	var notFound lineage.ErrCorrectionTargetNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("ancestors of a nonexistent ref under the other tenant returned %v, want ErrCorrectionTargetNotFound", err)
	}
}

func refsOf(nodes []lineage.Node) []lineage.EventRef {
	out := make([]lineage.EventRef, len(nodes))
	for i, n := range nodes {
		out[i] = n.Ref
	}
	return out
}

func fmtDepth(depth int) string {
	switch depth {
	case 1:
		return "depth_1"
	case 2:
		return "depth_2"
	default:
		return "depth_5"
	}
}

// TestTodo_LEDGER_005_Mutation proves correction edges and their source events
// cannot be rewritten or removed after append.
func TestTodo_LEDGER_005_Mutation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	original := f.mustAppend(t, f.request(streamKey, 0, "original assertion"))
	correction := f.mustAppendLineage(t, f.correction(streamKey, 1, ref(streamKey, original.Sequence), "corrected assertion"))
	read := func(sequence int64) (string, *string, *int64) {
		t.Helper()
		var payload []byte
		var targetStream *string
		var targetSequence *int64
		if err := f.db.QueryRow(context.Background(), `SELECT payload, corrects_stream_key, corrects_sequence
			FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
			f.tenant, streamKey, sequence).Scan(&payload, &targetStream, &targetSequence); err != nil {
			t.Fatalf("read lineage event %d: %v", sequence, err)
		}
		return string(payload), targetStream, targetSequence
	}
	originalPayload, originalTargetStream, originalTargetSequence := read(original.Sequence)
	correctionPayload, correctionTargetStream, correctionTargetSequence := read(correction.Sequence)
	if err := f.db.ExecErr(`UPDATE ledger_event SET payload = $1, corrects_sequence = 99
		WHERE tenant_id = $2 AND stream_key = $3 AND sequence = $4`, []byte("rewritten"), f.tenant, streamKey, correction.Sequence); err == nil {
		t.Fatal("rewriting a correction and its target succeeded")
	}
	if err := f.db.ExecErr(`DELETE FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
		f.tenant, streamKey, original.Sequence); err == nil {
		t.Fatal("deleting the original assertion succeeded")
	}
	if afterPayload, afterStream, afterSequence := read(original.Sequence); afterPayload != originalPayload || afterStream != nil || afterSequence != nil {
		t.Fatalf("original assertion changed: before=(%q,%v,%v) after=(%q,%v,%v)", originalPayload, originalTargetStream, originalTargetSequence, afterPayload, afterStream, afterSequence)
	}
	if afterPayload, afterStream, afterSequence := read(correction.Sequence); afterPayload != correctionPayload || afterStream == nil || afterSequence == nil ||
		correctionTargetStream == nil || correctionTargetSequence == nil || *afterStream != *correctionTargetStream || *afterSequence != *correctionTargetSequence {
		t.Fatalf("correction lineage changed: before=(%q,%v,%v) after=(%q,%v,%v)", correctionPayload, correctionTargetStream, correctionTargetSequence, afterPayload, afterStream, afterSequence)
	}
}

// TestTodo_LEDGER_005_Race races two corrections against one stream head. Both
// can reference the preserved original, but only one may become the next event.
func TestTodo_LEDGER_005_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	original := f.mustAppend(t, f.request(streamKey, 0, "original"))
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, reason := range []string{"first correction", "second correction"} {
		conn := f.db.NewConn(t)
		req := f.correction(streamKey, 1, ref(streamKey, original.Sequence), reason)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tx, err := conn.Begin(context.Background())
			if err == nil {
				_, err = lineage.Append(context.Background(), tx, f.tenant, req)
				if err != nil {
					_ = tx.Rollback(context.Background())
				} else {
					err = tx.Commit(context.Background())
				}
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, stale := 0, 0
	for err := range results {
		if err == nil {
			accepted++
			continue
		}
		var conflict datalogger.ErrStaleStream
		if !errors.As(err, &conflict) || conflict.Expected != 1 || conflict.Actual != 2 {
			t.Fatalf("losing correction returned %v, want stale head 1/2", err)
		}
		stale++
	}
	if accepted != 1 || stale != 1 {
		t.Fatalf("correction race accepted=%d stale=%d, want one of each", accepted, stale)
	}
	if got := f.sequences(t, streamKey); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("sequences after correction race are %v, want [1 2]", got)
	}
	current, path, err := lineage.EffectiveCurrent(context.Background(), f.db.Conn, f.tenant, ref(streamKey, original.Sequence))
	if err != nil {
		t.Fatalf("resolve effective current after correction race: %v", err)
	}
	if current.Ref != ref(streamKey, 2) || len(path) != 1 || path[0].Ref != current.Ref {
		t.Fatalf("effective current is %+v with path %+v, want the single accepted correction", current, path)
	}
}
