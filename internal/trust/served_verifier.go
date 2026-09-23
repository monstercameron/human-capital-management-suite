package trust

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// ServedVerifier is the INTAPI-001 credential verifier the cell serves
// with: machine JWT access tokens verify against the rotating server key
// set, and the development HMAC verifier applies only when the composition
// supplies one -- which serve does solely for the local development
// profile. Outside local-dev there is no HMAC fallback at all, so an HMAC
// development token presented to a production listener is refused.
//
// A verified machine principal carries identity only (subject, tenant,
// session, assurance as an integration subject); roles, scopes, purposes
// and delegations resolve on the server.
type ServedVerifier struct {
	Machine *machine.Verifier
	Request machine.VerifyRequest
	// Revocations admits one machine-token use: revoked clients and
	// sessions fail closed and a token identifier is accepted once per
	// lifetime. Nil admits without revocation information, which only the
	// development path uses.
	Revocations RevocationSource
	// Dev is the development HMAC verifier, or nil to refuse HMAC tokens.
	Dev *HMACVerifier
	Now func() time.Time
}

var _ Verifier = (*ServedVerifier)(nil)

// Verify implements [Verifier].
func (v *ServedVerifier) Verify(ctx context.Context, cred Credential) (*Principal, error) {
	if v == nil || v.Machine == nil {
		return nil, ErrInvalidCredential
	}
	if claims, err := v.Machine.Verify(cred.Token, v.Request); err == nil {
		if v.Revocations != nil {
			if err := v.Revocations.CheckRevocation(ctx, RevocationQuery{
				Tenant: claims.Tenant, ClientID: claims.Client, TokenID: claims.TokenID,
				Session: claims.Session, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(),
			}); err != nil {
				return nil, err
			}
		}
		return machinePrincipal(cred.Token, claims)
	} else if v.Dev == nil {
		return nil, err
	}
	return v.Dev.VerifyIdentity(ctx, cred)
}

func machinePrincipal(token string, claims machine.Claims) (*Principal, error) {
	assurance, ok := parseAssurance(claims.Assurance)
	if !ok {
		return nil, ErrMissingAssurance
	}
	return NewPrincipal(PrincipalSpec{
		Tenant:               values.TenantId(claims.Tenant),
		Subject:              claims.Subject,
		SubjectKind:          SubjectKindIntegration,
		AuthenticationMethod: AuthenticationMethodBearerToken,
		Assurance:            assurance,
		SessionRef:           claims.Session,
		Confirmation:         stringConfirmation(claims.Confirmation),
		IssuedAt:             time.Unix(claims.IssuedAt, 0).UTC(),
		ExpiresAt:            time.Unix(claims.ExpiresAt, 0).UTC(),
		CredentialDigest:     credentialDigest(token),
	})
}

// stringConfirmation projects the token's confirmation coordinates onto
// the principal's string map. Non-string coordinates are dropped rather
// than rendered: the principal carries coordinates, never values worth
// parsing.
func stringConfirmation(in map[string]any) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}
	return out
}
