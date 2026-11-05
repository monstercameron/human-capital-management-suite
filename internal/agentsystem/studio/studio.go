package studio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	agentcharter "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/charter"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/publication"
)

var (
	// ErrInvalid identifies a malformed Studio request.
	ErrInvalid = errors.New("agent studio: invalid request")
	// ErrNotFound identifies a missing draft. Cross-tenant lookups report the
	// same error without revealing whether the draft exists.
	ErrNotFound = errors.New("agent studio: draft not found")
	// ErrStaleDraft identifies an expected revision or digest that no longer
	// matches the stored draft.
	ErrStaleDraft = errors.New("agent studio: draft revision is stale")
	// ErrState identifies a lifecycle transition the draft state forbids.
	ErrState = errors.New("agent studio: draft state forbids this transition")
	// ErrSeparation identifies an author attempting to review their own draft.
	ErrSeparation = errors.New("agent studio: reviewer must differ from the author")
	// ErrEvaluation identifies sandbox evidence that is missing, failing,
	// stale, or bound to a different manifest digest.
	ErrEvaluation = errors.New("agent studio: exact fresh passing evaluation required")
	// ErrDenied identifies a current-policy or authorization denial.
	ErrDenied = errors.New("agent studio: denied by current policy")
)

// State is the Studio lifecycle of one draft.
type State string

const (
	StateDraft          State = "DRAFT"
	StateValidated      State = "VALIDATED"
	StateReviewRequired State = "REVIEW_REQUIRED"
	StatePublished      State = "PUBLISHED"
	StateRejected       State = "REJECTED"
)

// Step is one Studio creator step. The first six steps are editable; authority,
// sandbox, and review are derived from validation and governance evidence.
type Step string

const (
	StepJob       Step = "job"
	StepCharter   Step = "charter"
	StepContext   Step = "context"
	StepTools     Step = "tools"
	StepTriggers  Step = "triggers"
	StepBudgets   Step = "budgets"
	StepAuthority Step = "authority"
	StepSandbox   Step = "sandbox"
	StepReview    Step = "review"
)

// OrderedSteps is the creator journey in display order.
var OrderedSteps = []Step{StepJob, StepCharter, StepContext, StepTools, StepTriggers, StepBudgets, StepAuthority, StepSandbox, StepReview}

// JobSelection names the business outcome and its instructions.
type JobSelection struct {
	DisplayName        string
	Purpose            string
	InstructionsDigest string
}

// CharterSelection carries the business charter inputs for the draft.
type CharterSelection struct {
	OrganizationIDs []string
	Jurisdictions   []string
	DataCategories  []string
	EscalationOwner string
	SuccessMeasures string
}

// ContextSelection carries approved sources, chat spaces, audience, and purpose.
type ContextSelection struct {
	SourceIDs    []string
	ChatSpaceIDs []string
	Audience     string
	Purpose      string
}

// ToolSelection carries the requested capability ceiling.
type ToolSelection struct {
	CapabilityIDs []string
}

// TriggerSelection carries invocation modes, channels, and reply visibility.
type TriggerSelection struct {
	Modes           []string
	ChannelIDs      []string
	ReplyVisibility string
}

// BudgetSelection carries autonomy, model policy, and budget ceilings.
type BudgetSelection struct {
	MaxCostMicros      uint64
	MaxInputTokens     uint64
	MaxOutputTokens    uint64
	MaxConcurrentRuns  uint32
	AutonomyCeiling    string
	ModelPolicyID      string
	ModelPolicyVersion uint64
}

// StepUpdate carries the selection for exactly one editable step.
type StepUpdate struct {
	Job      *JobSelection
	Charter  *CharterSelection
	Context  *ContextSelection
	Tools    *ToolSelection
	Triggers *TriggerSelection
	Budgets  *BudgetSelection
}

// Draft is the tenant-owned mutable Studio working copy. Evidence fields bind
// to one exact sealed manifest and are cleared by every edit.
type Draft struct {
	TenantID           string
	AgentID            string
	Revision           uint64
	State              State
	OwnerID            string
	DisplayName        string
	Purpose            string
	InstructionsDigest string
	Charter            CharterSelection
	Context            ContextSelection
	Tools              ToolSelection
	Triggers           TriggerSelection
	Budgets            BudgetSelection
	ManifestVersion    uint64
	ManifestDigest     string
	EvaluationDigest   string
	ReviewDigest       string
	ReviewerID         string
	Published          bool
	Generation         uint64
}

