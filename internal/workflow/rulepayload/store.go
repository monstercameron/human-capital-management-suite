// Package rulepayload stores immutable, published workflow rule and transform
// programs. A lookup always names the exact reference, version and digest that
// a compiled workflow pinned.
package rulepayload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrInvalidPayload = errors.New("workflow rule payload: invalid payload")
	ErrNotFound       = errors.New("workflow rule payload: published payload not found")
	ErrDigestMismatch = errors.New("workflow rule payload: digest mismatch")
	ErrImmutable      = errors.New("workflow rule payload: published version is immutable")
)

// Kind identifies the closed set of executable payloads this store accepts.
type Kind string

const (
	KindDecisionTable Kind = "DECISION_TABLE"
	KindExpression    Kind = "EXPRESSION"
	KindTransform     Kind = "TRANSFORM_IR"
)

// Payload is one immutable published executable. Exactly one body is set.
// Ref.Kind is RULE for decision tables and expressions; transform IR uses the
// same RULE registry kind until workflow exposes a distinct transform kind.
type Payload struct {
	Ref workflow.Reference
	// BodyRef records the table's intrinsic identity when Ref is an explicitly
	// published alias. Direct publications omit it and require identity equality.
	BodyRef    *workflow.VersionedRef
	Digest     string
	Status     workflow.ReferenceStatus
	Kind       Kind
	Table      *rules.Table
	Expression *rules.CompiledExpression
	Transform  *ir.Program
}

type key struct {
	kind    workflow.ReferenceKind
	id      string
	version string
}

// Store owns published payload values. It has no package-global state; callers
// compose it into the compiler and simulator that share a publication set.
type Store struct {
	mu       sync.RWMutex
	payloads map[key]Payload
}

func New() *Store { return &Store{payloads: make(map[key]Payload)} }

// Marshal returns the stable typed persistence envelope for one payload. Its
// body digest is recomputed before bytes are emitted.
func Marshal(payload Payload) ([]byte, error) {
	if payload.Status == "" {
		payload.Status = workflow.ReferencePublished
	}
	if err := validateKind(payload); err != nil {
		return nil, err
	}
	frozen, err := freeze(payload)
	if err != nil {
		return nil, err
	}
	digest, err := payloadDigest(frozen)
	if err != nil {
		return nil, err
	}
	if payload.Digest != "" && payload.Digest != digest {
		return nil, ErrDigestMismatch
	}
	frozen.Digest = digest
	return json.Marshal(frozen)
}

