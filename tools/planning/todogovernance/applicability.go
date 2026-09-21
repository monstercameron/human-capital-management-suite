package todogovernance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	CodeMissingApplicableTestClass = "MISSING_APPLICABLE_TEST_CLASS"
	CodeMeaninglessTestClass       = "MEANINGLESS_TEST_CLASS"
	CodeDuplicateMatrixTestName    = "DUPLICATE_MATRIX_TEST_NAME"
	CodeUnitOnlyWithoutReason      = "UNIT_ONLY_WITHOUT_REASON"
)

// TestClasses is the reviewed vocabulary from the secondary test taxonomy in
// planning/todos.md. PRIMARY is deliberately absent: it is the primary test,
// not a secondary applicability class.
var TestClasses = []string{
	"ACCESSIBILITY", "ARCHITECTURE", "BENCHMARK", "BROWSER", "CONFORMANCE", "FAULT", "FUZZ",
	"GOLDEN", "I18N", "INTEGRATION", "MODEL_BASED", "MUTATION", "PERFORMANCE",
	"PROPERTY", "RACE", "RECOVERY", "REGRESSION", "SECURITY",
}

// RiskRule is one row in the GOV-018 derivation table. Marker is a stable
// description of the input predicate; Classes are the classes made required
// when that predicate matches.
type RiskRule struct {
	ID      string
	Marker  string
	Classes []string
}

var riskDerivationTable = []RiskRule{
	{ID: "phase:P0", Marker: "phase is P0", Classes: []string{"GOLDEN"}},
	{ID: "phase:GATE_A", Marker: "phase is GATE_A", Classes: []string{"GOLDEN"}},
	{ID: "tag:SECURITY", Marker: "SECURITY tag in title or refs", Classes: []string{"SECURITY"}},
	{ID: "tag:CONFORMANCE", Marker: "CONFORMANCE tag in title or refs", Classes: []string{"CONFORMANCE"}},
	{ID: "tag:PROPERTY", Marker: "PROPERTY tag in title or refs", Classes: []string{"PROPERTY"}},
	{ID: "tag:FUZZ", Marker: "FUZZ tag in title or refs", Classes: []string{"FUZZ"}},
	{ID: "tag:RACE", Marker: "RACE tag in title or refs", Classes: []string{"RACE"}},
	{ID: "tag:FAULT", Marker: "FAULT tag in title or refs", Classes: []string{"FAULT"}},
	{ID: "tag:BROWSER", Marker: "BROWSER tag in title or refs", Classes: []string{"BROWSER"}},
	{ID: "tag:RECOVERY", Marker: "RECOVERY tag in title or refs", Classes: []string{"RECOVERY"}},
	{ID: "tag:BENCHMARK", Marker: "BENCHMARK tag in title or refs", Classes: []string{"BENCHMARK"}},
	{ID: "tag:MUTATION", Marker: "MUTATION tag in title or refs", Classes: []string{"MUTATION"}},
	{ID: "tag:MODEL_BASED", Marker: "MODEL_BASED tag in title or refs", Classes: []string{"MODEL_BASED"}},
	{ID: "tag:INTEGRATION", Marker: "INTEGRATION tag in title or refs", Classes: []string{"INTEGRATION"}},
	{ID: "role:GOVERNANCE", Marker: "INTENT CONTEXT ROLE is GOVERNANCE", Classes: []string{"GOLDEN"}},
	{ID: "role:CONFORMANCE", Marker: "INTENT CONTEXT ROLE is CONFORMANCE", Classes: []string{"CONFORMANCE"}},
	{ID: "vocabulary:migration", Marker: "migration, backfill, cutover or rollback vocabulary", Classes: []string{"FAULT", "RECOVERY"}},
	{ID: "vocabulary:trust", Marker: "trust, identity, authorization, secret, privacy or DLP vocabulary", Classes: []string{"SECURITY"}},
	{ID: "vocabulary:transport", Marker: "transport, adapter, connector, provider, HTTP or gRPC vocabulary", Classes: []string{"INTEGRATION"}},
}

var unitOnlyReasonRe = regexp.MustCompile(`(?i)(?:^|[;` + "`" + `])UNIT_ONLY\(reason=([^)` + "`" + `;]+)\)`)

// RiskDerivationTable returns a copy of the reviewed GOV-018 table. The
// returned slices are also copied so callers cannot mutate policy globally.
func RiskDerivationTable() []RiskRule {
	out := make([]RiskRule, len(riskDerivationTable))
	for i, rule := range riskDerivationTable {
		out[i] = rule
		out[i].Classes = append([]string(nil), rule.Classes...)
	}
	return out
}

// TestClassApplicability is the deterministic result of applying the
// GOV-018 table to one todo. Reasons records the matching rule IDs per class.
type TestClassApplicability struct {
	Required      []string
	NotApplicable []string
	Reasons       map[string][]string
}

