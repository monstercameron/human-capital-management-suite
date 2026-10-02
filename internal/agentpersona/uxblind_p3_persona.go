// Package agentpersona owns immutable, reviewed persona profiles.
//
// A persona is a profile over an agent manifest. It is not a principal and it
// cannot widen the authority represented by its exact skill pins.
package agentpersona

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

var (
	ErrInvalidProfile        = errors.New("agentpersona: invalid profile")
	ErrUnknownSkill          = errors.New("agentpersona: unknown skill")
	ErrRetiredSkill          = errors.New("agentpersona: retired skill")
	ErrSkillDigest           = errors.New("agentpersona: skill digest mismatch")
	ErrSkillTier             = errors.New("agentpersona: skill exceeds tier ceiling")
	ErrAudience              = errors.New("agentpersona: audience exceeds skill grant")
	ErrAudienceGrant         = errors.New("agentpersona: skill audience grant unavailable")
	ErrInstructionReference  = errors.New("agentpersona: instructions contain a tool, recipient or URL")
	ErrReviewRequired        = errors.New("agentpersona: independent review required")
	ErrEvaluation            = errors.New("agentpersona: evaluation is not fresh and passing")
	ErrImmutable             = errors.New("agentpersona: published version is immutable")
	ErrInvalidTransition     = errors.New("agentpersona: invalid lifecycle transition")
	ErrNotFound              = errors.New("agentpersona: persona version not found")
	ErrAlreadyExists         = errors.New("agentpersona: persona version already exists")
	ErrManifestCompatibility = errors.New("agentpersona: compatible agent manifest is required")
)

// LifecycleState is the state of the current persona pointer.
type LifecycleState string

const (
	StateDraft     LifecycleState = "DRAFT"
	StateInReview  LifecycleState = "IN_REVIEW"
	StatePublished LifecycleState = "PUBLISHED"
	StateSuspended LifecycleState = "SUSPENDED"
	StateRetired   LifecycleState = "RETIRED"
)

// PermissionPersonaReview is the permission required for a separate reviewer.
const PermissionPersonaReview = "persona:review"

// ConversationKind and ChannelClass are deliberately closed vocabularies at
// validation time, while remaining strings at the boundary for stable JSON.
type ConversationKind string
type ChannelClass string

const (
	ConversationDirect  ConversationKind = "DIRECT"
	ConversationGroup   ConversationKind = "GROUP_DM"
	ConversationChannel ConversationKind = "CHANNEL"
	ConversationThread  ConversationKind = "THREAD"

	ChannelPrivate  ChannelClass = "PRIVATE"
	ChannelPublic   ChannelClass = "PUBLIC"
	ChannelExternal ChannelClass = "EXTERNAL"
)

// AgentManifestRef pins the compatible agent manifest underneath a persona.
// The manifest itself remains owned by the agent package; this package only
// stores its immutable identity and asks a compatibility seam to validate it.
type AgentManifestRef struct {
	ID            string `json:"id"`
	Version       uint32 `json:"version"`
	Digest        string `json:"digest"`
	SchemaVersion uint32 `json:"schema_version"`
}

// Audience is the maximum audience in which a persona may be installed.
type Audience struct {
	Roles              []string `json:"roles"`
	Populations        []string `json:"populations"`
	OrganizationScopes []string `json:"organization_scopes"`
}

// SkillAudienceGrant is the administrator-granted reach of one skill. A
// persona audience must be contained by every pinned skill grant.
type SkillAudienceGrant struct {
	Roles              []string
	Populations        []string
	OrganizationScopes []string
}

// EvaluationLimits are immutable limits used by the persona evaluation gate.
type EvaluationLimits struct {
	MaxCost      int `json:"max_cost"`
	MaxSteps     int `json:"max_steps"`
	MaxLatencyMS int `json:"max_latency_ms"`
}

