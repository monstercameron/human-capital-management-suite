// Package agentinvoke resolves persona mentions at the chat post boundary.
//
// The package deliberately owns only the invocation seam. Chat owns durable
// posts, delegation owns credential exchange, and the run service owns model
// execution. The ports below let those packages be composed without allowing
// a persona definition or an installation to become an authority source.
package agentinvoke

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MentionKind string

const PersonaMention MentionKind = "PERSONA_MENTION"

type AuthorKind string

const (
	HumanAuthor   AuthorKind = "HUMAN"
	PersonaAuthor AuthorKind = "PERSONA"
	AgentAuthor   AuthorKind = "AGENT"
	BotAuthor     AuthorKind = "BOT"
)

// Mention is the canonical reference emitted by Chat. Display is cosmetic
// and is never consulted for identity or authority.
type Mention struct {
	Kind      MentionKind
	PersonaID string
	Display   string
	Canonical bool
}

// PostCommit is the immutable post-commit projection consumed by this lane.
// New is explicit so a replayed delivery, edit, quote, or forward cannot be
// mistaken for the original authoring event.
type PostCommit struct {
	TenantID       string
	ConversationID string
	ThreadID       string
	PostID         string
	AuthorID       string
	AuthorKind     AuthorKind
	Body           string
	New            bool
	Edited         bool
	Quoted         bool
	Forwarded      bool
	Mentions       []Mention
}

type SkillScopes map[string][]string

// Clone returns a detached skill set suitable for a durable record.
func (s SkillScopes) Clone() SkillScopes {
	out := make(SkillScopes, len(s))
	for skill, scopes := range s {
		out[skill] = sortedUnique(scopes)
	}
	return out
}

// IntersectSkillScopes is the authority operation used at mention admission.
// A skill survives only when it is present, with the exact scope, in every
// operand. Empty or missing operands therefore fail closed.
func IntersectSkillScopes(sets ...SkillScopes) SkillScopes {
	if len(sets) == 0 {
		return SkillScopes{}
	}
	result := sets[0].Clone()
	for _, set := range sets[1:] {
		for skill, scopes := range result {
			allowed := make(map[string]struct{}, len(set[skill]))
			for _, scope := range set[skill] {
				allowed[scope] = struct{}{}
			}
			kept := make([]string, 0, len(scopes))
			for _, scope := range scopes {
				if _, ok := allowed[scope]; ok {
					kept = append(kept, scope)
				}
			}
			if len(kept) == 0 {
				delete(result, skill)
			} else {
				result[skill] = sortedUnique(kept)
			}
		}
	}
	return result
}

func SkillScopesSubset(child, parent SkillScopes) bool {
	for skill, scopes := range child {
		allowed := make(map[string]struct{}, len(parent[skill]))
		for _, scope := range parent[skill] {
			allowed[scope] = struct{}{}
		}
		for _, scope := range scopes {
			if _, ok := allowed[scope]; !ok {
				return false
			}
		}
	}
	return true
}

type Persona struct {
	ID             string
	Version        string
	PinnedSkills   SkillScopes
	Audience       string
	Current        bool
	Suspended      bool
	InstallationID string
}

type Installation struct {
	ID           string
	Current      bool
	Suspended    bool
	SkillCeiling SkillScopes
}

type ChannelPolicy struct {
	SkillCeiling SkillScopes
}

// AuthorityResolver performs all current membership, installation, audience,
// and per-user discovery checks in one server-owned call.
type AuthorityResolver interface {
	Resolve(context.Context, AdmissionRequest) (Admission, error)
}

type AdmissionRequest struct {
	TenantID       string
	ConversationID string
	InvokerID      string
	PersonaID      string
}

type Admission struct {
	Persona          Persona
	Installation     Installation
	Channel          ChannelPolicy
	Discoverable     SkillScopes
	HumanMember      bool
	AudienceMember   bool
	PersonaInstalled bool
}

