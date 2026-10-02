package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrPersonaCatalogLifecycleUnavailable identifies the deliberately read-only
// client returned by the catalog composition. Lifecycle commands require the
// AGENTP-006 review and command path; this composition never invents one.
var ErrPersonaCatalogLifecycleUnavailable = errors.New("application: persona catalog lifecycle is unavailable")

// personaAdminEvidenceCause is the text logged when stored publication
// evidence cannot be verified. The detail may name internal identifiers, so
// it is included only in an explicitly opted-in local diagnostic session.
func personaAdminEvidenceCause(err error) string {
	if err == nil || os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") != "1" {
		return "withheld"
	}
	return err.Error()
}

// PersonaAdminCatalogTenant reads the isolated, tenant-bound persona store.
// A tenant store is supplied by the composition root and cannot be selected by
// a request principal.
type PersonaAdminCatalogTenant interface {
	ListCatalog(context.Context) ([]agentpersonastore.CatalogEntry, error)
}

// PersonaAdminCatalogStore creates an isolated tenant reader.
type PersonaAdminCatalogStore interface {
	ForTenant(context.Context, values.TenantId) (PersonaAdminCatalogTenant, error)
}

type personaAdminCatalogStoreAdapter struct{ store *agentpersonastore.Store }

func (a personaAdminCatalogStoreAdapter) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaAdminCatalogTenant, error) {
	if a.store == nil {
		return nil, ErrPersonaCatalogDenied
	}
	return a.store.ForTenant(ctx, tenant)
}

// PersonaAdminCatalogComposition contains the dependencies needed to build a
// request-authorized, metadata-only PersonaAdminClient. Targets, grants and
// authorization must be backed by trusted server components; caller supplied
// role or conversation data is never accepted by this factory.
type PersonaAdminCatalogComposition struct {
	Store      PersonaAdminCatalogStore
	Skills     agentpersona.SkillResolver
	Targets    PersonaCatalogTargetReader
	Grants     PersonaCatalogGrantReader
	Authorizer PersonaCatalogAuthorizer
	Starters   productui.PersonaAdminStarterSource
	Documents  PersonaAdminDocumentReader
	// Reactions reads the owner's choice about each agent's reactions (AGENTUX-075).
	Reactions personaReactionSettingReader
}

// NewPersonaAdminCatalogClient composes the production catalog client over an
// isolated persona store. The returned client reads metadata and previews
// effective access; every lifecycle method fails closed because lifecycle
// writes must go through the reviewed command service.
func NewPersonaAdminCatalogClient(deps PersonaAdminCatalogComposition) (productui.PersonaAdminClient, error) {
	if deps.Store == nil || deps.Authorizer == nil {
		return nil, fmt.Errorf("%w: catalog dependencies are required", ErrPersonaCatalogDenied)
	}
	service := &PersonaAdminCatalogService{
		Versions:      personaAdminCatalogVersions{store: deps.Store},
		Installations: personaAdminCatalogInstallations{store: deps.Store},
		Targets:       deps.Targets,
		Skills:        deps.Skills,
		Grants:        deps.Grants,
		Authorizer:    deps.Authorizer,
		Starters:      deps.Starters,
		Documents:     deps.Documents,
		Reactions:     deps.Reactions,
	}
	if adapter, ok := deps.Store.(personaAdminCatalogStoreAdapter); ok {
		service.Versions = AgentIconCatalogVersions{Base: service.Versions, Store: adapter.store}
	}
	return readOnlyPersonaAdminCatalog{service: service}, nil
}

type readOnlyPersonaAdminCatalog struct{ service *PersonaAdminCatalogService }

func (c readOnlyPersonaAdminCatalog) Snapshot(ctx context.Context, req productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	if c.service == nil {
		return productui.PersonaAdminSnapshot{}, ErrPersonaCatalogDenied
	}
	snapshot, err := c.service.Snapshot(ctx, req)
	if err == nil {
		snapshot.CommandPermissionsAvailable = true
		snapshot.CommandsState.Unavailable = true
	}
	return snapshot, err
}

func (c readOnlyPersonaAdminCatalog) Preview(ctx context.Context, req productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	if c.service == nil {
		return productui.PersonaAdminPreview{}, ErrPersonaCatalogDenied
	}
	return c.service.Preview(ctx, req)
}

