// Package limits provides deterministic mention admission for agent personas.
//
// The package owns the policy and reservation contract, while Store is the
// atomic persistence seam. MemoryStore is suitable for tests and single-
// process compositions only; a production adapter must persist the same
// compare-and-reserve transitions.
package limits
