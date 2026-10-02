package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errAgentAvailablePersonas = errors.New("application: available personas unavailable")

// CurrentPersonaAudienceResolver resolves exact eligible installations from
// current server-side conversation membership and audience facts. Implementations
// must ignore caller-supplied roles or persona IDs and fail closed on uncertainty.
type CurrentPersonaAudienceResolver interface {
	ResolveAvailablePersonaInstallations(context.Context, *trust.Principal) ([]agentpersonastore.AvailableInstallation, error)
}

// AvailablePersonaBackend reads tenant-scoped published persona candidates.
// The exact installation tuples have already been narrowed by the resolver.
type AvailablePersonaBackend interface {
	ListAvailable(context.Context, values.TenantId, []agentpersonastore.AvailableInstallation) ([]agentpersonastore.PersonaVersion, error)
}

// AgentPersonaStoreBackend adapts the isolated persona store to the
// application reader without exposing its tenant factory to page code.
type AgentPersonaStoreBackend struct {
	Store *agentpersonastore.Store
}

// ListAvailable reads only candidates from the requested tenant.
func (b AgentPersonaStoreBackend) ListAvailable(ctx context.Context, tenant values.TenantId, allowed []agentpersonastore.AvailableInstallation) ([]agentpersonastore.PersonaVersion, error) {
	if b.Store == nil {
		return nil, errAgentAvailablePersonas
	}
	scoped, err := b.Store.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return scoped.ListAvailable(ctx, allowed)
}

// TenantAvailablePersonaReader supplies the verified persona versions used by
// the Agents page. It verifies the request principal from the trust context,
// then applies the trusted audience resolver before reading profile images.
type TenantAvailablePersonaReader struct {
	Backend  AvailablePersonaBackend
	Audience CurrentPersonaAudienceResolver
	// Skills applies the current per-call discovery gate to every pinned skill.
	// A nil source fails closed after audience resolution, so callers cannot
	// accidentally expose personas without current skill authority.
	Skills AgentSkillDiscoverer
}

var _ AvailablePersonaReader = (*TenantAvailablePersonaReader)(nil)

// ListAvailable returns only sealed profiles that are current, published,
// actively installed and allowed by current audience facts.
func (r *TenantAvailablePersonaReader) ListAvailable(ctx context.Context, principal *trust.Principal) ([]agentpersona.PersonaVersion, error) {
	if r == nil || r.Backend == nil || r.Audience == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return nil, errAgentAvailablePersonas
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.Subject() != principal.Subject() || verified.Tenant() != principal.Tenant() {
		return nil, errAgentAvailablePersonas
	}
	allowed, err := r.Audience.ResolveAvailablePersonaInstallations(ctx, verified)
	if err != nil {
		return nil, fmt.Errorf("resolve current persona audience: %w", err)
	}
	candidates, err := r.Backend.ListAvailable(ctx, verified.Tenant(), allowed)
	if err != nil {
		return nil, fmt.Errorf("read available personas: %w", err)
	}
	if len(allowed) == 0 || len(candidates) == 0 {
		slog.InfoContext(ctx, "hcmnext.persona_projection_empty", "projection", "available_personas", "allowed_installations", len(allowed), "candidates", len(candidates))
	}
	out := make([]agentpersona.PersonaVersion, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		profile, err := decodeAvailableProfile(candidate)
		if err != nil {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "available_personas", "item_type", "persona", "item_id", candidate.PersonaID, "reason", "invalid_profile")
			continue
		}
		if r.Skills == nil {
			return nil, errAgentAvailablePersonas
		}
		discovered, err := r.Skills.Discover(ctx, verified, personaChatReplyPurpose)
		if err != nil {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "available_personas", "item_type", "persona", "item_id", profile.Profile.PersonaID, "reason", "skill_discovery_unavailable", "error", err.Error())
			continue
		}
		if !hasExactPinnedSkills(profile.Profile.SkillPins, discovered) {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "available_personas", "item_type", "persona", "item_id", profile.Profile.PersonaID, "reason", "pinned_skill_not_discoverable", "pinned", len(profile.Profile.SkillPins), "discovered", len(discovered), "missing", missingPinnedSkills(profile.Profile.SkillPins, discovered))
			continue
		}
		if _, exists := seen[profile.Profile.PersonaID]; exists {
			slog.WarnContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "available_personas", "item_type", "persona", "item_id", profile.Profile.PersonaID, "reason", "duplicate_persona")
			continue
		}
		seen[profile.Profile.PersonaID] = struct{}{}
		out = append(out, profile)
	}
	return out, nil
}

// missingPinnedSkills names the pinned skills the current discovery did not
// return with the pinned digest, for the omission log only.
func missingPinnedSkills(pins []agentskills.SkillPin, discovered []agentskills.SkillRecord) []string {
	available := make(map[agentskills.SkillKey]agentskills.SkillRecord, len(discovered))
	for _, record := range discovered {
		available[record.Definition.Key()] = record
	}
	var missing []string
	for _, pin := range pins {
		record, ok := available[pin.Key()]
		switch {
		case !ok:
			missing = append(missing, pin.ID+":not_discovered")
		case record.Digest != pin.Digest:
			missing = append(missing, pin.ID+":digest_changed")
		}
	}
	return missing
}

func hasExactPinnedSkills(pins []agentskills.SkillPin, discovered []agentskills.SkillRecord) bool {
	available := make(map[agentskills.SkillKey]agentskills.SkillRecord, len(discovered))
	for _, record := range discovered {
		available[record.Definition.Key()] = record
	}
	for _, pin := range pins {
		record, ok := available[pin.Key()]
		if !ok || record.Digest != pin.Digest {
			return false
		}
	}
	return true
}

func decodeAvailableProfile(candidate agentpersonastore.PersonaVersion) (agentpersona.PersonaVersion, error) {
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(candidate.Profile, &profile); err != nil {
		return agentpersona.PersonaVersion{}, fmt.Errorf("%w: invalid persona profile", errAgentAvailablePersonas)
	}
	if profile.PersonaID != candidate.PersonaID || int64(profile.Version) != candidate.Version {
		return agentpersona.PersonaVersion{}, fmt.Errorf("%w: profile identity mismatch", errAgentAvailablePersonas)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != candidate.ContentDigest {
		return agentpersona.PersonaVersion{}, fmt.Errorf("%w: persona %s failed integrity verification", errAgentAvailablePersonas, candidate.PersonaID)
	}
	return sealed, nil
}
