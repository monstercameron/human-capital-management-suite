package rules

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func wfExt016Compile(t *testing.T, root Expr, inputs []Input, deps ...Dependency) CompiledExpression {
	t.Helper()
	expr := Expression{Root: root, Inputs: inputs, Dependencies: deps, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
	compiled, err := CompileExpression(expr)
	if err != nil {
		t.Fatalf("CompileExpression: %v", err)
	}
	return compiled
}

func wfExt016Date(value string) Expr { return DateLiteral(value) }

// TestTodo_WF_EXT_016 pins the function registry and exercises each published
// addition against the pure evaluator.
func TestTodo_WF_EXT_016(t *testing.T) {
	lib := DefaultExpressionFunctionLibrary()
	if lib.Version() != "1" || lib.Digest() == "" {
		t.Fatalf("library identity = %s/%s", lib.Version(), lib.Digest())
	}
	entries := lib.Entries()
	if !sortFunctionEntries(entries) {
		t.Fatalf("registry is not ordered: %+v", entries)
	}
	for _, name := range []string{"lower", "upper", "contains", "starts_with", "ends_with", "abs", "len", "list_contains", "interval_overlaps", "business_day_diff"} {
		found := false
		for _, entry := range entries {
			if entry.Name == name && entry.Version != "" && entry.Cost > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing versioned costed function %q", name)
		}
	}

	t.Run("typed list membership", func(t *testing.T) {
		compiled := wfExt016Compile(t, Call("list_contains", Variable("codes", ExpressionTypeList), StringLiteral("HR")), []Input{{Name: "codes", Type: ExpressionTypeList, ElementType: ExpressionTypeString}})
		result, err := compiled.Evaluate(map[string]Value{"codes": ListValue(StringValue("ENG"), StringValue("HR"))})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := result.Value.Bool()
		if result.State != EvalStatePresent || !got {
			t.Fatalf("membership result = %+v", result)
		}
		if compiled.IR.Nodes[compiled.IR.Root].FunctionVersion != "1" || compiled.FunctionLibraryDigest != lib.Digest() {
			t.Fatalf("unpublished function metadata: %+v", compiled)
		}
	})

	t.Run("half open date intervals", func(t *testing.T) {
		compiled := wfExt016Compile(t, Call("interval_overlaps", wfExt016Date("2026-01-01"), wfExt016Date("2026-02-01"), wfExt016Date("2026-02-01"), wfExt016Date("2026-03-01")), nil)
		result, err := compiled.Evaluate(nil)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := result.Value.Bool()
		if got {
			t.Fatal("adjacent half-open intervals overlap")
		}
	})

	t.Run("pinned business calendar", func(t *testing.T) {
		working := []string{"2026-01-05", "2026-01-06", "2026-01-07"}
		calendarDigest, err := BusinessCalendarSnapshotDigest("harborcare.us.business", "2026.1", working)
		if err != nil {
			t.Fatal(err)
		}
		call := Call("business_day_diff", wfExt016Date("2026-01-02"), wfExt016Date("2026-01-07"), StringLiteral("harborcare.us.business"), StringLiteral("2026.1"), StringLiteral(calendarDigest), Variable("working", ExpressionTypeList))
		compiled := wfExt016Compile(t, call, []Input{{Name: "working", Type: ExpressionTypeList, ElementType: ExpressionTypeDate}}, Dependency{Name: "calendar.harborcare.us.business", Version: "2026.1", Digest: calendarDigest})
		result, err := compiled.Evaluate(map[string]Value{"working": ListValue(StringValue(working[0]), StringValue(working[1]), StringValue(working[2]))})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := result.Value.Int()
		if got != 3 {
			t.Fatalf("business day difference = %d, want 3", got)
		}
		unpinned := Call("business_day_diff", wfExt016Date("2026-01-01"), wfExt016Date("2026-01-02"), StringLiteral("harborcare.us.business"), StringLiteral("2026.1"), StringLiteral(calendarDigest), Variable("working", ExpressionTypeList))
		if _, err := CompileExpression(Expression{Root: unpinned, Inputs: []Input{{Name: "working", Type: ExpressionTypeList, ElementType: ExpressionTypeDate}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}); !errors.Is(err, ErrExpressionDependency) {
			t.Fatalf("unpinned calendar error = %v", err)
		}
		badSnapshot := append([]string(nil), working...)
		badSnapshot[2] = "2026-01-08"
		_, err = compiled.Evaluate(map[string]Value{"working": ListValue(StringValue(badSnapshot[0]), StringValue(badSnapshot[1]), StringValue(badSnapshot[2]))})
		if !errors.Is(err, ErrExpressionDependency) {
			t.Fatalf("calendar contents not tied to dependency digest: %v", err)
		}
		forward, err := BusinessCalendarSnapshotDigest("harborcare.us.business", "2026.1", working)
		if err != nil {
			t.Fatal(err)
		}
		reverse, err := BusinessCalendarSnapshotDigest("harborcare.us.business", "2026.1", []string{working[2], working[1], working[0]})
		if err != nil || forward != reverse {
			t.Fatalf("snapshot order changed digest: %s / %s / %v", forward, reverse, err)
		}
	})

	t.Run("dynamic cost is enforced", func(t *testing.T) {
		expr := Expression{Root: Call("list_contains", Variable("codes", ExpressionTypeList), StringLiteral("absent")), Inputs: []Input{{Name: "codes", Type: ExpressionTypeList, ElementType: ExpressionTypeString}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
		compiled, err := CompileExpression(expr, CostLimit{MaxNodes: 16, MaxDepth: 8, MaxIterations: 32, MaxCost: 8})
		if err != nil {
			t.Fatal(err)
		}
		_, err = compiled.Evaluate(map[string]Value{"codes": ListValue(StringValue("a"), StringValue("b"), StringValue("c"), StringValue("d"), StringValue("e"), StringValue("f"), StringValue("g"), StringValue("h"), StringValue("i"), StringValue("j"))})
		if !errors.Is(err, ErrExpressionUnbounded) {
			t.Fatalf("cost limit error = %v", err)
		}
	})
}

func TestTodo_WF_EXT_016_CompiledArtifactIntegrity(t *testing.T) {
	expr := Expression{Root: Call("lower", Variable("value", ExpressionTypeString)), Inputs: []Input{{Name: "value", Type: ExpressionTypeString}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
	compiled, err := CompileExpression(expr)
	if err != nil {
		t.Fatal(err)
	}
	mutated := cloneCompiledForTest(compiled)
	mutated.IR.Nodes[0].Name = "changed"
	if _, err := mutated.Evaluate(map[string]Value{"value": StringValue("X")}); !errors.Is(err, ErrExpressionInvalid) {
		t.Fatalf("mutated IR was not rejected: %v", err)
	}
	mutated = cloneCompiledForTest(compiled)
	call := &mutated.IR.Nodes[mutated.IR.Root]
	call.FunctionVersion = "99"
	resealCompiledForTest(t, &mutated)
	if _, err := mutated.Evaluate(map[string]Value{"value": StringValue("X")}); !errors.Is(err, ErrExpressionFunction) {
		t.Fatalf("unsupported per-call version was not rejected: %v", err)
	}
	mutated = cloneCompiledForTest(compiled)
	call = &mutated.IR.Nodes[mutated.IR.Root]
	call.FunctionCost++
	resealCompiledForTest(t, &mutated)
	if _, err := mutated.Evaluate(map[string]Value{"value": StringValue("X")}); !errors.Is(err, ErrExpressionFunction) {
		t.Fatalf("modified per-call cost was not rejected: %v", err)
	}
	mutated = cloneCompiledForTest(compiled)
	mutated.Limits.MaxCost++
	if _, err := mutated.Evaluate(map[string]Value{"value": StringValue("X")}); !errors.Is(err, ErrExpressionInvalid) {
		t.Fatalf("modified authorization limit was not rejected: %v", err)
	}
}

func cloneCompiledForTest(compiled CompiledExpression) CompiledExpression {
	compiled.IR.Nodes = append([]IRNode(nil), compiled.IR.Nodes...)
	for i := range compiled.IR.Nodes {
		compiled.IR.Nodes[i].Children = append([]int(nil), compiled.IR.Nodes[i].Children...)
	}
	compiled.Inputs = append([]Input(nil), compiled.Inputs...)
	compiled.Dependencies = append([]Dependency(nil), compiled.Dependencies...)
	return compiled
}

func resealCompiledForTest(t *testing.T, compiled *CompiledExpression) {
	t.Helper()
	depDigest, err := dependenciesDigest(compiled.Dependencies)
	if err != nil {
		t.Fatal(err)
	}
	compiled.Digest, err = compiledDigest(compiled.Version, compiled.UnknownSemantics, compiled.Inputs, compiled.Dependencies, depDigest, compiled.FunctionLibraryVersion, compiled.FunctionLibraryDigest, compiled.IR, compiled.Cost, compiled.Limits)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WF_EXT_016_InputDeclarationsCost(t *testing.T) {
	inputs := []Input{{Name: "a", Type: ExpressionTypeString}, {Name: "b", Type: ExpressionTypeString}, {Name: "c", Type: ExpressionTypeString}}
	_, err := CompileExpression(Expression{Root: BoolLiteral(true), Inputs: inputs, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}, CostLimit{MaxNodes: 8, MaxDepth: 4, MaxIterations: 4, MaxCost: 2})
	if !errors.Is(err, ErrExpressionUnbounded) {
		t.Fatalf("unbounded unused declarations accepted: %v", err)
	}
}

func TestTodo_WF_EXT_016_AggregateListPreflight(t *testing.T) {
	expr := Expression{Root: BoolLiteral(true), Inputs: []Input{{Name: "a", Type: ExpressionTypeList, ElementType: ExpressionTypeString}, {Name: "b", Type: ExpressionTypeList, ElementType: ExpressionTypeString}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
	compiled, err := CompileExpression(expr, CostLimit{MaxNodes: 8, MaxDepth: 4, MaxIterations: 8, MaxCost: 5})
	if err != nil {
		t.Fatal(err)
	}
	// The first list is malformed. The second list pushes aggregate work past
	// MaxCost, so evaluation must preflight both lengths before inspecting a.
	_, err = compiled.Evaluate(map[string]Value{"a": ListValue(StringValue("x"), IntValue(1)), "b": ListValue(StringValue("y"), StringValue("z"))})
	if !errors.Is(err, ErrExpressionUnbounded) {
		t.Fatalf("aggregate list budget was not preflighted: %v", err)
	}
}

func sortFunctionEntries(entries []FunctionEntry) bool {
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name >= entries[i].Name {
			return false
		}
	}
	return true
}

// TestTodo_WF_EXT_016_Property checks deterministic publication and defensive
// copies of the registry contract.
func TestTodo_WF_EXT_016_Property(t *testing.T) {
	a, b := DefaultExpressionFunctionLibrary(), DefaultExpressionFunctionLibrary()
	if a.Digest() != b.Digest() || !reflect.DeepEqual(a.Entries(), b.Entries()) {
		t.Fatal("equal library versions published different entries or digests")
	}
	entries := a.Entries()
	entries[0].Name = "tampered"
	entries[0].Signature.Arguments[0] = ExpressionTypeUnknown
	if a.Digest() != b.Digest() || a.Entries()[0].Name == "tampered" {
		t.Fatal("registry entries were mutable through a returned slice")
	}
	expr := Expression{Root: Call("lower", Variable("value", ExpressionTypeString)), Inputs: []Input{{Name: "value", Type: ExpressionTypeString}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
	first, err := CompileExpression(expr)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileExpression(expr)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.IR.Canonical() != second.IR.Canonical() {
		t.Fatal("same published expression compiled nondeterministically")
	}
}

// FuzzTodo_WF_EXT_016 proves malformed and valid AST shapes stay inside the
// supplied cost limits and the evaluator never invokes ambient capabilities.
func FuzzTodo_WF_EXT_016(f *testing.F) {
	for _, seed := range []string{"list_contains(items, \"x\")", "interval_overlaps(\"2026-01-01\",\"2026-02-01\",\"2026-01-15\",\"2026-03-01\")", "today()", "exec(\"x\")", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		expr, err := Parse(source)
		if err != nil {
			return
		}
		expr.Version = "1"
		expr.UnknownSemantics = UnknownSemanticsPropagate
		compiled, err := CompileExpression(expr, CostLimit{MaxNodes: 32, MaxDepth: 8, MaxIterations: 16, MaxCost: 64})
		if err != nil {
			return
		}
		if compiled.Cost.Nodes > 32 || compiled.Cost.Depth > 8 || compiled.Cost.Iterations > 16 || compiled.Cost.Total > 64 {
			t.Fatalf("compiled cost exceeds limit: %+v", compiled.Cost)
		}
		_, _ = compiled.Evaluate(nil)

		// Independently exercise the input dependent scan charge with varied
		// list sizes so fuzzing covers evaluator work as well as compiler shape.
		count := len(source) % 16
		items := make([]Value, count)
		for i := range items {
			items[i] = StringValue("item")
		}
		bounded, compileErr := CompileExpression(Expression{Root: Call("list_contains", Variable("items", ExpressionTypeList), StringLiteral("missing")), Inputs: []Input{{Name: "items", Type: ExpressionTypeList, ElementType: ExpressionTypeString}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}, CostLimit{MaxNodes: 8, MaxDepth: 4, MaxIterations: 16, MaxCost: 64})
		if compileErr != nil {
			t.Fatalf("fixed bounded expression did not compile: %v", compileErr)
		}
		_, evalErr := bounded.Evaluate(map[string]Value{"items": ListValue(items...)})
		if count > 4 && !errors.Is(evalErr, ErrExpressionUnbounded) {
			t.Fatalf("list length %d escaped runtime cost limit: %v", count, evalErr)
		}
		if count <= 4 && evalErr != nil {
			t.Fatalf("bounded list length %d failed: %v", count, evalErr)
		}

		// String functions charge bytes scanned, including expressions whose
		// parser input drives arbitrary string lengths.
		textExpr := Expression{Root: Call("lower", Variable("text", ExpressionTypeString)), Inputs: []Input{{Name: "text", Type: ExpressionTypeString}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
		textCompiled, textCompileErr := CompileExpression(textExpr, CostLimit{MaxNodes: 8, MaxDepth: 4, MaxIterations: 16, MaxCost: 32})
		if textCompileErr != nil {
			t.Fatal(textCompileErr)
		}
		_, textErr := textCompiled.Evaluate(map[string]Value{"text": StringValue(source)})
		if len(source) > 28 && !errors.Is(textErr, ErrExpressionUnbounded) {
			t.Fatalf("string scan of %d bytes escaped its cost budget: %v", len(source), textErr)
		}
		if len(source) <= 28 && textErr != nil {
			t.Fatalf("bounded string scan of %d bytes failed: %v", len(source), textErr)
		}

		// Calendar path binds the literal snapshot identity, dependency and the
		// actual working-date list under fuzzed cardinalities.
		calendarDates := make([]string, count)
		for i := range calendarDates {
			calendarDates[i] = time.Date(2026, time.January, i+1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		}
		calendarRef, calendarVersion := "fuzz.calendar", "v1"
		calendarDigest, digestErr := BusinessCalendarSnapshotDigest(calendarRef, calendarVersion, calendarDates)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		dateValues := make([]Value, count)
		for i, date := range calendarDates {
			dateValues[i] = StringValue(date)
		}
		calendarExpr := Expression{Root: Call("business_day_diff", wfExt016Date("2026-01-01"), wfExt016Date("2026-01-31"), StringLiteral(calendarRef), StringLiteral(calendarVersion), StringLiteral(calendarDigest), Variable("working", ExpressionTypeList)), Inputs: []Input{{Name: "working", Type: ExpressionTypeList, ElementType: ExpressionTypeDate}}, Dependencies: []Dependency{{Name: "calendar." + calendarRef, Version: calendarVersion, Digest: calendarDigest}}, Version: "1", UnknownSemantics: UnknownSemanticsPropagate}
		calendarCompiled, calendarCompileErr := CompileExpression(calendarExpr, CostLimit{MaxNodes: 16, MaxDepth: 8, MaxIterations: 16, MaxCost: 64})
		if calendarCompileErr != nil {
			t.Fatal(calendarCompileErr)
		}
		_, calendarErr := calendarCompiled.Evaluate(map[string]Value{"working": ListValue(dateValues...)})
		if calendarErr != nil && !errors.Is(calendarErr, ErrExpressionUnbounded) {
			t.Fatalf("valid pinned calendar failed: %v", calendarErr)
		}
		if count > 0 {
			mutatedDates := append([]Value(nil), dateValues...)
			mutatedDates[count-1] = StringValue("2026-01-31")
			_, badCalendarErr := calendarCompiled.Evaluate(map[string]Value{"working": ListValue(mutatedDates...)})
			if calendarErr == nil && !errors.Is(badCalendarErr, ErrExpressionDependency) {
				t.Fatalf("calendar contents escaped snapshot digest: %v", badCalendarErr)
			}
			if calendarErr != nil && !errors.Is(badCalendarErr, ErrExpressionUnbounded) && !errors.Is(badCalendarErr, ErrExpressionDependency) {
				t.Fatalf("mutated calendar failed unexpectedly: %v", badCalendarErr)
			}
		}
	})
}
