package application

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chattoneDailyOperations is how many places a person has each day: one for each
// call that leaves the process (a rewrite is one model call; the meaning guard
// runs here and takes none). It equals the per-day step ceiling of the budget
// task, chattoneDailyModelCalls.
const chattoneDailyOperations = chattoneDailyModelCalls

// ChattoneServiceConfig supplies every port of the writing-style service. The
// ports that have a deterministic production implementation default to it.
type ChattoneServiceConfig struct {
	// Model is the governed model call (ChattoneGatewayModel in production).
	Model chatrewrite.Model
	// Policy is the workspace content filter a draft and its rewrite must pass.
	Policy chatrewrite.Policy
	// Conversations reads the writer's recent messages for register.
	Conversations ChattoneConversationSource
	// Authority decides that the caller may write in the conversation. It may be
	// bound after construction, once the chat service it reads exists.
	Authority ChattoneAuthority
	// Administration decides who may change the workspace's styles.
	Administration ChattoneAdministration
	// Settings keeps each workspace's choice across restarts; nil keeps it in
	// memory only.
	Settings ChattoneSettingsStore
	// Meaning defaults to ChattoneMeaningGuard.
	Meaning chatrewrite.Meaning
	// Outbound defaults to ChattoneOutboundVerifier.
	Outbound chatrewrite.Outbound
	// Ledger defaults to an in-process per-person daily counter.
	Ledger          chatrewrite.Ledger
	DailyOperations int
	// Registry defaults to a new opt-in registry: a workspace is off until
	// configured.
	Registry *chatrewrite.Registry
	// Decision is the audience decision port; nil selects the heuristic.
	Decision chatrewrite.AudienceDecision
	Now      func() time.Time
}

