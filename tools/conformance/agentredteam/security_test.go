package agentredteam

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

// TestTodo_AGENT2_023_Security proves exact approval binding and minimum-
// necessary egress at the underlying security boundaries.
func TestTodo_AGENT2_023_Security(t *testing.T) {
	fixture := loadFixture(t)
	approvalCase := fixture.Cases[7]
	args := []agentsecurity.WriteArgument{{Name: "subject", Value: "worker:p1", Role: agentsecurity.WriteSubject, Taint: []agentsecurity.TaintLabel{agentsecurity.TaintExternal}, Citations: []agentsecurity.Citation{{SourceID: approvalCase.ID, Location: "fixture:1", Digest: contentDigest(approvalCase.Content)}}}}
	card, err := agentsecurity.BuildWriteApprovalCard(args)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]agentsecurity.WriteArgument(nil), args...)
	changed[0].Value = "worker:p2"
	if _, err := agentsecurity.BindWriteArguments(context.Background(), agentsecurity.TierExternalWrite, changed, &card, ownerAuthorizer{allowed: map[string]bool{"worker:p2": true}}); refusalCode(t, err) != agentsecurity.RefusalArgsDigest {
		t.Fatalf("changed approved value error = %v", err)
	}
	if _, err := agentsecurity.BindWriteArguments(context.Background(), agentsecurity.TierExternalWrite, args, nil, ownerAuthorizer{allowed: map[string]bool{"worker:p1": true}}); refusalCode(t, err) != agentsecurity.RefusalEffectClass {
		t.Fatalf("missing T4 approval error = %v", err)
	}

	trust, err := outbound.NewPolicy(outbound.Destination{Name: "external.crm", TrustBundleRef: "bundle:external:v1", Purposes: []string{"agent.lookup"}, DataClasses: []string{string(trustdlp.ClassPublic)}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := trustdlp.NewPolicy(trust, trustdlp.Clearance{Destination: "external.crm", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}, Decision: trustdlp.Allow})
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := trustdlp.NewInspector()
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := agentegress.NewEvaluator(policy, inspector, trustdlp.NewReceiptLog())
	if err != nil {
		t.Fatal(err)
	}
	request := agentegress.OutboundRequest{
		TaskID: "task-redteam", Tenant: "tenant-a", Principal: "user:u1", Purpose: "agent.lookup",
		Profile: agentegress.Profile{ID: "external.crm", Kind: agentegress.TargetConnection,
			AllowedRegions: []string{"us-east"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic},
			Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone}},
		Region: "us-east", DeclaredFields: []string{"salary"},
		Fields: []agentegress.Field{{Name: "salary", Value: 125000, Class: trustdlp.ClassCompensation, Taint: []string{"USER_DATA"}, Provenance: []string{"hcm:people"}}},
		Task:   agentegress.TaskPolicy{AllowedRegions: []string{"us-east"}, AllowedResultClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, ResultRetention: time.Hour},
		Now:    time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}
	decision, err := evaluator.EvaluateOutbound(request)
	if refusalCodeEgress(t, err) != agentegress.RefusalClass || decision.Allowed || len(decision.Payload) != 0 || len(evaluator.Receipts()) != 0 {
		t.Fatalf("protected egress decision = %+v, err=%v, receipts=%d", decision, err, len(evaluator.Receipts()))
	}
}

func refusalCodeEgress(t *testing.T, err error) agentegress.RefusalCode {
	t.Helper()
	var refusal *agentegress.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("egress error = %v, want *agentegress.Refusal", err)
	}
	return refusal.Code
}
