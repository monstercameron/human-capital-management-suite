package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

var ErrOpenAICredentialScope = errors.New("application: OpenAI credential scope refused")

// OpenAIModelCredentialScope explicitly delegates a configured credential to
// one tenant, processing region, purpose and model profile. Wildcards are refused.
type OpenAIModelCredentialScope struct {
	TenantID    string
	Region      string
	Purpose     string
	Destination string
}

// OpenAIModelCredentialConfig declares the opaque credential version supplied
// to the provider adapter at process initialization. This authority has no secret
// getter and does not retain the provider key.
type OpenAIModelCredentialConfig struct {
	CredentialID string
	Version      string
	Workload     string
	Scopes       []OpenAIModelCredentialScope
	MaxTTL       time.Duration
	Now          func() time.Time
}

type OpenAIModelCredentialAuthority struct{ cfg OpenAIModelCredentialConfig }

func NewOpenAIModelCredentialAuthority(cfg OpenAIModelCredentialConfig) (*OpenAIModelCredentialAuthority, error) {
	if !canonicalOpenAIValue(cfg.CredentialID) || !canonicalOpenAIValue(cfg.Version) || !canonicalOpenAIValue(cfg.Workload) || len(cfg.Scopes) == 0 || cfg.MaxTTL <= 0 || cfg.Now == nil {
		return nil, ErrOpenAICredentialScope
	}
	for _, scope := range cfg.Scopes {
		if !canonicalOpenAIValue(scope.TenantID) || !canonicalOpenAIValue(scope.Region) || !canonicalOpenAIValue(scope.Purpose) || !canonicalOpenAIValue(scope.Destination) {
			return nil, ErrOpenAICredentialScope
		}
	}
	cfg.Scopes = slices.Clone(cfg.Scopes)
	return &OpenAIModelCredentialAuthority{cfg: cfg}, nil
}

func canonicalOpenAIValue(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.Contains(value, "*")
}

func (a *OpenAIModelCredentialAuthority) ResolveModelCredential(ctx context.Context, request ModelCredentialRequest) (custody.Handle, error) {
	if a == nil || ctx == nil || request.ProviderID != "openai" || !canonicalOpenAIValue(request.TaskID) || !canonicalOpenAIValue(request.AgentID) {
		return custody.Handle{}, ErrOpenAICredentialScope
	}
	if err := ctx.Err(); err != nil {
		return custody.Handle{}, err
	}
	if !a.allowed(request.TenantID, request.Region, request.Purpose, request.Destination) {
		return custody.Handle{}, ErrOpenAICredentialScope
	}
	return custody.Handle{ID: a.cfg.CredentialID, Version: a.cfg.Version, Kind: custody.Secret, Tenant: request.TenantID, Region: request.Region}, nil
}

// IssueLease implements the custody lease port for the private process-owned
// credential. lease.Manager adds destination binding, tamper detection,
// expiration, revocation, replay protection and evidence before any provider call.
func (a *OpenAIModelCredentialAuthority) IssueLease(ctx custody.Context, handle custody.Handle, operation custody.Operation, ttl time.Duration) (custody.Lease, error) {
	if a == nil || ctx.RequestContext.Validate() != nil || handle.Validate() != nil || handle.ID != a.cfg.CredentialID || handle.Version != a.cfg.Version || handle.Kind != custody.Secret || ctx.Workload != a.cfg.Workload || ctx.Tenant != handle.Tenant || ctx.Region != handle.Region || operation != custody.Decrypt || ttl <= 0 || ttl > a.cfg.MaxTTL || !a.allowed(ctx.Tenant, ctx.Region, ctx.Purpose, ctx.Destination) {
		return custody.Lease{}, ErrOpenAICredentialScope
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return custody.Lease{}, ErrOpenAICredentialScope
	}
	return custody.Lease{ID: hex.EncodeToString(random[:]), Handle: handle, Operation: operation, ExpiresAt: a.cfg.Now().UTC().Add(ttl), ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func (a *OpenAIModelCredentialAuthority) allowed(tenant, region, purpose, destination string) bool {
	return slices.Contains(a.cfg.Scopes, OpenAIModelCredentialScope{TenantID: tenant, Region: region, Purpose: purpose, Destination: destination})
}
