package apigate

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/compatibility"
)

func testRegister() Register {
	return Register{Version: 1, Disposition: Disposition{
		Specification: "planning/specs/platform-responsibility-boundaries.md",
		Owner:         "platform-engineering",
		Class:         "library-tooling",
		Status:        "ACTIVE_REGISTER",
	}, Consumers: []Consumer{{
		ID: "workflow-ui", Owner: "experience", Service: "hcmnext.intents.v1.IntentService", AdoptedVersion: 1,
		Watermark: "schema-baseline-2026-09-05", Sunset: "2027-09-05",
		Methods: []string{"ExplainIntent", "GetIntent"},
		Fields:  []FieldDependency{{Message: "hcmnext.intents.v1.IntentDefinition", Field: "revision"}},
	}}}
}

func TestTodo_PROTO_008(t *testing.T) {
	register := testRegister()
	if err := Validate(register); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	violations := []compatibility.BufBreakingViolation{{
		Path: "hcmnext.intents.v1.IntentDefinition.revision", Message: "field type changed",
	}}
	findings := ConsumerFindings(register, violations)
	if len(findings) != 1 || findings[0].ConsumerID != "workflow-ui" {
		t.Fatalf("ConsumerFindings = %+v, want workflow-ui field impact", findings)
	}
	if (Report{Decision: OutcomeBlock, Compatibility: compatibility.BufBreakingReport{Violations: violations}, ConsumerFindings: findings}).OK() {
		t.Fatal("breaking consumer change was accepted")
	}
	decisions := ConsumerDecisions(register, violations)
	if len(decisions) != 1 || decisions[0].Decision != OutcomeMigrate || decisions[0].Owner != "experience" || decisions[0].Sunset == "" {
		t.Fatalf("ConsumerDecisions = %+v, want owner/sunset-backed MIGRATE", decisions)
	}
}

func TestTodo_PROTO_008_Property(t *testing.T) {
	if err := Validate(Register{Version: 1}); err == nil {
		t.Fatal("register without disposition was accepted")
	}
	if err := Validate(Register{Version: 1, Disposition: testRegister().Disposition, Consumers: []Consumer{{ID: "x", Owner: "y", Service: "z", Methods: []string{"M"}}}}); err == nil {
		t.Fatal("consumer without field dependencies was accepted")
	}
}

func TestTodo_PROTO_008_Golden(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	fixturePath := filepath.Join(filepath.Dir(file), "testdata", "consumer-register.json")
	r, err := LoadRegister(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := CanonicalJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(filepath.Dir(file), "testdata", "consumer-register.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("consumer register golden drifted:\n got %s\nwant %s", data, want)
	}
	digest, err := Digest(r)
	if err != nil || digest == "" {
		t.Fatalf("Digest = %q, %v", digest, err)
	}
	if !EqualCanonical(data, r) {
		t.Fatal("canonical register did not round-trip")
	}
}

func TestTodo_PROTO_008_Conformance(t *testing.T) {
	r := testRegister()
	violations := []compatibility.BufBreakingViolation{{Path: "other.Message.field", Message: "unrelated"}}
	if got := ConsumerFindings(r, violations); len(got) != 0 {
		t.Fatalf("unrelated compatibility finding affected consumer: %+v", got)
	}
}

func TestTodo_PROTO_008_Integration(t *testing.T) {
	r := testRegister()
	violations := []compatibility.BufBreakingViolation{{
		Path:    "hcmnext.intents.v1.IntentService.GetIntent",
		Message: "method was removed",
	}}
	findings := ConsumerFindings(r, violations)
	if len(findings) != 1 || findings[0].ConsumerID != "workflow-ui" {
		t.Fatalf("method removal findings = %+v, want workflow-ui", findings)
	}
}

func FuzzTodo_PROTO_008(f *testing.F) {
	f.Add("hcmnext.intents.v1.IntentDefinition", "revision")
	f.Fuzz(func(t *testing.T, message, field string) {
		register := testRegister()
		register.Consumers[0].Fields[0] = FieldDependency{Message: message, Field: field}
		if message == "" || field == "" {
			if err := Validate(register); err == nil {
				t.Fatal("empty field dependency was accepted")
			}
			return
		}
		if err := Validate(register); err != nil {
			t.Fatal(err)
		}
	})
}
