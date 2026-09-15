package workflow

import (
	"fmt"
	"strings"
)

// NodeCancellation is how one compiled node answers a cancellation request,
// derived only from what the node declares: its effect class and its
// published compensation reference.
type NodeCancellation string

// Node cancellation classes.
const (
	// CancelFree nodes produce no effect (PURE, READ_ONLY, approvals, waits,
	// decisions, ends): stopping them leaves nothing behind.
	CancelFree NodeCancellation = "CANCEL_FREE"
	// CancelCompensable nodes write, and declare the published compensation
	// that releases a produced effect.
	CancelCompensable NodeCancellation = "COMPENSABLE"
	// CancelIrreversible nodes write and declare no compensation: a produced
	// effect cannot be released by cancelling.
	CancelIrreversible NodeCancellation = "IRREVERSIBLE"
)

// CancellationSemantics is one node's declared cancellation semantics.
type CancellationSemantics struct {
	NodeID       string           `json:"node_id"`
	Class        NodeCancellation `json:"class"`
	EffectClass  string           `json:"effect_class"`
	Compensation string           `json:"compensation,omitempty"`
}

// CancellationSemanticsOf derives node's cancellation semantics from its
// compiled declarations. A node declaring an effect class this runtime does
// not know is refused rather than guessed cancellation-free.
func CancellationSemanticsOf(node CompiledNode) (CancellationSemantics, error) {
	out := CancellationSemantics{NodeID: node.ID, EffectClass: string(node.EffectClass)}
	if node.EffectClass != "" && !node.EffectClass.Valid() {
		return CancellationSemantics{}, fmt.Errorf("workflow: node %s declares unknown effect class %q", node.ID, node.EffectClass)
	}
	switch {
	case !node.EffectClass.IsWrite():
		out.Class = CancelFree
	case node.CompensationRef != nil && strings.TrimSpace(node.CompensationRef.ID) != "":
		out.Class = CancelCompensable
		out.Compensation = node.CompensationRef.ID
		if node.CompensationRef.Version != "" {
			out.Compensation += "@" + node.CompensationRef.Version
		}
	default:
		out.Class = CancelIrreversible
	}
	return out, nil
}

// CancellationSemantics declares every node's cancellation semantics in plan
// order. It fails when any node's semantics cannot be derived, so a plan is
// never cancelled on a partial declaration.
func (p *CompiledWorkflow) CancellationSemantics() ([]CancellationSemantics, error) {
	if p == nil {
		return nil, fmt.Errorf("workflow: cancellation semantics need a compiled plan")
	}
	out := make([]CancellationSemantics, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		sem, err := CancellationSemanticsOf(node)
		if err != nil {
			return nil, err
		}
		out = append(out, sem)
	}
	return out, nil
}

// Effect projects one execution of the node onto the effect record
// [DecideCancellation] judges. settled reports that the node's effect is
// known to have been produced; an unsettled execution (in flight, or failed
// after recording an effect reference) is ambiguous. A cancellation-free node
// has no effect record at all.
func (s CancellationSemantics) Effect(id string, settled bool) (EffectRecord, bool) {
	if s.Class == CancelFree || s.Class == "" {
		return EffectRecord{}, false
	}
	if !settled {
		return EffectRecord{ID: id, Ambiguous: true}, true
	}
	if s.Class == CancelCompensable {
		return EffectRecord{ID: id, Compensation: s.Compensation}, true
	}
	return EffectRecord{ID: id}, true
}
