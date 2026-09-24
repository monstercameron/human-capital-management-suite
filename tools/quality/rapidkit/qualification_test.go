package rapidkit_test

import (
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This package is intentionally test-only. It is the standard-library
// replacement used by the LIB-016 REJECT decision; keeping the owned model,
// generator, and shrinker in _test.go makes it impossible for runtime code to
// acquire a dependency on the qualification fixture by accident.

type operationKind uint8

const (
	opDeposit operationKind = iota
	opWithdraw
	opTransition
	opInterval
)

type operation struct {
	kind       operationKind
	amount     int
	from, to   string
	start, end int
}

type defect string

const (
	defectBalanceUnderflow  defect = "balance_underflow"
	defectIllegalTransition defect = "illegal_workflow_transition"
	defectTemporalOverlap   defect = "temporal_overlap"
)

var allDefects = []defect{defectBalanceUnderflow, defectIllegalTransition, defectTemporalOverlap}

// seededFailure is deliberately noisy. The shrinker must remove irrelevant
// operations as well as simplify values, while preserving the named owned
// invariant failure.
func seededFailure(d defect) []operation {
	switch d {
	case defectBalanceUnderflow:
		return []operation{
			{kind: opDeposit, amount: 8},
			{kind: opWithdraw, amount: 3},
			{kind: opDeposit, amount: 2},
			{kind: opWithdraw, amount: 9},
			{kind: opDeposit, amount: 4},
		}
	case defectIllegalTransition:
		return []operation{
			{kind: opTransition, from: "draft", to: "review"},
			{kind: opDeposit, amount: 2},
			{kind: opTransition, from: "review", to: "approved"},
			{kind: opTransition, from: "approved", to: "draft"},
		}
	case defectTemporalOverlap:
		return []operation{
			{kind: opInterval, start: 0, end: 20},
			{kind: opDeposit, amount: 3},
			{kind: opInterval, start: 7, end: 13},
			{kind: opWithdraw, amount: 1},
		}
	default:
		panic("unknown seeded defect")
	}
}

// findDefect is the owned state-machine oracle. It intentionally models only
// semantic invariants owned by Human Capital Management Suite: balances cannot underflow, workflow
// transitions follow the lifecycle, and reservations cannot overlap.
func findDefect(ops []operation, wanted defect) bool {
	balance := 0
	state := "draft"
	var intervals [][2]int
	for _, op := range ops {
		switch op.kind {
		case opDeposit:
			balance += op.amount
		case opWithdraw:
			balance -= op.amount
			if wanted == defectBalanceUnderflow && balance < 0 {
				return true
			}
		case opTransition:
			valid := (state == "draft" && op.from == "draft" && op.to == "review") ||
				(state == "review" && op.from == "review" && op.to == "approved")
			if wanted == defectIllegalTransition && !valid {
				return true
			}
			if valid {
				state = op.to
			}
		case opInterval:
			if op.end <= op.start {
				continue
			}
			for _, prior := range intervals {
				if wanted == defectTemporalOverlap && op.start < prior[1] && prior[0] < op.end {
					return true
				}
			}
			intervals = append(intervals, [2]int{op.start, op.end})
		}
	}
	return false
}

// shrinkFailure mirrors the useful part of a property-testing shrinker using
// only deterministic standard-library operations. Candidate order is stable:
// remove operations left-to-right, then simplify each scalar toward zero.
func shrinkFailure(input []operation, fails func([]operation) bool) []operation {
	got := append([]operation(nil), input...)
	for {
		changed := false
		for i := 0; i < len(got); i++ {
			candidate := append([]operation(nil), got[:i]...)
			candidate = append(candidate, got[i+1:]...)
			if fails(candidate) {
				got = candidate
				changed = true
				break
			}
		}
		if changed {
			continue
		}
		for i := range got {
			for _, candidate := range simplifyOperation(got, i) {
				if fails(candidate) {
					got = candidate
					changed = true
					break
				}
			}
			if changed {
				break
			}
		}
		if !changed {
			return got
		}
	}
}

func simplifyOperation(ops []operation, index int) [][]operation {
	base := ops[index]
	values := make([][]operation, 0, 8)
	add := func(updated operation) {
		candidate := append([]operation(nil), ops...)
		candidate[index] = updated
		if reflect.DeepEqual(candidate, ops) {
			return
		}
		values = append(values, candidate)
	}
	if base.amount != 0 {
		updated := base
		updated.amount = 0
		add(updated)
		updated.amount = 1
		add(updated)
		if base.amount > 1 {
			updated.amount = base.amount / 2
			add(updated)
		}
	}
	if base.start != 0 {
		updated := base
		updated.start = 0
		add(updated)
	}
	if base.end != 0 {
		updated := base
		updated.end = 0
		add(updated)
		updated.end = 1
		add(updated)
	}
	return values
}

func assertStableMinimal(t *testing.T, input []operation, d defect) []operation {
	t.Helper()
	fails := func(ops []operation) bool { return findDefect(ops, d) }
	if !fails(input) {
		t.Fatalf("seed does not reproduce %s", d)
	}
	first := shrinkFailure(input, fails)
	second := shrinkFailure(input, fails)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("shrink for %s is not reproducible: first=%#v second=%#v", d, first, second)
	}
	if len(first) >= len(input) {
		t.Fatalf("shrink for %s made no progress: input=%#v output=%#v", d, input, first)
	}
	for i := range first {
		candidate := append([]operation(nil), first[:i]...)
		candidate = append(candidate, first[i+1:]...)
		if fails(candidate) {
			t.Fatalf("%s output is not deletion-minimal: removing operation %d gives %#v", d, i, candidate)
		}
	}
	return first
}

