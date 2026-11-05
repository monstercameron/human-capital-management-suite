package simulate

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// RecordedKind names the class of would-be effect a test run captured instead
// of performing. The kind derives from the node's compiled effect class, so a
// record can never claim a milder kind than the mutation it stands in for.
type RecordedKind string

const (
	// RecordedWrite is a would-be domain write (INTERNAL_MUTATION).
	RecordedWrite RecordedKind = "write"
	// RecordedExternalCall is a would-be call past the domain boundary
	// (EXTERNAL_MUTATION and IRREVERSIBLE_EXTERNAL_MUTATION).
	RecordedExternalCall RecordedKind = "external-call"
)

// RecordedEffect is one would-be effect a recording run captured: the node,
// the exact capability version it would have invoked, the typed inputs the
// mappings produced, the outputs the node declared, and a deterministic
// preview of what the invocation would have carried.
type RecordedEffect struct {
	NodeID        string                 `json:"node_id"`
	Capability    capability.Key         `json:"capability"`
	Kind          RecordedKind           `json:"kind"`
	EffectClass   capability.EffectClass `json:"effect_class"`
	EffectBinding string                 `json:"effect_binding,omitempty"`
	Inputs        Bag                    `json:"inputs"`
	Outputs       []workflow.Field       `json:"outputs"`
	InputDigest   string                 `json:"input_digest"`
	Preview       string                 `json:"preview"`
}

// Recorder receives the would-be effects a recording run captures, in walk
// order. A recording profile without a recorder is refused: a would-be effect
// that is captured nowhere is an effect the test silently dropped.
type Recorder interface {
	Record(RecordedEffect)
}

// Recording is an in-memory Recorder that retains every captured effect. It
// is safe for sequential use by one run; a run is one deterministic sequence.
type Recording struct {
	mu      sync.Mutex
	effects []RecordedEffect
}

// NewRecording returns an empty recording.
func NewRecording() *Recording { return &Recording{} }

// Record implements Recorder.
func (r *Recording) Record(effect RecordedEffect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effects = append(r.effects, effect)
}

// Effects returns the captured effects in walk order.
func (r *Recording) Effects() []RecordedEffect {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RecordedEffect(nil), r.effects...)
}

// RecordingProfile is the WF-TEST-001 execution contract for one test run: the
// intent execution contract the run executes under, plus the recorder that
// captures every would-be effect. The contract must carry the zero effect
// ceiling: a test run records what it would have done and never commits.
type RecordingProfile struct {
	Contract intent.ModeContract
	Recorder Recorder
}

// active reports whether the profile admits recording: a zero-effect-ceiling
// contract and a recorder to capture into.
func (p RecordingProfile) active() bool {
	return p.Recorder != nil && p.Contract.EffectCeiling == intent.EffectClassZero && p.Contract.ZeroEffect()
}

// check refuses a profile that cannot record honestly.
func (p RecordingProfile) check() error {
	if p.Contract.EffectCeiling != intent.EffectClassZero || !p.Contract.ZeroEffect() {
		return refuse(CodeRecordingRefused, "", "recording demands the zero effect ceiling, contract allows %s", p.Contract.EffectCeiling)
	}
	if p.Recorder == nil {
		return refuse(CodeRecordingRefused, "", "recording demands a recorder to capture would-be effects")
	}
	return nil
}

// kindOf resolves the recorded kind from a write effect class. Read classes
// are never recorded, so resolving one is a caller bug, reported as such.
func kindOf(class capability.EffectClass) (RecordedKind, error) {
	switch class {
	case capability.EffectInternalMutation:
		return RecordedWrite, nil
	case capability.EffectExternalMutation, capability.EffectIrreversibleExternalMutation:
		return RecordedExternalCall, nil
	default:
		return "", fmt.Errorf("simulate: effect class %s is not a recordable write", class)
	}
}

// previewOf renders the deterministic preview of a would-be invocation: the
// node, the exact capability version, the sorted typed inputs and the
// declared outputs it would have produced.
func previewOf(node workflow.CompiledNode, key capability.Key, inputs Bag) string {
	var b strings.Builder
	fmt.Fprintf(&b, "would invoke %s/v%d at node %s with ", key.ID, key.Version, node.ID)
	paths := inputs.Paths()
	sort.Strings(paths)
	for i, path := range paths {
		if i > 0 {
			b.WriteString(", ")
		}
		value, _ := inputs.Get(path)
		fmt.Fprintf(&b, "%s=%s:%s", path, value.Type.String(), value.Text)
	}
	if len(node.Outputs) > 0 {
		names := make([]string, 0, len(node.Outputs))
		for _, field := range node.Outputs {
			names = append(names, field.Path)
		}
		fmt.Fprintf(&b, " declaring outputs [%s]", strings.Join(names, ", "))
	}
	return b.String()
}

// recordEffect builds the typed record for a write-class node from its
// resolved inputs. It never invokes anything: the gateway is not consulted,
// so a recording cannot reach a live write path through any handler.
func recordEffect(node workflow.CompiledNode, key capability.Key, inputs Bag) (RecordedEffect, error) {
	kind, err := kindOf(node.EffectClass)
	if err != nil {
		return RecordedEffect{}, err
	}
	binding := ""
	if node.Capability != nil {
		binding = node.Capability.EffectBinding
	}
	return RecordedEffect{
		NodeID:        node.ID,
		Capability:    key,
		Kind:          kind,
		EffectClass:   node.EffectClass,
		EffectBinding: binding,
		Inputs:        inputs.clone(),
		Outputs:       append([]workflow.Field(nil), node.Outputs...),
		InputDigest:   digestBag(inputs),
		Preview:       previewOf(node, key, inputs),
	}, nil
}