func (readOnlyPersonaAdminCatalog) RequestReview(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (readOnlyPersonaAdminCatalog) PublishPersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (readOnlyPersonaAdminCatalog) RollbackPersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (readOnlyPersonaAdminCatalog) SuspendPersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}
func (readOnlyPersonaAdminCatalog) RetirePersona(string) error {
	return ErrPersonaCatalogLifecycleUnavailable
}

type personaAdminCatalogVersions struct{ store PersonaAdminCatalogStore }

func (r personaAdminCatalogVersions) ListPersonaCatalogVersions(ctx context.Context, tenant values.TenantId) ([]PersonaCatalogVersion, error) {
	entries, err := listPersonaAdminCatalog(ctx, r.store, tenant)
	if err != nil {
		return nil, err
	}
	scoped, err := r.store.ForTenant(ctx, tenant)
	if err != nil || scoped == nil {
		return nil, ErrPersonaCatalogDenied
	}
	evidenceSource, canReadEvidence := scoped.(interface {
		ResolvePublicationEvidence(context.Context, string, int64) (agentpersonastore.PublicationEvidence, error)
	})
	reviewSource, canReadReview := scoped.(interface {
		ResolveCurrentReview(context.Context, string, int64) (agentpersonastore.VerifiedReview, error)
	})
	out := make([]PersonaCatalogVersion, 0, len(entries))
	indices := make(map[string]int, len(entries))
	for _, entry := range entries {
		profile, err := decodeAvailableProfile(entry.Version)
		if err != nil {
			return nil, fmt.Errorf("%w: decode persona %q: %v", ErrPersonaCatalogDenied, entry.Version.PersonaID, err)
		}
		if profile.Profile.Owner != "" && profile.Profile.Owner != entry.BusinessOwner {
			return nil, fmt.Errorf("%w: owner mismatch for persona %q", ErrPersonaCatalogDenied, entry.Version.PersonaID)
		}
		if profile.Profile.Steward != "" && profile.Profile.Steward != entry.Steward {
			return nil, fmt.Errorf("%w: steward mismatch for persona %q", ErrPersonaCatalogDenied, entry.Version.PersonaID)
		}
		version := PersonaCatalogVersion{Profile: profile, Lifecycle: agentpersona.LifecycleState(entry.Lifecycle), Owner: entry.BusinessOwner, Steward: entry.Steward, ReviewRequired: true}
		if canReadReview && entry.Lifecycle != agentpersonastore.StateDraft {
			if review, readErr := reviewSource.ResolveCurrentReview(ctx, entry.Version.PersonaID, entry.Version.Version); readErr == nil {
				version.Reviewer = review.ReviewerID
				version.ReviewApproved = review.Decision == "APPROVE" && review.GrantCurrent
			}
		}
		if canReadEvidence && (entry.Lifecycle == agentpersonastore.StateInReview || entry.Lifecycle == agentpersonastore.StatePublished || entry.Lifecycle == agentpersonastore.StateSuspended) {
			evidence, readErr := evidenceSource.ResolvePublicationEvidence(ctx, entry.Version.PersonaID, entry.Version.Version)
			if readErr == nil && evidence.ReviewID != "" && evidence.EvaluationRunID != "" {
				version.ReviewApproved, version.EvaluationRef = true, evidence.EvaluationRunID
			} else if readErr != nil && !errors.Is(readErr, agentpersonastore.ErrPublicationEvidenceRequired) {
				// Evidence exists and could not be verified. The version stays
				// unevaluated for the reader, and the cause is logged so a
				// wiring fault does not look like "evaluation not run yet".
				slog.Warn("hcmnext.persona_admin_evidence_unreadable", "persona_id", entry.Version.PersonaID, "version", entry.Version.Version, "error_type", fmt.Sprintf("%T", readErr), "error", personaAdminEvidenceCause(readErr))
			}
		}
		if detailsSource, ok := scoped.(interface {
			ReadPublicationEvidenceDetails(context.Context, string, int64) (agentpersonastore.PublicationEvidenceDetails, error)
		}); ok && (entry.Lifecycle == agentpersonastore.StatePublished || entry.Lifecycle == agentpersonastore.StateSuspended) {
			if details, readErr := detailsSource.ReadPublicationEvidenceDetails(ctx, entry.Version.PersonaID, entry.Version.Version); readErr == nil {
				version.Reviewer = details.ReviewerID
				version.ReviewApproved = true
				version.ReviewApprovedAt = details.ReviewApprovedAt.Format("2006-01-02")
				version.EvaluationPassedAt = details.EvaluationPassedAt.Format("2006-01-02")
			}
		}
		if index, exists := indices[profile.Profile.PersonaID]; exists {
			if profile.Profile.Version > out[index].Profile.Profile.Version {
				out[index] = version
			}
		} else {
			indices[profile.Profile.PersonaID] = len(out)
			out = append(out, version)
		}
	}
	return out, nil
}

