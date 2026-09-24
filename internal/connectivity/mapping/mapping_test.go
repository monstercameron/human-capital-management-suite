package mapping_test

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/mapping"
)

func TestTodo_REV_038_01(t *testing.T) {
	ir := mapping.IR{Version: "rev038/v1", Rules: []mapping.Rule{
		{Source: "id", Target: "a.id", Op: mapping.OpIdentity},
		{Source: "trim", Target: "b.trim", Op: mapping.OpTrim},
		{Source: "upper", Target: "c.upper", Op: mapping.OpUpper},
		{Source: "lower", Target: "d.lower", Op: mapping.OpLower},
		{Target: "e.constant", Op: mapping.OpConstant, Argument: "fixed"},
		{Source: "lookup", Target: "f.lookup", Op: mapping.OpLookup, Lookup: map[string]string{"east": "BAND_E"}},
		{Source: "date", Target: "g.date", Op: mapping.OpDate, Argument: "2006-01-02"},
		{Source: "money", Target: "h.money", Op: mapping.OpMoney, Argument: "USD"},
		{Source: "compose", Target: "i.compose", Op: mapping.OpCompose, Argument: "urn:${value}:hr"},
	}}
	in := map[string]string{"id": "WD-1", "trim": "  Ada  ", "upper": " eng ", "lower": " ADA ", "lookup": "east", "date": "2026-03-01", "money": "USD 1,234.50", "compose": "staff"}
	result, err := mapping.ExecuteShared(ir, in)
	if err != nil {
		t.Fatalf("ExecuteShared: %v", err)
	}
	got, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Frozen legacy output generated from mapping.Execute at a11035e8 before
	// the interpreter was removed. Keep the complete bytes to guard migration
	// compatibility, including payload digest and diagnostics.
	const want = `{"Fields":[{"Target":"a.id","Value":"WD-1","Deleted":false},{"Target":"b.trim","Value":"Ada","Deleted":false},{"Target":"c.upper","Value":"ENG","Deleted":false},{"Target":"d.lower","Value":"ada","Deleted":false},{"Target":"e.constant","Value":"fixed","Deleted":false},{"Target":"f.lookup","Value":"BAND_E","Deleted":false},{"Target":"g.date","Value":"2026-03-01T00:00:00Z","Deleted":false},{"Target":"h.money","Value":"1234.50","Deleted":false},{"Target":"i.compose","Value":"urn:staff:hr","Deleted":false}],"Diagnostics":[],"PayloadDigest":"sha256:c38b4ba11ca45687bf33b3065cd6fef8280b669289b8eca5b0d6cacf3c5abfe6"}`
	if string(got) != want {
		t.Fatalf("shared mapping bytes = %s, want legacy bytes %s", got, want)
	}
}

func TestTodo_CONN_RT_005(t *testing.T) {
	ir := mapping.IR{Version: "partner/v1", Rules: []mapping.Rule{
		{Source: "name", Target: "person.name", Op: mapping.OpTrim},
		{Source: "department", Target: "department.id", Op: mapping.OpLookup, Lookup: map[string]string{"eng": "ENG-1"}},
		{Source: "missing", Target: "optional", Op: mapping.OpIdentity, Null: mapping.NullOmit},
	}}
	r1, err := mapping.ExecuteShared(ir, map[string]string{"name": "  Ada  ", "department": "eng"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := mapping.ExecuteShared(ir, map[string]string{"department": "eng", "name": "  Ada  "})
	if err != nil {
		t.Fatal(err)
	}
	if r1.PayloadDigest != r2.PayloadDigest {
		t.Fatalf("digest changed with input map order: %s != %s", r1.PayloadDigest, r2.PayloadDigest)
	}
	if len(r1.Fields) != 2 || r1.Fields[0].Target != "department.id" || r1.Fields[1].Value != "Ada" {
		t.Fatalf("unexpected fields: %#v", r1.Fields)
	}
	if len(r1.Diagnostics) != 1 || r1.Diagnostics[0].Code != "source.missing" {
		t.Fatalf("unexpected diagnostics: %#v", r1.Diagnostics)
	}
}

func TestTodo_CONN_RT_005_Golden(t *testing.T) {
	ir := mapping.IR{Version: "v1", Rules: []mapping.Rule{{Source: "amount", Target: "pay", Op: mapping.OpMoney, Argument: "USD"}}}
	r, err := mapping.ExecuteShared(ir, map[string]string{"amount": "USD 1234.50"})
	if err != nil {
		t.Fatal(err)
	}
	if r.PayloadDigest != "sha256:aa66b2458ea8e711ef09c961884a86cc0dbfa5bae4d929824c401aaa28a249d4" {
		t.Fatalf("digest = %s", r.PayloadDigest)
	}
}

func TestTodo_CONN_RT_005_Red(t *testing.T) {
	_, err := mapping.ExecuteShared(mapping.IR{Version: "v1", Rules: []mapping.Rule{{Source: "x", Target: "y", Op: mapping.Op("EVAL")}}}, nil)
	if err == nil {
		t.Fatal("arbitrary operation accepted")
	}
	_, err = mapping.ExecuteShared(mapping.IR{Version: "v1", Rules: []mapping.Rule{{Source: "x", Target: "y", Op: mapping.OpMoney, Argument: "usd"}}}, map[string]string{"x": "1"})
	if err == nil {
		t.Fatal("untyped currency accepted")
	}
}