// TestRapidQualificationShrinksOwnedStateMachineFailure is LIB-016's primary
// test. It is intentionally backend-neutral: the same owned seeds and
// invariant oracle are the contract a future Rapid adapter must satisfy.
func TestRapidQualificationShrinksOwnedStateMachineFailure(t *testing.T) {
	for _, d := range allDefects {
		t.Run(string(d), func(t *testing.T) {
			shrunk := assertStableMinimal(t, seededFailure(d), d)
			if len(shrunk) == 0 {
				t.Fatal("a reproducible failure must retain at least one operation")
			}
		})
	}
}

func generatedSequence(rng *rand.Rand, length int) []operation {
	ops := make([]operation, length)
	for i := range ops {
		switch rng.Intn(4) {
		case 0:
			ops[i] = operation{kind: opDeposit, amount: rng.Intn(9)}
		case 1:
			ops[i] = operation{kind: opWithdraw, amount: rng.Intn(9)}
		case 2:
			states := []string{"draft", "review", "approved", "closed"}
			ops[i] = operation{kind: opTransition, from: states[rng.Intn(len(states))], to: states[rng.Intn(len(states))]}
		case 3:
			start := rng.Intn(16)
			ops[i] = operation{kind: opInterval, start: start, end: start + rng.Intn(8)}
		}
	}
	return ops
}

// TestTodo_LIB_016_Property checks fixed-seed generation and the complete
// shrink contract over generated noisy cases, while keeping all generated
// state in this test package.
func TestTodo_LIB_016_Property(t *testing.T) {
	const seed int64 = 20260903
	left := rand.New(rand.NewSource(seed))
	right := rand.New(rand.NewSource(seed))
	for i := 0; i < 64; i++ {
		leftLength := 3 + left.Intn(10)
		rightLength := 3 + right.Intn(10)
		got := generatedSequence(left, leftLength)
		want := generatedSequence(right, rightLength)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("fixed seed generation diverged at case %d: got=%#v want=%#v", i, got, want)
		}
		for _, d := range allDefects {
			input := append(append([]operation(nil), seededFailure(d)...), got...)
			assertStableMinimal(t, input, d)
		}
	}
}

