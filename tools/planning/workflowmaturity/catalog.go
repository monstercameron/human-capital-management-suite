package workflowmaturity

import (
	"regexp"
	"sort"
	"strings"
)

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
