package pilotprovider

import (
	"strings"
	"testing"
)

// TestPlaceholderCannotSatisfyRealProviderSelectionGateByValueNotFlag proves
// the property the todo's owner specifically required: the checked-in
// placeholder topology cannot satisfy a downstream gate that requires a
// real, procured, contracted provider selection, and that this is true by
// VALUE - inherent in the vendor id and vendor-confirmation fields - not
// merely because some boolean "is this a placeholder" flag says so. A gate
// implemented as "read a single IsPlaceholder flag" could be defeated by
// flipping that one flag; SatisfiesRealProviderSelectionGate cannot,
// because flipping SelectionStatus alone still leaves the placeholder
// suffix on VendorID and VendorConfirmation empty, both of which the gate
// checks independently.
func TestPlaceholderCannotSatisfyRealProviderSelectionGateByValueNotFlag(t *testing.T) {
	original := mustLoadTopology(t)

	t.Run("the real checked-in placeholder fails the gate on every substantive ground", func(t *testing.T) {
		ok, violations := original.SatisfiesRealProviderSelectionGate()
		if ok {
			t.Fatal("the checked-in PLACEHOLDER_UNVERIFIED topology must not satisfy the real provider selection gate")
		}
		wantFields := []string{
			"selection_status",
			"provider.vendor_id",
			"vendor_confirmation.confirmed_by_contact",
			"vendor_confirmation.contract_document_ref",
			"vendor_confirmation.signed_effective_date",
		}
		for _, field := range wantFields {
			found := false
			for _, v := range violations {
				if v.Field == field {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a gate violation on %q, got %v", field, violations)
			}
		}
	})

	t.Run("flipping only the selection_status flag still fails the gate on the vendor id and confirmation values", func(t *testing.T) {
		flagOnly := deepCopy(original)
		flagOnly.SelectionStatus = StatusVendorConfirmed // the "flag"

		ok, violations := flagOnly.SatisfiesRealProviderSelectionGate()
		if ok {
			t.Fatal("flipping selection_status alone must not satisfy the gate")
		}
		// selection_status itself no longer names a violation (the flag says
		// VENDOR_CONFIRMED now), but the value-level facts still do.
		for _, v := range violations {
			if v.Field == "selection_status" {
				t.Errorf("selection_status should no longer be the failing ground once the flag is flipped, but it still is: %v", violations)
			}
		}
		var gotFields []string
		for _, v := range violations {
			gotFields = append(gotFields, v.Field)
		}
		joined := strings.Join(gotFields, ",")
		for _, want := range []string{"provider.vendor_id", "vendor_confirmation.confirmed_by_contact", "vendor_confirmation.contract_document_ref", "vendor_confirmation.signed_effective_date"} {
			if !strings.Contains(joined, want) {
				t.Errorf("expected the value-level violation %q to remain after flipping only the flag, got %v", want, violations)
			}
		}
	})

	t.Run("only a complete, consistent real selection - flag and every value - satisfies the gate", func(t *testing.T) {
		real := deepCopy(original)
		real.SelectionStatus = StatusVendorConfirmed
		real.Provider.VendorID = "acme-hcm-production"
		real.VendorConfirmation = VendorConfirmation{
			ConfirmedByContact:  "Jane Procurement",
			ContractDocumentRef: "contracts/acme-hcm-msa-2027.pdf",
			SignedEffectiveDate: "2027-01-01",
		}
		ok, violations := real.SatisfiesRealProviderSelectionGate()
		if !ok {
			t.Fatalf("a complete real selection must satisfy the gate, got violations: %v", violations)
		}
	})
}
