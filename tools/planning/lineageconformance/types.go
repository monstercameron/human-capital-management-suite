// Package lineageconformance proves DATA-022: every accepted intent
// family, composite child and trigger path has complete, tenant-scoped,
// redaction-safe and rebuildable lineage from intent through proposal,
// workflow, transaction, event, projection, outbox, effect, observation,
// reconciliation, repair and correction -- or it says exactly which link
// it cannot prove.
//
// Cases are never hand-listed. [GenerateCases] derives one ROOT case per
// SLICE-016 closure witness, one CHILD case per composite child a
// definition's MODEL-016 binding declares, and one TRIGGER case per
// non-interactive initiator a definition allows. Which links a case must
// carry is derived from the definition's own declarations (side-effect
// profile, proposal-binding, compensation and correction rules), and a
// link is NOT_APPLICABLE only when the definition says so, with the field
// quoted as the reason.
//
// Two evaluators share one status rule. [Evaluate] runs the shared lineage
// assertions over an actual lineage graph (records assembled from owner
// stores or from an owner's lineage service such as internal/data/lineage):
// completeness, tenant scope, causation and digest chaining, exact
// watermarks, redaction safety for the reader, projection rebuildability
// and append-only correction. [Compile] builds the live per-family report
// from the witnesses and the lineage producers the tree actually holds;
// a link no validated producer implements is UNKNOWN, and a case with
// unknown links is PARTIAL or UNKNOWN, never COMPLETE. [VerifyReport]
// recomputes every status and aggregate so a report claiming completion
// over a missing link is refused.
//
// Domain-specific events stay with their owners: this package only maps
// owner records onto the shared link vocabulary.
package lineageconformance

import "slices"

// SchemaVersion is the report's own format version.
const SchemaVersion = 1

// Link is one hop of the universal lineage chain.
type Link string

// The twelve chain links, plus CAUSATION, which heads every CHILD and
// TRIGGER case and names what caused the intent to exist.
const (
	LinkCausation      Link = "CAUSATION"
	LinkIntent         Link = "INTENT"
	LinkProposal       Link = "PROPOSAL"
	LinkWorkflow       Link = "WORKFLOW"
	LinkTransaction    Link = "TRANSACTION"
	LinkEvent          Link = "EVENT"
	LinkProjection     Link = "PROJECTION"
	LinkOutbox         Link = "OUTBOX"
	LinkEffect         Link = "EFFECT"
	LinkObservation    Link = "OBSERVATION"
	LinkReconciliation Link = "RECONCILIATION"
	LinkRepair         Link = "REPAIR"
	LinkCorrection     Link = "CORRECTION"
)

// ChainLinks returns the twelve chain links in lineage order.
func ChainLinks() []Link {
	return []Link{
		LinkIntent, LinkProposal, LinkWorkflow, LinkTransaction, LinkEvent,
		LinkProjection, LinkOutbox, LinkEffect, LinkObservation,
		LinkReconciliation, LinkRepair, LinkCorrection,
	}
}

// rank orders links: CAUSATION first, then the chain; -1 when unknown.
func (l Link) rank() int {
	if l == LinkCausation {
		return 0
	}
	if i := slices.Index(ChainLinks(), l); i >= 0 {
		return i + 1
	}
	return -1
}

// Valid reports whether l is CAUSATION or one of the twelve chain links.
func (l Link) Valid() bool { return l.rank() >= 0 }

// PathKind names how a case's intent came to exist.
type PathKind string

// Path kinds.
const (
	PathRoot    PathKind = "ROOT"
	PathChild   PathKind = "CHILD"
	PathTrigger PathKind = "TRIGGER"
)

// Status is a case's or report's lineage verdict.
type Status string

// Statuses. COMPLETE only when every required link is PROVEN and no
// defect exists; DEFECTIVE whenever any defect exists; PARTIAL when some
// downstream link is proven and some is unknown; UNKNOWN otherwise.
const (
	StatusComplete  Status = "COMPLETE"
	StatusPartial   Status = "PARTIAL"
	StatusUnknown   Status = "UNKNOWN"
	StatusDefective Status = "DEFECTIVE"
)

// Statuses returns every status in report order.
func Statuses() []Status {
	return []Status{StatusComplete, StatusPartial, StatusUnknown, StatusDefective}
}

