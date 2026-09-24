package workflowmaturity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/designownership"
)

// DefaultCatalogBaseline pins the reviewed set of currently known catalog
// disagreements. New disagreements fail validation; known UNASSIGNED
// disagreements remain visible without making the gate permanently red.
const DefaultCatalogBaseline = "definitions/planning/workflowmaturity-catalog-baseline.json"

// CatalogException is one reviewed catalog mismatch and its ownership
// disposition. UNASSIGNED is an honest open state, not a resolution owner.
type CatalogException struct {
	FlowID        string   `json:"flow_id"`
	Definition    string   `json:"definition"`
	Owner         string   `json:"owner"`
	CandidateRefs []string `json:"candidate_refs,omitempty"`
	Reason        string   `json:"reason"`
}

// CatalogBaseline records known mismatches and their current disposition.
type CatalogBaseline struct {
	SchemaVersion int                `json:"schema_version"`
	Status        string             `json:"status"`
	Entries       []CatalogException `json:"entries"`
}

// LoadCatalogBaseline reads and validates the checked-in exception policy.
func LoadCatalogBaseline(root, path string) (CatalogBaseline, error) {
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, resolved)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return CatalogBaseline{}, fmt.Errorf("read catalog baseline %s: %w", resolved, err)
	}
	var baseline CatalogBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return CatalogBaseline{}, fmt.Errorf("decode catalog baseline %s: %w", resolved, err)
	}
	if baseline.SchemaVersion != 1 || strings.TrimSpace(baseline.Status) == "" {
		return CatalogBaseline{}, fmt.Errorf("catalog baseline requires schema_version 1 and status")
	}
	seen := map[string]bool{}
	for _, entry := range baseline.Entries {
		key := entry.FlowID + "\x00" + entry.Definition
		if entry.FlowID == "" || entry.Definition == "" || strings.TrimSpace(entry.Owner) == "" || strings.TrimSpace(entry.Reason) == "" {
			return CatalogBaseline{}, fmt.Errorf("catalog baseline entry must name flow, definition, owner disposition, and reason")
		}
		if entry.Owner == "UNASSIGNED" && len(entry.CandidateRefs) == 0 {
			return CatalogBaseline{}, fmt.Errorf("unassigned catalog baseline entry %s %s must name ownership candidate refs", entry.FlowID, entry.Definition)
		}
		if seen[key] {
			return CatalogBaseline{}, fmt.Errorf("catalog baseline repeats %s for %s", entry.FlowID, entry.Definition)
		}
		seen[key] = true
	}
	return baseline, nil
}

