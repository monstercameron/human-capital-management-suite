package trust

import (
	"context"
	"fmt"
	"strings"
)

// Identity-only token enforcement for RBAC-RT-010.
//
// An identity-only token carries authentication identity alone: subject,
// tenant, session and authenticated assurance. Roles, organization scope,
// purposes, authority references and delegation references are resolved on
// the server from durable assignments, the organization closure, the JIT
// grant store and delegation records -- never from the token. Anything that
// mints or accepts authority outside the issuer is refused with
// [ErrCallerSelectedAuthority].
//
// IssueIdentity and VerifyIdentity are additive: the legacy Issue/Verify
// path is unchanged so existing callers keep working until they migrate.

// IsIdentityOnly reports whether c carries authentication identity only: no
// organization scope, roles, authority references, purposes or delegation
// references. Any present entry, even an empty string, counts as authority.
func (c Claims) IsIdentityOnly() bool {
	return len(c.identityViolations()) == 0
}

// identityViolations names the wire fields of c that carry caller-selected
// authority, in Claims struct order. Names only: values are never rendered
// into errors.
func (c Claims) identityViolations() []string {
	var fields []string
	if strings.TrimSpace(c.OrganizationScopeID) != "" {
		fields = append(fields, "org_scope")
	}
	if len(c.Roles) != 0 {
		fields = append(fields, "roles")
	}
	if len(c.AuthorityRefs) != 0 {
		fields = append(fields, "authority_refs")
	}
	if len(c.Purposes) != 0 {
		fields = append(fields, "purposes")
	}
	if len(c.DelegationRefs) != 0 {
		fields = append(fields, "delegation_refs")
	}
	return fields
}

// IssueIdentity signs claims only when they carry authentication identity
// alone. Claims naming any authority are refused before signing, so this
// issuer cannot mint authority outside itself.
func (v *HMACVerifier) IssueIdentity(claims Claims) (string, error) {
	if fields := claims.identityViolations(); len(fields) > 0 {
		return "", fmt.Errorf("%w: identity-only token must not carry %s", ErrCallerSelectedAuthority, strings.Join(fields, ", "))
	}
	return v.Issue(claims)
}

// VerifyIdentity verifies cred exactly like Verify and then refuses any
// token that carries caller-selected authority. The returned principal of a
// successful call holds subject, tenant, session and assurance only; every
// authorization input is empty and must be resolved server-side.
func (v *HMACVerifier) VerifyIdentity(ctx context.Context, cred Credential) (*Principal, error) {
	p, err := v.Verify(ctx, cred)
	if err != nil {
		return nil, err
	}
	var fields []string
	if strings.TrimSpace(p.organizationScopeID) != "" {
		fields = append(fields, "org_scope")
	}
	if len(p.roles) != 0 {
		fields = append(fields, "roles")
	}
	if len(p.authorityRefs) != 0 {
		fields = append(fields, "authority_refs")
	}
	if len(p.purposes) != 0 {
		fields = append(fields, "purposes")
	}
	if len(p.delegationRefs) != 0 {
		fields = append(fields, "delegation_refs")
	}
	if len(fields) != 0 {
		return nil, fmt.Errorf("%w: identity-only token must not carry %s", ErrCallerSelectedAuthority, strings.Join(fields, ", "))
	}
	return p, nil
}