type qualificationManifest struct {
	Version                         int    `yaml:"version"`
	Todo                            string `yaml:"todo"`
	Tool                            string `yaml:"tool"`
	Role                            string `yaml:"role"`
	Verdict                         string `yaml:"verdict"`
	RuntimeDependencyGraphUnchanged bool   `yaml:"runtime_dependency_graph_unchanged"`
	Command                         string `yaml:"command"`
	Candidate                       struct {
		Source                string `yaml:"source"`
		License               string `yaml:"license"`
		LatestReviewedRelease string `yaml:"latest_reviewed_release"`
		MaintenanceEvidence   string `yaml:"maintenance_evidence"`
		ReplacementPath       string `yaml:"replacement_path"`
	} `yaml:"candidate"`
	Alternative struct {
		Name           string   `yaml:"name"`
		Packages       []string `yaml:"packages"`
		Implementation string   `yaml:"implementation"`
		Contract       string   `yaml:"contract"`
	} `yaml:"alternative"`
	Scope struct {
		Covers   []string `yaml:"covers"`
		Excludes []string `yaml:"excludes"`
	} `yaml:"scope"`
	RegressionSeeds []string `yaml:"regression_seeds"`
	Evidence        []struct {
		Test    string `yaml:"test"`
		Package string `yaml:"package"`
	} `yaml:"evidence"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}

func loadManifest(t *testing.T) qualificationManifest {
	t.Helper()
	path := filepath.Join(repoRoot(t), "definitions", "architecture", "rapid-qualification.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read qualification manifest: %v", err)
	}
	var manifest qualificationManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse qualification manifest: %v", err)
	}
	return manifest
}

func rapidImportFiles(root string) ([]string, error) {
	var rapidImports []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imported := range parsed.Imports {
			if strings.Trim(imported.Path.Value, `"`) == "pgregory.net/rapid" {
				rapidImports = append(rapidImports, path)
			}
		}
		return nil
	})
	return rapidImports, err
}

// TestTodo_LIB_016_Golden pins the decision and the three regression-seed
// identities. This prevents an accidental switch to ADOPT without rerunning
// the qualification fixture and reviewing dependency admission.
func TestTodo_LIB_016_Golden(t *testing.T) {
	m := loadManifest(t)
	if m.Version != 1 || m.Todo != "LIB-016" || m.Tool != "pgregory.net/rapid" {
		t.Fatalf("manifest identity = version %d, todo %q, tool %q", m.Version, m.Todo, m.Tool)
	}
	if m.Role != "test-only" || m.Verdict != "REJECT" {
		t.Fatalf("decision = role %q verdict %q, want test-only/REJECT", m.Role, m.Verdict)
	}
	if !m.RuntimeDependencyGraphUnchanged {
		t.Fatal("REJECT must record unchanged runtime dependency graph")
	}
	want := []string{"balance_underflow", "illegal_workflow_transition", "temporal_overlap"}
	if !reflect.DeepEqual(m.RegressionSeeds, want) {
		t.Fatalf("regression seeds = %v, want %v", m.RegressionSeeds, want)
	}
	if m.Candidate.Source == "" || m.Candidate.License == "" || m.Candidate.MaintenanceEvidence == "" || m.Candidate.ReplacementPath == "" {
		t.Fatal("candidate must retain source, license, maintenance, and replacement evidence")
	}
	if m.Alternative.Name != "Go standard library" || m.Alternative.Implementation != "tools/quality/rapidkit" || len(m.Alternative.Packages) < 2 {
		t.Fatalf("incomplete standard-library alternative: %+v", m.Alternative)
	}
}

// FuzzTodo_LIB_016 validates that arbitrary encoded operation sequences never
// panic and that any discovered seeded failure remains shrinkable and
// reproducible. Seed corpus entries are persisted in the test itself because
// Go fuzzing records the byte input as the regression seed.
func FuzzTodo_LIB_016(f *testing.F) {
	for _, seed := range [][]byte{
		{byte(opWithdraw), 3},
		{byte(opTransition), 0, 3},
		{byte(opInterval), 0, 20, 7, 13},
		{0, 1, 2, 3, 4, 5, 6, 7},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ops := decodeOperations(data)
		for _, d := range allDefects {
			if findDefect(ops, d) {
				fails := func(candidate []operation) bool { return findDefect(candidate, d) }
				shrunk := shrinkFailure(ops, fails)
				if !fails(shrunk) {
					t.Fatalf("shrinking %s lost the failure", d)
				}
				if again := shrinkFailure(ops, fails); !reflect.DeepEqual(shrunk, again) {
					t.Fatalf("shrinking %s is not reproducible: %#v != %#v", d, shrunk, again)
				}
			}
		}
	})
}

