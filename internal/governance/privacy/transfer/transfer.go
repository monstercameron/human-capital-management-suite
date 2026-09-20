package transfer

// REV-099-01: the V2.0 privacy inventory tracks transfer safeguards by name,
// but no code compiled those names into rules. This package is the closed
// transfer-safeguard vocabulary: adequacy decisions, standard contractual
// clause modules, the UK extension and binding corporate rules. Region
// selectors and inventory validators read this one ruleset, so the UI and
// the backend can never disagree about which safeguards exist.
//
// The list is a curated snapshot, versioned by RulesVersion. It is not legal
// advice; it pins the rule names the platform enforces.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	// ErrEmptySafeguards reports a transfer with no safeguard at all.
	ErrEmptySafeguards = errors.New("transfer: at least one safeguard is required")
	// ErrUnknownSafeguard reports a safeguard name outside the ruleset.
	ErrUnknownSafeguard = errors.New("transfer: unknown safeguard")
)

// RulesVersion identifies the curated snapshot below.
const RulesVersion = "2026-09"

// Safeguard is one named transfer rule.
type Safeguard struct {
	// Name is the inventory-facing rule name, e.g. "adequacy:JP".
	Name string `json:"name"`
	// Basis is the legal basis family: adequacy, scc, uk-extension or bcr.
	Basis string `json:"basis"`
	// Reference cites the instrument, e.g. the Commission decision or the
	// 2021/914 module.
	Reference string `json:"reference"`
}

// rules is the closed vocabulary. Adequacy entries mirror the European
// Commission's adequacy decisions in force; SCC modules mirror Commission
// Implementing Decision 2021/914.
func rules() []Safeguard {
	adequate := []struct{ code, reference string }{
		{"AD", "Commission Decision 2010/625/EU (Andorra)"},
		{"AR", "Commission Decision 2003/490/EC (Argentina)"},
		{"CA", "Commission Decision 2002/2/EC (Canada, commercial organisations)"},
		{"FO", "Commission Decision 2010/146/EU (Faroe Islands)"},
		{"GG", "Commission Decision 2003/79/EC (Guernsey)"},
		{"IL", "Commission Decision 2011/61/EU (Israel)"},
		{"IM", "Commission Decision 2004/411/EC (Isle of Man)"},
		{"JP", "Commission Implementing Decision (EU) 2019/419 (Japan)"},
		{"JE", "Commission Decision 2008/393/EC (Jersey)"},
		{"NZ", "Commission Implementing Decision (EU) 2022/2291 (New Zealand)"},
		{"KR", "Commission Implementing Decision (EU) 2021/1289 (South Korea)"},
		{"CH", "Commission Decision 2000/518/EC (Switzerland)"},
		{"GB", "Commission Implementing Decision (EU) 2021/1772 (United Kingdom)"},
		{"US", "Commission Implementing Decision (EU) 2023/1795 (EU-US Data Privacy Framework)"},
		{"UY", "Commission Decision 2012/484/EU (Uruguay)"},
	}
	out := make([]Safeguard, 0, len(adequate)+6)
	for _, a := range adequate {
		out = append(out, Safeguard{Name: "adequacy:" + a.code, Basis: "adequacy", Reference: a.reference})
	}
	out = append(out,
		Safeguard{Name: "scc:module-one", Basis: "scc", Reference: "2021/914 Module One (controller to controller)"},
		Safeguard{Name: "scc:module-two", Basis: "scc", Reference: "2021/914 Module Two (controller to processor)"},
		Safeguard{Name: "scc:module-three", Basis: "scc", Reference: "2021/914 Module Three (processor to processor)"},
		Safeguard{Name: "uk-extension", Basis: "uk-extension", Reference: "UK Extension to the EU-US Data Privacy Framework"},
		Safeguard{Name: "uk-idta", Basis: "uk-extension", Reference: "UK International Data Transfer Agreement"},
		Safeguard{Name: "bcr", Basis: "bcr", Reference: "Binding Corporate Rules (Article 47 GDPR)"},
	)
	return out
}

// Rules returns the closed safeguard vocabulary in name order.
func Rules() []Safeguard {
	all := rules()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}

// Regions returns the adequate destinations a region selector offers, in
// name order. Selectors read this list so the UI offers exactly the regions
// the validator accepts.
func Regions() []string {
	var out []string
	for _, rule := range Rules() {
		if rule.Basis != "adequacy" {
			continue
		}
		out = append(out, strings.TrimPrefix(rule.Name, "adequacy:"))
	}
	return out
}

// Compile resolves safeguard names to rules. An empty set is refused, and
// the first unknown name is reported.
func Compile(names []string) ([]Safeguard, error) {
	if len(names) == 0 {
		return nil, ErrEmptySafeguards
	}
	known := make(map[string]Safeguard, 24)
	for _, rule := range rules() {
		known[rule.Name] = rule
	}
	out := make([]Safeguard, 0, len(names))
	for _, name := range names {
		rule, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownSafeguard, name)
		}
		out = append(out, rule)
	}
	return out, nil
}

// CanonicalRules renders the ruleset one name-per-line for the golden pin.
func CanonicalRules() string {
	var lines []string
	for _, rule := range Rules() {
		lines = append(lines, rule.Name+" :: "+rule.Basis+" :: "+rule.Reference)
	}
	return strings.Join(lines, "\n")
}
