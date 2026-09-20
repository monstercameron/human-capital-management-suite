package version

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Publish mints an immutable [CompiledVersion] for a compiled workflow plan
// and records it in store, in [StatusDraft].
//
// It refuses:
//   - a definition that does not itself compile under opts;
//   - a plan whose digest does not match recompiling def under opts — the
//     plan and the definition must be provably the same publication, never
//     two artifacts a caller merely asserts belong together;
//   - a meta with no PublishedAt (this package has no clock) or no canonical
//     SemVer 2.0.0 SemanticVersion;
//   - changed content whose semantic version does not advance the workflow
//     family, or existing content being relabeled with a different version.
//
// Publishing the same compiled plan twice is idempotent: if store already
// holds a record for plan.Digest(), Publish returns that existing record
// rather than minting a second version for identical content.
func Publish(store Store, def workflow.Definition, plan *workflow.CompiledWorkflow, opts workflow.Options, meta PublishMeta) (CompiledVersion, error) {
	if store == nil {
		return CompiledVersion{}, refuse(CodeInvalidRecord, def.WorkflowID, "no store supplied")
	}
	if plan == nil {
		return CompiledVersion{}, refuse(CodeInvalidRecord, def.WorkflowID, "no compiled plan supplied")
	}
	if meta.PublishedAt.IsZero() {
		return CompiledVersion{}, refuse(CodeMissingPublicationTime, def.WorkflowID,
			"publish meta carries no publication time")
	}
	if meta.SemanticVersion == "" {
		return CompiledVersion{}, refuse(CodeMissingSemanticVersion, def.WorkflowID,
			"publish meta carries no semantic version")
	}
	if err := ValidateSemanticVersion(meta.SemanticVersion); err != nil {
		return CompiledVersion{}, wrap(CodeInvalidSemanticVersion, def.WorkflowID, err,
			"publish meta carries an invalid semantic version")
	}

	recompiled, err := workflow.Compile(def, opts)
	if err != nil {
		return CompiledVersion{}, wrap(CodeCompilationRejected, def.WorkflowID, err,
			"definition does not compile under the supplied options")
	}
	if recompiled.Digest() != plan.Digest() {
		return CompiledVersion{}, refuse(CodePlanDigestMismatch, def.WorkflowID,
			"supplied plan digest %s does not match recompiling its definition (%s)",
			plan.Digest(), recompiled.Digest())
	}

	if existing, found, err := store.GetByDigest(plan.Digest()); err != nil {
		return CompiledVersion{}, err
	} else if found {
		if existing.WorkflowID != def.WorkflowID || existing.SemanticVersion != meta.SemanticVersion {
			return CompiledVersion{}, refuse(CodeSemanticVersionConflict, def.WorkflowID,
				"compiled content is already published as %s@%s and cannot be relabeled as %s@%s",
				existing.WorkflowID, existing.SemanticVersion, def.WorkflowID, meta.SemanticVersion)
		}
		return existing, nil
	}
	versions, err := store.List(def.WorkflowID)
	if err != nil {
		return CompiledVersion{}, err
	}
	for _, existing := range versions {
		order, compareErr := CompareSemanticVersions(meta.SemanticVersion, existing.SemanticVersion)
		if compareErr != nil {
			return CompiledVersion{}, wrap(CodeInvalidSemanticVersion, def.WorkflowID, compareErr,
				"cannot compare candidate version %s with published version %s", meta.SemanticVersion, existing.SemanticVersion)
		}
		if order == 0 {
			return CompiledVersion{}, refuse(CodeSemanticVersionConflict, def.WorkflowID,
				"semantic version %s does not have distinct precedence from published version %s with compiled-plan digest %s",
				meta.SemanticVersion, existing.SemanticVersion, existing.CompiledPlanDigest)
		}
		if order < 0 {
			return CompiledVersion{}, refuse(CodeVersionNotNewer, def.WorkflowID,
				"version %s cannot follow published version %s; changed content must advance the workflow family",
				meta.SemanticVersion, existing.SemanticVersion)
		}
	}

	planBytes, err := canonicalPlanBytes(plan)
	if err != nil {
		return CompiledVersion{}, wrap(CodeInvalidRecord, def.WorkflowID, err,
			"compiled plan could not be rendered to canonical bytes")
	}

	m := meta.clone()
	v := CompiledVersion{
		WorkflowID:         def.WorkflowID,
		DefinitionVersion:  def.Version,
		SemanticVersion:    m.SemanticVersion,
		DefinitionDigest:   computeDefinitionDigest(def),
		CompiledPlanDigest: plan.Digest(),
		CompilerVersion:    plan.CompilerVersion,
		CanonicalPlanBytes: planBytes,
		ToolVersions:       m.ToolVersions,
		FixtureRefs:        m.FixtureRefs,
		DependsOn:          m.DependsOn,
		PublishedAt:        m.PublishedAt,
		PublishedBy:        m.PublishedBy,
		Status:             StatusDraft,
	}
	v.digest = computeRecordDigest(v)

	if err := store.Put(v); err != nil {
		return CompiledVersion{}, err
	}
	return v, nil
}