// PersonaProfile is the versioned profile stored on an agent manifest. Data
// classes are outputs of validation; callers must not use them as a grant.
type PersonaProfile struct {
	Manifest                 AgentManifestRef                                `json:"manifest"`
	PersonaID                string                                          `json:"persona_id"`
	Version                  uint32                                          `json:"version"`
	Handle                   string                                          `json:"handle"`
	DisplayName              string                                          `json:"display_name"`
	AvatarRef                string                                          `json:"avatar_ref"`
	Purpose                  string                                          `json:"purpose"`
	Audience                 Audience                                        `json:"audience"`
	SkillPins                []agentskills.SkillPin                          `json:"skill_pins"`
	TierCeiling              agentskills.SideEffectTier                      `json:"tier_ceiling"`
	ConversationKinds        []ConversationKind                              `json:"conversation_kinds"`
	ChannelClasses           []ChannelClass                                  `json:"channel_classes"`
	AllowedPlacementClasses  []string                                        `json:"allowed_placement_classes,omitempty"`
	AlwaysPrivate            bool                                            `json:"always_private,omitempty"`
	ConversationTierCeilings map[ConversationKind]agentskills.SideEffectTier `json:"conversation_tier_ceilings,omitempty"`
	Template                 *TemplateProvenance                             `json:"template_provenance,omitempty"`
	Instructions             string                                          `json:"instructions"`
	InstructionsDigest       string                                          `json:"instructions_digest"`
	Guidance                 string                                          `json:"guidance,omitempty"`
	DocumentReferences       []agentdocref.Reference                         `json:"document_references,omitempty"`
	Owner                    string                                          `json:"owner"`
	Steward                  string                                          `json:"steward"`
	EvalSuiteRef             string                                          `json:"eval_suite_ref"`
	EvalLimits               EvaluationLimits                                `json:"eval_limits"`
	DataClassesRead          []string                                        `json:"data_classes_read,omitempty"`
	DataClassesWritten       []string                                        `json:"data_classes_written,omitempty"`
}

// TemplateProvenance pins the immutable starter copied into this tenant-owned
// profile. It conveys no skill, source, or installation grants.
type TemplateProvenance struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
	Digest  string `json:"digest"`
}

// TierForConversation applies the profile's narrower per-conversation ceiling.
func (p PersonaProfile) TierForConversation(kind ConversationKind) agentskills.SideEffectTier {
	if ceiling, ok := p.ConversationTierCeilings[kind]; ok && ceiling < p.TierCeiling {
		return ceiling
	}
	return p.TierCeiling
}

// PersonaVersion is a sealed immutable profile and its derived reach.
type PersonaVersion struct {
	Profile                   PersonaProfile
	Digest                    string
	DerivedDataClassesRead    []string
	DerivedDataClassesWritten []string
}

// SkillResolver is the only skill-registry surface needed by personas.
type SkillResolver interface {
	ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error)
}

// SkillGrantResolver supplies administrator grants without importing the
// connection or delegation packages.
type SkillGrantResolver interface {
	AudienceGrant(agentskills.SkillPin) (SkillAudienceGrant, error)
}

// SkillGrantAudienceMatcher verifies an audience against the original grant
// tuples. Implementations preserve correlations between audience dimensions.
type SkillGrantAudienceMatcher interface {
	AllowsAudience(agentskills.SkillPin, Audience) (bool, error)
}

// ManifestCompatibility validates the referenced AGENT-007 manifest schema.
type ManifestCompatibility interface {
	Compatible(AgentManifestRef) error
}

// AgentManifestResolver loads the immutable AGENT-007 manifest named by a
// persona reference. Implementations resolve by identity and return the
// canonical version so the adapter can verify its digest.
type AgentManifestResolver interface {
	ResolveAgentManifest(AgentManifestRef) (agentmanifest.Manifest, error)
}

// ManifestCompatibilityAdapter connects persona references to the canonical
// AGENT-007 manifest validator.
type ManifestCompatibilityAdapter struct {
	Resolver AgentManifestResolver
}

// Compatible resolves and verifies the exact manifest identity pinned by ref.
func (a ManifestCompatibilityAdapter) Compatible(ref AgentManifestRef) error {
	if a.Resolver == nil {
		return ErrManifestCompatibility
	}
	manifest, err := a.Resolver.ResolveAgentManifest(ref)
	if err != nil {
		return errors.Join(ErrManifestCompatibility, err)
	}
	canonicalRef := agentmanifest.ManifestRef{
		ID: ref.ID, Version: uint64(ref.Version), SchemaVersion: ref.SchemaVersion, Digest: ref.Digest,
	}
	if err := agentmanifest.Compatible(canonicalRef, manifest); err != nil {
		return errors.Join(ErrManifestCompatibility, err)
	}
	return nil
}

