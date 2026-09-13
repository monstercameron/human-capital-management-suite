// Package pilotprovider implements SELECT-002's signed pilot provider
// topology. See topology.go's package comment for the schema; this file
// records why the checked-in topology names a structurally-marked
// placeholder vendor rather than a real one.
//
// # No provider was ever selected
//
// WEDGE-001 selected a problem (a Promotion/Compensation-change failure
// class), never a partner. No named provider - with product, edition,
// region, API entitlements, quotas, credential model or data-processing
// terms - exists anywhere in this repository prior to this package. Given
// that gap, the choice put to the product owner was: (a) leave SELECT-002
// blocked until a real design-partner system is under contract, or (b)
// author a topology now against a named placeholder so NEXT-002 and its
// downstream gates have something to consume, with the placeholder's
// non-authority made structurally unmistakable. The owner chose (b).
//
// This package refuses to assert real product, edition, quota, credential
// or data-processing facts about an actual vendor in a signed governance
// artifact - that would be fabricated evidence, not a placeholder. Instead
// every fact below is either a documented illustrative value or an honest
// declaration that the fact does not yet exist (see DataProcessingTerms and
// VendorConfirmation below).
//
// # The placeholder vendor
//
// The checked-in topology models integration-platform.md's own Initial
// Named Connector Hypothesis #1 ("One major HCM used by the first design
// partners, likely Workday or an equivalent incumbent") and the Core HCM
// connector family it names (Workday, UKG, Oracle, SAP, Dayforce). It does
// NOT name Workday, UKG, or any other real company: doing so would read as
// an assertion of a real contract, edition and quota with that company,
// which nobody has negotiated. The vendor identity is instead
//
//	vendor_id:            legacyhcm-incumbent-PLACEHOLDER-UNVERIFIED
//	vendor_display_name:  "Unselected Incumbent Core HCM (Workday-class
//	                       placeholder, not a real vendor)"
//
// [PlaceholderSuffix] ("-PLACEHOLDER-UNVERIFIED") is mandatory and
// Validate-enforced on any VendorID while [SelectionStatus] is
// [StatusPlaceholderUnverified] - the provider-topology analogue of
// definitions/legal/packs/states/us-ca.json's "-draft" pack_id suffix. A
// human or program cannot mistake this vendor_id for a real one without
// deliberately ignoring the suffix in its own name.
//
// # The selection-status marker Validate enforces
//
// [SelectionStatus] is a required, closed field: [StatusPlaceholderUnverified]
// or [StatusVendorConfirmed]. [ProviderTopology.Validate] refuses a topology
// whose declared status disagrees with its own vendor-identity and
// [VendorConfirmation] fields in either direction - a PLACEHOLDER_UNVERIFIED
// topology may not carry a partially populated VendorConfirmation (that
// would be pretending part of a review happened), and a VENDOR_CONFIRMED
// topology may not keep the placeholder suffix or leave VendorConfirmation
// empty (that would be claiming confirmation with no evidence of it). This
// mirrors the way [github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction.Reviewer]
// is a required-but-honestly-empty field on SELECT-001's checked-in profile.
//
// # Structurally incapable of satisfying a real-selection gate
//
// [ProviderTopology.SatisfiesRealProviderSelectionGate] is the check any
// downstream gate (NEXT-002, or a future release gate) must run before
// trusting a ProviderTopology as a procured, contracted selection. It is
// deliberately independent of Validate and checks by value, not by flag:
// even a topology that quietly flips SelectionStatus to VENDOR_CONFIRMED
// still fails the gate as long as Provider.VendorID keeps the placeholder
// suffix or VendorConfirmation stays unpopulated. security_test.go proves
// the checked-in topology fails this gate today, and that flipping the
// status flag alone does not change that.
//
// # Honest incompleteness in the data-processing terms
//
// RED requires "data-processing terms" be present. The checked-in
// DataProcessing.DPAReference does not invent a contract number; it states
// plainly that no data-processing agreement has been executed and this
// field must be replaced with a real reference before pilot use. The same
// honesty applies to every other placeholder fact: they are illustrative
// values chosen to exercise this schema's structure (exact quotas, fault
// classes, credential rotation), not claims about a real vendor's actual
// terms.
//
// # REFACTOR: provider identity never leaks into semantic code
//
// scan.go's [ScanForIdentityLeak] and refactor_test.go's
// TestProviderIdentityNeverLeaksIntoSemanticWorkflowsOrCapabilities enforce
// REFACTOR's clause ("provider-specific facts remain configuration and
// adapter qualification inputs; semantic workflows and capabilities remain
// provider-neutral") against the real internal/ source tree: only
// internal/connectivity (the connector/adapter boundary
// specs/integration-platform.md defines) may reference this topology's
// vendor_id. Every other internal/ package failing that scan would be
// exactly the coupling REFACTOR forbids.
//
// # Swapping in a real vendor is a data change
//
// Nothing in this package, definitions/planning/gates/select-002-provider-topology.yaml,
// or any consumer is specific to the placeholder's field values. Replacing
// the placeholder with a real, contracted vendor means: rewriting the YAML
// file's provider/vendor_confirmation/api_entitlements/... fields, flipping
// selection_status to VENDOR_CONFIRMED, and re-signing - no Go code changes
// and no rebuild. mutation_test.go's "flip to VENDOR_CONFIRMED with a real
// vendor id and full confirmation" case exercises exactly this path against
// [ProviderTopology.SatisfiesRealProviderSelectionGate].
package pilotprovider
