// Leave conformance firewall: NEXT-007 proves the CONF-004
// leave-and-return closure stays a hypothetical conformance fixture.
//
// The check walks a workflow definition the way an auditor would: no
// structural node that could reach a Gate A/B or Phase 2 implementation,
// no capability outside the contract-double namespace, no execution mode
// beyond simulation, no write effect or mutation binding, no UNKNOWN
// default, no live legal lookup behind the hypothetical jurisdiction, and
// no engine-extraction verdict without cross-domain counterexamples.
// Anything else fails closed with exact rule violations; passing the
// firewall never authorizes an implementation, which remains a separate
// decision.
package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Firewall rule codes. Each names one RED-clause prohibition so a failure
// points at the exact contract line it breaks.
const (
	RuleNoStructuralImplementation = "NO_STRUCTURAL_IMPLEMENTATION"
	RuleContractDoublesOnly        = "CONTRACT_DOUBLES_ONLY"
	RuleSimulateOnly               = "SIMULATE_ONLY"
	RuleNoWrites                   = "NO_WRITES"
	RuleNoUnknownDefault           = "NO_UNKNOWN_DEFAULT"
	RulePinnedJurisdiction         = "PINNED_JURISDICTION"
	RuleEngineVerdictClosed        = "ENGINE_VERDICT_CLOSED"
)

// contractDoublePrefix is the only capability namespace a conformance
// closure may invoke. Every other namespace is a runtime, provider or
// legal-delivery implementation until proven otherwise.
const contractDoublePrefix = "hcmnext.conformance."

// HomeDomain is the domain this track reuses engines from. An EXTRACT
// verdict must cite counterexamples outside it.
const HomeDomain = "leave"

// isContractDouble reports whether a capability id is a conformance
// contract double rather than a runtime or provider implementation.
func isContractDouble(id string) bool {
	return strings.HasPrefix(id, contractDoublePrefix)
}

// EngineVerdict is the closed verdict vocabulary for reusability claims.
// Implementation remains a separate decision whatever the verdict.
type EngineVerdict string

const (
	VerdictExtract              EngineVerdict = "EXTRACT"
	VerdictKeepDomainLocal      EngineVerdict = "KEEP_DOMAIN_LOCAL"
	VerdictInsufficientEvidence EngineVerdict = "INSUFFICIENT_EVIDENCE"
)

// Valid reports whether the verdict is declared.
func (v EngineVerdict) Valid() bool {
	switch v {
	case VerdictExtract, VerdictKeepDomainLocal, VerdictInsufficientEvidence:
		return true
	default:
		return false
	}
}

// EngineClaim is one analyst-declared reusability verdict over the closure.
// CounterexampleDomains names the non-leave domains the verdict was tested
// against; an EXTRACT without them is an unproven generalization.
type EngineClaim struct {
	Engine                string
	Verdict               EngineVerdict
	CounterexampleDomains []string
}

// Violation is one firewall finding with the exact rule and node it breaks.
type Violation struct {
	Rule   string
	NodeID string
	Detail string
}

// FirewallError is the typed failure CheckGraph returns. Callers match with
// errors.As; every violation carries its rule so no failure is vague.
type FirewallError struct {
	WorkflowID string
	Violations []Violation
}

// Error implements error.
func (e *FirewallError) Error() string {
	return fmt.Sprintf("leave: conformance firewall rejects %s with %d violation(s), first at rule %s",
		e.WorkflowID, len(e.Violations), e.Violations[0].Rule)
}

// GraphReport is the bound pass result. CapabilityIDs are sorted and unique
// so identical closures always produce identical bytes.
type GraphReport struct {
	WorkflowID    string
	Version       uint32
	NodeCount     int
	CapabilityIDs []string
	VerdictCount  int
	Digest        string
}

// CheckGraph enforces the NEXT-007 firewall over one closure definition and
// its declared engine verdicts. It is pure: it reads the definition, writes
// nothing and calls no provider. A definition that reaches Gate A/B or
// Phase 2 implementation returns a *FirewallError; anything else fails with
// a plain error.
func CheckGraph(def workflow.Definition, engines []EngineClaim) (GraphReport, error) {
	violations := checkNodes(def)
	violations = append(violations, checkExecutionEnvelope(def)...)
	violations = append(violations, checkEngineClaims(engines)...)
	if len(violations) > 0 {
		return GraphReport{}, &FirewallError{WorkflowID: def.WorkflowID, Violations: violations}
	}
	report := GraphReport{
		WorkflowID:    def.WorkflowID,
		Version:       def.Version,
		NodeCount:     len(def.Nodes),
		CapabilityIDs: doubleCapabilities(def),
		VerdictCount:  len(engines),
	}
	report.Digest = report.digest()
	return report, nil
}

