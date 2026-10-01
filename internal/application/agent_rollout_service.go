package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrAgentVersionRolloutDenied = errors.New("application: agent version rollout denied")

// ComposeAgentVersionRollout constructs the production version source from the
// isolated persona database and reuses the served installation authority.
func ComposeAgentVersionRollout(store *agentpersonastore.Store, authorizer PersonaAdminCommandAuthorizer, profiles PersonaProfileBuilder, placement PersonaAdminPlacementSource) (*AgentVersionRolloutService, error) {
	return NewAgentVersionRolloutService(store, authorizer, GovernedPersonaAdminInstallation{Placement: placement, Versions: personaAdminInstallationVersions{store: store}, Profiles: profiles}, placement)
}

// AgentVersionRolloutCommand selects exact independent installations. Identity,
// evidence outcomes and current conversation authority are supplied by the server.
type AgentVersionRolloutCommand struct {
	Action          string   `json:"action"`
	RolloutID       string   `json:"rollout_id,omitempty"`
	Digest          string   `json:"digest,omitempty"`
	Revision        int64    `json:"revision,omitempty"`
	PersonaID       string   `json:"persona_id,omitempty"`
	TargetVersion   int64    `json:"target_version,omitempty"`
	InstallationIDs []string `json:"installation_ids,omitempty"`
	CanaryIDs       []string `json:"canary_ids,omitempty"`
	BatchLimit      int      `json:"batch_limit,omitempty"`
}

type AgentVersionRolloutReceipt struct {
	Plan     agentrollout.VersionPlan                 `json:"plan"`
	Progress agentpersonastore.VersionRolloutProgress `json:"progress"`
	Catalog  *AgentVersionRolloutCatalog              `json:"catalog,omitempty"`
}

// AgentVersionRolloutTenant stores desired rollout and actual installations separately.
type AgentVersionRolloutTenant interface {
	GetInstallation(context.Context, string) (agentpersonastore.PersonaInstallation, error)
	GetVersion(context.Context, string, int64) (agentpersonastore.PersonaVersion, error)
	Lifecycle(context.Context, string, int64) (agentpersonastore.LifecycleState, error)
	ResolvePublicationEvidence(context.Context, string, int64) (agentpersonastore.PublicationEvidence, error)
	SaveVersionRollout(context.Context, agentrollout.VersionPlan) (agentpersonastore.VersionRolloutProgress, error)
	GetVersionRollout(context.Context, string) (agentrollout.VersionPlan, agentpersonastore.VersionRolloutProgress, error)
	ApproveVersionRollout(context.Context, string, string, string, int64) (agentpersonastore.VersionRolloutProgress, error)
	PromoteVersionRollout(context.Context, string, string, string, int64) (agentpersonastore.VersionRolloutProgress, error)
	ApplyVersionRollout(context.Context, string, string, string, int64, agentrollout.VersionCandidate, agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, agentpersonastore.VersionRolloutProgress, error)
}

type AgentVersionRolloutStore interface {
	ForRolloutTenant(context.Context, values.TenantId) (AgentVersionRolloutTenant, error)
}
type agentVersionRolloutStore struct{ store *agentpersonastore.Store }

func (s agentVersionRolloutStore) ForRolloutTenant(ctx context.Context, tenant values.TenantId) (AgentVersionRolloutTenant, error) {
	return s.store.ForTenant(ctx, tenant)
}

// AgentVersionRolloutService reauthorizes every preview, approval, and mutation.
type AgentVersionRolloutService struct {
	Store      AgentVersionRolloutStore
	Authorizer PersonaAdminCommandAuthorizer
	Placement  PersonaAdminPlacementSource
	Governed   GovernedPersonaAdminInstallation
	NewID      func() string
}

func NewAgentVersionRolloutService(store *agentpersonastore.Store, authorizer PersonaAdminCommandAuthorizer, governed GovernedPersonaAdminInstallation, placement PersonaAdminPlacementSource) (*AgentVersionRolloutService, error) {
	if store == nil || authorizer == nil || placement == nil || governed.Profiles == nil || governed.Versions == nil || governed.Placement == nil {
		return nil, ErrAgentVersionRolloutDenied
	}
	if _, ok := placement.(PersonaAdminPlacementFence); !ok {
		return nil, ErrAgentVersionRolloutDenied
	}
	return &AgentVersionRolloutService{Store: agentVersionRolloutStore{store}, Authorizer: authorizer, Placement: placement, Governed: governed, NewID: uuid.NewString}, nil
}

