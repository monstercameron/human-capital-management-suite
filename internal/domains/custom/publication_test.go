// TestTodo_WF_DATA_009 proves publishing a new custom-type version
// classifies the change, lists every dependent definition and running
// instance on the old version, and refuses a breaking publication until
// each dependent is re-pinned, re-published or explicitly left behind.
// Running instances keep their pinned version: the gate lists them but
// can never move them.
package custom

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var wfd9DecidedAt = time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

func wfd9Type() TypeID { return TypeID{Namespace: "tenant.fleet", Kind: "Vehicle"} }

func wfd9Breaking(t *testing.T) CustomObjectDefinition {
	t.Helper()
	to := custom007V2(t)
	fields := make(map[string]FieldDefinition, len(to.Fields))
	for k, v := range to.Fields {
		if k != "active" {
			fields[k] = v
		}
	}
	to.Fields = fields
	if err := to.Validate(); err != nil {
		t.Fatal(err)
	}
	return to
}

func wfd9Plan(t *testing.T, to CustomObjectDefinition, opts MigrationOptions) TypeMigration {
	t.Helper()
	plan, err := PlanMigration(testDefinition(), to, opts)
	if err != nil {
		t.Fatalf("PlanMigration: %v", err)
	}
	return plan
}

func wfd9Static() []Dependent {
	return []Dependent{
		{Kind: DependentWorkflow, ID: "w-1", PinnedVersion: 1, Digest: "sha256:plan1"},
		{Kind: DependentForm, ID: "f-1", PinnedVersion: 1, Digest: "sha256:form1"},
		{Kind: DependentRule, ID: "r-1", PinnedVersion: 1, Digest: "sha256:rule1"},
	}
}

func wfd9Live() []Dependent {
	return []Dependent{
		{Kind: DependentInstance, ID: "i-1", PinnedVersion: 1, Digest: "sha256:inst1"},
		{Kind: DependentInstance, ID: "i-2", PinnedVersion: 1, Digest: "sha256:inst2"},
	}
}

func wfd9Lives(live []Dependent) InstancePins {
	return func(context.Context, TypeID, uint64) ([]Dependent, error) { return live, nil }
}

func wfd9Registry(t *testing.T, deps []Dependent) *PinRegistry {
	t.Helper()
	registry := NewPinRegistry()
	for _, dep := range deps {
		if err := registry.RecordPin(wfd9Type(), dep); err != nil {
			t.Fatalf("RecordPin: %v", err)
		}
	}
	return registry
}

func wfd9Record(dep Dependent, disposition Disposition) PinRecord {
	return PinRecord{
		Dependent: dep, Disposition: disposition,
		DecidedBy: "release-board", EvidenceDigest: "evidence:disposition:" + dep.ID, DecidedAt: wfd9DecidedAt,
	}
}