type personaAdminCatalogInstallations struct{ store PersonaAdminCatalogStore }

func (r personaAdminCatalogInstallations) ListPersonaCatalogInstallations(ctx context.Context, tenant values.TenantId) ([]PersonaCatalogInstallation, error) {
	entries, err := listPersonaAdminCatalog(ctx, r.store, tenant)
	if err != nil {
		return nil, err
	}
	var out []PersonaCatalogInstallation
	for _, entry := range entries {
		for _, installation := range entry.Installations {
			channel, kind, ok := catalogPlacementKinds(installation.ConversationClass)
			maxTier, validTier := catalogPlacementTier(installation.ChannelPolicy.MaxTier)
			if !ok || !validTier || len(installation.ChannelPolicy.AllowedDataClasses) == 0 {
				continue
			}
			out = append(out, PersonaCatalogInstallation{ID: installation.ID, PersonaID: installation.PersonaID, PersonaVersion: uint32(installation.PersonaVersion), ConversationID: installation.ConversationID,
				Active: installation.State == string(agentpersonastore.InstallationActive), ChannelClass: string(channel), ConversationKind: string(kind), MaxTier: maxTier, AllowedDataClasses: append([]string(nil), installation.ChannelPolicy.AllowedDataClasses...)})
		}
	}
	return out, nil
}

func catalogPlacementTier(value string) (agentskills.SideEffectTier, bool) {
	for tier := agentskills.TierRead; tier <= agentskills.TierExternalWrite; tier++ {
		if tier.String() == value {
			return tier, true
		}
	}
	return 0, false
}

func catalogPlacementKinds(value agentpersonastore.ConversationClass) (agentpersona.ChannelClass, agentpersona.ConversationKind, bool) {
	switch value {
	case agentpersonastore.ConversationPrivate, agentpersonastore.ConversationOneToOne:
		return agentpersona.ChannelPrivate, agentpersona.ConversationDirect, true
	case agentpersonastore.ConversationGroupDM:
		return agentpersona.ChannelPrivate, agentpersona.ConversationGroup, true
	case agentpersonastore.ConversationPublic:
		return agentpersona.ChannelPublic, agentpersona.ConversationChannel, true
	case agentpersonastore.ConversationExternal, agentpersonastore.ConversationCrossCompany:
		return agentpersona.ChannelExternal, agentpersona.ConversationChannel, true
	default:
		return "", "", false
	}
}

func listPersonaAdminCatalog(ctx context.Context, store PersonaAdminCatalogStore, tenant values.TenantId) ([]agentpersonastore.CatalogEntry, error) {
	if ctx == nil || store == nil || tenant.Validate() != nil || strings.TrimSpace(string(tenant)) != string(tenant) {
		return nil, fmt.Errorf("%w: tenant-scoped catalog request is invalid", ErrPersonaCatalogDenied)
	}
	scoped, err := store.ForTenant(ctx, tenant)
	if err != nil || scoped == nil {
		if err == nil {
			err = errors.New("tenant store is nil")
		}
		return nil, fmt.Errorf("%w: scope persona catalog: %v", ErrPersonaCatalogDenied, err)
	}
	entries, err := scoped.ListCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: read persona catalog: %v", ErrPersonaCatalogDenied, err)
	}
	return entries, nil
}

var _ productui.PersonaAdminClient = readOnlyPersonaAdminCatalog{}
