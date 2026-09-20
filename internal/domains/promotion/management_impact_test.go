package promotion

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestEvaluateManagementImpact proves the standalone read surface answers
// exactly what the preflight answers for the same selection.
func TestEvaluateManagementImpact(t *testing.T) {
	ctx := context.Background()
	tenant := promoux005Tenant()
	subject := promoux005Worker(t, promoux005Subject)
	reader := promoux005OrgFacts{
		exists:    map[string]bool{promoux005Subject: true, promoux005ManagerB: true, promoux005ManagerC: true, promoux005ManagerM: true},
		managerOf: map[string]string{promoux005Subject: promoux005ManagerB, promoux005ManagerB: promoux005ManagerC},
	}

	t.Run("safe selection is evaluated and certified", func(t *testing.T) {
		impact, findings, err := EvaluateManagementImpact(ctx, tenant, subject, reader, TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerB), AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
		})
		if err != nil || len(findings) != 0 {
			t.Fatalf("EvaluateManagementImpact = %v, %v; want no findings and no error", findings, err)
		}
		if !impact.Evaluated() || !impact.CycleSafe || impact.TargetManager.Id != promoux005ManagerB {
			t.Fatalf("impact = %+v, want an evaluated, cycle-safe impact naming B", impact)
		}
	})

	t.Run("same answer as the preflight for a cycle", func(t *testing.T) {
		sel := TargetManagerSelection{
			Reference:             promoux005Worker(t, promoux005ManagerM),
			AffectedDirectReports: []values.EntityRef{promoux005Worker(t, promoux005ManagerC)},
			AsOf:                  promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
		}
		impact, findings, err := EvaluateManagementImpact(ctx, tenant, subject, reader, sel)
		if err != nil {
			t.Fatal(err)
		}
		wantFindings, wantImpact, err := evaluateTargetManagerSelection(ctx, PreflightRequest{
			Tenant: tenant, Subject: subject, ManagerFacts: reader, TargetManagerSelection: &sel,
		})
		if err != nil {
			t.Fatal(err)
		}
		if impact.CycleSafe || len(findings) != 1 || findings[0].Code != CodeManagerRelationshipCycle {
			t.Fatalf("impact = %+v findings = %+v, want a refused cycle", impact, findings)
		}
		if string(impact.Canonical()) != string(wantImpact.Canonical()) || findings[0].Code != wantFindings[0].Code || findings[0].Message != wantFindings[0].Message {
			t.Fatalf("standalone answer diverged from the preflight's: %+v vs %+v", impact, wantImpact)
		}
	})

	t.Run("unauthorized candidate is not evaluated", func(t *testing.T) {
		impact, findings, err := EvaluateManagementImpact(ctx, tenant, subject, reader, TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerB), AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(false),
		})
		if err != nil {
			t.Fatal(err)
		}
		if impact.Evaluated() || len(findings) != 1 || findings[0].Code != CodeTargetManagerNotFound {
			t.Fatalf("impact = %+v findings = %+v, want no disclosed impact and the not-found finding", impact, findings)
		}
	})

	t.Run("a candidate two or more levels below the top is evaluated, not a contract failure", func(t *testing.T) {
		// B reports to C reports to D. A one-hop visibility bound refused
		// this with org.ErrDepthExceeded for every such candidate.
		deep := promoux005OrgFacts{
			exists:    map[string]bool{promoux005Subject: true, promoux005ManagerB: true, promoux005ManagerC: true, promoux005WorkerD: true},
			managerOf: map[string]string{promoux005Subject: promoux005ManagerB, promoux005ManagerB: promoux005ManagerC, promoux005ManagerC: promoux005WorkerD},
		}
		impact, findings, err := EvaluateManagementImpact(ctx, tenant, subject, deep, TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerB), AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
		})
		if err != nil || len(findings) != 0 || !impact.Evaluated() || !impact.CycleSafe {
			t.Fatalf("EvaluateManagementImpact = %+v, %+v, %v; want a certified impact", impact, findings, err)
		}
		// A chain deeper than the declared bound cannot be certified: the
		// unresolved finding, not an error and not a safe verdict.
		impact, findings, err = EvaluateManagementImpact(ctx, tenant, subject, deep, TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerB), AsOf: promoux005AsOf, ChainDepth: 1, Authorize: promoux005Authorize(true),
		})
		if err != nil || impact.Evaluated() || len(findings) != 1 || findings[0].Code != CodeManagerChainUnresolved {
			t.Fatalf("shallow bound = %+v, %+v, %v; want only the unresolved finding", impact, findings, err)
		}
	})

	t.Run("contract failures are errors", func(t *testing.T) {
		if _, _, err := EvaluateManagementImpact(ctx, tenant, values.EntityRef{}, reader, TargetManagerSelection{}); !errors.Is(err, ErrRequestInvalid) {
			t.Fatalf("invalid subject err = %v, want ErrRequestInvalid", err)
		}
		if _, _, err := EvaluateManagementImpact(ctx, tenant, subject, nil, TargetManagerSelection{
			Reference: promoux005Worker(t, promoux005ManagerB), AsOf: promoux005AsOf, ChainDepth: 10, Authorize: promoux005Authorize(true),
		}); !errors.Is(err, ErrRequestInvalid) {
			t.Fatalf("nil reader err = %v, want ErrRequestInvalid", err)
		}
	})
}
