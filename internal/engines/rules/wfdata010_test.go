package rules

import (
	"errors"
	"testing"
)

func wfdata010Pins() []TenantTypePin {
	return []TenantTypePin{{
		Namespace: "procurement", Kind: "VendorProfile", Version: 3,
		Fields: map[string]TenantField{
			"risk_score":  {Type: "decimal", Classification: "INTERNAL"},
			"vendor_name": {Type: "string", Classification: "PUBLIC"},
			"tax_id":      {Type: "string", Classification: "RESTRICTED"},
		},
	}}
}

func wfdata010Table(t *testing.T, col Column) Table {
	t.Helper()
	return Table{
		ID: "vendor-risk", Version: "1",
		Inputs:    []Column{col},
		Outputs:   []Column{{Name: "tier", Kind: KindString}},
		HitPolicy: HitPolicyFirst,
		Rows: []Row{
			{ID: "high", Conditions: []Condition{GreaterThan(DecimalValue(dec(t, "0.70", 2)))}, Outputs: []Value{StringValue("HIGH")}},
			{ID: "low", Conditions: []Condition{Any()}, Outputs: []Value{StringValue("LOW")}},
		},
	}
}

// TestTodo_WF_DATA_010 proves rule inputs may name typed paths into pinned
// tenant types: the compiler checks existence, type and classification
// against the pin, and evaluation rejects inputs from another version.
func TestTodo_WF_DATA_010(t *testing.T) {
	const path = "tenant.procurement.VendorProfile.risk_score"

	t.Run("tables resolve tenant paths against the pin", func(t *testing.T) {
		compiled, err := CompileTenantTable(wfdata010Table(t, Column{Name: path, Kind: KindDecimal, MaxClassification: TenantInternal}), wfdata010Pins())
		if err != nil {
			t.Fatalf("CompileTenantTable: %v", err)
		}
		result, err := EvaluateTenantTable(compiled, map[string]Value{path: DecimalValue(dec(t, "0.80", 2))}, map[string]uint64{path: 3})
		if err != nil {
			t.Fatalf("EvaluateTenantTable: %v", err)
		}
		if len(result.MatchedRowIDs) != 1 || result.MatchedRowIDs[0] != "high" {
			t.Fatalf("matched %+v, want [high]", result.MatchedRowIDs)
		}
	})

	t.Run("tables reject unknown types and fields", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			col  Column
			want error
		}{
			{"unknown type", Column{Name: "tenant.procurement.UnknownType.risk_score", Kind: KindDecimal}, ErrTenantTypeUnknown},
			{"unknown field", Column{Name: "tenant.procurement.VendorProfile.nope", Kind: KindDecimal}, ErrTenantFieldUnknown},
			{"malformed path", Column{Name: "tenant.procurement", Kind: KindDecimal}, ErrTenantPath},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := CompileTenantTable(wfdata010Table(t, tc.col), wfdata010Pins()); !errors.Is(err, tc.want) {
					t.Fatalf("CompileTenantTable error = %v, want %v", err, tc.want)
				}
			})
		}
	})

	t.Run("tables reject type mismatches", func(t *testing.T) {
		col := Column{Name: path, Kind: KindString, MaxClassification: TenantInternal}
		table := wfdata010Table(t, col)
		table.Rows[0].Conditions = []Condition{Equal(StringValue("x"))}
		if _, err := CompileTenantTable(table, wfdata010Pins()); !errors.Is(err, ErrTenantTypeMismatch) {
			t.Fatalf("CompileTenantTable error = %v, want %v", err, ErrTenantTypeMismatch)
		}
	})

	t.Run("tables check field classification against the ceiling", func(t *testing.T) {
		capped := Column{Name: path, Kind: KindDecimal, MaxClassification: TenantPublic}
		if _, err := CompileTenantTable(wfdata010Table(t, capped), wfdata010Pins()); !errors.Is(err, ErrTenantClassification) {
			t.Fatalf("over ceiling error = %v, want %v", err, ErrTenantClassification)
		}
		uncapped := Column{Name: "tenant.procurement.VendorProfile.tax_id", Kind: KindString}
		table := wfdata010Table(t, uncapped)
		table.Rows[0].Conditions = []Condition{Equal(StringValue("x"))}
		if _, err := CompileTenantTable(table, wfdata010Pins()); err != nil {
			t.Fatalf("uncapped column rejected: %v", err)
		}
	})

	t.Run("tables reject cross-version inputs", func(t *testing.T) {
		compiled, err := CompileTenantTable(wfdata010Table(t, Column{Name: path, Kind: KindDecimal, MaxClassification: TenantInternal}), wfdata010Pins())
		if err != nil {
			t.Fatalf("CompileTenantTable: %v", err)
		}
		inputs := map[string]Value{path: DecimalValue(dec(t, "0.80", 2))}
		if _, err := EvaluateTenantTable(compiled, inputs, map[string]uint64{path: 2}); !errors.Is(err, ErrTenantVersionMismatch) {
			t.Fatalf("stale version error = %v, want %v", err, ErrTenantVersionMismatch)
		}
		if _, err := EvaluateTenantTable(compiled, inputs, nil); !errors.Is(err, ErrTenantVersionMismatch) {
			t.Fatalf("missing version error = %v, want %v", err, ErrTenantVersionMismatch)
		}
	})
	t.Run("expressions resolve tenant paths against the pin", func(t *testing.T) {
		expr, err := Parse(path + " > 0.5")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		expr.UnknownSemantics = UnknownSemanticsPropagate
		expr.Inputs = []Input{{Name: path, Type: ExpressionTypeDecimal, MaxClassification: TenantInternal}}
		compiled, err := CompileTenantExpression(expr, wfdata010Pins())
		if err != nil {
			t.Fatalf("CompileTenantExpression: %v", err)
		}
		found := false
		for _, dep := range compiled.Dependencies {
			if dep.Name == "tenant.procurement.VendorProfile" && dep.Version == "3" && dep.Digest != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("compiled dependencies %+v lack the pinned tenant type", compiled.Dependencies)
		}
		result, err := EvaluateTenantExpression(compiled, map[string]Value{path: DecimalValue(dec(t, "0.80", 2))}, map[string]uint64{path: 3})
		if err != nil {
			t.Fatalf("EvaluateTenantExpression: %v", err)
		}
		ok, err := result.Value.Equal(BoolValue(true))
		if err != nil || !ok {
			t.Fatalf("result = %+v err=%v, want true", result.Value, err)
		}
	})

	t.Run("expressions reject cross-version inputs", func(t *testing.T) {
		expr, err := Parse(path + " > 0.5")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		expr.UnknownSemantics = UnknownSemanticsPropagate
		expr.Inputs = []Input{{Name: path, Type: ExpressionTypeDecimal, MaxClassification: TenantInternal}}
		compiled, err := CompileTenantExpression(expr, wfdata010Pins())
		if err != nil {
			t.Fatalf("CompileTenantExpression: %v", err)
		}
		inputs := map[string]Value{path: DecimalValue(dec(t, "0.80", 2))}
		if _, err := EvaluateTenantExpression(compiled, inputs, map[string]uint64{path: 4}); !errors.Is(err, ErrTenantVersionMismatch) {
			t.Fatalf("newer version error = %v, want %v", err, ErrTenantVersionMismatch)
		}
		if _, err := EvaluateTenantExpression(compiled, inputs, nil); !errors.Is(err, ErrTenantVersionMismatch) {
			t.Fatalf("missing version error = %v, want %v", err, ErrTenantVersionMismatch)
		}
	})
	t.Run("plain entry points refuse tenant paths", func(t *testing.T) {
		table := wfdata010Table(t, Column{Name: path, Kind: KindDecimal, MaxClassification: TenantInternal})
		if _, err := Compile(table); !errors.Is(err, ErrTenantPath) {
			t.Fatalf("Compile error = %v, want %v", err, ErrTenantPath)
		}
		expr, err := Parse(path + " > 0.5")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		expr.UnknownSemantics = UnknownSemanticsPropagate
		expr.Inputs = []Input{{Name: path, Type: ExpressionTypeDecimal}}
		if _, err := CompileExpression(expr); !errors.Is(err, ErrTenantPath) {
			t.Fatalf("CompileExpression error = %v, want %v", err, ErrTenantPath)
		}
	})

	t.Run("tables refuse field types without a table kind", func(t *testing.T) {
		pins := []TenantTypePin{{
			Namespace: "hr", Kind: "Leave", Version: 1,
			Fields: map[string]TenantField{"starts_on": {Type: "date", Classification: "INTERNAL"}},
		}}
		col := Column{Name: "tenant.hr.Leave.starts_on", Kind: KindString}
		table := wfdata010Table(t, col)
		table.Rows[0].Conditions = []Condition{Equal(StringValue("x"))}
		if _, err := CompileTenantTable(table, pins); !errors.Is(err, ErrTenantTypeMismatch) {
			t.Fatalf("CompileTenantTable error = %v, want %v", err, ErrTenantTypeMismatch)
		}
	})

	t.Run("expressions accept date fields and record pin conflicts", func(t *testing.T) {
		pins := []TenantTypePin{{
			Namespace: "hr", Kind: "Leave", Version: 1,
			Fields: map[string]TenantField{"starts_on": {Type: "date", Classification: "INTERNAL"}},
		}}
		name := "tenant.hr.Leave.starts_on"
		expr, err := Parse(name)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		expr.UnknownSemantics = UnknownSemanticsPropagate
		expr.Inputs = []Input{{Name: name, Type: ExpressionTypeDate}}
		compiled, err := CompileTenantExpression(expr, pins)
		if err != nil {
			t.Fatalf("date field rejected: %v", err)
		}
		result, err := EvaluateTenantExpression(compiled, map[string]Value{name: StringValue("2026-06-01")}, map[string]uint64{name: 1})
		if err != nil {
			t.Fatalf("EvaluateTenantExpression: %v", err)
		}
		ok, err := result.Value.Equal(StringValue("2026-06-01"))
		if err != nil || !ok {
			t.Fatalf("result = %+v err=%v, want the input", result.Value, err)
		}
		expr.Dependencies = []Dependency{{Name: "tenant.hr.Leave", Version: "9", Digest: "sha256:other"}}
		if _, err := CompileTenantExpression(expr, pins); !errors.Is(err, ErrExpressionDependency) {
			t.Fatalf("conflicting dependency error = %v, want %v", err, ErrExpressionDependency)
		}
	})

	t.Run("duplicate pins are refused", func(t *testing.T) {
		pins := append(wfdata010Pins(), wfdata010Pins()...)
		if _, err := CompileTenantTable(wfdata010Table(t, Column{Name: path, Kind: KindDecimal}), pins); !errors.Is(err, ErrTenantPinInvalid) {
			t.Fatalf("duplicate pins error = %v, want %v", err, ErrTenantPinInvalid)
		}
	})
}

