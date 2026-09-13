// Package extract turns the fifty-one state employment-law research files under
// planning/research/state-employment-law into checked-in rule-pack
// definition files under definitions/legal/packs/states.
//
// # What it is allowed to do
//
// The contract's section 7.1 states the one-way rule this package exists to
// enforce: extraction is mechanical and adversarial in one direction only —
// an extractor may narrow a research claim or drop it, never broaden it. Three
// mechanisms hold that line.
//
// First, the [Matrix] gates which kinds a state's pack may carry. A cell
// marked F produces no obligation of that kind at all, so a keyword that
// happens to appear in a Florida file can never manufacture a Florida
// pay-transparency duty. A cell marked L is a locality rule and a
// subdivision-level pack does not carry it. A cell marked P produces a
// [legal.PreemptionAssertion], not an obligation.
//
// Second, an unstated field stays unstated. Every typed body accepts an empty
// day count, floor amount or enumerated list, and the definition file records
// the gap rather than filling it with a plausible number. Only a pack claiming
// a releasable review status has to close those gaps.
//
// Third, a research claim written as a recommendation is extracted as
// [legal.RuleStandardRecommended] with [legal.ConfidenceMarkerVerify], never
// as a statutory requirement.
//
// # Determinism
//
// Every step is a pure function of file bytes: the contract file supplies the
// matrix, the research file supplies the evidence, and nothing consults a
// clock, a network, a map iteration order or the filesystem beyond reading
// those inputs. Regenerating the fifty-one files therefore produces byte-identical
// output, which is what TestTodo_LEGAL_011_Golden asserts.
//
// # What the output is not
//
// Every definition this package writes carries ReviewStatus UNREVIEWED. The
// research is agent-drafted, no counsel has reviewed it, and a pack at
// UNREVIEWED is unusable under any nonzero tenant review floor. None of it is
// legal advice.
//
// Owner: governance-and-trust. Tickets: LEGAL-010, LEGAL-011.
package extract
