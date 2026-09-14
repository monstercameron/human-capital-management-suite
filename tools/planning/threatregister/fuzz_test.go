package threatregister

import (
	"testing"
	"time"
)

var fixedFuzzNow = time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

// FuzzTodo_THREAT_001 fuzzes [Register.Validate]'s input surface: the free-
// text/enum-ish fields of one threat inside an otherwise-valid fixture
// (attack class, severity, owner, a consuming edge id, a mitigation id, a
// test kind and name). It asserts the one property Validate must hold for
// *any* input: it never panics, no matter how the fuzzer mangles those
// fields, and it always returns a []Violation slice a caller can range
// over safely. It deliberately does not assert which exact violations fire
// for a given input (that is property_test.go and mutation_test.go's job
// with concrete, named cases); a fuzz target proves the validation surface
// is total and crash-free, not that any one rule fires correctly.
func FuzzTodo_THREAT_001(f *testing.F) {
	seeds := []struct {
		attackClass, severity, owner, edge, mitigation, testKind, testName string
	}{
		{"CONFUSED_DEPUTY", "HIGH", "PLATFORM_TRUST", "E1", "MIT-SHARED", "NEGATIVE", "fixture test"},
		{"", "HIGH", "PLATFORM_TRUST", "E1", "MIT-SHARED", "NEGATIVE", "fixture test"},
		{"CONFUSED_DEPUTY", "", "PLATFORM_TRUST", "E1", "MIT-SHARED", "NEGATIVE", "fixture test"},
		{"CONFUSED_DEPUTY", "HIGH", "", "E1", "MIT-SHARED", "NEGATIVE", "fixture test"},
		{"CONFUSED_DEPUTY", "HIGH", "PLATFORM_TRUST", "", "MIT-SHARED", "NEGATIVE", "fixture test"},
		{"CONFUSED_DEPUTY", "HIGH", "PLATFORM_TRUST", "E1", "", "NEGATIVE", "fixture test"},
		{"CONFUSED_DEPUTY", "HIGH", "PLATFORM_TRUST", "E1", "MIT-SHARED", "", "fixture test"},
		{"CONFUSED_DEPUTY", "HIGH", "PLATFORM_TRUST", "E1", "MIT-SHARED", "NEGATIVE", ""},
		{"MADE_UP_CLASS", "MADE_UP_SEVERITY", "owner\x00", "NOT-AN-EDGE", "NOT-A-MITIGATION", "MADE_UP_KIND", "name\nwith\nnewlines"},
		{"CONFUSED_DEPUTY|TENANT_CROSSOVER", "HIGH", "owner", "E1", "MIT-SHARED", "NEGATIVE", "a very long test name " + string(make([]byte, 200))},
	}
	for _, s := range seeds {
		f.Add(s.attackClass, s.severity, s.owner, s.edge, s.mitigation, s.testKind, s.testName)
	}

	f.Fuzz(func(t *testing.T, attackClass, severity, owner, edge, mitigation, testKind, testName string) {
		r := deepCopy(validFixture())
		th := &r.Slices[0].Threats[0]
		th.AttackClass = attackClass
		th.Severity = severity
		th.Owner = owner
		th.ConsumingEdges = []string{edge}
		th.Mitigations = []string{mitigation}
		th.Tests = []ThreatTest{{Kind: testKind, Name: testName}}

		violations := r.Validate()
		_ = violations // must not panic; content is not asserted here.

		blocked, blockers := r.ReleaseDecision(fixedFuzzNow)
		_ = blocked
		_ = blockers // must not panic either, for any Severity string.
	})
}
