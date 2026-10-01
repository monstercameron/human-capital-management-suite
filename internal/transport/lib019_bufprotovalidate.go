package transport

import (
	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	kit "github.com/monstercameron/human-capital-management-suite/tools/quality/bufprotovalidatekit"
)

// QualifiedValidator composes the existing transport structural validator
// with an immutable descriptor-bound interceptor. The adapter is deliberately
// a transport.Validator: gRPC and the HTTP edge already call that same port,
// so both protocols receive the same owned violation projection.
type QualifiedValidator struct {
	base        Validator
	interceptor kit.RequestValidator
}

// NewQualifiedValidator returns a transport validator backed by the local
// Buf/Protovalidate qualification seam. A nil base selects DefaultValidator.
// The interceptor is immutable after construction and may be shared by all
// requests handled by one server instance.
func NewQualifiedValidator(base Validator, interceptor kit.RequestValidator) Validator {
	if base == nil {
		base = DefaultValidator{}
	}
	return &QualifiedValidator{base: base, interceptor: interceptor}
}

// Validate implements Validator and preserves the owned transport error
// model. The descriptor validator contributes only structural/local field
// violations; it cannot replace or augment business authorization decisions.
func (v *QualifiedValidator) Validate(method string, msg proto.Message) *envelope.Error {
	if v == nil {
		return nil
	}
	base := v.base
	if base == nil {
		base = DefaultValidator{}
	}
	err := base.Validate(method, msg)
	if v.interceptor == nil || msg == nil {
		return err
	}
	violations := v.interceptor.Validate(method, msg)
	if len(violations) == 0 {
		return err
	}
	if err == nil {
		err = envelope.New(envelope.CodeInvalidArgument, reasonStructuralRejection,
			"the request is malformed or structurally invalid")
	}
	for _, violation := range violations {
		err.WithViolation(violation.FieldPath, violation.Description, violation.RuleRef)
	}
	return err
}
