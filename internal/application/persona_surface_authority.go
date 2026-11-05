package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errSurfaceAuthority = errors.New("application: surface persona authority unavailable")

// surfacePublishedReader reads the current published persona version. Surfaces
// carry no per-channel installation pin, so current means the greatest
// published version; a superseding publication moves admission with it.
type surfacePublishedReader interface {
	ReadSurfacePublishedPersona(ctx context.Context, tenant, personaID string) (agentpersonastore.PersonaVersion, error)
}

// surfacePersonaStoreReader is the production surfacePublishedReader over the
// durable persona store.
type surfacePersonaStoreReader struct{ store *agentpersonastore.Store }

func newSurfacePersonaStoreReader(store *agentpersonastore.Store) (*surfacePersonaStoreReader, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: persona store is required", errSurfaceAuthority)
	}
	return &surfacePersonaStoreReader{store: store}, nil
}

func (r *surfacePersonaStoreReader) ReadSurfacePublishedPersona(ctx context.Context, tenant, personaID string) (agentpersonastore.PersonaVersion, error) {
	if r == nil || r.store == nil || ctx == nil {
		return agentpersonastore.PersonaVersion{}, errSurfaceAuthority
	}
	id := values.TenantId(tenant)
	if err := id.Validate(); err != nil {
		return agentpersonastore.PersonaVersion{}, fmt.Errorf("%w: invalid tenant: %v", errSurfaceAuthority, err)
	}
	if strings.TrimSpace(personaID) == "" {
		return agentpersonastore.PersonaVersion{}, fmt.Errorf("%w: persona id is required", errSurfaceAuthority)
	}
	scoped, err := r.store.ForTenant(ctx, id)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, fmt.Errorf("%w: scope persona store: %v", errSurfaceAuthority, err)
	}
	versions, err := scoped.ListPublished(ctx)
	if err != nil {
		return agentpersonastore.PersonaVersion{}, fmt.Errorf("%w: list published personas: %v", errSurfaceAuthority, err)
	}
	var current agentpersonastore.PersonaVersion
	for _, version := range versions {
		if version.TenantID != id || version.PersonaID != personaID || version.Version <= current.Version {
			continue
		}
		current = version
	}
	if current.Version <= 0 {
		return agentpersonastore.PersonaVersion{}, fmt.Errorf("%w: persona %q is not published", errSurfaceAuthority, personaID)
	}
	return current, nil
}

// surfaceMembershipChecker reports whether a subject is a current member of
// one surface resource: a case participant, or a holder of document read.
type surfaceMembershipChecker interface {
	SurfaceMember(ctx context.Context, tenant string, kind chatrecipient.SurfaceKind, resource, version, subject string) (bool, error)
}

// surfaceDocumentMembership is the production surfaceMembershipChecker. Hub
// documents authorize through the hub's own read grant, so revocation closes
// admission at once; workflow cases admit nothing until their comment surface
// exists in-tree.
type surfaceDocumentMembership struct{ store *documenthubstore.Store }

func newSurfaceDocumentMembership(store *documenthubstore.Store) (*surfaceDocumentMembership, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: document store is required", errSurfaceAuthority)
	}
	return &surfaceDocumentMembership{store: store}, nil
}

func (m *surfaceDocumentMembership) SurfaceMember(ctx context.Context, tenant string, kind chatrecipient.SurfaceKind, resource, version, subject string) (bool, error) {
	if m == nil || m.store == nil || ctx == nil {
		return false, errSurfaceAuthority
	}
	if !kind.Valid() || strings.TrimSpace(tenant) == "" || strings.TrimSpace(resource) == "" || strings.TrimSpace(subject) == "" {
		return false, fmt.Errorf("%w: tenant, surface, resource and subject are required", errSurfaceAuthority)
	}
	if kind == chatrecipient.SurfaceWorkflowCase {
		return false, nil
	}
	if strings.TrimSpace(version) == "" {
		return false, fmt.Errorf("%w: document version is required", errSurfaceAuthority)
	}
	if err := m.store.Authorize(ctx, tenant, resource, "person", subject, documenthubstore.ActionRead); err != nil {
		if errors.Is(err, documenthubstore.ErrDenied) {
			return false, nil
		}
		return false, fmt.Errorf("%w: authorize surface read: %v", errSurfaceAuthority, err)
	}
	return true, nil
}

// surfacePersonaAuthoritySource is the PersonaAuthoritySource for surface
// channels. Membership is the surface's own — case participation or document
// read — while publication currency, the sealed digest check, and the exact
// skill ceilings reuse the shared chat path: the scope translation is the
// same catalog-backed resolver, run against a deterministic synthetic surface
// installation, because surfaces carry no durable placement row to join.
// Chat channels are always denied here; the chat authority stays their only
// admission path.
type surfacePersonaAuthoritySource struct {
	personas surfacePublishedReader
	members  surfaceMembershipChecker
	scopes   PersonaAuthorityScopeSource
	catalog  agentgate.SkillCatalog
}

