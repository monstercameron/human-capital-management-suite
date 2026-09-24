package rulepayload

import (
	"bytes"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WF_EXT_006(t *testing.T) {
	table := rules.PromotionApprovalThresholdTable()
	store := New()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	published, err := store.Publish(Payload{Ref: ref, Kind: KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.Resolve(ref, published.Digest)
	if err != nil || resolved.Table == nil {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	if got, ok := store.ResolveReference(ref); !ok || got != published {
		t.Fatalf("reference = %+v, %v; want %+v", got, ok, published)
	}
	encoded, err := Marshal(Payload{Ref: ref, Kind: KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Unmarshal(encoded)
	if err != nil || decoded.Digest != published.Digest {
		t.Fatalf("table payload roundtrip = %q, %v", decoded.Digest, err)
	}
	if _, err := store.Resolve(ref, "sha256:wrong"); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("wrong digest error = %v", err)
	}
	if _, err := store.Resolve(workflow.Reference{Kind: ref.Kind, ID: ref.ID, Version: "other"}, published.Digest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong version error = %v", err)
	}
	aliasTable := rules.PromotionApprovalThresholdTable()
	aliasRef := workflow.Reference{Kind: workflow.RefRule, ID: "rules.alias.threshold", Version: "current"}
	if _, err := store.Publish(Payload{Ref: aliasRef, Kind: KindDecisionTable, Table: &aliasTable}); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("unmarked table alias error = %v", err)
	}
	aliasPin, err := store.Publish(Payload{Ref: aliasRef, BodyRef: &workflow.VersionedRef{ID: aliasTable.ID, Version: aliasTable.Version}, Kind: KindDecisionTable, Table: &aliasTable})
	if err != nil || aliasPin.Digest == "" {
		t.Fatalf("explicit table alias publication = %+v, %v", aliasPin, err)
	}

	// A caller cannot mutate either the original publication input or a
	// resolved copy and thereby change the bytes behind the reference.
	table.Rows[0].ID = "caller-mutated"
	resolved.Table.Rows[0].ID = "reader-mutated"
	again, err := store.Resolve(ref, published.Digest)
	if err != nil || again.Table.Rows[0].ID == "caller-mutated" || again.Table.Rows[0].ID == "reader-mutated" {
		t.Fatalf("published row changed after mutation: %+v, %v", again.Table.Rows[0], err)
	}

	changed := rules.PromotionApprovalThresholdTable()
	changed.Rows[0].ID = "different-body"
	if _, err := store.Publish(Payload{Ref: ref, Kind: KindDecisionTable, Table: &changed}); !errors.Is(err, ErrImmutable) {
		t.Fatalf("republish changed body error = %v", err)
	}

	expression, err := rules.CompileExpression(rules.Expression{
		Root: rules.BoolLiteral(true), Version: "1", UnknownSemantics: rules.UnknownSemanticsPropagate,
	})
	if err != nil {
		t.Fatal(err)
	}
	expressionRef := workflow.Reference{Kind: workflow.RefRule, ID: "rules.fixture.always", Version: "1"}
	expressionRefResult, err := store.Publish(Payload{Ref: expressionRef, Kind: KindExpression, Expression: &expression})
	if err != nil {
		t.Fatal(err)
	}
	loadedExpression, err := store.Resolve(expressionRef, expressionRefResult.Digest)
	if err != nil || loadedExpression.Expression == nil {
		t.Fatalf("resolve expression = %+v, %v", loadedExpression, err)
	}
	expressionBytes, err := Marshal(Payload{Ref: expressionRef, Kind: KindExpression, Expression: &expression})
	if err != nil {
		t.Fatal(err)
	}
	decodedExpression, err := Unmarshal(expressionBytes)
	if err != nil || decodedExpression.Digest != expressionRefResult.Digest {
		t.Fatalf("expression payload roundtrip = %q, %v", decodedExpression.Digest, err)
	}
	value, err := loadedExpression.Expression.Evaluate(nil)
	if err != nil || value.State != rules.EvalStatePresent || value.Value.Kind() != rules.KindBool {
		t.Fatalf("expression evaluation = %+v, %v", value, err)
	}

	program := ir.Program{
		IRVersion: ir.IRVersion, DefinitionName: "fixture.copy", DefinitionDigest: "sha256:definition",
		Instructions: []ir.Instruction{{Op: ir.OpProject,
			Sources:     []transformation.Path{{Schema: "source", Field: "name", Type: transformation.TypeString}},
			Destination: transformation.Path{Schema: "target", Field: "name", Type: transformation.TypeString}}},
		Dependencies: []string{"source.name"}, Limits: ir.Limits{MaxSteps: 1, MaxFanOut: 1},
	}
	transformRef := workflow.Reference{Kind: workflow.RefTransform, ID: "transforms.fixture.copy", Version: "1"}
	transformResult, err := store.Publish(Payload{Ref: transformRef, Kind: KindTransform, Transform: &program})
	if err != nil {
		t.Fatal(err)
	}
	loadedTransform, err := store.Resolve(transformRef, transformResult.Digest)
	if err != nil || loadedTransform.Transform == nil {
		t.Fatalf("resolve transform = %+v, %v", loadedTransform, err)
	}
	transformBytes, err := Marshal(Payload{Ref: transformRef, Kind: KindTransform, Transform: &program})
	if err != nil {
		t.Fatal(err)
	}
	decodedTransform, err := Unmarshal(transformBytes)
	if err != nil || decodedTransform.Digest != transformResult.Digest {
		t.Fatalf("transform payload roundtrip = %q, %v", decodedTransform.Digest, err)
	}
}

func TestTodo_WF_EXT_006_Property(t *testing.T) {
	table := rules.PromotionApprovalThresholdTable()
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	first, second := New(), New()
	a, err := first.Publish(Payload{Ref: ref, Kind: KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Publish(Payload{Ref: ref, Kind: KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("same pinned input has digests %q and %q", a.Digest, b.Digest)
	}

	inputs := map[string]rules.Value{
		rules.ColumnIncreasePercent: rules.DecimalValue(mustDecimal(t, "4.0000")),
		rules.ColumnBandPosition:    rules.StringValue(string(rules.BandPositionInBand)),
		rules.ColumnBudgetAuthority: rules.StringValue(string(rules.BudgetAuthoritySufficient)),
		rules.ColumnGradeChange:     rules.BoolValue(false),
	}
	left, err := rules.Evaluate(*tableCopy(t, first, ref, a.Digest), inputs)
	if err != nil {
		t.Fatal(err)
	}
	right, err := rules.Evaluate(*tableCopy(t, second, ref, b.Digest), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if left.TableDigest != right.TableDigest || left.Status != right.Status || len(left.Matches) != len(right.Matches) {
		t.Fatalf("repeated evaluation differs: left=%+v right=%+v", left, right)
	}
	for i := range left.Matches {
		if left.Matches[i].RowID != right.Matches[i].RowID || len(left.Matches[i].Outputs) != len(right.Matches[i].Outputs) {
			t.Fatalf("trace differs at row %d: left=%+v right=%+v", i, left.Matches[i], right.Matches[i])
		}
	}
}

func TestTodo_WF_EXT_006_Golden(t *testing.T) {
	table := rules.Table{
		ID: "rules.golden.route", Version: "1",
		Inputs:    []rules.Column{{Name: "amount", Kind: rules.KindDecimal}},
		Outputs:   []rules.Column{{Name: "route_key", Kind: rules.KindString}},
		HitPolicy: rules.HitPolicyFirst,
		Rows:      []rules.Row{{ID: "allow", Conditions: []rules.Condition{rules.Any()}, Outputs: []rules.Value{rules.StringValue("ALLOW")}}},
	}
	ref := workflow.Reference{Kind: workflow.RefRule, ID: table.ID, Version: table.Version}
	encoded, err := Marshal(Payload{Ref: ref, Kind: KindDecisionTable, Table: &table})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"Ref":{"Kind":"RULE","ID":"rules.golden.route","Version":"1"},"BodyRef":null,"Digest":"sha256:7f5703e44ebd28f61518dab356845ce1d8e0094dfcd8cc4887c0050a280ef515","Status":"PUBLISHED","Kind":"DECISION_TABLE","Table":{"ID":"rules.golden.route","Version":"1","Inputs":[{"Name":"amount","Kind":1}],"Outputs":[{"Name":"route_key","Kind":2}],"HitPolicy":1,"Rows":[{"ID":"allow","Conditions":[{"Op":1,"Operand":{"kind":""},"Operands":null,"Low":{"kind":""},"High":{"kind":""}}],"Outputs":[{"kind":"STRING","text":"ALLOW"}]}]},"Expression":null,"Transform":null}`
	if !bytes.Equal(encoded, []byte(want)) {
		t.Fatalf("serialized payload differs from byte golden:\n got %s\nwant %s", encoded, want)
	}
}

func tableCopy(t *testing.T, store *Store, ref workflow.Reference, digest string) *rules.Table {
	t.Helper()
	payload, err := store.Resolve(ref, digest)
	if err != nil {
		t.Fatal(err)
	}
	return payload.Table
}

func mustDecimal(t *testing.T, text string) (d values.Decimal) {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
