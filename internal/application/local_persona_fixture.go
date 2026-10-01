package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// LocalPersonaFixtureProfile is the only serve profile that may install the
// deterministic persona fixture. The fixture is intentionally absent from the
// standard profile.
const LocalPersonaFixtureProfile = ServeProfileLocalDev

const (
	// LocalPersonaFixtureTenant is the seeded Ironridge tenant used by local
	// browser and Company Chat checks.
	LocalPersonaFixtureTenant = "ironridge-demo"
	// LocalPersonaFixtureUser is the seeded Ironridge test user.
	LocalPersonaFixtureUser = "ir-001-walt-brennan"
	// LocalPersonaFixtureConversation is the explicit Company Chat placement.
	LocalPersonaFixtureConversation = "company-chat"
	localPersonaFixtureID           = "hcmnext.local.persona.policy-helper"
	localPersonaFixtureOwner        = "local-dev-fixture-owner"
	localPersonaFixtureDigest       = "sha256:local-persona-policy-helper-v1"
	localPersonaFixtureModelProfile = "hcmnext.local.fake-model.v1"
)

var (
	// ErrLocalPersonaFixtureUnavailable marks an invalid or non-local fixture
	// installation request.
	ErrLocalPersonaFixtureUnavailable = errors.New("application: local persona fixture unavailable")
	// ErrLocalPersonaFixtureData marks a model request that tries to provide
	// company data or capabilities to this no-data fixture.
	ErrLocalPersonaFixtureData = errors.New("application: local persona fixture accepts no company data")
)

// LocalPersonaFixture is a reviewable, immutable local installation splice.
// It is a description for the later composition root; constructing it does
// not publish a persona or mutate the isolated agentpersona store.
type LocalPersonaFixture struct {
	PersonaID       string
	Version         uint64
	ContentDigest   string
	OwnerID         string
	TenantID        string
	InvokerID       string
	ConversationID  string
	ModelProfile    string
	TierCeiling     string
	DataClassesRead []string
	Published       bool
	Production      bool
}

// InstallLocalPersonaFixture returns the one explicitly supported local
// installation. Every identity is exact-match bound to the seeded Ironridge
// Company Chat scenario; all other profiles, tenants, users, channels and
// production-shaped requests fail closed.
func InstallLocalPersonaFixture(profile, tenant, user, conversation string) (LocalPersonaFixture, error) {
	if profile != LocalPersonaFixtureProfile || tenant != LocalPersonaFixtureTenant ||
		user != LocalPersonaFixtureUser || conversation != LocalPersonaFixtureConversation {
		return LocalPersonaFixture{}, fmt.Errorf("%w: profile, tenant, user or conversation is not the approved local tuple", ErrLocalPersonaFixtureUnavailable)
	}
	return LocalPersonaFixture{
		PersonaID: localPersonaFixtureID, Version: 1, ContentDigest: localPersonaFixtureDigest,
		OwnerID: localPersonaFixtureOwner, TenantID: tenant, InvokerID: user,
		ConversationID: conversation, ModelProfile: localPersonaFixtureModelProfile,
		TierCeiling: "T0", DataClassesRead: []string{}, Published: false, Production: false,
	}, nil
}

// LocalPersonaFixtureModel is a deterministic no-company-data model adapter.
// It never reads context references, invokes tools, or emits side effects.
type LocalPersonaFixtureModel struct{}

// Capabilities reports the deliberately minimal text-only adapter contract.
func (LocalPersonaFixtureModel) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{ContractVersions: []int{agentmodel.ContractVersion}, OutputModes: []agentmodel.OutputMode{agentmodel.OutputText}, MaxTools: 0}
}

// Invoke returns a fixed local response after validating that the request is
// text-only and contains no company context. It makes no network call.
func (LocalPersonaFixtureModel) Invoke(ctx context.Context, req agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if ctx == nil {
		return agentmodel.ModelResult{}, ErrLocalPersonaFixtureUnavailable
	}
	if err := agentmodel.ValidateModelRequest(req); err != nil {
		return agentmodel.ModelResult{}, err
	}
	if req.ModelProfile != localPersonaFixtureModelProfile || len(req.ContextRefs) != 0 || len(req.Tools) != 0 || req.Output.Mode != agentmodel.OutputText {
		return agentmodel.ModelResult{}, ErrLocalPersonaFixtureData
	}
	return agentmodel.ModelResult{
		ContractVersion: agentmodel.ContractVersion,
		Text:            "This local policy helper is ready. It has no access to company data; install a reviewed persona to answer tenant questions.",
		Finish:          agentmodel.FinishComplete,
		Provider:        agentmodel.ModelIdentity{ProviderID: "local", ModelID: "fixture", Version: "1"},
	}, nil
}

// LocalPersonaFixtureConstants exposes the stable IDs needed by a composition
// splice without exposing mutable package state or a store dependency.
func LocalPersonaFixtureConstants() (personaID, digest, ownerID, modelProfile string) {
	return localPersonaFixtureID, localPersonaFixtureDigest, localPersonaFixtureOwner, localPersonaFixtureModelProfile
}

// LocalPersonaFixtureReplyIsSafe is a small assertion helper for callers that
// need to reject accidental data-bearing fixture replies.
func LocalPersonaFixtureReplyIsSafe(text string) bool {
	return strings.TrimSpace(text) == "This local policy helper is ready. It has no access to company data; install a reviewed persona to answer tenant questions."
}