type GrantRequest struct {
	InvocationID   string
	UserID         string
	TenantID       string
	AgentVersion   string
	TargetAgentID  string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	ConversationID string
	ThreadID       string
	InvokingPostID string
	Purpose        string
	Skills         SkillScopes
	Mode           RunMode
	ExpiresAt      time.Time
}

type DelegationGrant struct {
	ID            string
	UserID        string
	TenantID      string
	TaskID        string
	TargetAgentID string
	Skills        SkillScopes
	ExpiresAt     time.Time
}

type GrantIssuer interface {
	CreateOnBehalfOfGrant(context.Context, GrantRequest) (DelegationGrant, error)
}

type RunMode string

const OnBehalfOf RunMode = "ON_BEHALF_OF"

type RunRequest struct {
	InvocationID   string
	TenantID       string
	ConversationID string
	ThreadID       string
	InvokingPostID string
	InvokerID      string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	Mode           RunMode
	Grant          DelegationGrant
	Skills         SkillScopes
	Context        BoundContext
	Actor          ActorChain
}

type RunStarter interface {
	// Start must be idempotent by InvocationID. A post-commit delivery may
	// retry after a process interruption between durable run creation and the
	// invocation repository's STARTED confirmation.
	Start(context.Context, RunRequest) error
}

// TargetAgentResolver optionally resolves the exact immutable agent identity
// from trusted run facts before a grant is issued. Legacy starters may omit it.
type TargetAgentResolver interface {
	ResolveTargetAgentID(context.Context, RunRequest) (string, error)
}

type EphemeralRefusal struct {
	TenantID, ConversationID, ThreadID, PostID, InvokerID, PersonaID string
	Reason                                                           DenialReason
}

type EphemeralResponder interface {
	Refuse(context.Context, EphemeralRefusal) error
}

type DenialReason string

const (
	DenialInvalidPost       DenialReason = "INVALID_POST"
	DenialNotCanonical      DenialReason = "NOT_CANONICAL"
	DenialNotHuman          DenialReason = "AUTHOR_NOT_HUMAN"
	DenialNotMember         DenialReason = "AUTHOR_NOT_MEMBER"
	DenialNotInstalled      DenialReason = "PERSONA_NOT_INSTALLED"
	DenialNotCurrent        DenialReason = "PERSONA_NOT_CURRENT"
	DenialSuspended         DenialReason = "PERSONA_SUSPENDED"
	DenialNotAudience       DenialReason = "AUTHOR_NOT_IN_AUDIENCE"
	DenialNoEffectiveSkills DenialReason = "NO_EFFECTIVE_SKILLS"
)

var (
	ErrInvalidRequest = errors.New("agentinvoke: invalid request")
	ErrDenied         = errors.New("agentinvoke: mention denied")
	ErrDuplicate      = errors.New("agentinvoke: duplicate invocation")
	ErrConflict       = errors.New("agentinvoke: invocation conflict")
)

type EligibilityError struct {
	Reason DenialReason
	Detail string
}

func (e *EligibilityError) Error() string {
	if e == nil {
		return ErrDenied.Error()
	}
	return fmt.Sprintf("%s: %s", e.Reason, e.Detail)
}

func (e *EligibilityError) Unwrap() error { return ErrDenied }

type ActorChain struct {
	UserID         string `json:"user_id"`
	PersonaID      string `json:"persona_id"`
	PersonaVersion string `json:"persona_version"`
	InstallationID string `json:"installation_id"`
	ConversationID string `json:"conversation_id"`
	InvokingPostID string `json:"invoking_post_id"`
	InvocationID   string `json:"invocation_id"`
}

func (a ActorChain) Validate() error {
	for name, value := range map[string]string{
		"user_id": a.UserID, "persona_id": a.PersonaID,
		"persona_version": a.PersonaVersion, "installation_id": a.InstallationID,
		"conversation_id": a.ConversationID, "invoking_post_id": a.InvokingPostID,
		"invocation_id": a.InvocationID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: actor %s is required", ErrInvalidRequest, name)
		}
	}
	return nil
}

type InvocationState string

