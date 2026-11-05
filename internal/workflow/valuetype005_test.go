// WF-DATA-005 RED: the type system has no DURATION, RATE or REFERENCE
// kind.
package workflow_test

import (
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestTodo_WF_DATA_005 is the PRIMARY test: DURATION, RATE and REFERENCE
// join the type system under a new plan IR schema version; a reference to
// one kind never flows into another; there is still no untyped map.
func TestTodo_WF_DATA_005(t *testing.T) {
	t.Run("new kinds validate", func(t *testing.T) {
		duration := workflow.ValueType{Kind: workflow.KindDuration}
		rate := workflow.ValueType{Kind: workflow.KindRate}
		workerRef := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "worker"}
		customRef := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "custom:asset"}
		for _, typ := range []workflow.ValueType{duration, rate, workerRef, customRef} {
			if err := typ.Validate(); err != nil {
				t.Fatalf("%s: %v", typ, err)
			}
		}
		bare := workflow.ValueType{Kind: workflow.KindReference}
		if err := bare.Validate(); err == nil {
			t.Fatal("reference without a target validated")
		}
	})

	t.Run("references assign only within their target kind", func(t *testing.T) {
		worker := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "worker"}
		worker2 := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "worker"}
		position := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "position"}
		custom := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "custom:asset"}
		if err := worker.AssignableTo(worker2); err != nil {
			t.Fatalf("worker->worker: %v", err)
		}
		if err := worker.AssignableTo(position); err == nil {
			t.Fatal("worker reference flowed into a position target")
		}
		if err := worker.AssignableTo(custom); err == nil {
			t.Fatal("worker reference flowed into a custom-kind target")
		}
		if err := worker.AssignableTo(workflow.ValueType{Kind: workflow.KindString}); err == nil {
			t.Fatal("reference flowed into a bare string")
		}
		// Nullability still composes: nullable into nullable only.
		nullable := workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "worker", Nullable: true}
		if err := nullable.AssignableTo(worker); err == nil {
			t.Fatal("nullable reference flowed into a non-nullable target")
		}
		if err := nullable.AssignableTo(nullable); err != nil {
			t.Fatalf("nullable->nullable: %v", err)
		}
	})

	t.Run("new kinds require the new IR schema version", func(t *testing.T) {
		if got := (workflow.ValueType{Kind: workflow.KindString}).RequiresIRVersion(); got != 2 {
			t.Fatalf("STRING requires IR %d, want 2", got)
		}
		for _, kind := range []workflow.Kind{workflow.KindDuration, workflow.KindRate, workflow.KindReference} {
			typ := workflow.ValueType{Kind: kind, ReferenceTarget: "worker"}
			if got := typ.RequiresIRVersion(); got != 3 {
				t.Fatalf("%s requires IR %d, want 3", kind, got)
			}
		}
		fields := []workflow.Field{{Path: "name", Type: workflow.ValueType{Kind: workflow.KindString}}}
		if got := workflow.IRVersionForFields(fields); got != 2 {
			t.Fatalf("prior-IR fields require IR %d, want 2", got)
		}
		fields = append(fields, workflow.Field{Path: "tenure", Type: workflow.ValueType{Kind: workflow.KindDuration}})
		if got := workflow.IRVersionForFields(fields); got != 3 {
			t.Fatalf("duration fields require IR %d, want 3", got)
		}
	})

	t.Run("there is still no untyped map", func(t *testing.T) {
		for _, kind := range []workflow.Kind{"MAP", "OBJECT", "ANY", "JSON", ""} {
			if err := (workflow.ValueType{Kind: kind}).Validate(); err == nil {
				t.Fatalf("kind %q validated; the type algebra admits no untyped map", kind)
			}
		}
	})
}

