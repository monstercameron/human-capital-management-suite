package docquery

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fixtureType is a governed employee document used across tests.
func fixtureType() DocumentType {
	return DocumentType{
		Name:           "hris.employee.v1",
		Classification: ClassificationConfidential,
		Fields: []Field{
			{Name: "EmployeeID", WireType: "EntityId", Indexed: true, Required: true},
			{Name: "FullName", WireType: "string", Indexed: true},
			{Name: "Salary", WireType: "Money", Indexed: true},
			{Name: "Active", WireType: "bool"},
			{Name: "HiredOn", WireType: "TemporalInstant", Indexed: true, Required: true},
			{Name: "Tags", WireType: "[]string"},
			{Name: "Meta", WireType: "map[string]string"},
		},
	}
}

func mustRegister(t *testing.T, reg *Registry, dt DocumentType) {
	t.Helper()
	if err := reg.Register(dt); err != nil {
		t.Fatalf("Register(%q): %v", dt.Name, err)
	}
}

func TestTodo_WF_DATA_016(t *testing.T) {
	t.Run("RegisterAndLookup", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		got, err := reg.Lookup("hris.employee.v1")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if got.Classification != ClassificationConfidential {
			t.Fatalf("classification = %q", got.Classification)
		}
		if names := reg.Types(); len(names) != 1 || names[0] != "hris.employee.v1" {
			t.Fatalf("Types = %v", names)
		}
		if _, err := reg.Lookup("hris.nobody.v1"); !errors.Is(err, ErrUnknownType) {
			t.Fatalf("Lookup unknown: %v", err)
		}
	})

	t.Run("RegisterRejectsBadSchemas", func(t *testing.T) {
		cases := []struct {
			name string
			mut  func(*DocumentType)
			want error
		}{
			{"BadTypeName", func(dt *DocumentType) { dt.Name = "HRIS/employee" }, ErrInvalidTypeName},
			{"BadClassification", func(dt *DocumentType) { dt.Classification = "topsecret" }, ErrInvalidClassification},
			{"NoFields", func(dt *DocumentType) { dt.Fields = nil }, ErrInvalidSchema},
			{"DupField", func(dt *DocumentType) {
				dt.Fields = append(dt.Fields, Field{Name: "Active", WireType: "bool"})
			}, ErrInvalidSchema},
			{"BadFieldName", func(dt *DocumentType) { dt.Fields[0].Name = "$where" }, ErrInvalidFieldName},
			{"UnknownWire", func(dt *DocumentType) { dt.Fields[0].WireType = "uuid" }, ErrUnknownWireType},
		}
		for _, c := range cases {
			dt := fixtureType()
			c.mut(&dt)
			if err := NewRegistry().Register(dt); !errors.Is(err, c.want) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
			}
		}
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		if err := reg.Register(fixtureType()); !errors.Is(err, ErrTypeExists) {
			t.Fatalf("re-register: %v", err)
		}
	})

	t.Run("AllWireTypesRegister", func(t *testing.T) {
		wires := []string{"string", "bool", "int32", "uint32", "int64",
			"FixedDecimal", "Money", "Percentage", "Quantity", "Rate",
			"TemporalInstant", "LocalDate", "LocalTime", "ZonedDateTime",
			"TemporalInterval", "RecordedAt", "KnownAt",
			"EntityId", "EntityRef", "ResourceKey", "RevisionToken",
			"PresenceValue", "[]string", "[]byte",
			"[]int64", "[]EntityId", "map[string]string", "map[string]int64"}
		reg := NewRegistry()
		for i, w := range wires {
			dt := DocumentType{
				Name:           "wire.probe.v1",
				Classification: ClassificationInternal,
				Fields:         []Field{{Name: "F", WireType: w}},
			}
			_ = i
			r := NewRegistry()
			if err := r.Register(dt); err != nil {
				t.Errorf("wire %q: %v", w, err)
			}
		}
		_ = reg
	})

	t.Run("SourceRouting", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		mustRegister(t, reg, DocumentType{
			Name: "vault.apikey.v1", Classification: ClassificationRestricted,
			Fields: []Field{{Name: "Kid", WireType: "string", Indexed: true}},
		})
		src, err := reg.SourceOf("hris.employee.v1")
		if err != nil || src != DefaultSource {
			t.Fatalf("SourceOf employee = %q, %v", src, err)
		}
		src, err = reg.SourceOf("vault.apikey.v1")
		if err != nil || src != VaultSource {
			t.Fatalf("SourceOf apikey = %q, %v", src, err)
		}
		if _, err := reg.SourceOf("nope.v1"); !errors.Is(err, ErrUnknownType) {
			t.Fatalf("SourceOf unknown: %v", err)
		}
	})

	t.Run("ValidateAcceptsGovernedDocument", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		raw := json.RawMessage(`{"EmployeeID":"E-1","FullName":"A. R.","Salary":120000,` +
			`"Active":true,"HiredOn":{"$date":"2024-01-02T03:04:05Z"},` +
			`"Tags":["a","b"],"Meta":{"k":"v"}}`)
		doc, q, err := reg.ValidateDocument("hris.employee.v1", raw)
		if err != nil || q != nil {
			t.Fatalf("ValidateDocument: doc=%+v q=%v err=%v", doc, q, err)
		}
		if doc.Fields["FullName"] != "A. R." {
			t.Fatalf("Fields = %v", doc.Fields)
		}
		if len(reg.Quarantine()) != 0 {
			t.Fatalf("ledger should be empty, got %v", reg.Quarantine())
		}
	})

	t.Run("ValidateQuarantinesDrift", func(t *testing.T) {
		cases := []struct {
			name  string
			raw   string
			field string
			want  error
		}{
			{"Undeclared", `{"EmployeeID":"E-1","HiredOn":"2024-01-02T03:04:05Z","Nick":"x"}`, "Nick", ErrUndeclaredField},
			{"WrongType", `{"EmployeeID":42,"HiredOn":"2024-01-02T03:04:05Z"}`, "EmployeeID", ErrTypeMismatch},
			{"MissingRequired", `{"EmployeeID":"E-1"}`, "HiredOn", ErrMissingField},
			{"NullRequired", `{"EmployeeID":"E-1","HiredOn":null}`, "HiredOn", ErrMissingField},
			{"IntOverflow", `{"EmployeeID":"E-1","HiredOn":"2024-01-02T03:04:05Z","Salary":{"$numberDecimal":"12"}}`, "", nil}, // control: marker ok
		}
		for _, c := range cases {
			reg := NewRegistry()
			mustRegister(t, reg, fixtureType())
			_, q, err := reg.ValidateDocument("hris.employee.v1", json.RawMessage(c.raw))
			if c.want == nil {
				if err != nil || q != nil {
					t.Errorf("%s: want accept, got q=%v err=%v", c.name, q, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: hard error %v, want quarantine", c.name, err)
				continue
			}
			if q == nil {
				t.Errorf("%s: want quarantine, got accept", c.name)
				continue
			}
			if !errors.Is(q.Reason, c.want) {
				t.Errorf("%s: reason = %v, want %v", c.name, q.Reason, c.want)
			}
			if q.Field != c.field {
				t.Errorf("%s: field = %q, want %q", c.name, q.Field, c.field)
			}
			if len(reg.Quarantine()) != 1 {
				t.Errorf("%s: ledger has %d entries, want 1", c.name, len(reg.Quarantine()))
			}
			if !strings.Contains(q.Error(), c.field) && c.field != "" {
				t.Errorf("%s: Error() = %q, want field named", c.name, q.Error())
			}
		}
	})

	t.Run("ValidateRejectsNonObjects", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		for _, raw := range []string{`[1,2]`, `"str"`, `42`, `not json`} {
			_, q, err := reg.ValidateDocument("hris.employee.v1", json.RawMessage(raw))
			if err != nil || q == nil || !errors.Is(q.Reason, ErrInvalidDocument) {
				t.Errorf("raw %q: q=%v err=%v", raw, q, err)
			}
		}
		if _, _, err := reg.ValidateDocument("nope.v1", json.RawMessage(`{}`)); !errors.Is(err, ErrUnknownType) {
			t.Fatalf("unknown type: %v", err)
		}
	})

	t.Run("IntBounds", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, DocumentType{
			Name: "nums.v1", Classification: ClassificationInternal,
			Fields: []Field{
				{Name: "A", WireType: "int32", Required: true},
				{Name: "B", WireType: "uint32", Required: true},
			},
		})
		ok := `{"A":2147483647,"B":4294967295}`
		if _, q, err := reg.ValidateDocument("nums.v1", json.RawMessage(ok)); err != nil || q != nil {
			t.Fatalf("max bounds: q=%v err=%v", q, err)
		}
		for _, raw := range []string{`{"A":2147483648,"B":0}`, `{"A":0,"B":-1}`, `{"A":1.5,"B":0}`} {
			_, q, err := reg.ValidateDocument("nums.v1", json.RawMessage(raw))
			if err != nil || q == nil || !errors.Is(q.Reason, ErrTypeMismatch) {
				t.Errorf("raw %s: q=%v err=%v", raw, q, err)
			}
		}
	})

	t.Run("EncodeCanonical", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		q, err := reg.Query("hris.employee.v1").
			Where(Range("Salary", 50000, 150000), Eq("FullName", "A. R.")).
			Limit(25).
			Build()
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		raw, err := q.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		want := `{"FullName":{"$eq":"A. R."},"Salary":{"$gte":50000,"$lte":150000}}`
		if string(raw) != want {
			t.Fatalf("Encode =\n%s\nwant\n%s", raw, want)
		}
	})

	t.Run("BuilderRejectsBadPredicates", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		cases := []struct {
			name string
			p    Predicate
			want error
		}{
			{"NonIndexed", Eq("Active", true), ErrUnindexedField},
			{"Undeclared", Eq("Nope", true), ErrUnindexedField},
			{"EmptyField", Eq("", "x"), ErrInvalidPredicate},
			{"ObjectValue", Eq("FullName", map[string]any{"$gt": 1}), ErrInvalidPredicate},
			{"ArrayValue", Eq("FullName", []any{"x"}), ErrInvalidPredicate},
			{"NilValue", Eq("FullName", nil), ErrInvalidPredicate},
			{"UnboundedRange", Range("Salary", nil, nil), ErrInvalidPredicate},
			{"MixedRange", Range("Salary", "a", 3), ErrInvalidPredicate},
			{"FieldSmuggle", Eq("a.$gt", "x"), ErrInvalidFieldName},
		}
		for _, c := range cases {
			_, err := reg.Query("hris.employee.v1").Where(c.p).Build()
			if !errors.Is(err, c.want) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
			}
		}
		if _, err := reg.Query("hris.employee.v1").Where(Eq("FullName", "x")).Limit(0).Build(); !errors.Is(err, ErrInvalidLimit) {
			t.Errorf("limit 0: %v", err)
		}
		if _, err := reg.Query("hris.employee.v1").Where(Eq("FullName", "x")).Limit(MaxLimit + 1).Build(); !errors.Is(err, ErrInvalidLimit) {
			t.Errorf("limit max+1: %v", err)
		}
		if _, err := reg.Query("nope.v1").Build(); !errors.Is(err, ErrUnknownType) {
			t.Errorf("unknown type query: %v", err)
		}
		dup, err := reg.Query("hris.employee.v1").
			Where(Range("Salary", 1, 2), Range("Salary", 3, 4)).Build()
		if err != nil {
			t.Fatalf("dup build: %v", err)
		}
		if _, err := dup.Encode(); !errors.Is(err, ErrInvalidPredicate) {
			t.Errorf("dup bounds encode: %v", err)
		}
	})

	t.Run("ExecuteFiltersDrift", func(t *testing.T) {
		reg := NewRegistry()
		mustRegister(t, reg, fixtureType())
		fake := &fakeQuerier{rows: []string{
			`{"EmployeeID":"E-1","HiredOn":"2024-01-02T03:04:05Z","FullName":"Ok"}`,
			`{"EmployeeID":"E-2","HiredOn":"2024-01-02T03:04:05Z","Rogue":true}`,
		}}
		q, err := reg.Query("hris.employee.v1").Where(Eq("FullName", "Ok")).Limit(10).Build()
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		docs, err := q.Execute(reg, fake)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(docs) != 1 || docs[0].Fields["EmployeeID"] != "E-1" {
			t.Fatalf("docs = %+v", docs)
		}
		if fake.source != DefaultSource || fake.limit != 10 {
			t.Fatalf("store got source=%q limit=%d", fake.source, fake.limit)
		}
		if len(reg.Quarantine()) != 1 {
			t.Fatalf("ledger = %v", reg.Quarantine())
		}
		fake.err = errStoreDown
		if _, err := q.Execute(reg, fake); !errors.Is(err, errStoreDown) {
			t.Fatalf("store error: %v", err)
		}
	})
}

// fakeQuerier captures calls and replays canned rows.
type fakeQuerier struct {
	rows      []string
	source    string
	predicate string
	limit     int
	err       error
}

var errStoreDown = errors.New("store down")

func (f *fakeQuerier) Query(source string, predicate json.RawMessage, limit int) ([]json.RawMessage, error) {
	f.source = source
	f.predicate = string(predicate)
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	out := make([]json.RawMessage, len(f.rows))
	for i, r := range f.rows {
		out[i] = json.RawMessage(r)
	}
	return out, nil
}