func (r GraphReport) digest() string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%d\x00%d\x00%d", r.WorkflowID, r.Version, r.NodeCount, r.VerdictCount)
	for _, id := range r.CapabilityIDs {
		sum.Write([]byte(id + "\x00"))
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func doubleCapabilities(def workflow.Definition) []string {
	seen := make(map[string]struct{})
	for _, node := range def.Nodes {
		if node.Capability == nil {
			continue
		}
		seen[node.Capability.ID] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func checkNodes(def workflow.Definition) []Violation {
	var violations []Violation
	for _, node := range def.Nodes {
		switch node.Type {
		case workflow.StepJoin, workflow.StepSubworkflow, workflow.StepParallel:
			violations = append(violations, Violation{
				Rule:   RuleNoStructuralImplementation,
				NodeID: node.ID,
				Detail: fmt.Sprintf("structural node type %q can reach implementation", node.Type),
			})
		}
		if node.Capability != nil {
			ref := node.Capability
			if !isContractDouble(ref.ID) {
				violations = append(violations, Violation{
					Rule:   RuleContractDoublesOnly,
					NodeID: node.ID,
					Detail: fmt.Sprintf("capability %q is not a conformance contract double", ref.ID),
				})
			}
			if ref.OperationMode != workflow.ModeSimulate {
				violations = append(violations, Violation{
					Rule:   RuleSimulateOnly,
					NodeID: node.ID,
					Detail: fmt.Sprintf("capability %q runs in mode %q, not SIMULATE", ref.ID, ref.OperationMode),
				})
			}
			if ref.IdempotencyKeyMapping != "" || ref.EffectBinding != "" {
				violations = append(violations, Violation{
					Rule:   RuleNoWrites,
					NodeID: node.ID,
					Detail: fmt.Sprintf("capability %q carries a mutation binding", ref.ID),
				})
			}
		}
		if node.DeclaredEffect.IsWrite() {
			violations = append(violations, Violation{
				Rule:   RuleNoWrites,
				NodeID: node.ID,
				Detail: fmt.Sprintf("node declares write effect %q", node.DeclaredEffect),
			})
		}
		if node.Decision != nil {
			if node.Decision.DefaultRoute == "" || node.Decision.DefaultRoute == string(workflow.MissingUnknown) {
				violations = append(violations, Violation{
					Rule:   RuleNoUnknownDefault,
					NodeID: node.ID,
					Detail: "decision defaults to UNKNOWN instead of an explicit route",
				})
			}
			for _, route := range node.Decision.Routes {
				if route.Key == string(workflow.MissingUnknown) {
					violations = append(violations, Violation{
						Rule:   RuleNoUnknownDefault,
						NodeID: node.ID,
						Detail: "decision carries an UNKNOWN route key",
					})
				}
			}
		}
		for _, requirement := range node.RequiredContext {
			if requirement.Kind == "LegalContext" && (!requirement.Pinned || len(requirement.RequiredWatermarks) == 0) {
				violations = append(violations, Violation{
					Rule:   RulePinnedJurisdiction,
					NodeID: node.ID,
					Detail: "unpinned legal context claims live jurisdiction instead of a hypothetical fixture",
				})
			}
		}
	}
	return violations
}

func checkExecutionEnvelope(def workflow.Definition) []Violation {
	var violations []Violation
	for _, mode := range def.DeclaredModes {
		if mode != workflow.ModeSimulate {
			violations = append(violations, Violation{
				Rule:   RuleSimulateOnly,
				Detail: fmt.Sprintf("definition declares execution mode %q", mode),
			})
		}
	}
	if def.TerminalProfile != workflow.TerminalProfileSimulateOnly {
		violations = append(violations, Violation{
			Rule:   RuleSimulateOnly,
			Detail: fmt.Sprintf("terminal profile %q is not SIMULATE_ONLY", def.TerminalProfile),
		})
	}
	return violations
}

func checkEngineClaims(engines []EngineClaim) []Violation {
	var violations []Violation
	for _, claim := range engines {
		if !claim.Verdict.Valid() {
			violations = append(violations, Violation{
				Rule:   RuleEngineVerdictClosed,
				NodeID: claim.Engine,
				Detail: fmt.Sprintf("engine verdict %q is not declared", claim.Verdict),
			})
			continue
		}
		if claim.Verdict != VerdictExtract {
			continue
		}
		external := false
		for _, domain := range claim.CounterexampleDomains {
			if trimmed := strings.TrimSpace(domain); trimmed != "" && trimmed != HomeDomain {
				external = true
			}
		}
		if !external {
			violations = append(violations, Violation{
				Rule:   RuleEngineVerdictClosed,
				NodeID: claim.Engine,
				Detail: "EXTRACT verdict cites no counterexample domain outside leave",
			})
		}
	}
	return violations
}
