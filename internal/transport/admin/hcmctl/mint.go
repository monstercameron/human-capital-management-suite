package hcmctl

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
)

// mintToken mints a short-lived identity-only development Bearer [REDACTED] via
// internal/trust.HMACVerifier.IssueIdentity for the JIT (-mint) flow. This is
// authentication plumbing this codebase already owns, not business logic:
// hcmctl invents no token format and no claim semantics of its own.
//
// The minted credential carries authentication identity alone -- subject,
// tenant, session and the substantial assurance a loopback development mint
// actually achieves. It never carries roles, organization scope, purposes or
// delegation references: this issuer cannot mint authority outside itself,
// and elevated operator access is issued server-side through approved JIT
// grants only, never through a flag on this command. Development minting is
// refused unless the local development profile is selected and the target
// cell is loopback.
func mintToken(g globalFlags) (string, error) {
	if g.mintProfile != devprofile.Name {
		return "", fmt.Errorf("hcmctl: -mint is a development convenience refused outside the %q profile", devprofile.Name)
	}
	if !devprofile.IsLoopbackAddress(g.addr) {
		return "", fmt.Errorf("hcmctl: -mint requires a loopback -addr; %q is not one", g.addr)
	}
	if g.signingKey == "" {
		return "", fmt.Errorf("hcmctl: -mint requires -mint-key")
	}
	if g.issuer == "" || g.audience == "" || g.tenant == "" || g.subject == "" {
		return "", fmt.Errorf("hcmctl: -mint requires -mint-issuer, -mint-audience, -mint-tenant and -mint-subject")
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(g.signingKey),
		Issuer:   g.issuer,
		Audience: g.audience,
	})
	if err != nil {
		return "", fmt.Errorf("hcmctl: configuring the JIT signer: %w", err)
	}

	now := time.Now()
	token, err := verifier.IssueIdentity(trust.Claims{
		Issuer:               g.issuer,
		Audience:             g.audience,
		Subject:              g.subject,
		SubjectKind:          "human",
		Tenant:               g.tenant,
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "hcmctl-jit-session",
		IssuedAtUnix:         now.Unix(),
		ExpiresAtUnix:        now.Add(g.ttl).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("hcmctl: minting the JIT credential: %w", err)
	}
	return token, nil
}
