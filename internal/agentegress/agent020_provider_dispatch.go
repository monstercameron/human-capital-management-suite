package agentegress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

const (
	// RefusalProviderContract identifies missing or mismatched reviewed provider terms.
	RefusalProviderContract RefusalCode = "AGENT_EGRESS_PROVIDER_CONTRACT"
	// RefusalProviderLease identifies a provider credential lease refusal.
	RefusalProviderLease RefusalCode = "AGENT_EGRESS_PROVIDER_LEASE"
	// RefusalProviderSource identifies a missing or refused source classification.
	RefusalProviderSource RefusalCode = "AGENT_EGRESS_PROVIDER_SOURCE_CLASS"
	// RefusalProviderAdapter identifies an adapter that cannot satisfy the pinned request.
	RefusalProviderAdapter RefusalCode = "AGENT_EGRESS_PROVIDER_ADAPTER"
	// RefusalProviderTool identifies provider-hosted tools without a reviewed capability.
	RefusalProviderTool RefusalCode = "AGENT_EGRESS_PROVIDER_HOSTED_TOOL"
	// RefusalProviderCost identifies missing authoritative cost for token usage.
	RefusalProviderCost RefusalCode = "AGENT_EGRESS_PROVIDER_COST_UNRECONCILED"
)

// ProviderSourceRule allows a source classification to contribute only the
// explicitly listed data classes to this provider profile.
type ProviderSourceRule struct {
	Class   string
	Classes []trustdlp.DataClass
}

// ProviderTerms is the reviewed processing contract for one model profile.
// It contains references and terms only; credential material is never stored.
type ProviderTerms struct {
	ModelProfile     string
	ProviderID       string
	ModelID          string
	ModelVersion     string
	ContractRef      string
	EgressGrantRef   string
	Approved         bool
	Encryption       bool
	AllowedRegions   []string
	AllowedClasses   []trustdlp.DataClass
	Retention        RetentionPolicy
	TrainingUse      agentmodel.ProcessingUse
	Logging          agentmodel.ProcessingUse
	SourceRules      []ProviderSourceRule
	HostedNetworkUse bool
	HostedStateUse   bool
}

// providerIdentity reports the immutable identity pinned to a configured
// adapter. ModelAdapter intentionally keeps this outside its common contract.
type providerIdentity interface {
	Identity() agentmodel.ModelIdentity
}

// ProviderLeaseConsumer verifies and consumes a single-use credential lease.
// The interface carries no provider secret value.
type ProviderLeaseConsumer interface {
	Use(lease.CredentialLease, string, custody.Operation) (lease.Evidence, error)
}

// SourceClassificationVerifier resolves owner-issued provenance and current
// source labels before a field can be considered for provider egress.
type SourceClassificationVerifier interface {
	VerifySourceClassification(context.Context, SourceClassificationRequest) error
}

// SourceClassificationRequest carries labels and provenance only, never the
// field value, so the authoritative verifier need not receive prompt content.
type SourceClassificationRequest struct {
	Tenant, Purpose, FieldName, SourceClass string
	DataClass                               trustdlp.DataClass
	Provenance                              []string
	ValueDigest                             string
	MessageRole                             agentmodel.MessageRole
}

// ProviderDispatcher centralizes checks that must pass before model dispatch.
type ProviderDispatcher struct {
	evaluator *Evaluator
	leases    ProviderLeaseConsumer
	sources   SourceClassificationVerifier
	terms     map[string]ProviderTerms
}

// NewProviderDispatcher freezes reviewed model terms for fail-closed dispatch.
func NewProviderDispatcher(evaluator *Evaluator, leases ProviderLeaseConsumer, sources SourceClassificationVerifier, terms []ProviderTerms) (*ProviderDispatcher, error) {
	if evaluator == nil || leases == nil || sources == nil || len(terms) == 0 {
		return nil, refuse(RefusalProviderContract, "dispatcher", "egress evaluator, lease consumer, source verifier and provider terms are required")
	}
	byProfile := make(map[string]ProviderTerms, len(terms))
	for _, term := range terms {
		if err := validateProviderTerms(term); err != nil {
			return nil, err
		}
		if _, exists := byProfile[term.ModelProfile]; exists {
			return nil, refuse(RefusalProviderContract, "model_profile", "provider profile is duplicated")
		}
		byProfile[term.ModelProfile] = cloneProviderTerms(term)
	}
	return &ProviderDispatcher{evaluator: evaluator, leases: leases, sources: sources, terms: byProfile}, nil
}

