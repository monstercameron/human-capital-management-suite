package effects_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
)

type fakeCommitter struct {
	calls   int
	receipt transactioncommit.Receipt
	err     error
}

func (f *fakeCommitter) CommitInTx(context.Context, dbport.Tx, plan.TransactionPlan) (transactioncommit.Receipt, error) {
	f.calls++
	return f.receipt, f.err
}

type delegateTerminal struct{ calls int }

func (d *delegateTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	d.calls++
	return idempotency.ResultIdentity{ResultRef: "delegated"}, nil
}

func TestCommitTerminalWriterImplementsTerminalPortAndCommitsAPlan(t *testing.T) {
	tenant := uuid.New()
	fake := &fakeCommitter{receipt: transactioncommit.Receipt{ReceiptID: uuid.New(), PlanID: "plan-1"}}
	writer := &effects.CommitTerminalWriter{
		Committer: fake,
		Plan: func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error) {
			return plan.TransactionPlan{Tenant: values.TenantId(tenant.String()), PlanID: "plan-1", IdempotencyKey: "key-1"}, nil
		},
	}
	identity, err := writer.Write(context.Background(), nil, execute.TerminalWriteRequest{TenantID: tenant})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if fake.calls != 1 || identity.EvidenceID != fake.receipt.ReceiptID.String() || identity.EffectIdentity != "key-1" {
		t.Fatalf("calls/identity = %d/%+v", fake.calls, identity)
	}
}

func TestCommitTerminalWriterRetainsAdditiveDelegateDuringPlanRollout(t *testing.T) {
	delegate := &delegateTerminal{}
	writer := &effects.CommitTerminalWriter{Next: delegate}
	identity, err := writer.Write(context.Background(), nil, execute.TerminalWriteRequest{})
	if err != nil || identity.ResultRef != "delegated" || delegate.calls != 1 {
		t.Fatalf("delegate Write = %+v, %v; calls=%d", identity, err, delegate.calls)
	}
}

func TestCommitTerminalWriterRefusesMissingOrInvalidPlanBeforeCommit(t *testing.T) {
	ctx := context.Background()
	t.Run("missing dependencies", func(t *testing.T) {
		writer := &effects.CommitTerminalWriter{}
		_, err := writer.Write(ctx, nil, execute.TerminalWriteRequest{})
		if err == nil || !strings.Contains(err.Error(), "requires committer and plan provider") {
			t.Fatalf("missing dependencies error = %v", err)
		}
	})
	t.Run("plan failure", func(t *testing.T) {
		cause := errors.New("plan binding unavailable")
		committer := &fakeCommitter{}
		writer := &effects.CommitTerminalWriter{
			Committer: committer,
			Plan: func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error) {
				return plan.TransactionPlan{}, cause
			},
		}
		_, err := writer.Write(ctx, nil, execute.TerminalWriteRequest{})
		if !errors.Is(err, cause) || committer.calls != 0 {
			t.Fatalf("plan error = %v; commit calls = %d", err, committer.calls)
		}
	})
	t.Run("tenant mismatch", func(t *testing.T) {
		requestTenant, planTenant := uuid.New(), uuid.New()
		committer := &fakeCommitter{}
		writer := &effects.CommitTerminalWriter{
			Committer: committer,
			Plan: func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error) {
				return plan.TransactionPlan{Tenant: values.TenantId(planTenant.String())}, nil
			},
		}
		_, err := writer.Write(ctx, nil, execute.TerminalWriteRequest{TenantID: requestTenant})
		if err == nil || !strings.Contains(err.Error(), "does not match request tenant") || committer.calls != 0 {
			t.Fatalf("tenant mismatch error = %v; commit calls = %d", err, committer.calls)
		}
	})
}

func TestCommitTerminalWriterReportsCommitErrorsAndReceiptEventIdentity(t *testing.T) {
	t.Run("commit failure", func(t *testing.T) {
		cause := errors.New("transaction coordinator rejected plan")
		committer := &fakeCommitter{err: cause}
		writer := &effects.CommitTerminalWriter{
			Committer: committer,
			Plan: func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error) {
				return plan.TransactionPlan{IdempotencyKey: "key"}, nil
			},
		}
		_, err := writer.Write(context.Background(), nil, execute.TerminalWriteRequest{})
		if !errors.Is(err, cause) || committer.calls != 1 {
			t.Fatalf("commit error = %v; commit calls = %d", err, committer.calls)
		}
	})
	t.Run("event identity", func(t *testing.T) {
		stream := "tenant/acme/workflow/one"
		committer := &fakeCommitter{receipt: transactioncommit.Receipt{
			ReceiptID: uuid.New(),
			Events:    []datalogger.AppendReceipt{{StreamKey: stream, Sequence: 42}},
		}}
		writer := &effects.CommitTerminalWriter{
			Committer: committer,
			Plan: func(context.Context, dbport.Tx, execute.TerminalWriteRequest) (plan.TransactionPlan, error) {
				return plan.TransactionPlan{IdempotencyKey: "request-key"}, nil
			},
		}
		identity, err := writer.Write(context.Background(), nil, execute.TerminalWriteRequest{})
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		if identity.EventRef != stream+"@42" || identity.EffectIdentity != "request-key" || identity.EvidenceID != committer.receipt.ReceiptID.String() {
			t.Fatalf("receipt identity = %+v", identity)
		}
	})
}
