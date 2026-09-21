package demoworkforce

import "testing"

func TestPayBandSpecsAreExactAndCoverEveryPublishedRole(t *testing.T) {
	specs, err := PayBandSpecs()
	if err != nil {
		t.Fatal(err)
	}
	// Every role the company publishes, staffed or not: an unstaffed
	// promotion target still has to be priced in every pay zone, or the band
	// lookup that governs the move into it finds nothing.
	want := len(allRoles()) * len(PayZones())
	if len(specs) != want {
		t.Fatalf("got %d band specs, want %d", len(specs), want)
	}
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if seen[spec.ID] || spec.Version != PayBandPolicyVersion {
			t.Fatalf("duplicate or unversioned band %q", spec.ID)
		}
		seen[spec.ID] = true
		if spec.Minimum.Currency() != "USD" || spec.Midpoint.Currency() != "USD" || spec.Maximum.Currency() != "USD" ||
			spec.Minimum.Amount().Cmp(spec.Midpoint.Amount()) >= 0 || spec.Maximum.Amount().Cmp(spec.Midpoint.Amount()) <= 0 {
			t.Fatalf("invalid range for %s: %s..%s..%s", spec.ID, spec.Minimum, spec.Midpoint, spec.Maximum)
		}
	}
}
