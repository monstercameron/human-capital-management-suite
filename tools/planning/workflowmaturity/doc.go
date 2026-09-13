// Package workflowmaturity implements the WF-DISC-012 maturity gate:
// an accepted BusinessIntent definition may only be reported CONTRACTED,
// IMPLEMENTED or VERIFIED when its design is bound, its high-level graph
// compiles, every model/engine/capability/authority reference it touches
// resolves to one owner, an adversarial scenario suite exists for it,
// every decision affecting it is resolved (not left OPEN_OWNED) and its
// todo/test/evidence trail is not dangling. Any one of those gaps caps the
// definition at CATALOGUED and names the exact blocker; the report never
// collapses per-item gaps into one aggregate boolean.
//
// # Composition, not re-derivation
//
// Every dimension below is judged by importing the package that already
// owns it and reading its own findings; workflowmaturity re-derives none
// of them:
//
//   - unbound design         -> tools/planning/workflowdesignjoin (WF-DISC-006):
//     the one-to-one join over accepted definitions.
//   - unresolved reference   -> tools/planning/designownership (WF-DISC-009):
//     model/engine/capability/authority ownership resolution; its own
//     Candidate list already "blocks CONTRACTED maturity" by name.
//   - unjustified omission   -> tools/planning/workflowexpansion (WF-DISC-007)
//     and tools/planning/workflowdesign (WF-DISC-005): a bare NOT_APPLICABLE
//     marker or an omitted/replaced recipe phase with no stated reason.
//   - missing adversarial scenario -> tools/planning/scenariomatrix
//     (WF-DISC-010): a generated scenario or a typed justification is
//     required for every declared dimension.
//   - dangling todo/test/evidence edge and current evidence ->
//     tools/planning/intentcoverage (GOV-026): its own orphan kinds
//     intent_workflow_or_direct, intent_test, intent_evidence and
//     todo_direct_dangling, and its bottom-up CONCEPTUAL..VERIFIED ladder.
//   - only a catalog name/archetype label -> tools/planning/workflowarchetypes
//     (WF-DISC-002): an intent placed in one of the six HR catalogs with no
//     design record at all is exactly that condition, distinct from a design
//     record that exists but joined incorrectly.
//
// [Reconcile] is a pure function over an already-assembled [Snapshot]; all
// live filesystem/repository loading (six sibling loaders plus
// tools/planning/intentcoverage's own repository loader) lives in
// loader.go, exactly the split every sibling workflow* package already
// uses.
//
// # The delivery-manifest question, asked and answered
//
// PHASE-001 found that a "final" manifest (p1a-manifest.yaml) had already
// been published before the independent scope ceiling it should have been
// selected from existed, and the fix was to build the ceiling from
// independent sources and check the manifest against it, never the
// reverse. WF-DISC-012 is the same genus of question: does anything
// already assert per-intent workflow maturity by hand?
//
// The answer here is planning/workflows/catalog.md: 268 user-flow sections
// each carry a hand-typed "**Status**: `EXISTING`|`NEW`|`PARTIAL`" line
// with prose evidence citations, and many of those sections name one of
// the fourteen accepted definitions this gate covers. That is exactly the
// "manually maintained status prose" the REFACTOR clause names.
// [ParseCatalogClaims] reads that prose (never treating it as an input to
// the gate itself) and [CrossCheckCatalog] reports every accepted
// definition where a catalog.md section claims EXISTING but the generated
// gate cannot support CONTRACTED - the disagreement is the deliverable,
// not something to reconcile away. Going forward, any future delivery
// manifest that wants per-intent workflow maturity should read
// [Reconcile]'s [Report] instead of typing a Status line by hand; that is
// the whole of what REFACTOR asks this package to make possible.
package workflowmaturity
