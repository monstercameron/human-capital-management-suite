package balance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PostingReceipt identifies the opening and ending heads plus the unused
// remainder of one atomic commit.
type PostingReceipt struct {
	PlanID          string
	OpeningHead     string
	EndingHead      string
	UnusedRemainder string
	Entries         int
	Digest          string
}

func commitDigest(plan PostingPlan, key string) string {
	parts := []string{"balance-commit", plan.Digest, key}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Injector is the test-controlled failpoint: production passes nil.
type Injector func(step string) error

// BusinessTransaction is the owning transaction: one mutex-guarded head
// per account with an idempotency registry. Every planned entry commits
// under it once, or none do. Repeated transactions return the original
// receipt; partial and double debits are impossible.
type BusinessTransaction struct {
	mu                sync.Mutex
	heads             map[string]string
	receipts          map[string]PostingReceipt
	transfers         map[string]TransferReceipt
	transferKeyOwners map[string]string
}

// TransferReceipt seals both account settlements under one transaction key.
type TransferReceipt struct {
	Source      PostingReceipt
	Destination PostingReceipt
	Digest      string
}

// NewBusinessTransaction starts an empty transaction coordinator.
func NewBusinessTransaction() *BusinessTransaction {
	return &BusinessTransaction{heads: make(map[string]string), receipts: make(map[string]PostingReceipt), transfers: make(map[string]TransferReceipt), transferKeyOwners: make(map[string]string)}
}

// CommitTransfer verifies both linked plans and advances both account heads
// while holding the transaction lock. Any validation failure leaves both heads untouched.
func (tx *BusinessTransaction) CommitTransfer(plan TransferPlan, key string) (TransferReceipt, error) {
	if tx == nil || !plan.Verify() || strings.TrimSpace(key) == "" {
		return TransferReceipt{}, fmt.Errorf("balance: invalid transfer plan or key")
	}
	return tx.commitLinkedPair(plan.Source, plan.Destination, plan.Amount, key)
}

// commitLinkedPair is the shared atomic settlement core. Public TransferPlan
// only admits debit-to-credit direction; the correction Transfer operation
// also uses this core for its explicitly different contra/reissue semantics.
func (tx *BusinessTransaction) commitLinkedPair(sourcePlan, destinationPlan PostingPlan, amount values.Decimal, key string) (TransferReceipt, error) {
	if tx == nil || strings.TrimSpace(key) == "" || !sourcePlan.Verify() || !destinationPlan.Verify() {
		return TransferReceipt{}, fmt.Errorf("balance: invalid linked posting pair or key")
	}
	if len(sourcePlan.Entries) != 1 || len(destinationPlan.Entries) != 1 {
		return TransferReceipt{}, fmt.Errorf("balance: linked transfer requires one entry per leg")
	}
	sourceEntry, destinationEntry := sourcePlan.Entries[0], destinationPlan.Entries[0]
	if sourceEntry.Kind == destinationEntry.Kind || sourceEntry.AccountID == destinationEntry.AccountID || !sourceEntry.Amount.Equal(destinationEntry.Amount) || !sourceEntry.Amount.Equal(amount) || !sourcePlan.Accepted.Equal(amount) || !destinationPlan.Accepted.Equal(amount) {
		return TransferReceipt{}, fmt.Errorf("balance: linked posting legs must be opposite, equal, and fully accepted")
	}
	if sourceEntry.SourceTransactionID != key || destinationEntry.SourceTransactionID != key || sourceEntry.IdempotencyKey != key+"/source" || destinationEntry.IdempotencyKey != key+"/destination" {
		return TransferReceipt{}, fmt.Errorf("balance: transfer key does not match sealed leg identities")
	}
	digest := linkedPairDigest(sourcePlan, destinationPlan, amount)
	keys := []string{key, key + "/source", key + "/destination"}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.transferKeyOwners == nil {
		tx.transferKeyOwners = make(map[string]string)
	}
	for _, reserved := range keys {
		if owner, exists := tx.transferKeyOwners[reserved]; exists && owner != key {
			return TransferReceipt{}, fmt.Errorf("balance: transfer idempotency key or leg key is already reserved")
		}
		if _, exists := tx.receipts[reserved]; exists {
			return TransferReceipt{}, fmt.Errorf("balance: transfer idempotency key or leg key is already used by a posting")
		}
	}
	if prior, ok := tx.transfers[key]; ok {
		if prior.Digest != digest {
			return TransferReceipt{}, fmt.Errorf("balance: transfer idempotency key conflict")
		}
		return prior, nil
	}
	if sourcePlan.AccountID == destinationPlan.AccountID {
		return TransferReceipt{}, fmt.Errorf("balance: transfer accounts must differ")
	}
	for _, leg := range []PostingPlan{sourcePlan, destinationPlan} {
		head, ok := tx.heads[leg.AccountID]
		if !ok || head != leg.Opening.String() {
			return TransferReceipt{}, fmt.Errorf("balance: stale or missing transfer account head %s", leg.AccountID)
		}
	}
	if sourceEntry.AccountID != sourcePlan.AccountID || destinationEntry.AccountID != destinationPlan.AccountID {
		return TransferReceipt{}, fmt.Errorf("balance: transfer entries escape their accounts")
	}
	source := PostingReceipt{PlanID: sourcePlan.DefinitionID, OpeningHead: sourcePlan.Opening.String(), EndingHead: sourcePlan.Ending.String(), UnusedRemainder: sourcePlan.Remainder.String(), Entries: 1, Digest: commitDigest(sourcePlan, key+"/source")}
	destination := PostingReceipt{PlanID: destinationPlan.DefinitionID, OpeningHead: destinationPlan.Opening.String(), EndingHead: destinationPlan.Ending.String(), UnusedRemainder: destinationPlan.Remainder.String(), Entries: 1, Digest: commitDigest(destinationPlan, key+"/destination")}
	tx.heads[sourcePlan.AccountID] = sourcePlan.Ending.String()
	tx.heads[destinationPlan.AccountID] = destinationPlan.Ending.String()
	for _, reserved := range keys {
		tx.transferKeyOwners[reserved] = key
	}
	receipt := TransferReceipt{Source: source, Destination: destination, Digest: digest}
	tx.transfers[key] = receipt
	return receipt, nil
}

// SeedHead sets one account head for tests and bootstrapping.
func (tx *BusinessTransaction) SeedHead(account, head string) {
	if tx == nil {
		return
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.heads[account] = head
}

// CommitPosting posts one planned BalancePlan atomically with its owning
// business transaction. The plan must verify (changed plans and
// proposals refuse), the account head must match the plan opening (stale
// heads refuse), and any injected failure between the leave fact and one
// of the entries rolls everything back.
func (tx *BusinessTransaction) CommitPosting(plan PostingPlan, idempotencyKey string, inject Injector) (PostingReceipt, error) {
	if tx == nil {
		return PostingReceipt{}, fmt.Errorf("balance: nil business transaction")
	}
	if !plan.Verify() {
		return PostingReceipt{}, fmt.Errorf("balance: changed plan refuses commit")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return PostingReceipt{}, fmt.Errorf("balance: idempotency key is required")
	}
	if len(plan.Entries) == 0 {
		return PostingReceipt{}, fmt.Errorf("balance: plan carries no entries")
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if owner, exists := tx.transferKeyOwners[idempotencyKey]; exists {
		return PostingReceipt{}, fmt.Errorf("balance: idempotency key is reserved by transfer %s", owner)
	}
	if _, exists := tx.transfers[idempotencyKey]; exists {
		return PostingReceipt{}, fmt.Errorf("balance: idempotency key is already used by a transfer")
	}
	if prior, done := tx.receipts[idempotencyKey]; done {
		if prior.Digest != commitDigest(plan, idempotencyKey) {
			return PostingReceipt{}, fmt.Errorf("balance: posting idempotency key conflict")
		}
		return prior, nil
	}
	head, ok := tx.heads[plan.AccountID]
	if !ok {
		return PostingReceipt{}, fmt.Errorf("balance: account %s has no head", plan.AccountID)
	}
	if head != plan.Opening.String() {
		return PostingReceipt{}, fmt.Errorf("balance: stale account head")
	}
	fail := func(step string) error {
		if inject == nil {
			return nil
		}
		return inject(step)
	}
	if err := fail("leave-fact"); err != nil {
		return PostingReceipt{}, fmt.Errorf("balance: rolled back at the leave fact: %v", err)
	}
	for i, entry := range plan.Entries {
		if entry.AccountID != plan.AccountID {
			return PostingReceipt{}, fmt.Errorf("balance: entry %d escapes account %s", i, plan.AccountID)
		}
		if err := fail(fmt.Sprintf("entry:%d", i)); err != nil {
			return PostingReceipt{}, fmt.Errorf("balance: rolled back at entry %d: %v", i, err)
		}
	}
	if err := fail("commit"); err != nil {
		return PostingReceipt{}, fmt.Errorf("balance: rolled back at commit: %v", err)
	}
	tx.heads[plan.AccountID] = plan.Ending.String()
	receipt := PostingReceipt{
		PlanID: plan.DefinitionID, OpeningHead: plan.Opening.String(), EndingHead: plan.Ending.String(),
		UnusedRemainder: plan.Remainder.String(), Entries: len(plan.Entries),
	}
	receipt.Digest = commitDigest(plan, idempotencyKey)
	tx.receipts[idempotencyKey] = receipt
	return receipt, nil
}

// Head reports the current account head.
func (tx *BusinessTransaction) Head(account string) (string, bool) {
	if tx == nil {
		return "", false
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	head, ok := tx.heads[account]
	return head, ok
}

// Verify recomputes the receipt seal.
func (receipt PostingReceipt) Verify(plan PostingPlan, key string) error {
	if receipt.Digest == "" || commitDigest(plan, key) != receipt.Digest {
		return fmt.Errorf("balance: posting receipt seal is broken")
	}
	return nil
}