// EvaluationRecord is the exact persona evaluation evidence required for
// publication. Profile and suite digests prevent a mutable pointer from
// reusing an evaluation after a material change.
type EvaluationRecord struct {
	ProfileDigest string
	SuiteRef      string
	RunDigest     string
	Passed        bool
	Fresh         bool
}

// EvaluationStore is intentionally read-only during publication.
type EvaluationStore interface {
	LookupEvaluation(runDigest string) (EvaluationRecord, error)
}

// ReviewAuthority proves that the reviewer has the persona-review permission.
type ReviewAuthority interface {
	CanReview(reviewer string, profile PersonaProfile) bool
}

// Validator validates a profile and derives its effective reach.
type Validator struct {
	Skills    SkillResolver
	Grants    SkillGrantResolver
	Manifests ManifestCompatibility
}

// ValidationResult is the trusted output of profile validation.
type ValidationResult struct {
	Profile            PersonaProfile
	Digest             string
	DataClassesRead    []string
	DataClassesWritten []string
	HighestSkillTier   agentskills.SideEffectTier
}

var referencePattern = regexp.MustCompile(`(?i)(https?://|ftp://|\btool\b|\brecipient\b|\b(send|email|notify)\s+to\b|[[:alnum:]._%+\-]+@[[:alnum:].\-]+\.[[:alpha:]]{2,})`)

// Validate checks the profile, exact pins, audience ceiling, and instruction
// boundary. It never mutates the caller's profile.
func (v Validator) Validate(profile PersonaProfile) (ValidationResult, error) {
	if err := validateShape(profile); err != nil {
		return ValidationResult{}, err
	}
	if referencePattern.MatchString(profile.Instructions) {
		return ValidationResult{}, fmt.Errorf("%w: instruction text may not name tools, recipients or URLs", ErrInstructionReference)
	}
	if profile.InstructionsDigest != "" && profile.InstructionsDigest != digestText(profile.Instructions) {
		return ValidationResult{}, fmt.Errorf("%w: instructions digest does not match text", ErrInvalidProfile)
	}
	if v.Skills == nil {
		return ValidationResult{}, fmt.Errorf("%w: skill resolver is required", ErrInvalidProfile)
	}
	if v.Grants == nil {
		return ValidationResult{}, ErrAudienceGrant
	}
	if v.Manifests == nil {
		return ValidationResult{}, ErrManifestCompatibility
	}
	if err := v.Manifests.Compatible(profile.Manifest); err != nil {
		return ValidationResult{}, errors.Join(ErrInvalidProfile, ErrManifestCompatibility, err)
	}

	read := map[string]struct{}{}
	written := map[string]struct{}{}
	highest := agentskills.TierRead
	for _, pin := range profile.SkillPins {
		record, err := v.Skills.ResolvePin(pin)
		if err != nil {
			if errors.Is(err, agentskills.ErrRetiredSkill) {
				return ValidationResult{}, errors.Join(ErrRetiredSkill, err)
			}
			if errors.Is(err, agentskills.ErrDigestMismatch) {
				return ValidationResult{}, errors.Join(ErrSkillDigest, err)
			}
			return ValidationResult{}, errors.Join(ErrUnknownSkill, err)
		}
		if record.Status == agentskills.StatusRetired {
			return ValidationResult{}, fmt.Errorf("%w: %s", ErrRetiredSkill, pin.Key())
		}
		tier := record.Definition.SideEffectTier
		if record.HighestCapabilityTier > tier {
			tier = record.HighestCapabilityTier
		}
		if tier > highest {
			highest = tier
		}
		if tier > profile.TierCeiling {
			return ValidationResult{}, fmt.Errorf("%w: %s requires %s, ceiling is %s", ErrSkillTier, pin.Key(), tier, profile.TierCeiling)
		}
		if instructionNamesReferenceSkill(profile.Instructions, record) {
			return ValidationResult{}, fmt.Errorf("%w: instruction text names pinned operation %s", ErrInstructionReference, record.Definition.ID)
		}
		allowed := false
		if matcher, ok := v.Grants.(SkillGrantAudienceMatcher); ok {
			allowed, err = matcher.AllowsAudience(pin, profile.Audience)
			if err != nil {
				return ValidationResult{}, errors.Join(ErrAudienceGrant, err)
			}
		} else {
			grant, grantErr := v.Grants.AudienceGrant(pin)
			if grantErr != nil {
				return ValidationResult{}, errors.Join(ErrAudienceGrant, grantErr)
			}
			allowed = audienceContained(profile.Audience, grant)
		}
		if !allowed {
			return ValidationResult{}, fmt.Errorf("%w: %s grant is narrower than persona audience", ErrAudience, pin.Key())
		}
		for _, value := range record.Definition.DataClassesRead {
			read[value] = struct{}{}
		}
		for _, value := range record.Definition.DataClassesWritten {
			written[value] = struct{}{}
		}
	}

	derivedRead := sortedSet(read)
	derivedWritten := sortedSet(written)
	if (len(profile.DataClassesRead) > 0 && !containsAll(profile.DataClassesRead, derivedRead)) || (len(profile.DataClassesWritten) > 0 && !containsAll(profile.DataClassesWritten, derivedWritten)) {
		return ValidationResult{}, fmt.Errorf("%w: declared data reach is narrower than pinned skills", ErrAudience)
	}
	sealed := cloneProfile(profile)
	sealed.InstructionsDigest = digestText(profile.Instructions)
	sealed.DataClassesRead = append([]string(nil), derivedRead...)
	sealed.DataClassesWritten = append([]string(nil), derivedWritten...)
	digest, err := profileDigest(sealed)
	if err != nil {
		return ValidationResult{}, err
	}
	return ValidationResult{Profile: sealed, Digest: digest, DataClassesRead: derivedRead, DataClassesWritten: derivedWritten, HighestSkillTier: highest}, nil
}

