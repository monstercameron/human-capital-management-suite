package docquery

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestTodo_WF_DATA_016_Security proves the query builder cannot be turned
// into an arbitrary script runner: every dangerous operator is rejected on
// both the Where path and the Encode path, hostile values never reach the
// wire, and hostile names never register.
func TestTodo_WF_DATA_016_Security(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, fixtureType())

	dangerous := []string{
		"$where", "$expr", "$function", "$accumulator", "$regex",
		"$lookup", "$unionWith", "$graphLookup", "$out", "$merge",
		"$group", "$project", "$match", "$unwind", "$facet",
		"$gt", "$lt", "$gte", "$lte", "$ne", "$in", "$nin",
		"$exists", "$type", "$text", "$comment", "$meta",
		"$natural", "$slice", "$rand", "$literal",
	}

	t.Run("OperatorsRejectedOnWherePath", func(t *testing.T) {
		for _, op := range dangerous {
			p := Predicate{field: "Salary", op: op, lo: 1}
			if _, err := reg.Query("hris.employee.v1").Where(p).Build(); !errors.Is(err, ErrInvalidPredicate) {
				t.Errorf("op %q: err = %v, want ErrInvalidPredicate", op, err)
			}
		}
	})

	t.Run("OperatorsRejectedOnEncodePath", func(t *testing.T) {
		for _, op := range dangerous {
			q := Query{typeName: "hris.employee.v1", preds: []Predicate{{field: "Salary", op: op, lo: 1}}, limit: 1}
			if _, err := q.Encode(); !errors.Is(err, ErrInvalidPredicate) {
				t.Errorf("op %q: err = %v, want ErrInvalidPredicate", op, err)
			}
		}
	})

	t.Run("HostileValuesNeverReachWire", func(t *testing.T) {
		hostiles := []any{
			map[string]any{"$gt": 0},
			map[string]any{"$where": "sleep(1)"},
			[]any{map[string]any{"$eq": 1}},
			[]any{"x"},
			nil,
			json.Number("NaN"),
		}
		for i, h := range hostiles {
			if _, err := reg.Query("hris.employee.v1").Where(Eq("FullName", h)).Build(); !errors.Is(err, ErrInvalidPredicate) {
				t.Errorf("hostile[%d] (%T): err = %v", i, h, err)
			}
			if h == nil {
				continue // nil is the open-ended range bound, valid by design
			}
			if _, err := reg.Query("hris.employee.v1").Where(Range("Salary", h, 5)).Build(); !errors.Is(err, ErrInvalidPredicate) {
				t.Errorf("hostile-range[%d] (%T): err = %v", i, h, err)
			}
		}
	})

	t.Run("InjectionStringStaysAString", func(t *testing.T) {
		evil := `x"}} ,{"$where":"1`
		q, err := reg.Query("hris.employee.v1").Where(Eq("FullName", evil)).Build()
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		raw, err := q.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		var decoded map[string]map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("predicate is not valid JSON: %v\n%s", err, raw)
		}
		if decoded["FullName"]["$eq"] != evil {
			t.Fatalf("value mutated: %v", decoded)
		}
		if len(decoded) != 1 {
			t.Fatalf("predicate grew fields: %s", raw)
		}
	})

	t.Run("HostileNamesNeverRegister", func(t *testing.T) {
		for _, name := range []string{"", "A.B", "a b", "a/b", "..", "a..b", "$type", "a-b", ".a", "a."} {
			dt := fixtureType()
			dt.Name = name
			if err := NewRegistry().Register(dt); !errors.Is(err, ErrInvalidTypeName) {
				t.Errorf("type %q: err = %v", name, err)
			}
		}
		for _, name := range []string{"", "$where", "a.b", "a-b", "a b", "0a"} {
			dt := fixtureType()
			dt.Fields[0].Name = name
			if err := NewRegistry().Register(dt); !errors.Is(err, ErrInvalidFieldName) {
				t.Errorf("field %q: err = %v", name, err)
			}
		}
		for _, c := range []Classification{"", "PUBLIC", "topsecret", "internal "} {
			dt := fixtureType()
			dt.Classification = c
			if err := NewRegistry().Register(dt); !errors.Is(err, ErrInvalidClassification) {
				t.Errorf("classification %q: err = %v", c, err)
			}
		}
	})

	t.Run("LimitsCannotEscape", func(t *testing.T) {
		for _, n := range []int{0, -1, -1 << 30, MaxLimit + 1, 1 << 30} {
			_, err := reg.Query("hris.employee.v1").Where(Eq("FullName", "x")).Limit(n).Build()
			if !errors.Is(err, ErrInvalidLimit) {
				t.Errorf("limit %d: err = %v", n, err)
			}
		}
	})

	t.Run("QuarantineLeaksNoPayload", func(t *testing.T) {
		secret := "sk-live-SECRET"
		raw := `{"EmployeeID":"E-1","HiredOn":"2024-01-02","FullName":` + `"` + secret + `","Rogue":"` + secret + `"}`
		_, q, err := reg.ValidateDocument("hris.employee.v1", json.RawMessage(raw))
		if err != nil || q == nil {
			t.Fatalf("q=%v err=%v", q, err)
		}
		if strings.Contains(q.Error(), secret) || strings.Contains(q.Reason.Error(), secret) {
			t.Fatalf("quarantine entry leaks payload: %q", q.Error())
		}
	})
}
