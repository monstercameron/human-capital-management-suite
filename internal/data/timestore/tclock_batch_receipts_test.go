package timestore

import (
	"context"
	"testing"
)

func TestLookupWorkflowReceiptContextRejectsInvalidInput(t *testing.T) {
	_, found, err := (&Store{}).LookupWorkflowReceiptContext(context.Background(), "", "device", 1)
	if found || err != ErrInvalid {
		t.Fatalf("found=%v err=%v, want invalid", found, err)
	}
}
