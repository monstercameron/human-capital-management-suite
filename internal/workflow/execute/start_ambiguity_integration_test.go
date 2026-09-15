package execute_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type ambiguityBeginner struct {
	conn, admin    *pgxadapter.Conn
	rollback       bool
	readOnly       int
	readOnlyChecks int
	serialBegins   int
	beforeReadOnly func(context.Context) error
}

func (b *ambiguityBeginner) Begin(ctx context.Context) (dbport.Tx, error) {
	return b.conn.Begin(ctx)
}
func (b *ambiguityBeginner) BeginSerializable(ctx context.Context) (dbport.Tx, error) {
	b.serialBegins++
	tx, err := b.conn.BeginSerializable(ctx)
	if err != nil {
		return nil, err
	}
	return &ambiguityTx{Tx: tx, owner: b}, nil
}
func (b *ambiguityBeginner) BeginReadOnly(ctx context.Context) (dbport.Tx, error) {
	b.readOnly++
	if b.beforeReadOnly != nil {
		if err := b.beforeReadOnly(ctx); err != nil {
			return nil, err
		}
		b.beforeReadOnly = nil
	}
	tx, err := b.conn.BeginReadOnly(ctx)
	if err != nil {
		return nil, err
	}
	var mode string
	if err := tx.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&mode); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return nil, fmt.Errorf("inspect read-only transaction: %v; rollback: %w", err, rollbackErr)
		}
		return nil, err
	}
	b.readOnlyChecks++
	if mode != "on" {
		if err := tx.Rollback(ctx); err != nil {
			return nil, fmt.Errorf("transaction mode %q; rollback: %w", mode, err)
		}
		return nil, fmt.Errorf("transaction mode = %q, want on", mode)
	}
	return tx, nil
}

type ambiguityTx struct {
	dbport.Tx
	owner *ambiguityBeginner
}

func (t *ambiguityTx) Commit(ctx context.Context) error {
	if t.owner.rollback {
		if err := t.Tx.Rollback(ctx); err != nil {
			return fmt.Errorf("simulate ambiguous rollback: %w", err)
		}
	} else if err := t.Tx.Commit(ctx); err != nil {
		return err
	}
	return errors.New("connection lost after commit response")
}