// TestTodo_WF_DATA_005_Property covers assignability: reflexivity,
// target-kind separation for references, and brand/kind separation
// everywhere else, over generated types.
func TestTodo_WF_DATA_005_Property(t *testing.T) {
	targets := []string{"worker", "position", "custom:asset", "custom:case"}
	brands := []string{"", "WorkerID", "CaseKey"}
	for seed := int64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic property seed
		a, b := data005Type(rng, targets, brands), data005Type(rng, targets, brands)
		// Reflexivity: every generated type flows into itself.
		if err := a.AssignableTo(a); err != nil {
			t.Fatalf("seed %d: %s not assignable to itself: %v", seed, a, err)
		}
		err := a.AssignableTo(b)
		wantErr := a.Kind != b.Kind ||
			a.Brand != b.Brand ||
			(a.Kind == workflow.KindReference && a.ReferenceTarget != b.ReferenceTarget) ||
			(a.Kind == workflow.KindEnum && a.EnumRef != b.EnumRef) ||
			(a.Kind == workflow.KindMessage && a.MessageRef != b.MessageRef) ||
			(a.Nullable && !b.Nullable)
		if (err != nil) != wantErr {
			t.Fatalf("seed %d: %s -> %s err=%v wantErr=%v", seed, a, b, err, wantErr)
		}
		if err := a.Validate(); err != nil {
			t.Fatalf("seed %d: generated %s invalid: %v", seed, a, err)
		}
	}
}

func data005Type(rng *rand.Rand, targets, brands []string) workflow.ValueType {
	kinds := []workflow.Kind{
		workflow.KindString, workflow.KindInteger, workflow.KindDecimal,
		workflow.KindBool, workflow.KindInstant, workflow.KindLocalDate,
		workflow.KindMoney, workflow.KindDuration, workflow.KindRate,
		workflow.KindReference, workflow.KindEnum, workflow.KindMessage,
	}
	t := workflow.ValueType{Kind: kinds[rng.Intn(len(kinds))], Nullable: rng.Intn(2) == 0}
	switch t.Kind {
	case workflow.KindReference:
		t.ReferenceTarget = targets[rng.Intn(len(targets))]
	case workflow.KindEnum:
		t.EnumRef = "enum-" + string(rune('a'+rng.Intn(3)))
	case workflow.KindMessage:
		t.MessageRef = "msg-" + string(rune('a'+rng.Intn(3)))
	default:
		t.Brand = brands[rng.Intn(len(brands))]
	}
	return t
}

// TestTodo_WF_DATA_005_Golden pins the rendering and IR requirements:
// prior-IR plans render byte-identical, and the new kinds render with
// their targets.
func TestTodo_WF_DATA_005_Golden(t *testing.T) {
	prior := []workflow.Field{
		{Path: "name", Type: workflow.ValueType{Kind: workflow.KindString}},
		{Path: "salary", Type: workflow.ValueType{Kind: workflow.KindMoney}},
		{Path: "tags", Type: workflow.ValueType{Kind: workflow.KindList, Element: &workflow.ValueType{Kind: workflow.KindString}}},
	}
	var rendered []string
	for _, f := range prior {
		rendered = append(rendered, f.Path+":"+f.Type.String())
	}
	sort.Strings(rendered)
	const wantPrior = `name:STRING|salary:MONEY|tags:LIST<STRING>`
	if got := strings.Join(rendered, "|"); got != wantPrior {
		t.Fatalf("prior-IR rendering = %q, want %q", got, wantPrior)
	}
	if got := workflow.IRVersionForFields(prior); got != 2 {
		t.Fatalf("prior-IR version = %d, want 2", got)
	}
	next := []workflow.Field{
		{Path: "tenure", Type: workflow.ValueType{Kind: workflow.KindDuration}},
		{Path: "raise", Type: workflow.ValueType{Kind: workflow.KindRate}},
		{Path: "manager", Type: workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "worker"}},
		{Path: "asset", Type: workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "custom:asset", Nullable: true}},
	}
	rendered = rendered[:0]
	for _, f := range next {
		rendered = append(rendered, f.Path+":"+f.Type.String())
	}
	sort.Strings(rendered)
	const wantNext = `asset:REFERENCE<custom:asset>?|manager:REFERENCE<worker>|raise:RATE|tenure:DURATION`
	if got := strings.Join(rendered, "|"); got != wantNext {
		t.Fatalf("new-kind rendering = %q, want %q", got, wantNext)
	}
	if got := workflow.IRVersionForFields(next); got != 3 {
		t.Fatalf("new-kind version = %d, want 3", got)
	}
	// A LIST of references requires the new IR through its element.
	listed := []workflow.Field{{
		Path: "team",
		Type: workflow.ValueType{Kind: workflow.KindList, Element: &workflow.ValueType{Kind: workflow.KindReference, ReferenceTarget: "worker"}},
	}}
	if got := workflow.IRVersionForFields(listed); got != 3 {
		t.Fatalf("listed-reference version = %d, want 3", got)
	}
}
