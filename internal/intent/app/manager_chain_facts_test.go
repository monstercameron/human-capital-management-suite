package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// chainLocator resolves a fixed reporting line: linh -> rafael -> darius ->
// board sentinel, plus a two-worker cycle a <-> b.
func chainLocator(fail bool) WorkerLocator {
	known := time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)
	row := func(key, manager string) *workforce.WorkerRow {
		return &workforce.WorkerRow{WorkerID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)), WorkerKey: key, ManagerRelationshipRef: manager,
			EffectiveFrom: "2026-01-01", KnownAt: known, RecordedAt: known}
	}
	rows := map[string]*workforce.WorkerRow{
		"linh": row("linh", "rafael"), "rafael": row("rafael", "darius"), "darius": row("darius", "board:sentinel"),
		"cycle-a": row("cycle-a", "cycle-b"), "cycle-b": row("cycle-b", "cycle-a"),
		"undated": {WorkerID: uuid.New(), WorkerKey: "undated", ManagerRelationshipRef: "rafael", EffectiveFrom: "not-a-date", KnownAt: known, RecordedAt: known},
	}
	byID := map[string]*workforce.WorkerRow{}
	for _, r := range rows {
		byID[r.WorkerID.String()] = r
	}
	return func(_ context.Context, tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
		if fail {
			return WorkerLocation{}, false, errors.New("directory offline")
		}
		r, ok := rows[ref]
		if !ok {
			r, ok = byID[ref]
		}
		if !ok {
			return WorkerLocation{Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: ref}, Key: ref}, true, nil
		}
		return WorkerLocation{Ref: values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: r.WorkerID.String()}, Key: r.WorkerKey, Created: r}, true, nil
	}
}

func chainPrincipal(t *testing.T, subject string, roles ...string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: subject, SubjectKind: trust.SubjectKindHuman, Roles: roles,
		Purposes: []string{"compensation_review"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "chain-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// TestManagerChainFactsEstablishOnlyTheReportingLine proves the manager-chain
// facts PROMOUX-015 supplies to authz: a direct and an indirect manager each
// get a MANAGER_CHAIN fact authz accepts, and nobody else does.
func TestManagerChainFactsEstablishOnlyTheReportingLine(t *testing.T) {
	ctx := context.Background()
	locate := chainLocator(false)
	subject, _, _ := locate(ctx, "acme-corp", "linh")
	at := values.NewInstant(time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC))

	for _, manager := range []string{"rafael", "darius"} {
		principal := chainPrincipal(t, manager, string(authz.RoleManager))
		facts := managerChainFacts(ctx, locate, principal, subject.Ref)
		if len(facts) != 1 || facts[0].Kind != authz.RelationshipManagerChain || facts[0].Subject != subject.Ref || facts[0].Validate() != nil {
			t.Fatalf("managerChainFacts(%s) = %+v, want one valid MANAGER_CHAIN fact over linh", manager, facts)
		}
		scope, err := authz.ResolveAuthorizationScope(principal, authz.ScopeInput{Subject: subject.Ref, EffectiveAt: at, Relationships: facts})
		if err != nil || scope.Effect != authz.EffectAllow || scope.Relationship != authz.RelationshipManagerChain {
			t.Fatalf("authz scope for %s = %+v, %v, want ALLOW via MANAGER_CHAIN", manager, scope, err)
		}
	}

	refused := []struct {
		name      string
		principal *trust.Principal
		subject   string
		locate    WorkerLocator
	}{
		{"a report of the subject", chainPrincipal(t, "linh", string(authz.RoleManager)), "rafael", locate},
		{"the manager without the manager role", chainPrincipal(t, "rafael", "worker_self"), "linh", locate},
		{"an unrelated manager", chainPrincipal(t, "thomas", string(authz.RoleManager)), "linh", locate},
		{"a reporting cycle", chainPrincipal(t, "nobody", string(authz.RoleManager)), "cycle-a", locate},
		{"a corpus subject with no created row", chainPrincipal(t, "rafael", string(authz.RoleManager)), "jane-doe", locate},
		{"an unreadable effective date", chainPrincipal(t, "rafael", string(authz.RoleManager)), "undated", locate},
		{"a locator failure", chainPrincipal(t, "rafael", string(authz.RoleManager)), "linh", chainLocator(true)},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			ref := values.EntityRef{Tenant: "acme-corp", Kind: people.KindWorker, Id: tc.subject}
			if loc, ok, err := locate(ctx, "acme-corp", tc.subject); err == nil && ok {
				ref = loc.Ref
			}
			if facts := managerChainFacts(ctx, tc.locate, tc.principal, ref); len(facts) != 0 {
				t.Fatalf("managerChainFacts = %+v, want none", facts)
			}
		})
	}
	if facts := managerChainFacts(ctx, nil, chainPrincipal(t, "rafael", string(authz.RoleManager)), subject.Ref); facts != nil {
		t.Fatalf("managerChainFacts with no locator = %+v, want none", facts)
	}
}
