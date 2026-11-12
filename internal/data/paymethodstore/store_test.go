package paymethodstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/paymethodstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/settlement"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var testAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 106); err != nil {
		t.Fatalf("apply paymethod migrations: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantTx(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func tenantTxErr(conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := conn.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

func testDestination(t *testing.T, id string) paymethod.Destination {
	t.Helper()
	worker := uuid.New()
	effective, err := values.NewOpenInstantInterval(values.NewInstant(testAt))
	if err != nil {
		t.Fatal(err)
	}
	destination, err := paymethod.NewDestination(paymethod.Destination{
		ID: id, WorkerRef: worker.String(), Rail: paymethod.RailACH, Risk: paymethod.RiskLow,
		GovernedRef: "bank-detail:" + id, DisplayHint: "•••• 1234", Currency: "USD", CountryCode: "US",
		Verification: paymethod.VerificationUnverified, Effective: effective, State: paymethod.DestinationActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func testInstruction(t *testing.T, id string) settlement.PaymentInstruction {
	t.Helper()
	period := payroll.PeriodRef{ID: "period-2026-09", Version: "v1", Digest: "sha256:period"}
	population := payroll.PopulationBindingRef{DefinitionID: "population-2026-09", RevisionVersion: "v1", Digest: "sha256:population"}
	run, err := payroll.NewPayrollRun("run-2026-09", "monthly", period, population, "sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Calculate("sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Release("sha256:release")
	if err != nil {
		t.Fatal(err)
	}
	amount, err := values.NewDecimal("12.34", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	valueDate, err := values.ParseLocalDate("2026-09-07")
	if err != nil {
		t.Fatal(err)
	}
	payee := uuid.New().String()
	bankDetail := "bank-detail:" + id
	// The settlement domain requires a consenting election bound to the payee
	// and a jurisdiction rule snapshot matching the election.
	instruction, err := settlement.NewPaymentInstruction(run, settlement.PaymentInstructionSpec{
		InstructionID: id, PayeeRef: payee, Amount: amount, Currency: "USD",
		FundingSourceRef: "funding:payroll", Rail: settlement.RailACH, BankDetailRef: bankDetail,
		ScheduleRef: "schedule:weekly", ValueDate: valueDate,
		PaymentMethodElection: settlement.PaymentMethodElection{
			ElectionID: "election:" + id, WorkerRef: payee, Method: settlement.MethodDirectDeposit,
			Consented: true, ConsentEvidenceRef: "consent:" + id, DestinationRef: bankDetail,
			Jurisdiction: "US-CA", RulePackRef: "rules:ca-v1",
		},
		SettlementPolicy: settlement.SettlementPolicy{Jurisdiction: "US-CA", RulePackRef: "rules:ca-v1", PaperCheckAllowed: true, PayCardAllowed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return instruction
}

func testVerificationEvent(t *testing.T, destination paymethod.Destination) paymethod.VerificationEvent {
	t.Helper()
	challenge, err := paymethod.NewVerificationChallenge(destination, "challenge-1", paymethod.MethodMicroDeposit,
		values.NewInstant(testAt), values.NewInstant(testAt.Add(time.Hour)), 2)
	if err != nil {
		t.Fatal(err)
	}
	event, err := challenge.Verify(values.NewInstant(testAt.Add(10*time.Minute)), "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestTodo_PERSIST_PAYMETHOD_001(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "paymethod-primary")
	store := paymethodstore.New()
	destination := testDestination(t, "destination-001")
	account, err := paymethod.NewProtectedAccount(paymethod.StorageTokenized, "token:destination-001", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	validation, err := paymethod.NewAccountValidationRecord("INSTANT_VERIFICATION", testAt, paymethod.ValidationPassed, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatal(err)
	}
	event := testVerificationEvent(t, destination)
	instruction := testInstruction(t, "instruction-001")
	confirmation := paymethod.ChangeConfirmation{ChangeDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", DestinationID: destination.DestinationID, DispatchedAt: testAt}

	tenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		if _, err := store.PutDestination(context.Background(), tx, tenantID, destination); err != nil {
			return err
		}
		if _, err := store.PutProtectedAccount(context.Background(), tx, tenantID, "account-001", account); err != nil {
			return err
		}
		if _, err := store.PutAccountValidation(context.Background(), tx, tenantID, "account-001", validation); err != nil {
			return err
		}
		if err := store.AppendVerificationEvent(context.Background(), tx, tenantID, event, 1); err != nil {
			return err
		}
		if err := store.AppendChangeConfirmation(context.Background(), tx, tenantID, confirmation, 1); err != nil {
			return err
		}
		_, err := store.PutPaymentInstruction(context.Background(), tx, tenantID, instruction)
		return err
	})

	tenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		gotDestination, err := store.LoadDestination(context.Background(), tx, tenantID, destination.DestinationID, 1)
		if err != nil {
			return err
		}
		if gotDestination.CanonicalDigest != destination.CanonicalDigest || gotDestination.Revision != 1 {
			t.Fatalf("destination reload = %+v", gotDestination)
		}
		gotAccount, err := store.LoadProtectedAccount(context.Background(), tx, tenantID, "account-001")
		if err != nil {
			return err
		}
		if gotAccount.OpaqueReference != account.OpaqueReference || gotAccount.ValueDigest != account.ValueDigest {
			t.Fatalf("protected account reload = %+v", gotAccount)
		}
		gotInstruction, err := store.LoadPaymentInstruction(context.Background(), tx, tenantID, instruction.InstructionID, 1)
		if err != nil {
			return err
		}
		if gotInstruction.CanonicalDigest != instruction.CanonicalDigest || !gotInstruction.Amount.Equal(instruction.Amount) {
			t.Fatalf("payment instruction reload = %+v", gotInstruction)
		}
		var tables int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.tables
			WHERE table_schema=current_schema() AND table_name IN
			('paymethod_destination','paymethod_bank_detail_change','paymethod_verification_event',
			 'paymethod_change_confirmation','paymethod_protected_account','paymethod_account_validation',
			 'settlement_payment_instruction')`).Scan(&tables); err != nil {
			return err
		}
		if tables != 7 {
			t.Fatalf("paymethod table count = %d, want 7", tables)
		}
		return nil
	})
}

func TestTodo_PERSIST_PAYMETHOD_001_Integration(t *testing.T) {
	db := newDB(t)
	writer, reader := appConn(t, db), appConn(t, db)
	tenantID := insertTenant(t, db, "paymethod-integration")
	store := paymethodstore.New()
	instruction := testInstruction(t, "instruction-integration")
	tenantTx(t, writer, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutPaymentInstruction(context.Background(), tx, tenantID, instruction)
		return err
	})
	tenantTx(t, reader, tenantID, func(tx dbport.Tx) error {
		got, err := store.LoadPaymentInstruction(context.Background(), tx, tenantID, instruction.InstructionID, 1)
		if err != nil {
			return err
		}
		if got.CanonicalDigest != instruction.CanonicalDigest {
			t.Fatalf("fresh connection digest = %s, want %s", got.CanonicalDigest, instruction.CanonicalDigest)
		}
		return nil
	})
}

func TestTodo_PERSIST_PAYMETHOD_001_Security(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	alpha, beta := insertTenant(t, db, "paymethod-alpha"), insertTenant(t, db, "paymethod-beta")
	store := paymethodstore.New()
	destination := testDestination(t, "destination-isolated")
	tenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, alpha, destination)
		return err
	})
	tenantTx(t, conn, beta, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM paymethod_destination`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("beta saw %d alpha destination rows", count)
		}
		_, err := store.LoadDestination(context.Background(), tx, beta, destination.DestinationID, 1)
		if !errors.Is(err, paymethodstore.ErrNotFound) {
			t.Fatalf("cross-tenant load = %v, want ErrNotFound", err)
		}
		return nil
	})
}

func TestTodo_PERSIST_PAYMETHOD_001_Recovery(t *testing.T) {
	db := newDB(t)
	writer, fresh := appConn(t, db), appConn(t, db)
	tenantID := insertTenant(t, db, "paymethod-recovery")
	store := paymethodstore.New()
	instruction := testInstruction(t, "instruction-recovery")
	tenantTx(t, writer, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutPaymentInstruction(context.Background(), tx, tenantID, instruction)
		return err
	})
	tenantTx(t, fresh, tenantID, func(tx dbport.Tx) error {
		got, err := store.GetPaymentInstruction(context.Background(), tx, tenantID, instruction.InstructionID, 1)
		if err != nil {
			return err
		}
		if got.InstructionID != instruction.InstructionID || got.CanonicalDigest != instruction.CanonicalDigest {
			t.Fatalf("recovered instruction = %+v", got)
		}
		return nil
	})
}

func TestTodo_PERSIST_PAYMETHOD_001_Fault(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "paymethod-fault")
	store := paymethodstore.New()
	first := testDestination(t, "destination-fault")
	tenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, tenantID, first)
		return err
	})
	err := tenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, tenantID, first)
		return err
	})
	if !errors.Is(err, paymethodstore.ErrDuplicate) {
		t.Fatalf("duplicate revision = %v", err)
	}
	var duplicate *paymethodstore.Error
	if !errors.As(err, &duplicate) || duplicate.Code != paymethodstore.CodeDuplicateRevision {
		t.Fatalf("duplicate code = %v", err)
	}
	next, err := first.NewRevision(paymethod.Destination{Rail: paymethod.RailACH, Risk: paymethod.RiskLow, GovernedRef: "bank-detail:next", DisplayHint: "•••• 5678", Currency: "USD", CountryCode: "US", Verification: paymethod.VerificationUnverified, Effective: first.Effective, State: paymethod.DestinationActive})
	if err != nil {
		t.Fatal(err)
	}
	tenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, tenantID, next)
		return err
	})
	stale, err := next.NewRevision(paymethod.Destination{Rail: paymethod.RailACH, Risk: paymethod.RiskLow, GovernedRef: "bank-detail:stale", DisplayHint: "•••• 9999", Currency: "USD", CountryCode: "US", Verification: paymethod.VerificationUnverified, Effective: first.Effective, State: paymethod.DestinationActive})
	if err != nil {
		t.Fatal(err)
	}
	stale.Revision = 4
	stale.SupersedesRevision = 1
	stale.SupersedesDigest = first.CanonicalDigest
	stale, err = paymethod.NewDestination(stale)
	if err != nil {
		t.Fatal(err)
	}
	err = tenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, tenantID, stale)
		return err
	})
	if !errors.Is(err, paymethodstore.ErrVersionConflict) {
		t.Fatalf("stale CAS = %v", err)
	}
}

func TestTodo_PERSIST_PAYMETHOD_001_Mutation(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "paymethod-mutation")
	store := paymethodstore.New()
	destination := testDestination(t, "destination-mutation")
	event := testVerificationEvent(t, destination)
	tenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		if _, err := store.PutDestination(context.Background(), tx, tenantID, destination); err != nil {
			return err
		}
		return store.AppendVerificationEvent(context.Background(), tx, tenantID, event, 1)
	})
	for _, statement := range []string{
		`UPDATE paymethod_destination SET state='RETIRED' WHERE tenant_id=$1`,
		`DELETE FROM paymethod_destination WHERE tenant_id=$1`,
		`UPDATE paymethod_verification_event SET attempt=2 WHERE tenant_id=$1`,
		`DELETE FROM paymethod_verification_event WHERE tenant_id=$1`,
	} {
		err := tenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), statement, tenantID)
			return err
		})
		if err == nil {
			t.Fatalf("mutation accepted: %s", statement)
		}
	}
}
