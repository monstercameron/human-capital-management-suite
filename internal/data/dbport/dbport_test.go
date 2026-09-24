package dbport

import (
	"context"
	"errors"
	"testing"
)

type mockRow struct{ err error }

func (m mockRow) Scan(dest ...any) error { return m.err }

type mockRows struct {
	next bool
	err  error
}

func (m *mockRows) Next() bool        { return m.next }
func (m *mockRows) Scan(...any) error { return nil }
func (m *mockRows) Err() error        { return m.err }
func (m *mockRows) Close()            {}

type mockConn struct{}

func (m mockConn) Exec(_ context.Context, _ string, _ ...any) (int64, error) { return 1, nil }
func (m mockConn) Query(_ context.Context, _ string, _ ...any) (Rows, error) { return &mockRows{}, nil }
func (m mockConn) QueryRow(_ context.Context, _ string, _ ...any) Row        { return mockRow{} }

type mockTx struct{ mockConn }

func (m mockTx) Commit(_ context.Context) error   { return nil }
func (m mockTx) Rollback(_ context.Context) error { return nil }

type mockBeginner struct{ mockConn }

func (m mockBeginner) Begin(_ context.Context) (Tx, error) { return mockTx{}, nil }

func TestErrNoRows(t *testing.T) {
	if ErrNoRows.Error() != "no rows in result set" {
		t.Fatalf("unexpected message %q", ErrNoRows.Error())
	}
	if !errors.Is(ErrNoRows, ErrNoRows) {
		t.Fatal("errors.Is self check failed")
	}
	wrapped := errors.Join(errors.New("x"), ErrNoRows)
	if !errors.Is(wrapped, ErrNoRows) {
		t.Fatal("wrapped ErrNoRows not detected")
	}
}

func TestInterfaces(t *testing.T) {
	var _ Row = mockRow{}
	var _ Rows = &mockRows{}
	var _ Execer = mockConn{}
	var _ Querier = mockConn{}
	var _ Conn = mockConn{}
	var _ Tx = mockTx{}
	var _ Beginner = mockBeginner{}
}

func TestTransactionContextRoundtrip(t *testing.T) {
	want := mockTx{}
	ctx := ContextWithTx(context.Background(), want)
	got, ok := TxFromContext(ctx)
	if !ok || got == nil {
		t.Fatalf("TxFromContext() = %v, %v; want transaction", got, ok)
	}
	if _, ok := TxFromContext(context.Background()); ok {
		t.Fatal("TxFromContext() found a transaction in an empty context")
	}
}

func TestMockConnExec(t *testing.T) {
	var c Conn = mockConn{}
	n, err := c.Exec(context.Background(), "select 1")
	if err != nil || n != 1 {
		t.Fatalf("Exec got %d %v", n, err)
	}
}

func TestMockBeginner(t *testing.T) {
	var b Beginner = mockBeginner{}
	tx, err := b.Begin(context.Background())
	if err != nil || tx == nil {
		t.Fatalf("Begin failed %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
}
