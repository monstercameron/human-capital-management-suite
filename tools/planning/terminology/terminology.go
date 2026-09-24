// Package terminology enforces canonical-term and alias conformance
// (GOV-012) against the boundary decisions recorded in plan.md section 9.4
// and planning/data/models/registry-and-coverage-contracts.md's Canonical
// boundary decisions: Platform IAM must never be confused with the
// Workforce Access product, Candidate/Worker/Former Worker must never be
// treated as mutually exclusive Person states, and an external observation
// must never be relabeled a domain fact.
//
// Detection is deliberately conservative: each rule matches only a
// positive claim of the prohibited confusion, not prose that discusses or
// explicitly negates it (a sentence containing a negation cue such as
// "not", "never", or "rather than" between the two terms is not flagged),
// because the source documents themselves correctly discuss these
// distinctions at length.
//
// REFACTOR note: emit glossary links (to plan.md#94-identity-and-permission-contract
// and the canonical boundary decisions section) from generated
// documentation instead of leaving the rule only enforced here.
package terminology

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Violation names one canonical-term confusion found in text.
type Violation struct {
	Line int
	Rule string
	Text string
}

func (v Violation) String() string {
	return fmt.Sprintf("line %d: %s: %q", v.Line, v.Rule, v.Text)
}

var negationCues = []string{"not ", "n't", "never", "rather than", "instead of", "distinct from"}

// GlossaryEntry is the canonical term and its normative definition link.
// Aliases carry an explicit version so callers cannot silently introduce a
// second name for the same concept.
type GlossaryEntry struct {
	Canonical string
	Aliases   []VersionedAlias
	URL       string
}

// VersionedAlias is a reviewed historical or presentation name.
type VersionedAlias struct {
	Name    string
	Version string
}

// Glossary is the terminology registry for GOV-012's protected boundaries.
// It points to the canonical boundary decisions rather than duplicating their
// definitions here.
var Glossary = []GlossaryEntry{
	{Canonical: "Platform IAM", URL: "planning/plan.md#94-identity-and-permission-contract"},
	{Canonical: "Workforce Access Product", URL: "planning/plan.md#94-identity-and-permission-contract", Aliases: []VersionedAlias{{Name: "Workforce Access", Version: "v1"}}},
	{Canonical: "Person", URL: "planning/data/models/registry-and-coverage-contracts.md#canonical-boundary-decisions"},
	{Canonical: "Candidate", URL: "planning/data/models/registry-and-coverage-contracts.md#canonical-boundary-decisions"},
	{Canonical: "Worker", URL: "planning/data/models/registry-and-coverage-contracts.md#canonical-boundary-decisions"},
	{Canonical: "Former Worker", URL: "planning/data/models/registry-and-coverage-contracts.md#canonical-boundary-decisions"},
	{Canonical: "External Observation", URL: "planning/plan.md#9-governing-platform-contracts"},
	{Canonical: "Domain Fact", URL: "planning/plan.md#9-governing-platform-contracts"},
}

// Finding identifies a terminology violation in a source file.
type Finding struct {
	Path      string
	Violation Violation
}

// ScanRepository checks planning documents, SchemaFlux/protobuf schemas, and
// YAML/JSON model definitions. These are the authored documentation and
// contract surfaces governed by GOV-012.
func ScanRepository(root string) ([]Finding, error) {
	var findings []Finding
	for _, dir := range []string{"planning", "schema", "definitions"} {
		base := filepath.Join(root, dir)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !isTerminologySurface(path) {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, v := range CheckCanonicalTerms(string(content)) {
				findings = append(findings, Finding{Path: path, Violation: v})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", base, err)
		}
	}
	return findings, nil
}

func isTerminologySurface(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".yaml", ".yml", ".proto", ".json":
		return true
	default:
		return false
	}
}

// LinkGlossaryTerms adds links for registered canonical terms in generated
// Markdown. Existing links and inline code remain untouched.
func LinkGlossaryTerms(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		lines[i] = linkTermsInLine(line)
	}
	return strings.Join(lines, "\n")
}

var protectedMarkdown = regexp.MustCompile("`[^`]*`|\\[[^\\]]+\\]\\([^)]+\\)")

