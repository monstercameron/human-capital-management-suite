package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"
)

// Executable is a validated, immutable-by-convention release of the
// processing inventory. The release contains references and metadata only;
// it never carries workforce payloads.
type Executable struct {
	Inventory Inventory
	Digest    string
}

// ValidateExecutable applies the stronger publication checks needed by
// PRIV-001 and returns a canonical, content-addressed release.
func ValidateExecutable(input Inventory) (Executable, error) {
	pack, err := CurrentTransferRulePack()
	if err != nil {
		return Executable{}, err
	}
	return ValidateExecutableWithTransferPolicy(input, pack, time.Now().UTC())
}

// ValidateExecutableWithTransferPolicy applies the executable publication
// checks against an explicit, versioned transfer policy and evaluation time.
func ValidateExecutableWithTransferPolicy(input Inventory, pack TransferRulePack, asOf time.Time) (Executable, error) {
	if err := input.ValidateWithTransferPolicy(pack, asOf); err != nil {
		return Executable{}, err
	}
	if len(input.Activities) == 0 || len(input.Flows) == 0 {
		return Executable{}, fmt.Errorf("%w: executable inventory needs an activity and a flow", ErrInvalid)
	}

	activities := make(map[string]ProcessingActivity, len(input.Activities))
	for _, activity := range input.Activities {
		activities[activity.ID+"@"+activity.Version] = activity
	}
	flowCount := make(map[string]int, len(input.Activities))
	for _, flow := range input.Flows {
		key := flow.ActivityID + "@" + flow.Version
		activity := activities[key]
		flowCount[key]++
		for _, category := range flow.DataCategories {
			if !contains(activity.DataCategories, category) {
				return Executable{}, fmt.Errorf("%w: flow %q category %q exceeds activity scope", ErrInvalid, flow.ID, category)
			}
		}
		for _, region := range flow.TransferRegions {
			if !contains(activity.Regions, region) {
				return Executable{}, fmt.Errorf("%w: flow %q region %q exceeds activity scope", ErrInvalid, flow.ID, region)
			}
		}
		if flow.Controller != activity.Controller || flow.Processor != activity.Processor {
			return Executable{}, fmt.Errorf("%w: flow %q authority differs from activity", ErrInvalid, flow.ID)
		}
	}
	for key, activity := range activities {
		if activity.Status == StatusApproved && flowCount[key] == 0 {
			return Executable{}, fmt.Errorf("%w: approved activity %q has no executable flow", ErrInvalid, key)
		}
	}

	canonical := canonicalInventory(input)
	b, err := json.Marshal(canonical)
	if err != nil {
		return Executable{}, fmt.Errorf("%w: canonical inventory: %v", ErrInvalid, err)
	}
	sum := sha256.Sum256(b)
	return Executable{Inventory: canonical, Digest: hex.EncodeToString(sum[:])}, nil
}

// Explain returns a redaction-safe release summary.
func (e Executable) Explain() string {
	return fmt.Sprintf("processing inventory activities=%d flows=%d occurrences=%d digest=%s",
		len(e.Inventory.Activities), len(e.Inventory.Flows), len(e.Inventory.Occurrences), e.Digest)
}