// ValidateCatalogBaseline rejects new disagreements and baseline entries
// whose ownership references no longer describe the live candidates. A
// known UNASSIGNED entry is accepted as a pinned open issue.
func ValidateCatalogBaseline(disagreements []CatalogDisagreement, baseline CatalogBaseline, ownership designownership.Ownership) []string {
	allowed := make(map[string]CatalogException, len(baseline.Entries))
	for _, entry := range baseline.Entries {
		allowed[entry.FlowID+"\x00"+entry.Definition] = entry
	}
	var violations []string
	candidates := make(map[string]designownership.Candidate, len(ownership.Candidates))
	for _, candidate := range ownership.Candidates {
		candidates[candidate.ID] = candidate
	}
	live := make(map[string]bool, len(disagreements))
	for _, d := range disagreements {
		key := d.FlowID + "\x00" + d.Definition
		live[key] = true
		entry, ok := allowed[key]
		if !ok {
			violations = append(violations, fmt.Sprintf("unreviewed catalog disagreement: %s %s", d.FlowID, d.Definition))
			continue
		}
		if strings.TrimSpace(entry.Owner) == "" || strings.TrimSpace(entry.Reason) == "" {
			violations = append(violations, fmt.Sprintf("catalog disagreement has no owner or reason: %s %s", d.FlowID, d.Definition))
			continue
		}
		if entry.Owner == "UNASSIGNED" {
			validRefs := 0
			for _, ref := range entry.CandidateRefs {
				candidate, ok := candidates[ref]
				if !ok || candidate.Owner != "UNASSIGNED" || !contains(candidate.Refs, d.Definition) {
					violations = append(violations, fmt.Sprintf("catalog disagreement has stale or unrelated ownership candidate %s: %s %s", ref, d.FlowID, d.Definition))
					continue
				}
				validRefs++
			}
			if validRefs == 0 {
				violations = append(violations, fmt.Sprintf("catalog disagreement is explicitly UNASSIGNED with no live candidate ref: %s %s", d.FlowID, d.Definition))
			}
		}
	}
	for _, entry := range baseline.Entries {
		key := entry.FlowID + "\x00" + entry.Definition
		if !live[key] {
			violations = append(violations, fmt.Sprintf("stale catalog baseline entry: %s %s", entry.FlowID, entry.Definition))
		}
	}
	sort.Strings(violations)
	return violations
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// CatalogClaim is one hand-typed status assertion read from
// planning/workflows/catalog.md's "### WF-xxx. Title" flow sections. It is
// data only: ParseCatalogClaims never feeds this into Reconcile, exactly
// because REFACTOR requires the gate to be built from generated evidence,
// never derived from this prose.
type CatalogClaim struct {
	FlowID      string   `json:"flow_id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Definitions []string `json:"definitions"`
}

// CatalogDisagreement names one accepted definition where catalog.md
// claims a flow that names it is EXISTING while the generated
// [Reconcile] evidence cannot support CONTRACTED for that definition. This
// is the deliverable the todo's REFACTOR/ordering-hazard guidance asks
// for: the disagreement is reported, never silently resolved in either
// document's favor.
type CatalogDisagreement struct {
	FlowID     string    `json:"flow_id"`
	Title      string    `json:"title"`
	Definition string    `json:"definition"`
	Claimed    string    `json:"claimed_in_catalog_md"`
	Evidence   string    `json:"generated_allowed_status"`
	Blockers   []Blocker `json:"blockers"`
}

var (
	flowHeaderRe    = regexp.MustCompile(`^### (WF-[A-Z0-9-]+)\. (.+)$`)
	statusLineRe    = regexp.MustCompile("^-\\s+\\*\\*Status\\*\\*:\\s+`([A-Z_]+)`")
	definitionRefRe = regexp.MustCompile(`hcmnext\.[a-z0-9_]+\.[a-z0-9_]+/v[0-9]+`)
)

// ParseCatalogClaims scans markdown (planning/workflows/catalog.md's
// content) for every flow section: a "### WF-xxx. Title" header, the
// section's own "**Status**" line, and every definition_ref mentioned
// anywhere in the section body before the next header. A section with no
// Status line or no definition_ref mention is simply not returned; this
// parser only reports what the document actually states; it never
// guesses.
func ParseCatalogClaims(markdown string) []CatalogClaim {
	lines := strings.Split(markdown, "\n")
	var claims []CatalogClaim
	var current *CatalogClaim
	definitionSeen := map[string]bool{}

	flush := func() {
		if current == nil {
			return
		}
		if current.Status != "" && len(current.Definitions) > 0 {
			sort.Strings(current.Definitions)
			claims = append(claims, *current)
		}
		current = nil
	}

	for _, line := range lines {
		if match := flowHeaderRe.FindStringSubmatch(line); match != nil {
			flush()
			current = &CatalogClaim{FlowID: match[1], Title: strings.TrimSpace(match[2])}
			definitionSeen = map[string]bool{}
			continue
		}
		if current == nil {
			continue
		}
		if match := statusLineRe.FindStringSubmatch(line); match != nil && current.Status == "" {
			current.Status = match[1]
		}
		for _, ref := range definitionRefRe.FindAllString(line, -1) {
			if !definitionSeen[ref] {
				definitionSeen[ref] = true
				current.Definitions = append(current.Definitions, ref)
			}
		}
	}
	flush()

	sort.Slice(claims, func(i, j int) bool { return claims[i].FlowID < claims[j].FlowID })
	return claims
}

// CrossCheckCatalog compares generated evidence against catalog.md's
// hand-typed claims and returns one CatalogDisagreement for every accepted
// definition a claimed-EXISTING flow names that the generated report
// cannot support past CATALOGUED. It never mutates report or claims and
// never resolves the disagreement - report is the intended output.
func CrossCheckCatalog(report Report, claims []CatalogClaim) []CatalogDisagreement {
	byDefinition := make(map[string]DefinitionResult, len(report.Results))
	for _, r := range report.Results {
		byDefinition[r.Definition] = r
	}

	var disagreements []CatalogDisagreement
	seen := map[string]bool{}
	for _, claim := range claims {
		if claim.Status != "EXISTING" {
			continue
		}
		for _, definition := range claim.Definitions {
			result, ok := byDefinition[definition]
			if !ok || len(result.Blockers) == 0 {
				continue
			}
			key := claim.FlowID + "\x00" + definition
			if seen[key] {
				continue
			}
			seen[key] = true
			disagreements = append(disagreements, CatalogDisagreement{
				FlowID:     claim.FlowID,
				Title:      claim.Title,
				Definition: definition,
				Claimed:    claim.Status,
				Evidence:   string(result.AllowedStatus),
				Blockers:   result.Blockers,
			})
		}
	}
	sort.Slice(disagreements, func(i, j int) bool {
		if disagreements[i].FlowID != disagreements[j].FlowID {
			return disagreements[i].FlowID < disagreements[j].FlowID
		}
		return disagreements[i].Definition < disagreements[j].Definition
	})
	return disagreements
}
