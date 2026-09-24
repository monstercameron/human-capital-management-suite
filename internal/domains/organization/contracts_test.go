package organization

import (
	"errors"
	"testing"
	"time"
)

func org(id string) OrganizationUnit {
	return OrganizationUnit{ID: id, Tenant: "t1", Name: id, Type: "TEAM", EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func edge(id, from, to string) RelationshipEdge {
	return RelationshipEdge{ID: id, Tenant: "t1", Source: from, Target: to, Type: Hierarchy, EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func TestReadAsOfAndScopeClosure(t *testing.T) {
	s := Snapshot{Tenant: "t1", Watermark: "org:v7", ResolverPolicyVersion: "policy:v2", Units: []OrganizationUnit{org("root"), org("child"), org("other")}, Edges: []RelationshipEdge{edge("e1", "root", "child")}}
	r, err := Read(s, ReadRequest{Tenant: "t1", Root: "child", AsOf: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || r.Watermark != "org:v7" {
		t.Fatalf("result=%+v", r)
	}
}
func TestValidateRejectsCycleDuplicateAndCrossTenant(t *testing.T) {
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b")}, Edges: []RelationshipEdge{edge("1", "a", "b"), edge("2", "b", "a")}}).Validate(at); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle err=%v", err)
	}
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b"), org("c")}, Edges: []RelationshipEdge{edge("1", "a", "b"), edge("2", "c", "b")}}).Validate(at); !errors.Is(err, ErrDuplicateParent) {
		t.Fatalf("duplicate err=%v", err)
	}
	x := edge("1", "a", "b")
	x.Tenant = "t2"
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b")}, Edges: []RelationshipEdge{x}}).Validate(at); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("tenant err=%v", err)
	}
}
func TestReadFailsClosedAuthorization(t *testing.T) {
	s := Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("root"), org("child")}, Edges: []RelationshipEdge{edge("e", "root", "child")}}
	_, err := Read(s, ReadRequest{Tenant: "t1", Root: "root", AsOf: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Authorize: func(n OrganizationUnit) bool { return n.ID == "root" }})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Read(s, ReadRequest{Tenant: "t1", Root: "child", AsOf: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Authorize: func(n OrganizationUnit) bool { return false }})
	if len(r.Units) != 0 {
		t.Fatalf("unauthorized units leaked: %+v", r.Units)
	}
}

// Matrix coverage for ORG-001's named contract tests.
func TestTodo_ORG_001(t *testing.T) {
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	snapshot := Snapshot{Tenant: "t1", Watermark: "org:v7", Units: []OrganizationUnit{org("root"), org("child"), org("other")}, Edges: []RelationshipEdge{edge("e1", "root", "child")}}
	result, err := Read(snapshot, ReadRequest{Tenant: "t1", Root: "child", AsOf: at})
	if err != nil || len(result.Units) != 2 || result.Watermark != "org:v7" {
		t.Fatalf("scoped organization read = %+v, %v", result, err)
	}
	if err := snapshot.Validate(at); err != nil {
		t.Fatalf("valid hierarchy rejected: %v", err)
	}
	cycle := Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("a"), org("b")}, Edges: []RelationshipEdge{edge("ab", "a", "b"), edge("ba", "b", "a")}}
	if err := cycle.Validate(at); !errors.Is(err, ErrCycle) {
		t.Fatalf("cycle validation = %v, want ErrCycle", err)
	}
}

func TestTodo_ORG_001_Golden(t *testing.T) {
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s := Snapshot{Tenant: "t1", Watermark: "w9", ResolverPolicyVersion: "p3", Units: []OrganizationUnit{org("root"), org("parent"), org("child")}, Edges: []RelationshipEdge{edge("z", "parent", "child"), edge("a", "root", "parent")}}
	r, err := Ancestry(s, ReadRequest{Tenant: "t1", Root: "child", AsOf: at})
	if err != nil || len(r.Units) != 3 || r.Units[0].ID != "child" || r.Watermark != "w9" || r.ResolverPolicyVersion != "p3" {
		t.Fatalf("ancestry=%+v err=%v", r, err)
	}
}

func TestTodo_ORG_001_Mutation(t *testing.T) {
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	old := org("old")
	end := at.Add(-time.Hour)
	old.EffectiveTo = &end
	if err := (Snapshot{Tenant: "t1", Units: []OrganizationUnit{old}}).Validate(at); err != nil {
		t.Fatal(err)
	}
	bad := org("bad")
	bad.EffectiveTo = &bad.EffectiveFrom
	if _, err := Read(Snapshot{Tenant: "t1", Units: []OrganizationUnit{bad}}, ReadRequest{Tenant: "t1", Root: "bad", AsOf: at}); !errors.Is(err, ErrOrphan) {
		t.Fatalf("invalid interval should be inactive/orphan: %v", err)
	}
}

func TestTodo_ORG_001_Property(t *testing.T) {
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s := Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("r"), org("a"), org("b")}, Edges: []RelationshipEdge{edge("2", "r", "b"), edge("1", "r", "a")}}
	r, err := Descendency(s, ReadRequest{Tenant: "t1", Root: "r", AsOf: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Edges) != 2 || r.Edges[0].ID != "1" || r.Units[0].ID != "a" {
		t.Fatalf("non-deterministic result=%+v", r)
	}
}

func TestTodo_ORG_001_Security(t *testing.T) {
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s := Snapshot{Tenant: "t1", Units: []OrganizationUnit{org("r"), org("denied"), org("secret")}, Edges: []RelationshipEdge{edge("1", "r", "denied"), edge("2", "denied", "secret")}}
	r, err := Read(s, ReadRequest{Tenant: "t1", Root: "r", AsOf: at, Authorize: func(n OrganizationUnit) bool { return n.ID != "denied" }})
	if err != nil || len(r.Units) != 1 || r.Units[0].ID != "r" {
		t.Fatalf("denied intermediary traversal leaked scope: %+v err=%v", r, err)
	}
}