// DeriveTestClassApplicability derives required test classes from phase,
// title, refs and INTENT CONTEXT. It is pure and does not inspect the current
// matrix, preventing a declared but meaningless class from making itself
// applicable.
func DeriveTestClassApplicability(record Record) TestClassApplicability {
	text := strings.ToLower(record.Todo.Title + " " + record.Todo.Refs)
	markers := text + " " + strings.ToLower(record.IntentContextRaw)
	app := TestClassApplicability{Reasons: map[string][]string{}}
	for _, rule := range riskDerivationTable {
		matched := false
		switch rule.ID {
		case "phase:P0":
			matched = record.Todo.Phase == "P0"
		case "phase:GATE_A":
			matched = record.Todo.Phase == "GATE_A"
		case "tag:SECURITY":
			matched = hasRiskTag(markers, "security")
		case "tag:CONFORMANCE":
			matched = hasRiskTag(markers, "conformance")
		case "role:GOVERNANCE":
			matched = record.IntentContext["ROLE"] == "GOVERNANCE"
		case "role:CONFORMANCE":
			matched = record.IntentContext["ROLE"] == "CONFORMANCE"
		case "vocabulary:migration":
			matched = containsAny(markers, "migration", "migrate", "backfill", "cutover", "rollback")
		case "vocabulary:trust":
			matched = containsAny(markers, "trust", "identity", "authorization", "authz", "secret", "privacy", "dlp")
		case "vocabulary:transport":
			matched = containsAny(markers, "transport", "adapter", "connector", "provider", "http", "grpc")
		case "tag:PROPERTY", "tag:FUZZ", "tag:RACE", "tag:FAULT", "tag:BROWSER", "tag:RECOVERY", "tag:BENCHMARK", "tag:MUTATION", "tag:MODEL_BASED", "tag:INTEGRATION":
			matched = hasRiskTag(markers, strings.TrimPrefix(rule.ID, "tag:"))
		}
		if !matched {
			continue
		}
		for _, class := range rule.Classes {
			if !contains(app.Required, class) {
				app.Required = append(app.Required, class)
			}
			app.Reasons[class] = append(app.Reasons[class], rule.ID)
		}
	}
	if len(app.Required) == 0 {
		app.NotApplicable = append(app.NotApplicable, TestClasses...)
	}
	sort.Strings(app.Required)
	sort.Strings(app.NotApplicable)
	for class := range app.Reasons {
		sort.Strings(app.Reasons[class])
	}
	return app
}

func hasRiskTag(text, tag string) bool {
	return strings.Contains(text, "["+tag+"]") || strings.Contains(text, "`"+tag+"`")
}

func containsAny(text string, words ...string) bool {
	for _, word := range words {
		if containsWord(text, word) {
			return true
		}
	}
	return false
}

func containsWord(text, word string) bool {
	if strings.Contains(word, " ") || strings.Contains(word, "-") {
		return strings.Contains(text, word)
	}
	pattern := `(^|[^a-z0-9])` + regexp.QuoteMeta(word) + `[a-z0-9-]*([^a-z0-9]|$)`
	return regexp.MustCompile(pattern).MatchString(text)
}

// ValidateTestMatrixApplicability enforces that all derived classes are
// declared, no unsupported class is smuggled into a matrix, matrix test names
// are unique, and UNIT_ONLY carries a reviewed applicability reason.
func ValidateTestMatrixApplicability(records []Record) []Finding {
	known := make(map[string]bool, len(TestClasses))
	for _, class := range TestClasses {
		known[class] = true
	}
	var findings []Finding
	owners := map[string][]string{}
	for _, record := range records {
		t := record.Todo
		app := DeriveTestClassApplicability(record)
		declared := map[string]string{}
		unitOnly := strings.Contains(strings.ToUpper(record.TestMatrixRaw), "UNIT_ONLY")
		for class, name := range t.TestMatrix {
			class = strings.ToUpper(strings.TrimSpace(class))
			if class == "PRIMARY" {
				continue
			}
			if strings.HasPrefix(class, "UNIT_ONLY") {
				unitOnly = true
				continue
			}
			declared[class] = name
			owners[name] = append(owners[name], t.ID)
			if !known[class] {
				findings = append(findings, Finding{TodoID: t.ID, Rule: "GOV-018", Code: CodeMeaninglessTestClass, Line: t.Line, Detail: class, Reason: fmt.Sprintf("TEST MATRIX class %q is not in the declared taxonomy", class)})
				continue
			}
		}
		for _, class := range app.Required {
			if _, ok := declared[class]; !ok {
				findings = append(findings, Finding{TodoID: t.ID, Rule: "GOV-018", Code: CodeMissingApplicableTestClass, Line: t.Line, Detail: class, Reason: fmt.Sprintf("risk-derived class %q is not declared; rules: %s", class, strings.Join(app.Reasons[class], ", "))})
			}
		}
		if unitOnly {
			if len(unitOnlyReasonRe.FindStringSubmatch(record.TestMatrixRaw)) != 2 {
				findings = append(findings, Finding{TodoID: t.ID, Rule: "GOV-018", Code: CodeUnitOnlyWithoutReason, Line: t.Line, Reason: "UNIT_ONLY must include a non-empty evaluated reason"})
			}
		}
	}
	for name, ids := range owners {
		if name == "" || len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		for _, id := range ids {
			findings = append(findings, Finding{TodoID: id, Rule: "GOV-018", Code: CodeDuplicateMatrixTestName, Detail: name, Reason: fmt.Sprintf("matrix test name %q is also declared by %s", name, strings.Join(without(ids, id), ", "))})
		}
	}
	sortFindings(findings)
	return findings
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// Ensure the compiler catches accidental drift between the table and the
// published taxonomy while keeping the table itself the golden-pinned source.
func init() {
	for _, rule := range riskDerivationTable {
		for _, class := range rule.Classes {
			if !contains(TestClasses, class) {
				panic("GOV-018 risk table names undeclared test class: " + class)
			}
		}
	}
}
