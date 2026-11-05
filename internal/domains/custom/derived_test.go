// TestTodo_WF_DATA_021 proves derived fields compute from bounded
// expressions: declared once on the type, validated at publication,
// computed and stored on write, and maintained over linked records by
// the propagation engine. Recomputation from the ledger yields the
// stored values, and cost limits hold for any expression.
package custom

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var wfd21Now = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

func wfd21Classification(label string) FieldClassification {
	return FieldClassification{
		AuthZDomain: "tenant.sales", Classification: label,
		ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL",
	}
}

func wfd21CustomerDef() CustomObjectDefinition {
	return CustomObjectDefinition{
		Kind: "Customer", Namespace: "acme.sales", Version: 1,
		Fields: map[string]FieldDefinition{
			"customer_id": {Type: "string", Classification: wfd21Classification("INTERNAL")},
			"name":        {Type: "string", Classification: wfd21Classification("PUBLIC")},
			"order_count": {
				Type: "integer", Classification: wfd21Classification("INTERNAL"),
				Derived: &DerivedSpec{
					Expression: "len(order_amounts)",
					Links: []DerivedLink{{
						Input: "order_amounts", Relationship: "CustomerOrders",
						Field: "customer_id", Value: "amount", MaxTargets: 100,
					}},
				},
			},
			"vip": {
				Type: "bool", Classification: wfd21Classification("INTERNAL"),
				Derived: &DerivedSpec{Expression: "order_count >= 2"},
			},
		},
	}
}

func wfd21OrderDef() CustomObjectDefinition {
	return CustomObjectDefinition{
		Kind: "Order", Namespace: "acme.sales", Version: 1,
		Fields: map[string]FieldDefinition{
			"order_id":    {Type: "string", Classification: wfd21Classification("INTERNAL")},
			"customer_id": {Type: "string", Classification: wfd21Classification("INTERNAL")},
			"amount":      {Type: "integer", Classification: wfd21Classification("INTERNAL")},
			"big": {
				Type: "bool", Classification: wfd21Classification("INTERNAL"),
				Derived: &DerivedSpec{Expression: "amount > 10000"},
			},
		},
	}
}

func wfd21Relationship() CustomRelationshipDefinition {
	return CustomRelationshipDefinition{
		Name: "CustomerOrders", Namespace: "acme.sales", Version: 1,
		SourceKind: "Order", TargetKind: "Customer",
		Cardinality: CardinalityManyToMany, EffectiveDateRule: "INSTANT_INTERVAL",
	}
}

func wfd21Derived(t *testing.T, store *EventStore) *DerivedRecords {
	t.Helper()
	derived, err := NewDerivedRecords(DerivedDeps{
		Port:          store,
		Definitions:   []CustomObjectDefinition{wfd21CustomerDef(), wfd21OrderDef()},
		Relationships: []CustomRelationshipDefinition{wfd21Relationship()},
		Clock:         func() time.Time { return wfd21Now },
	})
	if err != nil {
		t.Fatalf("NewDerivedRecords: %v", err)
	}
	return derived
}

func wfd21Grants(t *testing.T, def CustomObjectDefinition) map[string]bool {
	t.Helper()
	policy, err := ResolvePolicy(def)
	if err != nil {
		t.Fatalf("ResolvePolicy: %v", err)
	}
	out := map[string]bool{}
	for _, field := range policy.Fields {
		out[field.AuthZDomain] = true
	}
	return out
}

func wfd21Values(pairs ...any) map[string]TypedValue {
	out := map[string]TypedValue{}
	for i := 0; i+2 < len(pairs)+1; i += 3 {
		name, _ := pairs[i].(string)
		typ, _ := pairs[i+1].(string)
		out[name] = TypedValue{FieldName: name, Type: typ, Value: pairs[i+2]}
	}
	return out
}

func wfd21Commit(t *testing.T, port EventProjectionPort, def CustomObjectDefinition, objectID string, fields map[string]TypedValue, op Operation, head int64, rev uint64, tag string) CommitReceipt {
	t.Helper()
	effective, err := values.NewOpenInstantInterval(values.NewInstant(wfd21Now))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	receipt, err := port.Commit(context.Background(), MutationRequest{
		Tenant: "acme", ObjectID: objectID, Definition: def,
		Record: CustomRecordRevision{
			ObjectID: objectID, ObjectKind: def.Kind, Namespace: def.Namespace,
			DefinitionVersion: def.Version, Effective: effective, FieldValues: fields,
		},
		Operation: op, ExpectedHead: head, ExpectedRevision: rev,
		Actor: "alice", Purpose: "sales.ops", EvidenceDigest: "evidence-" + tag,
		GrantedDomains: wfd21Grants(t, def), OccurredAt: wfd21Now, EffectiveAt: wfd21Now,
	})
	if err != nil {
		t.Fatalf("commit %s: %v", tag, err)
	}
	return receipt
}

