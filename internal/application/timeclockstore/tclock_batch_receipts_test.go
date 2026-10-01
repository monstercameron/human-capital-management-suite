package timeclockstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

func TestAdapterLookupWorkflowReceipt_requiresDurableStore(t *testing.T) {
	_, found, err := (WorkflowReceiptLookupAdapter{}).LookupWorkflowReceipt(context.Background(), "tenant", "device", 1)
	if found || !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("found=%v err=%v, want unavailable", found, err)
	}
}
