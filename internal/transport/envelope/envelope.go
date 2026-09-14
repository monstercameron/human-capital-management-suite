// Package envelope owns the one canonical Human Capital Management Suite error model and its
// projections onto transport status codes.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todos: CAP-003,
// ENDPOINT-003.
//
// The owned condition is canonical; a gRPC status code and an HTTP status code
// are projections of it, never the source of truth. That direction is the
// whole point: a capability decides "stale revision", and every channel then
// reports the same semantic code, the same retry classification, the same
// field paths and the same correlation and evidence references. A channel that
// invents its own status mapping breaks the contract even when the HTTP status
// happens to look right.
//
// Two rules govern everything here:
//
//  1. Nothing crosses the edge that the platform does not own. [Error.Message]
//     is a short, safe, owned summary; [Error.Violations] carry safe field
//     paths and rule references. Provider text, driver text, SQL, policy
//     source, stack traces and secrets live in the nested diagnostic, which is
//     never projected and is readable only through [Error.Diagnostic] with an
//     explicit access grant.
//  2. One projection table. [Error.GRPCCode] and [Error.HTTPStatus] are the
//     only mappings in the codebase, and they implement the canonical error
//     projection table in
//     planning/specs/http-grpc-endpoint-contract.md#canonical-error-projection.
//
// The package deliberately does not import a transport library. gRPC status
// wrapping lives here because [Error] implements the grpc-go status interface;
// the HTTP edge wraps the same [Error.Detail] and [Error.GRPCCode] into its own
// wire form, which is how "identical in meaning" stays checkable rather than
// aspirational.
package envelope

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Code is the canonical owned error condition. Values match
// hcmnext.common.v1.ErrorCode one for one so that the wire enum and the Go
// type can never drift apart; [TestTodo_CAP_003_Golden] asserts that.
type Code uint8

// Canonical owned error conditions.
const (
	CodeUnspecified Code = iota
	CodeInvalidArgument
	CodeUnauthenticated
	CodePermissionDenied
	CodeNotFound
	CodeAlreadyExists
	CodeAborted
	CodeFailedPrecondition
	CodeResourceExhausted
	CodeDeadlineExceeded
	CodeUnavailable
)

// maxCode is the highest defined code, used to bound conversions from the wire.
const maxCode = CodeUnavailable

// String returns the canonical spelling of the owned condition.
func (c Code) String() string {
	switch c {
	case CodeInvalidArgument:
		return "INVALID_ARGUMENT"
	case CodeUnauthenticated:
		return "UNAUTHENTICATED"
	case CodePermissionDenied:
		return "PERMISSION_DENIED"
	case CodeNotFound:
		return "NOT_FOUND"
	case CodeAlreadyExists:
		return "ALREADY_EXISTS"
	case CodeAborted:
		return "ABORTED"
	case CodeFailedPrecondition:
		return "FAILED_PRECONDITION"
	case CodeResourceExhausted:
		return "RESOURCE_EXHAUSTED"
	case CodeDeadlineExceeded:
		return "DEADLINE_EXCEEDED"
	case CodeUnavailable:
		return "UNAVAILABLE"
	case CodeUnspecified:
		return "UNSPECIFIED"
	default:
		return "INVALID(" + strconv.Itoa(int(c)) + ")"
	}
}

// Proto returns the wire enum for the owned condition.
func (c Code) Proto() commonv1.ErrorCode {
	if c > maxCode {
		return commonv1.ErrorCode_ERROR_CODE_UNSPECIFIED
	}
	return commonv1.ErrorCode(c)
}

// CodeFromProto returns the owned condition for a wire enum value. An
// unrecognized value maps to [CodeUnspecified] rather than being guessed at.
func CodeFromProto(c commonv1.ErrorCode) Code {
	if c < 0 || commonv1.ErrorCode(maxCode) < c {
		return CodeUnspecified
	}
	return Code(c)
}

