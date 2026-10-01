package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

var ErrPersonaModelConfiguration = errors.New("application: invalid persona model deployment configuration")

// PersonaModelDeployment is a deployment-owned approval document, containing
// no provider credentials or private signing material. Durations are seconds.
// Pricing.Signature signs the UTF-8 PricingScheduleDigest with PricingPublicKey.
type PersonaModelDeployment struct {
	PublicTaskRetentionSeconds int                                    `json:"public_task_retention_seconds,omitempty"`
	Version                    int                                    `json:"version"`
	BaseURL                    string                                 `json:"base_url,omitempty"`
	Profiles                   []agentmodel.ModelProfile              `json:"profiles"`
	Terms                      []agentegress.ProviderTerms            `json:"terms"`
	Pricing                    agentmodel.PricingSchedule             `json:"pricing"`
	PricingPublicKey           string                                 `json:"pricing_public_key"`
	Destinations               []outbound.Destination                 `json:"destinations"`
	Clearances                 []trustdlp.Clearance                   `json:"clearances"`
	Credential                 PersonaModelCredentialDeployment       `json:"credential"`
	Worker                     PersonaModelWorkerDeployment           `json:"worker"`
	OutputKeyID                string                                 `json:"output_key_id"`
	PolicySources              []PersonaModelPolicySourceConfig       `json:"policy_sources,omitempty"`
	PolicyRegistrations        []PersonaModelPolicyRegistrationConfig `json:"policy_registrations,omitempty"`
}

type PersonaModelCredentialDeployment struct {
	ID              string                       `json:"id"`
	Version         string                       `json:"version"`
	Scopes          []OpenAIModelCredentialScope `json:"scopes"`
	LeaseTTLSeconds int64                        `json:"lease_ttl_seconds"`
}

type PersonaModelWorkerDeployment struct {
	ID                 string `json:"id"`
	Workload           string `json:"workload"`
	Issuer             string `json:"issuer"`
	KeyID              string `json:"key_id"`
	Cell               string `json:"cell"`
	IdentityTTLSeconds int64  `json:"identity_ttl_seconds"`
	RunLeaseTTLSeconds int64  `json:"run_lease_ttl_seconds"`
}

type PersonaModelDeploymentDependencies struct {
	Budget    agentmodel.Budget
	Routes    agentmodel.RouteRecorder
	Sources   agentegress.SourceClassificationVerifier
	Resources AgentModelResourceAdmission
	Now       func() time.Time
}

