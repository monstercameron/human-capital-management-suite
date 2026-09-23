package trust

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// identityOnlyClaimsForTest returns claims carrying authentication identity
// only: subject, tenant, session and assurance. No roles, organization scope,
// purposes, authority references or delegation references.
func identityOnlyClaimsForTest() Claims {
	return Claims{
		Issuer:               "issuer",
		Audience:             "audience",
		Subject:              "subject-1",
		SubjectKind:          "human",
		Tenant:               "tenant-a",
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-1",
		IssuedAtUnix:         100,
		ExpiresAtUnix:        200,
	}
}

// TestTodo_RBAC_RT_010 is the RBAC-RT-010 primary test.
//
// RED: trust.Claims carries roles, organization scope, purposes, authority
// references and delegation references that every check trusts, and the
// operator tool mints high-assurance tokens with caller-chosen roles.
//
// GREEN: the token carries subject, tenant, session and authenticated
// assurance only; roles, scopes, purposes and delegations are resolved on
// the server. IssueIdentity refuses to mint authority-bearing claims and
// VerifyIdentity refuses to verify them, while the legacy Issue/Verify path
// is unchanged so existing callers keep working until they migrate.
func TestTodo_RBAC_RT_010(t *testing.T) {
	v := hmacVerifierForTest(t, nil)
	ctx := context.Background()

	t.Run("identity claims are recognized as identity only", func(t *testing.T) {
		if !identityOnlyClaimsForTest().IsIdentityOnly() {
			t.Fatal("identity-only claims are not recognized as identity only")
		}
		for name, mutate := range map[string]func(*Claims){
			"roles":              func(c *Claims) { c.Roles = []string{"reader"} },
			"organization scope": func(c *Claims) { c.OrganizationScopeID = "org-a" },
			"purposes":           func(c *Claims) { c.Purposes = []string{"operations"} },
			"authority refs":     func(c *Claims) { c.AuthorityRefs = []string{"authority:position:vp"} },
			"delegation refs":    func(c *Claims) { c.DelegationRefs = []string{"delegation:9a12"} },
		} {
			c := identityOnlyClaimsForTest()
			mutate(&c)
			if c.IsIdentityOnly() {
				t.Errorf("claims carrying %s are recognized as identity only", name)
			}
		}
	})

	t.Run("identity token round-trips with no authority", func(t *testing.T) {
		token, err := v.IssueIdentity(identityOnlyClaimsForTest())
		if err != nil {
			t.Fatalf("IssueIdentity: %v", err)
		}
		p, err := v.VerifyIdentity(ctx, Credential{Token: token})
		if err != nil {
			t.Fatalf("VerifyIdentity: %v", err)
		}
		if p.Subject() != "subject-1" || p.Tenant() != "tenant-a" || p.SessionRef() != "session-1" {
			t.Fatalf("verified identity = %s", p)
		}
		if p.Assurance() != AssuranceSubstantial {
			t.Fatalf("assurance = %v, want substantial", p.Assurance())
		}
		if len(p.Roles()) != 0 || len(p.AuthorityRefs()) != 0 || len(p.Purposes()) != 0 ||
			len(p.DelegationRefs()) != 0 || p.OrganizationScopeID() != "" {
			t.Fatalf("identity principal carries authority: roles=%v purposes=%v org=%q authority=%v delegations=%v",
				p.Roles(), p.Purposes(), p.OrganizationScopeID(), p.AuthorityRefs(), p.DelegationRefs())
		}
	})

	t.Run("identity issuance refuses caller-selected authority", func(t *testing.T) {
		c := identityOnlyClaimsForTest()
		c.Roles = []string{"hcm_admin"}
		c.Purposes = []string{"operations"}
		if _, err := v.IssueIdentity(c); !errors.Is(err, ErrCallerSelectedAuthority) {
			t.Fatalf("IssueIdentity with authority = %v, want ErrCallerSelectedAuthority", err)
		}
	})

	t.Run("identity verification refuses an authority-bearing token", func(t *testing.T) {
		legacy, err := v.Issue(validClaimsForTest())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.VerifyIdentity(ctx, Credential{Token: legacy}); !errors.Is(err, ErrCallerSelectedAuthority) {
			t.Fatalf("VerifyIdentity with authority = %v, want ErrCallerSelectedAuthority", err)
		}
	})

	t.Run("legacy path is unchanged for migrating callers", func(t *testing.T) {
		legacy, err := v.Issue(validClaimsForTest())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(ctx, Credential{Token: legacy}); err != nil {
			t.Fatalf("legacy Verify rejects a legacy token: %v", err)
		}
	})
}

