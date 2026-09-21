package journeyclient

import "testing"

// UXLIVE-009's RED was measured on the running server: the journey's
// "Business reason" read `promotion_into_senior_hrbp_fix_verify`, a machine
// token printed where an approver expects the prose someone wrote.
//
// The stored value is not rewritten -- the page shows what the intent
// carries -- but a token-shaped value is presented in the identifier
// treatment rather than as a sentence, so a reader can see what it is.

// TestTodo_UXLIVE_009 is the primary red/green test.
func TestTodo_UXLIVE_009(t *testing.T) {
	tokens := []string{
		"promotion_into_senior_hrbp_fix_verify",
		"promotion-into-workplace-services-manager",
		"a_b",
	}
	for _, value := range tokens {
		if !tokenShaped(value) {
			t.Fatalf("%q is a machine token and is presented as prose", value)
		}
	}

	prose := []string{
		"Taking on the Atlanta team after the reorganisation.",
		"Reorganisation",
		"promotion into senior hrbp",
		"  ",
		"",
	}
	for _, value := range prose {
		if tokenShaped(value) {
			t.Fatalf("%q is prose and is presented as an identifier", value)
		}
	}
}
