package projectworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// AIProvider is the residency-bound provider location selected by a tenant.
type AIProvider struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// AIPolicy is the complete tenant boundary for a project-management AI run.
// The policy is deliberately value-shaped so an admission decision can be
// audited without consulting mutable process state.
type AIPolicy struct {
	TenantID               string        `json:"tenant_id"`
	ApprovedProviders      []AIProvider  `json:"approved_providers"`
	PromptRetention        time.Duration `json:"prompt_retention"`
	TraceRetention         time.Duration `json:"trace_retention"`
	AllowedClassifications []string      `json:"allowed_classifications"`
	MaxPromptTokens        int64         `json:"max_prompt_tokens"`
	MaxOutputTokens        int64         `json:"max_output_tokens"`
	MaxConcurrentRuns      int64         `json:"max_concurrent_runs"`
	MaxSpendCents          int64         `json:"max_spend_cents"`
	Currency               string        `json:"currency"`
}

type AIRequest struct {
	TenantID            string
	Provider            string
	Region              string
	Classification      string
	PromptTokens        int64
	OutputTokens        int64
	EstimatedSpendCents int64
	CurrentSpendCents   int64
	InFlightRuns        int64
}

type AIAdmission struct {
	Provider         AIProvider
	PromptRetention  time.Duration
	TraceRetention   time.Duration
	Classification   string
	PromptTokenLimit int64
	OutputTokenLimit int64
	ConcurrencyLimit int64
	SpendLimitCents  int64
	Currency         string
}

var (
	ErrInvalidAIPolicy = errors.New("projectworkflow: invalid AI policy")
	ErrAIPolicyDenied  = errors.New("projectworkflow: AI policy denied")
)

type AIPolicyDenialCode string

const (
	AIDenialTenantMismatch AIPolicyDenialCode = "TENANT_MISMATCH"
	AIDenialProvider       AIPolicyDenialCode = "PROVIDER_NOT_APPROVED"
	AIDenialResidency      AIPolicyDenialCode = "REGION_NOT_APPROVED"
	AIDenialClassification AIPolicyDenialCode = "CLASSIFICATION_NOT_APPROVED"
	AIDenialPromptTokens   AIPolicyDenialCode = "PROMPT_TOKEN_CEILING"
	AIDenialOutputTokens   AIPolicyDenialCode = "OUTPUT_TOKEN_CEILING"
	AIDenialConcurrency    AIPolicyDenialCode = "CONCURRENCY_CEILING"
	AIDenialSpend          AIPolicyDenialCode = "SPEND_CEILING"
	AIDenialInvalidRequest AIPolicyDenialCode = "INVALID_REQUEST"
)

type AIPolicyDenial struct {
	Code    AIPolicyDenialCode `json:"code"`
	Message string             `json:"message"`
}

func (e *AIPolicyDenial) Error() string        { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *AIPolicyDenial) Is(target error) bool { return target == ErrAIPolicyDenied }

func ValidateAIPolicy(policy AIPolicy) error {
	if strings.TrimSpace(policy.TenantID) == "" || len(policy.ApprovedProviders) == 0 ||
		policy.MaxPromptTokens <= 0 || policy.MaxOutputTokens <= 0 || policy.MaxConcurrentRuns <= 0 ||
		policy.MaxSpendCents < 0 || strings.TrimSpace(policy.Currency) == "" ||
		len(policy.AllowedClassifications) == 0 || policy.PromptRetention < 0 || policy.TraceRetention < 0 {
		return ErrInvalidAIPolicy
	}
	providers := map[string]bool{}
	for _, provider := range policy.ApprovedProviders {
		name, region := strings.TrimSpace(provider.Name), strings.TrimSpace(provider.Region)
		if name == "" || region == "" || providers[name+"\x00"+region] {
			return ErrInvalidAIPolicy
		}
		providers[name+"\x00"+region] = true
	}
	classifications := map[string]bool{}
	for _, classification := range policy.AllowedClassifications {
		classification = strings.TrimSpace(classification)
		if classification == "" || classifications[classification] {
			return ErrInvalidAIPolicy
		}
		classifications[classification] = true
	}
	return nil
}

