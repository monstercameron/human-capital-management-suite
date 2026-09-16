// The four-domain eligibility parity gate proves Benefits, Leave,
// Learning and Rewards share the engine's status, time, evidence and
// explanation semantics while retaining domain-specific rules (ELIG-008).
//
// Any divergence — a missing or extra domain, an invalid request, an
// unevaluable fixture, an unbound result, non-shared time semantics, a
// cloned subject matter or one mono-rule across all four — refuses, so
// divergence blocks shared engine publication.
package eligibility

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// The conformance domain set: exactly these four, no more and no fewer.
var parityDomains = []string{"benefits", "leave", "learning", "rewards"}

// DomainCase is one domain's conformance fixture: its request, its
// compiled plan and its fact/rule readers.
type DomainCase struct {
	Name    string
	Request Request
	Plan    CompiledPlan
	Facts   FactReader
	Rules   RuleReader
}

// ParityReport is the sealed cross-domain verdict.
type ParityReport struct {
	Domains  []string
	Statuses map[string]Status
	Digests  map[string]string
	Digest   string
}

func parityDigest(statuses map[string]Status, digests map[string]string) string {
	names := make([]string, 0, len(statuses))
	for name := range statuses {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := []string{"eligibility-parity"}
	for _, name := range names {
		parts = append(parts, name+"\x01"+statuses[name].String()+"\x01"+digests[name])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CheckDomainParity evaluates every domain fixture through the shared
// engine and seals the shared verdict. Divergence refuses.
func CheckDomainParity(ctx context.Context, cases []DomainCase) (ParityReport, error) {
	if len(cases) != len(parityDomains) {
		return ParityReport{}, fmt.Errorf("eligibility: parity covers exactly %d domains, got %d", len(parityDomains), len(cases))
	}
	byName := make(map[string]DomainCase, len(cases))
	for _, c := range cases {
		if _, dup := byName[c.Name]; dup {
			return ParityReport{}, fmt.Errorf("eligibility: duplicate parity domain %q", c.Name)
		}
		byName[c.Name] = c
	}
	for _, name := range parityDomains {
		if _, ok := byName[name]; !ok {
			return ParityReport{}, fmt.Errorf("eligibility: parity omits domain %q", name)
		}
	}
	report := ParityReport{Statuses: map[string]Status{}, Digests: map[string]string{}}
	matters := map[string]string{}
	ruleSets := map[string]map[string]bool{}
	for _, name := range parityDomains {
		c := byName[name]
		if err := c.Request.Validate(); err != nil {
			return ParityReport{}, fmt.Errorf("eligibility: %s request: %v", name, err)
		}
		if c.Plan.Digest == "" {
			return ParityReport{}, fmt.Errorf("eligibility: %s plan is not compiled", name)
		}
		if c.Facts == nil || c.Rules == nil {
			return ParityReport{}, fmt.Errorf("eligibility: %s has no fact or rule reader", name)
		}
		result, err := Evaluate(ctx, c.Facts, c.Rules, c.Request, c.Plan)
		if err != nil {
			return ParityReport{}, fmt.Errorf("eligibility: %s evaluation: %v", name, err)
		}
		if !result.Status.Valid() {
			return ParityReport{}, fmt.Errorf("eligibility: %s status is outside the shared vocabulary", name)
		}
		if err := ValidateBinding(c.Request, result); err != nil {
			return ParityReport{}, fmt.Errorf("eligibility: %s binding: %v", name, err)
		}
		if err := result.EffectiveInterval.Validate(); err != nil {
			return ParityReport{}, fmt.Errorf("eligibility: %s time: %v", name, err)
		}
		if result.Digest == "" {
			return ParityReport{}, fmt.Errorf("eligibility: %s result has no seal", name)
		}
		if prior, dup := matters[c.Request.SubjectMatter.ID]; dup {
			return ParityReport{}, fmt.Errorf("eligibility: subject matter %q shared by %s and %s", c.Request.SubjectMatter.ID, prior, name)
		}
		matters[c.Request.SubjectMatter.ID] = name
		set := map[string]bool{}
		for _, ref := range c.Plan.Rules {
			set[ref.RuleID+"@"+ref.Version] = true
		}
		if len(set) == 0 {
			return ParityReport{}, fmt.Errorf("eligibility: %s plan names no pinned rule", name)
		}
		ruleSets[name] = set
		report.Statuses[name] = result.Status
		report.Digests[name] = result.Digest
	}
	union := map[string]bool{}
	for _, set := range ruleSets {
		for rule := range set {
			union[rule] = true
		}
	}
	if len(union) < 2 {
		return ParityReport{}, fmt.Errorf("eligibility: one mono-rule across four domains retains no domain rules")
	}
	report.Domains = append([]string(nil), parityDomains...)
	report.Digest = parityDigest(report.Statuses, report.Digests)
	return report, nil
}

// Verify recomputes the parity seal over the same cases: a restated
// request, plan or reader set never verifies.
func (report ParityReport) Verify(cases []DomainCase) error {
	if report.Digest == "" {
		return fmt.Errorf("eligibility: parity seal is broken")
	}
	fresh, err := CheckDomainParity(context.Background(), cases)
	if err != nil {
		return err
	}
	if fresh.Digest != report.Digest {
		return fmt.Errorf("eligibility: parity seal is broken")
	}
	return nil
}
