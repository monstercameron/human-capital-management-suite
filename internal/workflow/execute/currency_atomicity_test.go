package execute

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type currencyFaultTx struct {
	memoryTx
	fail string
	err  error
}

func (tx *currencyFaultTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if sql == tx.fail {
		return 0, tx.err
	}
	return tx.memoryTx.Exec(ctx, sql, args...)
}

type currencyFaultDB struct{ tx *currencyFaultTx }

func (db currencyFaultDB) Begin(context.Context) (dbport.Tx, error) { return db.tx, nil }

func TestTodo_WF_RUN_042(t *testing.T) {
	for _, statement := range []string{"SAVEPOINT workflow_currency_guard", "ROLLBACK TO SAVEPOINT workflow_currency_guard", "RELEASE SAVEPOINT workflow_currency_guard"} {
		t.Run(statement, func(t *testing.T) {
			scn := newOBSScenario(t, "")
			d := scn.driver(t)
			fault := errors.New("injected savepoint fault")
			tx := &currencyFaultTx{fail: statement, err: fault}
			d.opts.DB = currencyFaultDB{tx: tx}
			facts := runtime.MemoryProposalFacts{}
			if statement == "ROLLBACK TO SAVEPOINT workflow_currency_guard" {
				rev := scn.req.Start.Proposal.Revision
				changed := materiallyChangedRevision(rev)
				facts.Facts = map[string]runtime.ProposalSupersessionFact{rev.ProposalRevisionID: {Superseded: true, CurrentRevision: &changed}}
			}
			d.opts.Currency = &CurrencyGuard{Proposal: facts, Approval: approvedApprovalFacts(scn.req.Start.Proposal.Revision)}
			_, err := d.Resume(context.Background(), scn.req)
			if !errors.Is(err, fault) || tx.committed || !tx.rolledBack || scn.advanceCalls != 0 {
				t.Fatalf("error=%v commit=%v rollback=%v advances=%d", err, tx.committed, tx.rolledBack, scn.advanceCalls)
			}
		})
	}
}

func TestTodo_WF_RUN_042_Integration(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "current"
		if blocked {
			name = "superseded"
		}
		t.Run(name, func(t *testing.T) {
			f := newWfrun028Fixture(t, "currency-atomicity-"+name)
			f.db.Exec(t, "CREATE TABLE currency_effect_probe (effect_id integer PRIMARY KEY)")
			f.db.Exec(t, "GRANT INSERT, SELECT ON currency_effect_probe TO hcmnext_app")
			facts := runtime.MemoryProposalFacts{}
			if blocked {
				changed := materiallyChangedRevision(f.proposal)
				facts.Facts = map[string]runtime.ProposalSupersessionFact{f.proposal.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:newer", CurrentRevision: &changed}}
			}
			d := f.driverWithCurrency(t, &CurrencyGuard{Proposal: facts, Approval: approvedApprovalFacts(f.proposal)})
			req := f.resumeRequest()
			selection, err := req.Start.Resolver.ResolveWorkflow(context.Background(), req.Start)
			if err != nil {
				t.Fatal(err)
			}
			run := runContext{start: req.Start, selection: selection, instanceID: f.instanceID}
			_, _, _, _, err = d.advanceOnce(context.Background(), run, req.ExpectedInstanceVersion, f.at, 1,
				func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
					if _, err := ex.Exec(ctx, "INSERT INTO currency_effect_probe (effect_id) VALUES (1)"); err != nil {
						return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, err
					}
					outcome, refs, err := checkWorkItemDrift(req, selection, f.item)
					return outcome, refs, nil, err
				}, nil)
			if blocked && !errors.Is(err, ErrCurrencyBlocked) || !blocked && err != nil {
				t.Fatalf("advance error=%v", err)
			}
			var count int
			if err := f.conn.QueryRow(context.Background(), "SELECT count(*) FROM currency_effect_probe").Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 1
			if blocked {
				want = 0
			}
			if count != want {
				t.Fatalf("committed step effects=%d want %d", count, want)
			}
			if blocked {
				work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
					instance, err := (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
					if err == nil && instance.RuntimeStatus != runtime.InstanceBlocked {
						t.Fatalf("status=%s want BLOCKED", instance.RuntimeStatus)
					}
					return err
				})
			}
		})
	}
}