const (
	InvocationClaimed InvocationState = "CLAIMED"
	InvocationStarted InvocationState = "STARTED"
)

type Invocation struct {
	ID             string
	TenantID       string
	ConversationID string
	ThreadID       string
	PostID         string
	InvokerID      string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	Mode           RunMode
	Skills         SkillScopes
	Grant          DelegationGrant
	Actor          ActorChain
	State          InvocationState
}

type InvocationRepository interface {
	Claim(context.Context, Invocation) (Invocation, bool, error)
	SetGrant(context.Context, string, string, DelegationGrant) (Invocation, error)
	MarkStarted(context.Context, string, string) (bool, error)
}

type Service struct {
	authority     AuthorityResolver
	grants        GrantIssuer
	runs          RunStarter
	repo          InvocationRepository
	ephemeral     EphemeralResponder
	threads       ThreadReader
	peerExtractor PeerExtractor
	clock         func() time.Time
	newID         func(PostCommit, string) string
	startMu       sync.Mutex
	starts        map[string]*invocationStartLock
}

type invocationStartLock struct {
	mu   sync.Mutex
	refs int
}

type Config struct {
	Authority     AuthorityResolver
	Grants        GrantIssuer
	Runs          RunStarter
	Repository    InvocationRepository
	Ephemeral     EphemeralResponder
	Threads       ThreadReader
	PeerExtractor PeerExtractor
	Now           func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	if cfg.Authority == nil || cfg.Grants == nil || cfg.Runs == nil || cfg.Repository == nil {
		return nil, fmt.Errorf("%w: authority, grants, runs and repository are required", ErrInvalidRequest)
	}
	if cfg.Threads != nil && cfg.PeerExtractor == nil {
		return nil, fmt.Errorf("%w: peer extractor is required when thread context is enabled", ErrInvalidRequest)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{authority: cfg.Authority, grants: cfg.Grants, runs: cfg.Runs, repo: cfg.Repository, ephemeral: cfg.Ephemeral, threads: cfg.Threads, peerExtractor: cfg.PeerExtractor, clock: now, newID: invocationID, starts: make(map[string]*invocationStartLock)}, nil
}

// ResolveMention processes every canonical persona reference in one newly
// committed human post. A duplicate delivery claims the same key and cannot
// start another run.
func (s *Service) ResolveMention(ctx context.Context, post PostCommit) ([]Invocation, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil service", ErrInvalidRequest)
	}
	if post.AuthorKind != HumanAuthor {
		// Machine-authored posts are inert even when their reference metadata is
		// malformed. Do not send refusal posts to a bot or another persona.
		return nil, nil
	}
	if err := validatePost(post); err != nil {
		var denied *EligibilityError
		if errors.As(err, &denied) {
			if sendErr := s.refuseAll(ctx, post, denied.Reason); sendErr != nil {
				return nil, sendErr
			}
			return nil, nil
		}
		return nil, err
	}
	seen := make(map[string]struct{})
	result := make([]Invocation, 0, len(post.Mentions))
	for _, mention := range post.Mentions {
		if mention.Kind != PersonaMention || !mention.Canonical || strings.TrimSpace(mention.PersonaID) == "" {
			continue
		}
		if _, duplicate := seen[mention.PersonaID]; duplicate {
			continue
		}
		seen[mention.PersonaID] = struct{}{}
		invocation, err := s.resolveOne(ctx, post, mention.PersonaID)
		if err != nil {
			var denied *EligibilityError
			if errors.As(err, &denied) {
				if sendErr := s.sendRefusal(ctx, post, mention.PersonaID, denied.Reason); sendErr != nil {
					return result, sendErr
				}
				continue
			}
			return result, err
		}
		result = append(result, invocation)
	}
	return result, nil
}

// OnPostCommit is the explicit server-boundary spelling retained for callers
// that model Chat's event name rather than the mention operation.
func (s *Service) OnPostCommit(ctx context.Context, post PostCommit) ([]Invocation, error) {
	return s.ResolveMention(ctx, post)
}

