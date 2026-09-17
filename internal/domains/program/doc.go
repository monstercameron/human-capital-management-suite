// Package program owns the governed Program vocabulary for Benefit,
// Bonus, Learning and Leave: one ProgramDefinition per program, exact
// effective revisions, explicit population/eligibility/cycle bindings, a
// governed participate/enroll/withdraw lifecycle, explainable outcomes
// computed only through registered formulas, and explicit reconciliation
// with domain-owned repair.
//
// Definitions are immutable once recorded, revisions resolve exactly for
// a tenant/org/jurisdiction/effective-known context (never ambient
// latest), and no domain-specific formula hides in the calculation core.
package program