// LinkState is one required link's state on one case.
type LinkState string

// Link states.
const (
	StateProven        LinkState = "PROVEN"
	StateNotApplicable LinkState = "NOT_APPLICABLE"
	StateUnknown       LinkState = "UNKNOWN"
	StateDefect        LinkState = "DEFECT"
)

// Finding codes. CodeLinkMissing, CodeProducerInvalid and
// CodeNoProducer make a link UNKNOWN; every other code is a defect.
const (
	CodeLinkMissing        = "LINK_MISSING"
	CodeNoProducer         = "NO_LINEAGE_PRODUCER"
	CodeProducerInvalid    = "PRODUCER_INVALID"
	CodeLinkDuplicate      = "LINK_DUPLICATE"
	CodeCrossTenant        = "CROSS_TENANT"
	CodeOverDisclosed      = "OVER_DISCLOSED"
	CodeRedactionReason    = "REDACTION_REASON_MISSING"
	CodeBrokenCausation    = "BROKEN_CAUSATION"
	CodeDigestMismatch     = "DIGEST_MISMATCH"
	CodeWatermarkMissing   = "WATERMARK_MISSING"
	CodeNotRebuildable     = "NOT_REBUILDABLE"
	CodeHistoryRewritten   = "HISTORY_REWRITTEN"
	CodeHistoryBroken      = "HISTORY_BROKEN"
	CodeTriggerMismatch    = "TRIGGER_MISMATCH"
	CodeNotApplicableFound = "NOT_APPLICABLE_CONTRADICTED"
	CodeUnknownLink        = "UNKNOWN_LINK"
	CodeDefinitionAbsent   = "DEFINITION_ABSENT"
	CodeChildNotAccepted   = "CHILD_NOT_ACCEPTED"
	CodeProducerOrphan     = "PRODUCER_ORPHAN"
)

// isGap reports whether a finding code only leaves a link unproven rather
// than proving a defect.
func isGap(code string) bool {
	switch code {
	case CodeLinkMissing, CodeNoProducer, CodeProducerInvalid, CodeDefinitionAbsent, CodeChildNotAccepted:
		return true
	}
	return false
}

// Finding is one exact reason a case or report is not complete.
type Finding struct {
	Case   string `json:"case"`
	Link   Link   `json:"link,omitempty"`
	Code   string `json:"code"`
	Record string `json:"record,omitempty"`
	Detail string `json:"detail"`
}

// NotApplicable is one link the definition itself declares absent.
type NotApplicable struct {
	Link   Link   `json:"link"`
	Reason string `json:"reason"`
}

// Case is one generated lineage obligation.
type Case struct {
	ID            string          `json:"id"`
	Definition    string          `json:"definition"`
	DisplayName   string          `json:"display_name"`
	Family        string          `json:"family"`
	Path          PathKind        `json:"path"`
	Parent        string          `json:"parent,omitempty"`
	Trigger       string          `json:"trigger,omitempty"`
	WitnessDigest string          `json:"witness_digest"`
	WitnessResult string          `json:"witness_result"`
	Required      []Link          `json:"required"`
	NotApplicable []NotApplicable `json:"not_applicable"`
}

// LinkStatus is one link's state with the evidence behind it.
type LinkStatus struct {
	Link     Link      `json:"link"`
	State    LinkState `json:"state"`
	Detail   string    `json:"detail"`
	Evidence []string  `json:"evidence,omitempty"`
}

// CaseResult is one case's verdict.
type CaseResult struct {
	Case     Case         `json:"case"`
	Status   Status       `json:"status"`
	Links    []LinkStatus `json:"links"`
	Findings []Finding    `json:"findings"`
}

// Report is the per-family lineage report.
type Report struct {
	SchemaVersion int                         `json:"schema_version"`
	AsOf          string                      `json:"as_of"`
	WitnessDigest string                      `json:"witness_digest"`
	Complete      bool                        `json:"complete"`
	Counts        map[Status]int              `json:"counts"`
	ByPath        map[PathKind]map[Status]int `json:"by_path"`
	Cases         []CaseResult                `json:"cases"`
	Findings      []Finding                   `json:"findings"`
	Digest        string                      `json:"digest"`
}
