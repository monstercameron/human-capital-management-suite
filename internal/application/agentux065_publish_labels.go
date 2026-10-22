package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

// resolvePersonaPublishLabel computes the semantic label one publication
// seals (AGENTUX-065). The first publication is 1.0.0. Later ones advance
// past the greatest sealed label by the author's choice, which must reach
// at least the bump the change from the baseline profile requires; an
// absent choice publishes under exactly that required bump.
func resolvePersonaPublishLabel(ctx context.Context, tenant PersonaAdminLifecycleTenant, personaID string, newProfile agentpersona.PersonaProfile, choice semver.Bump, choiceSet bool) (string, error) {
	versions, err := tenant.ListVersions(ctx, personaID)
	if err != nil {
		return "", err
	}
	var greatest *semver.Version
	var baseline agentpersonastore.PersonaVersion
	var baselineSet bool
	for _, version := range versions {
		label, err := tenant.PublishedLabel(ctx, personaID, version.Version)
		if errors.Is(err, agentpersonastore.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		parsed, err := semver.Parse(label)
		if err != nil {
			continue
		}
		if greatest == nil || greatest.Less(parsed) || (greatest.Compare(parsed) == 0 && version.Version > baseline.Version) {
			greatest, baseline, baselineSet = &parsed, version, true
		}
	}
	if greatest == nil {
		return semver.FirstVersion().String(), nil
	}
	required := semver.BumpPatch
	if baselineSet {
		var prev agentpersona.PersonaProfile
		if err := json.Unmarshal(baseline.Profile, &prev); err != nil {
			return "", fmt.Errorf("%w: baseline profile is not readable", ErrPersonaDraftInvalid)
		}
		required = agentpersona.RequiredBump(prev, newProfile)
	}
	if !choiceSet {
		choice = required
	}
	if choice < required {
		return "", fmt.Errorf("%w: this change needs at least %s", ErrPersonaAdminVersionBumpTooSmall, bumpChoiceName(required))
	}
	return greatest.Next(choice).String(), nil
}

// bumpChoiceName renders a bump as the plain choice the author picks.
func bumpChoiceName(bump semver.Bump) string {
	switch bump {
	case semver.BumpMajor:
		return "major"
	case semver.BumpMinor:
		return "minor"
	default:
		return "patch"
	}
}
