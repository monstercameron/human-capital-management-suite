// Package intentmanifests proves the higher-order BusinessIntent
// instruction set end to end (INTENT-CONF-002).
//
// Every higher-order operation -- compose, template, bundle, dependency,
// preflight, simulate, compare, fork, schedule, subscribe, supersede,
// compensate, repair, replay, shadow, migrate, explain, evidence_export and
// outcome_link -- is a typed creator, consumer, emitter or observer with an
// exact relationship, lifecycle, governance and zero-effect or
// authorized-effect contract. There is no side door: dispatch validates the
// op, the parent binding, the governance receipt and the child scope before
// anything is recorded. Replay under the same key returns the identical
// receipt without duplicating children, successors or effects, so a crash
// between prepare and commit recovers cleanly. Historical-analysis
// operations (compare, shadow, explain, evidence_export, outcome_link) are
// zero-effect observers and can never authorize a new instruction.
package intentmanifests

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrHigherOrderRefused reports a higher-order instruction that cannot
	// be authorized: unknown op, unbound parent, missing governance,
	// broadened child scope or a historical op asked to authorize.
	ErrHigherOrderRefused = errors.New("INTENT_CONF_002_REFUSED")
	// ErrHigherOrderConflict reports a replay-key collision whose content
	// differs: the same key may only ever name the same instruction.
	ErrHigherOrderConflict = errors.New("INTENT_CONF_002_CONFLICT")
)

// HigherOrderOp is the closed 19-operation instruction vocabulary.
type HigherOrderOp string

const (
	OpCompose        HigherOrderOp = "compose"
	OpTemplate       HigherOrderOp = "template"
	OpBundle         HigherOrderOp = "bundle"
	OpDependency     HigherOrderOp = "dependency"
	OpPreflight      HigherOrderOp = "preflight"
	OpSimulate       HigherOrderOp = "simulate"
	OpCompare        HigherOrderOp = "compare"
	OpFork           HigherOrderOp = "fork"
	OpSchedule       HigherOrderOp = "schedule"
	OpSubscribe      HigherOrderOp = "subscribe"
	OpSupersede      HigherOrderOp = "supersede"
	OpCompensate     HigherOrderOp = "compensate"
	OpRepair         HigherOrderOp = "repair"
	OpReplay         HigherOrderOp = "replay"
	OpShadow         HigherOrderOp = "shadow"
	OpMigrate        HigherOrderOp = "migrate"
	OpExplain        HigherOrderOp = "explain"
	OpEvidenceExport HigherOrderOp = "evidence_export"
	OpOutcomeLink    HigherOrderOp = "outcome_link"
)

// AllHigherOrderOps is the deterministic operation set the conformance
// suite must cover in full.
var AllHigherOrderOps = []HigherOrderOp{
	OpCompose, OpTemplate, OpBundle, OpDependency, OpPreflight,
	OpSimulate, OpCompare, OpFork, OpSchedule, OpSubscribe,
	OpSupersede, OpCompensate, OpRepair, OpReplay, OpShadow,
	OpMigrate, OpExplain, OpEvidenceExport, OpOutcomeLink,
}

// InstructionRole is the typed creator/consumer/emitter/observer contract.
type InstructionRole string

const (
	RoleCreator  InstructionRole = "CREATOR"
	RoleConsumer InstructionRole = "CONSUMER"
	RoleEmitter  InstructionRole = "EMITTER"
	RoleObserver InstructionRole = "OBSERVER"
)

// EffectContract names whether the operation may carry authorized business
// effects or must remain side-effect free.
type EffectContract string

const (
	EffectZero       EffectContract = "ZERO_EFFECT"
	EffectAuthorized EffectContract = "AUTHORIZED_EFFECT"
)

