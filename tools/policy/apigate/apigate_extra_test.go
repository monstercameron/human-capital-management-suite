package apigate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/compatibility"
)

func TestAPIGate_ReportAndRegisterValidation(t *testing.T) {
	if (Report{Decision: OutcomeCompatible}).OK() != true || (Report{Decision: OutcomeBlock}).OK() {
		t.Fatal("Report.OK did not honor decision")
	}
	violations := []struct {
		name   string
		mutate func(*Register)
		want   string
	}{
		{"zero version", func(r *Register) { r.Version = 0 }, "version must be positive"},
		{"empty consumers", func(r *Register) { r.Consumers = nil }, "register is empty"},
		{"missing identity", func(r *Register) { r.Consumers[0].ID = "" }, "requires id"},
		{"missing evidence", func(r *Register) { r.Consumers[0].AdoptedVersion = 0 }, "adopted_version"},
		{"duplicate id", func(r *Register) { r.Consumers = append(r.Consumers, r.Consumers[0]) }, "registered more than once"},
		{"no methods", func(r *Register) { r.Consumers[0].Methods = nil }, "no service methods"},
		{"empty method", func(r *Register) { r.Consumers[0].Methods = []string{" "} }, "empty method"},
		{"no fields", func(r *Register) { r.Consumers[0].Fields = nil }, "no message fields"},
		{"incomplete field", func(r *Register) { r.Consumers[0].Fields = []FieldDependency{{Message: "", Field: "revision"}} }, "incomplete field"},
	}
	for _, tc := range violations {
		t.Run(tc.name, func(t *testing.T) {
			r := testRegister()
			tc.mutate(&r)
			if err := Validate(r); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate error = %v, want text %q", err, tc.want)
			}
		})
	}
	canonical, err := CanonicalJSON(testRegister())
	if err != nil || !bytes.Contains(canonical, []byte("workflow-ui")) {
		t.Fatalf("CanonicalJSON = %q, err=%v", canonical, err)
	}
	if _, err := CanonicalJSON(Register{}); err == nil {
		t.Fatal("CanonicalJSON accepted an invalid register")
	}
	if _, err := Digest(Register{}); err == nil {
		t.Fatal("Digest accepted an invalid register")
	}
}

func TestAPIGate_ConsumerFindingsAndCanonicalIdentity(t *testing.T) {
	r := testRegister()
	r.Consumers = append(r.Consumers, Consumer{ID: "api", Owner: "team", Service: "svc.v1", AdoptedVersion: 1, Watermark: "w", Sunset: "s", Methods: []string{"Run"}, Fields: []FieldDependency{{Message: "Thing", Field: "id"}}})
	findings := ConsumerFindings(r, []compatibility.BufBreakingViolation{
		{Path: "svc.v1.Run", Message: "removed"},
		{Path: "Thing.id", Message: "changed"},
		{Path: "unrelated", Message: "unchanged"},
	})
	if len(findings) != 2 || findings[0].ConsumerID != "api" || findings[1].ConsumerID != "api" {
		t.Fatalf("ConsumerFindings = %+v", findings)
	}
	other := testRegister()
	other.Consumers[0].Methods = []string{"GetIntent", "ExplainIntent"}
	other.Consumers[0].Fields = []FieldDependency{{Message: "hcmnext.intents.v1.IntentDefinition", Field: "revision"}}
	digestA, err := Digest(testRegister())
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := Digest(other)
	if err != nil || digestA != digestB {
		t.Fatalf("Digest changed with ordering only: %q, %q, err=%v", digestA, digestB, err)
	}
	canonical, err := CanonicalJSON(testRegister())
	if err != nil || !EqualCanonical(canonical, testRegister()) || EqualCanonical([]byte("not canonical"), testRegister()) {
		t.Fatalf("EqualCanonical identity checks failed: err=%v", err)
	}
}

func TestAPIGate_LoadPathsAndRunErrors(t *testing.T) {
	dir := t.TempDir()
	registerPath := filepath.Join(dir, "register.json")
	data, err := CanonicalJSON(testRegister())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registerPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegister(registerPath)
	if err != nil || loaded.Version != 1 || len(loaded.Consumers) != 1 {
		t.Fatalf("LoadRegister = %+v, err=%v", loaded, err)
	}
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("not: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegister(badPath); err == nil || !strings.Contains(err.Error(), "parse consumer register") {
		t.Fatalf("malformed LoadRegister error = %v", err)
	}
	if _, err := LoadRegister(filepath.Join(dir, "missing.json")); err == nil || !strings.Contains(err.Error(), "read consumer register") {
		t.Fatalf("missing LoadRegister error = %v", err)
	}
	options := Options{SchemaModule: "schema", Baseline: "baseline", RegisterPath: registerPath}.withRoot("C:\\root")
	if options.SchemaModule != filepath.Join("C:\\root", "schema") || options.Baseline != filepath.Join("C:\\root", "baseline") || options.RegisterPath != registerPath {
		t.Fatalf("withRoot relative/absolute paths = %+v", options)
	}
	gotSchema, gotBaseline, gotRegister := DefaultPaths("C:\\root")
	if gotSchema != filepath.Join("C:\\root", defaultSchemaModule) || gotBaseline != filepath.Join("C:\\root", defaultBaseline) || gotRegister != filepath.Join("C:\\root", defaultRegister) {
		t.Fatalf("DefaultPaths = %q, %q, %q", gotSchema, gotBaseline, gotRegister)
	}
	if _, err := Run(dir, Options{RegisterPath: filepath.Join(dir, "missing.json")}); err == nil {
		t.Fatal("Run accepted a missing register")
	}
	if _, err := Run(dir, Options{RegisterPath: registerPath, BufBinary: filepath.Join(dir, "missing-buf")}); err == nil {
		t.Fatal("Run accepted a missing buf executable")
	}
	if _, err := Evaluate(dir, Options{RegisterPath: registerPath, BufBinary: filepath.Join(dir, "missing-buf")}); err == nil {
		t.Fatal("Evaluate accepted a missing buf executable")
	}
}
