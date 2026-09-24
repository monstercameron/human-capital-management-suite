package evolution

import "errors"

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidDefinition wraps a definition that fails its own
	// [intent.Definition.Validate]. This package never judges the
	// compatibility, digest or supersession of a definition it cannot
	// itself validate.
	ErrInvalidDefinition = errors.New("evolution: invalid intent definition")

	// ErrDifferentIntentType reports a compatibility check attempted between
	// two definitions that do not share an intent_type_id. Compatibility is
	// a question about successive versions of the SAME intent type;
	// comparing across types is a caller mistake, not a compatibility
	// question this package can answer.
	ErrDifferentIntentType = errors.New("evolution: definitions do not share an intent type id")

	// ErrVersionNotAdvancing reports a candidate whose version is not
	// strictly greater than the version of the predecessor it claims to
	// succeed.
	ErrVersionNotAdvancing = errors.New("evolution: candidate version does not advance the previous version")

	// ErrInPlaceEdit reports that a candidate definition carries the same
	// (intent_type_id, version) as a published definition but a different
	// content digest. A published version is immutable: this is never a
	// legal republication, whatever the content difference would otherwise
	// have classified as under CompatibilityCheck.
	ErrInPlaceEdit = errors.New("evolution: in-place edit of a published definition version is refused")

	// ErrSupersessionRequiresIncompatibility reports an attempt to record a
	// supersession over a compatibility report that found no incompatible
	// change. Under the declared policy, compatible successors need no
	// supersession artifact; runtime binding is outside this package.
	ErrSupersessionRequiresIncompatibility = errors.New("evolution: supersession requires an incompatible compatibility report")

	// ErrApproverIsAuthor reports a supersession whose approver and author
	// are the same principal. Superseding a published definition is never
	// self-approved.
	ErrApproverIsAuthor = errors.New("evolution: supersession approver may not be the author")

	// ErrInvalidSupersession reports a supersession record missing a
	// required field: reason, author, approver, effective instant, or a
	// recognized live-instance policy.
	ErrInvalidSupersession = errors.New("evolution: invalid supersession record")

	// ErrSupersessionTampered reports a supersession record whose recorded
	// digest no longer matches its recomputed content digest: a mutation
	// after construction.
	ErrSupersessionTampered = errors.New("evolution: supersession record content does not match its digest")
)
