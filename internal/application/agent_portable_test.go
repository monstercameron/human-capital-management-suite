package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type portableManifestSourceFake struct{ manifest agentmanifest.Manifest }

func (f portableManifestSourceFake) ResolveAgentManifest(context.Context, string, uint64) (agentmanifest.Manifest, error) {
	return f.manifest, nil
}

type portableInstructionSourceFake struct{ text string }

func (f portableInstructionSourceFake) ResolveAgentInstructions(context.Context, string, uint64, string) (string, error) {
	return f.text, nil
}

type portableAuthorizerFake struct{ err error }

func (f portableAuthorizerFake) AuthorizePortable(context.Context, string) error { return f.err }

func TestAgentPortableExportRejectsInstructionDigestMismatch(t *testing.T) {
	m := portableManifestForApplication("Answer approved questions.")
	s := &AgentPortableService{Manifests: portableManifestSourceFake{m}, Instructions: portableInstructionSourceFake{"tampered"}, Authorizer: portableAuthorizerFake{}}
	ctx := trust.WithPrincipal(context.Background(), portableApplicationPrincipal(t))
	if _, err := s.Export(ctx, AgentPortableExportRequest{ManifestID: m.ID, ManifestVersion: m.Version}); !errors.Is(err, ErrAgentPortableInvalid) {
		t.Fatalf("Export error = %v, want digest rejection", err)
	}
}

func TestAgentPortableInstructionDigestMatchesExactBytes(t *testing.T) {
	text := "Answer approved questions."
	m := portableManifestForApplication(text)
	if !portableInstructionDigestMatches(text, m.InstructionsDigest) || portableInstructionDigestMatches(text+" ", m.InstructionsDigest) {
		t.Fatal("instruction digest check did not bind exact bytes")
	}
}

func portableApplicationPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "owner-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "credential-a"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func portableManifestForApplication(instructions string) agentmanifest.Manifest {
	digest := portableInstructionDigest(instructions)
	ref := func(id string, n byte) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat(string(n), 64)}
	}
	return agentmanifest.Manifest{SchemaVersion: 1, ID: "agent.policy", Version: 1, OwnerID: "owner-a", Purpose: "answer policy questions", InstructionsDigest: digest, SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: ref("model", 'a'), AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxConcurrentRuns: 1}, OutputSchema: ref("output", 'b'), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{ref("eval", 'c')}}
}

func portableInstructionDigest(content string) string {
	// Reuse the portable package's validation by creating a tiny exported
	// definition is unnecessary here; the service's test only needs a valid
	// manifest digest, which is computed by the same SHA-256 convention.
	// Keep this helper local so the test does not depend on store internals.
	return "sha256:" + hexDigest(content)
}

func hexDigest(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}