func AdmitAIRequest(policy AIPolicy, request AIRequest) (AIAdmission, error) {
	if err := ValidateAIPolicy(policy); err != nil {
		return AIAdmission{}, err
	}
	deny := func(code AIPolicyDenialCode, message string) (AIAdmission, error) {
		return AIAdmission{}, &AIPolicyDenial{Code: code, Message: message}
	}
	if request.TenantID != policy.TenantID {
		return deny(AIDenialTenantMismatch, "request tenant does not match the policy tenant")
	}
	if request.PromptTokens < 0 || request.OutputTokens < 0 || request.EstimatedSpendCents < 0 || request.CurrentSpendCents < 0 || request.InFlightRuns < 0 {
		return deny(AIDenialInvalidRequest, "AI usage values must be non-negative")
	}
	var selected AIProvider
	for _, provider := range policy.ApprovedProviders {
		if provider.Name == request.Provider && provider.Region == request.Region {
			selected = provider
			break
		}
	}
	if selected.Name == "" {
		for _, provider := range policy.ApprovedProviders {
			if provider.Name == request.Provider {
				return deny(AIDenialResidency, "provider is approved only in another region")
			}
		}
		return deny(AIDenialProvider, "provider is not approved for this tenant")
	}
	if !containsString(policy.AllowedClassifications, request.Classification) {
		return deny(AIDenialClassification, "prompt classification is not approved for AI retrieval")
	}
	if request.PromptTokens > policy.MaxPromptTokens {
		return deny(AIDenialPromptTokens, "prompt exceeds the tenant token ceiling")
	}
	if request.OutputTokens > policy.MaxOutputTokens {
		return deny(AIDenialOutputTokens, "output exceeds the tenant token ceiling")
	}
	if request.InFlightRuns >= policy.MaxConcurrentRuns {
		return deny(AIDenialConcurrency, "tenant AI concurrency ceiling is reached")
	}
	if request.CurrentSpendCents > policy.MaxSpendCents || request.EstimatedSpendCents > policy.MaxSpendCents-request.CurrentSpendCents {
		return deny(AIDenialSpend, "request would exceed the tenant spend ceiling")
	}
	return AIAdmission{Provider: selected, PromptRetention: policy.PromptRetention, TraceRetention: policy.TraceRetention, Classification: request.Classification, PromptTokenLimit: policy.MaxPromptTokens, OutputTokenLimit: policy.MaxOutputTokens, ConcurrencyLimit: policy.MaxConcurrentRuns, SpendLimitCents: policy.MaxSpendCents, Currency: policy.Currency}, nil
}

type SourceCitation struct {
	ID             string `json:"id"`
	Revision       uint64 `json:"revision"`
	Classification string `json:"classification,omitempty"`
}

type SourceGrant struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	CanRead  bool   `json:"can_read"`
}

type ProposalShare struct {
	PrincipalID string `json:"principal_id"`
	Role        string `json:"role"`
	External    bool   `json:"external"`
}

type SampleCard struct {
	ID              string                     `json:"id"`
	Title           string                     `json:"title"`
	TypeID          string                     `json:"type_id"`
	InitialStatusID string                     `json:"initial_status_id"`
	StatusID        string                     `json:"status_id"`
	Fields          map[string]json.RawMessage `json:"fields,omitempty"`
	Synthetic       bool                       `json:"synthetic"`
}

// BoardProposal is an untrusted, typed model output. It contains source
// references only; source excerpts and prompt text are intentionally absent.
type BoardProposal struct {
	ID                string           `json:"id"`
	TenantID          string           `json:"tenant_id"`
	ProjectID         string           `json:"project_id"`
	RequesterID       string           `json:"requester_id"`
	BaseConfigVersion uint64           `json:"base_config_version"`
	Config            Config           `json:"config"`
	Rationale         string           `json:"rationale"`
	Methodology       string           `json:"methodology,omitempty"`
	Assumptions       []string         `json:"assumptions,omitempty"`
	CitedSources      []SourceCitation `json:"cited_sources,omitempty"`
	SampleCards       []SampleCard     `json:"sample_cards,omitempty"`
	Sharing           []ProposalShare  `json:"sharing,omitempty"`
}

var (
	ErrInvalidProposal          = errors.New("projectworkflow: invalid AI board proposal")
	ErrProposalRevisionConflict = errors.New("projectworkflow: AI proposal configuration revision conflict")
)

