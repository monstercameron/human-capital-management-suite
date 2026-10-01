package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_WTIME004_TerminalWriterPostgresLedgerReplay(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant, instance := uuid.New(), uuid.New()
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	planDigest := "sha256:" + strings.Repeat("a", 64)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-clock','Clock terminal','ACTIVE',$3)`, tenant, "clock-terminal-"+tenant.String(), at.Add(-time.Hour))
	db.Exec(t, `INSERT INTO workflow_instance (tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,business_subject_refs,execution_mode,runtime_status,completion_dimensions,input_ref,variable_revision_head,current_node_ids,instance_version,correlation_id,created_at) VALUES ($1,$2,'cell-clock',$3,1,$4,$5,'EXECUTE','RUNNING','{}'::jsonb,$6,0,$7,4,$8,$9)`, tenant, instance, clockpunch.WorkflowID, planDigest, []string{"time_session:session-1"}, "sha256:"+strings.Repeat("b", 64), []string{"session_closed"}, "corr-clock", at)
	for _, node := range []string{clockpunch.NodeCommitPunch, clockpunch.NodeCommitClockOut} {
		nodeExecutionID := runtime.NodeExecutionID(tenant, instance, node, 1)
		db.Exec(t, `INSERT INTO workflow_node_execution (tenant_id,node_execution_id,instance_id,node_id,attempt,step_type,status,output_artifact_ref,capability_execution_id,effect_refs,trace_id,started_at,completed_at,recorded_at) VALUES ($1,$2,$3,$4,1,'CAPABILITY','SUCCEEDED',$5,$6,$7,$8,$9,$9,$9)`, tenant, nodeExecutionID, instance, node, "sha256:"+strings.Repeat("c", 64), nodeExecutionID, []string{"time_observation:obs-1", "time_session:session-1"}, "trace:"+node, at)
	}
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	writer := &ClockWorkflowTerminalWriter{Appender: ledgerport.NewAppender(registry), Ledger: PostgresClockWorkflowLedger{}, Proof: PostgresClockWorkflowTerminalProof{}}
	req := execute.TerminalWriteRequest{TenantID: tenant, InstanceID: instance, WorkflowID: clockpunch.WorkflowID, PlanDigest: planDigest, TerminalCode: "TIME_SESSION_CLOSED", EndNodeID: clockpunch.NodeClosed, EndOutputDigest: "sha256:" + strings.Repeat("d", 64), CorrelationID: "corr-clock", IdempotencyKey: "clock-terminal-1", RecordedAt: at, Source: runtime.StartSource{Kind: runtime.StartSourceTrigger}}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := writer.Write(ctx, tx, req)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if first.EventRef == "" {
		t.Fatal("missing event reference")
	}
	tx, err = db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := writer.Write(ctx, tx, req)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("replay identity=%+v, first=%+v", second, first)
	}
	var count int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id=$1 AND idempotency_key=$2`, tenant, req.IdempotencyKey).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("ledger event count=%d, want 1", count)
	}
	events, err := ledgerport.NewReader().ReadStream(ctx, db.Conn, tenant, "workflow:"+clockpunch.WorkflowID+":"+instance.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d, want 1", len(events))
	}
	if err := ledgerport.NewKernelDigester(registry).VerifyEvent(events[0]); err != nil {
		t.Fatalf("canonical digest verification: %v", err)
	}
	var got clockWorkflowOutcome
	if err := jsonUnmarshal(events[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.WorkflowID != clockpunch.WorkflowID || got.TerminalNodeID != clockpunch.NodeClosed || len(got.EffectReceiptRefs) != 4 {
		t.Fatalf("outcome=%+v", got)
	}
}

func TestClockWorkflowTerminalWriterWritesTypedOutcome(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	appender := &clockTerminalAppender{receipt: ledgerport.AppendReceipt{Tenant: tenant, StreamKey: "workflow:" + clockpunch.WorkflowID + ":" + instance.String(), Sequence: 1, EventID: uuid.New()}}
	prep := &clockTerminalPreparation{head: 0}
	w := ClockWorkflowTerminalWriter{Appender: appender, Ledger: prep, Proof: clockTerminalProof{refs: []string{"time_observation:in", "time_observation:out"}}}
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	identity, err := w.Write(context.Background(), fakeClockTx{}, execute.TerminalWriteRequest{
		TenantID: tenant, InstanceID: instance, WorkflowID: clockpunch.WorkflowID,
		PlanDigest: "sha256:clock-plan", TerminalCode: "TIME_SESSION_CLOSED", EndNodeID: "session_closed",
		EndOutputDigest: "sha256:end", CorrelationID: "corr", IdempotencyKey: "start-key", RecordedAt: at,
		Source: runtime.StartSource{Kind: runtime.StartSourceTrigger},
	})
	if err != nil {
		t.Fatal(err)
	}
	if identity.EventRef == "" || identity.ResultRef == "" {
		t.Fatalf("identity=%+v", identity)
	}
	if appender.request.SchemaRef != ClockWorkflowOutcomeSchema || appender.request.ExpectedHead != 0 {
		t.Fatalf("append request=%+v", appender.request)
	}
	var got clockWorkflowOutcome
	if err := jsonUnmarshal(appender.request.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.WorkflowID != clockpunch.WorkflowID || got.WorkflowName != "Clock in and clock out" || got.TerminalCode != "TIME_SESSION_CLOSED" || got.TerminalNodeID != "session_closed" || got.TerminalOutputDigest != "sha256:end" {
		t.Fatalf("outcome=%+v", got)
	}
}

func TestClockWorkflowTerminalWriterRejectsPromotionAndMissingPreparation(t *testing.T) {
	req := execute.TerminalWriteRequest{TenantID: uuid.New(), InstanceID: uuid.New(), WorkflowID: "promotion", PlanDigest: "plan", TerminalCode: "DONE", EndNodeID: "end", EndOutputDigest: "out", IdempotencyKey: "key", RecordedAt: time.Now().UTC(), Source: runtime.StartSource{Kind: runtime.StartSourceTrigger}}
	w := ClockWorkflowTerminalWriter{Appender: &clockTerminalAppender{}, Ledger: &clockTerminalPreparation{}, Proof: clockTerminalProof{}}
	if _, err := w.Write(context.Background(), fakeClockTx{}, req); err == nil {
		t.Fatal("promotion workflow accepted")
	}
	w.Ledger = nil
	req.WorkflowID = clockpunch.WorkflowID
	if _, err := w.Write(context.Background(), fakeClockTx{}, req); err == nil {
		t.Fatal("missing preparation accepted")
	}
}

type clockTerminalProof struct{ refs []string }

func (p clockTerminalProof) VerifyTerminal(context.Context, dbport.Tx, execute.TerminalWriteRequest) ([]string, error) {
	return p.refs, nil
}

type clockTerminalPreparation struct{ head int64 }

func (p *clockTerminalPreparation) EnsureSchema(context.Context, dbport.Tx, uuid.UUID, string) error {
	return nil
}
func (p *clockTerminalPreparation) EnsureStream(context.Context, dbport.Tx, uuid.UUID, string, string, string) error {
	return nil
}
func (p *clockTerminalPreparation) CurrentHead(context.Context, dbport.Tx, uuid.UUID, string) (int64, error) {
	return p.head, nil
}

type clockTerminalAppender struct {
	request ledgerport.AppendRequest
	receipt ledgerport.AppendReceipt
}

func (a *clockTerminalAppender) Append(_ context.Context, _ dbport.Tx, req ledgerport.AppendRequest) (ledgerport.AppendReceipt, error) {
	a.request = req
	return a.receipt, nil
}

type fakeClockTx struct{}

func (fakeClockTx) Exec(context.Context, string, ...any) (int64, error)        { return 0, nil }
func (fakeClockTx) Query(context.Context, string, ...any) (dbport.Rows, error) { return nil, nil }
func (fakeClockTx) QueryRow(context.Context, string, ...any) dbport.Row        { return nil }
func (fakeClockTx) Commit(context.Context) error                               { return nil }
func (fakeClockTx) Rollback(context.Context) error                             { return nil }

func jsonUnmarshal(b []byte, out any) error {
	var msg structpb.Struct
	if err := proto.Unmarshal(b, &msg); err != nil {
		return err
	}
	encoded, err := json.Marshal(msg.AsMap())
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}