// ScopeItem is one requested tool or source with its effective decision and a
// human-readable explanation naming the deciding policy layers.
type ScopeItem struct {
	ID      string
	Allowed bool
	Reason  string
}

// AuthorityPreview is the effective scope for the draft's requested tools and
// sources. Denied items appear alongside allowed ones with their reason.
type AuthorityPreview struct {
	Tools   []ScopeItem
	Sources []ScopeItem
}

// Validation is the deterministic result of validating one draft revision.
type Validation struct {
	Draft    Draft
	Manifest agentmanifest.Manifest
	Digest   string
	Charter  agentcharter.Result
	Preview  AuthorityPreview
}

// StepStatus is one creator step with its completion state.
type StepStatus struct {
	Step    Step
	Done    bool
	Current bool
}

// PublicationStatus is the pinned publication pointer for the draft.
type PublicationStatus struct {
	Present    bool
	Generation uint64
	Version    uint64
	Digest     string
}

// SandboxStatus is the bound sandbox evaluation evidence.
type SandboxStatus struct {
	EvaluationDigest string
	Passed           bool
	Fresh            bool
}

// StatusView is the canonical Studio read model. The browser renders this
// view; it contains no browser-only agent model.
type StatusView struct {
	TenantID         string
	AgentID          string
	Revision         uint64
	ManifestVersion  uint64
	State            State
	OwnerID          string
	DisplayName      string
	Purpose          string
	ManifestDigest   string
	EvaluationDigest string
	ReviewDigest     string
	ReviewerID       string
	Charter          CharterSelection
	Context          ContextSelection
	Tools            ToolSelection
	Triggers         TriggerSelection
	Budgets          BudgetSelection
	Preview          AuthorityPreview
	Steps            []StepStatus
	Publication      PublicationStatus
	Sandbox          SandboxStatus
}

// DraftRepository persists tenant-scoped drafts with compare-and-swap on the
// expected revision. Implementations must scope every key by tenant.
type DraftRepository interface {
	Load(context.Context, string, string) (Draft, error)
	Save(context.Context, uint64, Draft) (Draft, error)
}

// CharterLayers resolves the current layered charter input for a draft from
// the owning organization and policy services.
type CharterLayers interface {
	ResolveLayers(context.Context, string, Draft) (agentcharter.Input, error)
}

// ManifestStore seals validated manifests and resolves them back for the
// governed review and publication path.
type ManifestStore interface {
	Seal(context.Context, agentmanifest.Manifest) error
	publication.ManifestResolver
}

// Publisher is the governed review and publication boundary. It is satisfied
// by *publication.Service.
type Publisher interface {
	Review(context.Context, string, string, uint64, string, string) (publication.Review, error)
	Publish(context.Context, string, string, uint64, uint64, string, string, string) (publication.Publication, error)
	Admit(context.Context, string, string, string) (publication.Publication, error)
}

// Dependencies are the trusted Studio boundaries.
type Dependencies struct {
	Drafts    DraftRepository
	Charters  CharterLayers
	Sandbox   publication.EvaluationResolver
	Reviewers publication.ReviewAuthority
	Manifests ManifestStore
	Publisher Publisher
}

// Service coordinates the Studio flow without owning storage or policy.
type Service struct{ deps Dependencies }

// New constructs the Studio service only when every fail-closed boundary exists.
func New(deps Dependencies) (*Service, error) {
	if deps.Drafts == nil || deps.Charters == nil || deps.Sandbox == nil || deps.Reviewers == nil || deps.Manifests == nil || deps.Publisher == nil {
		return nil, fmt.Errorf("%w: all studio dependencies are required", ErrInvalid)
	}
	return &Service{deps: deps}, nil
}

// CreateDraft starts a tenant-owned draft at revision 1 in DRAFT state.
func (s *Service) CreateDraft(ctx context.Context, tenantID, agentID, ownerID, displayName, purpose, instructionsDigest string) (Draft, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(agentID) == "" || strings.TrimSpace(ownerID) == "" {
		return Draft{}, fmt.Errorf("%w: tenant, agent, and owner are required", ErrInvalid)
	}
	if strings.TrimSpace(displayName) == "" || strings.TrimSpace(purpose) == "" || !validDigest(instructionsDigest) {
		return Draft{}, fmt.Errorf("%w: display name, purpose, and a sha256 instructions digest are required", ErrInvalid)
	}
	draft := Draft{TenantID: tenantID, AgentID: agentID, Revision: 1, State: StateDraft, OwnerID: ownerID, DisplayName: displayName, Purpose: purpose, InstructionsDigest: instructionsDigest}
	return s.deps.Drafts.Save(ctx, 0, draft)
}