func canonicalInventory(input Inventory) Inventory {
	out := input
	out.Activities = slices.Clone(input.Activities)
	for i := range out.Activities {
		out.Activities[i].Obligations = slices.Clone(input.Activities[i].Obligations)
		out.Activities[i].Subprocessors = sortedClone(out.Activities[i].Subprocessors)
		out.Activities[i].DataSubjects = sortedClone(out.Activities[i].DataSubjects)
		out.Activities[i].DataCategories = sortedClone(out.Activities[i].DataCategories)
		out.Activities[i].Systems = sortedClone(out.Activities[i].Systems)
		out.Activities[i].Recipients = sortedClone(out.Activities[i].Recipients)
		out.Activities[i].Regions = sortedClone(out.Activities[i].Regions)
		out.Activities[i].SecurityControls = sortedClone(out.Activities[i].SecurityControls)
		out.Activities[i].TransferAssessmentRefs = sortedClone(out.Activities[i].TransferAssessmentRefs)
		out.Activities[i].TransferPartyRoles = slices.Clone(input.Activities[i].TransferPartyRoles)
		sort.Slice(out.Activities[i].TransferPartyRoles, func(a, b int) bool {
			if out.Activities[i].TransferPartyRoles[a].Party != out.Activities[i].TransferPartyRoles[b].Party {
				return out.Activities[i].TransferPartyRoles[a].Party < out.Activities[i].TransferPartyRoles[b].Party
			}
			return out.Activities[i].TransferPartyRoles[a].Role < out.Activities[i].TransferPartyRoles[b].Role
		})
		sort.Slice(out.Activities[i].Obligations, func(a, b int) bool {
			x, y := out.Activities[i].Obligations[a], out.Activities[i].Obligations[b]
			if x.Kind != y.Kind {
				return x.Kind < y.Kind
			}
			if x.Jurisdiction != y.Jurisdiction {
				return x.Jurisdiction < y.Jurisdiction
			}
			return x.RulePackRelease < y.RulePackRelease
		})
	}
	sort.Slice(out.Activities, func(i, j int) bool {
		return out.Activities[i].ID+"@"+out.Activities[i].Version < out.Activities[j].ID+"@"+out.Activities[j].Version
	})
	out.Flows = slices.Clone(input.Flows)
	for i := range out.Flows {
		out.Flows[i].DataCategories = sortedClone(out.Flows[i].DataCategories)
		out.Flows[i].Operations = sortedClone(out.Flows[i].Operations)
		out.Flows[i].TransferRegions = sortedClone(out.Flows[i].TransferRegions)
		out.Flows[i].ContractRefs = sortedClone(out.Flows[i].ContractRefs)
		out.Flows[i].Safeguards = sortedClone(out.Flows[i].Safeguards)
		out.Flows[i].TransferRegimes = slices.Clone(input.Flows[i].TransferRegimes)
		sort.Slice(out.Flows[i].TransferRegimes, func(a, b int) bool { return out.Flows[i].TransferRegimes[a] < out.Flows[i].TransferRegimes[b] })
		out.Flows[i].MechanismEvidence = slices.Clone(input.Flows[i].MechanismEvidence)
		sort.Slice(out.Flows[i].MechanismEvidence, func(a, b int) bool {
			if out.Flows[i].MechanismEvidence[a].MechanismID != out.Flows[i].MechanismEvidence[b].MechanismID {
				return out.Flows[i].MechanismEvidence[a].MechanismID < out.Flows[i].MechanismEvidence[b].MechanismID
			}
			return out.Flows[i].MechanismEvidence[a].DocumentRef < out.Flows[i].MechanismEvidence[b].DocumentRef
		})
		out.Flows[i].SecurityControls = sortedClone(out.Flows[i].SecurityControls)
	}
	sort.Slice(out.Flows, func(i, j int) bool { return out.Flows[i].ID < out.Flows[j].ID })
	out.TransferImpactAssessments = slices.Clone(input.TransferImpactAssessments)
	sort.Slice(out.TransferImpactAssessments, func(i, j int) bool {
		return out.TransferImpactAssessments[i].ID < out.TransferImpactAssessments[j].ID
	})
	out.Occurrences = slices.Clone(input.Occurrences)
	for i := range out.Occurrences {
		out.Occurrences[i].DataCategories = sortedClone(out.Occurrences[i].DataCategories)
	}
	sort.Slice(out.Occurrences, func(i, j int) bool { return out.Occurrences[i].ID < out.Occurrences[j].ID })
	return out
}

func sortedClone(values []string) []string {
	out := slices.Clone(values)
	sort.Strings(out)
	return out
}
