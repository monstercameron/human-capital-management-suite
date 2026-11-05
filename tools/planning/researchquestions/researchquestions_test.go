package researchquestions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlockingResearchQuestionsAreTracked(t *testing.T) {
	spec := `## 11. Open Questions

Blocking for a release:

1. **Federal baseline is stated once.**
2. **New York personnel-file access is disputed.**

Non-blocking, but informative.
`
	readme := `### 2026-09-03 — LEGAL-018 contradiction closure

1. **FIXED** after reviewing the federal baseline.
2. **Personnel-file access.** New York: **DISPUTED** pending legislative resolution.

## Queue
`

	if got := CheckBlockingQuestionsTracked(spec, readme); len(got) != 0 {
		t.Fatalf("complete review log produced violations: %v", got)
	}

	t.Run("RED_missing_item_is_not_silently_accepted", func(t *testing.T) {
		incomplete := strings.Replace(readme, "2. **Personnel-file access.** New York: **DISPUTED** pending legislative resolution.\n", "", 1)
		violations := CheckBlockingQuestionsTracked(spec, incomplete)
		if !hasViolation(violations, "missing_review_log_entry") {
			t.Fatalf("missing blocking item produced %v, want missing_review_log_entry", violations)
		}
	})

	t.Run("RED_unresolved_item_requires_a_recognized_outcome", func(t *testing.T) {
		unclassified := strings.Replace(readme, "**FIXED**", "**UNDER_REVIEW**", 1)
		violations := CheckBlockingQuestionsTracked(spec, unclassified)
		if !hasViolation(violations, "no_recognized_outcome") {
			t.Fatalf("unclassified blocking item produced %v, want no_recognized_outcome", violations)
		}
	})
}

func TestDisputedResearchCannotBeMarkedReleasable(t *testing.T) {
	spec := `### 5.1 Table A

| State | PERSONNEL_FILE |
| ----- | -------------- |
| NY    | ?              |

### 5.2 Table B

| State | NOTICE |
| ----- | ------ |
| NY    | F      |

## 6. Evaluation Semantics
`
	readme := `### 2026-09-03 — LEGAL-018 contradiction closure

3. **Personnel-file access.** New York: **DISPUTED** pending legislative resolution.
`
	if got := CheckDisputedItemOutcomesGated(spec, readme); len(got) != 0 {
		t.Fatalf("unreviewed matrix cell produced violations: %v", got)
	}

	releasable := strings.Replace(spec, "| NY    | ?              |", "| NY    | Y              |", 1)
	violations := CheckDisputedItemOutcomesGated(releasable, readme)
	if !hasViolation(violations, "disputed_state_releasable") {
		t.Fatalf("releasable disputed cell produced %v, want disputed_state_releasable", violations)
	}
}

func TestFileStatusMatchesReviewQueue(t *testing.T) {
	readme := "| New York | `new-york.md` | REVIEWED | 2026-09-03 | 2026-09-03 |\n"
	files := map[string]string{
		"new-york.md": "# New York\n\n**State:** New York | **Status:** REVIEWED\n",
	}
	if got := CheckFileStatusMatchesReadme(readme, files); len(got) != 0 {
		t.Fatalf("matching status produced violations: %v", got)
	}

	files["new-york.md"] = "# New York\n\n**State:** New York | **Status:** DRAFTED\n"
	if got := CheckFileStatusMatchesReadme(readme, files); !hasViolation(got, "file_status_disagrees_with_readme") {
		t.Fatalf("mismatched status produced %v, want file_status_disagrees_with_readme", got)
	}
}

// TestTodo_LEGAL_018_Golden runs the checker over the source-of-truth
// contract, review log, and every state file. This keeps a release from being
// marked ready after a source edit reopens a tracked contradiction.
func TestTodo_LEGAL_018_Golden(t *testing.T) {
	root := repoRoot(t)
	spec, err := os.ReadFile(filepath.Join(root, "planning", "specs", "legal-rule-packs-and-state-configuration.md"))
	if err != nil {
		t.Fatalf("read legal-rule-packs-and-state-configuration.md: %v", err)
	}
	researchDir := filepath.Join(root, "planning", "research", "state-employment-law")
	readme, err := os.ReadFile(filepath.Join(researchDir, "README.md"))
	if err != nil {
		t.Fatalf("read research README: %v", err)
	}
	files := stateFiles(t, researchDir)

	if got := Check(string(spec), string(readme), files); len(got) != 0 {
		t.Fatalf("LEGAL-018 research-question check found %d violation(s):\n%s", len(got), formatViolations(got))
	}
}

func hasViolation(violations []Violation, kind string) bool {
	for _, violation := range violations {
		if violation.Kind == kind {
			return true
		}
	}
	return false
}

func formatViolations(violations []Violation) string {
	var b strings.Builder
	for _, violation := range violations {
		b.WriteString(violation.String())
		b.WriteByte('\n')
	}
	return b.String()
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "planning", "archive", "todos-2026-10-22.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root above %s", dir)
		}
		dir = parent
	}
}

func stateFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "README.md" || entry.Name() == "us-federal.md" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		files[entry.Name()] = string(content)
	}
	return files
}