// NewChattoneService puts the writing-style service together. It refuses a
// configuration without a model, a content policy or a conversation source, so
// a half-composed service can never answer a request.
func NewChattoneService(cfg ChattoneServiceConfig) (*ChattoneService, error) {
	if isNilPersonaOutputPort(cfg.Model) || isNilPersonaOutputPort(cfg.Policy) || isNilPersonaOutputPort(cfg.Conversations) {
		return nil, fmt.Errorf("%w: model, content policy and conversation source are required", chatrewrite.ErrUnavailable)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if isNilPersonaOutputPort(cfg.Meaning) {
		cfg.Meaning = ChattoneMeaningGuard{}
	}
	if isNilPersonaOutputPort(cfg.Outbound) {
		verifier, err := NewChattoneOutboundVerifier()
		if err != nil {
			return nil, err
		}
		cfg.Outbound = verifier
	}
	if isNilPersonaOutputPort(cfg.Ledger) {
		limit := cfg.DailyOperations
		if limit <= 0 {
			limit = chattoneDailyOperations
		}
		cfg.Ledger = chatrewrite.NewMemoryLedger(limit)
	}
	if cfg.Registry == nil {
		cfg.Registry = chatrewrite.NewRegistry()
		cfg.Registry.RequireConfiguration()
	}
	rewrite := &chatrewrite.Service{Registry: cfg.Registry, Model: cfg.Model, Policy: cfg.Policy, Meaning: cfg.Meaning, Outbound: cfg.Outbound, Ledger: cfg.Ledger, Now: cfg.Now}
	suggestions := chatrewrite.NewSuggestions(cfg.Registry, cfg.Decision)
	suggestions.Now = cfg.Now
	service := &ChattoneService{Rewrite: rewrite, Suggestions: suggestions, Conversations: cfg.Conversations, Now: cfg.Now}
	if !isNilPersonaOutputPort(cfg.Settings) {
		service.Settings = cfg.Settings
	}
	if !isNilPersonaOutputPort(cfg.Authority) {
		service.Authority = cfg.Authority
	}
	if !isNilPersonaOutputPort(cfg.Administration) {
		service.Administration = cfg.Administration
	}
	return service, nil
}

// WritingStylesEnabled reports whether the controls should be offered to the
// caller: the service is composed, the caller's own workspace has the controls
// on, and the model behind it is bound. It answers for the authenticated
// session's workspace only and never reveals another workspace's setting.
func (s *ChattoneService) WritingStylesEnabled(ctx context.Context) bool {
	if s == nil || s.Rewrite == nil || s.Rewrite.Registry == nil || s.Suggestions == nil || s.Authority == nil || s.Conversations == nil {
		return false
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return false
	}
	if ready, ok := s.Rewrite.Model.(interface{ Ready() bool }); ok && !ready.Ready() {
		return false
	}
	if err := s.ensureSettings(ctx, p.Tenant().String()); err != nil {
		return false
	}
	_, enabled := s.Rewrite.Registry.Styles(p.Tenant().String())
	return enabled
}

// Reasons the writing-style controls are not offered, as the features answer
// states them. The page turns each into a plain sentence.
const (
	ChattoneNoteNotQualified = "not_qualified"
	ChattoneNoteWorkspaceOff = "workspace_off"
	ChattoneNoteUnavailable  = "unavailable"
)

// ChattoneNotProvisioned is the service a deployment gets when no model has
// passed the writing-style qualification run. It offers nothing and answers the
// features read with the reason, so the composer says why the controls are not
// there instead of silently having none. Why is for logs and is never sent to
// a member.
func ChattoneNotProvisioned(why string) *ChattoneService {
	return &ChattoneService{Unprovisioned: why}
}

// WritingStylesNote is the reason WritingStylesEnabled is false for the caller,
// or "" when the controls are on.
func (s *ChattoneService) WritingStylesNote(ctx context.Context) string {
	if s == nil || s.Unprovisioned != "" || s.Rewrite == nil || s.Rewrite.Registry == nil {
		return ChattoneNoteNotQualified
	}
	if s.WritingStylesEnabled(ctx) {
		return ""
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return ChattoneNoteUnavailable
	}
	if ready, ok := s.Rewrite.Model.(interface{ Ready() bool }); ok && !ready.Ready() {
		return ChattoneNoteNotQualified
	}
	if _, enabled := s.Rewrite.Registry.Styles(p.Tenant().String()); !enabled {
		return ChattoneNoteWorkspaceOff
	}
	return ChattoneNoteUnavailable
}

// chattoneFeatureNote is the features endpoint's reason the controls are off:
// "" when they are on, and "" when no service exists at all (the composition
// root puts ChattoneNotProvisioned there when it has a reason to give).
func chattoneFeatureNote(ctx context.Context, surface ChattoneSurface) string {
	if isNilPersonaOutputPort(surface) {
		return ""
	}
	if noted, ok := surface.(interface {
		WritingStylesNote(context.Context) string
	}); ok {
		return noted.WritingStylesNote(ctx)
	}
	return ""
}

// chattoneFeatureOn is the features endpoint's answer for the writing-style
// controls. A surface that can answer per workspace (ChattoneService) does; any
// other composed surface is simply on.
func chattoneFeatureOn(ctx context.Context, surface ChattoneSurface) bool {
	if isNilPersonaOutputPort(surface) {
		return false
	}
	if gated, ok := surface.(interface {
		WritingStylesEnabled(context.Context) bool
	}); ok {
		return gated.WritingStylesEnabled(ctx)
	}
	return true
}

// Ready reports whether a qualified model is bound behind the gateway model.
func (m ChattoneGatewayModel) Ready() bool {
	if m.Gateway == nil || m.Binding == nil {
		return false
	}
	if ready, ok := m.Binding.(interface{ Ready() bool }); ok {
		return ready.Ready()
	}
	return true
}

// ChattoneEnableTenant turns the controls on for one workspace with the
// default styles unless it already has a setting (an administrator's choice,
// including "off", is never overwritten). The composition root calls it for the
// local development tenant only.
func ChattoneEnableTenant(registry *chatrewrite.Registry, tenant string) error {
	if registry == nil {
		return chatrewrite.ErrInvalid
	}
	if registry.Configured(tenant) {
		return nil
	}
	return registry.Configure(tenant, true, chatrewrite.DefaultStyles())
}