func LoadPersonaModelDeployment(path string) (PersonaModelDeployment, error) {
	if strings.TrimSpace(path) == "" {
		return PersonaModelDeployment{}, modelConfigurationError("configuration file is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return PersonaModelDeployment{}, modelConfigurationError("configuration file cannot be opened")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return PersonaModelDeployment{}, modelConfigurationError("configuration file exceeds 1 MiB or cannot be read")
	}
	return ParsePersonaModelDeployment(raw)
}

func ParsePersonaModelDeployment(raw []byte) (PersonaModelDeployment, error) {
	var cfg PersonaModelDeployment
	if len(raw) > 1<<20 {
		return cfg, modelConfigurationError("configuration exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return PersonaModelDeployment{}, modelConfigurationError("configuration must be valid JSON with known fields")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return PersonaModelDeployment{}, modelConfigurationError("configuration must contain one JSON document")
	}
	if _, _, err := cfg.validate(); err != nil {
		return PersonaModelDeployment{}, err
	}
	return cfg, nil
}

func modelConfigurationError(message string) error {
	return fmt.Errorf("%w: %s", ErrPersonaModelConfiguration, message)
}

func (c PersonaModelDeployment) validate() (*agentmodel.PricingSchedule, *agentegress.Evaluator, error) {
	if c.PublicTaskRetentionSeconds < 0 || c.PublicTaskRetentionSeconds > 30*24*60*60 {
		return nil, nil, modelConfigurationError("public task retention must be between zero and 30 days")
	}
	if err := validateConfiguredPersonaModelPolicies(c); err != nil {
		return nil, nil, err
	}
	if c.Version != 1 || len(c.Profiles) == 0 || len(c.Terms) != len(c.Profiles) || len(c.Destinations) == 0 || len(c.Clearances) == 0 {
		return nil, nil, modelConfigurationError("version 1, profiles, terms, destinations and clearances are required")
	}
	for _, v := range []string{c.OutputKeyID, c.Worker.ID, c.Worker.Workload, c.Worker.Issuer, c.Worker.KeyID, c.Worker.Cell, c.Credential.ID, c.Credential.Version} {
		if !canonicalOpenAIValue(v) {
			return nil, nil, modelConfigurationError("explicit canonical worker, output and credential identities are required")
		}
	}
	if c.OutputKeyID == c.Worker.KeyID || c.Worker.IdentityTTLSeconds < 1 || c.Worker.IdentityTTLSeconds > int64(workload.MaxIdentityLifetime/time.Second) || c.Worker.RunLeaseTTLSeconds < 1 || c.Worker.RunLeaseTTLSeconds > 900 || c.Credential.LeaseTTLSeconds < 60 || c.Credential.LeaseTTLSeconds > 900 {
		return nil, nil, modelConfigurationError("distinct signing key IDs, identity/run lease lifetimes of 1-900 seconds and credential lease lifetime of 60-900 seconds are required")
	}
	if _, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: c.Credential.ID, Version: c.Credential.Version, Workload: c.Worker.Workload, Scopes: c.Credential.Scopes, MaxTTL: time.Duration(c.Credential.LeaseTTLSeconds) * time.Second, Now: time.Now}); err != nil {
		return nil, nil, modelConfigurationError("credential scope is invalid")
	}
	for _, p := range c.Profiles {
		if p.Identity.ProviderID != "openai" || !p.Evaluation.Passed || !canonicalOpenAIValue(p.Evaluation.AgentVersionDigest) || !canonicalOpenAIValue(p.Evaluation.SuiteDigest) {
			return nil, nil, modelConfigurationError("OpenAI profiles require passing agent-version evaluation evidence")
		}
	}
	if _, err := agentmodel.NewRouter(c.Profiles, personaModelConfigurationRouteValidator{}); err != nil {
		return nil, nil, modelConfigurationError("model profile or profile digest is invalid")
	}
	pub, err := base64.StdEncoding.Strict().DecodeString(c.PricingPublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, nil, modelConfigurationError("pricing_public_key must be a standard-base64 Ed25519 public key")
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(c.Pricing.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, []byte(agentmodel.PricingScheduleDigest(c.Pricing)), sig) {
		return nil, nil, modelConfigurationError("pricing signature does not verify")
	}
	pricing, err := agentmodel.NewPricingSchedule(c.Pricing)
	if err != nil {
		return nil, nil, modelConfigurationError("pricing schedule is invalid")
	}
	for _, p := range c.Profiles {
		matched := false
		for _, rate := range pricing.Entries {
			if rate.Identity == p.Identity {
				matched = true
			}
		}
		if !matched {
			return nil, nil, modelConfigurationError("profile lacks signed pricing")
		}
	}
	egress, err := c.egress()
	if err != nil {
		return nil, nil, err
	}
	// The dispatcher owns full reviewed-term validation. The lease and source
	// ports below cannot be called while its constructor validates the document.
	if _, err := agentegress.NewProviderDispatcher(egress, personaModelConfigurationLeaseValidator{}, personaModelConfigurationSourceValidator{}, c.Terms); err != nil {
		return nil, nil, modelConfigurationError("reviewed provider terms are invalid")
	}
	for _, p := range c.Profiles {
		matches := 0
		for _, term := range c.Terms {
			if term.ModelProfile == p.ID && term.ProviderID == p.Identity.ProviderID && term.ModelID == p.Identity.ModelID && term.ModelVersion == p.Identity.Version {
				matches++
			}
		}
		if matches != 1 {
			return nil, nil, modelConfigurationError("profile requires exactly one matching provider contract")
		}
	}
	seenScopes := make(map[OpenAIModelCredentialScope]bool)
	for _, scope := range c.Credential.Scopes {
		if seenScopes[scope] {
			return nil, nil, modelConfigurationError("credential scopes must be unique")
		}
		seenScopes[scope] = true
		profileOK, destinationOK, termsOK := false, false, false
		for _, p := range c.Profiles {
			if p.ID == scope.Destination && slices.Contains(p.Regions, scope.Region) {
				profileOK = true
			}
		}
		for _, d := range c.Destinations {
			if d.Name == scope.Destination && slices.Contains(d.Purposes, scope.Purpose) {
				destinationOK = true
			}
		}
		for _, term := range c.Terms {
			if term.ModelProfile == scope.Destination && slices.Contains(term.AllowedRegions, scope.Region) {
				termsOK = true
			}
		}
		if !profileOK || !destinationOK || !termsOK {
			return nil, nil, modelConfigurationError("credential scope must match an approved profile, processing region and destination purpose")
		}
	}
	return pricing, egress, nil
}

func (c PersonaModelDeployment) egress() (*agentegress.Evaluator, error) {
	policy, err := outbound.NewPolicy(c.Destinations...)
	if err != nil {
		return nil, modelConfigurationError("outbound destination policy is invalid")
	}
	dlp, err := trustdlp.NewPolicy(policy, c.Clearances...)
	if err != nil {
		return nil, modelConfigurationError("data classification clearance is invalid")
	}
	inspector, err := trustdlp.NewInspector()
	if err != nil {
		return nil, modelConfigurationError("data inspector cannot be constructed")
	}
	return agentegress.NewEvaluator(dlp, inspector, trustdlp.NewReceiptLog())
}

// ComposePersonaRuntimeModel installs only explicitly configured authority.
// Signed worker credentials are renewed and verified for each resolution.
func ComposePersonaRuntimeModel(cfg PersonaModelDeployment, apiKey, outputSeed, workloadSeed string, deps PersonaModelDeploymentDependencies) (PersonaRuntimeModelComposition, *agentsecurity.FinalOutputRecoveryVerifier, error) {
	return composePersonaRuntimeModel(cfg, apiKey, outputSeed, workloadSeed, deps, false)
}

// ComposePersonaRuntimeTypedModel places a raw priced adapter beneath the
// ordinary platform's outer SchemaFlux generator and shares the same authority.
func ComposePersonaRuntimeTypedModel(cfg PersonaModelDeployment, apiKey, outputSeed, workloadSeed string, deps PersonaModelDeploymentDependencies) (PersonaRuntimeModelComposition, *agentsecurity.FinalOutputRecoveryVerifier, error) {
	return composePersonaRuntimeModel(cfg, apiKey, outputSeed, workloadSeed, deps, true)
}

func composePersonaRuntimeModel(cfg PersonaModelDeployment, apiKey, outputSeed, workloadSeed string, deps PersonaModelDeploymentDependencies, typed bool) (PersonaRuntimeModelComposition, *agentsecurity.FinalOutputRecoveryVerifier, error) {
	var empty PersonaRuntimeModelComposition
	if strings.TrimSpace(apiKey) == "" {
		return empty, nil, modelConfigurationError("MODEL_API_KEY is required")
	}
	if isNilPersonaOutputPort(deps.Budget) || isNilPersonaOutputPort(deps.Routes) || isNilPersonaOutputPort(deps.Sources) || deps.Now == nil {
		return empty, nil, modelConfigurationError("durable budget, route recorder, source classifier and clock are required")
	}
	pricing, egress, err := cfg.validate()
	if err != nil {
		return empty, nil, err
	}
	output, worker, err := personaModelSigningKeys(outputSeed, workloadSeed)
	if err != nil {
		return empty, nil, err
	}
	for _, source := range cfg.PolicySources {
		key, _ := base64.StdEncoding.Strict().DecodeString(source.PublicKey)
		if bytes.Equal(key, output.Public().(ed25519.PublicKey)) || bytes.Equal(key, worker.Public().(ed25519.PublicKey)) {
			return empty, nil, modelConfigurationError("policy trust keys must be distinct from output and worker signing keys")
		}
	}
	authority, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: cfg.Credential.ID, Version: cfg.Credential.Version, Workload: cfg.Worker.Workload, Scopes: cfg.Credential.Scopes, MaxTTL: time.Duration(cfg.Credential.LeaseTTLSeconds) * time.Second, Now: deps.Now})
	if err != nil {
		return empty, nil, err
	}
	manager, err := lease.NewManager(authority, deps.Now)
	if err != nil {
		return empty, nil, err
	}
	leases, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": authority}, Leases: manager, MaxTTL: time.Duration(cfg.Credential.LeaseTTLSeconds) * time.Second})
	if err != nil {
		return empty, nil, err
	}
	gatewayConfig := OpenAIAgentModelGatewayConfig{APIKey: apiKey, BaseURL: cfg.BaseURL, Profiles: cfg.Profiles, Terms: cfg.Terms, Pricing: pricing, Budget: deps.Budget, Routes: deps.Routes, Egress: egress, Leases: manager, Sources: deps.Sources, Resources: deps.Resources, LeaseBindings: leases}
	constructor := NewOpenAIAgentModelGateway
	if typed {
		constructor = NewOpenAITypedAgentModelGateway
	}
	gateway, err := constructor(gatewayConfig)
	if err != nil {
		return empty, nil, err
	}
	recovery, err := agentsecurity.NewFinalOutputRecoveryAuthority(cfg.OutputKeyID, output)
	if err != nil {
		return empty, nil, err
	}
	recoveryVerifier, err := agentsecurity.NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{cfg.OutputKeyID: output.Public().(ed25519.PublicKey)})
	if err != nil {
		return empty, nil, err
	}
	issuer, err := workload.NewIssuer(workload.IssuerConfig{Name: cfg.Worker.Issuer, KeyID: cfg.Worker.KeyID, Private: worker, Now: deps.Now})
	if err != nil {
		return empty, nil, err
	}
	keys := &personaModelWorkloadPublicKey{issuer: cfg.Worker.Issuer, keyID: cfg.Worker.KeyID, key: worker.Public().(ed25519.PublicKey)}
	verifier, err := workload.NewVerifier(workload.VerifierConfig{Keys: keys, Cell: cfg.Worker.Cell, Now: deps.Now})
	if err != nil {
		return empty, nil, err
	}
	credentials := &personaModelRenewingCredential{issuer: issuer, spec: workload.IssueSpec{Subject: cfg.Worker.ID, Role: workload.RoleWorker, Cell: cfg.Worker.Cell, Lifetime: time.Duration(cfg.Worker.IdentityTTLSeconds) * time.Second}}
	return PersonaRuntimeModelComposition{Gateway: gateway, Leases: leases, Workload: cfg.Worker.Workload, WorkerID: cfg.Worker.ID, LeaseTTL: time.Duration(cfg.Worker.RunLeaseTTLSeconds) * time.Second, WorkerIdentity: &VerifiedPersonaPrivateChatWorkloadIdentitySource{Verifier: verifier, Credentials: credentials}, Recovery: recovery, RecoveryVerifier: recoveryVerifier}, recoveryVerifier, nil
}

