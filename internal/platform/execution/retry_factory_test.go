package execution

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

func TestNewPromotionExecutionRejectsStaticAndFactoryRetryPolicies(t *testing.T) {
	_, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: stubBeginner{}, Terminal: stubTerminal{},
		StartRetry: &transactioncommit.RetryOptions{Admit: func(context.Context) error { return nil }},
		StartRetryFor: func(context.Context, execute.StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("static and request-scoped retry policies were accepted together")
	}
}

func TestStartRetryForDenialReceivesExactBindingAndWritesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	database := pgtest.New(t)
	tenant := uuid.New()
	at := time.Date(2026, 9, 8, 12, 0, 0, 123456000, time.UTC)
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'retry-tenant','cell-local','Retry tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	conn := database.NewConn(t)
	defer conn.Close(ctx)

	denied := errors.New("request retry policy denied")
	var calls atomic.Int32
	execution, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: conn, Terminal: stubTerminal{}, Clock: func() time.Time { return at },
		StartRetryFor: func(_ context.Context, req execute.StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
			calls.Add(1)
			if req.TenantID != tenant || req.StartIdempotencyKey != "start:factory-denied" {
				t.Fatalf("factory request tenant/key = %s/%q", req.TenantID, req.StartIdempotencyKey)
			}
			return nil, denied
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := execution.Executor.Execute(ctx, retryStart(execution, tenant, "factory-denied", at))
	if !errors.Is(gotErr, denied) || calls.Load() != 1 {
		t.Fatalf("Execute error=%v factory calls=%d", gotErr, calls.Load())
	}
	var instances, workItems int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenant).Scan(&instances); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM work_item WHERE tenant_id=$1`, tenant).Scan(&workItems); err != nil {
		t.Fatal(err)
	}
	if instances != 0 || workItems != 0 {
		t.Fatalf("denied factory wrote instances/workitems = %d/%d", instances, workItems)
	}
}

func TestStartRetryForSelectedPolicyExecutesBoundStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	database := pgtest.New(t)
	tenant := uuid.New()
	at := time.Date(2026, 9, 8, 12, 0, 0, 123456000, time.UTC)
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'retry-tenant','cell-local','Retry tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	database.Exec(t, `CREATE TABLE execution_retry_approval (tenant_id uuid PRIMARY KEY, approved boolean NOT NULL)`)
	database.Exec(t, `INSERT INTO execution_retry_approval (tenant_id,approved) VALUES ($1,true)`, tenant)
	conn := database.NewConn(t)
	defer conn.Close(ctx)
	var factoryCalls, admissions atomic.Int32
	execution, err := NewPromotionExecution(PromotionExecutionConfig{
		DB: conn, Terminal: stubTerminal{}, Clock: func() time.Time { return at },
		StartRetryFor: func(_ context.Context, req execute.StartRetryIdentity) (*transactioncommit.RetryOptions, error) {
			factoryCalls.Add(1)
			if req.TenantID != tenant || req.StartIdempotencyKey != "start:factory-approved" {
				t.Fatalf("factory request tenant/key = %s/%q", req.TenantID, req.StartIdempotencyKey)
			}
			return &transactioncommit.RetryOptions{MaxAttempts: 1, Admit: func(context.Context) error {
				admissions.Add(1)
				return nil
			}}, nil
		},
		Currency: retryCurrency(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := execution.Executor.Execute(ctx, retryStart(execution, tenant, "factory-approved", at))
	if err != nil || !result.Parked || len(result.ParkedWorkItems) != 1 {
		t.Fatalf("factory-selected execution result=%+v err=%v", result, err)
	}
	if factoryCalls.Load() != 1 || admissions.Load() != 1 {
		t.Fatalf("factory/admission calls = %d/%d", factoryCalls.Load(), admissions.Load())
	}
	var instances int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id=$1`, tenant).Scan(&instances); err != nil || instances != 1 {
		t.Fatalf("workflow instance count=%d err=%v", instances, err)
	}
}