// Activate moves a DRAFT or QUARANTINED version to ACTIVE. It is the
// governance gate WF-COMP-006's RED clause describes and refuses:
//
//   - an unauthorized attempt (evidence.Authorized is false or ApprovedBy is
//     empty);
//   - a version changed after review (evidence.ReviewedPlanDigest does not
//     match the version's compiled-plan digest);
//   - a failed-test draft (evidence.TestsPassed is false);
//   - an unresolved dependency (a version this one declares [PublishMeta.DependsOn]
//     is not currently active for its workflow);
//   - reactivating a RETIRED version, which is not reversible;
//   - a competing active version, unless evidence.SupersedeActive is true and
//     the candidate semantic version is strictly newer, in which case the
//     current active version is quarantined first.
func Activate(store Store, planDigest string, evidence ActivationEvidence) (CompiledVersion, error) {
	if store == nil {
		return CompiledVersion{}, refuse(CodeInvalidRecord, "", "no store supplied")
	}
	v, found, err := store.GetByDigest(planDigest)
	if err != nil {
		return CompiledVersion{}, err
	}
	if !found {
		return CompiledVersion{}, refuse(CodeUnknownRecord, planDigest, "no published version carries this compiled-plan digest")
	}
	if v.Status == StatusRetired {
		return CompiledVersion{}, refuse(CodeRetiredCannotActivate, v.WorkflowID,
			"version %s of %s is retired and cannot be reactivated", planDigest, v.WorkflowID)
	}
	if !evidence.Authorized || evidence.ApprovedBy == "" {
		return CompiledVersion{}, refuse(CodeUnauthorizedActivation, v.WorkflowID,
			"activation of %s presented no authorized approver", planDigest)
	}
	if evidence.ReviewedPlanDigest != v.CompiledPlanDigest {
		return CompiledVersion{}, refuse(CodeChangedAfterReview, v.WorkflowID,
			"approval was granted against %s but activation targets %s", evidence.ReviewedPlanDigest, v.CompiledPlanDigest)
	}
	if !evidence.TestsPassed {
		return CompiledVersion{}, refuse(CodeFailedTest, v.WorkflowID,
			"activation of %s presented no passing test evidence", planDigest)
	}
	for _, dep := range v.DependsOn {
		active, found, err := store.GetActiveForWorkflow(dep.WorkflowID)
		if err != nil {
			return CompiledVersion{}, err
		}
		if !found || active.CompiledPlanDigest != dep.CompiledPlanDigest {
			return CompiledVersion{}, refuse(CodeUnresolvedDependency, v.WorkflowID,
				"dependency on %s@%s is not active", dep.WorkflowID, dep.CompiledPlanDigest)
		}
	}

	if current, found, err := store.GetActiveForWorkflow(v.WorkflowID); err != nil {
		return CompiledVersion{}, err
	} else if found && current.CompiledPlanDigest != v.CompiledPlanDigest {
		if !evidence.SupersedeActive {
			return CompiledVersion{}, refuse(CodeAnotherVersionActive, v.WorkflowID,
				"version %s is already active for %s; supersede it explicitly to activate %s",
				current.CompiledPlanDigest, v.WorkflowID, planDigest)
		}
		order, compareErr := CompareSemanticVersions(v.SemanticVersion, current.SemanticVersion)
		if compareErr != nil {
			return CompiledVersion{}, wrap(CodeInvalidSemanticVersion, v.WorkflowID, compareErr,
				"cannot compare replacement version %s with active version %s", v.SemanticVersion, current.SemanticVersion)
		}
		if order <= 0 {
			return CompiledVersion{}, refuse(CodeVersionNotNewer, v.WorkflowID,
				"version %s cannot replace active version %s; a replacement must be strictly newer",
				v.SemanticVersion, current.SemanticVersion)
		}
		quarantined := current.withStatus(StatusQuarantined, &ApprovalRecord{
			ApprovedBy: evidence.ApprovedBy,
			Authority:  evidence.Authority,
			Reason:     SupersededReasonPrefix + planDigest,
			ApprovedAt: evidence.ApprovedAt,
			Result:     StatusQuarantined,
		})
		if err := store.Put(quarantined); err != nil {
			return CompiledVersion{}, err
		}
	}

	activated := v.withStatus(StatusActive, &ApprovalRecord{
		ApprovedBy: evidence.ApprovedBy,
		Authority:  evidence.Authority,
		Reason:     evidence.Reason,
		ApprovedAt: evidence.ApprovedAt,
		Result:     StatusActive,
	})
	if err := store.Put(activated); err != nil {
		return CompiledVersion{}, err
	}
	return activated, nil
}

// SupersededReasonPrefix opens the reason [Activate] records when it
// quarantines the version a superseding activation replaces.
const SupersededReasonPrefix = "superseded by activation of "

