package agentcandidateevalstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_AGENTP_021_CandidateJournalIntegration(t *testing.T) {
	database := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, database.SQL); err != nil {
		t.Fatalf("migrate candidate journal: %v", err)
	}
	production, synthetic := uuid.New(), uuid.New()
	for _, tenant := range []uuid.UUID{production, synthetic} {
		if _, err := database.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant); err != nil {
			t.Fatal(err)
		}
	}
	connection := database.NewConn(t)
	if _, err := connection.Exec(ctx, "SET ROLE hcmnext_agent_eval_runtime"); err != nil {
		t.Fatal(err)
	}
	journal, err := New(connection)
	if err != nil {
		t.Fatal(err)
	}
	digestValue := "sha256:" + strings.Repeat("a", 64)
	record := Record{Target: agenteval.PersonaEvaluationTarget{TenantID: production.String(), SyntheticTenantID: synthetic.String(), PersonaID: "policy", PersonaVersion: 1, ProfileDigest: digestValue, ModelDigest: digestValue, InvokerID: "invoker"},
		CaseDigest: digestValue, RequestDigest: digestValue, InvocationID: "invocation", TaskID: "refused-admission", AdmissionDigest: digestValue,
		Outcome: "REFUSED", RefusalCode: "OUT_OF_SCOPE", RefusalPointer: "/agents/policy", CompletedAt: time.Now().UTC()}
	if err := journal.Append(ctx, record); err != nil {
		t.Fatalf("retain runtime checkpoint: %v", err)
	}
	retained, err := journal.Read(ctx, record.Target, record.InvocationID)
	if err != nil || retained.EvidenceDigest == "" {
		t.Fatalf("read retained checkpoint: %+v %v", retained, err)
	}
	reopened, err := New(database.NewConn(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Read(ctx, record.Target, record.InvocationID)
	if err != nil || got.EvidenceDigest != retained.EvidenceDigest {
		t.Fatalf("restarted evaluator lost evidence: %+v %v", got, err)
	}
	call := ModelCall{Target: record.Target, TaskID: "measured-task", StepID: "measured-step", RequestDigest: digestValue, ResultDigest: digestValue, LeaseID: "single-use-lease", Provider: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-5-mini", Version: "gpt-5-mini-2025-08-07"}, Usage: agentmodel.ModelUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CostMicros: 65}, CompletedAt: time.Now().UTC()}
	if err := journal.AppendModelCall(ctx, call); err != nil {
		t.Fatalf("retain measured provider outcome: %v", err)
	}
	calls, err := reopened.ReadModelCalls(ctx, record.Target, call.TaskID)
	if err != nil || len(calls) != 1 || calls[0].Provider != call.Provider || calls[0].Usage != call.Usage || calls[0].EvidenceDigest == "" {
		t.Fatalf("restarted model evidence: %+v %v", calls, err)
	}
	if err := journal.AppendModelCall(ctx, call); err != nil {
		t.Fatalf("same observation replay: %v", err)
	}
	call.Usage.CostMicros++
	if err := journal.AppendModelCall(ctx, call); err == nil {
		t.Fatal("conflicting billed observation replaced the measured call")
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE persona_candidate_model_calls SET evidence_digest=$1 WHERE tenant_id=$2`, digestValue, synthetic); err == nil {
		t.Fatal("measured provider evidence was mutated")
	}
	other := record.Target
	other.SyntheticTenantID = production.String()
	if _, err := journal.Read(ctx, other, record.InvocationID); err == nil {
		t.Fatal("foreign tenant read candidate checkpoint")
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE persona_candidate_case_journal SET evidence_digest=$1 WHERE tenant_id=$2`, digestValue, synthetic); err == nil {
		t.Fatal("append-only checkpoint was mutated")
	}
	if _, err := connection.Exec(ctx, "SET ROLE hcmnext_agent_app"); err != nil {
		t.Fatal(err)
	}
	record.InvocationID = "forged-serving-evidence"
	if err := journal.Append(ctx, record); err == nil {
		t.Fatal("tenant serving role fabricated evaluation evidence")
	}
	call.StepID = "forged-serving-model"
	if err := journal.AppendModelCall(ctx, call); err == nil {
		t.Fatal("serving role fabricated billed provider evidence")
	}
}