// SaveStep stores one editable step under the expected revision. Every edit
// resets the draft to DRAFT and clears evidence bound to earlier content, so a
// changed draft can never publish on a stale validation, review, or digest.
func (s *Service) SaveStep(ctx context.Context, tenantID, agentID string, expectedRevision uint64, step Step, update StepUpdate) (Draft, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Revision != expectedRevision {
		return Draft{}, ErrStaleDraft
	}
	if err := applyStep(&draft, step, update); err != nil {
		return Draft{}, err
	}
	draft.State = StateDraft
	draft.ManifestVersion = 0
	draft.ManifestDigest = ""
	draft.EvaluationDigest = ""
	draft.ReviewDigest = ""
	draft.ReviewerID = ""
	draft.Published = false
	draft.Generation = 0
	draft.Revision++
	return s.deps.Drafts.Save(ctx, expectedRevision, draft)
}

// BuildManifest renders the canonical manifest for the current draft content.
// The result previews the exact bytes validation would seal; only ValidateDraft
// seals and pins a manifest version.
func (s *Service) BuildManifest(ctx context.Context, tenantID, agentID string) (agentmanifest.Manifest, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return agentmanifest.Manifest{}, err
	}
	version := draft.ManifestVersion
	if version == 0 {
		version = draft.Revision
	}
	manifest := buildManifest(draft, version)
	if err := manifest.Validate(); err != nil {
		return agentmanifest.Manifest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return manifest, nil
}

// Preview resolves the current charter and explains the effective scope of the
// draft's requested tools and sources, denied items included.
func (s *Service) Preview(ctx context.Context, tenantID, agentID string) (AuthorityPreview, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return AuthorityPreview{}, err
	}
	resolution, err := s.resolveCharter(ctx, tenantID, draft)
	if err != nil {
		return AuthorityPreview{}, err
	}
	return PreviewAuthority(draft, resolution), nil
}