// TestTodo_WF_DATA_010_Property proves version pinning is total: a rule
// compiled against one tenant type version rejects inputs from every other
// version, and unknown labels fail closed everywhere.
func TestTodo_WF_DATA_010_Property(t *testing.T) {
	const path = "tenant.procurement.VendorProfile.risk_score"

	compileTable := func(t *testing.T, pins []TenantTypePin) TenantTable {
		t.Helper()
		compiled, err := CompileTenantTable(wfdata010Table(t, Column{Name: path, Kind: KindDecimal}), pins)
		if err != nil {
			t.Fatalf("CompileTenantTable: %v", err)
		}
		return compiled
	}
	compileExpr := func(t *testing.T, pins []TenantTypePin) CompiledExpression {
		t.Helper()
		expr, err := Parse(path + " > 0.5")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		expr.UnknownSemantics = UnknownSemanticsPropagate
		expr.Inputs = []Input{{Name: path, Type: ExpressionTypeDecimal}}
		compiled, err := CompileTenantExpression(expr, pins)
		if err != nil {
			t.Fatalf("CompileTenantExpression: %v", err)
		}
		return compiled
	}

	t.Run("any non-pinned version is rejected", func(t *testing.T) {
		table := compileTable(t, wfdata010Pins())
		expr := compileExpr(t, wfdata010Pins())
		inputs := map[string]Value{path: DecimalValue(dec(t, "0.80", 2))}
		others := []uint64{0, 1, 2, 4, 7, 100, 1<<64 - 1}
		for _, v := range others {
			versions := map[string]uint64{path: v}
			if _, err := EvaluateTenantTable(table, inputs, versions); !errors.Is(err, ErrTenantVersionMismatch) {
				t.Fatalf("table accepted version %d", v)
			}
			if _, err := EvaluateTenantExpression(expr, inputs, versions); !errors.Is(err, ErrTenantVersionMismatch) {
				t.Fatalf("expression accepted version %d", v)
			}
		}
		if _, err := EvaluateTenantTable(table, inputs, map[string]uint64{path: 3}); err != nil {
			t.Fatalf("table rejected the pinned version: %v", err)
		}
		if _, err := EvaluateTenantExpression(expr, inputs, map[string]uint64{path: 3}); err != nil {
			t.Fatalf("expression rejected the pinned version: %v", err)
		}
	})

	t.Run("unknown labels fail closed", func(t *testing.T) {
		bad := wfdata010Pins()
		bad[0].Fields["risk_score"] = TenantField{Type: "decimal", Classification: "TOP_SECRET"}
		if _, err := CompileTenantTable(wfdata010Table(t, Column{Name: path, Kind: KindDecimal}), bad); err == nil {
			t.Fatal("pin with an unknown label compiled")
		}
		if _, err := ParseTenantDataClassification("TOP_SECRET"); err == nil {
			t.Fatal("unknown label parsed")
		}
		if TenantDataClassification("BOGUS").Dominates(TenantPublic) || TenantPublic.Dominates(TenantDataClassification("BOGUS")) {
			t.Fatal("lattice outsiders dominate")
		}
	})

	t.Run("re-pinning requires recompilation", func(t *testing.T) {
		old := compileTable(t, wfdata010Pins())
		fields := map[string]TenantField{
			"risk_score":  {Type: "decimal", Classification: "INTERNAL"},
			"vendor_name": {Type: "string", Classification: "PUBLIC"},
			"tax_id":      {Type: "string", Classification: "RESTRICTED"},
		}
		rePinned := compileTable(t, []TenantTypePin{{Namespace: "procurement", Kind: "VendorProfile", Version: 4, Fields: fields}})
		oldDigest, err := old.Digest()
		if err != nil {
			t.Fatalf("old digest: %v", err)
		}
		newDigest, err := rePinned.Digest()
		if err != nil {
			t.Fatalf("new digest: %v", err)
		}
		if oldDigest == newDigest {
			t.Fatal("re-pin did not change the digest")
		}
		inputs := map[string]Value{path: DecimalValue(dec(t, "0.80", 2))}
		if _, err := EvaluateTenantTable(old, inputs, map[string]uint64{path: 4}); !errors.Is(err, ErrTenantVersionMismatch) {
			t.Fatalf("old artifact accepted v4: %v", err)
		}
		if _, err := EvaluateTenantTable(rePinned, inputs, map[string]uint64{path: 3}); !errors.Is(err, ErrTenantVersionMismatch) {
			t.Fatalf("new artifact accepted v3: %v", err)
		}
	})
}