func decodeOperations(data []byte) []operation {
	ops := make([]operation, 0, len(data)/2)
	for i := 0; i < len(data); {
		kind := operationKind(data[i] % 4)
		i++
		switch kind {
		case opDeposit, opWithdraw:
			amount := 0
			if i < len(data) {
				amount = int(data[i] % 16)
				i++
			}
			ops = append(ops, operation{kind: kind, amount: amount})
		case opTransition:
			from, to := "draft", "draft"
			if i < len(data) {
				from = []string{"draft", "review", "approved", "closed"}[int(data[i])%4]
				i++
			}
			if i < len(data) {
				to = []string{"draft", "review", "approved", "closed"}[int(data[i])%4]
				i++
			}
			ops = append(ops, operation{kind: kind, from: from, to: to})
		case opInterval:
			start, end := 0, 0
			if i < len(data) {
				start = int(data[i] % 16)
				i++
			}
			if i < len(data) {
				end = start + int(data[i]%16)
				i++
			}
			ops = append(ops, operation{kind: kind, start: start, end: end})
		}
	}
	return ops
}

// TestTodo_LIB_016_Conformance verifies the machine-readable decision, test
// evidence, and graph boundary. It also ensures Rapid appears only in this
// test/manifest documentation and never in a non-test Go source file.
func TestTodo_LIB_016_Conformance(t *testing.T) {
	root := repoRoot(t)
	m := loadManifest(t)
	wantTests := map[string]bool{
		"TestRapidQualificationShrinksOwnedStateMachineFailure": false,
		"TestTodo_LIB_016_Property":                             false,
		"TestTodo_LIB_016_Golden":                               false,
		"FuzzTodo_LIB_016":                                      false,
		"TestTodo_LIB_016_Conformance":                          false,
	}
	for _, ev := range m.Evidence {
		if _, ok := wantTests[ev.Test]; !ok {
			t.Errorf("unexpected evidence test %q", ev.Test)
			continue
		}
		if wantTests[ev.Test] {
			t.Errorf("duplicate evidence test %q", ev.Test)
		}
		wantTests[ev.Test] = true
		if ev.Package != "tools/quality/rapidkit" {
			t.Errorf("%s evidence package = %q", ev.Test, ev.Package)
		}
	}
	for name, found := range wantTests {
		if !found {
			t.Errorf("evidence missing %q", name)
		}
	}
	if m.Command != "go test -count=1 ./tools/quality/rapidkit" {
		t.Errorf("command = %q", m.Command)
	}

	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := strings.ToLower(string(data))
		if strings.Contains(text, "pgregory.net/rapid") || strings.Contains(text, "flyingmutant/rapid") {
			t.Fatalf("%s admits Rapid despite REJECT decision", name)
		}
	}
	rapidImports, err := rapidImportFiles(root)
	if err != nil {
		t.Fatalf("scan Go graph: %v", err)
	}
	for _, path := range rapidImports {
		if !strings.HasSuffix(path, "_test.go") {
			t.Errorf("Rapid mention in non-test Go source %s", path)
		}
	}
	if len(rapidImports) != 0 {
		sort.Strings(rapidImports)
		t.Fatalf("Rapid import found in %s", strings.Join(rapidImports, ", "))
	}

	for _, d := range allDefects {
		assertStableMinimal(t, seededFailure(d), d)
	}
}

func TestRapidImportScanIgnoresArtifactNULFixtures(t *testing.T) {
	root := t.TempDir()
	eligible := filepath.Join(root, "source.go")
	if err := os.WriteFile(eligible, []byte("package fixture\nimport _ \"pgregory.net/rapid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nulFixture := filepath.Join(root, ".artifacts", "backup", "nul-source-20260924", "internal", "application", "siem_http.go")
	if err := os.MkdirAll(filepath.Dir(nulFixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nulFixture, []byte("package broken\n\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := rapidImportFiles(root)
	if err != nil {
		t.Fatalf("scan Go graph with isolated NUL fixture: %v", err)
	}
	if len(got) != 1 || got[0] != eligible {
		t.Fatalf("Rapid imports = %v, want only eligible source %s", got, eligible)
	}
}
