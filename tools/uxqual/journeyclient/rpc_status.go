package journeyclient

import (
	"reflect"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrorHasCode reports whether any wrapped or joined error carries code.
// errors.As/status.Code select only one matching status from a joined error;
// walking each unwrap branch is required when independent RPC failures were
// aggregated and an earlier failure has a different code.
func ErrorHasCode(err error, code codes.Code) bool {
	const maxVisited = 1024
	stack := []error{err}
	seen := make(map[error]struct{})
	visited := 0
	for len(stack) > 0 && visited < maxVisited {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == nil {
			continue
		}
		if typ := reflect.TypeOf(current); typ != nil && typ.Comparable() {
			if _, ok := seen[current]; ok {
				continue
			}
			seen[current] = struct{}{}
		}
		visited++
		if status.Code(current) == code {
			return true
		}
		if many, ok := current.(interface{ Unwrap() []error }); ok {
			stack = append(stack, many.Unwrap()...)
			continue
		}
		if one, ok := current.(interface{ Unwrap() error }); ok {
			stack = append(stack, one.Unwrap())
		}
	}
	return false
}