// TestIdentityVerifierAdapter proves the listener adapter admits exactly
// what VerifyIdentity admits: an identity-only token verifies through the
// [Verifier] interface, and an authority-bearing token is refused with
// [ErrCallerSelectedAuthority] before any handler could run.
func TestIdentityVerifierAdapter(t *testing.T) {
	v := hmacVerifierForTest(t, nil)
	ctx := context.Background()

	token, err := v.IssueIdentity(identityOnlyClaimsForTest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.IdentityVerifier().Verify(ctx, Credential{Token: token}); err != nil {
		t.Fatalf("identity token through adapter: %v", err)
	}

	c := identityOnlyClaimsForTest()
	c.Roles = []string{"hcm_admin"}
	legacy, err := v.Issue(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.IdentityVerifier().Verify(ctx, Credential{Token: legacy}); !errors.Is(err, ErrCallerSelectedAuthority) {
		t.Fatalf("authority token through adapter = %v, want ErrCallerSelectedAuthority", err)
	}
}

// TestTodo_RBAC_RT_010_Security proves the identity-only gate fails closed:
// tampered tokens are rejected as invalid credentials, authority smuggled
// through any single field is rejected as caller-selected authority, and the
// rejection names the offending field without echoing its value.
func TestTodo_RBAC_RT_010_Security(t *testing.T) {
	v := hmacVerifierForTest(t, nil)
	ctx := context.Background()

	t.Run("each authority field is rejected on its own", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*Claims)
			field  string
		}{
			{"roles", func(c *Claims) { c.Roles = []string{"hcm_admin"} }, "roles"},
			{"organization scope", func(c *Claims) { c.OrganizationScopeID = "org:other:unit" }, "org_scope"},
			{"purposes", func(c *Claims) { c.Purposes = []string{"payroll_run"} }, "purposes"},
			{"authority refs", func(c *Claims) { c.AuthorityRefs = []string{"authority:grant:1"} }, "authority_refs"},
			{"delegation refs", func(c *Claims) { c.DelegationRefs = []string{"delegation:ffff"} }, "delegation_refs"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := identityOnlyClaimsForTest()
				tc.mutate(&c)
				if _, err := v.IssueIdentity(c); !errors.Is(err, ErrCallerSelectedAuthority) {
					t.Fatalf("IssueIdentity = %v, want ErrCallerSelectedAuthority", err)
				}
				legacy, err := v.Issue(c)
				if err != nil {
					t.Fatal(err)
				}
				_, err = v.VerifyIdentity(ctx, Credential{Token: legacy})
				if !errors.Is(err, ErrCallerSelectedAuthority) {
					t.Fatalf("VerifyIdentity = %v, want ErrCallerSelectedAuthority", err)
				}
				if err == nil || !strings.Contains(err.Error(), tc.field) {
					t.Fatalf("rejection = %v, want it to name field %q", err, tc.field)
				}
			})
		}
	})

	t.Run("rejection does not echo the smuggled value", func(t *testing.T) {
		c := identityOnlyClaimsForTest()
		c.Roles = []string{"super-secret-role-value"}
		_, issueErr := v.IssueIdentity(c)
		if issueErr == nil || strings.Contains(issueErr.Error(), "super-secret-role-value") {
			t.Fatalf("rejection echoes the value: %v", issueErr)
		}
		legacy, err := v.Issue(c)
		if err != nil {
			t.Fatal(err)
		}
		_, verifyErr := v.VerifyIdentity(ctx, Credential{Token: legacy})
		if verifyErr == nil || strings.Contains(verifyErr.Error(), "super-secret-role-value") {
			t.Fatalf("rejection echoes the value: %v", verifyErr)
		}
	})

	t.Run("tampered identity token is an invalid credential", func(t *testing.T) {
		token, err := v.IssueIdentity(identityOnlyClaimsForTest())
		if err != nil {
			t.Fatal(err)
		}
		tampered := token[:len(token)-2] + "AA"
		if _, err := v.VerifyIdentity(ctx, Credential{Token: tampered}); !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("VerifyIdentity tampered = %v, want ErrInvalidCredential", err)
		}
	})

	t.Run("high assurance stays verifiable as authenticated assurance", func(t *testing.T) {
		c := identityOnlyClaimsForTest()
		c.Assurance = "high"
		token, err := v.IssueIdentity(c)
		if err != nil {
			t.Fatal(err)
		}
		p, err := v.VerifyIdentity(ctx, Credential{Token: token})
		if err != nil {
			t.Fatal(err)
		}
		if !p.Assurance().AtLeast(AssuranceSubstantial) {
			t.Fatalf("assurance = %v, want at least substantial", p.Assurance())
		}
	})
}

