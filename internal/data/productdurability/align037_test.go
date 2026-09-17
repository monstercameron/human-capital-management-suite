package productdurability

import (
	"errors"
	"testing"
	"time"
)

func usdMinor(minor int64) Money {
	return Money{AmountMinor: minor, Currency: "USD"}
}

func align037Commit() (string, []LedgerLine, []OutboxMessage) {
	return "commit-1",
		[]LedgerLine{
			{EntryID: "entry-1", Account: "cash", Side: "debit", Amount: usdMinor(105000)},
			{EntryID: "entry-2", Account: "revenue", Side: "credit", Amount: usdMinor(105000)},
		},
		[]OutboxMessage{
			{MessageID: "msg-1", EntryID: "entry-1", Destination: "payroll", PayloadDigest: "sha256:payload-1"},
		}
}

// TestTodo_ALIGN_037 proves atomic ledger projection and outbox
// persistence: balanced lines and their messages land together, and an
// unbalanced or dangling commit lands nothing.
func TestTodo_ALIGN_037(t *testing.T) {
	store := NewLedgerOutbox()
	commitID, lines, messages := align037Commit()
	receipt, err := store.Commit(durabilityTenant, commitID, lines, messages, durabilityBase())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if receipt.Lines != 2 || receipt.Messages != 1 || receipt.Digest == "" {
		t.Fatalf("receipt = %+v", receipt)
	}
	keptLines, keptMessages := store.Counts()
	if keptLines != 2 || keptMessages != 1 {
		t.Fatalf("kept = %d lines, %d messages", keptLines, keptMessages)
	}
}

