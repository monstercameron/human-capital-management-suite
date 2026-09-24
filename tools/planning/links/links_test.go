package links

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanningLinks is the primary test for GOV-004.
// It validates that all markdown links in planning/ and README.md resolve
// correctly, except edges explicitly allow-listed in
// definitions/planning/known-broken-links.yaml.
func TestPlanningLinks(t *testing.T) {
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	// Navigate to root: tools/planning/links -> tools/planning -> tools -> root
	rootPath, err := filepath.Abs(filepath.Join(currentDir, "../../../"))
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	issues, err := CheckPlanningLinks(rootPath)
	if err != nil {
		t.Fatalf("failed to check links: %v", err)
	}

	knownBrokenList, err := LoadKnownBrokenLinks(filepath.Join(rootPath, "definitions", "planning", "known-broken-links.yaml"))
	if err != nil {
		t.Fatalf("failed to load known-broken-links.yaml: %v", err)
	}
	knownBroken := make(map[string]bool, len(knownBrokenList))
	for _, k := range knownBrokenList {
		knownBroken[fmt.Sprintf("%s:%d:%s", k.File, k.Line, k.Link)] = true
	}

	var realIssues []LinkIssue
	seen := make(map[string]bool, len(knownBrokenList))
	for _, issue := range issues {
		// Skip testdata fixtures (these are intentional)
		if strings.Contains(issue.File, "testdata") {
			continue
		}

		relPath := strings.TrimPrefix(issue.File, rootPath)
		relPath = strings.TrimPrefix(relPath, `\`)
		relPath = strings.TrimPrefix(relPath, "/")
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		key := fmt.Sprintf("%s:%d:%s", relPath, issue.Line, issue.Link)
		if knownBroken[key] {
			seen[key] = true
			continue
		}

		realIssues = append(realIssues, issue)
	}

	if len(realIssues) > 0 {
		t.Errorf("found %d broken links:", len(realIssues))
		for _, issue := range realIssues {
			t.Errorf("  %s:%d: %s -> %s", issue.File, issue.Line, issue.Link, issue.Reason)
		}
	}

	// Every allow-listed entry must still be genuinely broken - a stale
	// entry would silently hide a regression once the link is fixed.
	for key := range knownBroken {
		if !seen[key] {
			t.Errorf("known-broken-links.yaml entry %s is no longer broken; remove it", key)
		}
	}
}

// TestTodo_GOV_004_Golden pins the scanner's diagnostic coordinates and
// wording for a missing file and a missing heading anchor.
func TestTodo_GOV_004_Golden(t *testing.T) {
	root := t.TempDir()
	planning := filepath.Join(root, "planning")
	if err := os.MkdirAll(planning, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planning, "index.md"), []byte("# Index\n\n[missing](absent.md)\n[anchor](target.md#nope)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planning, "target.md"), []byte("# Present\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := CheckPlanningLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []LinkIssue{
		{File: filepath.Join(planning, "index.md"), Line: 3, Link: "absent.md", Reason: "file not found: absent.md"},
		{File: filepath.Join(planning, "index.md"), Line: 4, Link: "target.md#nope", Reason: "anchor not found in target.md: nope"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("link diagnostics = %#v, want %#v", got, want)
	}
}

// TestGitHubAnchor tests the anchor generation algorithm against GitHub's
// actual behavior: only letters/digits/hyphens/underscores survive, spaces
// become hyphens one-for-one (no collapsing), and everything else is
// dropped in place (so an em dash between two spaces leaves a double
// hyphen behind).
func TestGitHubAnchor(t *testing.T) {
	tests := []struct {
		heading string
		anchor  string
	}{
		{"Hello World", "hello-world"},
		{"Phase 1 Execution Plan", "phase-1-execution-plan"},
		{"Business Intent is the behavioral root", "business-intent-is-the-behavioral-root"},
		{"Test-Case", "test-case"},
		{"Test_Case", "test_case"},
		{"Multiple   Spaces", "multiple---spaces"},
		{"Special!@#$%Characters", "specialcharacters"},
		{"Simple", "simple"},
		{"UPPERCASE TEXT", "uppercase-text"},
		{"Gate A Acceptance — Paid Observation", "gate-a-acceptance--paid-observation"},
		{"Gate B Acceptance — Limited Write Authority", "gate-b-acceptance--limited-write-authority"},
		{"P1B — One Bounded Authority Amendment", "p1b--one-bounded-authority-amendment"},
	}

	for _, tc := range tests {
		result := githubAnchor(tc.heading)
		if result != tc.anchor {
			t.Errorf("githubAnchor(%q) = %q, want %q", tc.heading, result, tc.anchor)
		}
	}
}

// TestHeadingExtraction tests that headings are correctly extracted from markdown.
func TestHeadingExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "test.md")

	content := `# Main Heading
## Section One
### Subsection
## Section One
## Another Section
`

	err := os.WriteFile(testFilePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	headings, err := extractHeadings(testFilePath)
	if err != nil {
		t.Fatalf("failed to extract headings: %v", err)
	}

	expectedHeadings := map[string]bool{
		"main-heading":    true,
		"section-one":     true,
		"subsection":      true,
		"section-one-1":   true, // Duplicate
		"another-section": true,
	}

	for heading := range expectedHeadings {
		if !headings[heading] {
			t.Errorf("expected heading %q not found", heading)
		}
	}

	for heading := range headings {
		if !expectedHeadings[heading] {
			t.Errorf("unexpected heading %q found", heading)
		}
	}
}

// TestHeadingExtractionIgnoresCodeBlocks verifies a "#"-prefixed line inside
// a fenced code block is not mistaken for a heading.
func TestHeadingExtractionIgnoresCodeBlocks(t *testing.T) {
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "test.md")

	content := "# Real Heading\n\n```text\n# Not A Heading\n```\n\n## Another Real Heading\n"

	if err := os.WriteFile(testFilePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	headings, err := extractHeadings(testFilePath)
	if err != nil {
		t.Fatalf("failed to extract headings: %v", err)
	}

	if !headings["real-heading"] || !headings["another-real-heading"] {
		t.Errorf("expected real headings not found: %v", headings)
	}
	if headings["not-a-heading"] {
		t.Errorf("heading inside code block was incorrectly extracted")
	}
}

// TestParseLink tests markdown link parsing.
func TestParseLink(t *testing.T) {
	tests := []struct {
		link   string
		text   string
		path   string
		anchor string
	}{
		{"[text](path.md)", "text", "path.md", ""},
		{"[text](path.md#anchor)", "text", "path.md", "anchor"},
		{"[Go Home](../README.md)", "Go Home", "../README.md", ""},
		{"[See Spec](specs/contract.md#requirements)", "See Spec", "specs/contract.md", "requirements"},
	}

	for _, tc := range tests {
		text, path, anchor := parseLink(tc.link)
		if text != tc.text || path != tc.path || anchor != tc.anchor {
			t.Errorf("parseLink(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tc.link, text, path, anchor, tc.text, tc.path, tc.anchor)
		}
	}
}

// TestFindMarkdownLinks tests markdown link detection.
func TestFindMarkdownLinks(t *testing.T) {
	line := "See [this](path.md) and [that](other.md#anchor) for more."
	links := findMarkdownLinks(line)

	if len(links) != 2 {
		t.Errorf("expected 2 links, found %d", len(links))
	}

	expected := []string{"[this](path.md)", "[that](other.md#anchor)"}
	for i, want := range expected {
		if i >= len(links) {
			t.Errorf("missing link: %s", want)
			continue
		}
		if links[i] != want {
			t.Errorf("link %d: got %q, want %q", i, links[i], want)
		}
	}
}

// TestCreateTestDataFixture creates deliberately broken links for testing.
func TestCreateTestDataFixture(t *testing.T) {
	tmpDir := t.TempDir()
	testdataDir := filepath.Join(tmpDir, "testdata")
	err := os.MkdirAll(testdataDir, 0755)
	if err != nil {
		t.Fatalf("failed to create testdata dir: %v", err)
	}

	brokenMarkdown := `# Test Document

See [broken file](nonexistent.md) for reference.

See [broken anchor](../README.md#nonexistent-heading) for details.
`

	testFilePath := filepath.Join(testdataDir, "broken-links.md")
	err = os.WriteFile(testFilePath, []byte(brokenMarkdown), 0644)
	if err != nil {
		t.Fatalf("failed to create test fixture: %v", err)
	}

	if _, err := os.Stat(testFilePath); err != nil {
		t.Errorf("test fixture not found: %v", err)
	}
}