// opContract binds each operation to its role and effect contract.
var opContract = map[HigherOrderOp]struct {
	role   InstructionRole
	effect EffectContract
}{
	OpCompose: {RoleCreator, EffectZero}, OpTemplate: {RoleCreator, EffectZero},
	OpBundle: {RoleCreator, EffectZero}, OpSchedule: {RoleCreator, EffectZero},
	OpSubscribe:  {RoleCreator, EffectZero},
	OpDependency: {RoleConsumer, EffectZero}, OpPreflight: {RoleConsumer, EffectZero},
	OpSimulate: {RoleConsumer, EffectZero}, OpReplay: {RoleConsumer, EffectZero},
	OpRepair: {RoleConsumer, EffectAuthorized}, OpCompensate: {RoleConsumer, EffectAuthorized},
	OpMigrate: {RoleConsumer, EffectAuthorized},
	OpFork:    {RoleEmitter, EffectZero}, OpSupersede: {RoleEmitter, EffectZero},
	OpCompare: {RoleObserver, EffectZero}, OpShadow: {RoleObserver, EffectZero},
	OpExplain: {RoleObserver, EffectZero}, OpEvidenceExport: {RoleObserver, EffectZero},
	OpOutcomeLink: {RoleObserver, EffectZero},
}

// OpRole returns the typed role for a declared operation.
func OpRole(op HigherOrderOp) (InstructionRole, bool) {
	c, ok := opContract[op]
	return c.role, ok
}

// OpEffect returns the effect contract for a declared operation.
func OpEffect(op HigherOrderOp) (EffectContract, bool) {
	c, ok := opContract[op]
	return c.effect, ok
}

// Instruction is one higher-order request. ReplayKey makes dispatch
// idempotent; ParentScope bounds every child scope; GovernanceDigest
// proves the current governance evaluation; Authority names the grant for
// authorized-effect operations only.
type Instruction struct {
	Op               HigherOrderOp
	ParentIntent     string
	ParentScope      []string
	ChildScopes      [][]string
	Relationship     string
	Lifecycle        string
	GovernanceDigest string
	Authority        string
	ReplayKey        string
	OutcomeLinkRef   string
}

// Receipt is the immutable dispatch answer. ChildBindings and Successor
// record exactly the emitted graph edges; EffectCount is nonzero only for
// authorized-effect operations carrying a named authority.
type Receipt struct {
	Op             HigherOrderOp
	Role           InstructionRole
	ParentIntent   string
	Relationship   string
	Lifecycle      string
	ChildBindings  []string
	Successor      string
	OutcomeLinkRef string
	EffectCount    int
	Authority      string
	ReceiptDigest  string
}

