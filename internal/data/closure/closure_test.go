package closure_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/closure"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var closureBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func closureDay(n int) time.Time { return closureBase.AddDate(0, 0, n) }
func closureKnown(n int) time.Time {
	return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Hour)
}

func closureTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func closureTx(t *testing.T, db *pgtest.DB, tenant uuid.UUID) (context.Context, dbport.Tx) {
	t.Helper()
	ctx := context.Background()
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume role: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return ctx, tx
}

func closureAdd(t *testing.T, ctx context.Context, tx dbport.Tx, tenant uuid.UUID, hier, parent, child string, fromN, knownN int, event string) {
	t.Helper()
	applied, err := closure.AddEdge(ctx, tx, tenant, hier, parent, child, closureDay(fromN), closureKnown(knownN), event)
	if err != nil {
		t.Fatalf("AddEdge %s->%s: %v", parent, child, err)
	}
	if !applied {
		t.Fatalf("AddEdge %s->%s not applied", parent, child)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTodo_WF_DATA_020(t *testing.T) {
	db := pgtest.New(t)
	tenant := closureTenant(t, db, "closure-primary")
	ctx, tx := closureTx(t, db, tenant)

	if _, err := closure.DeclareHierarchy(ctx, tx, tenant, "reporting", 8, closureKnown(0)); err != nil {
		t.Fatalf("DeclareHierarchy: %v", err)
	}
	closureAdd(t, ctx, tx, tenant, "reporting", "ceo", "vp", 0, 10, "e1")
	closureAdd(t, ctx, tx, tenant, "reporting", "vp", "mgr", 0, 11, "e2")
	closureAdd(t, ctx, tx, tenant, "reporting", "mgr", "ic", 0, 12, "e3")

	anc, err := closure.Ancestors(ctx, tx, tenant, "reporting", "ic", closureDay(15), closureKnown(100))
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if !sameStrings(anc, []string{"mgr", "vp", "ceo"}) {
		t.Fatalf("ancestors(ic) = %v, want [mgr vp ceo]", anc)
	}
	desc, err := closure.Descendants(ctx, tx, tenant, "reporting", "ceo", closureDay(15), closureKnown(100))
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	if !sameStrings(desc, []string{"vp", "mgr", "ic"}) {
		t.Fatalf("descendants(ceo) = %v, want [vp mgr ic]", desc)
	}
	related, err := closure.Related(ctx, tx, tenant, "reporting", "vp", "ic", closureDay(15), closureKnown(100))
	if err != nil || !related {
		t.Fatalf("Related(vp, ic) = %v, %v, want true", related, err)
	}
	related, _ = closure.Related(ctx, tx, tenant, "reporting", "ic", "vp", closureDay(15), closureKnown(100))
	if related {
		t.Fatal("Related(ic, vp) = true, want false")
	}

	closureAdd(t, ctx, tx, tenant, "reporting", "ceo", "cto", 0, 13, "e4")
	if err := closure.Reparent(ctx, tx, tenant, "reporting", "mgr", "cto", closureDay(60), closureKnown(200), "ev-reparent"); err != nil {
		t.Fatalf("Reparent: %v", err)
	}
	anc, _ = closure.Ancestors(ctx, tx, tenant, "reporting", "ic", closureDay(30), closureKnown(300))
	if !sameStrings(anc, []string{"mgr", "vp", "ceo"}) {
		t.Fatalf("ancestors(ic)@Feb = %v, want [mgr vp ceo]", anc)
	}
	anc, _ = closure.Ancestors(ctx, tx, tenant, "reporting", "ic", closureDay(100), closureKnown(300))
	if !sameStrings(anc, []string{"mgr", "cto", "ceo"}) {
		t.Fatalf("ancestors(ic)@Apr = %v, want [mgr cto ceo]", anc)
	}
	anc, _ = closure.Ancestors(ctx, tx, tenant, "reporting", "ic", closureDay(100), closureKnown(50))
	if !sameStrings(anc, []string{"mgr", "vp", "ceo"}) {
		t.Fatalf("ancestors(ic)@Apr-known-Feb = %v, want [mgr vp ceo]", anc)
	}

	if _, err := closure.DeclareHierarchy(ctx, tx, tenant, "custom:mentors", 4, closureKnown(0)); err != nil {
		t.Fatalf("DeclareHierarchy mentors: %v", err)
	}
	closureAdd(t, ctx, tx, tenant, "custom:mentors", "senior", "junior", 0, 10, "m1")
	anc, _ = closure.Ancestors(ctx, tx, tenant, "custom:mentors", "junior", closureDay(15), closureKnown(100))
	if !sameStrings(anc, []string{"senior"}) {
		t.Fatalf("mentors ancestors = %v", anc)
	}

	other := closureTenant(t, db, "closure-other")
	_, tx2 := closureTx(t, db, other)
	anc, err = closure.Ancestors(ctx, tx2, other, "reporting", "ic", closureDay(15), closureKnown(100))
	if err != nil || len(anc) != 0 {
		t.Fatalf("cross-tenant ancestors = %v, %v, want none", anc, err)
	}

	if _, err := closure.DeclareHierarchy(ctx, tx, tenant, "shallow", 1, closureKnown(0)); err != nil {
		t.Fatalf("DeclareHierarchy shallow: %v", err)
	}
	closureAdd(t, ctx, tx, tenant, "shallow", "a", "b", 0, 10, "s1")
	if _, err := closure.AddEdge(ctx, tx, tenant, "shallow", "b", "c", closureDay(0), closureKnown(11), "s2"); !errors.Is(err, closure.ErrDepthExceeded) {
		t.Fatalf("depth err = %v, want ErrDepthExceeded", err)
	}
}

type closureOracleEdge struct {
	parent, child string
	from, to      time.Time
	hasTo         bool
	known         time.Time
}

func closureOracleEffective(edges []closureOracleEdge, eff, known time.Time) map[string]string {
	type key struct{ parent, child string }
	latest := make(map[key]closureOracleEdge)
	for _, e := range edges {
		if e.known.After(known) {
			continue
		}
		k := key{e.parent, e.child}
		if cur, ok := latest[k]; !ok || e.known.After(cur.known) {
			latest[k] = e
		}
	}
	childParent := make(map[string]string)
	for _, e := range latest {
		if (e.from.Before(eff) || e.from.Equal(eff)) && (!e.hasTo || eff.Before(e.to)) {
			childParent[e.child] = e.parent
		}
	}
	return childParent
}

func closureOracleAncestors(childParent map[string]string, node string) []string {
	var out []string
	for cur, ok := childParent[node]; ok; cur, ok = childParent[cur] {
		out = append(out, cur)
	}
	return out
}

func closureOracleDescendants(childParent map[string]string, node string) []string {
	children := make(map[string][]string)
	for child, parent := range childParent {
		children[parent] = append(children[parent], child)
	}
	var out []string
	queue := append([]string(nil), children[node]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, cur)
		queue = append(queue, children[cur]...)
	}
	sort.Strings(out)
	return out
}

func TestTodo_WF_DATA_020_Property(t *testing.T) {
	db := pgtest.New(t)
	for seed := int64(1); seed <= 10; seed++ {
		rng := rand.New(rand.NewSource(seed))
		tenant := closureTenant(t, db, fmt.Sprintf("closure-prop-%d", seed))
		ctx, tx := closureTx(t, db, tenant)
		hier := "reporting"
		if _, err := closure.DeclareHierarchy(ctx, tx, tenant, hier, 16, closureKnown(0)); err != nil {
			t.Fatalf("seed %d: DeclareHierarchy: %v", seed, err)
		}
		n := 3 + rng.Intn(9)
		names := make([]string, n)
		for i := range names {
			names[i] = fmt.Sprintf("u%d", i)
		}
		var events []closureOracleEdge
		knownTick := 10
		for i := 1; i < n; i++ {
			parent := names[rng.Intn(i)]
			from := closureDay(rng.Intn(20))
			knownTick++
			closureAdd(t, ctx, tx, tenant, hier, parent, names[i], int(from.Sub(closureBase).Hours()/24), knownTick, fmt.Sprintf("s%d-e%d", seed, i))
			events = append(events, closureOracleEdge{parent: parent, child: names[i], from: from, known: closureKnown(knownTick)})
		}
		ended := make(map[string]bool)
		for i := 0; i < n/3; i++ {
			pick := events[rng.Intn(len(events))]
			ek := pick.parent + ">" + pick.child
			if ended[ek] {
				continue
			}
			ended[ek] = true
			to := pick.from.AddDate(0, 0, 30+rng.Intn(30))
			knownTick++
			applied, err := closure.EndEdge(ctx, tx, tenant, hier, pick.parent, pick.child, pick.from, to, closureKnown(knownTick), fmt.Sprintf("s%d-x%d", seed, i))
			if err != nil || !applied {
				t.Fatalf("seed %d: EndEdge: %v %v", seed, applied, err)
			}
			events = append(events, closureOracleEdge{parent: pick.parent, child: pick.child, from: pick.from, to: to, hasTo: true, known: closureKnown(knownTick)})
		}
		for q := 0; q < 6; q++ {
			node := names[rng.Intn(n)]
			eff := closureDay(rng.Intn(90))
			known := closureKnown(10 + rng.Intn(knownTick))
			childParent := closureOracleEffective(events, eff, known)
			wantAnc := closureOracleAncestors(childParent, node)
			gotAnc, err := closure.Ancestors(ctx, tx, tenant, hier, node, eff, known)
			if err != nil {
				t.Fatalf("seed %d: Ancestors: %v", seed, err)
			}
			if !sameStrings(gotAnc, wantAnc) {
				t.Fatalf("seed %d: ancestors(%s) = %v, oracle %v", seed, node, gotAnc, wantAnc)
			}
			wantDesc := closureOracleDescendants(childParent, node)
			gotDesc, err := closure.Descendants(ctx, tx, tenant, hier, node, eff, known)
			if err != nil {
				t.Fatalf("seed %d: Descendants: %v", seed, err)
			}
			sort.Strings(gotDesc)
			if !sameStrings(gotDesc, wantDesc) {
				t.Fatalf("seed %d: descendants(%s) = %v, oracle %v", seed, node, gotDesc, wantDesc)
			}
		}
	}
}

func TestTodo_WF_DATA_020_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := closureTenant(t, db, "closure-recovery")
	ctx, tx := closureTx(t, db, tenant)
	hier := "reporting"
	if _, err := closure.DeclareHierarchy(ctx, tx, tenant, hier, 8, closureKnown(0)); err != nil {
		t.Fatalf("DeclareHierarchy: %v", err)
	}
	edges := [][2]string{{"ceo", "vp1"}, {"ceo", "vp2"}, {"vp1", "mgr1"}, {"vp1", "mgr2"}, {"mgr1", "ic1"}}
	for i, e := range edges {
		if _, err := tx.Exec(ctx, `INSERT INTO hierarchy_edge
(tenant_id, hierarchy_key, parent_id, child_id, effective_from, effective_to, known_at, event_id)
VALUES ($1, $2, $3, $4, $5::date, NULL, $6, $7)`, tenant, hier, e[0], e[1], closureDay(0), closureKnown(10+i), fmt.Sprintf("bulk-%d", i)); err != nil {
			t.Fatalf("bulk edge: %v", err)
		}
	}
	before, err := closure.CountClosure(ctx, tx, tenant, hier)
	if err != nil || before != 0 {
		t.Fatalf("closure rows before rebuild = %d, %v, want 0", before, err)
	}
	if err := closure.Rebuild(ctx, tx, tenant, hier, closureKnown(100), "rebuild-1"); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	anc, err := closure.Ancestors(ctx, tx, tenant, hier, "ic1", closureDay(15), closureKnown(200))
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if !sameStrings(anc, []string{"mgr1", "vp1", "ceo"}) {
		t.Fatalf("ancestors(ic1) = %v", anc)
	}
	desc, err := closure.Descendants(ctx, tx, tenant, hier, "ceo", closureDay(15), closureKnown(200))
	if err != nil || len(desc) != 5 {
		t.Fatalf("descendants(ceo) = %v, %v, want 5", desc, err)
	}
}

