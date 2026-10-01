package bufprotovalidatekit

import (
	"errors"
	"sort"
	"strings"
)

// Interceptor is the immutable, method-bound validation boundary used by
// transport adapters. It contains only validators compiled from local
// descriptors; it has no callbacks, authority hooks, or mutable registry.
//
// The name describes its role at the transport boundary. It is deliberately
// independent of gRPC and Connect so both transports can project the same
// Violation values.
type Interceptor struct {
	validators map[string]*Validator
}

// RequestValidator is the replaceable contract consumed by a transport
// adapter. Implementations must return owned structural/local violations and
// must not make authorization, legal, eligibility, or mutation decisions.
type RequestValidator interface {
	Validate(method string, input Message) []Violation
}

// NewInterceptor binds compiled validators to fully-qualified method names.
// Bindings are copied before publication, so changing the caller's map cannot
// change validation behavior after construction.
func NewInterceptor(bindings map[string]*Validator) (*Interceptor, error) {
	validators := make(map[string]*Validator, len(bindings))
	for method, validator := range bindings {
		if strings.TrimSpace(method) == "" {
			return nil, errors.New("validation method is required")
		}
		if validator == nil {
			return nil, errors.New("validation validator is required")
		}
		validators[method] = validator
	}
	return &Interceptor{validators: validators}, nil
}

// Validate returns the stable owned violations for method. An unbound method
// is intentionally a no-op: the transport's ordinary structural validator
// remains authoritative for methods without an explicitly compiled rule set.
func (i *Interceptor) Validate(method string, input Message) []Violation {
	if i == nil || input == nil {
		return nil
	}
	validator := i.validators[method]
	if validator == nil {
		return nil
	}
	return validator.Validate(input)
}

// Methods returns the bound method names in stable order for diagnostics and
// startup conformance checks. It does not expose the mutable backing map.
func (i *Interceptor) Methods() []string {
	if i == nil {
		return nil
	}
	methods := make([]string, 0, len(i.validators))
	for method := range i.validators {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}