// ProviderDispatchRequest binds the model contract, classified source fields,
// destination policy and one use of the provider credential lease.
type ProviderDispatchRequest struct {
	Model        agentmodel.ModelRequest
	Outbound     OutboundRequest
	FieldSources map[string]string
	Lease        lease.CredentialLease
}

// DispatchResult contains the provider result and audit-safe egress evidence.
type DispatchResult struct {
	Model          agentmodel.ModelResult
	Egress         EgressReceipt
	LeaseID        string
	ContractRef    string
	EgressGrantRef string
	SourceClasses  []string
}

// ProviderDispatchFailure exposes only the normalized failure code and retry
// flag, never the underlying adapter or provider error string.
type ProviderDispatchFailure struct {
	Code      agentmodel.FailureCode
	Retryable bool
}

// Error returns a normalized failure without provider response details.
func (e ProviderDispatchFailure) Error() string {
	return fmt.Sprintf("agent egress: provider dispatch failed (%s)", e.Code)
}

// Dispatch checks the complete request before consuming the credential lease
// and calling the provider. Every refusal before the final call produces zero
// adapter invocations.
func (d *ProviderDispatcher) Dispatch(ctx context.Context, req ProviderDispatchRequest, adapter agentmodel.ModelAdapter) (DispatchResult, error) {
	if d == nil || d.evaluator == nil || d.leases == nil || d.sources == nil || adapter == nil {
		return DispatchResult{}, refuse(RefusalProviderContract, "dispatcher", "dispatcher and provider adapter are required")
	}
	if ctx == nil {
		return DispatchResult{}, refuse(RefusalInvalidRequest, "context", "context is required")
	}
	if err := ctx.Err(); err != nil {
		return DispatchResult{}, refuse(RefusalInvalidRequest, "context", "cancelled requests cannot dispatch")
	}
	if err := agentmodel.ValidateModelRequest(req.Model); err != nil {
		return DispatchResult{}, refuse(RefusalProviderContract, "model_request", "model request contract is invalid")
	}
	term, ok := d.terms[req.Model.ModelProfile]
	if !ok {
		return DispatchResult{}, refuse(RefusalProviderContract, "model_profile", "model profile has no approved provider terms")
	}
	identified, ok := adapter.(providerIdentity)
	if !ok {
		return DispatchResult{}, refuse(RefusalProviderAdapter, "adapter_identity", "adapter must expose its pinned provider identity before dispatch")
	}
	if err := validateProviderDispatch(req, term, identified.Identity()); err != nil {
		return DispatchResult{}, err
	}
	if refusal := agentmodel.CheckCapabilities(req.Model, adapter.Capabilities()); refusal != nil {
		return DispatchResult{}, refuse(RefusalProviderAdapter, "capabilities", "adapter cannot honor the model request contract")
	}
	if err := validateMessageBinding(req); err != nil {
		return DispatchResult{}, err
	}
	if err := validateSourceBindings(ctx, d.sources, req, term); err != nil {
		return DispatchResult{}, err
	}
	egress, err := d.evaluator.EvaluateOutbound(req.Outbound)
	if err != nil {
		return DispatchResult{}, err
	}
	if !egress.Allowed {
		return DispatchResult{}, refuse(RefusalDLP, "egress", "outbound evaluator did not allow the provider payload")
	}
	if err := validateLeaseBinding(req, term); err != nil {
		return DispatchResult{}, err
	}
	if _, err := d.leases.Use(req.Lease, req.Outbound.Profile.ID, custody.Decrypt); err != nil {
		return DispatchResult{}, refuse(RefusalProviderLease, "credential_lease", "provider credential lease is invalid, expired, revoked or already used")
	}
	result, invokeErr := adapter.Invoke(ctx, req.Model)
	if err := agentmodel.ValidateModelResult(req.Model, result, adapter.Capabilities()); err != nil {
		if invokeErr != nil {
			// An adapter that failed returns no result; name its own error
			// instead of the validation of that empty result.
			return DispatchResult{}, fmt.Errorf("%w: adapter: %v", err, invokeErr)
		}
		return DispatchResult{}, err
	}
	if err := validateProviderIdentity(term, result); err != nil {
		return DispatchResult{}, err
	}
	if result.Failure == nil && result.Refusal == nil && result.Usage.TotalTokens > 0 && result.Usage.CostMicros == 0 {
		return DispatchResult{}, refuse(RefusalProviderCost, "provider_usage.cost", "zero reported cost requires an authoritative pricing schedule before result delivery")
	}
	if invokeErr != nil {
		if result.Finish == agentmodel.FinishCancelled && (errors.Is(invokeErr, context.Canceled) || errors.Is(invokeErr, context.DeadlineExceeded)) {
			return providerDispatchResult(req, term, result, egress.Receipt), invokeErr
		}
		if result.Failure == nil {
			return DispatchResult{}, refuse(RefusalProviderContract, "provider_failure", "adapter failed without a valid normalized provider failure")
		}
		failure := *result.Failure
		failure.Message = ""
		result.Failure = &failure
		return providerDispatchResult(req, term, result, egress.Receipt), ProviderDispatchFailure{Code: failure.Code, Retryable: failure.Retryable}
	}
	return providerDispatchResult(req, term, result, egress.Receipt), nil
}

