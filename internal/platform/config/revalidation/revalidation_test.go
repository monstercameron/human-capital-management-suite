package revalidation

import (
	"strings"
	"sync"
	"testing"
)

func dependency(key string, kind DependencyKind, version string, active bool) Dependency {
	return Dependency{Key: key, Kind: kind, Version: version, Digest: "sha256:" + strings.Repeat("a", 64), Active: active}
}

// placeholderWorkload names no real customer or provider; refs are mechanics
// fixtures for the configuration dependency contract.
func placeholderWorkload() (Workload, []Dependency, Graph) {
	pinned := []Dependency{dependency("placeholder:promotion-rule", KindRule, "v1", true), dependency("placeholder:promotion-form", KindForm, "v1", true), dependency("placeholder:worker-mapping", KindMapping, "v1", true), dependency("placeholder:eligible-population", KindPopulation, "v1", true), dependency("placeholder:person-schema", KindSchema, "v1", true), dependency("placeholder:promotion-capability", KindCapability, "v1", true)}
	w := Workload{ID: "workload:placeholder-promotion", TenantID: "placeholder:design-partner", State: "PAUSED", Dependencies: pinned}
	return w, append([]Dependency(nil), pinned...), Graph{SchemaVersion: 1}
}

func TestActiveWorkloadDependencyRevocationProducesDeterministicInvalidateReplanMigrateOrBlock(t *testing.T) {
	w, current, graph := placeholderWorkload()
	decision, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != StatusValid || decision.EvidenceDigest == "" {
		t.Fatalf("unchanged workload = %+v", decision)
	}
	current[0].Active = false
	invalidated, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if invalidated.Status != StatusInvalidated {
		t.Fatalf("revoked dependency = %+v", invalidated)
	}
	current[0].Active = true
	current[0].Version = "v2"
	current[0].Digest = "sha256:" + strings.Repeat("b", 64)
	replanned, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if replanned.Status != StatusReplan {
		t.Fatalf("unreviewed incompatibility = %+v", replanned)
	}
	graph.Edges = []Compatibility{{Kind: KindRule, Key: current[0].Key, FromVersion: "v1", FromDigest: w.Dependencies[0].Digest, ToVersion: "v2", ToDigest: current[0].Digest, Compatible: true, MigrationRef: "placeholder:migration-review", Reviewed: true}}
	migrated, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Status != StatusMigrate {
		t.Fatalf("reviewed compatibility = %+v", migrated)
	}
	missing := append([]Dependency(nil), current...)
	missing[0].Key = "placeholder:unpublished-rule"
	blocked, err := Revalidate(w, missing, graph)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != StatusBlocked {
		t.Fatalf("unknown dependency = %+v", blocked)
	}
}

func TestTodo_CONFIG_010_Property(t *testing.T) {
	w, current, graph := placeholderWorkload()
	current[1].Version = "v2"
	one, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if one.Status != two.Status || one.EvidenceDigest != two.EvidenceDigest || strings.Join(one.Changed, "|") != strings.Join(two.Changed, "|") {
		t.Fatalf("non-deterministic decision: %+v %+v", one, two)
	}
}

func TestTodo_CONFIG_010_Golden(t *testing.T) {
	w, current, graph := placeholderWorkload()
	current[5].Active = false
	one, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	current[0], current[5] = current[5], current[0]
	two, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if one.EvidenceDigest != two.EvidenceDigest {
		t.Fatalf("dependency ordering changed evidence: %s != %s", one.EvidenceDigest, two.EvidenceDigest)
	}
}

func TestTodo_CONFIG_010_Race(t *testing.T) {
	w, current, graph := placeholderWorkload()
	r := NewRegistry()
	if err := r.Put(w); err != nil {
		t.Fatal(err)
	}
	current[0].Active = false
	var wg sync.WaitGroup
	decisions := make(chan Decision, 20)
	errs := make(chan error, 20)
	for i := 0; i < cap(decisions); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := r.Revalidate(w.ID, current, graph)
			decisions <- decision
			errs <- err
		}()
	}
	wg.Wait()
	close(decisions)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for decision := range decisions {
		if decision.Status != StatusInvalidated || decision.FenceToken == 0 {
			t.Fatalf("concurrent decision = %+v", decision)
		}
	}
	stored, ok := r.Get(w.ID)
	if !ok || stored.FenceToken != 20 || !stored.Fenced {
		t.Fatalf("concurrent revocations lost a fence update: %+v", stored)
	}
}

func TestTodo_CONFIG_010_Integration(t *testing.T) {
	w, current, graph := placeholderWorkload()
	current[0].Active = false
	r := NewRegistry()
	if err := r.Put(w); err != nil {
		t.Fatal(err)
	}
	decision, err := r.Revalidate(w.ID, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := r.Get(w.ID)
	if decision.Status != StatusInvalidated || !stored.Fenced || stored.FenceToken != 1 || decision.FenceToken != stored.FenceToken {
		t.Fatalf("atomic fence failed: decision=%+v workload=%+v", decision, stored)
	}
}

func TestTodo_CONFIG_010_Fault(t *testing.T) {
	w, current, graph := placeholderWorkload()
	current[0].Digest = ""
	if _, err := Revalidate(w, current, graph); err == nil {
		t.Fatal("malformed current dependency was accepted")
	}
}

func TestTodo_CONFIG_010_Security(t *testing.T) {
	w, current, graph := placeholderWorkload()
	current[0].Active = false
	decision, err := Revalidate(w, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status == StatusValid || !strings.Contains(Explain(), "fencing") {
		t.Fatalf("revocation was not fenced: %+v", decision)
	}
}

func TestTodo_CONFIG_010_Conformance(t *testing.T) {
	if Version() != 1 || !strings.Contains(Explain(), "invalidate") {
		t.Fatalf("contract metadata missing: version=%d explain=%q", Version(), Explain())
	}
	w, current, graph := placeholderWorkload()
	if err := Check(w, current, graph); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CONFIG_010_Recovery(t *testing.T) {
	w, current, graph := placeholderWorkload()
	current[0].Active = false
	r := NewRegistry()
	if err := r.Put(w); err != nil {
		t.Fatal(err)
	}
	decision, err := r.Revalidate(w.ID, current, graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Resume(w.ID, decision.EvidenceDigest, decision); err == nil {
		t.Fatal("fenced workload resumed without migration or replan")
	}
}

func TestTodo_CONFIG_010_ModelBased(t *testing.T) {
	w, current, graph := placeholderWorkload()
	for _, status := range []struct {
		mutate func([]Dependency)
		want   Status
	}{{func(c []Dependency) { c[0].Active = false }, StatusInvalidated}, {func(c []Dependency) { c[1].Version = "v2" }, StatusReplan}} {
		copyCurrent := append([]Dependency(nil), current...)
		status.mutate(copyCurrent)
		got, err := Revalidate(w, copyCurrent, graph)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != status.want {
			t.Fatalf("status=%s want=%s", got.Status, status.want)
		}
	}
}

func TestTodo_CONFIG_010_Mutation(t *testing.T) {
	w, current, graph := placeholderWorkload()
	before := w
	if _, err := Revalidate(w, current, graph); err != nil {
		t.Fatal(err)
	}
	if w.Fenced != before.Fenced || w.FenceToken != before.FenceToken {
		t.Fatal("pure classifier mutated workload")
	}
}