func newSurfacePersonaAuthoritySource(personas surfacePublishedReader, members surfaceMembershipChecker, scopes PersonaAuthorityScopeSource, catalog agentgate.SkillCatalog) (*surfacePersonaAuthoritySource, error) {
	if personas == nil || members == nil || scopes == nil || catalog == nil {
		return nil, fmt.Errorf("%w: published personas, membership, scopes and catalog are required", errSurfaceAuthority)
	}
	return &surfacePersonaAuthoritySource{personas: personas, members: members, scopes: scopes, catalog: catalog}, nil
}

var _ PersonaAuthoritySource = (*surfacePersonaAuthoritySource)(nil)

func (s *surfacePersonaAuthoritySource) ResolvePersonaAuthority(ctx context.Context, request agentinvoke.AdmissionRequest, invoker TrustedInvoker) (agentinvoke.Admission, error) {
	if s == nil || ctx == nil || s.personas == nil || s.members == nil || s.scopes == nil || s.catalog == nil ||
		strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.ConversationID) == "" || strings.TrimSpace(request.InvokerID) == "" || strings.TrimSpace(request.PersonaID) == "" {
		return agentinvoke.Admission{}, errSurfaceAuthority
	}
	tenant := values.TenantId(request.TenantID)
	if err := tenant.Validate(); err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: invalid tenant: %v", errSurfaceAuthority, err)
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant() != tenant || verified.Subject() != request.InvokerID {
		return agentinvoke.Admission{}, fmt.Errorf("%w: verified human principal required", errSurfaceAuthority)
	}
	if invoker.ID != request.InvokerID || invoker.TenantID != request.TenantID {
		return agentinvoke.Admission{}, fmt.Errorf("%w: invoker binding mismatch", errSurfaceAuthority)
	}
	kind, resource, version, ok := parseSurfaceChannel(request.ConversationID)
	if !ok {
		return agentinvoke.Admission{}, fmt.Errorf("%w: not a surface channel", errSurfaceAuthority)
	}
	member, err := s.members.SurfaceMember(ctx, request.TenantID, kind, resource, version, request.InvokerID)
	if err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: current surface membership: %v", errSurfaceAuthority, err)
	}
	if !member {
		return agentinvoke.Admission{}, fmt.Errorf("%w: invoker is not a current surface member", errSurfaceAuthority)
	}
	stored, err := s.personas.ReadSurfacePublishedPersona(ctx, request.TenantID, request.PersonaID)
	if err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: current published persona: %v", errSurfaceAuthority, err)
	}
	if stored.TenantID != tenant || stored.PersonaID != request.PersonaID || stored.Version <= 0 || strings.TrimSpace(stored.ContentDigest) == "" {
		return agentinvoke.Admission{}, fmt.Errorf("%w: published persona binding mismatch", errSurfaceAuthority)
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(stored.Profile, &profile); err != nil || profile.PersonaID != stored.PersonaID || int64(profile.Version) != stored.Version {
		return agentinvoke.Admission{}, fmt.Errorf("%w: published persona profile integrity failure", errSurfaceAuthority)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != stored.ContentDigest {
		return agentinvoke.Admission{}, fmt.Errorf("%w: published persona digest mismatch", errSurfaceAuthority)
	}
	policy, err := surfaceChannelPolicy(profile, s.catalog)
	if err != nil {
		return agentinvoke.Admission{}, err
	}
	installation := agentpersonastore.ActiveInstallation{
		InstallationID: surfaceInstallationID(request.TenantID, request.ConversationID, request.PersonaID, stored.Version),
		PersonaID:      stored.PersonaID, PersonaVersion: stored.Version,
		ConversationID: request.ConversationID, ConversationClass: agentpersonastore.ConversationPrivate,
		ChannelPolicy: policy, Revision: 1, RevocationEpoch: 1,
	}
	personaScopes, installationScopes, channelScopes, err := s.scopes.ResolvePersonaScopes(ctx, tenant, profile, installation, policy)
	if err != nil {
		return agentinvoke.Admission{}, fmt.Errorf("%w: resolve exact persona ceilings: %v", errSurfaceAuthority, err)
	}
	return agentinvoke.Admission{
		Persona:          agentinvoke.Persona{ID: stored.PersonaID, Version: strconv.FormatInt(stored.Version, 10), PinnedSkills: personaScopes, Current: true, InstallationID: installation.InstallationID},
		Installation:     agentinvoke.Installation{ID: installation.InstallationID, Current: true, SkillCeiling: installationScopes},
		Channel:          agentinvoke.ChannelPolicy{SkillCeiling: channelScopes},
		Discoverable:     invoker.Discoverable,
		HumanMember:      true,
		AudienceMember:   true,
		PersonaInstalled: true,
	}, nil
}

// surfaceInstallationID names the synthetic installation binding one persona
// version to one surface channel. It is deterministic so a retried invocation
// reuses its durable grant, and it changes with the channel, persona, or
// version so a superseding publication or another resource mints a new one.
func surfaceInstallationID(tenant, channel, personaID string, version int64) string {
	sum := sha256.Sum256([]byte("persona-surface-installation/v1\x00" + tenant + "\x00" + channel + "\x00" + personaID + "\x00" + strconv.FormatInt(version, 10)))
	return "surface-install_" + hex.EncodeToString(sum[:16])
}

// surfaceChannelPolicy is the channel-owned outer bound for a surface
// invocation. No per-surface policy store exists, so the surface adds no
// ceiling beyond the persona's own: the tier is the persona's channel
// ceiling and the data classes are the catalog's full reach, leaving the
// pinned skills, the invoker's discovered skills, and the audience floor at
// delivery as the narrowing bounds.
func surfaceChannelPolicy(profile agentpersona.PersonaProfile, catalog agentgate.SkillCatalog) (agentpersonastore.ChannelPolicy, error) {
	if catalog == nil {
		return agentpersonastore.ChannelPolicy{}, fmt.Errorf("%w: current skill catalog is required", errSurfaceAuthority)
	}
	allowed := make(map[string]struct{})
	for _, record := range catalog.List() {
		for _, class := range append(append([]string{}, record.Definition.DataClassesRead...), record.Definition.DataClassesWritten...) {
			if strings.TrimSpace(class) == "" || strings.TrimSpace(class) != class {
				return agentpersonastore.ChannelPolicy{}, fmt.Errorf("%w: invalid catalog data class", errSurfaceAuthority)
			}
			allowed[class] = struct{}{}
		}
	}
	classes := make([]string, 0, len(allowed))
	for class := range allowed {
		classes = append(classes, class)
	}
	slices.Sort(classes)
	return agentpersonastore.ChannelPolicy{
		PlacementClass: "PRIVATE", MaxTier: profile.TierForConversation(agentpersona.ConversationChannel).String(),
		AllowedDataClasses: classes, AlwaysPrivate: true,
		AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate},
	}, nil
}