func personaModelSigningKeys(outputSeed, workloadSeed string) (ed25519.PrivateKey, ed25519.PrivateKey, error) {
	output, err := base64.StdEncoding.Strict().DecodeString(outputSeed)
	if err != nil || len(output) != ed25519.SeedSize {
		return nil, nil, modelConfigurationError(EnvPersonaOutputSigningSeed + " must be a standard-base64 Ed25519 seed")
	}
	worker, err := base64.StdEncoding.Strict().DecodeString(workloadSeed)
	if err != nil || len(worker) != ed25519.SeedSize {
		return nil, nil, modelConfigurationError(EnvPersonaWorkloadSigningSeed + " must be a standard-base64 Ed25519 seed")
	}
	if bytes.Equal(output, worker) {
		return nil, nil, modelConfigurationError("output and worker signing seeds must be distinct")
	}
	return ed25519.NewKeyFromSeed(output), ed25519.NewKeyFromSeed(worker), nil
}

func (c ServeConfig) validatePersonaModelConfiguration() error {
	if c.AgentModelConfigFile == "" && c.PersonaOutputSigningSeed == "" && c.PersonaWorkloadSigningSeed == "" {
		return nil
	}
	if strings.TrimSpace(c.AgentModelConfigFile) == "" {
		return modelConfigurationError(EnvAgentModelConfigFile + " is required with persona signing seeds")
	}
	if strings.TrimSpace(c.AgentDatabaseURL) == "" {
		return modelConfigurationError("agent model configuration requires the independent agent database")
	}
	output, worker, err := personaModelSigningKeys(c.PersonaOutputSigningSeed, c.PersonaWorkloadSigningSeed)
	if err != nil {
		return err
	}
	for _, other := range []string{c.DevHMACKey, c.PageCursorKey, c.PageCursorPreviousKey, c.ChatCursorKey, c.OIDCSessionSigningKey, c.ClockRuntime.WorkerTokenKey, c.PayrollWebhookSecret, c.IAMWebhookSecret} {
		if bytes.Equal(output.Seed(), []byte(other)) || bytes.Equal(worker.Seed(), []byte(other)) {
			return modelConfigurationError("persona signing seeds must be dedicated")
		}
	}
	for _, other := range []string{c.ConfigBundleSigningSeed, c.ConfigBundleReceiptSeed} {
		if other == "" {
			continue
		}
		decoded, err := decodeEd25519Seed(other)
		if err != nil {
			return modelConfigurationError("configured signing seed is malformed")
		}
		if bytes.Equal(output.Seed(), decoded) || bytes.Equal(worker.Seed(), decoded) {
			return modelConfigurationError("persona signing seeds must be dedicated")
		}
	}
	return nil
}