// TestTodo_RBAC_RT_010_Integration proves elevated operator access is
// mediated by an approved JIT grant, not by the token: the grant carries the
// role, capabilities and purpose while the token carries identity only, the
// two join on principal and tenant, and revoking the grant ends the elevation
// even though the token still verifies.
func TestTodo_RBAC_RT_010_Integration(t *testing.T) {
	now := time.Unix(400, 0).UTC()
	v, err := NewHMACVerifier(HMACVerifierConfig{
		Key:      []byte("01234567890123456789012345678901"),
		Issuer:   "issuer",
		Audience: "audience",
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	grant, err := jit.New("grant-1", jit.Request{
		Principal:     "subject-1",
		Tenant:        "tenant-a",
		Role:          jit.RoleIncidentResponder,
		TicketRef:     "INC-123",
		Justification: "production incident response",
		Capabilities:  []string{"diagnostics.inspect"},
		Purpose:       "incident_response",
		TTL:           time.Hour,
	}, jit.Approval{Approver: "approver-1", At: now}, now)
	if err != nil {
		t.Fatalf("jit.New: %v", err)
	}

	token, err := v.IssueIdentity(Claims{
		Issuer: "issuer", Audience: "audience", Subject: "subject-1", SubjectKind: "human",
		Tenant: "tenant-a", AuthenticationMethod: "bearer_token", Assurance: "high",
		SessionRef: "session-1", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("IssueIdentity: %v", err)
	}
	p, err := v.VerifyIdentity(ctx, Credential{Token: token})
	if err != nil {
		t.Fatalf("VerifyIdentity: %v", err)
	}

	// The join between grant authority and token identity is principal and
	// tenant. The token itself authorizes nothing: no roles, no purposes.
	if p.Subject() != grant.Principal || string(p.Tenant()) != string(grant.Tenant) {
		t.Fatalf("token identity %s/%s does not join grant %s/%s",
			p.Subject(), p.Tenant(), grant.Principal, grant.Tenant)
	}
	if len(p.Roles()) != 0 || len(p.Purposes()) != 0 {
		t.Fatalf("token carries authority alongside the grant: roles=%v purposes=%v", p.Roles(), p.Purposes())
	}
	if !grant.IsActive(now) {
		t.Fatal("approved grant is not active")
	}
	if err := grant.Use("diagnostics.inspect", now); err != nil {
		t.Fatalf("grant.Use: %v", err)
	}

	// Revoking the grant ends the elevation. The token still verifies as
	// identity -- revocation lives in the grant, exactly as RBAC-RT-010
	// requires -- so a second use is refused while verification succeeds.
	if err := grant.Revoke("approver-1", "incident closed", now); err != nil {
		t.Fatalf("grant.Revoke: %v", err)
	}
	if err := grant.Use("diagnostics.inspect", now); !errors.Is(err, jit.ErrGrantRevoked) {
		t.Fatalf("grant.Use after revoke = %v, want ErrGrantRevoked", err)
	}
	if _, err := v.VerifyIdentity(ctx, Credential{Token: token}); err != nil {
		t.Fatalf("identity verification must not depend on grant state: %v", err)
	}

	// A token minted for another tenant never joins this grant.
	other, err := v.IssueIdentity(Claims{
		Issuer: "issuer", Audience: "audience", Subject: "subject-1", SubjectKind: "human",
		Tenant: "tenant-b", AuthenticationMethod: "bearer_token", Assurance: "high",
		SessionRef: "session-1", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	op, err := v.VerifyIdentity(ctx, Credential{Token: other})
	if err != nil {
		t.Fatal(err)
	}
	if string(op.Tenant()) == string(grant.Tenant) {
		t.Fatal("other-tenant token joins the grant")
	}
}