// QuarantinedBySupersession reports whether v is QUARANTINED only because a
// later version superseded it ([Activate] with SupersedeActive), as opposed
// to a governed [Quarantine] of its own: its latest approval record is the
// supersession. Such a version is out of new starts but remains the exact
// plan its live instances pinned; a governed quarantine is enforced on them
// (the runtime's live-instance disposition) instead.
func (v CompiledVersion) QuarantinedBySupersession() bool {
	if v.Status != StatusQuarantined || len(v.Approvals) == 0 {
		return false
	}
	last := v.Approvals[len(v.Approvals)-1]
	return last.Result == StatusQuarantined && strings.HasPrefix(last.Reason, SupersededReasonPrefix)
}

// Quarantine pulls an ACTIVE version from new starts. It is reversible only
// through a further [Activate] call, matching planning/specs/workflow-runtime.md's
// quarantine semantics.
func Quarantine(store Store, planDigest, reason, by, authority string, at ActivationEvidence) (CompiledVersion, error) {
	v, found, err := store.GetByDigest(planDigest)
	if err != nil {
		return CompiledVersion{}, err
	}
	if !found {
		return CompiledVersion{}, refuse(CodeUnknownRecord, planDigest, "no published version carries this compiled-plan digest")
	}
	if v.Status != StatusActive {
		return CompiledVersion{}, refuse(CodeNotActive, v.WorkflowID, "version %s is %s, not active", planDigest, v.Status)
	}
	quarantined := v.withStatus(StatusQuarantined, &ApprovalRecord{
		ApprovedBy: by,
		Authority:  authority,
		Reason:     reason,
		ApprovedAt: at.ApprovedAt,
		Result:     StatusQuarantined,
	})
	if err := store.Put(quarantined); err != nil {
		return CompiledVersion{}, err
	}
	return quarantined, nil
}

// Retire ends a version's normal availability for good. Unlike quarantine,
// a retired version cannot be reactivated.
func Retire(store Store, planDigest, reason, by, authority string, at ActivationEvidence) (CompiledVersion, error) {
	v, found, err := store.GetByDigest(planDigest)
	if err != nil {
		return CompiledVersion{}, err
	}
	if !found {
		return CompiledVersion{}, refuse(CodeUnknownRecord, planDigest, "no published version carries this compiled-plan digest")
	}
	if v.Status == StatusRetired {
		return CompiledVersion{}, refuse(CodeAlreadyRetired, v.WorkflowID, "version %s is already retired", planDigest)
	}
	retired := v.withStatus(StatusRetired, &ApprovalRecord{
		ApprovedBy: by,
		Authority:  authority,
		Reason:     reason,
		ApprovedAt: at.ApprovedAt,
		Result:     StatusRetired,
	})
	if err := store.Put(retired); err != nil {
		return CompiledVersion{}, err
	}
	return retired, nil
}

// Resolve returns exactly the version pin names, or a typed error. It never
// falls back to "latest" or "active": an empty or ambiguous pin is refused
// rather than guessed.
func Resolve(store Store, workflowID string, pin Pin) (CompiledVersion, error) {
	if store == nil {
		return CompiledVersion{}, refuse(CodeInvalidRecord, workflowID, "no store supplied")
	}
	if workflowID == "" {
		return CompiledVersion{}, refuse(CodeInvalidPin, "", "resolve names no workflow id")
	}
	if pin.CompiledPlanDigest == "" && pin.SemanticVersion == "" {
		return CompiledVersion{}, refuse(CodeInvalidPin, workflowID,
			"pin names neither a compiled-plan digest nor a semantic version")
	}
	if pin.CompiledPlanDigest == "" {
		if err := ValidateSemanticVersion(pin.SemanticVersion); err != nil {
			return CompiledVersion{}, wrap(CodeInvalidPin, workflowID, err,
				"pin carries an invalid semantic version")
		}
	}

	if pin.CompiledPlanDigest != "" {
		v, found, err := store.GetByDigest(pin.CompiledPlanDigest)
		if err != nil {
			return CompiledVersion{}, err
		}
		if !found || v.WorkflowID != workflowID {
			return CompiledVersion{}, refuse(CodeUnknownVersion, workflowID,
				"no published version of %s carries digest %s", workflowID, pin.CompiledPlanDigest)
		}
		return v, nil
	}

	versions, err := store.List(workflowID)
	if err != nil {
		return CompiledVersion{}, err
	}
	var match CompiledVersion
	matches := 0
	for _, v := range versions {
		if v.SemanticVersion == pin.SemanticVersion {
			match = v
			matches++
		}
	}
	switch matches {
	case 0:
		return CompiledVersion{}, refuse(CodeUnknownVersion, workflowID,
			"no published version of %s names semantic version %s", workflowID, pin.SemanticVersion)
	case 1:
		return match, nil
	default:
		return CompiledVersion{}, refuse(CodeAmbiguousPin, workflowID,
			"%d published versions of %s name semantic version %s", matches, workflowID, pin.SemanticVersion)
	}
}