type personaModelRenewingCredential struct {
	issuer *workload.Issuer
	spec   workload.IssueSpec
}

func (s *personaModelRenewingCredential) PersonaChatWorkerCredential(ctx context.Context) (string, error) {
	if s == nil || s.issuer == nil || ctx == nil || ctx.Err() != nil {
		return "", errPersonaPrivateChatGatewayIdentity
	}
	return s.issuer.Issue(s.spec)
}

type personaModelWorkloadPublicKey struct {
	issuer, keyID string
	key           ed25519.PublicKey
}

func (s *personaModelWorkloadPublicKey) ResolveKey(ctx context.Context, issuer, keyID string) (ed25519.PublicKey, error) {
	if s == nil || ctx == nil || ctx.Err() != nil || issuer != s.issuer || keyID != s.keyID {
		return nil, workload.ErrKeyNotFound
	}
	return append(ed25519.PublicKey(nil), s.key...), nil
}

// These refusing ports let the owning constructors validate immutable
// deployment documents before live dependencies are composed. They cannot
// authorize dispatch, record an inference decision or consume a credential.
type personaModelConfigurationRouteValidator struct{}

func (personaModelConfigurationRouteValidator) RecordRoute(context.Context, agentmodel.RouteRecord) error {
	return ErrPersonaModelConfiguration
}

type personaModelConfigurationLeaseValidator struct{}

func (personaModelConfigurationLeaseValidator) Use(lease.CredentialLease, string, custody.Operation) (lease.Evidence, error) {
	return lease.Evidence{}, ErrPersonaModelConfiguration
}

type personaModelConfigurationSourceValidator struct{}

func (personaModelConfigurationSourceValidator) VerifySourceClassification(context.Context, agentegress.SourceClassificationRequest) error {
	return ErrPersonaModelConfiguration
}