// Seal validates a profile that has already been checked for skill reach. It
// is useful to compute a stable digest for review/evaluation evidence.
func Seal(profile PersonaProfile) (PersonaVersion, error) {
	if err := validateShape(profile); err != nil {
		return PersonaVersion{}, err
	}
	profile.InstructionsDigest = digestText(profile.Instructions)
	digest, err := profileDigest(profile)
	if err != nil {
		return PersonaVersion{}, err
	}
	return PersonaVersion{Profile: cloneProfile(profile), Digest: digest, DerivedDataClassesRead: append([]string(nil), profile.DataClassesRead...), DerivedDataClassesWritten: append([]string(nil), profile.DataClassesWritten...)}, nil
}

// Build validates and seals one profile.
func (v Validator) Build(profile PersonaProfile) (PersonaVersion, error) {
	result, err := v.Validate(profile)
	if err != nil {
		return PersonaVersion{}, err
	}
	return PersonaVersion{Profile: result.Profile, Digest: result.Digest, DerivedDataClassesRead: result.DataClassesRead, DerivedDataClassesWritten: result.DataClassesWritten}, nil
}

// ReviewRequest is an independent approval over one exact profile digest.
type ReviewRequest struct {
	Reviewer   string
	Permission string
	Decision   string
}

// ReviewRecord is immutable review evidence.
type ReviewRecord struct {
	ProfileDigest string
	Reviewer      string
	Permission    string
	Digest        string
}

// PublishRequest binds independent review and evaluation evidence.
type PublishRequest struct {
	Review           ReviewRecord
	EvaluationDigest string
}

// Publication is the exact evidence pinned to a published pointer.
type Publication struct {
	ProfileDigest    string
	ReviewDigest     string
	EvaluationDigest string
	Reviewer         string
	Generation       uint64
}

type storedVersion struct {
	version     PersonaVersion
	publication Publication
	published   bool
	review      *ReviewRecord
}

type personaState struct {
	versions map[uint32]*storedVersion
	current  uint32
	state    LifecycleState
}