func (s *Service) resolveOne(ctx context.Context, post PostCommit, personaID string) (Invocation, error) {
	admission, err := s.authority.Resolve(ctx, AdmissionRequest{TenantID: post.TenantID, ConversationID: post.ConversationID, InvokerID: post.AuthorID, PersonaID: personaID})
	if err != nil {
		return Invocation{}, err
	}
	if !admission.HumanMember {
		return Invocation{}, &EligibilityError{Reason: DenialNotMember, Detail: "the author is not a current member"}
	}
	if !admission.PersonaInstalled {
		return Invocation{}, &EligibilityError{Reason: DenialNotInstalled, Detail: "the persona is not installed in this conversation"}
	}
	if admission.Persona.ID == "" || admission.Persona.ID != personaID || (admission.Persona.InstallationID != "" && admission.Persona.InstallationID != admission.Installation.ID) {
		return Invocation{}, &EligibilityError{Reason: DenialNotInstalled, Detail: "authority returned a different persona"}
	}
	if !admission.Installation.Current || admission.Persona.Suspended || admission.Installation.Suspended {
		return Invocation{}, &EligibilityError{Reason: DenialSuspended, Detail: "the persona installation is not active"}
	}
	if !admission.Persona.Current {
		return Invocation{}, &EligibilityError{Reason: DenialNotCurrent, Detail: "the persona version is not current"}
	}
	if !admission.AudienceMember {
		return Invocation{}, &EligibilityError{Reason: DenialNotAudience, Detail: "the author is outside the persona audience"}
	}
	skills := IntersectSkillScopes(admission.Persona.PinnedSkills, admission.Installation.SkillCeiling, admission.Channel.SkillCeiling, admission.Discoverable)
	if len(skills) == 0 {
		return Invocation{}, &EligibilityError{Reason: DenialNoEffectiveSkills, Detail: "no skill is available to this invoker in this channel"}
	}
	id := s.newID(post, personaID)
	releaseStart := s.lockInvocationStart(id)
	defer releaseStart()
	actor := ActorChain{UserID: post.AuthorID, PersonaID: personaID, PersonaVersion: admission.Persona.Version, InstallationID: admission.Installation.ID, ConversationID: post.ConversationID, InvokingPostID: post.PostID, InvocationID: id}
	if err := actor.Validate(); err != nil {
		return Invocation{}, err
	}
	candidate := Invocation{ID: id, TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: post.ThreadID, PostID: post.PostID, InvokerID: post.AuthorID, PersonaID: personaID, PersonaVersion: admission.Persona.Version, InstallationID: admission.Installation.ID, Mode: OnBehalfOf, Skills: skills, Actor: actor, State: InvocationClaimed}
	claimed, created, err := s.repo.Claim(ctx, candidate)
	if err != nil {
		return Invocation{}, err
	}
	if !created && claimed.State == InvocationStarted {
		return claimed, nil
	}
	if claimed.Grant.ID == "" {
		targetAgentID := ""
		if resolver, ok := s.runs.(TargetAgentResolver); ok {
			targetAgentID, err = resolver.ResolveTargetAgentID(ctx, invocationRunRequest(claimed))
			if err != nil || !validTargetAgentID(targetAgentID) {
				return Invocation{}, fmt.Errorf("%w: trusted target agent identity unavailable", ErrInvalidRequest)
			}
		}
		grant, grantErr := s.grants.CreateOnBehalfOfGrant(ctx, GrantRequest{InvocationID: id, UserID: post.AuthorID, TenantID: post.TenantID, AgentVersion: admission.Persona.Version, TargetAgentID: targetAgentID, PersonaID: personaID, PersonaVersion: admission.Persona.Version, InstallationID: admission.Installation.ID, ConversationID: post.ConversationID, ThreadID: post.ThreadID, InvokingPostID: post.PostID, Purpose: "persona-mention", Skills: skills, Mode: OnBehalfOf, ExpiresAt: s.clock().UTC().Add(7 * 24 * time.Hour)})
		if grantErr != nil {
			return Invocation{}, grantErr
		}
		if !SkillScopesSubset(grant.Skills, skills) || grant.TargetAgentID != targetAgentID || (grant.TaskID != "" && grant.TaskID != id) {
			return Invocation{}, fmt.Errorf("%w: grant widened effective skills", ErrConflict)
		}
		if len(grant.Skills) == 0 {
			return Invocation{}, fmt.Errorf("%w: grant contains no effective skills", ErrConflict)
		}
		claimed, err = s.repo.SetGrant(ctx, claimed.TenantID, id, grant)
		if err != nil {
			return Invocation{}, err
		}
	}
	bound := BoundContext{Goal: post.Body}
	if s.threads != nil {
		bound, err = s.BuildContext(ctx, claimed, post.Body)
		if err != nil {
			return Invocation{}, err
		}
	}
	request := invocationRunRequest(claimed)
	request.Context = bound
	if err := s.runs.Start(ctx, request); err != nil {
		return Invocation{}, err
	}
	if _, err := s.repo.MarkStarted(ctx, claimed.TenantID, id); err != nil {
		return Invocation{}, err
	}
	claimed.State = InvocationStarted
	return claimed, nil
}