func providerDispatchResult(req ProviderDispatchRequest, term ProviderTerms, result agentmodel.ModelResult, receipt EgressReceipt) DispatchResult {
	sources := make([]string, 0, len(req.Outbound.DeclaredFields))
	seen := make(map[string]struct{}, len(req.Outbound.DeclaredFields))
	for _, name := range req.Outbound.DeclaredFields {
		source := req.FieldSources[name]
		if _, exists := seen[source]; !exists {
			seen[source] = struct{}{}
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)
	return DispatchResult{Model: result, Egress: receipt, LeaseID: req.Lease.ID, ContractRef: term.ContractRef, EgressGrantRef: term.EgressGrantRef, SourceClasses: sources}
}

func validateProviderDispatch(req ProviderDispatchRequest, term ProviderTerms, identity agentmodel.ModelIdentity) error {
	if !term.Approved || !term.Encryption || term.ContractRef == "" || term.EgressGrantRef == "" {
		return refuse(RefusalProviderContract, "provider_terms", "provider contract, encryption and egress grant must be approved")
	}
	if identity.ProviderID != term.ProviderID || identity.ModelID != term.ModelID || identity.Version != term.ModelVersion {
		return refuse(RefusalProviderContract, "adapter_identity", "adapter identity does not match the approved provider model")
	}
	if req.Outbound.Profile.Kind != TargetModel || req.Outbound.Profile.ID != term.ModelProfile || req.Model.ModelProfile != term.ModelProfile {
		return refuse(RefusalProviderContract, "model_profile", "request, destination and approved profile must match")
	}
	if req.Model.Processing.Residency == "" || req.Model.Processing.Residency != req.Outbound.Region {
		return refuse(RefusalRegion, "processing.residency", "model residency must match the selected provider region")
	}
	if !req.Model.Deadline.After(req.Outbound.Now) {
		return refuse(RefusalInvalidRequest, "deadline", "model deadline must be later than the dispatch evaluation time")
	}
	if !contains(req.Outbound.Profile.AllowedRegions, req.Outbound.Region) {
		return refuse(RefusalRegion, "region", "provider region is outside its approved profile")
	}
	if !sameStrings(req.Outbound.Profile.AllowedRegions, term.AllowedRegions) || !sameClasses(req.Outbound.Profile.AllowedClasses, term.AllowedClasses) {
		return refuse(RefusalProviderContract, "provider_profile", "destination residency and class policy must match reviewed provider terms")
	}
	if req.Outbound.Profile.Retention != term.Retention || req.Model.Processing.Retention != retentionContract(term.Retention) {
		return refuse(RefusalRetention, "processing.retention", "provider retention does not match the approved contract")
	}
	if !processingUseSatisfies(term.TrainingUse, req.Model.Processing.TrainingUse) ||
		!processingUseSatisfies(term.Logging, req.Model.Processing.Logging) {
		return refuse(RefusalProviderContract, "processing.use", "provider training or logging terms do not satisfy the request")
	}
	if term.HostedNetworkUse || term.HostedStateUse {
		return refuse(RefusalProviderTool, "hosted_tools", "provider-hosted network or persistent-state tools require a separately reviewed adapter")
	}
	return nil
}

func validateProviderIdentity(term ProviderTerms, result agentmodel.ModelResult) error {
	if result.ContractVersion != agentmodel.ContractVersion || result.Provider.ProviderID != term.ProviderID || result.Provider.ModelID != term.ModelID || result.Provider.Version != term.ModelVersion {
		return refuse(RefusalProviderContract, "provider_result", "provider result identity does not match the approved model profile")
	}
	return nil
}

func validateMessageBinding(req ProviderDispatchRequest) error {
	for i, message := range req.Model.Messages {
		name := fmt.Sprintf("model.message.%d", i)
		field, found := findField(req.Outbound.Fields, name)
		value := message.Content
		if message.Role == agentmodel.RoleAssistant && message.ToolCallID != "" {
			value = string(message.ToolArguments)
		}
		if !found || field.Value != value || !contains(req.Outbound.DeclaredFields, name) {
			return refuse(RefusalMinimumNecessary, name, "every model message must be present verbatim in the classified outbound field set")
		}
	}
	for i, ref := range req.Model.ContextRefs {
		name := fmt.Sprintf("model.context.%d", i)
		field, found := findField(req.Outbound.Fields, name)
		if !found || !contains(req.Outbound.DeclaredFields, name) {
			return refuse(RefusalClassification, name, "every context reference must be classified before dispatch")
		}
		encoded := fmt.Sprintf("%s\x00%s\x00%s", ref.ID, ref.Version, ref.Digest)
		if field.Value != encoded {
			return refuse(RefusalClassification, name, "context reference does not match its classified outbound field")
		}
	}
	return nil
}

func validateSourceBindings(ctx context.Context, verifier SourceClassificationVerifier, req ProviderDispatchRequest, term ProviderTerms) error {
	roles := make(map[string]agentmodel.MessageRole, len(req.Model.Messages))
	for i, message := range req.Model.Messages {
		roles[fmt.Sprintf("model.message.%d", i)] = message.Role
	}
	rules := make(map[string]map[trustdlp.DataClass]struct{}, len(term.SourceRules))
	for _, rule := range term.SourceRules {
		classes := make(map[trustdlp.DataClass]struct{}, len(rule.Classes))
		for _, class := range rule.Classes {
			classes[class] = struct{}{}
		}
		rules[rule.Class] = classes
	}
	for _, name := range req.Outbound.DeclaredFields {
		source, ok := req.FieldSources[name]
		allowed, sourceOK := rules[source]
		field, fieldOK := findField(req.Outbound.Fields, name)
		if !ok || !sourceOK || !fieldOK {
			return refuse(RefusalProviderSource, name, "field has no approved source classification")
		}
		if _, classOK := allowed[field.Class]; !classOK {
			return refuse(RefusalProviderSource, name, "source classification does not permit this data class")
		}
		value, ok := field.Value.(string)
		if !ok {
			return refuse(RefusalProviderSource, name, "model source value must be canonical text")
		}
		digest := sha256.Sum256([]byte(value))
		proof := SourceClassificationRequest{Tenant: req.Outbound.Tenant, Purpose: req.Outbound.Purpose, FieldName: name, SourceClass: source, DataClass: field.Class, Provenance: append([]string(nil), field.Provenance...), ValueDigest: "sha256:" + hex.EncodeToString(digest[:]), MessageRole: roles[name]}
		if err := verifier.VerifySourceClassification(ctx, proof); err != nil {
			return refuse(RefusalProviderSource, name, "authoritative source classification verification failed")
		}
	}
	return nil
}

func validateLeaseBinding(req ProviderDispatchRequest, term ProviderTerms) error {
	l := req.Lease
	if strings.TrimSpace(l.ID) == "" || l.Handle.Validate() != nil || l.Workload == "" || l.Destination != req.Outbound.Profile.ID ||
		l.Tenant != req.Outbound.Tenant || l.Purpose != req.Outbound.Purpose ||
		l.Operation != custody.Decrypt || l.Handle.Tenant != req.Outbound.Tenant ||
		l.Handle.Region != req.Outbound.Region || term.ProviderID == "" {
		return refuse(RefusalProviderLease, "credential_lease", "credential lease must be scoped to this tenant, purpose, region and provider destination")
	}
	return nil
}

func validateProviderTerms(term ProviderTerms) error {
	if strings.TrimSpace(term.ModelProfile) == "" || strings.TrimSpace(term.ProviderID) == "" ||
		strings.TrimSpace(term.ModelID) == "" || strings.TrimSpace(term.ModelVersion) == "" ||
		strings.TrimSpace(term.ContractRef) == "" || strings.TrimSpace(term.EgressGrantRef) == "" ||
		validateRetention(term.Retention, "provider_terms.retention") != nil || len(term.AllowedRegions) == 0 || len(term.AllowedClasses) == 0 ||
		!validProcessingUse(term.TrainingUse) || !validProcessingUse(term.Logging) || len(term.SourceRules) == 0 {
		return refuse(RefusalProviderContract, "provider_terms", "provider profile, contract, egress grant, processing terms and source rules are required")
	}
	if err := validateRegions(term.AllowedRegions, "provider_terms.allowed_regions", RefusalProviderContract); err != nil {
		return err
	}
	classes := make(map[trustdlp.DataClass]struct{}, len(term.AllowedClasses))
	for _, class := range term.AllowedClasses {
		if !class.Valid() {
			return refuse(RefusalProviderContract, "provider_terms.allowed_classes", "provider terms contain an unknown data class")
		}
		if _, duplicate := classes[class]; duplicate {
			return refuse(RefusalProviderContract, "provider_terms.allowed_classes", "provider terms contain duplicate data classes")
		}
		classes[class] = struct{}{}
	}
	seen := make(map[string]struct{}, len(term.SourceRules))
	for _, rule := range term.SourceRules {
		if strings.TrimSpace(rule.Class) == "" || strings.TrimSpace(rule.Class) != rule.Class || len(rule.Classes) == 0 {
			return refuse(RefusalProviderSource, "source_rules", "source classifications must be canonical and closed-world")
		}
		if _, duplicate := seen[rule.Class]; duplicate {
			return refuse(RefusalProviderSource, "source_rules", "source classification is duplicated")
		}
		seen[rule.Class] = struct{}{}
		classes := make(map[trustdlp.DataClass]struct{}, len(rule.Classes))
		for _, class := range rule.Classes {
			if !class.Valid() {
				return refuse(RefusalProviderSource, "source_rules", "source rule contains an unknown data class")
			}
			if _, duplicate := classes[class]; duplicate {
				return refuse(RefusalProviderSource, "source_rules", "source rule contains a duplicate data class")
			}
			classes[class] = struct{}{}
		}
	}
	return nil
}

func validProcessingUse(use agentmodel.ProcessingUse) bool {
	return use == agentmodel.UseAllowed || use == agentmodel.UseDenied
}

// processingUseSatisfies requires explicit provider terms and rejects any
// provider behavior that is more permissive than the request requires.
func processingUseSatisfies(provider, required agentmodel.ProcessingUse) bool {
	if !validProcessingUse(provider) || !validProcessingUse(required) {
		return false
	}
	return provider == agentmodel.UseDenied || required == agentmodel.UseAllowed
}

func retentionContract(policy RetentionPolicy) string {
	return fmt.Sprintf("%s:%d", policy.Mode, int64(policy.MaxAge))
}

func findField(fields []Field, name string) (Field, bool) {
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

func cloneProviderTerms(term ProviderTerms) ProviderTerms {
	term.SourceRules = append([]ProviderSourceRule(nil), term.SourceRules...)
	term.AllowedRegions = append([]string(nil), term.AllowedRegions...)
	term.AllowedClasses = append([]trustdlp.DataClass(nil), term.AllowedClasses...)
	for i := range term.SourceRules {
		term.SourceRules[i].Classes = append([]trustdlp.DataClass(nil), term.SourceRules[i].Classes...)
	}
	return term
}

// CanonicalProviderTerms is an audit-safe stable representation used for
// review and golden checks. It contains no credential or payload material.
func CanonicalProviderTerms(term ProviderTerms) string {
	rules := make([]string, 0, len(term.SourceRules))
	for _, rule := range term.SourceRules {
		classes := make([]string, 0, len(rule.Classes))
		for _, class := range rule.Classes {
			classes = append(classes, string(class))
		}
		sort.Strings(classes)
		rules = append(rules, rule.Class+"="+strings.Join(classes, ","))
	}
	sort.Strings(rules)
	regions := append([]string(nil), term.AllowedRegions...)
	sort.Strings(regions)
	classes := make([]string, 0, len(term.AllowedClasses))
	for _, class := range term.AllowedClasses {
		classes = append(classes, string(class))
	}
	sort.Strings(classes)
	return strings.Join([]string{term.ModelProfile, term.ProviderID, term.ModelID, term.ModelVersion, term.ContractRef, term.EgressGrantRef, string(term.Retention.Mode), fmt.Sprint(int64(term.Retention.MaxAge)), string(term.TrainingUse), string(term.Logging), strings.Join(regions, ","), strings.Join(classes, ","), strings.Join(rules, ";"), fmt.Sprint(term.Approved), fmt.Sprint(term.Encryption), fmt.Sprint(term.HostedNetworkUse), fmt.Sprint(term.HostedStateUse)}, "\x00")
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameClasses(left, right []trustdlp.DataClass) bool {
	a, b := make([]string, len(left)), make([]string, len(right))
	for i, class := range left {
		a[i] = string(class)
	}
	for i, class := range right {
		b[i] = string(class)
	}
	return sameStrings(a, b)
}