// GRPCCode projects the owned condition onto a gRPC status code.
func (c Code) GRPCCode() codes.Code {
	switch c {
	case CodeInvalidArgument:
		return codes.InvalidArgument
	case CodeUnauthenticated:
		return codes.Unauthenticated
	case CodePermissionDenied:
		return codes.PermissionDenied
	case CodeNotFound:
		return codes.NotFound
	case CodeAlreadyExists:
		return codes.AlreadyExists
	case CodeAborted:
		return codes.Aborted
	case CodeFailedPrecondition:
		return codes.FailedPrecondition
	case CodeResourceExhausted:
		return codes.ResourceExhausted
	case CodeDeadlineExceeded:
		return codes.DeadlineExceeded
	case CodeUnavailable:
		return codes.Unavailable
	default:
		return codes.Internal
	}
}

// CodeFromGRPC maps a gRPC status code back onto the owned condition.
func CodeFromGRPC(c codes.Code) Code {
	switch c {
	case codes.InvalidArgument, codes.OutOfRange:
		return CodeInvalidArgument
	case codes.Unauthenticated:
		return CodeUnauthenticated
	case codes.PermissionDenied:
		return CodePermissionDenied
	case codes.NotFound:
		return CodeNotFound
	case codes.AlreadyExists:
		return CodeAlreadyExists
	case codes.Aborted:
		return CodeAborted
	case codes.FailedPrecondition:
		return CodeFailedPrecondition
	case codes.ResourceExhausted:
		return CodeResourceExhausted
	case codes.DeadlineExceeded:
		return CodeDeadlineExceeded
	case codes.Unavailable:
		return CodeUnavailable
	default:
		return CodeUnspecified
	}
}

