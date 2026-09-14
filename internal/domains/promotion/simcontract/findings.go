package simcontract

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
)

// Finding is the typed simulation finding shape. Identity is semantic (not a
// rendered sentence), while Source records the independently computed
// authority that corroborated it.
type Finding struct {
	Identity    string
	Owner       string
	Severity    promotion.Severity
	Field       string
	Effect      string
	Explanation string
	Source      string
}

// NewFinding creates a fully named typed finding.
func NewFinding(identity, owner string, severity promotion.Severity, field, effect, explanation, source string) (Finding, error) {
	f := Finding{Identity: identity, Owner: owner, Severity: severity, Field: field, Effect: effect, Explanation: explanation, Source: source}
	if err := f.Validate(); err != nil {
		return Finding{}, err
	}
	return f, nil
}

func (f Finding) Validate() error {
	for _, v := range []struct{ name, value string }{
		{"identity", f.Identity}, {"owner", f.Owner}, {"field", f.Field},
		{"explanation", f.Explanation}, {"source", f.Source},
	} {
		if strings.TrimSpace(v.value) == "" {
			return fmt.Errorf("%w: finding %s is required", ErrInvalidInput, v.name)
		}
	}
	switch f.Severity {
	case promotion.SeverityAdvisory, promotion.SeverityBlocking,
		promotion.SeverityNeedsData, promotion.SeverityDenied:
		// All known wire severities are valid.
	default:
		return fmt.Errorf("%w: finding severity is invalid", ErrInvalidInput)
	}
	return nil
}

// canonicalFindings converts the compatibility findings and additive typed
// findings into one deterministic set. Sources are retained as corroboration;
// rendered messages are never used as the identity key.
func canonicalFindings(legacy []promotion.Finding, typed []Finding) ([]Finding, error) {
	all := make([]Finding, 0, len(legacy)+len(typed))
	for _, f := range legacy {
		owner := f.Owner
		if owner == "" {
			owner = "promotion"
		}
		all = append(all, Finding{
			Identity: string(f.Code), Owner: owner, Severity: f.Severity,
			Field: f.Field, Effect: "promotion.proposal", Explanation: f.Message, Source: "promotion.preflight",
		})
	}
	all = append(all, typed...)
	if len(all) == 0 {
		return nil, nil
	}
	groups := make(map[string]Finding, len(all))
	for _, f := range all {
		if err := f.Validate(); err != nil {
			return nil, err
		}
		key := strings.Join([]string{f.Identity, f.Owner, f.Severity.String(), f.Field, f.Effect}, "\x00")
		if prior, ok := groups[key]; ok {
			if f.Explanation < prior.Explanation {
				prior.Explanation = f.Explanation
			}
			if f.Source != prior.Source && !strings.Contains("\x00"+prior.Source+"\x00", "\x00"+f.Source+"\x00") {
				prior.Source += "\x00" + f.Source
			}
			groups[key] = prior
			continue
		}
		groups[key] = f
	}
	out := make([]Finding, 0, len(groups))
	for _, f := range groups {
		sources := strings.Split(f.Source, "\x00")
		slices.Sort(sources)
		f.Source = strings.Join(sources, "\x00")
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		return findingKey(out[i]) < findingKey(out[j])
	})
	return out, nil
}

func findingKey(f Finding) string {
	return strings.Join([]string{f.Identity, f.Owner, f.Severity.String(), f.Field, f.Effect, f.Explanation, f.Source}, "\x00")
}

func legacyFindings(typed []Finding) []promotion.Finding {
	out := make([]promotion.Finding, 0, len(typed))
	for _, f := range typed {
		out = append(out, promotion.Finding{Code: f.Identity, Severity: f.Severity, Field: f.Field, Message: f.Explanation, Owner: f.Owner})
	}
	return out
}