func TestTodo_ALIGN_037_Property(t *testing.T) {
	commitID, lines, messages := align037Commit()
	left, right := NewLedgerOutbox(), NewLedgerOutbox()
	first, err := left.Commit(durabilityTenant, commitID, lines, messages, durabilityBase())
	if err != nil {
		t.Fatal(err)
	}
	second, err := right.Commit(durabilityTenant, commitID, lines, messages, durabilityBase())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("commit receipt is not deterministic: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_ALIGN_037_Golden(t *testing.T) {
	store := NewLedgerOutbox()
	commitID, lines, messages := align037Commit()
	receipt, err := store.Commit(durabilityTenant, commitID, lines, messages, durabilityBase())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	const wantDigest = "sha256:a75689103840c4e4f43b668c7b40b1ab76fefdf1ea31d1d92d419d1a77901d08"
	if receipt.Digest != wantDigest {
		t.Fatalf("receipt digest=%q want=%q", receipt.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_037_Security(t *testing.T) {
	store := NewLedgerOutbox()
	// An unbalanced commit keeps nothing.
	_, lines, messages := align037Commit()
	lines[1].Amount = usdMinor(104999)
	if _, err := store.Commit(durabilityTenant, "bad-balance", lines, messages, durabilityBase()); !errors.Is(err, ErrLedgerUnbalanced) {
		t.Fatalf("Commit(unbalanced) = %v, want ErrLedgerUnbalanced", err)
	}
	// A message for an uncommitted entry keeps nothing.
	_, lines, _ = align037Commit()
	ghost := []OutboxMessage{{MessageID: "msg-x", EntryID: "entry-ghost", Destination: "payroll", PayloadDigest: "sha256:x"}}
	if _, err := store.Commit(durabilityTenant, "ghost-message", lines, ghost, durabilityBase()); !errors.Is(err, ErrLedgerInvalid) {
		t.Fatalf("Commit(ghost) = %v, want ErrLedgerInvalid", err)
	}
	if keptLines, keptMessages := store.Counts(); keptLines != 0 || keptMessages != 0 {
		t.Fatalf("refused commits kept %d lines and %d messages", keptLines, keptMessages)
	}
}

func TestTodo_ALIGN_037_Integration(t *testing.T) {
	store := NewLedgerOutbox()
	commitID, lines, messages := align037Commit()
	if _, err := store.Commit(durabilityTenant, commitID, lines, messages, durabilityBase()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(durabilityTenant, "commit-2",
		[]LedgerLine{
			{EntryID: "entry-3", Account: "cash", Side: "debit", Amount: usdMinor(25000)},
			{EntryID: "entry-4", Account: "revenue", Side: "credit", Amount: usdMinor(25000)},
		},
		[]OutboxMessage{
			{MessageID: "msg-2", EntryID: "entry-3", Destination: "payroll", PayloadDigest: "sha256:payload-2"},
		}, durabilityBase().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The projection accumulates exactly the kept lines: cash nets two
	// debits, revenue nets two credits.
	cash, err := store.ProjectedBalance("cash", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if cash.AmountMinor != 130000 {
		t.Fatalf("cash balance = %v, want 1300.00 USD", cash)
	}
	revenue, err := store.ProjectedBalance("revenue", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if revenue.AmountMinor != -130000 {
		t.Fatalf("revenue balance = %v, want -1300.00 USD", revenue)
	}
}

func TestTodo_ALIGN_037_Fault(t *testing.T) {
	store := NewLedgerOutbox()
	// A crash between the ledger persist and the outbox persist leaves
	// neither side visible, and the store stays usable afterwards.
	store.FaultAfterLedger = 1
	commitID, lines, messages := align037Commit()
	if _, err := store.Commit(durabilityTenant, commitID, lines, messages, durabilityBase()); !errors.Is(err, ErrLedgerAtomic) {
		t.Fatalf("Commit(crash) = %v, want ErrLedgerAtomic", err)
	}
	if keptLines, keptMessages := store.Counts(); keptLines != 0 || keptMessages != 0 {
		t.Fatalf("crashed commit kept %d lines and %d messages", keptLines, keptMessages)
	}
	store.FaultAfterLedger = 0
	if _, err := store.Commit(durabilityTenant, commitID, lines, messages, durabilityBase()); err != nil {
		t.Fatalf("Commit(retry): %v", err)
	}
	if keptLines, keptMessages := store.Counts(); keptLines != 2 || keptMessages != 1 {
		t.Fatalf("retried commit kept %d lines and %d messages", keptLines, keptMessages)
	}
}

func TestTodo_ALIGN_037_Conformance(t *testing.T) {
	store := NewLedgerOutbox()
	commitID, lines, messages := align037Commit()
	receipt, err := store.Commit(durabilityTenant, commitID, lines, messages, durabilityBase())
	if err != nil {
		t.Fatal(err)
	}
	// The receipt accounts for every kept line and message, and the books
	// net to zero across the projection.
	keptLines, keptMessages := store.Counts()
	if receipt.Lines != keptLines || receipt.Messages != keptMessages {
		t.Fatalf("receipt %+v does not account for kept state", receipt)
	}
	for _, account := range []string{"cash", "revenue"} {
		balance, err := store.ProjectedBalance(account, "USD")
		if err != nil {
			t.Fatal(err)
		}
		_ = balance
	}
	cash, _ := store.ProjectedBalance("cash", "USD")
	revenue, _ := store.ProjectedBalance("revenue", "USD")
	total, err := cash.Add(revenue)
	if err != nil || total.AmountMinor != 0 {
		t.Fatalf("books net to %v, %v, want zero", total, err)
	}
}

func FuzzTodo_ALIGN_037_Fuzz(f *testing.F) {
	f.Add(int64(105000), uint8(0))
	f.Fuzz(func(t *testing.T, minor int64, sideByte uint8) {
		if minor < 0 {
			t.Skip("negative fuzz amounts are a separate refusal path")
		}
		side := "debit"
		if sideByte%2 == 1 {
			side = "credit"
		}
		other := "credit"
		if side == "credit" {
			other = "debit"
		}
		store := NewLedgerOutbox()
		_, firstErr := store.Commit(durabilityTenant, "fuzz",
			[]LedgerLine{
				{EntryID: "e1", Account: "cash", Side: side, Amount: usdMinor(minor)},
				{EntryID: "e2", Account: "revenue", Side: other, Amount: usdMinor(minor)},
			},
			[]OutboxMessage{
				{MessageID: "m1", EntryID: "e1", Destination: "payroll", PayloadDigest: "sha256:fuzz"},
			}, durabilityBase())
		if firstErr != nil {
			t.Fatalf("balanced fuzz commit failed: %v", firstErr)
		}
		if keptLines, keptMessages := store.Counts(); keptLines != 2 || keptMessages != 1 {
			t.Fatalf("balanced fuzz commit kept %d lines and %d messages", keptLines, keptMessages)
		}
	})
}
