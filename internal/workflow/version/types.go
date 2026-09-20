package version

import "time"

// ActivationStatus is a compiled workflow version's lifecycle status.
// Publishing mints DRAFT; every other status is reached only through a
// governed lifecycle call in this package, never by editing the field.
type ActivationStatus string

// The four declared activation statuses. There is no fifth: PUBLISHED and
// APPROVED are pipeline steps that lead here, not resting states — a
// [CompiledVersion] records the evidence for each of them (see
// [PublishMeta] and [ApprovalRecord]), but the status it carries is always
// one of these.
const (
	// StatusDraft is a published-but-unauthorized version: it exists,
	// verifiably, but no instance may pin it as active.
	StatusDraft ActivationStatus = "DRAFT"
	// StatusActive is a version an authorized [Activate] call approved. At
	// most one version per workflow is active at a time.
	StatusActive ActivationStatus = "ACTIVE"
	// StatusQuarantined is an active version pulled from new starts pending
	// investigation. It is reversible only through a further [Activate].
	StatusQuarantined ActivationStatus = "QUARANTINED"
	// StatusRetired ends normal availability. Unlike quarantine, retirement
	// is not reversible through [Activate].
	StatusRetired ActivationStatus = "RETIRED"
)

// VersionRef names one exact compiled workflow version by the identity a pin
// or a dependency declaration uses: the workflow it belongs to and the
// compiled-plan digest that is its content identity.
type VersionRef struct {
	WorkflowID         string `json:"workflow_id"`
	CompiledPlanDigest string `json:"compiled_plan_digest"`
}

// ApprovalRecord is one authorization decision recorded against a version.
// [Activate] appends one on every successful call, so a version quarantined
// and later reactivated carries the full approval history, not just the
// latest decision.
type ApprovalRecord struct {
	ApprovedBy string    `json:"approved_by"`
	Authority  string    `json:"authority"`
	Reason     string    `json:"reason,omitempty"`
	ApprovedAt time.Time `json:"approved_at"`
	// Result is the status the approval moved the version to (ACTIVE or
	// QUARANTINED), so the history reads as a timeline rather than a pile of
	// undated approvals.
	Result ActivationStatus `json:"result"`
}

// PublishMeta is the caller-supplied provenance for one publication. It
// carries no digest and no status: [Publish] mints both, so a caller cannot
// hand this package a manufactured identity.
type PublishMeta struct {
	// SemanticVersion is the human-facing SemVer 2.0.0 identity, e.g. "1.4.0".
	// It is canonical and has no leading "v". [Publish] refuses empty or
	// malformed values.
	SemanticVersion string
	// PublishedAt is the moment of publication. Required and never defaulted
	// to "now" — this package has no clock of its own.
	PublishedAt time.Time
	// PublishedBy names the principal who ran the publish action. Provenance
	// only; it is not an authorization decision.
	PublishedBy string
	// ToolVersions records the compiler/tooling identities material to
	// reproducing this publication, e.g. {"go": "1.26.3"}. Optional.
	ToolVersions map[string]string
	// FixtureRefs names the conformance fixtures this compiled plan was
	// proved against at publish time. Optional; [ActivationEvidence] separately
	// asserts they passed before activation.
	FixtureRefs []string
	// DependsOn declares the other workflow versions this version requires to
	// be active before it may itself be activated. Empty means no
	// dependencies.
	DependsOn []VersionRef
}

// clone returns a deep copy so a caller's later edits to slices/maps inside a
// PublishMeta cannot reach into a minted CompiledVersion.
func (m PublishMeta) clone() PublishMeta {
	c := m
	c.ToolVersions = cloneStringMap(m.ToolVersions)
	c.FixtureRefs = append([]string(nil), m.FixtureRefs...)
	c.DependsOn = append([]VersionRef(nil), m.DependsOn...)
	return c
}

// ActivationEvidence is what a caller presents to move a DRAFT (or
// quarantined) version toward ACTIVE. [Activate] is the governance boundary
// WF-COMP-006's RED clause describes: it refuses unless every one of these
// gates clears.
type ActivationEvidence struct {
	// Authorized must be true and ApprovedBy non-empty, or the call is an
	// unauthorized activation attempt (RED).
	Authorized bool
	ApprovedBy string
	Authority  string
	Reason     string
	// ApprovedAt is the moment of the authorization decision. Required.
	ApprovedAt time.Time
	// ReviewedPlanDigest is the compiled-plan digest the approval was granted
	// against. If it does not match the version being activated, the version
	// changed after review (RED).
	ReviewedPlanDigest string
	// TestsPassed reports whether the version's declared fixtures/conformance
	// suite passed. false is a failed-test draft (RED).
	TestsPassed bool
	// SupersedeActive, when true, permits activating this version while a
	// different version of the same workflow is already active: the
	// currently active version is quarantined first, as one governed act.
	// When false (the default), a competing active version refuses the call
	// outright rather than silently swapping it out.
	SupersedeActive bool
}