func wfd21Read(t *testing.T, port EventProjectionPort, def CustomObjectDefinition, objectID string) SearchRecord {
	t.Helper()
	report, err := port.Search(context.Background(), SearchRequest{
		Tenant: "acme", Definition: def, GrantedDomains: wfd21Grants(t, def), Purpose: "sales.ops",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, rec := range report.Records {
		if rec.ObjectID == objectID {
			return rec
		}
	}
	t.Fatalf("object %s not found", objectID)
	return SearchRecord{}
}

func TestTodo_WF_DATA_021(t *testing.T) {
	t.Run("publish_validates_declarations", func(t *testing.T) {
		customer, order := wfd21CustomerDef(), wfd21OrderDef()
		if err := customer.Validate(); err != nil {
			t.Fatalf("customer: %v", err)
		}
		if err := order.Validate(); err != nil {
			t.Fatalf("order: %v", err)
		}
		rel := wfd21Relationship()
		objects := map[TypeID]CustomObjectDefinition{
			{Namespace: "acme.sales", Kind: "Customer"}: customer,
			{Namespace: "acme.sales", Kind: "Order"}:    order,
		}
		rels := map[CustomRelationshipKey]CustomRelationshipDefinition{
			{Namespace: "acme.sales", Name: "CustomerOrders"}: rel,
		}
		if err := ValidateDerived(customer, objects, rels); err != nil {
			t.Fatalf("customer derived: %v", err)
		}
		if err := ValidateDerived(order, objects, rels); err != nil {
			t.Fatalf("order derived: %v", err)
		}
		plain := wfd21CustomerDef()
		derived := plain.Fields["vip"]
		derived.Derived = nil
		plain.Fields["vip"] = derived
		if plain.Digest() == customer.Digest() {
			t.Fatal("declaration must pin the digest")
		}
	})

	t.Run("declaration_refusals", func(t *testing.T) {
		cases := map[string]func(*CustomObjectDefinition){
			"syntax": func(def *CustomObjectDefinition) {
				broken := def.Fields["vip"]
				broken.Derived = &DerivedSpec{Expression: "order_count >="}
				def.Fields["vip"] = broken
			},
			"unknown variable": func(def *CustomObjectDefinition) {
				broken := def.Fields["vip"]
				broken.Derived = &DerivedSpec{Expression: "missing > 1"}
				def.Fields["vip"] = broken
			},
			"self reference": func(def *CustomObjectDefinition) {
				broken := def.Fields["vip"]
				broken.Derived = &DerivedSpec{Expression: "vip == true"}
				def.Fields["vip"] = broken
			},
			"governed and derived": func(def *CustomObjectDefinition) {
				broken := def.Fields["vip"]
				broken.Kind = FieldKindSequence
				broken.Sequence = &SequenceConfig{Format: "V-{seq:4}"}
				def.Fields["vip"] = broken
			},
			"missing bound": func(def *CustomObjectDefinition) {
				broken := def.Fields["order_count"]
				links := append([]DerivedLink(nil), broken.Derived.Links...)
				links[0].MaxTargets = 0
				broken.Derived = &DerivedSpec{Expression: broken.Derived.Expression, Links: links}
				def.Fields["order_count"] = broken
			},
		}
		for name, mutate := range cases {
			def := wfd21CustomerDef()
			mutate(&def)
			if err := def.Validate(); err == nil {
				t.Fatalf("%s: invalid declaration validates", name)
			} else if !errors.Is(err, ErrInvalidDerived) && !errors.Is(err, ErrInvalidFieldKind) && !errors.Is(err, ErrInvalidObjectDefinition) {
				t.Fatalf("%s: err = %v, want a declaration refusal", name, err)
			}
		}
		pubCases := map[string]func(*CustomObjectDefinition){
			"unknown relationship": func(def *CustomObjectDefinition) {
				broken := def.Fields["order_count"]
				links := append([]DerivedLink(nil), broken.Derived.Links...)
				links[0].Relationship = "Nobody"
				broken.Derived = &DerivedSpec{Expression: broken.Derived.Expression, Links: links}
				def.Fields["order_count"] = broken
			},
			"under classified": func(def *CustomObjectDefinition) {
				broken := def.Fields["vip"]
				broken.Classification = wfd21Classification("PUBLIC")
				def.Fields["vip"] = broken
			},
		}
		pubObjects := map[TypeID]CustomObjectDefinition{
			{Namespace: "acme.sales", Kind: "Customer"}: wfd21CustomerDef(),
			{Namespace: "acme.sales", Kind: "Order"}:    wfd21OrderDef(),
		}
		pubRels := map[CustomRelationshipKey]CustomRelationshipDefinition{
			{Namespace: "acme.sales", Name: "CustomerOrders"}: wfd21Relationship(),
		}
		for name, mutate := range pubCases {
			def := wfd21CustomerDef()
			mutate(&def)
			if err := ValidateDerived(def, pubObjects, pubRels); !errors.Is(err, ErrInvalidDerived) {
				t.Fatalf("%s: err = %v, want ErrInvalidDerived", name, err)
			}
		}
		cycle := wfd21CustomerDef()
		cycle.Fields["flag_a"] = FieldDefinition{
			Type: "bool", Classification: wfd21Classification("INTERNAL"),
			Derived: &DerivedSpec{Expression: "flag_b == true"},
		}
		cycle.Fields["flag_b"] = FieldDefinition{
			Type: "bool", Classification: wfd21Classification("INTERNAL"),
			Derived: &DerivedSpec{Expression: "flag_a == true"},
		}
		if err := cycle.Validate(); !errors.Is(err, ErrInvalidDerived) {
			t.Fatalf("cycle err = %v, want ErrInvalidDerived", err)
		}
	})

	t.Run("write_computes_and_propagates", func(t *testing.T) {
		store := NewEventStore()
		derived := wfd21Derived(t, store)
		customer, order := wfd21CustomerDef(), wfd21OrderDef()
		wfd21Commit(t, derived, customer, "c-1", wfd21Values(
			"customer_id", "string", "c-1",
			"name", "string", "Acme",
		), OperationCreate, 0, 0, "c1")
		got := wfd21Read(t, derived, customer, "c-1")
		if got.Fields["order_count"].Value != int64(0) || got.Fields["vip"].Value != false {
			t.Fatalf("fresh customer = %+v, want count 0 vip false", got.Fields)
		}
		wfd21Commit(t, derived, order, "o-1", wfd21Values(
			"order_id", "string", "o-1",
			"customer_id", "string", "c-1",
			"amount", "integer", int64(5000),
		), OperationCreate, 0, 0, "o1")
		got = wfd21Read(t, derived, order, "o-1")
		if got.Fields["big"].Value != false {
			t.Fatalf("o-1 big = %v, want false", got.Fields["big"].Value)
		}
		wfd21Commit(t, derived, order, "o-2", wfd21Values(
			"order_id", "string", "o-2",
			"customer_id", "string", "c-1",
			"amount", "integer", int64(50000),
		), OperationCreate, 0, 0, "o2")
		got = wfd21Read(t, derived, order, "o-2")
		if got.Fields["big"].Value != true {
			t.Fatalf("o-2 big = %v, want true", got.Fields["big"].Value)
		}
		got = wfd21Read(t, derived, customer, "c-1")
		if got.Fields["order_count"].Value != int64(2) || got.Fields["vip"].Value != true {
			t.Fatalf("customer after orders = %+v, want count 2 vip true", got.Fields)
		}
		if got.Revision != 3 {
			t.Fatalf("customer revision = %d, want 3 (create plus two maintenances)", got.Revision)
		}
	})

	t.Run("caller_mismatch_is_refused", func(t *testing.T) {
		store := NewEventStore()
		derived := wfd21Derived(t, store)
		order := wfd21OrderDef()
		effective, _ := values.NewOpenInstantInterval(values.NewInstant(wfd21Now))
		_, err := derived.Commit(context.Background(), MutationRequest{
			Tenant: "acme", ObjectID: "o-9", Definition: order,
			Record: CustomRecordRevision{
				ObjectID: "o-9", ObjectKind: order.Kind, Namespace: order.Namespace,
				DefinitionVersion: 1, Effective: effective,
				FieldValues: wfd21Values(
					"order_id", "string", "o-9",
					"customer_id", "string", "c-9",
					"amount", "integer", int64(5),
					"big", "bool", true,
				),
			},
			Operation: OperationCreate, Actor: "mallory", Purpose: "sales.ops",
			EvidenceDigest: "evidence-x", GrantedDomains: wfd21Grants(t, order),
			OccurredAt: wfd21Now, EffectiveAt: wfd21Now,
		})
		if !errors.Is(err, ErrDerivedMismatch) {
			t.Fatalf("mismatch err = %v, want ErrDerivedMismatch", err)
		}
		wfd21Commit(t, derived, order, "o-9", wfd21Values(
			"order_id", "string", "o-9",
			"customer_id", "string", "c-9",
			"amount", "integer", int64(5),
			"big", "bool", false,
		), OperationCreate, 0, 0, "o9")
	})

	t.Run("unchanged_maintenance_commits_nothing", func(t *testing.T) {
		store := NewEventStore()
		derived := wfd21Derived(t, store)
		customer, order := wfd21CustomerDef(), wfd21OrderDef()
		wfd21Commit(t, derived, customer, "c-1", wfd21Values(
			"customer_id", "string", "c-1", "name", "string", "Acme",
		), OperationCreate, 0, 0, "c1")
		wfd21Commit(t, derived, order, "o-1", wfd21Values(
			"order_id", "string", "o-1", "customer_id", "string", "c-1", "amount", "integer", int64(5),
		), OperationCreate, 0, 0, "o1")
		before := wfd21Read(t, derived, customer, "c-1")
		wfd21Commit(t, derived, customer, "c-1", wfd21Values(
			"customer_id", "string", "c-1", "name", "string", "Acme Inc",
		), OperationChange, int64(before.Revision), before.Revision, "rename")
		after := wfd21Read(t, derived, customer, "c-1")
		if after.Revision != before.Revision+1 {
			t.Fatalf("revision %d -> %d, want exactly one change event", before.Revision, after.Revision)
		}
		if len(store.Events("acme", "Customer", "c-1")) != int(after.Revision) {
			t.Fatal("maintenance must not append when nothing derived changed")
		}
	})

	t.Run("unindexed_types_pass_through", func(t *testing.T) {
		store := NewEventStore()
		derived := wfd21Derived(t, store)
		vehicle := testDefinition()
		wfd21Commit(t, derived, vehicle, "v-1", wfd21Values(
			"registration", "string", "R-1",
			"active", "bool", true,
		), OperationCreate, 0, 0, "v1")
		got := wfd21Read(t, derived, vehicle, "v-1")
		if len(got.Fields) != 2 {
			t.Fatalf("vehicle fields = %+v, want exactly the stored two", got.Fields)
		}
	})
}

func TestTodo_WF_DATA_021_Property(t *testing.T) {
	t.Run("ledger_recompute_yields_stored", func(t *testing.T) {
		ctx := context.Background()
		store := NewEventStore()
		derived := wfd21Derived(t, store)
		customer, order := wfd21CustomerDef(), wfd21OrderDef()
		wfd21Commit(t, derived, customer, "c-1", wfd21Values(
			"customer_id", "string", "c-1", "name", "string", "Acme",
		), OperationCreate, 0, 0, "c1")
		wfd21Commit(t, derived, customer, "c-2", wfd21Values(
			"customer_id", "string", "c-2", "name", "string", "Globex",
		), OperationCreate, 0, 0, "c2")
		wfd21Commit(t, derived, order, "o-1", wfd21Values(
			"order_id", "string", "o-1", "customer_id", "string", "c-1", "amount", "integer", int64(10),
		), OperationCreate, 0, 0, "o1")
		wfd21Commit(t, derived, order, "o-2", wfd21Values(
			"order_id", "string", "o-2", "customer_id", "string", "c-1", "amount", "integer", int64(20),
		), OperationCreate, 0, 0, "o2")
		wfd21Commit(t, derived, order, "o-3", wfd21Values(
			"order_id", "string", "o-3", "customer_id", "string", "c-2", "amount", "integer", int64(30),
		), OperationCreate, 0, 0, "o3")
		moved := wfd21Read(t, derived, order, "o-1")
		wfd21Commit(t, derived, order, "o-1", wfd21Values(
			"order_id", "string", "o-1", "customer_id", "string", "c-2", "amount", "integer", int64(10),
		), OperationChange, int64(moved.Revision), moved.Revision, "move")
		// The ledger is quiescent: synchronous maintenance settled every
		// commit. Fold every stream and recompute every derived field
		// from ledger state; each must equal its stored value.
		checked := 0
		for _, def := range []CustomObjectDefinition{customer, order} {
			report, err := derived.Search(ctx, SearchRequest{
				Tenant: "acme", Definition: def, GrantedDomains: wfd21Grants(t, def), Purpose: "sales.ops",
			})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			compiled := derived.compiled(def)
			if compiled == nil {
				t.Fatalf("def %s is not indexed", def.Kind)
			}
			for _, rec := range report.Records {
				events := store.Events("acme", def.Kind, rec.ObjectID)
				if len(events) == 0 {
					t.Fatalf("object %s has no events", rec.ObjectID)
				}
				folded := events[len(events)-1].Record.FieldValues
				recomputed, err := derived.compute(ctx, "acme", compiled, folded, rec.ObjectID)
				if err != nil {
					t.Fatalf("recompute %s: %v", rec.ObjectID, err)
				}
				for name, value := range recomputed {
					stored, ok := folded[name]
					if !ok || !nativeEqual(stored.Value, value.Value) {
						t.Fatalf("recomputed %s.%s = %v, stored %v", rec.ObjectID, name, value.Value, stored.Value)
					}
					checked++
				}
			}
		}
		if checked == 0 {
			t.Fatal("no derived values recomputed")
		}
		t.Logf("recomputed %d derived values from the ledger", checked)
	})

	t.Run("join_matches_the_flow_lattice", func(t *testing.T) {
		labels := []string{"PUBLIC", "INTERNAL", "CONFIDENTIAL", "RESTRICTED"}
		for _, a := range labels {
			for _, b := range labels {
				want, wantOK := joinLabel(a, b)
				da, erra := workflow.ParseDataClassification(a)
				db, errb := workflow.ParseDataClassification(b)
				if erra != nil || errb != nil {
					t.Fatalf("lattice rejects %s/%s: %v / %v", a, b, erra, errb)
				}
				got := a
				if db.Dominates(da) && !da.Dominates(db) {
					got = b
				}
				if !wantOK || want != got {
					t.Fatalf("join(%s,%s) = %s, lattice says %s", a, b, want, got)
				}
			}
		}
	})

	t.Run("compiled_cost_stays_within_limits", func(t *testing.T) {
		defs := []CustomObjectDefinition{wfd21CustomerDef(), wfd21OrderDef()}
		objects := map[TypeID]CustomObjectDefinition{}
		for _, def := range defs {
			objects[TypeID{Namespace: def.Namespace, Kind: def.Kind}] = def
		}
		rels := map[CustomRelationshipKey]CustomRelationshipDefinition{
			{Namespace: "acme.sales", Name: "CustomerOrders"}: wfd21Relationship(),
		}
		for _, def := range defs {
			for name, fieldDef := range def.Fields {
				if fieldDef.Derived == nil {
					continue
				}
				elements, err := linkElementTypes(def, fieldDef.Derived, objects, rels)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				compiled, _, err := compileDerived(name, def.Fields, fieldDef.Derived, elements)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				cost, limit := compiled.Cost, compiled.Limits
				if cost.Nodes > limit.MaxNodes || cost.Depth > limit.MaxDepth ||
					cost.Iterations > limit.MaxIterations || cost.Total > limit.MaxCost {
					t.Fatalf("%s cost %+v exceeds %+v", name, cost, limit)
				}
			}
		}
	})
}

// TestTodo_WF_DATA_021_Fuzz is the deterministic leg of the cost-limit
// proof: seeded expressions either refuse or compile within limits and
// evaluate without hanging or panicking.
func TestTodo_WF_DATA_021_Fuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(20260401))
	alphabet := []rune("abcxyz_0123456789+-*/%<>=!&|()[]{}\"' .,Àé")
	for i := 0; i < 300; i++ {
		var b strings.Builder
		for n, l := 0, 1+rng.Intn(48); n < l; n++ {
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		wfd21CheckCost(t, b.String())
	}
}

// FuzzDerivedExpressionCost is the go fuzz entry over the same proof.
func FuzzDerivedExpressionCost(f *testing.F) {
	for _, seed := range []string{`true`, `a > 1`, `amount > 10000`, `len(order_amounts)`, `order_count >= 2`, ``, `((a`, `a == "x"`, `!(a)`, `a+b*c`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) { wfd21CheckCost(t, source) })
}

func wfd21CheckCost(t *testing.T, source string) {
	t.Helper()
	expr, err := rules.Parse(source)
	if err != nil {
		return
	}
	expr.UnknownSemantics = rules.UnknownSemanticsReject
	compiled, err := rules.CompileExpression(expr, rules.DefaultCostLimit)
	if err != nil {
		return
	}
	cost, limit := compiled.Cost, compiled.Limits
	if cost.Nodes > limit.MaxNodes || cost.Depth > limit.MaxDepth ||
		cost.Iterations > limit.MaxIterations || cost.Total > limit.MaxCost {
		t.Fatalf("expression %q compiles over budget: %+v vs %+v", source, cost, limit)
	}
	if _, err := compiled.Evaluate(nil); err != nil {
		return
	}
}
