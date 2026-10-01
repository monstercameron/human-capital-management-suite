// Package recovery is the production boundary for truthful participant-facing
// recovery projections. It keeps the lifecycle resolver behind a stable
// experience-owned import that serving code can consume without importing a
// test-only package or inventing client-side state rules.
package recovery

import "github.com/monstercameron/human-capital-management-suite/internal/experience/presentation"

type (
	Input        = presentation.Input
	Presentation = presentation.Presentation
)

// Resolve maps server-known lifecycle, authorization, freshness, and
// evidence dimensions to the exact state and safe actions a surface may show.
// The returned projection is a value copy; callers cannot mutate the resolver
// or use it as an effect boundary.
func Resolve(input Input) Presentation {
	return presentation.Resolve(input)
}
