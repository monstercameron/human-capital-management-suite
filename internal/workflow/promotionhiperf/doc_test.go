package promotionhiperf

import (
	"strings"
	"testing"
)

// TestDoc_VariantIdentityIsDistinct proves the documented package contract:
// the variant publishes its own workflow identity and version, never the
// execute plan's.
func TestDoc_VariantIdentityIsDistinct(t *testing.T) {
	if !strings.HasPrefix(WorkflowID, "hcmnext.workflows.promotion.") {
		t.Fatalf("WorkflowID = %q, want the promotion workflow namespace", WorkflowID)
	}
	if WorkflowID == "hcmnext.workflows.promotion.execute" {
		t.Fatal("the variant must not publish the execute workflow identity")
	}
	if Version == 0 || SemanticVersion == "" || NodeFetchMarketRate == "" || CapabilityMarketRate == "" {
		t.Fatal("the variant identity, fetch node and market capability are all named")
	}
}
