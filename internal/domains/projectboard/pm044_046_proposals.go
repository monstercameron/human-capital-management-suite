package projectboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

const (
	MaxProposalCitations = 32
	MaxProposalUnknowns  = 16
	MaxProposalText      = 256

	AITraceRetentionClass      = "AI_PROPOSAL_TRACE_RESTRICTED"
	ProjectAuditRetentionClass = "PROJECT_AUDIT"
)

var (
	ErrAIProviderUnavailable = errors.New("projectboard: ai provider unavailable")
	ErrAIQuotaExceeded       = errors.New("projectboard: ai quota exceeded")
	ErrAIOutputMalformed     = errors.New("projectboard: ai output malformed")
	ErrAIPolicyRejected      = errors.New("projectboard: ai proposal rejected")
	ErrProposalStale         = errors.New("projectboard: ai proposal is stale")
	ErrProposalSourceRevoked = errors.New("projectboard: cited proposal source is unavailable")
	ErrProposalUnauthorized  = errors.New("projectboard: proposal publication requires a human actor")
	ErrProposalInvalid       = errors.New("projectboard: invalid proposal")
)

// ProposalFailure is deliberately non-material: callers can show its Kind to
// a user while retaining the current board and manual draft unchanged.
type ProposalFailure struct {
	Kind   error
	Detail string
}

func (e ProposalFailure) Error() string {
	if strings.TrimSpace(e.Detail) == "" {
		return e.Kind.Error()
	}
	return e.Kind.Error() + ": " + e.Detail
}

func (e ProposalFailure) Unwrap() error { return e.Kind }

type ProposalSource struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
}

type ProposalValidation struct {
	SchemaVersion string   `json:"schema_version"`
	Valid         bool     `json:"valid"`
	Digest        string   `json:"digest"`
	ErrorCodes    []string `json:"error_codes,omitempty"`
}