// HTTPStatus projects the owned condition onto an HTTP status code, per the
// canonical error projection table. It is deliberately not delegated to a
// transport library: connect-go, for one, maps FAILED_PRECONDITION to 400
// rather than the 412 this contract requires, so the edge overrides its
// default with this function.
func (c Code) HTTPStatus() int {
	switch c {
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists, CodeAborted:
		return http.StatusConflict
	case CodeFailedPrecondition:
		return http.StatusPreconditionFailed
	case CodeResourceExhausted:
		return http.StatusTooManyRequests
	case CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// DefaultRetryable is the canonical retry classification for an owned
// condition. A caller may not retry a request that failed for a reason a retry
// cannot change; only the transient conditions are retryable, and
// DEADLINE_EXCEEDED is not among them because the result is ambiguous rather
// than known-not-applied.
func DefaultRetryable(c Code) bool {
	switch c {
	case CodeAborted, CodeResourceExhausted, CodeUnavailable:
		return true
	default:
		return false
	}
}

// Violation is one safe, non-sensitive field-level rejection reason.
type Violation struct {
	// FieldPath is the dotted path of the offending field in the request
	// message, e.g. "scope.tenant_id".
	FieldPath string
	// Description is a short owned explanation. It never echoes the rejected
	// value.
	Description string
	// RuleRef is the stable identifier of the rule that rejected it.
	RuleRef string
	// MoneyRange is an optional, typed server-owned correction for a monetary
	// field. An empty Minimum means no range is published.
	MoneyRange MoneyRange
}

type MoneyRange struct {
	Minimum  string
	Maximum  string
	Currency string
}

// Evidence references one durable evidence record.
type Evidence struct {
	ID     string
	Kind   string
	Digest string
}

// DiagnosticGrant authorizes reading an error's nested internal diagnostic.
// Only an operator-facing, access-controlled surface implements it; the
// transport edge never does, which is why a provider or driver message cannot
// reach a caller by accident.
type DiagnosticGrant interface {
	AllowsInternalDiagnostics() bool
}

// Error is the canonical owned error. It is safe to project as-is: everything
// it exposes was authored by Human Capital Management Suite.
type Error struct {
	code          Code
	reasonRef     string
	message       string
	violations    []Violation
	retryable     bool
	correlationID string
	evidence      Evidence
	diagnostic    error
}

// New returns an owned error with the canonical retry classification for code.
//
// reasonRef is the stable owned reason identifier (for example
// "trusted_context.caller_selected_authority"); message is a short safe
// summary. An unsafe message is replaced by the code's generic summary and
// preserved as the nested diagnostic instead of being projected, so a careless
// call site degrades to less information rather than to a leak.
func New(code Code, reasonRef, message string) *Error {
	e := &Error{
		code:      code,
		reasonRef: reasonRef,
		retryable: DefaultRetryable(code),
	}
	if safe, ok := safeMessage(message); ok {
		e.message = safe
	} else {
		e.message = genericMessage(code)
		e.diagnostic = errors.New(message)
	}
	return e
}

// Newf is [New] with a formatted message. The formatted result is subject to
// the same safety screen, so interpolating a provider string still cannot
// escape.
func Newf(code Code, reasonRef, format string, args ...any) *Error {
	return New(code, reasonRef, fmt.Sprintf(format, args...))
}

// WithViolation appends one safe field violation and returns e.
func (e *Error) WithViolation(fieldPath, description, ruleRef string) *Error {
	safe, ok := safeMessage(description)
	if !ok {
		safe = "field rejected by " + ruleRef
	}
	if e.violations == nil {
		e.violations = make([]Violation, 0, 3)
	}
	e.violations = append(e.violations, Violation{
		FieldPath:   fieldPath,
		Description: safe,
		RuleRef:     ruleRef,
	})
	return e
}

// WithViolationMoneyRange attaches exact monetary bounds to the most recent
// field violation. Invalid values are omitted rather than published.
func (e *Error) WithViolationMoneyRange(minimum, maximum values.Money) *Error {
	if safe, ok := safeMoneyRange(minimum, maximum); ok && len(e.violations) > 0 {
		e.violations[len(e.violations)-1].MoneyRange = safe
	}
	return e
}

func safeMoneyRange(minimum, maximum values.Money) (MoneyRange, bool) {
	if minimum.Validate() != nil || maximum.Validate() != nil || minimum.Currency() != maximum.Currency() ||
		minimum.Amount().Scale() != 2 || maximum.Amount().Scale() != 2 ||
		minimum.Amount().Sign() <= 0 || minimum.Amount().Cmp(maximum.Amount()) > 0 {
		return MoneyRange{}, false
	}
	return MoneyRange{Minimum: minimum.Amount().String(), Maximum: maximum.Amount().String(), Currency: minimum.Currency()}, true
}

func moneyRangeFromDetail(raw *commonv1.MoneyRange) MoneyRange {
	if raw == nil {
		return MoneyRange{}
	}
	minimum, err := values.NewMoney(raw.GetMinimum(), raw.GetCurrency(), 2, values.RoundingExactRequired)
	if err != nil {
		return MoneyRange{}
	}
	maximum, err := values.NewMoney(raw.GetMaximum(), raw.GetCurrency(), 2, values.RoundingExactRequired)
	if err != nil {
		return MoneyRange{}
	}
	rangeValue, _ := safeMoneyRange(minimum, maximum)
	return rangeValue
}

// WithRetryable overrides the canonical retry classification and returns e.
func (e *Error) WithRetryable(retryable bool) *Error {
	e.retryable = retryable
	return e
}

// WithCorrelation sets the correlation identifier and returns e.
func (e *Error) WithCorrelation(correlationID string) *Error {
	e.correlationID = correlationID
	return e
}

// WithEvidence sets the evidence reference and returns e.
func (e *Error) WithEvidence(ev Evidence) *Error {
	e.evidence = ev
	return e
}

// WithDiagnostic nests an internal diagnostic error. It is never projected
// across the edge and is readable only through [Error.Diagnostic].
func (e *Error) WithDiagnostic(err error) *Error {
	e.diagnostic = err
	return e
}

// Code returns the owned condition.
func (e *Error) Code() Code { return e.code }

// ReasonRef returns the stable owned reason identifier.
func (e *Error) ReasonRef() string { return e.reasonRef }

// Message returns the safe owned summary.
func (e *Error) Message() string { return e.message }

// Violations returns a copy of the safe field violations.
func (e *Error) Violations() []Violation { return slices.Clone(e.violations) }

// Retryable reports the canonical retry classification.
func (e *Error) Retryable() bool { return e.retryable }

// CorrelationID returns the request correlation identifier.
func (e *Error) CorrelationID() string { return e.correlationID }

// EvidenceRef returns the evidence reference.
func (e *Error) EvidenceRef() Evidence { return e.evidence }

// Diagnostic returns the nested internal diagnostic when grant allows it. A
// nil or refusing grant gets nothing, and there is no other accessor.
func (e *Error) Diagnostic(grant DiagnosticGrant) (error, bool) {
	if grant == nil || !grant.AllowsInternalDiagnostics() || e.diagnostic == nil {
		return nil, false
	}
	return e.diagnostic, true
}

// Error implements the error interface with the safe projection only. The
// nested diagnostic is deliberately absent: a handler that logs err.Error()
// must not thereby publish provider text.
func (e *Error) Error() string {
	if e.reasonRef == "" {
		return e.code.String() + ": " + e.message
	}
	return e.code.String() + " (" + e.reasonRef + "): " + e.message
}

// Unwrap deliberately returns nil. The nested diagnostic is not reachable by
// errors.Is/errors.As walking, only through [Error.Diagnostic].
func (e *Error) Unwrap() error { return nil }

// Detail returns the canonical typed error payload carried alongside every
// transport failure. It is the same message on every channel.
func (e *Error) Detail() *commonv1.ErrorDetail {
	detail := &commonv1.ErrorDetail{
		Code:          e.code.Proto(),
		Retryable:     e.retryable,
		CorrelationId: e.correlationID,
		ReasonRef:     e.reasonRef,
	}
	if len(e.violations) > 0 {
		detail.FieldViolations = make([]*commonv1.FieldViolation, len(e.violations))
	}
	for i, v := range e.violations {
		detail.FieldViolations[i] = &commonv1.FieldViolation{
			FieldPath:   v.FieldPath,
			Description: v.Description,
			RuleRef:     v.RuleRef,
		}
		if v.MoneyRange.Minimum != "" {
			detail.FieldViolations[i].PermittedMoneyRange = &commonv1.MoneyRange{
				Minimum: v.MoneyRange.Minimum, Maximum: v.MoneyRange.Maximum, Currency: v.MoneyRange.Currency,
			}
		}
	}
	if e.evidence.ID != "" || e.evidence.Kind != "" || e.evidence.Digest != "" {
		detail.EvidenceRef = &commonv1.EvidenceRef{
			EvidenceId:   e.evidence.ID,
			EvidenceKind: e.evidence.Kind,
			Digest:       e.evidence.Digest,
		}
	}
	return detail
}

// GRPCCode projects the owned condition onto a gRPC status code.
func (e *Error) GRPCCode() codes.Code { return e.code.GRPCCode() }

// HTTPStatus projects the owned condition onto an HTTP status code.
func (e *Error) HTTPStatus() int { return e.code.HTTPStatus() }

// GRPCStatus makes *Error satisfy the interface grpc-go's status package
// recognizes, so returning an *Error from a service implementation produces the
// projected status code and the canonical detail without a wrapping step.
func (e *Error) GRPCStatus() *status.Status {
	st := status.New(e.code.GRPCCode(), e.message)
	withDetails, err := st.WithDetails(e.Detail())
	if err != nil {
		return st
	}
	return withDetails
}

// FromDetail rebuilds an owned error from a projected code, message and
// canonical detail. Both the gRPC client path and the HTTP edge client path go
// through it, which is what makes "identical in meaning across channels"
// something a test can assert rather than describe.
func FromDetail(code Code, message string, detail *commonv1.ErrorDetail) *Error {
	e := &Error{code: code, retryable: DefaultRetryable(code)}
	if safe, ok := safeMessage(message); ok {
		e.message = safe
	} else {
		e.message = genericMessage(code)
	}
	if detail == nil {
		return e
	}
	if dc := CodeFromProto(detail.GetCode()); dc != CodeUnspecified {
		e.code = dc
	}
	e.retryable = detail.GetRetryable()
	e.correlationID = detail.GetCorrelationId()
	e.reasonRef = detail.GetReasonRef()
	if violations := detail.GetFieldViolations(); len(violations) > 0 {
		e.violations = make([]Violation, len(violations))
		for i, v := range violations {
			e.violations[i] = Violation{
				FieldPath:   v.GetFieldPath(),
				Description: v.GetDescription(),
				RuleRef:     v.GetRuleRef(),
			}
			if r := v.GetPermittedMoneyRange(); r != nil {
				e.violations[i].MoneyRange = moneyRangeFromDetail(r)
			}
		}
	}
	if ev := detail.GetEvidenceRef(); ev != nil {
		e.evidence = Evidence{ID: ev.GetEvidenceId(), Kind: ev.GetEvidenceKind(), Digest: ev.GetDigest()}
	}
	return e
}

// FromGRPC extracts the owned error from a gRPC client error, using the
// canonical detail when the server attached one and falling back to the status
// code otherwise.
func FromGRPC(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	st, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	var detail *commonv1.ErrorDetail
	for _, d := range st.Details() {
		if ed, isDetail := d.(*commonv1.ErrorDetail); isDetail {
			detail = ed
			break
		}
	}
	return FromDetail(CodeFromGRPC(st.Code()), st.Message(), detail), true
}

// As extracts an *Error from err, if there is one.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Coerce returns the owned error for err. An error that is not already owned
// becomes a generic UNAVAILABLE with the original nested as an unprojected
// diagnostic: an unrecognized failure must never reach a caller as its own
// text.
func Coerce(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := As(err); ok {
		return e
	}
	return New(CodeUnavailable, "transport.unclassified_failure",
		genericMessage(CodeUnavailable)).WithDiagnostic(err)
}

// genericMessage is the owned fallback summary for a condition.
func genericMessage(c Code) string {
	switch c {
	case CodeInvalidArgument:
		return "the request is malformed or structurally invalid"
	case CodeUnauthenticated:
		return "the request carries no valid authentication"
	case CodePermissionDenied:
		return "the action is not permitted for this principal"
	case CodeNotFound:
		return "the resource does not exist or is not visible"
	case CodeAlreadyExists:
		return "the resource already exists"
	case CodeAborted:
		return "the operation was aborted by a concurrency conflict"
	case CodeFailedPrecondition:
		return "a precondition for the operation is not met"
	case CodeResourceExhausted:
		return "a quota or admission budget is exhausted"
	case CodeDeadlineExceeded:
		return "the caller deadline expired before a result was known"
	case CodeUnavailable:
		return "the request could not be served"
	default:
		return "the request could not be served"
	}
}

// unsafeMarkers are substrings that indicate a message carries provider text,
// storage text, policy source, a stack trace or a secret. The screen is
// deliberately conservative: a false positive costs a generic message, a false
// negative costs a disclosure.
var unsafeMarkers = []string{
	".go:",
	"/internal/",
	"bearer ",
	"delete from",
	"goroutine ",
	"insert into",
	"panic:",
	"password",
	"pgx",
	"pq:",
	"private key",
	"secret",
	"select ",
	"sql",
	"stack trace",
	"update set",
}

// maxMessageBytes bounds an owned summary. A long message is a sign that
// something unowned was interpolated into it.
const maxMessageBytes = 200

// safeMessage reports whether message is a safe owned summary, returning it
// trimmed when it is.
func safeMessage(message string) (string, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" || len(trimmed) > maxMessageBytes {
		return "", false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] < 0x20 || trimmed[i] > 0x7e {
			return "", false
		}
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range unsafeMarkers {
		if strings.Contains(lower, marker) {
			return "", false
		}
	}
	return trimmed, true
}