func TestTodo_WF_DATA_009(t *testing.T) {
	ctx := context.Background()

	t.Run("compatible_lists_but_needs_nothing", func(t *testing.T) {
		plan := wfd9Plan(t, custom007V2(t), custom007Options())
		if plan.Breaking() {
			t.Fatalf("added-only plan reports breaking: %+v", plan.Impact)
		}
		decision, err := CheckPublication(ctx, plan, wfd9Registry(t, wfd9Static()), wfd9Lives(wfd9Live()), nil)
		if err != nil {
			t.Fatalf("compatible gate: %v", err)
		}
		if !decision.Allowed || decision.Breaking || len(decision.Missing) != 0 {
			t.Fatalf("decision = %+v, want allowed non-breaking with no missing", decision)
		}
		if len(decision.Dependents) != 5 {
			t.Fatalf("dependents = %d, want all 3 definitions and 2 instances", len(decision.Dependents))
		}
	})

	t.Run("breaking_refuses_until_dispositioned", func(t *testing.T) {
		plan := wfd9Plan(t, wfd9Breaking(t), custom007Options())
		if !plan.Breaking() {
			t.Fatal("removal plan must report breaking")
		}
		registry := wfd9Registry(t, wfd9Static())
		lives := wfd9Lives(wfd9Live())
		decision, err := CheckPublication(ctx, plan, registry, lives, nil)
		if !errors.Is(err, ErrMigrationBlocked) {
			t.Fatalf("undispositioned err = %v, want ErrMigrationBlocked", err)
		}
		if decision.Allowed || len(decision.Missing) != 5 {
			t.Fatalf("decision = %+v, want blocked with 5 missing", decision)
		}
		records := map[DependentKey]PinRecord{}
		static, live := wfd9Static(), wfd9Live()
		records[static[0].Key()] = wfd9Record(static[0], DispositionRepinned)
		records[static[1].Key()] = wfd9Record(static[1], DispositionRepublished)
		records[static[2].Key()] = wfd9Record(static[2], DispositionPinnedOld)
		for _, dep := range live {
			records[dep.Key()] = wfd9Record(dep, DispositionPinnedOld)
		}
		decision, err = CheckPublication(ctx, plan, registry, lives, records)
		if err != nil {
			t.Fatalf("dispositioned gate: %v", err)
		}
		if !decision.Allowed || len(decision.Missing) != 0 {
			t.Fatalf("decision = %+v, want allowed with no missing", decision)
		}
	})

	t.Run("instance_cannot_repin", func(t *testing.T) {
		plan := wfd9Plan(t, wfd9Breaking(t), custom007Options())
		live := wfd9Live()
		records := map[DependentKey]PinRecord{live[0].Key(): wfd9Record(live[0], DispositionRepinned)}
		if _, err := CheckPublication(ctx, plan, NewPinRegistry(), wfd9Lives(live[:1]), records); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("instance re-pin err = %v, want ErrInvalidPublication", err)
		}
	})

	t.Run("unattested_disposition_decides_nothing", func(t *testing.T) {
		plan := wfd9Plan(t, wfd9Breaking(t), custom007Options())
		static := wfd9Static()[:1]
		registry := wfd9Registry(t, static)
		bare := wfd9Record(static[0], DispositionRepinned)
		bare.EvidenceDigest = ""
		decision, err := CheckPublication(ctx, plan, registry, wfd9Lives(nil),
			map[DependentKey]PinRecord{static[0].Key(): bare})
		if !errors.Is(err, ErrMigrationBlocked) {
			t.Fatalf("unattested err = %v, want ErrMigrationBlocked", err)
		}
		if len(decision.Missing) != 1 {
			t.Fatalf("missing = %+v, want the unattested dependent", decision.Missing)
		}
	})

	t.Run("registry_refusals", func(t *testing.T) {
		registry := NewPinRegistry()
		inst := Dependent{Kind: DependentInstance, ID: "i-1", PinnedVersion: 1, Digest: "sha256:x"}
		if err := registry.RecordPin(wfd9Type(), inst); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("instance record err = %v, want ErrInvalidPublication", err)
		}
		dep := Dependent{Kind: DependentWorkflow, ID: "w-1", PinnedVersion: 1, Digest: "sha256:x"}
		if err := registry.RecordPin(wfd9Type(), dep); err != nil {
			t.Fatalf("RecordPin: %v", err)
		}
		if err := registry.RecordPin(wfd9Type(), dep); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("duplicate err = %v, want ErrInvalidPublication", err)
		}
		bad := dep
		bad.Digest = ""
		if err := registry.RecordPin(wfd9Type(), bad); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("blank digest err = %v, want ErrInvalidPublication", err)
		}
	})

	t.Run("gate_refusals", func(t *testing.T) {
		plan := wfd9Plan(t, wfd9Breaking(t), custom007Options())
		if _, err := CheckPublication(ctx, plan, nil, wfd9Lives(nil), nil); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("nil registry err = %v, want ErrInvalidPublication", err)
		}
		if _, err := CheckPublication(ctx, plan, NewPinRegistry(), nil, nil); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("nil provider err = %v, want ErrInvalidPublication", err)
		}
		raw := errors.New("instance store offline")
		broken := func(context.Context, TypeID, uint64) ([]Dependent, error) { return nil, raw }
		if _, err := CheckPublication(ctx, plan, NewPinRegistry(), broken, nil); !errors.Is(err, raw) {
			t.Fatalf("provider err = %v, want the raw error", err)
		}
		versioned := wfd9Registry(t, []Dependent{{Kind: DependentWorkflow, ID: "w-9", PinnedVersion: 7, Digest: "sha256:x"}})
		if got := versioned.DependentsOn(wfd9Type(), 1); len(got) != 0 {
			t.Fatalf("v1 dependents = %+v, want none (w-9 pins v7)", got)
		}
		if got := versioned.DependentsOn(wfd9Type(), 7); len(got) != 1 || got[0].ID != "w-9" {
			t.Fatalf("v7 dependents = %+v, want w-9", got)
		}
		lying := wfd9Lives([]Dependent{{Kind: DependentWorkflow, ID: "w-9", PinnedVersion: 7, Digest: "sha256:x"}})
		if _, err := CheckPublication(ctx, plan, NewPinRegistry(), lying, nil); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("mis-pinned err = %v, want ErrInvalidPublication", err)
		}
		dup := Dependent{Kind: DependentForm, ID: "f-9", PinnedVersion: 1, Digest: "sha256:x"}
		if _, err := CheckPublication(ctx, plan, wfd9Registry(t, []Dependent{dup}), wfd9Lives([]Dependent{dup}), nil); !errors.Is(err, ErrInvalidPublication) {
			t.Fatalf("duplicate listing err = %v, want ErrInvalidPublication", err)
		}
	})

	t.Run("classification", func(t *testing.T) {
		if plan := wfd9Plan(t, custom007V2(t), custom007Options()); plan.Breaking() {
			t.Fatal("added-only must classify compatible")
		}
		if plan := wfd9Plan(t, wfd9Breaking(t), custom007Options()); !plan.Breaking() {
			t.Fatal("removal must classify breaking")
		}
		retyped := custom007V2(t)
		fields := mapsClone(retyped.Fields)
		active := fields["active"]
		active.Type = "string"
		fields["active"] = active
		retyped.Fields = fields
		opts := custom007Options()
		opts.TypeMappings = map[string]string{"active": "bool->string"}
		if plan := wfd9Plan(t, retyped, opts); !plan.Breaking() {
			t.Fatal("retype must classify breaking")
		}
		renamed := testDefinition()
		renamed.Version = 2
		fields = mapsClone(renamed.Fields)
		fields["act"] = fields["active"]
		delete(fields, "active")
		renamed.Fields = fields
		opts = custom007Options()
		opts.Renames = map[string]string{"active": "act"}
		if plan := wfd9Plan(t, renamed, opts); !plan.Breaking() {
			t.Fatal("rename must classify breaking")
		}
	})
}