func invocationRunRequest(invocation Invocation) RunRequest {
	return RunRequest{InvocationID: invocation.ID, TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID, InvokingPostID: invocation.PostID, InvokerID: invocation.InvokerID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, Mode: invocation.Mode, Grant: invocation.Grant, Skills: invocation.Skills.Clone(), Actor: invocation.Actor}
}

func validTargetAgentID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func (s *Service) lockInvocationStart(id string) func() {
	s.startMu.Lock()
	if s.starts == nil {
		s.starts = make(map[string]*invocationStartLock)
	}
	entry := s.starts[id]
	if entry == nil {
		entry = &invocationStartLock{}
		s.starts[id] = entry
	}
	entry.refs++
	s.startMu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.startMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(s.starts, id)
		}
		s.startMu.Unlock()
	}
}

func (s *Service) sendRefusal(ctx context.Context, post PostCommit, personaID string, reason DenialReason) error {
	if s.ephemeral == nil {
		return nil
	}
	return s.ephemeral.Refuse(ctx, EphemeralRefusal{TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: post.ThreadID, PostID: post.PostID, InvokerID: post.AuthorID, PersonaID: personaID, Reason: reason})
}

func (s *Service) refuseAll(ctx context.Context, post PostCommit, reason DenialReason) error {
	for _, mention := range post.Mentions {
		if mention.Kind == PersonaMention && mention.Canonical && mention.PersonaID != "" {
			if err := s.sendRefusal(ctx, post, mention.PersonaID, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePost(post PostCommit) error {
	for name, value := range map[string]string{"tenant_id": post.TenantID, "conversation_id": post.ConversationID, "post_id": post.PostID, "author_id": post.AuthorID} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidRequest, name)
		}
	}
	if strings.TrimSpace(post.ThreadID) == "" {
		return &EligibilityError{Reason: DenialInvalidPost, Detail: "thread is required"}
	}
	if !post.New || post.Edited || post.Quoted || post.Forwarded {
		return &EligibilityError{Reason: DenialInvalidPost, Detail: "only a newly authored, unquoted, unforwarded post can invoke a persona"}
	}
	return nil
}

func invocationID(post PostCommit, personaID string) string {
	sum := sha256.Sum256([]byte("agentinvoke/persona/v1\x00" + post.TenantID + "\x00" + post.ConversationID + "\x00" + post.PostID + "\x00" + personaID))
	return "pinv_" + hex.EncodeToString(sum[:])
}

func sortedUnique(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

// MemoryRepository is a deterministic, concurrency-safe reference store. Its
// key is deliberately (post, persona), never merely the thread.
type MemoryRepository struct {
	mu    sync.Mutex
	items map[string]Invocation
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Invocation)}
}

func invocationKey(tenantID, postID, personaID string) string {
	return tenantID + "\x00" + postID + "\x00" + personaID
}

