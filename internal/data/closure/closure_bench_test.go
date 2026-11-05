package closure_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/closure"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_WF_DATA_020_Benchmark(t *testing.T) {
	db := pgtest.New(t)
	tenant := closureTenant(t, db, "closure-bench")
	ctx, tx := closureTx(t, db, tenant)
	hier := "reporting"
	if _, err := closure.DeclareHierarchy(ctx, tx, tenant, hier, 16, closureKnown(0)); err != nil {
		t.Fatalf("DeclareHierarchy: %v", err)
	}
	tick := 10
	add := func(parent, child string) {
		t.Helper()
		tick++
		applied, err := closure.AddEdge(ctx, tx, tenant, hier, parent, child, closureDay(0), closureKnown(tick), fmt.Sprintf("bench-b-%d", tick))
		if err != nil || !applied {
			t.Fatalf("AddEdge %s->%s: %v %v", parent, child, applied, err)
		}
	}
	add("corp", "op")
	add("corp", "np")
	built := 0
	queue := []string{"op"}
	for len(queue) > 0 && built < 2000 {
		parent := queue[0]
		queue = queue[1:]
		for b := 0; b < 4 && built < 2000; b++ {
			child := fmt.Sprintf("n%04d", built)
			built++
			add(parent, child)
			queue = append(queue, child)
		}
	}
	if built != 2000 {
		t.Fatalf("built %d subtree units, want 2000", built)
	}
	tick++
	reparentKnown := closureKnown(tick)
	start := time.Now()
	if err := closure.Reparent(ctx, tx, tenant, hier, "op", "np", closureDay(60), reparentKnown, "bench-reparent"); err != nil {
		t.Fatalf("Reparent: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("re-parented 2000-unit subtree in %v", elapsed)
	if elapsed > 60*time.Second {
		t.Fatalf("re-parent took %v, over the 60s propagation budget", elapsed)
	}
	desc, err := closure.Descendants(ctx, tx, tenant, hier, "np", closureDay(100), closureKnown(tick+1000))
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	if len(desc) != 2001 {
		t.Fatalf("descendants(np) = %d, want 2001 (op + 2000 units)", len(desc))
	}
	anc, err := closure.Ancestors(ctx, tx, tenant, hier, "n1999", closureDay(100), closureKnown(tick+1000))
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	foundNP, foundOP := false, false
	for _, a := range anc {
		if a == "np" {
			foundNP = true
		}
		if a == "op" {
			foundOP = true
		}
	}
	if !foundNP || !foundOP || anc[len(anc)-1] != "corp" {
		t.Fatalf("leaf ancestors miss the re-parented path: %v", anc)
	}
	rel, err := closure.Related(ctx, tx, tenant, hier, "np", "n1999", closureDay(100), closureKnown(tick+1000))
	if err != nil || !rel {
		t.Fatalf("Related(np, n1999) = %v, %v, want true", rel, err)
	}
}
