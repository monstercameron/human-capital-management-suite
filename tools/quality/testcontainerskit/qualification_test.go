package testcontainerskit_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/testcontainerskit"
)

const qualificationFile = "testcontainers-go-qualification.yaml"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := testcontainerskit.FindRepoRoot(file)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func loadQualification(t *testing.T) testcontainerskit.Qualification {
	t.Helper()
	q, err := testcontainerskit.LoadQualification(filepath.Join(repoRoot(t), "definitions", "architecture", qualificationFile))
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func acceptedPlan() testcontainerskit.AdoptionPlan {
	digest := strings.Repeat("a", 64)
	return testcontainerskit.AdoptionPlan{
		TestOnly:      true,
		ModuleVersion: "v1.0.0",
		Images: map[string]string{
			"postgres": "postgres@sha256:" + digest,
			"s3":       "minio/minio@sha256:" + digest,
			"smtp":     "mailhog/mailhog@sha256:" + digest,
			"provider": "example/provider-fake@sha256:" + digest,
		},
		Namespace:         "tc-run-0123456789abcdef",
		ReadinessTimeout:  90 * time.Second,
		CleanupTarget:     "owned-run-namespace",
		PreserveOnFailure: true,
	}
}

// TestTestcontainersQualification is LIB-009's primary proof. The candidate
// is explicitly rejected while unavailable: no module admission, no image
// execution claim, and no route into a release graph. Its future-adoption
// contract is still checked without downloading or importing the candidate.
func TestTestcontainersQualification(t *testing.T) {
	q := loadQualification(t)
	if err := testcontainerskit.ValidateQualification(q); err != nil {
		t.Fatalf("qualification contract: %v", err)
	}
	if q.Version != 1 || q.Todo != "LIB-009" || q.Module != testcontainerskit.CandidateModule {
		t.Fatalf("qualification identity = %#v", q)
	}
	if q.Role != "test-only" || q.Verdict != "REJECT" || !q.RuntimeDependencyGraphUnchanged {
		t.Fatalf("decision = role %q verdict %q unchanged=%v", q.Role, q.Verdict, q.RuntimeDependencyGraphUnchanged)
	}
	if q.RuntimeProbe.ModuleInGoMod != "absent" || q.RuntimeProbe.ModuleInGoSum != "absent" || q.RuntimeProbe.DockerDaemon != "unavailable_at_recording" {
		t.Fatalf("runtime probe = %#v", q.RuntimeProbe)
	}
	if q.ExecutionEvidence.Status != "not_executed" || q.ExecutionEvidence.Integration != "release_dependency_graph_only" {
		t.Fatalf("execution evidence overstates integration: %#v", q.ExecutionEvidence)
	}
	for _, workload := range []string{"postgres", "s3", "smtp", "provider"} {
		if q.ExecutionEvidence.Workloads[workload] != "not_executed" {
			t.Errorf("%s execution evidence = %q, want not_executed", workload, q.ExecutionEvidence.Workloads[workload])
		}
	}
	if err := testcontainerskit.ValidateAdoptionPlan(acceptedPlan()); err != nil {
		t.Fatalf("future ADOPT gate rejects its valid baseline: %v", err)
	}

	root := repoRoot(t)
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(data), testcontainerskit.CandidateModule) {
			t.Fatalf("%s admits %s despite REJECT", name, testcontainerskit.CandidateModule)
		}
	}
}

// TestTodo_LIB_009_QualificationContract ensures each decision-record field
// is part of the admission contract, rather than merely decorative YAML.
func TestTodo_LIB_009_QualificationContract(t *testing.T) {
	base := loadQualification(t)
	cases := []struct {
		name string
		edit func(*testcontainerskit.Qualification)
	}{
		{"optimistic verdict", func(q *testcontainerskit.Qualification) { q.Verdict = "ADOPT" }},
		{"admitted module", func(q *testcontainerskit.Qualification) { q.RuntimeProbe.ModuleInGoMod = "present" }},
		{"missing workload", func(q *testcontainerskit.Qualification) { q.AdoptionRequirements.Workloads = nil }},
		{"missing evidence", func(q *testcontainerskit.Qualification) { q.Evidence = q.Evidence[:5] }},
		{"changed command", func(q *testcontainerskit.Qualification) { q.Command = "go test ./..." }},
		{"claimed workload execution", func(q *testcontainerskit.Qualification) { q.ExecutionEvidence.Workloads["postgres"] = "passed" }},
		{"mislabelled integration", func(q *testcontainerskit.Qualification) {
			q.ExecutionEvidence.Integration = "testcontainers_cross_system"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			tc.edit(&q)
			if err := testcontainerskit.ValidateQualification(q); err == nil {
				t.Fatal("invalid qualification accepted")
			}
		})
	}
}