// ValidateDraft seals the canonical manifest for this revision, resolves the
// current charter, refuses denied tools and sources by name, and binds the
// exact fresh passing sandbox evaluation.
func (s *Service) ValidateDraft(ctx context.Context, tenantID, agentID string, expectedRevision uint64, evaluationDigest string) (Validation, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return Validation{}, err
	}
	if draft.Revision != expectedRevision {
		return Validation{}, ErrStaleDraft
	}
	if draft.State != StateDraft && draft.State != StateValidated {
		return Validation{}, fmt.Errorf("%w: only a draft can validate", ErrState)
	}
	manifest := buildManifest(draft, draft.Revision)
	if err := manifest.Validate(); err != nil {
		return Validation{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		return Validation{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	resolution, err := s.resolveCharter(ctx, tenantID, draft)
	if err != nil {
		return Validation{}, err
	}
	preview := PreviewAuthority(draft, resolution)
	if denied := deniedItems(preview); len(denied) > 0 {
		return Validation{}, fmt.Errorf("%w: remove or replace %s", ErrDenied, strings.Join(denied, ", "))
	}
	evaluation, err := s.deps.Sandbox.ResolveEvaluation(ctx, evaluationDigest)
	if err != nil || evaluation.Digest != evaluationDigest || evaluation.ManifestDigest != digest || !evaluation.Passed || !evaluation.Fresh {
		return Validation{}, ErrEvaluation
	}
	if err := s.deps.Manifests.Seal(ctx, manifest); err != nil {
		return Validation{}, fmt.Errorf("seal manifest: %w", err)
	}
	draft.State = StateValidated
	draft.ManifestVersion = draft.Revision
	draft.ManifestDigest = digest
	draft.EvaluationDigest = evaluationDigest
	draft.ReviewDigest = ""
	draft.ReviewerID = ""
	draft.Revision++
	saved, err := s.deps.Drafts.Save(ctx, expectedRevision, draft)
	if err != nil {
		return Validation{}, err
	}
	return Validation{Draft: saved, Manifest: manifest, Digest: digest, Charter: resolution, Preview: preview}, nil
}

// SubmitForReview moves a validated draft to REVIEW_REQUIRED after rechecking
// the exact digest and current charter under the same revision fence.
func (s *Service) SubmitForReview(ctx context.Context, tenantID, agentID string, expectedRevision uint64, expectedDigest string) (Draft, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Revision != expectedRevision {
		return Draft{}, ErrStaleDraft
	}
	if draft.State != StateValidated {
		return Draft{}, fmt.Errorf("%w: only a validated draft can request review", ErrState)
	}
	if draft.ManifestDigest == "" || draft.ManifestDigest != expectedDigest {
		return Draft{}, ErrStaleDraft
	}
	resolution, err := s.resolveCharter(ctx, tenantID, draft)
	if err != nil {
		return Draft{}, err
	}
	if denied := deniedItems(PreviewAuthority(draft, resolution)); len(denied) > 0 {
		return Draft{}, fmt.Errorf("%w: policy now denies %s", ErrDenied, strings.Join(denied, ", "))
	}
	draft.State = StateReviewRequired
	draft.Revision++
	return s.deps.Drafts.Save(ctx, expectedRevision, draft)
}

// RecordReview authorizes an independent reviewer and seals their approval of
// the exact manifest digest through the governed publication path.
func (s *Service) RecordReview(ctx context.Context, tenantID, agentID, reviewerID string, expectedRevision uint64, expectedDigest string) (publication.Review, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return publication.Review{}, err
	}
	if draft.Revision != expectedRevision {
		return publication.Review{}, ErrStaleDraft
	}
	if draft.State != StateReviewRequired {
		return publication.Review{}, fmt.Errorf("%w: only a submitted draft can be reviewed", ErrState)
	}
	if draft.ManifestDigest == "" || draft.ManifestDigest != expectedDigest {
		return publication.Review{}, ErrStaleDraft
	}
	if strings.TrimSpace(reviewerID) == "" || reviewerID == draft.OwnerID {
		return publication.Review{}, ErrSeparation
	}
	if err := s.deps.Reviewers.AuthorizeAgentReviewer(ctx, tenantID, agentID, reviewerID); err != nil {
		return publication.Review{}, fmt.Errorf("%w: %v", ErrDenied, err)
	}
	review, err := s.deps.Publisher.Review(ctx, tenantID, agentID, draft.ManifestVersion, reviewerID, draft.ManifestDigest)
	if err != nil {
		return publication.Review{}, err
	}
	draft.ReviewDigest = review.Digest
	draft.ReviewerID = review.ReviewerID
	draft.Revision++
	if _, err := s.deps.Drafts.Save(ctx, expectedRevision, draft); err != nil {
		return publication.Review{}, err
	}
	return review, nil
}

// Publish pins the exact reviewed manifest, evaluation, and review digests
// after rechecking the revision fence, reviewer authority, and digest.
func (s *Service) Publish(ctx context.Context, tenantID, agentID string, expectedRevision uint64, expectedDigest string, expectedGeneration uint64) (publication.Publication, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return publication.Publication{}, err
	}
	if draft.Revision != expectedRevision {
		return publication.Publication{}, ErrStaleDraft
	}
	if draft.State != StateReviewRequired || draft.ReviewDigest == "" || strings.TrimSpace(draft.ReviewerID) == "" {
		return publication.Publication{}, fmt.Errorf("%w: only a reviewed draft can publish", ErrState)
	}
	if draft.ManifestDigest == "" || draft.ManifestDigest != expectedDigest {
		return publication.Publication{}, ErrStaleDraft
	}
	if err := s.deps.Reviewers.AuthorizeAgentReviewer(ctx, tenantID, agentID, draft.ReviewerID); err != nil {
		return publication.Publication{}, fmt.Errorf("%w: reviewer authority lapsed: %v", ErrDenied, err)
	}
	published, err := s.deps.Publisher.Publish(ctx, tenantID, agentID, draft.ManifestVersion, expectedGeneration, draft.ManifestDigest, draft.EvaluationDigest, draft.ReviewDigest)
	if err != nil {
		return publication.Publication{}, err
	}
	draft.State = StatePublished
	draft.Published = true
	draft.Generation = published.Generation
	draft.Revision++
	if _, err := s.deps.Drafts.Save(ctx, expectedRevision, draft); err != nil {
		return publication.Publication{}, err
	}
	return published, nil
}

