package productdurability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-037: the ledger projection and the outbox persist atomically. One
// commit carries balanced double-entry lines and the outbound messages
// that describe them; either both land or neither does, so a downstream
// consumer never sees a message for a projection that was not kept, and a
// projection never hides entries no consumer was told about.

// Ledger errors.
var (
	ErrLedgerInvalid    = errors.New("productdurability: ledger commit is invalid")
	ErrLedgerUnbalanced = errors.New("productdurability: ledger commit does not balance")
	ErrLedgerAtomic     = errors.New("productdurability: ledger commit did not persist atomically")
)

// LedgerLine is one balanced entry line. Amounts are exact minor units;
// Side is "debit" or "credit".
type LedgerLine struct {
	EntryID string `json:"entry_id"`
	Account string `json:"account"`
	Side    string `json:"side"`
	Amount  Money  `json:"amount"`
}

// OutboxMessage is one outbound notice for a committed entry.
type OutboxMessage struct {
	MessageID     string `json:"message_id"`
	EntryID       string `json:"entry_id"`
	Destination   string `json:"destination"`
	PayloadDigest string `json:"payload_digest"`
}

// CommitReceipt is the durable proof of one atomic commit.
type CommitReceipt struct {
	Tenant      string `json:"tenant"`
	CommitID    string `json:"commit_id"`
	Lines       int    `json:"lines"`
	Messages    int    `json:"messages"`
	CommittedAt string `json:"committed_at"`
	Digest      string `json:"digest"`
}

// LedgerOutbox is the atomic ledger-plus-outbox store. The zero value is
// not usable; build one with NewLedgerOutbox. FaultAfterLedger is a test
// hook: when positive, the commit with that sequence fails after staging
// the ledger lines, proving the rollback keeps both sides empty.
type LedgerOutbox struct {
	mu               sync.Mutex
	commits          int
	lines            []LedgerLine
	messages         []OutboxMessage
	FaultAfterLedger int
}

// NewLedgerOutbox builds an empty store.
func NewLedgerOutbox() *LedgerOutbox {
	return &LedgerOutbox{}
}

// Commit validates, then persists lines and messages as one atomic unit.
// Debits must equal credits per currency; every message must name a
// committed line; everything is tenant-scoped to one tenant.
func (s *LedgerOutbox) Commit(tenant values.TenantId, commitID string, lines []LedgerLine, messages []OutboxMessage, at time.Time) (CommitReceipt, error) {
	if err := tenant.Validate(); err != nil {
		return CommitReceipt{}, fmt.Errorf("%w: tenant: %v", ErrLedgerInvalid, err)
	}
	if strings.TrimSpace(commitID) == "" {
		return CommitReceipt{}, fmt.Errorf("%w: commit id is required", ErrLedgerInvalid)
	}
	if len(lines) == 0 {
		return CommitReceipt{}, fmt.Errorf("%w: at least one ledger line is required", ErrLedgerInvalid)
	}
	if at.IsZero() {
		return CommitReceipt{}, fmt.Errorf("%w: commit time is required", ErrLedgerInvalid)
	}
	balances := make(map[string]int64)
	seenLines := make(map[string]bool, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.EntryID) == "" || strings.TrimSpace(line.Account) == "" {
			return CommitReceipt{}, fmt.Errorf("%w: entry id and account are required", ErrLedgerInvalid)
		}
		if line.Side != "debit" && line.Side != "credit" {
			return CommitReceipt{}, fmt.Errorf("%w: side %q is not debit or credit", ErrLedgerInvalid, line.Side)
		}
		if _, err := currencyScale(line.Amount.Currency); err != nil {
			return CommitReceipt{}, fmt.Errorf("%w: %v", ErrLedgerInvalid, err)
		}
		if seenLines[line.EntryID] {
			return CommitReceipt{}, fmt.Errorf("%w: duplicate entry %q", ErrLedgerInvalid, line.EntryID)
		}
		seenLines[line.EntryID] = true
		if line.Side == "debit" {
			balances[line.Amount.Currency] += line.Amount.AmountMinor
		} else {
			balances[line.Amount.Currency] -= line.Amount.AmountMinor
		}
	}
	for currency, net := range balances {
		if net != 0 {
			return CommitReceipt{}, fmt.Errorf("%w: %s nets %d minor units", ErrLedgerUnbalanced, currency, net)
		}
	}
	seenMessages := make(map[string]bool, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.MessageID) == "" || strings.TrimSpace(message.Destination) == "" || strings.TrimSpace(message.PayloadDigest) == "" {
			return CommitReceipt{}, fmt.Errorf("%w: message id, destination, and payload digest are required", ErrLedgerInvalid)
		}
		if !seenLines[message.EntryID] {
			return CommitReceipt{}, fmt.Errorf("%w: message %q names uncommitted entry %q", ErrLedgerInvalid, message.MessageID, message.EntryID)
		}
		if seenMessages[message.MessageID] {
			return CommitReceipt{}, fmt.Errorf("%w: duplicate message %q", ErrLedgerInvalid, message.MessageID)
		}
		seenMessages[message.MessageID] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits++
	stagedLines := len(s.lines)
	s.lines = append(s.lines, lines...)
	if s.FaultAfterLedger == s.commits {
		// Injected crash between the two persists: roll the staged
		// lines back so neither side is visible.
		s.lines = s.lines[:stagedLines]
		return CommitReceipt{}, fmt.Errorf("%w: commit %q lost its outbox", ErrLedgerAtomic, commitID)
	}
	s.messages = append(s.messages, messages...)
	raw := strings.Join([]string{tenant.String(), commitID, fmt.Sprint(len(lines)), fmt.Sprint(len(messages)), at.UTC().Format(time.RFC3339Nano)}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return CommitReceipt{
		Tenant: tenant.String(), CommitID: commitID,
		Lines: len(lines), Messages: len(messages),
		CommittedAt: at.UTC().Format(time.RFC3339Nano),
		Digest:      "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

// ProjectedBalance sums the kept lines of one account: debits add,
// credits subtract.
func (s *LedgerOutbox) ProjectedBalance(account, currency string) (Money, error) {
	if _, err := currencyScale(currency); err != nil {
		return Money{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var net int64
	for _, line := range s.lines {
		if line.Account != account || line.Amount.Currency != currency {
			continue
		}
		if line.Side == "debit" {
			net += line.Amount.AmountMinor
		} else {
			net -= line.Amount.AmountMinor
		}
	}
	return Money{AmountMinor: net, Currency: currency}, nil
}

// Counts reports kept lines and messages.
func (s *LedgerOutbox) Counts() (lines, messages int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lines), len(s.messages)
}
