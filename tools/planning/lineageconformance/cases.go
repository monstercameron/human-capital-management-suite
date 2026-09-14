package lineageconformance

import (
	"fmt"
	"slices"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

// CaseInput is everything case generation reads. Every field is registry
// data: the SLICE-016 witness report, the compiled definitions and their
// MODEL-016 bindings.
type CaseInput struct {
	Witnesses   closurewitness.Report
	Definitions []intent.Definition
	Bindings    []intent.Binding
}

// triggerInitiators are the initiator kinds that are not a live caller:
// an intent they raise exists because something fired, so its lineage
// must begin with the firing.
func triggerInitiators() []intent.Initiator {
	return []intent.Initiator{
		intent.InitiatorIntegration, intent.InitiatorSchedule,
		intent.InitiatorRule, intent.InitiatorSystemEvent,
	}
}

// notApplicableMarker is the exact rule value definitions use to declare a
// rule absent.
const notApplicableMarker = "NOT_APPLICABLE"

// ChildCaseID names the CHILD case for one parent/child pair.
func ChildCaseID(parent, child string) string { return parent + ">" + child }

// TriggerCaseID names the TRIGGER case for one definition and initiator.
func TriggerCaseID(definition, trigger string) string { return definition + "@" + trigger }

// GenerateCases derives every lineage case from the witnesses, sorted by
// case ID, plus findings for any generation gap (a witness with no compiled
// definition, a composite child that is not itself an accepted definition).
// A gap never drops a case: the case is emitted with every chain link
// required so it cannot pass by omission.
func GenerateCases(in CaseInput) ([]Case, []Finding) {
	defs := make(map[string]intent.Definition, len(in.Definitions))
	for _, d := range in.Definitions {
		defs[d.Ref.String()] = d
	}
	witnesses := make(map[string]closurewitness.Witness, len(in.Witnesses.Witnesses))
	for _, w := range in.Witnesses.Witnesses {
		witnesses[w.Definition] = w
	}
	var cases []Case
	var findings []Finding
	for _, w := range in.Witnesses.Witnesses {
		def, ok := defs[w.Definition]
		root := newCase(w.Definition, w, ok, def)
		root.ID = w.Definition
		root.Path = PathRoot
		if !ok {
			findings = append(findings, Finding{Case: root.ID, Code: CodeDefinitionAbsent,
				Detail: "closure witness names a definition with no compiled intent.Definition; every chain link stays required"})
		}
		cases = append(cases, root)
		if !ok {
			continue
		}
		for _, initiator := range triggerInitiators() {
			if !def.AllowsInitiator(initiator) {
				continue
			}
			tc := newCase(w.Definition, w, true, def)
			tc.ID = TriggerCaseID(w.Definition, initiator.String())
			tc.Path = PathTrigger
			tc.Trigger = initiator.String()
			tc.Required = append([]Link{LinkCausation}, tc.Required...)
			cases = append(cases, tc)
		}
	}
	for _, b := range in.Bindings {
		parent := b.Definition.String()
		if _, accepted := witnesses[parent]; !accepted {
			continue
		}
		for _, childRef := range b.ChildDefinitions {
			child := childRef.String()
			cw, childAccepted := witnesses[child]
			cdef, childDefined := defs[child]
			cc := newCase(child, cw, childDefined, cdef)
			cc.ID = ChildCaseID(parent, child)
			cc.Path = PathChild
			cc.Parent = parent
			cc.Required = append([]Link{LinkCausation}, cc.Required...)
			if !childAccepted {
				findings = append(findings, Finding{Case: cc.ID, Code: CodeChildNotAccepted,
					Detail: fmt.Sprintf("composite child %s of %s has no closure witness", child, parent)})
			}
			cases = append(cases, cc)
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	sortFindings(findings)
	return cases, findings
}

func newCase(definition string, w closurewitness.Witness, defined bool, def intent.Definition) Case {
	c := Case{Definition: definition, WitnessDigest: w.Digest, WitnessResult: w.Result}
	if !defined {
		c.Required = ChainLinks()
		return c
	}
	c.DisplayName = def.DisplayName
	c.Family = def.Family.String()
	c.Required, c.NotApplicable = RequiredLinks(def)
	return c
}

// RequiredLinks derives the links one definition must carry and the ones
// it declares absent. A link is NOT_APPLICABLE only on the definition's own
// declaration, and the reason quotes the declaring field:
//   - PROPOSAL when proposal_binding_rule is NOT_APPLICABLE;
//   - TRANSACTION, OUTBOX, EFFECT, OBSERVATION and RECONCILIATION when the
//     side-effect profile does not mutate;
//   - REPAIR when compensation_rule is NOT_APPLICABLE;
//   - CORRECTION when the profile does not mutate and neither a correction
//     nor a compensation rule is declared.
//
// INTENT, WORKFLOW, EVENT and PROJECTION are always required.
func RequiredLinks(def intent.Definition) ([]Link, []NotApplicable) {
	absent := map[Link]string{}
	if def.ProposalBindingRule == notApplicableMarker {
		absent[LinkProposal] = "proposal_binding_rule=NOT_APPLICABLE"
	}
	if !def.SideEffect.Mutates() {
		reason := "side_effect_profile=" + def.SideEffect.String()
		for _, l := range []Link{LinkTransaction, LinkOutbox, LinkEffect, LinkObservation, LinkReconciliation} {
			absent[l] = reason
		}
		if def.CorrectionRule == "" && def.CompensationRule == notApplicableMarker {
			absent[LinkCorrection] = reason + "; correction_rule empty; compensation_rule=NOT_APPLICABLE"
		}
	}
	if def.CompensationRule == notApplicableMarker {
		absent[LinkRepair] = "compensation_rule=NOT_APPLICABLE"
	}
	var required []Link
	var na []NotApplicable
	for _, l := range ChainLinks() {
		if reason, ok := absent[l]; ok {
			na = append(na, NotApplicable{Link: l, Reason: reason})
			continue
		}
		required = append(required, l)
	}
	return required, na
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Case != b.Case {
			return a.Case < b.Case
		}
		if a.Link.rank() != b.Link.rank() {
			return a.Link.rank() < b.Link.rank()
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Record < b.Record
	})
}

// requires reports whether a case requires a link.
func (c Case) requires(l Link) bool { return slices.Contains(c.Required, l) }