// Dependencies are the external, read-only seams used by Manager.
type Dependencies struct {
	Validator   Validator
	Evaluations EvaluationStore
	Reviews     ReviewAuthority
}

// Manager owns the lifecycle pointer while each stored version remains
// immutable. Publication performs all external checks before changing state.
type Manager struct {
	mu       sync.RWMutex
	deps     Dependencies
	personas map[string]*personaState
	sequence uint64
}

// NewManager returns an empty persona lifecycle manager.
func NewManager(deps Dependencies) *Manager {
	return &Manager{deps: deps, personas: make(map[string]*personaState)}
}

// Register adds one immutable draft version.
func (m *Manager) Register(version PersonaVersion) error {
	if m == nil {
		return ErrInvalidProfile
	}
	if err := version.Verify(); err != nil {
		return err
	}
	validated, err := m.deps.Validator.Build(version.Profile)
	if err != nil {
		return err
	}
	if validated.Digest != version.Digest {
		return ErrImmutable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.personas[version.Profile.PersonaID]
	if state == nil {
		state = &personaState{versions: make(map[uint32]*storedVersion), current: version.Profile.Version, state: StateDraft}
		m.personas[version.Profile.PersonaID] = state
	}
	if _, exists := state.versions[version.Profile.Version]; exists {
		return ErrAlreadyExists
	}
	state.versions[version.Profile.Version] = &storedVersion{version: cloneVersion(version)}
	return nil
}

// Review records a separate review for a draft version.
func (m *Manager) Review(personaID string, version uint32, request ReviewRequest) (ReviewRecord, error) {
	if m == nil || m.deps.Reviews == nil {
		return ReviewRecord{}, ErrReviewRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, state, err := m.lookupLocked(personaID, version)
	if err != nil {
		return ReviewRecord{}, err
	}
	if strings.TrimSpace(request.Reviewer) == "" || request.Reviewer == stored.version.Profile.Owner || request.Permission != PermissionPersonaReview || request.Decision != "APPROVE" {
		return ReviewRecord{}, ErrReviewRequired
	}
	if stored.published {
		return ReviewRecord{}, ErrImmutable
	}
	if !m.deps.Reviews.CanReview(request.Reviewer, stored.version.Profile) {
		return ReviewRecord{}, ErrReviewRequired
	}
	record := ReviewRecord{ProfileDigest: stored.version.Digest, Reviewer: request.Reviewer, Permission: request.Permission}
	record.Digest = digestReview(record)
	stored.review = &record
	state.state = StateInReview
	return record, nil
}

// Publish moves an exact draft through the publication gate. No pointer is
// changed until review and evaluation have both been verified.
func (m *Manager) Publish(personaID string, version uint32, request PublishRequest) (Publication, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, state, err := m.lookupLocked(personaID, version)
	if err != nil {
		return Publication{}, err
	}
	if stored.published {
		return Publication{}, ErrImmutable
	}
	if stored.review == nil || stored.review.Digest != request.Review.Digest || stored.review.ProfileDigest != request.Review.ProfileDigest || stored.review.Reviewer != request.Review.Reviewer || stored.review.Permission != request.Review.Permission {
		return Publication{}, ErrReviewRequired
	}
	if err := verifyReview(stored.version, request.Review); err != nil {
		return Publication{}, err
	}
	if m.deps.Reviews == nil || !m.deps.Reviews.CanReview(request.Review.Reviewer, stored.version.Profile) {
		return Publication{}, ErrReviewRequired
	}
	validated, err := m.deps.Validator.Build(stored.version.Profile)
	if err != nil {
		return Publication{}, err
	}
	if validated.Digest != stored.version.Digest {
		return Publication{}, ErrImmutable
	}
	evaluation, err := m.lookupEvaluation(request.EvaluationDigest)
	if err != nil {
		return Publication{}, errors.Join(ErrEvaluation, err)
	}
	if evaluation.ProfileDigest != stored.version.Digest || evaluation.SuiteRef != stored.version.Profile.EvalSuiteRef || evaluation.RunDigest != request.EvaluationDigest || !evaluation.Passed || !evaluation.Fresh {
		return Publication{}, ErrEvaluation
	}
	m.sequence++
	publication := Publication{ProfileDigest: stored.version.Digest, ReviewDigest: request.Review.Digest, EvaluationDigest: request.EvaluationDigest, Reviewer: request.Review.Reviewer, Generation: m.sequence}
	stored.publication = publication
	stored.published = true
	state.current = version
	state.state = StatePublished
	return publication, nil
}

// Suspend fences the current pointer without deleting its evidence.
func (m *Manager) Suspend(personaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.personas[personaID]
	if !ok || state.current == 0 {
		return ErrNotFound
	}
	if !state.versions[state.current].published {
		return ErrInvalidTransition
	}
	state.state = StateSuspended
	return nil
}

// Rollback restores a previously published version only after rechecking its
// exact skill pins and the freshness of its original evaluation evidence.
func (m *Manager) Rollback(personaID string, version uint32) (Publication, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, state, err := m.lookupLocked(personaID, version)
	if err != nil {
		return Publication{}, err
	}
	if !stored.published {
		return Publication{}, ErrInvalidTransition
	}
	if _, err := m.deps.Validator.Build(stored.version.Profile); err != nil {
		return Publication{}, errors.Join(ErrEvaluation, err)
	}
	evaluation, err := m.lookupEvaluation(stored.publication.EvaluationDigest)
	if err != nil || evaluation.ProfileDigest != stored.version.Digest || evaluation.SuiteRef != stored.version.Profile.EvalSuiteRef || evaluation.RunDigest != stored.publication.EvaluationDigest || !evaluation.Passed || !evaluation.Fresh {
		return Publication{}, ErrEvaluation
	}
	m.sequence++
	publication := stored.publication
	publication.Generation = m.sequence
	state.current = version
	state.state = StatePublished
	return publication, nil
}

// Current returns the current immutable version and lifecycle state.
func (m *Manager) Current(personaID string) (PersonaVersion, LifecycleState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.personas[personaID]
	if !ok || state.current == 0 {
		return PersonaVersion{}, "", ErrNotFound
	}
	stored := state.versions[state.current]
	return cloneVersion(stored.version), state.state, nil
}

func (m *Manager) lookupLocked(personaID string, version uint32) (*storedVersion, *personaState, error) {
	state, ok := m.personas[personaID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	stored, ok := state.versions[version]
	if !ok {
		return nil, nil, ErrNotFound
	}
	return stored, state, nil
}

func (m *Manager) lookupEvaluation(digest string) (EvaluationRecord, error) {
	if m.deps.Evaluations == nil || strings.TrimSpace(digest) == "" {
		return EvaluationRecord{}, ErrEvaluation
	}
	return m.deps.Evaluations.LookupEvaluation(digest)
}

func (v PersonaVersion) Verify() error {
	if v.Digest == "" {
		return ErrInvalidProfile
	}
	digest, err := profileDigest(v.Profile)
	if err != nil || digest != v.Digest {
		return ErrImmutable
	}
	return nil
}

func verifyReview(version PersonaVersion, review ReviewRecord) error {
	if review.ProfileDigest != version.Digest || strings.TrimSpace(review.Reviewer) == "" || review.Reviewer == version.Profile.Owner || review.Permission != PermissionPersonaReview || review.Digest != digestReview(ReviewRecord{ProfileDigest: review.ProfileDigest, Reviewer: review.Reviewer, Permission: review.Permission}) {
		return ErrReviewRequired
	}
	return nil
}

func validateShape(p PersonaProfile) error {
	if err := agentdocref.Validate(p.DocumentReferences, agentdocref.MaxPersonaReferences); err != nil {
		return fmt.Errorf("%w: document references: %w", ErrInvalidProfile, err)
	}
	if err := agentdocref.ValidateGuidance(p.Guidance, p.DocumentReferences); err != nil {
		return fmt.Errorf("%w: guidance: %w", ErrInvalidProfile, err)
	}
	if p.Template != nil && (strings.TrimSpace(p.Template.ID) == "" || p.Template.Version == 0 || len(p.Template.Digest) != len("sha256:")+64 || !strings.HasPrefix(p.Template.Digest, "sha256:")) {
		return fmt.Errorf("%w: incomplete starter provenance", ErrInvalidProfile)
	}
	for kind, ceiling := range p.ConversationTierCeilings {
		found := false
		for _, allowed := range p.ConversationKinds {
			if allowed == kind {
				found = true
				break
			}
		}
		if !found || !ceiling.Valid() || ceiling > p.TierCeiling {
			return fmt.Errorf("%w: invalid conversation tier ceiling", ErrInvalidProfile)
		}
	}
	if len(p.AllowedPlacementClasses) > 0 {
		if err := validateList("placement class", p.AllowedPlacementClasses); err != nil {
			return err
		}
	}
	if strings.TrimSpace(p.Manifest.ID) == "" || p.Manifest.Version == 0 || strings.TrimSpace(p.Manifest.Digest) == "" || p.Manifest.SchemaVersion == 0 {
		return fmt.Errorf("%w: manifest reference is incomplete", ErrInvalidProfile)
	}
	for name, value := range map[string]string{"persona_id": p.PersonaID, "handle": p.Handle, "display_name": p.DisplayName, "avatar_ref": p.AvatarRef, "purpose": p.Purpose, "owner": p.Owner, "steward": p.Steward, "eval_suite_ref": p.EvalSuiteRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidProfile, name)
		}
	}
	if p.Version == 0 || len(p.SkillPins) == 0 || !p.TierCeiling.Valid() || p.TierCeiling > agentskills.TierT3 {
		return fmt.Errorf("%w: version, skills, or tier ceiling is invalid", ErrInvalidProfile)
	}
	if referencePattern.MatchString(p.Instructions) {
		return ErrInstructionReference
	}
	if p.EvalLimits.MaxCost <= 0 || p.EvalLimits.MaxSteps <= 0 || p.EvalLimits.MaxLatencyMS <= 0 {
		return fmt.Errorf("%w: evaluation limits must be positive", ErrInvalidProfile)
	}
	if err := validateList("audience role", p.Audience.Roles); err != nil {
		return err
	}
	if err := validateList("audience population", p.Audience.Populations); err != nil {
		return err
	}
	if err := validateList("organization scope", p.Audience.OrganizationScopes); err != nil {
		return err
	}
	if len(p.ConversationKinds) == 0 || len(p.ChannelClasses) == 0 {
		return fmt.Errorf("%w: conversation and channel classes are required", ErrInvalidProfile)
	}
	for _, kind := range p.ConversationKinds {
		if kind != ConversationDirect && kind != ConversationGroup && kind != ConversationChannel && kind != ConversationThread {
			return fmt.Errorf("%w: unknown conversation kind %q", ErrInvalidProfile, kind)
		}
	}
	for _, class := range p.ChannelClasses {
		if class != ChannelPrivate && class != ChannelPublic && class != ChannelExternal {
			return fmt.Errorf("%w: unknown channel class %q", ErrInvalidProfile, class)
		}
	}
	seen := map[agentskills.SkillKey]struct{}{}
	for _, pin := range p.SkillPins {
		if strings.TrimSpace(pin.ID) == "" || pin.Version == 0 || strings.TrimSpace(pin.Digest) == "" {
			return fmt.Errorf("%w: skill pin is incomplete", ErrInvalidProfile)
		}
		if _, exists := seen[pin.Key()]; exists {
			return fmt.Errorf("%w: duplicate skill pin %s", ErrInvalidProfile, pin.Key())
		}
		seen[pin.Key()] = struct{}{}
	}
	return nil
}

func validateList(label string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("%w: %s is required", ErrInvalidProfile, label)
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is invalid", ErrInvalidProfile, label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%w: duplicate %s", ErrInvalidProfile, label)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func audienceContained(a Audience, grant SkillAudienceGrant) bool {
	return containsAllGrant(grant.Roles, a.Roles) && containsAllGrant(grant.Populations, a.Populations) && containsAllGrant(grant.OrganizationScopes, a.OrganizationScopes)
}

func instructionNamesReferenceSkill(text string, record agentskills.SkillRecord) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, strings.ToLower(record.Definition.ID)) {
		return true
	}
	for _, operation := range record.Definition.Operations {
		if operation.Operation != "" && strings.Contains(lower, strings.ToLower(operation.Operation)) {
			return true
		}
		if operation.Capability.ID != "" && strings.Contains(lower, strings.ToLower(operation.Capability.ID)) {
			return true
		}
	}
	return false
}

