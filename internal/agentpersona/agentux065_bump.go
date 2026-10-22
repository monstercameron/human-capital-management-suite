package agentpersona

import (
	"reflect"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// RequiredBump is the smallest label a change from prev to next may publish
// under (AGENTUX-065). Wording alone — instructions, guidance, display text,
// examples, ownership — is a patch. Anything that changes skills, audience or
// documents, or any other capability of the agent, needs at least a minor.
// The profiles are versions of one persona; derived outputs (digests, data
// classes) and the version sequence itself are not compared.
func RequiredBump(prev, next PersonaProfile) semver.Bump {
	if pinsChanged(prev.SkillPins, next.SkillPins) ||
		!reflect.DeepEqual(prev.Audience, next.Audience) ||
		referencesChanged(prev.DocumentReferences, next.DocumentReferences) ||
		prev.TierCeiling != next.TierCeiling ||
		!equalSets(prev.ConversationKinds, next.ConversationKinds) ||
		!equalSets(prev.ChannelClasses, next.ChannelClasses) ||
		!equalSets(prev.AllowedPlacementClasses, next.AllowedPlacementClasses) ||
		prev.AlwaysPrivate != next.AlwaysPrivate ||
		!reflect.DeepEqual(prev.ConversationTierCeilings, next.ConversationTierCeilings) ||
		!reflect.DeepEqual(prev.Template, next.Template) ||
		prev.EvalSuiteRef != next.EvalSuiteRef ||
		prev.EvalLimits != next.EvalLimits {
		return semver.BumpMinor
	}
	return semver.BumpPatch
}

// pinsChanged compares pinned skills as a set: reordering pins without
// changing them is not a change.
func pinsChanged(prev, next []agentskills.SkillPin) bool {
	byID := func(pins []agentskills.SkillPin) []agentskills.SkillPin {
		out := append([]agentskills.SkillPin(nil), pins...)
		sort.Slice(out, func(i, j int) bool {
			if out[i].ID != out[j].ID {
				return out[i].ID < out[j].ID
			}
			if out[i].Version != out[j].Version {
				return out[i].Version < out[j].Version
			}
			return out[i].Digest < out[j].Digest
		})
		return out
	}
	return !reflect.DeepEqual(byID(prev), byID(next))
}

// referencesChanged compares document references keyed by document: a
// repinned, added or removed document is a change, a reordered list is not.
func referencesChanged(prev, next []agentdocref.Reference) bool {
	byDocument := func(refs []agentdocref.Reference) []agentdocref.Reference {
		out := append([]agentdocref.Reference(nil), refs...)
		sort.Slice(out, func(i, j int) bool { return out[i].DocumentID < out[j].DocumentID })
		return out
	}
	return !reflect.DeepEqual(byDocument(prev), byDocument(next))
}

// equalSets compares two slices as sets.
func equalSets[T comparable](prev, next []T) bool {
	if len(prev) != len(next) {
		return false
	}
	counts := make(map[T]int, len(prev))
	for _, v := range prev {
		counts[v]++
	}
	for _, v := range next {
		counts[v]--
		if counts[v] < 0 {
			return false
		}
	}
	return true
}