func ValidateProposal(proposal BoardProposal) ValidationErrors {
	var errs ValidationErrors
	add := func(code, path, message string) {
		errs = append(errs, ValidationError{Code: code, Path: path, Message: message})
	}
	if strings.TrimSpace(proposal.ID) == "" {
		add("MISSING_PROPOSAL_ID", "id", "proposal ID is required")
	}
	if strings.TrimSpace(proposal.TenantID) == "" {
		add("MISSING_TENANT_ID", "tenant_id", "tenant ID is required")
	}
	if strings.TrimSpace(proposal.ProjectID) == "" {
		add("MISSING_PROJECT_ID", "project_id", "project ID is required")
	}
	if strings.TrimSpace(proposal.RequesterID) == "" {
		add("MISSING_REQUESTER_ID", "requester_id", "requester ID is required")
	}
	if proposal.BaseConfigVersion == 0 {
		add("MISSING_CONFIG_VERSION", "base_config_version", "proposal must pin a configuration revision")
	}
	if strings.TrimSpace(proposal.Rationale) == "" {
		add("MISSING_RATIONALE", "rationale", "proposal rationale is required")
	}
	errs = append(errs, Validate(proposal.Config)...)
	if methodology := strings.ToUpper(strings.TrimSpace(proposal.Methodology)); methodology != "" && methodology != "CONTINUOUS_FLOW" {
		add("UNSUPPORTED_METHODOLOGY", "methodology", "methodology claim is not enabled by this workflow compiler")
	}
	seenSources := map[string]bool{}
	for i, source := range proposal.CitedSources {
		path := fmt.Sprintf("cited_sources[%d]", i)
		if strings.TrimSpace(source.ID) == "" || source.Revision == 0 {
			add("INVALID_SOURCE_CITATION", path, "source ID and revision are required")
		}
		key := source.ID + "\x00" + fmt.Sprint(source.Revision)
		if seenSources[key] {
			add("DUPLICATE_SOURCE_CITATION", path, "source ID and revision are already cited")
		}
		seenSources[key] = true
	}
	seenCards := map[string]bool{}
	statuses := statusIndex(proposal.Config)
	for i, card := range proposal.SampleCards {
		path := fmt.Sprintf("sample_cards[%d]", i)
		if strings.TrimSpace(card.ID) == "" || seenCards[card.ID] {
			add("DUPLICATE_SAMPLE_ID", path+".id", "sample card ID must be present and unique")
		}
		seenCards[card.ID] = true
		if !card.Synthetic {
			add("SAMPLE_NOT_SYNTHETIC", path+".synthetic", "preview sample cards must be explicitly synthetic")
		}
		if _, ok := statuses[card.StatusID]; !ok || statuses[card.StatusID].Retired {
			add("INVALID_SAMPLE_STATUS", path+".status_id", "sample card status must be active")
		}
		if err := ValidateTaskCreation(proposal.Config, proposal.BaseConfigVersion, proposal.BaseConfigVersion, card.TypeID, card.InitialStatusID, card.Fields); err != nil {
			if creation, ok := err.(TaskCreationErrors); ok {
				for _, item := range creation {
					item.Path = path + "." + item.Path
					errs = append(errs, item)
				}
			} else {
				add("INVALID_SAMPLE_CARD", path, err.Error())
			}
		}
	}
	seenShares := map[string]bool{}
	for i, share := range proposal.Sharing {
		path := fmt.Sprintf("sharing[%d]", i)
		if strings.TrimSpace(share.PrincipalID) == "" || seenShares[share.PrincipalID] {
			add("DUPLICATE_SHARE", path, "shared principal must be present and unique")
		}
		seenShares[share.PrincipalID] = true
		if share.External {
			add("UNSAFE_EXTERNAL_SHARE", path+".external", "AI proposals cannot grant external project access")
		}
		switch share.Role {
		case "VIEWER", "CONTRIBUTOR", "MANAGER":
		default:
			add("UNSAFE_SHARE_ROLE", path+".role", "proposal sharing may not grant this role")
		}
	}
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Path == errs[j].Path {
			return errs[i].Code < errs[j].Code
		}
		return errs[i].Path < errs[j].Path
	})
	return errs
}

type ProposalConfigDiff struct {
	AddedStatusIDs   []string `json:"added_status_ids,omitempty"`
	RemovedStatusIDs []string `json:"removed_status_ids,omitempty"`
	ChangedStatusIDs []string `json:"changed_status_ids,omitempty"`
	AddedFieldIDs    []string `json:"added_field_ids,omitempty"`
	RemovedFieldIDs  []string `json:"removed_field_ids,omitempty"`
	ChangedFieldIDs  []string `json:"changed_field_ids,omitempty"`
	AddedColumnIDs   []string `json:"added_column_ids,omitempty"`
	RemovedColumnIDs []string `json:"removed_column_ids,omitempty"`
	ChangedColumnIDs []string `json:"changed_column_ids,omitempty"`
}

type PreviewAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type ProposalPreview struct {
	ProposalID       string             `json:"proposal_id"`
	Rationale        string             `json:"rationale"`
	CitedSources     []SourceCitation   `json:"cited_sources"`
	SyntheticSamples []SampleCard       `json:"synthetic_samples"`
	Assumptions      []string           `json:"assumptions,omitempty"`
	ConfigDiff       ProposalConfigDiff `json:"config_diff"`
	Migration        MigrationPreview   `json:"migration"`
	Actions          []PreviewAction    `json:"actions"`
	Accessible       bool               `json:"accessible"`
	SampleLabel      string             `json:"sample_label"`
	ReviewedDigest   string             `json:"reviewed_digest"`
	Safe             bool               `json:"safe"`
}

type ProposalPreviewRequest struct {
	Proposal       BoardProposal
	CurrentConfig  Config
	CurrentVersion uint64
	Tasks          []TaskSnapshot
	Mappings       MigrationMappings
}

func PreviewProposal(request ProposalPreviewRequest) (ProposalPreview, error) {
	if errs := ValidateProposal(request.Proposal); len(errs) > 0 {
		return ProposalPreview{}, fmt.Errorf("%w: %v", ErrInvalidProposal, errs)
	}
	if request.CurrentVersion == 0 || request.Proposal.BaseConfigVersion != request.CurrentVersion {
		return ProposalPreview{}, ErrProposalRevisionConflict
	}
	if errs := Validate(request.CurrentConfig); len(errs) > 0 {
		return ProposalPreview{}, fmt.Errorf("%w: current config: %v", ErrInvalidProposal, errs)
	}
	migration, migrationErr := PreviewMigration(MigrationRequest{Current: request.CurrentConfig, Pending: request.Proposal.Config, Tasks: request.Tasks, StatusMappings: request.Mappings.Statuses, FieldMappings: request.Mappings.Fields})
	diff := diffConfigs(request.CurrentConfig, request.Proposal.Config)
	preview := ProposalPreview{ProposalID: request.Proposal.ID, Rationale: request.Proposal.Rationale, CitedSources: cloneCitations(request.Proposal.CitedSources), SyntheticSamples: cloneSamples(request.Proposal.SampleCards), Assumptions: append([]string(nil), request.Proposal.Assumptions...), ConfigDiff: diff, Migration: migration, Actions: []PreviewAction{{ID: "edit", Label: "Edit proposal"}, {ID: "accept", Label: "Accept proposal"}}, Accessible: true, SampleLabel: "Synthetic sample card", Safe: migrationErr == nil && migration.Safe}
	if sealed, err := Publish(request.Proposal.Config, 1); err == nil {
		preview.ReviewedDigest = sealed.Digest()
	}
	if migrationErr != nil && !errors.Is(migrationErr, ErrUnsafeMigration) && !errors.Is(migrationErr, ErrSnapshotLimitExceeded) {
		return ProposalPreview{}, migrationErr
	}
	return preview, nil
}

type PublicationRevalidation struct {
	ProposalDigest string `json:"proposal_digest"`
	ConfigDigest   string `json:"config_digest"`
	SourceDigest   string `json:"source_digest"`
}
type ProposalPublicationRequest struct {
	Proposal               BoardProposal
	CurrentConfig          Config
	CurrentConfigVersion   uint64
	ExpectedConfigVersion  uint64
	ReviewedProposalDigest string
	ReviewedConfigDigest   string
	ReviewedSourceDigest   string
	RequesterID            string
	PublisherID            string
	RequesterGrants        []SourceGrant
	PublisherGrants        []SourceGrant
}

var (
	ErrSourceGrantStale = errors.New("projectworkflow: AI proposal source grant is stale or revoked")
	ErrPublicationStale = errors.New("projectworkflow: AI proposal publication is stale")
)

func RevalidateProposalPublication(request ProposalPublicationRequest) (PublicationRevalidation, error) {
	if request.ExpectedConfigVersion == 0 || request.CurrentConfigVersion != request.ExpectedConfigVersion || request.Proposal.BaseConfigVersion != request.ExpectedConfigVersion {
		return PublicationRevalidation{}, ErrPublicationStale
	}
	if request.RequesterID == "" || request.PublisherID == "" || request.RequesterID != request.Proposal.RequesterID {
		return PublicationRevalidation{}, ErrPublicationStale
	}
	if errs := ValidateProposal(request.Proposal); len(errs) > 0 {
		return PublicationRevalidation{}, fmt.Errorf("%w: %v", ErrInvalidProposal, errs)
	}
	proposalDigest := ProposalDigest(request.Proposal)
	configDigest, err := ConfigDigest(request.Proposal.Config)
	if err != nil {
		return PublicationRevalidation{}, err
	}
	if request.ReviewedProposalDigest == "" || request.ReviewedProposalDigest != proposalDigest || request.ReviewedConfigDigest == "" || request.ReviewedConfigDigest != configDigest {
		return PublicationRevalidation{}, ErrPublicationStale
	}
	sourceDigest := SourcesDigest(request.Proposal.CitedSources)
	if request.ReviewedSourceDigest == "" || request.ReviewedSourceDigest != sourceDigest {
		return PublicationRevalidation{}, ErrSourceGrantStale
	}
	if !grantsMatch(request.Proposal.CitedSources, request.RequesterGrants) || !grantsMatch(request.Proposal.CitedSources, request.PublisherGrants) {
		return PublicationRevalidation{}, ErrSourceGrantStale
	}
	return PublicationRevalidation{ProposalDigest: proposalDigest, ConfigDigest: configDigest, SourceDigest: sourceDigest}, nil
}