func linkTermsInLine(line string) string {
	var result strings.Builder
	last := 0
	for _, bounds := range protectedMarkdown.FindAllStringIndex(line, -1) {
		result.WriteString(linkTerms(line[last:bounds[0]]))
		result.WriteString(line[bounds[0]:bounds[1]])
		last = bounds[1]
	}
	result.WriteString(linkTerms(line[last:]))
	return result.String()
}

func linkTerms(text string) string {
	for _, entry := range Glossary {
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(entry.Canonical) + `\b`)
		text = pattern.ReplaceAllString(text, "["+entry.Canonical+"]("+entry.URL+")")
	}
	return text
}

// hasNegationCue reports whether span contains wording that negates or
// warns against the match rather than committing it - e.g. "Candidate...
// are not mutually exclusive" or "external observations rather than...
// domain fact".
func hasNegationCue(span string) bool {
	lower := strings.ToLower(span)
	for _, cue := range negationCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

var (
	// Rule 1: Platform IAM vs Workforce Access (plan.md 9.4). Platform IAM
	// authenticates/authorizes platform users/services/operators; Workforce
	// Access governs customer-worker accounts/provisioning. Either verb
	// set attached to the wrong subject is a confusion.
	workforceActingAsIAMRe = regexp.MustCompile(`(?i)\bworkforce access\b[^.]{0,80}\b(authenticates|authorizes)\b`)
	iamActingAsWorkforceRe = regexp.MustCompile(`(?i)\bplatform iam\b[^.]{0,80}\b(customer-worker accounts?|provisions?|deprovisions?)\b`)

	// Rule 2: Candidate/Worker/Former Worker are not mutually exclusive
	// Person states (plan.md line ~200). Only a positive exclusivity claim
	// is flagged.
	candidateExclusiveRe = regexp.MustCompile(`(?i)\bcandidate\b[^.]{0,80}\b(is|as|are)\b[^.]{0,40}\bexclusive\b[^.]{0,40}\bperson\b`)
	personEitherOrRe     = regexp.MustCompile(`(?i)\ba? ?person\b[^.]{0,40}\bis either\b[^.]{0,40}\bcandidate\b`)

	// Rule 3: external observation is a distinct assertion-authority class
	// from domain fact (plan.md line ~1099). Only a positive relabeling
	// claim is flagged.
	observationAsFactRe = regexp.MustCompile(`(?i)\bexternal observation\b[^.]{0,60}\b(is|as|labeled|recorded as)\b[^.]{0,40}\bdomain fact\b`)
)

// specFieldPrefixes are todo fields that describe a checker's expected
// rejections rather than assert domain terminology - e.g. GOV-012's own
// RED field names "Candidate used as exclusive Person state" as the exact
// anti-pattern its test must catch. Lines starting with one of these
// (after trimming leading "- ") are not scanned.
var specFieldPrefixes = []string{"**RED:**", "**GREEN:**"}

func isSpecDescriptionLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "-")
	trimmed = strings.TrimSpace(trimmed)
	for _, p := range specFieldPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, `"red":`) || strings.HasPrefix(lower, `"green":`)
}

// CheckCanonicalTerms scans text line by line for canonical-term
// confusions and returns every violation found.
func CheckCanonicalTerms(text string) []Violation {
	var violations []Violation

	for i, line := range strings.Split(text, "\n") {
		lineNum := i + 1
		if isSpecDescriptionLine(line) {
			continue
		}

		if m := workforceActingAsIAMRe.FindString(line); m != "" && !hasNegationCue(m) {
			violations = append(violations, Violation{Line: lineNum, Rule: "Platform IAM confused with Workforce Access", Text: m})
		}
		if m := iamActingAsWorkforceRe.FindString(line); m != "" && !hasNegationCue(m) {
			violations = append(violations, Violation{Line: lineNum, Rule: "Platform IAM confused with Workforce Access", Text: m})
		}
		if m := candidateExclusiveRe.FindString(line); m != "" && !hasNegationCue(m) {
			violations = append(violations, Violation{Line: lineNum, Rule: "Candidate used as an exclusive Person state", Text: m})
		}
		if m := personEitherOrRe.FindString(line); m != "" && !hasNegationCue(m) {
			violations = append(violations, Violation{Line: lineNum, Rule: "Candidate used as an exclusive Person state", Text: m})
		}
		if m := observationAsFactRe.FindString(line); m != "" && !hasNegationCue(m) {
			violations = append(violations, Violation{Line: lineNum, Rule: "external observation labeled a domain fact", Text: m})
		}
	}

	return violations
}