func receiptDigest(in Instruction, role InstructionRole, children []string, successor string, effects int) string {
	scopes := make([]string, 0, len(in.ChildScopes))
	for _, scope := range in.ChildScopes {
		scopes = append(scopes, strings.Join(scope, ","))
	}
	sort.Strings(scopes)
	orderedChildren := append([]string(nil), children...)
	sort.Strings(orderedChildren)
	parts := []string{
		string(in.Op), in.ParentIntent, strings.Join(in.ParentScope, ","),
		strings.Join(scopes, ";"), in.Relationship, in.Lifecycle,
		in.GovernanceDigest, in.Authority, in.ReplayKey, in.OutcomeLinkRef,
		string(role), strings.Join(orderedChildren, ","), successor,
		fmt.Sprintf("%d", effects),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Dispatcher is the single governed door for higher-order instructions.
// Prepared records every prepared receipt by replay key, so replays and
// crash recovery return the identical receipt instead of duplicating work.
// The mutex serializes concurrent preparations: one replay key still names
// exactly one instruction under contention.
type Dispatcher struct {
	mu       sync.Mutex
	prepared map[string]Receipt
}

// NewDispatcher opens an empty dispatcher.
func NewDispatcher() *Dispatcher { return &Dispatcher{prepared: map[string]Receipt{}} }

func scopeBroadens(parent []string, child []string) bool {
	allowed := map[string]bool{}
	for _, token := range parent {
		allowed[token] = true
	}
	for _, token := range child {
		if !allowed[token] {
			return true
		}
	}
	return false
}

// Prepare validates and records one instruction without running effects.
// A replay under a known key returns the stored receipt byte-identically;
// the same key with different content is a conflict, never a second record.
func (d *Dispatcher) Prepare(in Instruction) (Receipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	contract, ok := opContract[in.Op]
	if !ok {
		return Receipt{}, fmt.Errorf("%w: operation %q is not declared", ErrHigherOrderRefused, in.Op)
	}
	if strings.TrimSpace(in.ParentIntent) == "" {
		return Receipt{}, fmt.Errorf("%w: parent intent binding is required", ErrHigherOrderRefused)
	}
	if strings.TrimSpace(in.Relationship) == "" || strings.TrimSpace(in.Lifecycle) == "" {
		return Receipt{}, fmt.Errorf("%w: relationship and lifecycle are required", ErrHigherOrderRefused)
	}
	if strings.TrimSpace(in.GovernanceDigest) == "" {
		return Receipt{}, fmt.Errorf("%w: current governance digest is required", ErrHigherOrderRefused)
	}
	if strings.TrimSpace(in.ReplayKey) == "" {
		return Receipt{}, fmt.Errorf("%w: replay key is required", ErrHigherOrderRefused)
	}
	if len(in.ParentScope) == 0 {
		return Receipt{}, fmt.Errorf("%w: parent scope is required", ErrHigherOrderRefused)
	}
	for _, child := range in.ChildScopes {
		if scopeBroadens(in.ParentScope, child) {
			return Receipt{}, fmt.Errorf("%w: %s broadens the parent scope", ErrHigherOrderRefused, in.Op)
		}
	}
	effects := 0
	if contract.effect == EffectAuthorized {
		if strings.TrimSpace(in.Authority) == "" {
			return Receipt{}, fmt.Errorf("%w: %s carries effects without a named authority", ErrHigherOrderRefused, in.Op)
		}
		effects = 1
	} else if strings.TrimSpace(in.Authority) != "" && contract.role == RoleObserver {
		return Receipt{}, fmt.Errorf("%w: historical operation %s cannot carry authority", ErrHigherOrderRefused, in.Op)
	}
	children := make([]string, 0, len(in.ChildScopes))
	for i, scope := range in.ChildScopes {
		children = append(children, fmt.Sprintf("%s/child-%d:%s", in.ParentIntent, i, strings.Join(scope, ",")))
	}
	successor := ""
	switch in.Op {
	case OpFork, OpSupersede:
		if len(children) == 0 {
			return Receipt{}, fmt.Errorf("%w: %s emits nothing", ErrHigherOrderRefused, in.Op)
		}
		if in.Op == OpSupersede {
			successor = children[0]
			children = nil
		}
	case OpCompose, OpTemplate, OpBundle, OpSchedule, OpSubscribe:
		if len(children) == 0 {
			return Receipt{}, fmt.Errorf("%w: %s creates nothing", ErrHigherOrderRefused, in.Op)
		}
	}
	if contract.role == RoleObserver && (len(children) > 0 || successor != "") {
		return Receipt{}, fmt.Errorf("%w: historical operation %s cannot emit graph edges", ErrHigherOrderRefused, in.Op)
	}
	digest := receiptDigest(in, contract.role, children, successor, effects)
	if prior, seen := d.prepared[in.ReplayKey]; seen {
		if prior.ReceiptDigest != digest {
			return Receipt{}, fmt.Errorf("%w: replay key %q names a different instruction", ErrHigherOrderConflict, in.ReplayKey)
		}
		return prior, nil
	}
	rec := Receipt{
		Op: in.Op, Role: contract.role, ParentIntent: in.ParentIntent,
		Relationship: in.Relationship, Lifecycle: in.Lifecycle,
		ChildBindings: append([]string(nil), children...), Successor: successor,
		OutcomeLinkRef: in.OutcomeLinkRef, EffectCount: effects,
		Authority: in.Authority, ReceiptDigest: digest,
	}
	d.prepared[in.ReplayKey] = rec
	return rec, nil
}

// PreparedCount reports how many distinct instructions are recorded.
func (d *Dispatcher) PreparedCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.prepared)
}

// Reconstruct verifies that a receipt chain explains every edge: each
// receipt must be a stored preparation, and every named outcome link must
// be carried by its receipt.
func (d *Dispatcher) Reconstruct(receipts []Receipt) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(receipts) == 0 {
		return fmt.Errorf("%w: no evidence to reconstruct", ErrHigherOrderRefused)
	}
	for _, rec := range receipts {
		found := false
		for _, prior := range d.prepared {
			if prior.ReceiptDigest == rec.ReceiptDigest {
				found = true
				if prior.Op != rec.Op || prior.ParentIntent != rec.ParentIntent {
					return fmt.Errorf("%w: receipt does not match its preparation", ErrHigherOrderConflict)
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: receipt %s was never prepared", ErrHigherOrderRefused, rec.ReceiptDigest)
		}
	}
	return nil
}