func containsAllGrant(grant, requested []string) bool {
	for _, want := range requested {
		found := false
		for _, have := range grant {
			if have == want || have == "*" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsAll(union, declared []string) bool {
	for _, value := range declared {
		if !sortSearch(union, value) {
			return false
		}
	}
	return true
}

func sortSearch(values []string, want string) bool {
	i := sort.SearchStrings(values, want)
	return i < len(values) && values[i] == want
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func digestText(text string) string {
	sum := sha256.Sum256(append([]byte("hcm-next-agent-persona-instructions/v1\x00"), []byte(text)...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func profileDigest(profile PersonaProfile) (string, error) {
	p := cloneProfile(profile)
	p.SkillPins = sortedPins(p.SkillPins)
	p.Audience.Roles = sortedCopy(p.Audience.Roles)
	p.Audience.Populations = sortedCopy(p.Audience.Populations)
	p.Audience.OrganizationScopes = sortedCopy(p.Audience.OrganizationScopes)
	p.ConversationKinds = sortedConversations(p.ConversationKinds)
	p.ChannelClasses = sortedChannels(p.ChannelClasses)
	p.AllowedPlacementClasses = sortedCopy(p.AllowedPlacementClasses)
	p.DataClassesRead = sortedCopy(p.DataClassesRead)
	p.DataClassesWritten = sortedCopy(p.DataClassesWritten)
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("%w: profile digest: %v", ErrInvalidProfile, err)
	}
	sum := sha256.Sum256(append([]byte("hcm-next-agent-persona/v1\x00"), b...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func digestReview(review ReviewRecord) string {
	b, _ := json.Marshal(struct{ ProfileDigest, Reviewer, Permission string }{review.ProfileDigest, review.Reviewer, review.Permission})
	sum := sha256.Sum256(append([]byte("hcm-next-agent-persona-review/v1\x00"), b...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func cloneVersion(v PersonaVersion) PersonaVersion {
	v.Profile = cloneProfile(v.Profile)
	v.DerivedDataClassesRead = append([]string(nil), v.DerivedDataClassesRead...)
	v.DerivedDataClassesWritten = append([]string(nil), v.DerivedDataClassesWritten...)
	return v
}

func cloneProfile(p PersonaProfile) PersonaProfile {
	p.AllowedPlacementClasses = append([]string(nil), p.AllowedPlacementClasses...)
	if p.Template != nil {
		copied := *p.Template
		p.Template = &copied
	}
	if p.ConversationTierCeilings != nil {
		ceilings := make(map[ConversationKind]agentskills.SideEffectTier, len(p.ConversationTierCeilings))
		for kind, ceiling := range p.ConversationTierCeilings {
			ceilings[kind] = ceiling
		}
		p.ConversationTierCeilings = ceilings
	}
	p.Audience.Roles = append([]string(nil), p.Audience.Roles...)
	p.Audience.Populations = append([]string(nil), p.Audience.Populations...)
	p.Audience.OrganizationScopes = append([]string(nil), p.Audience.OrganizationScopes...)
	p.SkillPins = append([]agentskills.SkillPin(nil), p.SkillPins...)
	p.ConversationKinds = append([]ConversationKind(nil), p.ConversationKinds...)
	p.ChannelClasses = append([]ChannelClass(nil), p.ChannelClasses...)
	p.DocumentReferences = append([]agentdocref.Reference(nil), p.DocumentReferences...)
	p.DataClassesRead = append([]string(nil), p.DataClassesRead...)
	p.DataClassesWritten = append([]string(nil), p.DataClassesWritten...)
	return p
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
func sortedPins(values []agentskills.SkillPin) []agentskills.SkillPin {
	out := append([]agentskills.SkillPin(nil), values...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Version < out[j].Version
	})
	return out
}
func sortedConversations(values []ConversationKind) []ConversationKind {
	out := append([]ConversationKind(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func sortedChannels(values []ChannelClass) []ChannelClass {
	out := append([]ChannelClass(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