func ProposalDigest(proposal BoardProposal) string {
	proposal = cloneProposal(proposal)
	proposal.Rationale = strings.TrimSpace(proposal.Rationale)
	b, _ := json.Marshal(proposal)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ConfigDigest(config Config) (string, error) {
	sealed, err := Publish(config, 1)
	if err != nil {
		return "", err
	}
	return sealed.Digest(), nil
}
func SourcesDigest(sources []SourceCitation) string {
	ordered := cloneCitations(sources)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ID == ordered[j].ID {
			return ordered[i].Revision < ordered[j].Revision
		}
		return ordered[i].ID < ordered[j].ID
	})
	b, _ := json.Marshal(ordered)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func grantsMatch(sources []SourceCitation, grants []SourceGrant) bool {
	byID := map[string]SourceGrant{}
	for _, grant := range grants {
		byID[grant.ID] = grant
	}
	for _, source := range sources {
		grant, ok := byID[source.ID]
		if !ok || !grant.CanRead || grant.Revision != source.Revision {
			return false
		}
	}
	return true
}
func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func cloneCitations(values []SourceCitation) []SourceCitation {
	return append([]SourceCitation(nil), values...)
}
func cloneSamples(values []SampleCard) []SampleCard {
	out := append([]SampleCard(nil), values...)
	for i := range out {
		out[i].Fields = cloneRawMap(out[i].Fields)
	}
	return out
}
func cloneProposal(proposal BoardProposal) BoardProposal {
	proposal.Assumptions = append([]string(nil), proposal.Assumptions...)
	proposal.CitedSources = cloneCitations(proposal.CitedSources)
	proposal.SampleCards = cloneSamples(proposal.SampleCards)
	proposal.Sharing = append([]ProposalShare(nil), proposal.Sharing...)
	return proposal
}

func diffConfigs(current, pending Config) ProposalConfigDiff {
	diff := ProposalConfigDiff{}
	statuses := func(values []Status) map[string]Status {
		out := map[string]Status{}
		for _, value := range values {
			out[value.ID] = value
		}
		return out
	}
	fields := func(values []Field) map[string]Field {
		out := map[string]Field{}
		for _, value := range values {
			out[value.ID] = value
		}
		return out
	}
	columns := func(values []Column) map[string]Column {
		out := map[string]Column{}
		for _, value := range values {
			out[value.ID] = value
		}
		return out
	}
	collect := func(a, b map[string]any, added, removed, changed *[]string) {
		for id, value := range b {
			old, ok := a[id]
			if !ok {
				*added = append(*added, id)
			} else if !reflect.DeepEqual(old, value) {
				*changed = append(*changed, id)
			}
		}
		for id := range a {
			if _, ok := b[id]; !ok {
				*removed = append(*removed, id)
			}
		}
		sort.Strings(*added)
		sort.Strings(*removed)
		sort.Strings(*changed)
	}
	toAny := func(values map[string]Status) map[string]any {
		out := map[string]any{}
		for id, value := range values {
			out[id] = value
		}
		return out
	}
	collect(toAny(statuses(current.Statuses)), toAny(statuses(pending.Statuses)), &diff.AddedStatusIDs, &diff.RemovedStatusIDs, &diff.ChangedStatusIDs)
	toFields := func(values map[string]Field) map[string]any {
		out := map[string]any{}
		for id, value := range values {
			out[id] = value
		}
		return out
	}
	collect(toFields(fields(current.Fields)), toFields(fields(pending.Fields)), &diff.AddedFieldIDs, &diff.RemovedFieldIDs, &diff.ChangedFieldIDs)
	toColumns := func(values map[string]Column) map[string]any {
		out := map[string]any{}
		for id, value := range values {
			out[id] = value
		}
		return out
	}
	collect(toColumns(columns(current.Columns)), toColumns(columns(pending.Columns)), &diff.AddedColumnIDs, &diff.RemovedColumnIDs, &diff.ChangedColumnIDs)
	return diff
}