// TransferOwner moves accountability to a new owner. A pending review is
// cleared back to VALIDATED so the new owner requests a fresh independent
// review instead of publishing on stale separation evidence.
func (s *Service) TransferOwner(ctx context.Context, tenantID, agentID, actorID, newOwnerID string, expectedRevision uint64) (Draft, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return Draft{}, err
	}
	if draft.Revision != expectedRevision {
		return Draft{}, ErrStaleDraft
	}
	if actorID != draft.OwnerID {
		return Draft{}, fmt.Errorf("%w: only the current owner transfers ownership", ErrDenied)
	}
	if strings.TrimSpace(newOwnerID) == "" || strings.TrimSpace(newOwnerID) != newOwnerID {
		return Draft{}, fmt.Errorf("%w: new owner must be a canonical principal ID", ErrInvalid)
	}
	draft.OwnerID = newOwnerID
	if draft.State == StateReviewRequired && draft.ReviewDigest != "" {
		draft.State = StateValidated
		draft.ReviewDigest = ""
		draft.ReviewerID = ""
	}
	draft.Revision++
	return s.deps.Drafts.Save(ctx, expectedRevision, draft)
}

// Status renders the canonical Studio read model: steps, selections, charter,
// authority preview, sandbox evidence, review digest, and publication pointer.
func (s *Service) Status(ctx context.Context, tenantID, agentID string) (StatusView, error) {
	draft, err := s.load(ctx, tenantID, agentID)
	if err != nil {
		return StatusView{}, err
	}
	resolution, err := s.resolveCharter(ctx, tenantID, draft)
	if err != nil {
		return StatusView{}, err
	}
	view := StatusView{
		TenantID: tenantID, AgentID: agentID, Revision: draft.Revision, ManifestVersion: draft.ManifestVersion,
		State: draft.State, OwnerID: draft.OwnerID, DisplayName: draft.DisplayName, Purpose: draft.Purpose,
		ManifestDigest: draft.ManifestDigest, EvaluationDigest: draft.EvaluationDigest,
		ReviewDigest: draft.ReviewDigest, ReviewerID: draft.ReviewerID,
		Charter: draft.Charter, Context: draft.Context, Tools: draft.Tools, Triggers: draft.Triggers, Budgets: draft.Budgets,
		Preview: PreviewAuthority(draft, resolution),
		Steps:   stepStatuses(draft),
	}
	if draft.EvaluationDigest != "" {
		evaluation, err := s.deps.Sandbox.ResolveEvaluation(ctx, draft.EvaluationDigest)
		if err != nil || evaluation.Digest != draft.EvaluationDigest || evaluation.ManifestDigest != draft.ManifestDigest {
			return StatusView{}, fmt.Errorf("%w: sandbox evidence no longer resolves", ErrEvaluation)
		}
		view.Sandbox = SandboxStatus{EvaluationDigest: evaluation.Digest, Passed: evaluation.Passed, Fresh: evaluation.Fresh}
	}
	if draft.Published && draft.ManifestDigest != "" {
		published, err := s.deps.Publisher.Admit(ctx, tenantID, agentID, draft.ManifestDigest)
		if err != nil {
			return StatusView{}, fmt.Errorf("resolve publication: %w", err)
		}
		view.Publication = PublicationStatus{Present: true, Generation: published.Generation, Version: published.ManifestVersion, Digest: published.ManifestDigest}
	}
	return view, nil
}

func (s *Service) load(ctx context.Context, tenantID, agentID string) (Draft, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(agentID) == "" {
		return Draft{}, fmt.Errorf("%w: tenant and agent are required", ErrInvalid)
	}
	draft, err := s.deps.Drafts.Load(ctx, tenantID, agentID)
	if err != nil {
		return Draft{}, ErrNotFound
	}
	if draft.TenantID != tenantID || draft.AgentID != agentID {
		return Draft{}, ErrNotFound
	}
	return draft, nil
}

func (s *Service) resolveCharter(ctx context.Context, tenantID string, draft Draft) (agentcharter.Result, error) {
	input, err := s.deps.Charters.ResolveLayers(ctx, tenantID, draft)
	if err != nil {
		return agentcharter.Result{}, fmt.Errorf("resolve charter: %w", err)
	}
	resolution, err := agentcharter.Resolve(input)
	if err != nil {
		return agentcharter.Result{}, fmt.Errorf("resolve charter: %w", err)
	}
	return resolution, nil
}

