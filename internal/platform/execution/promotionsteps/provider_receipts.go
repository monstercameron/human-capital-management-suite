package promotionsteps

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Provider receipt outcomes a [ProviderReceipt] carries. The payroll provider
// answers APPLIED, the identity provider GRANTED; either may answer REJECTED.
const (
	ProviderOutcomeApplied  = "APPLIED"
	ProviderOutcomeGranted  = "GRANTED"
	ProviderOutcomeRejected = "REJECTED"
)

// Provider receipt detail keys the promotion observations compare against the
// committed command.
const (
	ProviderDetailAmount        = "amount"
	ProviderDetailCurrency      = "currency"
	ProviderDetailEffectiveDate = "effective_date"
	ProviderDetailJobCode       = "job_code"
	ProviderDetailGrade         = "grade"
)

// ProviderReceipt is the latest confirmation a downstream provider returned
// for one outbound change: its outcome and the values it says it applied.
type ProviderReceipt struct {
	Outcome string
	Details map[string]string
}

// ProviderReceiptReader is the port the 1.1.0 promotion observations read
// provider confirmations through, so observation does not depend on the store
// that records them. changeRef is the outbox effect identity of the change
// ([PayrollChangeRef], [AccessChangeRef]); found is false when the provider
// has not answered. The read runs inside the observation's tenant-scoped
// read transaction tx.
type ProviderReceiptReader interface {
	LatestForChange(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, changeRef string) (ProviderReceipt, bool, error)
}

// PayrollChangeRef is the outbox effect identity of a promotion's payroll
// change: the ref the payroll provider's receipt is recorded against.
func PayrollChangeRef(proposalRevisionID string) string { return "payroll:" + proposalRevisionID }

// AccessChangeRef is the outbox effect identity of a promotion's identity
// (IAM) change: the ref the identity provider's receipt is recorded against.
func AccessChangeRef(proposalRevisionID string) string { return "iam:" + proposalRevisionID }