func mapsClone(in map[string]FieldDefinition) map[string]FieldDefinition {
	out := make(map[string]FieldDefinition, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestTodo_WF_DATA_009_Golden(t *testing.T) {
	plan := wfd9Plan(t, wfd9Breaking(t), custom007Options())
	static := []Dependent{
		{Kind: DependentWorkflow, ID: "w-1", PinnedVersion: 1, Digest: "sha256:plan1"},
		{Kind: DependentForm, ID: "f-1", PinnedVersion: 1, Digest: "sha256:form1"},
	}
	live := []Dependent{{Kind: DependentInstance, ID: "i-9", PinnedVersion: 1, Digest: "sha256:inst9"}}
	records := map[DependentKey]PinRecord{static[0].Key(): wfd9Record(static[0], DispositionRepinned)}
	decision, err := CheckPublication(context.Background(), plan, wfd9Registry(t, static), wfd9Lives(live), records)
	if !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("gate err = %v, want ErrMigrationBlocked", err)
	}
	got, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":{"namespace":"tenant.fleet","kind":"Vehicle"},"from_version":1,"to_version":2,"breaking":true,"dependents":[{"kind":"form","id":"f-1","version":1,"digest":"sha256:form1"},{"kind":"instance","id":"i-9","version":1,"digest":"sha256:inst9"},{"kind":"workflow","id":"w-1","version":1,"digest":"sha256:plan1"}],"missing":[{"kind":"form","id":"f-1","version":1,"digest":"sha256:form1"},{"kind":"instance","id":"i-9","version":1,"digest":"sha256:inst9"}],"allowed":false}`
	if string(got) != want {
		t.Fatalf("decision golden changed:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_WF_DATA_009_Property(t *testing.T) {
	types := map[string]string{"registration": "string", "active": "bool", "nickname": "string", "color": "string", "owner": "string"}

	cloneWith := func(t *testing.T, names []string, version uint64) CustomObjectDefinition {
		t.Helper()
		def := testDefinition()
		def.Version = version
		fields := make(map[string]FieldDefinition, len(names))
		for _, name := range names {
			fields[name] = FieldDefinition{Type: types[name], Classification: FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}}
		}
		def.Fields = fields
		if err := def.Validate(); err != nil {
			t.Fatal(err)
		}
		return def
	}

	t.Run("compatible_never_breaks_a_plan_path", func(t *testing.T) {
		names := []string{"registration", "active", "nickname", "color", "owner"}
		cases := 0
		for mask := 1; mask < (1 << len(names)); mask++ {
			var from []string
			var rest []string
			for i, name := range names {
				if mask&(1<<i) != 0 {
					from = append(from, name)
				} else {
					rest = append(rest, name)
				}
			}
			for _, add := range rest {
				to := append(append([]string(nil), from...), add)
				plan, err := PlanMigration(cloneWith(t, from, 1), cloneWith(t, to, 2), custom007Options())
				if err != nil {
					t.Fatalf("v1 %v + %s: %v", from, add, err)
				}
				if plan.Breaking() {
					t.Fatalf("added-only %v -> %v reports breaking", from, to)
				}
				have := map[string]bool{}
				for _, f := range plan.ToFields {
					have[f] = true
				}
				for _, f := range plan.FromFields {
					if !have[f] {
						t.Fatalf("compatible %v -> %v drops path %s", from, to, f)
					}
				}
				decision, err := CheckPublication(context.Background(), plan, NewPinRegistry(), wfd9Lives(nil), nil)
				if err != nil || !decision.Allowed {
					t.Fatalf("compatible %v -> %v gate: allowed=%v err=%v", from, to, decision.Allowed, err)
				}
				cases++
			}
		}
		if cases == 0 {
			t.Fatal("no compatible cases ran")
		}
		t.Logf("compatible cases: %d", cases)
	})

	t.Run("breaking_always_blocks_undispositioned", func(t *testing.T) {
		mutations := map[string]func(CustomObjectDefinition) CustomObjectDefinition{
			"remove": func(def CustomObjectDefinition) CustomObjectDefinition {
				fields := mapsClone(def.Fields)
				delete(fields, "active")
				def.Fields = fields
				return def
			},
			"retype": func(def CustomObjectDefinition) CustomObjectDefinition {
				fields := mapsClone(def.Fields)
				active := fields["active"]
				active.Type = "string"
				fields["active"] = active
				def.Fields = fields
				return def
			},
			"rename": func(def CustomObjectDefinition) CustomObjectDefinition {
				fields := mapsClone(def.Fields)
				fields["handle"] = fields["nickname"]
				delete(fields, "nickname")
				def.Fields = fields
				return def
			},
		}
		for name, mutate := range mutations {
			from := cloneWith(t, []string{"registration", "active", "nickname"}, 1)
			to := mutate(cloneWith(t, []string{"registration", "active", "nickname"}, 2))
			opts := custom007Options()
			switch name {
			case "retype":
				opts.TypeMappings = map[string]string{"active": "bool->string"}
			case "rename":
				opts.Renames = map[string]string{"nickname": "handle"}
			}
			plan, err := PlanMigration(from, to, opts)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !plan.Breaking() {
				t.Fatalf("%s must classify breaking", name)
			}
			decision, err := CheckPublication(context.Background(), plan, NewPinRegistry(), wfd9Lives(nil), nil)
			if err != nil || !decision.Allowed {
				t.Fatalf("%s with no dependents: allowed=%v err=%v, want vacuous allow", name, decision.Allowed, err)
			}
			dep := Dependent{Kind: DependentWorkflow, ID: "w-" + name, PinnedVersion: 1, Digest: "sha256:x"}
			decision, err = CheckPublication(context.Background(), plan, wfd9Registry(t, []Dependent{dep}), wfd9Lives(nil), nil)
			if !errors.Is(err, ErrMigrationBlocked) || len(decision.Missing) != 1 {
				t.Fatalf("%s with a dependent: missing=%+v err=%v, want blocked", name, decision.Missing, err)
			}
		}
	})

	t.Run("decision_determinism", func(t *testing.T) {
		plan := wfd9Plan(t, wfd9Breaking(t), custom007Options())
		static, live := wfd9Static(), wfd9Live()
		all := append(append([]Dependent(nil), static...), live...)
		var want string
		for i := 0; i < 25; i++ {
			registry := NewPinRegistry()
			for j := len(static) - 1; j >= 0; j-- {
				if err := registry.RecordPin(wfd9Type(), static[(j+i)%len(static)]); err != nil {
					t.Fatal(err)
				}
			}
			records := map[DependentKey]PinRecord{}
			subset := all[:3]
			for j := range subset {
				dep := subset[(j+i)%len(subset)]
				disposition := DispositionPinnedOld
				if dep.Kind != DependentInstance {
					disposition = DispositionRepinned
				}
				records[dep.Key()] = wfd9Record(dep, disposition)
			}
			decision, _ := CheckPublication(context.Background(), plan, registry, wfd9Lives(live), records)
			got, err := json.Marshal(decision)
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				want = string(got)
			} else if string(got) != want {
				t.Fatalf("decision run %d differs:\n%s\n%s", i, got, want)
			}
		}
	})
}