// PreviewAuthority explains the effective scope of the draft's requested tools
// and sources against a resolved charter. Deny precedence comes from the
// charter package; this function only renders per-item decisions with the
// deciding policy layers named.
func PreviewAuthority(draft Draft, resolution agentcharter.Result) AuthorityPreview {
	return AuthorityPreview{
		Tools:   previewItems(draft.Tools.CapabilityIDs, resolution.Tools),
		Sources: previewItems(draft.Context.SourceIDs, resolution.Sources),
	}
}

func previewItems(requested []string, effective agentcharter.ResolvedConstraint[agentcharter.CapabilityID]) []ScopeItem {
	items := make([]ScopeItem, 0, len(requested))
	for _, id := range requested {
		denied := containsID(effective.Denied, id)
		allowed := !denied && containsID(effective.Allowed, id)
		items = append(items, ScopeItem{ID: id, Allowed: allowed, Reason: scopeReason(id, allowed, denied, effective.Sources)})
	}
	return items
}

func containsID(haystack []agentcharter.CapabilityID, needle string) bool {
	for _, item := range haystack {
		if string(item) == needle {
			return true
		}
	}
	return false
}

func scopeReason(id string, allowed, denied bool, sources []agentcharter.Source) string {
	layers := make([]string, 0, len(sources))
	for _, source := range sources {
		layers = append(layers, layerName(source.Layer))
	}
	switch {
	case denied:
		if len(layers) == 0 {
			return id + " is denied by effective policy"
		}
		return id + " is denied by " + strings.Join(layers, ", ")
	case allowed:
		if len(layers) == 0 {
			return id + " is allowed by effective policy"
		}
		return id + " is allowed by " + strings.Join(layers, ", ")
	default:
		return id + " is outside the effective ceiling"
	}
}

func layerName(kind agentcharter.LayerKind) string {
	switch kind {
	case agentcharter.LayerPlatform:
		return "platform policy"
	case agentcharter.LayerTenant:
		return "tenant policy"
	case agentcharter.LayerEntity:
		return "organization policy"
	case agentcharter.LayerPack:
		return "starter pack"
	case agentcharter.LayerAgent:
		return "agent definition"
	case agentcharter.LayerInstallation:
		return "installation"
	case agentcharter.LayerRun:
		return "run"
	default:
		return "effective policy"
	}
}

func deniedItems(preview AuthorityPreview) []string {
	var denied []string
	for _, item := range append(append([]ScopeItem{}, preview.Tools...), preview.Sources...) {
		if !item.Allowed {
			denied = append(denied, item.ID)
		}
	}
	sort.Strings(denied)
	return denied
}

func stepStatuses(draft Draft) []StepStatus {
	done := map[Step]bool{
		StepJob:       strings.TrimSpace(draft.DisplayName) != "" && strings.TrimSpace(draft.Purpose) != "" && validDigest(draft.InstructionsDigest),
		StepCharter:   len(draft.Charter.OrganizationIDs) > 0,
		StepContext:   strings.TrimSpace(draft.Context.Audience) != "" && strings.TrimSpace(draft.Context.Purpose) != "",
		StepTools:     draft.Tools.CapabilityIDs != nil,
		StepTriggers:  len(draft.Triggers.Modes) > 0,
		StepBudgets:   draft.Budgets.MaxCostMicros > 0 && strings.TrimSpace(draft.Budgets.ModelPolicyID) != "",
		StepAuthority: draft.State == StateValidated || draft.State == StateReviewRequired || draft.State == StatePublished,
		StepSandbox:   draft.EvaluationDigest != "",
		StepReview:    draft.ReviewDigest != "" || draft.State == StatePublished,
	}
	statuses := make([]StepStatus, 0, len(OrderedSteps))
	currentAssigned := false
	for _, step := range OrderedSteps {
		status := StepStatus{Step: step, Done: done[step]}
		if !done[step] && !currentAssigned {
			status.Current = true
			currentAssigned = true
		}
		statuses = append(statuses, status)
	}
	if !currentAssigned && len(statuses) > 0 {
		statuses[len(statuses)-1].Current = true
	}
	return statuses
}