type HumanDecision struct {
	ActorID string    `json:"actor_id"`
	Action  string    `json:"action"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// ProposalProvenance intentionally contains identifiers and digests only. It
// has no prompt, excerpt, trace, title, or other source-text field.
type ProposalProvenance struct {
	ProposalID            string             `json:"proposal_id"`
	TenantID              string             `json:"tenant_id"`
	ProjectID             string             `json:"project_id"`
	ModelProfile          string             `json:"model_profile"`
	PromptProfile         string             `json:"prompt_profile"`
	OutputSchemaVersion   string             `json:"output_schema_version"`
	CitedSources          []ProposalSource   `json:"cited_sources"`
	Validation            ProposalValidation `json:"validation"`
	Decision              HumanDecision      `json:"human_decision"`
	PublishedDigest       string             `json:"published_digest"`
	PublishedBy           string             `json:"published_by"`
	TraceRetentionClass   string             `json:"trace_retention_class"`
	ProjectRetentionClass string             `json:"project_retention_class"`
}

func (p ProposalProvenance) Validate() error {
	if strings.TrimSpace(p.ProposalID) == "" || strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.ProjectID) == "" {
		return fmt.Errorf("%w: proposal, tenant, and project IDs are required", ErrProposalInvalid)
	}
	if strings.TrimSpace(p.ModelProfile) == "" || strings.TrimSpace(p.PromptProfile) == "" || strings.TrimSpace(p.OutputSchemaVersion) == "" {
		return fmt.Errorf("%w: model, prompt, and output schema profiles are required", ErrProposalInvalid)
	}
	if len(p.CitedSources) > MaxProposalCitations {
		return fmt.Errorf("%w: too many cited sources", ErrProposalInvalid)
	}
	seen := make(map[string]struct{}, len(p.CitedSources))
	for _, source := range p.CitedSources {
		if strings.TrimSpace(source.ID) == "" || source.Revision == 0 {
			return fmt.Errorf("%w: cited sources require an ID and revision", ErrProposalInvalid)
		}
		if _, ok := seen[source.ID]; ok {
			return fmt.Errorf("%w: duplicate cited source %q", ErrProposalInvalid, source.ID)
		}
		seen[source.ID] = struct{}{}
	}
	if p.Validation.SchemaVersion == "" || !p.Validation.Valid || p.Validation.Digest == "" {
		return fmt.Errorf("%w: a valid output validation record is required", ErrProposalInvalid)
	}
	if p.Decision.ActorID == "" || p.Decision.Action != "PUBLISHED" || p.Decision.At.IsZero() {
		return fmt.Errorf("%w: a human publication decision is required", ErrProposalInvalid)
	}
	if p.PublishedDigest == "" || p.PublishedBy == "" {
		return fmt.Errorf("%w: published digest and actor are required", ErrProposalInvalid)
	}
	if p.TraceRetentionClass != AITraceRetentionClass || p.ProjectRetentionClass != ProjectAuditRetentionClass {
		return fmt.Errorf("%w: proposal records require separate retention classes", ErrProposalInvalid)
	}
	return nil
}

// BoardConfiguration is the shared material form used by manual editing and
// proposal publication. A proposal never has a write path around this value.
type BoardConfiguration struct {
	Revision uint64                 `json:"revision"`
	Workflow projectworkflow.Config `json:"workflow,omitempty"`
	Views    []BoardView            `json:"views"`
}

type SyntheticExample struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	StatusID  string `json:"status_id"`
	Synthetic bool   `json:"synthetic"`
}

type BoardProposal struct {
	ID                  string                 `json:"id"`
	TenantID            string                 `json:"tenant_id"`
	ProjectID           string                 `json:"project_id"`
	BaseRevision        uint64                 `json:"base_revision"`
	PromptProfile       string                 `json:"prompt_profile"`
	ModelProfile        string                 `json:"model_profile"`
	OutputSchemaVersion string                 `json:"output_schema_version"`
	Workflow            projectworkflow.Config `json:"workflow,omitempty"`
	Views               []BoardView            `json:"views"`
	Examples            []SyntheticExample     `json:"examples,omitempty"`
	CitedSources        []ProposalSource       `json:"cited_sources"`
	Unknowns            []string               `json:"unknowns,omitempty"`
}

type ProposalDraft struct {
	Proposal   BoardProposal      `json:"proposal"`
	Validation ProposalValidation `json:"validation"`
	Digest     string             `json:"digest"`
	Edited     bool               `json:"edited"`
}

type ProposalRequest struct {
	TenantID            string
	ProjectID           string
	BaseRevision        uint64
	PromptProfile       string
	ModelProfile        string
	OutputSchemaVersion string
	CitedSources        []ProposalSource
}

type ProposalProvider func(context.Context, ProposalRequest) (BoardProposal, error)

type SourceAuthorizer interface {
	AuthorizeProposalSource(context.Context, string, string, ProposalSource) bool
}

type SourceAuthorizerFunc func(context.Context, string, string, ProposalSource) bool

func (f SourceAuthorizerFunc) AuthorizeProposalSource(ctx context.Context, tenant, project string, source ProposalSource) bool {
	return f(ctx, tenant, project, source)
}

type ProvenanceStore struct {
	mu      sync.RWMutex
	records map[string]ProposalProvenance
}

func NewProvenanceStore() *ProvenanceStore {
	return &ProvenanceStore{records: make(map[string]ProposalProvenance)}
}

func (s *ProvenanceStore) Append(record ProposalProvenance) error {
	if s == nil {
		return fmt.Errorf("%w: provenance store is nil", ErrProposalInvalid)
	}
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[record.ProposalID]; exists {
		return fmt.Errorf("%w: proposal provenance already exists", ErrProposalInvalid)
	}
	s.records[record.ProposalID] = cloneValue(record)
	return nil
}

func (s *ProvenanceStore) Get(proposalID string) (ProposalProvenance, bool) {
	if s == nil {
		return ProposalProvenance{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[proposalID]
	return cloneValue(record), ok
}

type ProposalService struct {
	mu         sync.RWMutex
	current    BoardConfiguration
	draft      *ProposalDraft
	provider   ProposalProvider
	authorizer SourceAuthorizer
	audit      *ProvenanceStore
	maxRuns    int
	runs       int
}

func NewProposalService(current BoardConfiguration, provider ProposalProvider, authorizer SourceAuthorizer, audit *ProvenanceStore, maxRuns int) (*ProposalService, error) {
	if current.Revision == 0 {
		return nil, fmt.Errorf("%w: board revision is required", ErrProposalInvalid)
	}
	if err := validateConfiguration(current); err != nil {
		return nil, err
	}
	if audit == nil {
		audit = NewProvenanceStore()
	}
	if maxRuns < 0 {
		return nil, fmt.Errorf("%w: max AI runs cannot be negative", ErrProposalInvalid)
	}
	return &ProposalService{current: cloneValue(current), provider: provider, authorizer: authorizer, audit: audit, maxRuns: maxRuns}, nil
}

func (s *ProposalService) Current() BoardConfiguration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneValue(s.current)
}

func (s *ProposalService) Draft() (ProposalDraft, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.draft == nil {
		return ProposalDraft{}, false
	}
	return cloneValue(*s.draft), true
}

func (s *ProposalService) Refine(ctx context.Context, request ProposalRequest) (ProposalDraft, error) {
	if s == nil || s.provider == nil {
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIProviderUnavailable, Detail: "no proposal provider is configured"}
	}
	if err := validateRequest(request); err != nil {
		return ProposalDraft{}, err
	}
	s.mu.Lock()
	if request.BaseRevision != s.current.Revision {
		s.mu.Unlock()
		return ProposalDraft{}, ErrProposalStale
	}
	if s.maxRuns > 0 && s.runs >= s.maxRuns {
		s.mu.Unlock()
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIQuotaExceeded, Detail: "tenant AI run limit reached"}
	}
	s.runs++
	currentRevision := s.current.Revision
	s.mu.Unlock()
	for _, source := range request.CitedSources {
		if s.authorizer == nil || !s.authorizer.AuthorizeProposalSource(ctx, request.TenantID, request.ProjectID, source) {
			return ProposalDraft{}, ErrProposalSourceRevoked
		}
	}
	proposal, err := s.provider(ctx, request)
	if err != nil {
		if errors.Is(err, ErrAIProviderUnavailable) || errors.Is(err, ErrAIQuotaExceeded) || errors.Is(err, ErrAIOutputMalformed) || errors.Is(err, ErrAIPolicyRejected) {
			return ProposalDraft{}, err
		}
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIProviderUnavailable, Detail: err.Error()}
	}
	if proposal.BaseRevision != currentRevision || proposal.TenantID != request.TenantID || proposal.ProjectID != request.ProjectID {
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIOutputMalformed, Detail: "provider output does not match the requested board"}
	}
	if proposal.PromptProfile != request.PromptProfile || proposal.ModelProfile != request.ModelProfile || proposal.OutputSchemaVersion != request.OutputSchemaVersion {
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIOutputMalformed, Detail: "provider output profile mismatch"}
	}
	if !sameProposalSources(proposal.CitedSources, request.CitedSources) {
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIOutputMalformed, Detail: "provider output cited sources do not match the authorized request"}
	}
	if err := validateProposal(proposal); err != nil {
		return ProposalDraft{}, ProposalFailure{Kind: ErrAIOutputMalformed, Detail: err.Error()}
	}
	validation := proposalValidation(proposal)
	draft := ProposalDraft{Proposal: cloneValue(proposal), Validation: validation, Digest: proposalDigest(proposal)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current.Revision != currentRevision {
		return ProposalDraft{}, ErrProposalStale
	}
	s.draft = &draft
	return cloneValue(draft), nil
}

func (s *ProposalService) EditDraft(proposalID string, edit func(*BoardProposal) error) error {
	if s == nil || edit == nil {
		return fmt.Errorf("%w: edit is required", ErrProposalInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft == nil || s.draft.Proposal.ID != proposalID {
		return ErrProposalStale
	}
	proposal := cloneValue(s.draft.Proposal)
	original := proposal
	if err := edit(&proposal); err != nil {
		return err
	}
	if proposal.ID != original.ID || proposal.TenantID != original.TenantID || proposal.ProjectID != original.ProjectID || proposal.BaseRevision != original.BaseRevision {
		return ProposalFailure{Kind: ErrAIPolicyRejected, Detail: "draft identity and base revision are not editable"}
	}
	if err := validateProposal(proposal); err != nil {
		return ProposalFailure{Kind: ErrAIPolicyRejected, Detail: err.Error()}
	}
	validation := proposalValidation(proposal)
	s.draft = &ProposalDraft{Proposal: proposal, Validation: validation, Digest: proposalDigest(proposal), Edited: true}
	return nil
}

func (s *ProposalService) ManualEdit(expectedRevision uint64, next BoardConfiguration) error {
	if s == nil {
		return ErrProposalInvalid
	}
	if next.Revision != expectedRevision+1 {
		return ErrProposalStale
	}
	if err := validateConfiguration(next); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.current.Revision {
		return ErrProposalStale
	}
	s.current = cloneValue(next)
	s.draft = nil
	return nil
}

func (s *ProposalService) Publish(actorID, reason, reviewedDigest string, at time.Time) (BoardConfiguration, ProposalProvenance, error) {
	if s == nil {
		return BoardConfiguration{}, ProposalProvenance{}, ErrProposalInvalid
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardConfiguration{}, ProposalProvenance{}, ErrProposalUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft == nil || s.draft.Proposal.BaseRevision != s.current.Revision {
		return BoardConfiguration{}, ProposalProvenance{}, ErrProposalStale
	}
	if reviewedDigest == "" || reviewedDigest != s.draft.Digest {
		return BoardConfiguration{}, ProposalProvenance{}, ProposalFailure{Kind: ErrAIPolicyRejected, Detail: "human review digest does not match the draft"}
	}
	for _, source := range s.draft.Proposal.CitedSources {
		if s.authorizer == nil || !s.authorizer.AuthorizeProposalSource(context.Background(), s.draft.Proposal.TenantID, s.draft.Proposal.ProjectID, source) {
			return BoardConfiguration{}, ProposalProvenance{}, ErrProposalSourceRevoked
		}
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	next := BoardConfiguration{Revision: s.current.Revision + 1, Workflow: s.draft.Proposal.Workflow, Views: s.draft.Proposal.Views}
	if err := validateConfiguration(next); err != nil {
		return BoardConfiguration{}, ProposalProvenance{}, err
	}
	publishedDigest := configurationDigest(next)
	record := ProposalProvenance{ProposalID: s.draft.Proposal.ID, TenantID: s.draft.Proposal.TenantID, ProjectID: s.draft.Proposal.ProjectID, ModelProfile: s.draft.Proposal.ModelProfile, PromptProfile: s.draft.Proposal.PromptProfile, OutputSchemaVersion: s.draft.Proposal.OutputSchemaVersion, CitedSources: s.draft.Proposal.CitedSources, Validation: s.draft.Validation, Decision: HumanDecision{ActorID: actorID, Action: "PUBLISHED", Reason: reason, At: at.UTC()}, PublishedDigest: publishedDigest, PublishedBy: actorID, TraceRetentionClass: AITraceRetentionClass, ProjectRetentionClass: ProjectAuditRetentionClass}
	if err := s.audit.Append(record); err != nil {
		return BoardConfiguration{}, ProposalProvenance{}, err
	}
	s.current, s.draft = cloneValue(next), nil
	return cloneValue(next), cloneValue(record), nil
}

func validateRequest(request ProposalRequest) error {
	if request.TenantID == "" || request.ProjectID == "" || request.BaseRevision == 0 || request.PromptProfile == "" || request.ModelProfile == "" || request.OutputSchemaVersion == "" {
		return fmt.Errorf("%w: incomplete proposal request", ErrProposalInvalid)
	}
	if len(request.CitedSources) > MaxProposalCitations {
		return fmt.Errorf("%w: too many cited sources", ErrProposalInvalid)
	}
	seen := map[string]bool{}
	for _, source := range request.CitedSources {
		if strings.TrimSpace(source.ID) == "" || source.Revision == 0 || seen[source.ID] {
			return fmt.Errorf("%w: cited source IDs must be unique and revisioned", ErrProposalInvalid)
		}
		seen[source.ID] = true
	}
	return nil
}

func validateProposal(proposal BoardProposal) error {
	if proposal.ID == "" || proposal.TenantID == "" || proposal.ProjectID == "" || proposal.BaseRevision == 0 || proposal.OutputSchemaVersion == "" {
		return fmt.Errorf("%w: proposal identity and schema are required", ErrProposalInvalid)
	}
	if len(proposal.Views) == 0 {
		return fmt.Errorf("%w: at least one board view is required", ErrProposalInvalid)
	}
	if err := validateSources(proposal.CitedSources); err != nil {
		return err
	}
	for _, view := range proposal.Views {
		if err := Validate(view); err != nil {
			return err
		}
	}
	if hasWorkflow(proposal.Workflow) {
		if errs := projectworkflow.Validate(proposal.Workflow); len(errs) != 0 {
			return errs
		}
	}
	if len(proposal.Examples) > 25 {
		return fmt.Errorf("%w: too many examples", ErrProposalInvalid)
	}
	for _, example := range proposal.Examples {
		if example.ID == "" || example.Title == "" || example.StatusID == "" || !example.Synthetic || len(example.Title) > MaxProposalText {
			return fmt.Errorf("%w: examples must be bounded and visibly synthetic", ErrProposalInvalid)
		}
	}
	if len(proposal.Unknowns) > MaxProposalUnknowns {
		return fmt.Errorf("%w: too many unknowns", ErrProposalInvalid)
	}
	for _, unknown := range proposal.Unknowns {
		if strings.TrimSpace(unknown) == "" || len(unknown) > MaxProposalText {
			return fmt.Errorf("%w: unknowns must be bounded", ErrProposalInvalid)
		}
	}
	return nil
}

func validateSources(sources []ProposalSource) error {
	if len(sources) > MaxProposalCitations {
		return fmt.Errorf("%w: too many cited sources", ErrProposalInvalid)
	}
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.ID) == "" || source.Revision == 0 || seen[source.ID] {
			return fmt.Errorf("%w: cited source IDs must be unique and revisioned", ErrProposalInvalid)
		}
		seen[source.ID] = true
	}
	return nil
}

func sameProposalSources(left, right []ProposalSource) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func proposalValidation(proposal BoardProposal) ProposalValidation {
	return ProposalValidation{SchemaVersion: proposal.OutputSchemaVersion, Valid: true, Digest: proposalDigest(proposal)}
}

func proposalDigest(proposal BoardProposal) string { return digest(proposal) }

func configurationDigest(config BoardConfiguration) string {
	return digest(struct {
		Workflow projectworkflow.Config
		Views    []BoardView
	}{config.Workflow, config.Views})
}

func digest(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validateConfiguration(config BoardConfiguration) error {
	if config.Revision == 0 || len(config.Views) == 0 {
		return fmt.Errorf("%w: configuration requires a revision and board view", ErrProposalInvalid)
	}
	for _, view := range config.Views {
		if err := Validate(view); err != nil {
			return err
		}
	}
	if hasWorkflow(config.Workflow) {
		if errs := projectworkflow.Validate(config.Workflow); len(errs) != 0 {
			return errs
		}
	}
	return nil
}

func hasWorkflow(config projectworkflow.Config) bool {
	return len(config.TaskTypes) != 0 || len(config.Statuses) != 0 || len(config.Transitions) != 0 || len(config.Fields) != 0 || len(config.Columns) != 0
}

func cloneValue[T any](value T) T {
	b, _ := json.Marshal(value)
	var clone T
	_ = json.Unmarshal(b, &clone)
	return clone
}