// Execute uses only the verified request principal for tenancy and manager identity.
func (s *AgentVersionRolloutService) Execute(ctx context.Context, r AgentVersionRolloutCommand) (AgentVersionRolloutReceipt, error) {
	if s == nil || ctx == nil || s.Store == nil || s.Authorizer == nil || s.Placement == nil || s.NewID == nil {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || !validPersonaAdminActor(p) {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	actor := PersonaAdminCommandActor{Principal: p, Tenant: p.Tenant(), Subject: p.Subject()}
	if s.Authorizer.AuthorizePersonaAdminCommand(ctx, actor, PersonaAdminInstall, r.PersonaID) != nil {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	scoped, err := s.Store.ForRolloutTenant(ctx, actor.Tenant)
	if err != nil || scoped == nil {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	if r.Action == "CATALOG" {
		return s.catalog(ctx, scoped, actor, r)
	}
	if r.Action == "PREVIEW" {
		return s.preview(ctx, scoped, actor, r)
	}
	if r.RolloutID == "" || r.PersonaID != "" || r.TargetVersion != 0 || len(r.InstallationIDs) != 0 || len(r.CanaryIDs) != 0 || r.BatchLimit != 0 {
		return AgentVersionRolloutReceipt{}, agentrollout.ErrInvalid
	}
	plan, progress, err := scoped.GetVersionRollout(ctx, r.RolloutID)
	if err != nil {
		return AgentVersionRolloutReceipt{}, err
	}
	if plan.Verify() != nil || plan.TenantID != string(actor.Tenant) || progress.Cursor < 0 || progress.Cursor > len(plan.Candidates) || progress.Revision <= 0 {
		return AgentVersionRolloutReceipt{}, agentrollout.ErrPreviewStale
	}
	if s.Authorizer.AuthorizePersonaAdminCommand(ctx, actor, PersonaAdminInstall, plan.AgentID) != nil {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	if r.Action == "READ" {
		if err = s.checkManagers(ctx, actor, plan); err != nil {
			return AgentVersionRolloutReceipt{}, err
		}
		return AgentVersionRolloutReceipt{Plan: plan, Progress: progress}, nil
	}
	if r.Digest != plan.Digest || r.Revision != progress.Revision {
		return AgentVersionRolloutReceipt{}, agentrollout.ErrPreviewStale
	}
	switch r.Action {
	case "APPROVE", "PROMOTE":
		if err = s.checkCandidates(ctx, scoped, actor, plan, progress.Cursor); err != nil {
			return AgentVersionRolloutReceipt{}, err
		}
		if r.Action == "APPROVE" {
			progress, err = scoped.ApproveVersionRollout(ctx, plan.ID, plan.Digest, actor.Subject, r.Revision)
		} else {
			progress, err = scoped.PromoteVersionRollout(ctx, plan.ID, plan.Digest, actor.Subject, r.Revision)
		}
	case "ADVANCE":
		if progress.ApproverID != actor.Subject {
			return AgentVersionRolloutReceipt{}, agentrollout.ErrApprovalRequired
		}
		if progress.Stage != "APPROVED" {
			return AgentVersionRolloutReceipt{}, agentrollout.ErrApprovalRequired
		}
		stop := min(len(plan.Candidates), progress.Cursor+plan.BatchLimit)
		if progress.Cursor < plan.CanaryCount {
			stop = min(stop, plan.CanaryCount)
		}
		for progress.Cursor < stop {
			c := plan.Candidates[progress.Cursor]
			installation, facts, e := s.checkedCandidate(ctx, scoped, actor, plan, c)
			if e != nil {
				return AgentVersionRolloutReceipt{Plan: plan, Progress: progress}, e
			}
			fence, ok := s.Placement.(PersonaAdminPlacementFence)
			if !ok {
				return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
			}
			err = fence.WithPersonaAdminPlacementFence(ctx, facts, func() error {
				var e error
				_, progress, e = scoped.ApplyVersionRollout(ctx, plan.ID, plan.Digest, actor.Subject, progress.Revision, c, installation)
				return e
			})
			if err != nil {
				return AgentVersionRolloutReceipt{Plan: plan, Progress: progress}, err
			}
		}
	default:
		return AgentVersionRolloutReceipt{}, agentrollout.ErrInvalid
	}
	return AgentVersionRolloutReceipt{Plan: plan, Progress: progress}, err
}

func (s *AgentVersionRolloutService) preview(ctx context.Context, store AgentVersionRolloutTenant, actor PersonaAdminCommandActor, r AgentVersionRolloutCommand) (AgentVersionRolloutReceipt, error) {
	if r.RolloutID != "" || r.Digest != "" || r.Revision != 0 || r.PersonaID == "" || r.TargetVersion <= 0 || len(r.InstallationIDs) == 0 || len(r.InstallationIDs) > 1000 || r.BatchLimit < 1 || r.BatchLimit > 100 {
		return AgentVersionRolloutReceipt{}, agentrollout.ErrInvalid
	}
	version, err := store.GetVersion(ctx, r.PersonaID, r.TargetVersion)
	if err != nil {
		return AgentVersionRolloutReceipt{}, err
	}
	state, err := store.Lifecycle(ctx, r.PersonaID, r.TargetVersion)
	if err != nil || state != agentpersonastore.StatePublished || version.TenantID != actor.Tenant {
		return AgentVersionRolloutReceipt{}, ErrAgentVersionRolloutDenied
	}
	evidence, err := store.ResolvePublicationEvidence(ctx, r.PersonaID, r.TargetVersion)
	if err != nil {
		return AgentVersionRolloutReceipt{}, err
	}
	request := agentrollout.VersionRequest{ID: s.NewID(), TenantID: string(actor.Tenant), AgentID: r.PersonaID, Version: r.TargetVersion, ProfileDigest: version.ContentDigest, EvaluationRef: evidence.EvaluationRunID, ReviewRef: evidence.ReviewID, BatchLimit: r.BatchLimit, CanaryIDs: r.CanaryIDs}
	seen := map[string]bool{}
	for _, id := range r.InstallationIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return AgentVersionRolloutReceipt{}, agentrollout.ErrInvalid
		}
		seen[id] = true
		installation, err := store.GetInstallation(ctx, id)
		if err != nil {
			return AgentVersionRolloutReceipt{}, err
		}
		if installation.TenantID != actor.Tenant || installation.PersonaID != r.PersonaID || installation.State != agentpersonastore.InstallationActive {
			return AgentVersionRolloutReceipt{}, agentrollout.ErrPreviewStale
		}
		facts, err := s.Placement.ResolvePersonaAdminPlacement(ctx, actor, installation.ConversationID)
		if err != nil {
			return AgentVersionRolloutReceipt{}, err
		}
		c := agentrollout.VersionCandidate{InstallationID: id, ConversationID: installation.ConversationID, Version: installation.PersonaVersion, Revision: installation.Revision, RevocationEpoch: installation.RevocationEpoch, AuthorityRevision: facts.Revision, PolicyDigest: agentRolloutPolicyDigest(installation.ChannelPolicy)}
		trial := agentrollout.VersionPlan{VersionRequest: request}
		if _, _, err = s.checkedCandidate(ctx, store, actor, trial, c); err != nil {
			return AgentVersionRolloutReceipt{}, err
		}
		request.Candidates = append(request.Candidates, c)
	}
	plan, err := agentrollout.PreviewVersion(request)
	if err != nil {
		return AgentVersionRolloutReceipt{}, err
	}
	progress, err := store.SaveVersionRollout(ctx, plan)
	if err != nil {
		return AgentVersionRolloutReceipt{}, err
	}
	return AgentVersionRolloutReceipt{Plan: plan, Progress: progress}, err
}

func (s *AgentVersionRolloutService) checkManagers(ctx context.Context, actor PersonaAdminCommandActor, plan agentrollout.VersionPlan) error {
	for _, c := range plan.Candidates {
		facts, err := s.Placement.ResolvePersonaAdminPlacement(ctx, actor, c.ConversationID)
		if err != nil || facts.Tenant != actor.Tenant || facts.ManagerID != actor.Subject || facts.ConversationID != c.ConversationID {
			return ErrAgentVersionRolloutDenied
		}
	}
	return nil
}
func (s *AgentVersionRolloutService) checkCandidates(ctx context.Context, store AgentVersionRolloutTenant, actor PersonaAdminCommandActor, plan agentrollout.VersionPlan, start int) error {
	if err := s.checkManagers(ctx, actor, plan); err != nil {
		return err
	}
	for _, c := range plan.Candidates[start:] {
		if _, _, err := s.checkedCandidate(ctx, store, actor, plan, c); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentVersionRolloutService) checkedCandidate(ctx context.Context, store AgentVersionRolloutTenant, actor PersonaAdminCommandActor, plan agentrollout.VersionPlan, c agentrollout.VersionCandidate) (agentpersonastore.PersonaInstallation, PersonaAdminPlacementFacts, error) {
	installation, err := store.GetInstallation(ctx, c.InstallationID)
	if err != nil {
		return installation, PersonaAdminPlacementFacts{}, err
	}
	if installation.TenantID != actor.Tenant || installation.PersonaID != plan.AgentID || installation.ConversationID != c.ConversationID || installation.PersonaVersion != c.Version || installation.Revision != c.Revision || installation.RevocationEpoch != c.RevocationEpoch || installation.State != agentpersonastore.InstallationActive || agentRolloutPolicyDigest(installation.ChannelPolicy) != c.PolicyDigest {
		return installation, PersonaAdminPlacementFacts{}, agentrollout.ErrPreviewStale
	}
	facts, err := s.Placement.ResolvePersonaAdminPlacement(ctx, actor, c.ConversationID)
	if err != nil || facts.Revision != c.AuthorityRevision || facts.Tenant != actor.Tenant || facts.ManagerID != actor.Subject || facts.ConversationID != c.ConversationID || facts.Class != installation.ConversationClass || !slices.Contains(installation.ChannelPolicy.AllowedChannelClasses, facts.Class) {
		return installation, facts, agentrollout.ErrPreviewStale
	}
	row, err := store.GetVersion(ctx, plan.AgentID, plan.Version)
	if err != nil || row.ContentDigest != plan.ProfileDigest {
		return installation, facts, agentrollout.ErrPreviewStale
	}
	evidence, err := store.ResolvePublicationEvidence(ctx, plan.AgentID, plan.Version)
	if err != nil || evidence.ReviewID != plan.ReviewRef || evidence.EvaluationRunID != plan.EvaluationRef {
		return installation, facts, agentrollout.ErrReviewRequired
	}
	state, err := store.Lifecycle(ctx, plan.AgentID, plan.Version)
	if err != nil || state != agentpersonastore.StatePublished {
		return installation, facts, agentrollout.ErrReviewRequired
	}
	installation.PersonaVersion = plan.Version
	installation.InstallerID = actor.Subject
	verified, err := s.Governed.AuthorizePersonaInstallation(ctx, actor, installation)
	if err != nil {
		return installation, facts, err
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(row.Profile, &profile) != nil || !agentRolloutScopeFits(profile, installation.ChannelPolicy, installation.ConversationClass) {
		return installation, facts, agentrollout.ErrReviewRequired
	}
	// Preserve the installation's exact independent ceiling; current room policy
	// remains an additional authority bound and cannot widen this stored grant.
	verified.ChannelPolicy = installation.ChannelPolicy
	return verified, facts, nil
}

func agentRolloutScopeFits(p agentpersona.PersonaProfile, policy agentpersonastore.ChannelPolicy, class agentpersonastore.ConversationClass) bool {
	ceiling, ok := catalogPlacementTier(policy.MaxTier)
	kind := agentpersona.ConversationChannel
	if class == agentpersonastore.ConversationOneToOne {
		kind = agentpersona.ConversationDirect
	}
	if class == agentpersonastore.ConversationGroupDM {
		kind = agentpersona.ConversationGroup
	}
	if !ok || p.TierForConversation(kind) > ceiling {
		return false
	}
	for _, class := range append(slices.Clone(p.DataClassesRead), p.DataClassesWritten...) {
		if !slices.Contains(policy.AllowedDataClasses, class) {
			return false
		}
	}
	return true
}

func agentRolloutPolicyDigest(policy agentpersonastore.ChannelPolicy) string {
	raw, _ := json.Marshal(policy)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%s", hex.EncodeToString(sum[:]))
}