// Unmarshal decodes a stored envelope, validates the bounded IR and verifies
// the exact payload digest before making it available to a resolver.
func Unmarshal(raw []byte) (Payload, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload Payload
	if err := decoder.Decode(&payload); err != nil {
		return Payload{}, fmt.Errorf("%w: decode: %v", ErrInvalidPayload, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Payload{}, fmt.Errorf("%w: trailing JSON value", ErrInvalidPayload)
	}
	if err := validateKind(payload); err != nil {
		return Payload{}, err
	}
	actual, err := payloadDigest(payload)
	if err != nil {
		return Payload{}, err
	}
	if actual != payload.Digest {
		return Payload{}, fmt.Errorf("%w: declared %s, computed %s", ErrDigestMismatch, payload.Digest, actual)
	}
	return freeze(payload)
}

// Publish validates and freezes a payload. Republishing identical bytes is
// idempotent; a different body under the same reference and version is refused.
func (s *Store) Publish(payload Payload) (workflow.ResolvedReference, error) {
	if s == nil {
		return workflow.ResolvedReference{}, fmt.Errorf("%w: nil store", ErrInvalidPayload)
	}
	if payload.Ref.ID == "" || payload.Ref.Version == "" || payload.Ref.Kind == "" {
		return workflow.ResolvedReference{}, fmt.Errorf("%w: reference identity is required", ErrInvalidPayload)
	}
	if payload.Status == "" {
		payload.Status = workflow.ReferencePublished
	}
	if payload.Status != workflow.ReferencePublished && payload.Status != workflow.ReferenceDeprecated {
		return workflow.ResolvedReference{}, fmt.Errorf("%w: status %q cannot be published", ErrInvalidPayload, payload.Status)
	}
	if err := validateKind(payload); err != nil {
		return workflow.ResolvedReference{}, err
	}
	payload, err := freeze(payload)
	if err != nil {
		return workflow.ResolvedReference{}, err
	}
	actual, err := payloadDigest(payload)
	if err != nil {
		return workflow.ResolvedReference{}, err
	}
	if payload.Digest != "" && payload.Digest != actual {
		return workflow.ResolvedReference{}, fmt.Errorf("%w: declared %s, computed %s", ErrDigestMismatch, payload.Digest, actual)
	}
	payload.Digest = actual
	k := key{payload.Ref.Kind, payload.Ref.ID, payload.Ref.Version}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.payloads[k]; ok {
		if existing.Digest != payload.Digest || existing.Kind != payload.Kind {
			return workflow.ResolvedReference{}, fmt.Errorf("%w: %s@%s already has digest %s", ErrImmutable, payload.Ref.ID, payload.Ref.Version, existing.Digest)
		}
		return reference(existing), nil
	}
	s.payloads[k] = payload
	return reference(payload), nil
}

// Resolve returns a defensive copy only when all three identity components
// match. An empty digest is never treated as a wildcard.
func (s *Store) Resolve(ref workflow.Reference, digest string) (Payload, error) {
	if s == nil || ref.ID == "" || ref.Version == "" || ref.Kind == "" || digest == "" {
		return Payload{}, ErrNotFound
	}
	s.mu.RLock()
	payload, ok := s.payloads[key{ref.Kind, ref.ID, ref.Version}]
	s.mu.RUnlock()
	if !ok {
		return Payload{}, ErrNotFound
	}
	if payload.Digest != digest {
		return Payload{}, fmt.Errorf("%w: requested %s, published %s", ErrDigestMismatch, digest, payload.Digest)
	}
	return freeze(payload)
}

// ResolveReference implements workflow.ReferenceResolver against the same
// publication set used to resolve executable bodies.
func (s *Store) ResolveReference(ref workflow.Reference) (workflow.ResolvedReference, bool) {
	if s == nil {
		return workflow.ResolvedReference{}, false
	}
	s.mu.RLock()
	payload, ok := s.payloads[key{ref.Kind, ref.ID, ref.Version}]
	s.mu.RUnlock()
	if !ok {
		return workflow.ResolvedReference{}, false
	}
	return reference(payload), true
}

func reference(p Payload) workflow.ResolvedReference {
	return workflow.ResolvedReference{Kind: p.Ref.Kind, ID: p.Ref.ID, Version: p.Ref.Version, Digest: p.Digest, Status: p.Status}
}

func validateKind(p Payload) error {
	count := 0
	if p.Table != nil {
		count++
	}
	if p.Expression != nil {
		count++
	}
	if p.Transform != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: exactly one executable body is required", ErrInvalidPayload)
	}
	switch p.Kind {
	case KindDecisionTable:
		if p.Ref.Kind != workflow.RefRule || p.Table == nil {
			break
		}
		if p.BodyRef == nil {
			if p.Ref.ID != p.Table.ID || p.Ref.Version != p.Table.Version {
				return fmt.Errorf("%w: decision table identity differs from its reference without an explicit body alias", ErrInvalidPayload)
			}
		} else if p.BodyRef.ID != p.Table.ID || p.BodyRef.Version != p.Table.Version {
			return fmt.Errorf("%w: explicit table body alias does not match its body identity", ErrInvalidPayload)
		}
		if err := p.Table.Validate(); err != nil {
			return fmt.Errorf("%w: decision table: %v", ErrInvalidPayload, err)
		}
		return nil
	case KindExpression:
		if p.Ref.Kind != workflow.RefRule || p.Expression == nil || p.Expression.Version != p.Ref.Version || p.BodyRef != nil {
			break
		}
		return nil
	case KindTransform:
		if p.Ref.Kind != workflow.RefTransform || p.Transform == nil || p.Transform.IRVersion <= 0 || p.BodyRef != nil {
			break
		}
		if err := p.Transform.Validate(); err != nil {
			return fmt.Errorf("%w: transform IR: %v", ErrInvalidPayload, err)
		}
		return nil
	}
	return fmt.Errorf("%w: kind, reference and body do not agree", ErrInvalidPayload)
}

func payloadDigest(p Payload) (string, error) {
	switch p.Kind {
	case KindDecisionTable:
		return p.Table.Digest()
	case KindExpression:
		if p.Expression.Digest == "" {
			return "", fmt.Errorf("%w: expression has no compiled digest", ErrInvalidPayload)
		}
		return p.Expression.Digest, nil
	case KindTransform:
		return p.Transform.Digest()
	default:
		return "", fmt.Errorf("%w: unknown kind %q", ErrInvalidPayload, p.Kind)
	}
}

func freeze(p Payload) (Payload, error) {
	if p.BodyRef != nil {
		bodyRef := *p.BodyRef
		p.BodyRef = &bodyRef
	}
	if p.Table != nil {
		t := *p.Table
		t.Inputs = append([]rules.Column(nil), t.Inputs...)
		t.Outputs = append([]rules.Column(nil), t.Outputs...)
		t.Rows = append([]rules.Row(nil), t.Rows...)
		for i := range t.Rows {
			t.Rows[i].Conditions = append([]rules.Condition(nil), t.Rows[i].Conditions...)
			t.Rows[i].Outputs = cloneValues(t.Rows[i].Outputs)
		}
		p.Table = &t
	}
	if p.Expression != nil {
		e := *p.Expression
		e.IR.Nodes = append([]rules.IRNode(nil), e.IR.Nodes...)
		for i := range e.IR.Nodes {
			e.IR.Nodes[i].Children = append([]int(nil), e.IR.Nodes[i].Children...)
		}
		e.Inputs = append([]rules.Input(nil), e.Inputs...)
		e.Dependencies = append([]rules.Dependency(nil), e.Dependencies...)
		p.Expression = &e
	}
	if p.Transform != nil {
		program := *p.Transform
		program.Instructions = append([]ir.Instruction(nil), program.Instructions...)
		for i := range program.Instructions {
			program.Instructions[i].Sources = append(program.Instructions[i].Sources[:0:0], program.Instructions[i].Sources...)
		}
		program.Dependencies = append([]string(nil), program.Dependencies...)
		p.Transform = &program
	}
	return p, nil
}

func cloneValues(values []rules.Value) []rules.Value {
	cloned := append([]rules.Value(nil), values...)
	for i, value := range cloned {
		if items, ok := value.List(); ok {
			cloned[i] = rules.ListValue(items...)
		}
	}
	return cloned
}