// TestTodo_LIB_009_Property checks that every required adoption invariant is
// independently necessary. It uses only a side-effect-free contract so an
// unavailable Docker daemon cannot be mistaken for an exercised integration
// environment.
func TestTodo_LIB_009_Property(t *testing.T) {
	for i := 0; i < 64; i++ {
		plan := acceptedPlan()
		plan.Namespace = fmt.Sprintf("tc-run-%032x", i)
		if err := testcontainerskit.ValidateAdoptionPlan(plan); err != nil {
			t.Fatalf("valid generated plan %d: %v", i, err)
		}
	}

	mutations := []struct {
		name string
		edit func(*testcontainerskit.AdoptionPlan)
	}{
		{"production scope", func(p *testcontainerskit.AdoptionPlan) { p.TestOnly = false }},
		{"floating module", func(p *testcontainerskit.AdoptionPlan) { p.ModuleVersion = "latest" }},
		{"path namespace", func(p *testcontainerskit.AdoptionPlan) { p.Namespace = "tc-../shared" }},
		{"unbounded readiness", func(p *testcontainerskit.AdoptionPlan) { p.ReadinessTimeout = 3 * time.Minute }},
		{"unowned cleanup", func(p *testcontainerskit.AdoptionPlan) { p.CleanupTarget = "all-containers" }},
		{"destroy failure evidence", func(p *testcontainerskit.AdoptionPlan) { p.PreserveOnFailure = false }},
		{"floating postgres image", func(p *testcontainerskit.AdoptionPlan) { p.Images["postgres"] = "postgres:latest" }},
		{"missing S3 fake", func(p *testcontainerskit.AdoptionPlan) { delete(p.Images, "s3") }},
		{"unapproved extra workload", func(p *testcontainerskit.AdoptionPlan) { p.Images["other"] = p.Images["smtp"] }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			plan := acceptedPlan()
			mutation.edit(&plan)
			if err := testcontainerskit.ValidateAdoptionPlan(plan); err == nil {
				t.Fatal("unsafe adoption plan accepted")
			}
		})
	}
}

// TestTodo_LIB_009_Golden pins the REJECT boundary and the entire list of
// future workload obligations. This prevents a documentation-only change from
// silently turning a non-executed candidate into an ADOPT claim.
func TestTodo_LIB_009_Golden(t *testing.T) {
	q := loadQualification(t)
	if q.Decision == "" || q.RemovalPath == "" {
		t.Fatal("decision and removal path are required")
	}
	if got, want := q.AdoptionRequirements.Workloads, []string{"postgres", "s3", "smtp", "provider"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workloads = %v, want %v", got, want)
	}
	if q.AdoptionRequirements.ImagePin != "each workload image must use an immutable sha256 digest; floating tags are forbidden" ||
		q.AdoptionRequirements.Namespace != "each run must use an opaque tc- prefixed namespace unique to that run" ||
		q.AdoptionRequirements.Readiness != "readiness must be deterministic and bounded to two minutes or less" ||
		q.AdoptionRequirements.Cleanup != "cleanup may target only the harness-owned run namespace" ||
		q.AdoptionRequirements.FailurePreservation != "failures must preserve diagnostic evidence until explicit operator cleanup" {
		t.Fatalf("adoption requirements drifted: %#v", q.AdoptionRequirements)
	}
	if !strings.Contains(q.Decision, "REJECT:") || !strings.Contains(q.Decision, "Docker Desktop") {
		t.Fatalf("decision does not preserve the observed rejection basis: %q", q.Decision)
	}
}

// TestTodo_LIB_009_Race proves the pre-admission gate and manifest parser do
// not retain cross-suite state: concurrent prospective suites get independent
// validation with no shared namespace registry or cleanup side effect.
func TestTodo_LIB_009_Race(t *testing.T) {
	const workers = 32
	root := repoRoot(t)
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plan := acceptedPlan()
			plan.Namespace = fmt.Sprintf("tc-run-%032x", i)
			if err := testcontainerskit.ValidateAdoptionPlan(plan); err != nil {
				errs <- fmt.Errorf("validate worker %d: %w", i, err)
				return
			}
			if _, err := testcontainerskit.LoadQualification(filepath.Join(root, "definitions", "architecture", qualificationFile)); err != nil {
				errs <- fmt.Errorf("load worker %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestTodo_LIB_009_Integration runs Go's real release dependency resolution
// for all shipped commands. The REJECT decision is valid only while no release
// binary resolves the candidate, whether directly or transitively.
func TestTodo_LIB_009_Integration(t *testing.T) {
	graph, err := testcontainerskit.ReleaseGraph(repoRoot(t), "./cmd/hcmnext", "./cmd/migrate", "./cmd/projector", "./cmd/worker")
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range graph {
		if imported == testcontainerskit.CandidateModule || strings.HasPrefix(imported, testcontainerskit.CandidateModule+"/") {
			t.Fatalf("release graph imports rejected candidate %q", imported)
		}
	}
}

// TestTodo_LIB_009_Conformance checks that the evidence set is exact and
// scans the source import graph as a second, fast failure mode before a full
// release build reaches the Integration test.
func TestTodo_LIB_009_Conformance(t *testing.T) {
	root := repoRoot(t)
	q := loadQualification(t)
	wantTests := map[string]bool{
		"TestTestcontainersQualification": false,
		"TestTodo_LIB_009_Property":       false,
		"TestTodo_LIB_009_Golden":         false,
		"TestTodo_LIB_009_Race":           false,
		"TestTodo_LIB_009_Integration":    false,
		"TestTodo_LIB_009_Conformance":    false,
	}
	for _, evidence := range q.Evidence {
		found, known := wantTests[evidence.Test]
		if !known {
			t.Errorf("unexpected evidence test %q", evidence.Test)
			continue
		}
		if found {
			t.Errorf("duplicate evidence test %q", evidence.Test)
		}
		wantTests[evidence.Test] = true
		if evidence.Package != "tools/quality/testcontainerskit" {
			t.Errorf("%s evidence package = %q", evidence.Test, evidence.Package)
		}
	}
	for name, found := range wantTests {
		if !found {
			t.Errorf("evidence missing %q", name)
		}
	}
	if q.Command != "go test -count=1 ./tools/quality/testcontainerskit" {
		t.Errorf("command = %q", q.Command)
	}

	var imports []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".artifacts" || entry.Name() == ".git" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			name := strings.Trim(imported.Path.Value, `"`)
			if name == testcontainerskit.CandidateModule || strings.HasPrefix(name, testcontainerskit.CandidateModule+"/") {
				imports = append(imports, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Go imports: %v", err)
	}
	if len(imports) != 0 {
		t.Fatalf("REJECT candidate imported by %s", strings.Join(imports, ", "))
	}
}