func (r *MemoryRepository) Claim(_ context.Context, candidate Invocation) (Invocation, bool, error) {
	if r == nil || strings.TrimSpace(candidate.PostID) == "" || strings.TrimSpace(candidate.PersonaID) == "" {
		return Invocation{}, false, fmt.Errorf("%w: invocation key is required", ErrInvalidRequest)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := invocationKey(candidate.TenantID, candidate.PostID, candidate.PersonaID)
	if prior, ok := r.items[key]; ok {
		if prior.ID != candidate.ID || prior.TenantID != candidate.TenantID || prior.ConversationID != candidate.ConversationID || prior.ThreadID != candidate.ThreadID || prior.InvokerID != candidate.InvokerID || prior.PersonaVersion != candidate.PersonaVersion || prior.InstallationID != candidate.InstallationID || prior.Mode != candidate.Mode || !sameSkills(prior.Skills, candidate.Skills) || prior.Actor != candidate.Actor {
			return Invocation{}, false, ErrConflict
		}
		return cloneInvocation(prior), false, nil
	}
	r.items[key] = cloneInvocation(candidate)
	return cloneInvocation(candidate), true, nil
}

func (r *MemoryRepository) SetGrant(_ context.Context, tenantID, id string, grant DelegationGrant) (Invocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, item := range r.items {
		if item.ID != id || item.TenantID != tenantID || grant.TenantID != tenantID {
			continue
		}
		if grant.UserID != item.InvokerID || grant.TenantID != item.TenantID || (grant.TaskID != "" && grant.TaskID != item.ID) || !SkillScopesSubset(grant.Skills, item.Skills) || len(grant.Skills) == 0 {
			return Invocation{}, ErrConflict
		}
		if item.Grant.ID != "" && !sameGrant(item.Grant, grant) {
			return Invocation{}, ErrConflict
		}
		item.Grant = cloneGrant(grant)
		item.Skills = IntersectSkillScopes(item.Skills, grant.Skills)
		r.items[key] = cloneInvocation(item)
		return cloneInvocation(item), nil
	}
	return Invocation{}, ErrInvalidRequest
}

func (r *MemoryRepository) MarkStarted(_ context.Context, tenantID, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, item := range r.items {
		if item.ID != id || item.TenantID != tenantID || item.Grant.ID == "" || item.Grant.ExpiresAt.IsZero() {
			continue
		}
		if item.State == InvocationStarted {
			return false, nil
		}
		item.State = InvocationStarted
		r.items[key] = cloneInvocation(item)
		return true, nil
	}
	return false, ErrInvalidRequest
}

// Get returns a detached invocation for adapters and audit projections. The
// lookup key remains the post/persona pair, making thread-wide memory
// impossible to address accidentally.
func (r *MemoryRepository) Get(tenantID, postID, personaID string) (Invocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[invocationKey(tenantID, postID, personaID)]
	if !ok {
		return Invocation{}, fmt.Errorf("%w: invocation not found", ErrInvalidRequest)
	}
	return cloneInvocation(item), nil
}

func cloneGrant(g DelegationGrant) DelegationGrant { g.Skills = g.Skills.Clone(); return g }

func sameGrant(a, b DelegationGrant) bool {
	if a.ID != b.ID || a.UserID != b.UserID || a.TenantID != b.TenantID || !a.ExpiresAt.Equal(b.ExpiresAt) {
		return false
	}
	if len(a.Skills) != len(b.Skills) {
		return false
	}
	for skill, scopes := range a.Skills {
		if strings.Join(sortedUnique(scopes), "\x00") != strings.Join(sortedUnique(b.Skills[skill]), "\x00") {
			return false
		}
	}
	return true
}

func sameSkills(a, b SkillScopes) bool {
	return SkillScopesSubset(a, b) && SkillScopesSubset(b, a)
}

func cloneInvocation(i Invocation) Invocation {
	i.Skills = i.Skills.Clone()
	i.Grant = cloneGrant(i.Grant)
	return i
}
