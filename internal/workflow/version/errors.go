package version

import (
	"errors"
	"fmt"
)

// ErrVersion is the sentinel every refusal from this package unwraps to.
// Classify with [errors.Is] and read [Error.Code] for the exact reason; never
// match message text.
var ErrVersion = errors.New("workflow/version: rejected")

// Stable refusal codes. A transport, a runtime or a test matches these; they
// never change spelling once published.
const (
	// CodeMissingPublicationTime reports a [PublishMeta] with a zero
	// PublishedAt. There is no clock inside this package: the caller supplies
	// the moment a publication happened.
	CodeMissingPublicationTime = "MISSING_PUBLICATION_TIME"
	// CodeMissingSemanticVersion reports a [PublishMeta] with no semantic
	// version, the identity a human names a version by.
	CodeMissingSemanticVersion = "MISSING_SEMANTIC_VERSION"
	// CodeCompilationRejected reports a definition that does not compile at
	// all under the supplied options. Wraps the underlying
	// [workflow.Diagnostics].
	CodeCompilationRejected = "COMPILATION_REJECTED"
	// CodePlanDigestMismatch reports a compiled plan whose digest does not
	// match recompiling its own definition: the plan and the definition
	// disagree about what was published (WF-COMP-006 RED).
	CodePlanDigestMismatch = "PLAN_DIGEST_MISMATCH"

	// CodeInvalidPin reports a [Pin] that names neither a compiled-plan
	// digest nor a semantic version. [Resolve] never guesses; an unpinned
	// request is refused rather than silently resolved to "latest".
	CodeInvalidPin = "INVALID_PIN"
	// CodeUnknownVersion reports a pin that names no published version of the
	// workflow.
	CodeUnknownVersion = "UNKNOWN_VERSION"
	// CodeAmbiguousPin reports a semantic-version pin matching more than one
	// published record. [Resolve] refuses rather than picking one.
	CodeAmbiguousPin = "AMBIGUOUS_PIN"

	// CodeUnauthorizedActivation reports an [Activate] call with no
	// authorized approver (WF-COMP-006 RED: "unauthorized ... draft cannot
	// activate").
	CodeUnauthorizedActivation = "UNAUTHORIZED_ACTIVATION"
	// CodeChangedAfterReview reports activation evidence granted against a
	// different compiled-plan digest than the version being activated
	// (WF-COMP-006 RED: "changed-after-review").
	CodeChangedAfterReview = "CHANGED_AFTER_REVIEW"
	// CodeFailedTest reports activation evidence declaring its fixtures or
	// conformance suite did not pass (WF-COMP-006 RED: "failed-test").
	CodeFailedTest = "FAILED_TEST"
	// CodeUnresolvedDependency reports a declared dependency on another
	// workflow version that is not currently active (WF-COMP-006 RED:
	// "unresolved-dependency").
	CodeUnresolvedDependency = "UNRESOLVED_DEPENDENCY"
	// CodeRetiredCannotActivate reports an activation attempt on a version
	// already retired. Retirement ends normal availability; it is not
	// reversible the way quarantine is.
	CodeRetiredCannotActivate = "RETIRED_CANNOT_ACTIVATE"
	// CodeAnotherVersionActive reports an activation attempt while a
	// different version of the same workflow is already active, with no
	// explicit supersede instruction in the evidence. A rollback or a
	// forward roll is an explicit governed act, never an implicit swap.
	CodeAnotherVersionActive = "ANOTHER_VERSION_ACTIVE"
	// CodeNotActive reports a [Quarantine] call against a version that is not
	// currently active.
	CodeNotActive = "NOT_ACTIVE"
	// CodeAlreadyRetired reports a [Retire] call against a version that is
	// already retired.
	CodeAlreadyRetired = "ALREADY_RETIRED"

	// CodeUnknownRecord reports a lifecycle call (Activate/Quarantine/Retire)
	// naming a compiled-plan digest the store has never seen.
	CodeUnknownRecord = "UNKNOWN_RECORD"
	// CodeRecordMutated reports a [CompiledVersion] whose recomputed digest no
	// longer matches the digest it was minted with.
	CodeRecordMutated = "RECORD_MUTATED"
	// CodeInvalidRecord reports a record a [Store] cannot persist: no
	// workflow id or no compiled-plan digest.
	CodeInvalidRecord = "INVALID_RECORD"
)

// Error is one typed refusal: the code, the workflow or digest it concerns,
// and a detail written for a person. Code is for a program.
type Error struct {
	Code   string
	Ref    string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	loc := ""
	if e.Ref != "" {
		loc = " [" + e.Ref + "]"
	}
	msg := fmt.Sprintf("workflow/version: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel, so
// errors.Is(err, ErrVersion) classifies every refusal this package produces.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrVersion, e.Err}
	}
	return []error{ErrVersion}
}

// refuse builds a typed refusal.
func refuse(code, ref, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...)}
}

// wrap builds a typed refusal around an underlying error, e.g. a
// [workflow.Diagnostics] set from a failed recompilation.
func wrap(code, ref string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...), Err: err}
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