// CompiledVersion is the immutable, durable record of one compiled workflow
// publication. It is a value, not a handle: every accessor and every
// lifecycle call in this package returns a copy, never a pointer into a
// [Store]'s own storage. There are no setters; a status change is always a
// new [CompiledVersion] value produced by [Activate], [Quarantine] or
// [Retire] and recorded through [Store.Put].
type CompiledVersion struct {
	WorkflowID        string `json:"workflow_id"`
	DefinitionVersion uint32 `json:"definition_version"`
	SemanticVersion   string `json:"semantic_version"`

	// DefinitionDigest is the content digest of the draft definition this
	// version was compiled from.
	DefinitionDigest string `json:"definition_digest"`
	// CompiledPlanDigest is [workflow.CompiledWorkflow.Digest]: the identity a
	// runtime pins and [Resolve] looks a version up by.
	CompiledPlanDigest string `json:"compiled_plan_digest"`
	// CompilerVersion pins the exact compiler identity that produced the
	// plan, taken verbatim from the plan itself.
	CompilerVersion string `json:"compiler_version"`
	// CanonicalPlanBytes is the deterministic JSON rendering of the compiled
	// plan at publish time: the bytes a runtime or an auditor replays without
	// needing the compiler or the capability registry that produced them.
	CanonicalPlanBytes []byte `json:"canonical_plan_bytes"`

	ToolVersions map[string]string `json:"tool_versions,omitempty"`
	FixtureRefs  []string          `json:"fixture_refs,omitempty"`
	DependsOn    []VersionRef      `json:"depends_on,omitempty"`

	PublishedAt time.Time `json:"published_at"`
	PublishedBy string    `json:"published_by"`

	Approvals []ApprovalRecord `json:"approvals,omitempty"`

	Status ActivationStatus `json:"status"`

	digest string
}

// Digest is the record's own content identity: the canonical digest over
// every field except Status and this digest itself. Status is excluded
// deliberately — activation, quarantine and retirement are governed
// transitions on a fixed artifact, not edits to it, so the record's identity
// must not move underneath a pin just because its lifecycle status did. Use
// [CompiledVersion.CompiledPlanDigest] to key a [Store] lookup; use Digest to
// detect whether the record's own content was ever tampered with.
func (v CompiledVersion) Digest() string { return v.digest }

// Verify recomputes the record's digest from its current content and reports
// whether it still matches the digest minted at publication.
func (v CompiledVersion) Verify() error {
	if err := ValidateSemanticVersion(v.SemanticVersion); err != nil {
		return wrap(CodeInvalidSemanticVersion, v.WorkflowID, err,
			"compiled version carries an invalid semantic version")
	}
	got := computeRecordDigest(v)
	if got != v.digest {
		return refuse(CodeRecordMutated, v.WorkflowID,
			"compiled version content no longer matches its digest")
	}
	return nil
}

// clone returns a deep copy of v so a caller mutating the value it received
// back from this package can never reach into a [Store]'s own storage.
func (v CompiledVersion) clone() CompiledVersion {
	c := v
	c.CanonicalPlanBytes = append([]byte(nil), v.CanonicalPlanBytes...)
	c.ToolVersions = cloneStringMap(v.ToolVersions)
	c.FixtureRefs = append([]string(nil), v.FixtureRefs...)
	c.DependsOn = append([]VersionRef(nil), v.DependsOn...)
	c.Approvals = append([]ApprovalRecord(nil), v.Approvals...)
	return c
}

// withStatus returns a copy of v with a new status and, when ar is non-nil,
// one more entry appended to its approval history. It never edits v itself:
// this is the only way this package changes a version's lifecycle status,
// and it always produces a value a caller must explicitly [Store.Put].
func (v CompiledVersion) withStatus(status ActivationStatus, ar *ApprovalRecord) CompiledVersion {
	c := v.clone()
	c.Status = status
	if ar != nil {
		c.Approvals = append(c.Approvals, *ar)
	}
	return c
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// Pin names exactly which published version [Resolve] must return. At least
// one of CompiledPlanDigest or SemanticVersion is required; there is no
// "latest" or "active" pin — that guess belongs to a caller's own policy, not
// to this package.
type Pin struct {
	CompiledPlanDigest string
	SemanticVersion    string
}