func TestTodo_DB_EDGE_003_AmbiguousCommitResolvesReadOnlyCurrentState(t *testing.T) {
	db := pgtest.New(t)
	// The deadline starts once PostgreSQL is up, so a loaded sweep's
	// start-up time does not consume the budget the test bounds.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, at := uuid.New(), time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','Ambiguous','ACTIVE',$3)`, tenant, "ambiguous-"+tenant.String(), at.Add(-time.Hour))
	db.Exec(t, `CREATE TABLE execute_start_retry_fact (chosen integer NOT NULL)`)
	db.Exec(t, `INSERT INTO execute_start_retry_fact VALUES (1)`)
	plan, err := workflow.Compile(prototype.ApprovalDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatal(err)
	}
	versions := retryVersionStore{plan.Digest(): {WorkflowID: plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: plan.Digest(), Status: version.StatusActive}}
	conn, admin := db.NewConn(t), db.NewConn(t)
	defer conn.Close(ctx)
	defer admin.Close(ctx)
	b := &ambiguityBeginner{conn: conn, admin: admin}
	resolver := &switchingResolver{plans: map[int]*workflow.CompiledWorkflow{1: plan}}
	runner := &retryApprovalRunner{}
	d, err := execute.New(execute.Options{DB: b, Steps: runner, StartRetry: &transactioncommit.RetryOptions{MaxAttempts: 3, Admit: func(context.Context) error { return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	req := ambiguityStartRequest(tenant, at, resolver, versions, plan)
	result, err := d.Execute(ctx, execute.ExecuteRequest{Start: req})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execute.StatusResolved || result.StartResolution == nil {
		t.Fatalf("result=%+v, want RESOLVED current state", result)
	}
	if runner.calls.Load() != 0 || len(result.WorkItems) != 0 {
		t.Fatalf("resolution ran work: steps=%d items=%d", runner.calls.Load(), len(result.WorkItems))
	}
	if b.readOnly != 1 || b.readOnlyChecks != 1 || b.serialBegins != 1 {
		t.Fatalf("transaction counts: serial=%d read-only=%d mode-checks=%d", b.serialBegins, b.readOnly, b.readOnlyChecks)
	}
	if result.Start.InstanceID != uuid.Nil || result.Start.WorkflowID != "" || result.Start.Digest() != "" || len(result.Start.Frontier) != 0 {
		t.Fatalf("resolved outcome fabricated original StartReceipt: %+v", result.Start)
	}
	resolved := result.StartResolution.Instance
	wantInstanceID := uuid.NewSHA1(uuid.MustParse("2f7e9c3a-8b1d-4e6f-9a2c-5d8b1e4f7a3c"), []byte(tenant.String()+"\x00"+plan.WorkflowID+"\x00start:ambiguous"))
	if resolved.TenantID != tenant || resolved.WorkflowID != plan.WorkflowID || resolved.WorkflowVersion != plan.Version ||
		resolved.InstanceID != wantInstanceID || resolved.CompiledPlanHash != plan.Digest() || resolved.InstanceVersion != result.InstanceVersion ||
		len(resolved.CurrentNodeIDs) != 1 || resolved.CurrentNodeIDs[0] != plan.StartNodeID {
		t.Fatalf("resolved durable instance is not exact: %+v", resolved)
	}
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("instances=%d, want 1", count)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("work items=%d during resolution, want 0", count)
	}
}

func TestTodo_DB_EDGE_003_RolledBackAmbiguousCommitRemainsUnresolved(t *testing.T) {
	db := pgtest.New(t)
	// The deadline starts once PostgreSQL is up, so a loaded sweep's
	// start-up time does not consume the budget the test bounds.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, at := uuid.New(), time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','Rollback','ACTIVE',$3)`, tenant, "rollback-"+tenant.String(), at.Add(-time.Hour))
	db.Exec(t, `CREATE TABLE execute_start_retry_fact (chosen integer NOT NULL)`)
	db.Exec(t, `INSERT INTO execute_start_retry_fact VALUES (1)`)
	plan, err := workflow.Compile(prototype.ApprovalDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatal(err)
	}
	versions := retryVersionStore{plan.Digest(): {WorkflowID: plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: plan.Digest(), Status: version.StatusActive}}
	conn, admin := db.NewConn(t), db.NewConn(t)
	defer conn.Close(ctx)
	defer admin.Close(ctx)
	b := &ambiguityBeginner{conn: conn, admin: admin, rollback: true}
	resolver := &switchingResolver{plans: map[int]*workflow.CompiledWorkflow{1: plan}}
	runner := &retryApprovalRunner{}
	d, err := execute.New(execute.Options{DB: b, Steps: runner, StartRetry: &transactioncommit.RetryOptions{MaxAttempts: 3, Admit: func(context.Context) error { return nil }, Sleep: func(context.Context, time.Duration) error { return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Execute(ctx, execute.ExecuteRequest{Start: ambiguityStartRequest(tenant, at, resolver, versions, plan)})
	if !errors.Is(err, transactioncommit.ErrCommitAmbiguous) {
		t.Fatalf("err=%v, want ErrCommitAmbiguous", err)
	}
	if b.serialBegins != 1 {
		t.Fatalf("serializable attempts=%d, want 1", b.serialBegins)
	}
	if b.readOnly != 1 || b.readOnlyChecks != 1 || runner.calls.Load() != 0 {
		t.Fatalf("resolution activity: read-only=%d checks=%d steps=%d", b.readOnly, b.readOnlyChecks, runner.calls.Load())
	}
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("instances=%d after rollback, want 0", count)
	}
}

func TestTodo_DB_EDGE_003_MismatchedCommittedStateIsNotReplayed(t *testing.T) {
	db := pgtest.New(t)
	// The deadline starts once PostgreSQL is up, so a loaded sweep's
	// start-up time does not consume the budget the test bounds.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, at := uuid.New(), time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local','Mismatch','ACTIVE',$3)`, tenant, "mismatch-"+tenant.String(), at.Add(-time.Hour))
	db.Exec(t, `CREATE TABLE execute_start_retry_fact (chosen integer NOT NULL)`)
	db.Exec(t, `INSERT INTO execute_start_retry_fact VALUES (1)`)
	plan, err := workflow.Compile(prototype.ApprovalDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatal(err)
	}
	versions := retryVersionStore{plan.Digest(): {WorkflowID: plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: plan.Digest(), Status: version.StatusActive}}
	conn, admin := db.NewConn(t), db.NewConn(t)
	defer conn.Close(ctx)
	defer admin.Close(ctx)
	b := &ambiguityBeginner{conn: conn, admin: admin}
	b.beforeReadOnly = func(ctx context.Context) error {
		_, err := admin.Exec(ctx, `UPDATE workflow_instance SET cell_id='cell-other' WHERE tenant_id=$1`, tenant)
		return err
	}
	resolver := &switchingResolver{plans: map[int]*workflow.CompiledWorkflow{1: plan}}
	runner := &retryApprovalRunner{}
	d, err := execute.New(execute.Options{DB: b, Steps: runner, StartRetry: &transactioncommit.RetryOptions{MaxAttempts: 3, Admit: func(context.Context) error { return nil }, Sleep: func(context.Context, time.Duration) error { return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.Execute(ctx, execute.ExecuteRequest{Start: ambiguityStartRequest(tenant, at, resolver, versions, plan)})
	if !errors.Is(err, transactioncommit.ErrCommitAmbiguous) {
		t.Fatalf("err=%v, want unresolved ambiguous commit", err)
	}
	if result.Status != "" || result.StartResolution != nil || result.Start.InstanceID != uuid.Nil {
		t.Fatalf("mismatched state returned a resolution: %+v", result)
	}
	if b.serialBegins != 1 || b.readOnly != 1 || b.readOnlyChecks != 1 || runner.calls.Load() != 0 {
		t.Fatalf("mismatch was replayed: serial=%d read-only=%d checks=%d steps=%d", b.serialBegins, b.readOnly, b.readOnlyChecks, runner.calls.Load())
	}
}

func ambiguityStartRequest(tenant uuid.UUID, at time.Time, resolver runtime.WorkflowResolver, versions version.Store, plan *workflow.CompiledWorkflow) runtime.StartRequest {
	intentID, revisionID := "intent:ambiguous", "proposal:ambiguous:1"
	rev := intent.ProposalRevision{IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1, Tenant: values.TenantId("ambiguous"), OrganizationScopeID: "organization:ambiguous", Subjects: []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:ambiguous", AuthorityDomain: "PEOPLE"}}, CreatedBy: intent.PrincipalReference{PrincipalID: "principal:test-initiator", Kind: intent.InitiatorHuman}, MaterialDigest: digestReference(revisionID, intentID)}
	return runtime.StartRequest{TenantID: tenant, CellID: "cell-local", StartIdempotencyKey: "start:ambiguous", Resolver: resolver, Versions: versions, Proposal: runtime.ProposalBinding{Revision: rev}, ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(rev), ExpectedIntentID: intentID, ExpectedTenant: rev.Tenant, BusinessSubjectRefs: []string{"employment:ambiguous"}, ExecutionMode: workflow.ModeExecute, CorrelationID: "correlation:ambiguous", CreatedAt: at}
}
