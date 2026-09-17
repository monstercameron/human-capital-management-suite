// Package program is PROGRAM-CONF-001's proof that Benefit, Bonus,
// Learning and Leave share one Program abstraction: definition, revision,
// population, eligibility, cycle, participation and outcome facets with a
// common digest vocabulary and declared variation points.
//
// It is a conformance fixture, not a domain implementation: the four
// fixtures are in-memory shapes this package owns, never live benefit,
// bonus, LMS or leave integrations. What is under test is the
// abstraction's own discipline: every domain binds every facet, shared
// status vocabularies stay shared, domain rule sets stay distinct (no
// mono-rule), and any forced or meaningless field rejects the abstraction
// instead of silently passing.
package program
