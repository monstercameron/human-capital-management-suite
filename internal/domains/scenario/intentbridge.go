// Scenario-to-intent bridge: a validated scenario IntentCompilation crosses
// into the intent pipeline (internal/intent/plan.go) with no field silently
// dropped.
//
// Every CompiledIntent becomes one bridge participant plus one planned
// append: the append stream names the intent key, so the resulting
// TransactionPlan carries exactly the source keys. The write set and the
// dependencies travel as namespaced commit preconditions grouped by key, so
// the plan carries exactly the source write sets and dependencies too. The
// extractors below read them back; the REV-040-02 tests prove the round
// trip over generated compilations.
//
// The bridge only adds scenario-derived entries to a caller-supplied base
// PlanInput. Everything the intent pipeline requires on its own terms (the
// minted proposal digest, governance and conflict snapshots, read baselines,
// idempotency, approvals, revalidation, expiry) stays the caller's
// responsibility, and compilation itself runs the real intent.CompilePlan:
// nothing about the pipeline is mocked or reimplemented here.
package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// Bridge namespaces. Base inputs must not mint entries under these
// prefixes; the extractors treat everything under them as scenario content.
const (
	// BridgeStreamPrefix namespaces one planned-append stream per intent key.
	BridgeStreamPrefix = "scenario.bridge.stream."
	// BridgeParticipantPrefix namespaces one local participant per intent key.
	BridgeParticipantPrefix = "scenario.bridge.participant:"
	// BridgeWriteKindPrefix groups one precondition per write target by key.
	BridgeWriteKindPrefix = "scenario.bridge.write-set:"
	// BridgeDependsKindPrefix groups one precondition per dependency by key.
	BridgeDependsKindPrefix = "scenario.bridge.depends-on:"
	// bridgePayloadPrefix tags the content-bound payload digest of one bridge append.
	bridgePayloadPrefix = "scenario.bridge.payload:"
)

// bridgePayload binds one bridge append to the exact intent content behind
// it: key, proposal, write set and dependencies. Two appends compare equal
// only when the intents behind them do.
func bridgePayload(key, proposal string, writeSet, dependsOn []string) string {
	parts := make([]string, 0, 2+len(writeSet)+len(dependsOn))
	parts = append(parts, key, proposal)
	parts = append(parts, writeSet...)
	parts = append(parts, dependsOn...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return bridgePayloadPrefix + hex.EncodeToString(sum[:])
}

// ToIntentPlanInput converts a validated compilation into intent plan input.
// The returned input is base plus exactly one participant and one append per
// compiled intent and one precondition per write target and dependency; base
// itself is never mutated. An invalid compilation is refused before anything
// is converted.
func (c IntentCompilation) ToIntentPlanInput(base intent.PlanInput) (intent.PlanInput, error) {
	if err := c.Validate(); err != nil {
		return intent.PlanInput{}, fmt.Errorf("scenario: intent bridge refused invalid compilation: %w", err)
	}
	out := base
	out.Participants = slices.Clone(base.Participants)
	out.Appends = slices.Clone(base.Appends)
	out.Preconditions = slices.Clone(base.Preconditions)
	for i, compiled := range c.Intents {
		stream := BridgeStreamPrefix + compiled.Key
		out.Participants = append(out.Participants, intent.PlanParticipant{
			ParticipantID: BridgeParticipantPrefix + compiled.Key,
			StreamID:      stream,
			StorageClass:  "LOCAL_POSTGRES",
			Local:         true,
		})
		out.Appends = append(out.Appends, intent.PlannedAppend{
			StreamID:         stream,
			ExpectedSequence: uint64(i + 1),
			EventType:        compiled.Key,
			PayloadDigest:    bridgePayload(compiled.Key, compiled.Proposal, compiled.WriteSet, compiled.DependsOn),
		})
		for _, target := range compiled.WriteSet {
			out.Preconditions = append(out.Preconditions, intent.CommitPrecondition{
				Kind: BridgeWriteKindPrefix + compiled.Key,
				Ref:  target,
			})
		}
		for _, dep := range compiled.DependsOn {
			out.Preconditions = append(out.Preconditions, intent.CommitPrecondition{
				Kind: BridgeDependsKindPrefix + compiled.Key,
				Ref:  dep,
			})
		}
	}
	return out, nil
}

// CompileIntentPlan converts a validated compilation and compiles it through
// the real intent pipeline entry point. The returned plan carries the same
// keys, write sets and dependencies as the source compilation; use the
// extractors below to read them back.
func (c IntentCompilation) CompileIntentPlan(base intent.PlanInput, ids intent.IDSource) (intent.TransactionPlan, error) {
	in, err := c.ToIntentPlanInput(base)
	if err != nil {
		return intent.TransactionPlan{}, err
	}
	plan, err := intent.CompilePlan(in, ids)
	if err != nil {
		return intent.TransactionPlan{}, fmt.Errorf("scenario: intent pipeline refused bridged compilation: %w", err)
	}
	return plan, nil
}

// BridgedIntentKeys reads the scenario intent keys back out of a bridged
// plan, sorted. Only appends on bridge streams count; the caller's own
// appends never leak in.
func BridgedIntentKeys(plan intent.TransactionPlan) []string {
	var keys []string
	for _, planned := range plan.Appends {
		if key, ok := strings.CutPrefix(planned.StreamID, BridgeStreamPrefix); ok && key != "" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// bridgeGrouped collects one Ref per key for a bridge precondition
// namespace, ensuring every bridged key is present even when it carries no
// entries, so a silently dropped field shows up as a missing key rather
// than a missing map entry.
func bridgeGrouped(plan intent.TransactionPlan, prefix string) map[string][]string {
	out := map[string][]string{}
	for _, planned := range plan.Appends {
		if key, ok := strings.CutPrefix(planned.StreamID, BridgeStreamPrefix); ok && key != "" {
			out[key] = []string{}
		}
	}
	for _, precondition := range plan.Preconditions {
		if key, ok := strings.CutPrefix(precondition.Kind, prefix); ok {
			out[key] = append(out[key], precondition.Ref)
		}
	}
	return out
}

// BridgedIntentWriteSets reads the per-key write sets back out of a bridged
// plan. Every bridged key is present; keys with no write targets cannot
// occur because compilation refuses intents without write sets.
func BridgedIntentWriteSets(plan intent.TransactionPlan) map[string][]string {
	return bridgeGrouped(plan, BridgeWriteKindPrefix)
}

// BridgedIntentDependencies reads the per-key dependencies back out of a
// bridged plan. Every bridged key is present, carrying an empty slice when
// the source intent depends on nothing.
func BridgedIntentDependencies(plan intent.TransactionPlan) map[string][]string {
	return bridgeGrouped(plan, BridgeDependsKindPrefix)
}