// surfaceMembershipSource is the PersonaCurrentMembershipSource for surface
// invocations. Membership is always bound to the committed surface channel
// carried in the server-created authority tuple; without that tuple there is
// no surface to be a member of.
type surfaceMembershipSource struct{ members surfaceMembershipChecker }

var _ PersonaCurrentMembershipSource = surfaceMembershipSource{}

func (s surfaceMembershipSource) ResolveCurrentMembership(ctx context.Context, tenantID, subjectID string) (bool, error) {
	if s.members == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(subjectID) == "" {
		return false, errSurfaceAuthority
	}
	tuple, bound := ctx.Value(personaChatAuthorityTupleKey{}).(PersonaChatAuthorityRequest)
	if !bound || tuple.Tenant.String() != tenantID || tuple.InvokerID != subjectID {
		return false, errSurfaceAuthority
	}
	kind, resource, version, ok := parseSurfaceChannel(tuple.ConversationID)
	if !ok {
		return false, errSurfaceAuthority
	}
	member, err := s.members.SurfaceMember(ctx, tenantID, kind, resource, version, subjectID)
	if err != nil {
		return false, fmt.Errorf("%w: current surface membership: %v", errSurfaceAuthority, err)
	}
	return member, nil
}

// SurfaceAuthoritySourcesConfig supplies the production stores behind surface
// admission. Catalog is the same current skill registry chat admission uses;
// Skills discovers the invoker's current skills under the shared
// persona-mention purpose.
type SurfaceAuthoritySourcesConfig struct {
	Personas  *agentpersonastore.Store
	Documents *documenthubstore.Store
	Catalog   agentgate.SkillCatalog
	Skills    AgentSkillDiscoverer
}

// SurfaceAuthoritySources contains the current surface authority projection
// used by the surface mention bridge's invocation service.
type SurfaceAuthoritySources struct {
	Personas PersonaAuthoritySource
	Identity *ServerTrustedInvokerSource
}

// NewSurfaceAuthoritySources binds the production persona and document stores
// into surface admission. Invoker skills resolve through tenant discovery
// rather than the chat-bound projection: surfaces are not conversations.
func NewSurfaceAuthoritySources(cfg SurfaceAuthoritySourcesConfig) (*SurfaceAuthoritySources, error) {
	if cfg.Personas == nil || cfg.Documents == nil || cfg.Catalog == nil || cfg.Skills == nil {
		return nil, errSurfaceAuthority
	}
	reader, err := newSurfacePersonaStoreReader(cfg.Personas)
	if err != nil {
		return nil, err
	}
	members, err := newSurfaceDocumentMembership(cfg.Documents)
	if err != nil {
		return nil, err
	}
	scopes, err := NewPersonaAuthorityScopeResolver(cfg.Catalog)
	if err != nil {
		return nil, fmt.Errorf("%w: persona scope resolver: %v", errSurfaceAuthority, err)
	}
	personas, err := newSurfacePersonaAuthoritySource(reader, members, scopes, cfg.Catalog)
	if err != nil {
		return nil, err
	}
	identity, err := NewServerTrustedInvokerSource(TrustedInvokerSourceConfig{
		Membership: surfaceMembershipSource{members: members},
		Skills:     currentPersonaMentionSkills{skills: cfg.Skills},
		Purpose:    personaChatReplyPurpose,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: identity projection: %v", errSurfaceAuthority, err)
	}
	return &SurfaceAuthoritySources{Personas: personas, Identity: identity}, nil
}