func applyStep(draft *Draft, step Step, update StepUpdate) error {
	switch step {
	case StepJob:
		if update.Job == nil {
			return fmt.Errorf("%w: job selection is required", ErrInvalid)
		}
		if err := validateJob(*update.Job); err != nil {
			return err
		}
		draft.DisplayName = update.Job.DisplayName
		draft.Purpose = update.Job.Purpose
		draft.InstructionsDigest = update.Job.InstructionsDigest
	case StepCharter:
		if update.Charter == nil {
			return fmt.Errorf("%w: charter selection is required", ErrInvalid)
		}
		if err := validateCharter(*update.Charter); err != nil {
			return err
		}
		draft.Charter = normalizeCharter(*update.Charter)
	case StepContext:
		if update.Context == nil {
			return fmt.Errorf("%w: context selection is required", ErrInvalid)
		}
		if err := validateContext(*update.Context); err != nil {
			return err
		}
		draft.Context = normalizeContext(*update.Context)
	case StepTools:
		if update.Tools == nil {
			return fmt.Errorf("%w: tool selection is required", ErrInvalid)
		}
		if err := validateIDs("capability", update.Tools.CapabilityIDs); err != nil {
			return err
		}
		draft.Tools = ToolSelection{CapabilityIDs: orderedIDs(update.Tools.CapabilityIDs)}
	case StepTriggers:
		if update.Triggers == nil {
			return fmt.Errorf("%w: trigger selection is required", ErrInvalid)
		}
		if err := validateTriggers(*update.Triggers); err != nil {
			return err
		}
		draft.Triggers = TriggerSelection{Modes: orderedIDs(update.Triggers.Modes), ChannelIDs: orderedIDs(update.Triggers.ChannelIDs), ReplyVisibility: update.Triggers.ReplyVisibility}
	case StepBudgets:
		if update.Budgets == nil {
			return fmt.Errorf("%w: budget selection is required", ErrInvalid)
		}
		if err := validateBudgets(*update.Budgets); err != nil {
			return err
		}
		draft.Budgets = *update.Budgets
	default:
		return fmt.Errorf("%w: step %q is derived from validation evidence and cannot be saved", ErrInvalid, step)
	}
	return nil
}

func validateJob(job JobSelection) error {
	if strings.TrimSpace(job.DisplayName) == "" || strings.TrimSpace(job.Purpose) == "" {
		return fmt.Errorf("%w: display name and purpose are required", ErrInvalid)
	}
	if !validDigest(job.InstructionsDigest) {
		return fmt.Errorf("%w: instructions must pin a sha256 digest", ErrInvalid)
	}
	return nil
}

func validateCharter(charter CharterSelection) error {
	if len(charter.OrganizationIDs) == 0 {
		return fmt.Errorf("%w: at least one organization is required", ErrInvalid)
	}
	if err := validateIDs("organization", charter.OrganizationIDs); err != nil {
		return err
	}
	if err := validateIDs("jurisdiction", charter.Jurisdictions); err != nil {
		return err
	}
	if err := validateIDs("data_category", charter.DataCategories); err != nil {
		return err
	}
	if strings.TrimSpace(charter.EscalationOwner) == "" || strings.TrimSpace(charter.SuccessMeasures) == "" {
		return fmt.Errorf("%w: escalation owner and success measures are required", ErrInvalid)
	}
	return nil
}

func validateContext(selection ContextSelection) error {
	if strings.TrimSpace(selection.Audience) == "" || strings.TrimSpace(selection.Purpose) == "" {
		return fmt.Errorf("%w: audience and purpose are required", ErrInvalid)
	}
	if err := validateIDs("source", selection.SourceIDs); err != nil {
		return err
	}
	return validateIDs("chat_space", selection.ChatSpaceIDs)
}

var allowedTriggerModes = map[string]bool{
	"DM": true, "MENTION": true, "SLASH": true, "CHANNEL_EVENT": true,
	"SCHEDULE": true, "DOMAIN_EVENT": true, "API": true, "WORKFLOW": true,
}

var allowedReplyVisibility = map[string]bool{"public": true, "private": true, "workflow_result": true}

func validateTriggers(triggers TriggerSelection) error {
	if len(triggers.Modes) == 0 {
		return fmt.Errorf("%w: at least one trigger mode is required", ErrInvalid)
	}
	for _, mode := range triggers.Modes {
		if !allowedTriggerModes[mode] {
			return fmt.Errorf("%w: trigger mode %q is not supported", ErrInvalid, mode)
		}
	}
	if !allowedReplyVisibility[triggers.ReplyVisibility] {
		return fmt.Errorf("%w: reply visibility must be public, private, or workflow_result", ErrInvalid)
	}
	return validateIDs("channel", triggers.ChannelIDs)
}