func TestTodo_WF_DATA_020_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := closureTenant(t, db, "closure-fault")
	ctx, tx := closureTx(t, db, tenant)
	hier := "reporting"
	if _, err := closure.DeclareHierarchy(ctx, tx, tenant, hier, 8, closureKnown(0)); err != nil {
		t.Fatalf("DeclareHierarchy: %v", err)
	}
	closureAdd(t, ctx, tx, tenant, hier, "a", "b", 0, 10, "f1")
	closureAdd(t, ctx, tx, tenant, hier, "b", "c", 0, 11, "f2")
	before, _ := closure.CountClosure(ctx, tx, tenant, hier)
	if _, err := closure.AddEdge(ctx, tx, tenant, hier, "c", "a", closureDay(0), closureKnown(12), "f3"); !errors.Is(err, closure.ErrCycle) {
		t.Fatalf("cycle err = %v, want ErrCycle", err)
	}
	if _, err := closure.AddEdge(ctx, tx, tenant, hier, "x", "x", closureDay(0), closureKnown(12), "f4"); !errors.Is(err, closure.ErrClosureInvalid) {
		t.Fatalf("self err = %v, want ErrClosureInvalid", err)
	}
	if _, err := closure.AddEdge(ctx, tx, tenant, hier, "", "y", closureDay(0), closureKnown(12), "f5"); !errors.Is(err, closure.ErrClosureInvalid) {
		t.Fatalf("blank err = %v, want ErrClosureInvalid", err)
	}
	if _, err := closure.EndEdge(ctx, tx, tenant, hier, "a", "b", closureDay(10), closureDay(5), closureKnown(12), "f6"); !errors.Is(err, closure.ErrClosureInvalid) {
		t.Fatalf("inverted err = %v, want ErrClosureInvalid", err)
	}
	after, _ := closure.CountClosure(ctx, tx, tenant, hier)
	if after != before {
		t.Fatalf("closure rows %d -> %d across refused events", before, after)
	}
	applied, err := closure.AddEdge(ctx, tx, tenant, hier, "a", "b", closureDay(0), closureKnown(10), "f1")
	if err != nil || applied {
		t.Fatalf("replay = %v, %v, want applied=false", applied, err)
	}
	after, _ = closure.CountClosure(ctx, tx, tenant, hier)
	if after != before {
		t.Fatalf("replay changed closure rows %d -> %d", before, after)
	}
}
