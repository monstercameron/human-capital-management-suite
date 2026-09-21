// Package docfix holds the DOCFIX-001 through DOCFIX-012 regression checks:
// each documentation-accuracy fix from planning/todos.md section 76 gets a
// PRIMARY test that fails if the same drift returns, plus a GOLDEN test that
// pins the corrected bytes.
package docfix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot resolves the repository root from tools/planning/docfix.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("docfix: get working directory: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", "..", ".."))
	if err != nil {
		t.Fatalf("docfix: resolve root: %v", err)
	}
	return root
}

// readText reads a repo-relative file or fails the test.
func readText(t *testing.T, root string, rel ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{root}, rel...)...))
	if err != nil {
		t.Fatalf("docfix: read %s: %v", filepath.Join(rel...), err)
	}
	return string(data)
}

// packageScripts returns the scripts map from package.json.
func packageScripts(t *testing.T, root string) map[string]string {
	t.Helper()
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(readText(t, root, "package.json")), &pkg); err != nil {
		t.Fatalf("docfix: parse package.json: %v", err)
	}
	return pkg.Scripts
}

// testAllSteps splits the test:all script into short step names, stripping
// the leading "npm run " from each step.
func testAllSteps(t *testing.T, root string) []string {
	t.Helper()
	scripts := packageScripts(t, root)
	all, ok := scripts["test:all"]
	if !ok {
		t.Fatalf("docfix: package.json has no test:all script")
	}
	var steps []string
	for _, part := range strings.Split(all, "&&") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "npm run ")
		if part != "" {
			steps = append(steps, part)
		}
	}
	return steps
}

// agentsTokenForStep maps a test:all short step to the substring AGENTS.md
// must contain to document it.
func agentsTokenForStep(step string) (string, bool) {
	tokens := map[string]string{
		"format:check":            "format",
		"typecheck":               "typecheck",
		"lint":                    "lint",
		"check:code-style":        "code-style",
		"check:go":                "check:go",
		"check:race-policy":       "race-policy",
		"check:decomposition":     "decomposition",
		"check:driftgate":         "drift",
		"check:apigate":           "API",
		"check:substratecoverage": "substrate",
		"check:enginecoverage":    "engine",
		"test":                    "unit tests",
		"test:go":                 "nested-module",
		"build":                   "build",
	}
	token, ok := tokens[step]
	return token, ok
}

// settingsDeny returns the permissions.deny list from .claude/settings.json.
func settingsDeny(t *testing.T, root string) []string {
	t.Helper()
	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(readText(t, root, ".claude", "settings.json")), &settings); err != nil {
		t.Fatalf("docfix: parse settings.json: %v", err)
	}
	return settings.Permissions.Deny
}

// section extracts the markdown section starting with "## heading" through
// the next "## " heading or end of file.
func section(md, heading string) string {
	marker := "## " + heading
	start := strings.Index(md, marker)
	if start < 0 {
		return ""
	}
	rest := md[start+len(marker):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		return marker + rest[:next]
	}
	return marker + rest
}

// fencedBlockContaining returns the first fenced code block containing needle.
func fencedBlockContaining(md, needle string) string {
	blocks := strings.Split(md, "```")
	for i := 1; i+1 < len(blocks); i += 2 {
		if strings.Contains(blocks[i], needle) {
			return "```" + blocks[i] + "```"
		}
	}
	return ""
}

// migrateUpCount counts occurrences of the migrate-up command.
func migrateUpCount(readme string) int {
	return strings.Count(readme, "go run ./cmd/migrate up")
}

// backtickGoFiles extracts backtick-quoted .go file names from text.
var goFilePattern = regexp.MustCompile("`([^`]*\\.go)`")

func backtickGoFiles(text string) []string {
	var out []string
	for _, m := range goFilePattern.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if strings.ContainsAny(name, "*/") {
			continue
		}
		out = append(out, name)
	}
	return out
}

var migRangePattern = regexp.MustCompile(`^typed_mig_([A-F])\.go$`)

// expandMigRange expands a typed_mig_A.go/typed_mig_F.go endpoint pair into
// the full A-F file list.
func expandMigRange(files []string) []string {
	var letters []string
	var rest []string
	for _, f := range files {
		if m := migRangePattern.FindStringSubmatch(f); m != nil {
			letters = append(letters, m[1])
		} else {
			rest = append(rest, f)
		}
	}
	if len(letters) < 2 {
		return files
	}
	min, max := letters[0], letters[0]
	for _, l := range letters[1:] {
		if l < min {
			min = l
		}
		if l > max {
			max = l
		}
	}
	out := rest
	for c := min[0]; c <= max[0]; c++ {
		out = append(out, fmt.Sprintf("typed_mig_%c.go", c))
	}
	return out
}

// approvedCommands returns approved_commands.initial from the layout manifest.
func approvedCommands(t *testing.T, root string) []string {
	t.Helper()
	var manifest struct {
		ApprovedCommands struct {
			Initial []string `yaml:"initial"`
		} `yaml:"approved_commands"`
	}
	if err := yaml.Unmarshal([]byte(readText(t, root, "definitions", "architecture", "repository-layout.yaml")), &manifest); err != nil {
		t.Fatalf("docfix: parse repository-layout.yaml: %v", err)
	}
	return manifest.ApprovedCommands.Initial
}

// manifestBlock extracts the "- name: <name>" block from the manifest text
// through the next "- name:" entry or end of file.
func manifestBlock(manifest, name string) string {
	marker := "- name: " + name
	start := strings.Index(manifest, marker)
	if start < 0 {
		return ""
	}
	rest := manifest[start+len(marker):]
	if next := strings.Index(rest, "\n  - name: "); next >= 0 {
		return marker + rest[:next]
	}
	return marker + rest
}