var allowedAutonomyCeilings = map[string]bool{"READ_ONLY": true, "ASSISTED": true, "SUPERVISED": true, "AUTONOMOUS": true}

func validateBudgets(budgets BudgetSelection) error {
	if budgets.MaxCostMicros == 0 || budgets.MaxInputTokens == 0 || budgets.MaxOutputTokens == 0 || budgets.MaxConcurrentRuns == 0 {
		return fmt.Errorf("%w: all budget bounds must be positive", ErrInvalid)
	}
	if !allowedAutonomyCeilings[budgets.AutonomyCeiling] {
		return fmt.Errorf("%w: autonomy ceiling %q is not supported", ErrInvalid, budgets.AutonomyCeiling)
	}
	if strings.TrimSpace(budgets.ModelPolicyID) == "" || budgets.ModelPolicyVersion == 0 {
		return fmt.Errorf("%w: model policy ID and positive version are required", ErrInvalid)
	}
	return nil
}

func validateIDs(kind string, ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
			return fmt.Errorf("%w: %s IDs must be canonical non-empty text", ErrInvalid, kind)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: duplicate %s ID %q", ErrInvalid, kind, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func normalizeCharter(charter CharterSelection) CharterSelection {
	return CharterSelection{
		OrganizationIDs: orderedIDs(charter.OrganizationIDs),
		Jurisdictions:   orderedIDs(charter.Jurisdictions),
		DataCategories:  orderedIDs(charter.DataCategories),
		EscalationOwner: charter.EscalationOwner,
		SuccessMeasures: charter.SuccessMeasures,
	}
}

func normalizeContext(selection ContextSelection) ContextSelection {
	return ContextSelection{
		SourceIDs:    orderedIDs(selection.SourceIDs),
		ChatSpaceIDs: orderedIDs(selection.ChatSpaceIDs),
		Audience:     selection.Audience,
		Purpose:      selection.Purpose,
	}
}

func orderedIDs(ids []string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
	return out
}

// buildManifest renders the canonical immutable manifest for draft content.
// Reference digests pin separately governed records by stable ID; the Studio
// never invents policy, it only names the records validation rechecks.
func buildManifest(draft Draft, version uint64) agentmanifest.Manifest {
	return agentmanifest.Manifest{
		SchemaVersion:      agentmanifest.CurrentSchemaVersion,
		ID:                 draft.AgentID,
		Version:            version,
		OwnerID:            draft.OwnerID,
		Purpose:            draft.Purpose,
		InstructionsDigest: draft.InstructionsDigest,
		SourceCeiling:      buildReferences(draft.Context.SourceIDs),
		ToolCeiling:        buildReferences(draft.Tools.CapabilityIDs),
		ModelPolicy:        agentmanifest.Reference{ID: draft.Budgets.ModelPolicyID, Version: draft.Budgets.ModelPolicyVersion, SchemaVersion: 1, Digest: referenceDigest(draft.Budgets.ModelPolicyID)},
		AutonomyCeiling:    draft.Budgets.AutonomyCeiling,
		Budget: agentmanifest.Budget{
			MaxCostMicros: draft.Budgets.MaxCostMicros, MaxInputTokens: draft.Budgets.MaxInputTokens,
			MaxOutputTokens: draft.Budgets.MaxOutputTokens, MaxConcurrentRuns: draft.Budgets.MaxConcurrentRuns,
		},
		OutputSchema:   agentmanifest.Reference{ID: "hcmnext.agent.output", Version: 1, SchemaVersion: 1, Digest: referenceDigest("hcmnext.agent.output")},
		ContextGrants:  buildReferences(draft.Context.ChatSpaceIDs),
		EvaluationRefs: []agentmanifest.Reference{{ID: "hcmnext.agent.sandbox", Version: 1, SchemaVersion: 1, Digest: referenceDigest("hcmnext.agent.sandbox")}},
	}
}

func buildReferences(ids []string) []agentmanifest.Reference {
	refs := make([]agentmanifest.Reference, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: referenceDigest(id)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs
}

func referenceDigest(id string) string {
	sum := sha256.Sum256([]byte("hcm-next-agent-studio-ref/v1\x00" + id))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == sha256.Size
}
