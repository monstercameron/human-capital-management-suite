package timeclockstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// IdentificationPolicySource resolves a published, tenant-scoped policy.
// The adapter intentionally requires injection because timestore has no
// published policy table yet.
type IdentificationPolicySource interface {
	IdentificationPolicy(context.Context, string, string, string) (clock.RateLimitPolicy, error)
}

// DeviceWorkerTokenIssuer mints the configured signed device token. No key
// or development signer is supplied by this package.
type DeviceWorkerTokenIssuer interface {
	IssueDeviceWorkerToken(context.Context, clockservice.DeviceWorkerTokenClaims) (clockservice.DeviceWorkerToken, error)
}

// CredentialLookup resolves badge and QR credentials atomically.
type CredentialLookup interface {
	LookupCredential(context.Context, string, string, string, string) (clockservice.CredentialRecord, error)
}

// SupervisorCredentialVerifier authenticates supervisor credentials.
type SupervisorCredentialVerifier interface {
	VerifySupervisorCredential(context.Context, string, string, string) (*trust.Principal, error)
}

// IdentificationAdapter adds injected identity dependencies to the durable
// credential adapter. Every dependency is explicit; absent dependencies fail
// closed through the application service's interface assertions.
type IdentificationAdapter struct {
	CredentialAdapter
	Policy     IdentificationPolicySource
	Token      DeviceWorkerTokenIssuer
	Lookup     CredentialLookup
	Supervisor SupervisorCredentialVerifier
}

var _ clockservice.IdentificationPolicySource = IdentificationAdapter{}
var _ clockservice.DeviceWorkerTokenIssuer = IdentificationAdapter{}
var _ clockservice.CredentialLookup = IdentificationAdapter{}
var _ clockservice.SupervisorCredentialVerifier = IdentificationAdapter{}

// IdentificationPolicy resolves the injected published rate-limit policy.
func (a IdentificationAdapter) IdentificationPolicy(ctx context.Context, tenant, site, device string) (clock.RateLimitPolicy, error) {
	if a.Policy == nil {
		return clock.RateLimitPolicy{}, clockservice.ErrUnavailable
	}
	return a.Policy.IdentificationPolicy(ctx, tenant, site, device)
}

// IssueDeviceWorkerToken delegates token signing to the configured issuer.
func (a IdentificationAdapter) IssueDeviceWorkerToken(ctx context.Context, claims clockservice.DeviceWorkerTokenClaims) (clockservice.DeviceWorkerToken, error) {
	if a.Token == nil {
		return clockservice.DeviceWorkerToken{}, clockservice.ErrUnavailable
	}
	return a.Token.IssueDeviceWorkerToken(ctx, claims)
}

// LookupCredential delegates badge/QR resolution to the configured source.
func (a IdentificationAdapter) LookupCredential(ctx context.Context, tenant, worker, kind, value string) (clockservice.CredentialRecord, error) {
	if a.Lookup == nil {
		return clockservice.CredentialRecord{}, clockservice.ErrUnavailable
	}
	return a.Lookup.LookupCredential(ctx, tenant, worker, kind, value)
}

// VerifySupervisorCredential delegates supervisor verification to the
// configured identity authority.
func (a IdentificationAdapter) VerifySupervisorCredential(ctx context.Context, tenant, device, ref string) (*trust.Principal, error) {
	if a.Supervisor == nil {
		return nil, clockservice.ErrUnavailable
	}
	return a.Supervisor.VerifySupervisorCredential(ctx, tenant, device, ref)
}
