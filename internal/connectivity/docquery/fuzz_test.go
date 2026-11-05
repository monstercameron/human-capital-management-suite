package docquery

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTodo_WF_DATA_016_Fuzz is the deterministic adversarial corpus: hostile
// shapes that must either fail closed or encode to a predicate containing
// only the sealed operator vocabulary.
func TestTodo_WF_DATA_016_Fuzz(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, fixtureType())

	fields := []string{"FullName", "Salary", "HiredOn", "EmployeeID",
		"", "$where", "a.b", "Salary ", " salary", "SALARY",
		"FullName\x00", "FullName\n", strings.Repeat("F", 4096)}
	values := []string{"", "x", "0", "-1", "1.5", "true", "null",
		"{}", "[]", `{"$gt":1}`, `"$eq"`, `$where`, "\x00", "\xff",
		`"`, `\`, "{{", "}}", strings.Repeat("$", 1024)}

	checked := 0
	for _, f := range fields {
		for _, v := range values {
			for _, num := range []bool{false, true} {
				var val any = v
				if num {
					val = json.Number(v)
				}
				assertClosed(t, reg, Eq(f, val))
				assertClosed(t, reg, Range(f, val, nil))
				assertClosed(t, reg, Range(f, nil, val))
				assertClosed(t, reg, Range(f, val, val))
				checked += 4
			}
		}
	}
	t.Logf("corpus: %d predicate shapes checked", checked)
}

// assertClosed requires a predicate to either fail closed or encode to a
// shape using only the sealed operators.
func assertClosed(t *testing.T, reg *Registry, p Predicate) {
	t.Helper()
	q, err := reg.Query("hris.employee.v1").Where(p).Build()
	if err != nil {
		return // failed closed on the Where path
	}
	raw, err := q.Encode()
	if err != nil {
		return // failed closed on the Encode path
	}
	assertSealed(t, raw)
}

// assertSealed requires encoded predicate JSON to carry only $eq/$gte/$lte
// operator keys.
func assertSealed(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var decoded map[string]map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("predicate not valid JSON: %v\n%s", err, raw)
	}
	for field, cond := range decoded {
		for op := range cond {
			if !allowedOperators[op] {
				t.Fatalf("field %q carries foreign operator %q in %s", field, op, raw)
			}
		}
	}
}

// FuzzDocQueryOperators fuzzes the public builder surface: arbitrary field
// names and string/number values must either fail closed or encode sealed.
func FuzzDocQueryOperators(f *testing.F) {
	seeds := []string{"FullName", "Salary", "", "$where", "a.b",
		`{"$gt":1}`, "\x00", strings.Repeat("S", 512)}
	for _, field := range seeds {
		for _, value := range seeds {
			f.Add(field, value, int64(0))
			f.Add(field, value, int64(1))
			f.Add(field, value, int64(2))
		}
	}
	f.Fuzz(func(t *testing.T, field, value string, shape int64) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		var val any = value
		if shape%2 == 1 {
			val = json.Number(value)
		}
		var p Predicate
		switch shape % 3 {
		case 0:
			p = Eq(field, val)
		case 1:
			p = Range(field, val, nil)
		default:
			p = Range(field, val, val)
		}
		assertClosed(t, reg, p)
	})
}
