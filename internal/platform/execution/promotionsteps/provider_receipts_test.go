package promotionsteps

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// receiptReaderFake proves the port is implementable by a store adapter with
// no dependency on the store package itself.
type receiptReaderFake map[string]ProviderReceipt

func (f receiptReaderFake) LatestForChange(_ context.Context, _ dbport.Tx, _ uuid.UUID, changeRef string) (ProviderReceipt, bool, error) {
	receipt, ok := f[changeRef]
	return receipt, ok, nil
}

var _ ProviderReceiptReader = receiptReaderFake{}

func TestProviderChangeRefsAreTheOutboxEffectIdentities(t *testing.T) {
	if got := PayrollChangeRef("rev-1"); got != "payroll:rev-1" {
		t.Fatalf("PayrollChangeRef = %q, want payroll:rev-1", got)
	}
	if got := AccessChangeRef("rev-1"); got != "iam:rev-1" {
		t.Fatalf("AccessChangeRef = %q, want iam:rev-1", got)
	}
	reader := receiptReaderFake{PayrollChangeRef("rev-1"): {Outcome: ProviderOutcomeApplied, Details: map[string]string{ProviderDetailAmount: "1"}}}
	got, found, err := reader.LatestForChange(context.Background(), nil, uuid.Nil, "payroll:rev-1")
	if err != nil || !found || got.Outcome != ProviderOutcomeApplied {
		t.Fatalf("LatestForChange = %+v, %t, %v", got, found, err)
	}
	if _, found, _ := reader.LatestForChange(context.Background(), nil, uuid.Nil, AccessChangeRef("rev-1")); found {
		t.Fatal("a change the provider never answered reported a receipt")
	}
}
