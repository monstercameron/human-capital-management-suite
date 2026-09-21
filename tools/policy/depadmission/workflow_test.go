package depadmission_test

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"gopkg.in/yaml.v3"
)

// workflowFile is the minimal shape this test needs out of
// .github/workflows/tests.yml: enough to prove the file parses as valid
// YAML and that every job this task added or touched (the split root lanes,
// the new ephemeral-PostgreSQL job, and the new dependency-admission job)
// is actually present and wired to a runner.
type workflowFile struct {
	On struct {
		Push struct {
			Branches []string `yaml:"branches"`
		} `yaml:"push"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Name     string   `yaml:"name"`
		Needs    []string `yaml:"needs"`
		RunsOn   string   `yaml:"runs-on"`
		Strategy struct {
			Matrix struct {
				Shard []int `yaml:"shard"`
			} `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
			Uses string `yaml:"uses"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// TestTestsWorkflowYAMLIsValid parses .github/workflows/tests.yml and
// checks its structure, so a syntax error or an accidentally-orphaned job
// (wrong indentation nesting it under the wrong parent, a typo'd key) in
// this task's edits fails a `go test` run instead of surfacing only when
// GitHub Actions itself rejects the file.
func TestTestsWorkflowYAMLIsValid(t *testing.T) {
	path := repopath.RootDir() + "/.github/workflows/tests.yml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}
	if len(wf.On.Push.Branches) != 1 || wf.On.Push.Branches[0] != "main" {
		t.Fatalf("topic-branch pushes must not duplicate pull-request checks; push branches = %v", wf.On.Push.Branches)
	}

	for _, name := range []string{"node", "go", "go-core-quality", "go-core-race", "go-core-coverage", "go-core", "go-core-ephemeral-pg", "dependency-admission"} {
		job, ok := wf.Jobs[name]
		if !ok {
			t.Errorf("workflow has no job named %q", name)
			continue
		}
		if job.RunsOn == "" {
			t.Errorf("job %q has no runs-on", name)
		}
		if len(job.Steps) == 0 {
			t.Errorf("job %q has no steps", name)
		}
	}

	race := findStep(t, wf, "go-core-race", "race")
	if race == "" {
		t.Fatal("go-core-race has no step invoking `go test -race` (TOOL-012)")
	}
	coverage := findStep(t, wf, "go-core-coverage", "covergate")
	if coverage == "" {
		t.Fatal("go-core-coverage has no full coverage sweep")
	}
	for _, jobName := range []string{"go-core-race", "go-core-coverage"} {
		shards := wf.Jobs[jobName].Strategy.Matrix.Shard
		if len(shards) != 4 || shards[0] != 0 || shards[1] != 1 || shards[2] != 2 || shards[3] != 3 {
			t.Errorf("job %q must cover deterministic shards [0 1 2 3], got %v", jobName, shards)
		}
	}
	if !strings.Contains(race, "race-shard.txt") || !strings.Contains(race, "(NR - 1) % 4") {
		t.Fatal("go-core-race does not partition the complete policy package list")
	}
	if !strings.Contains(coverage, "-shard-index") || !strings.Contains(coverage, "-shard-count 4") {
		t.Fatal("go-core-coverage does not invoke all-package deterministic sharding")
	}
	root := wf.Jobs["go-core"]
	if root.Name != "Go tests (root module)" {
		t.Fatalf("required root check name changed: %q", root.Name)
	}
	wantNeeds := map[string]bool{"go-core-quality": true, "go-core-race": true, "go-core-coverage": true}
	for _, need := range root.Needs {
		delete(wantNeeds, need)
	}
	if len(wantNeeds) != 0 {
		t.Fatalf("required root check does not fan in every root lane: missing %v", wantNeeds)
	}

	ephemeral := findStep(t, wf, "go-core-ephemeral-pg", "pgtest")
	if ephemeral == "" {
		t.Fatal("go-core-ephemeral-pg has no step running the pgtest package (TOOL-014)")
	}

	admission := findStep(t, wf, "dependency-admission", "depadmission")
	if admission == "" {
		t.Fatal("dependency-admission has no step invoking tools/policy/depadmission (TOOL-019/TOOL-024)")
	}
	govulncheck := findStep(t, wf, "dependency-admission", "govulncheck")
	if govulncheck == "" {
		t.Fatal("dependency-admission has no step invoking govulncheck")
	}
}

// findStep returns the `run` text of the first step in jobName whose `run`
// field contains substr, or "" if none matches.
func findStep(t *testing.T, wf workflowFile, jobName, substr string) string {
	t.Helper()
	job, ok := wf.Jobs[jobName]
	if !ok {
		t.Fatalf("no job named %q", jobName)
	}
	for _, step := range job.Steps {
		if strings.Contains(strings.ToLower(step.Run), strings.ToLower(substr)) {
			return step.Run
		}
	}
	return ""
}
