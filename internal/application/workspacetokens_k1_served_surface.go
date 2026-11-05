package application

import "github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"

// ServedWorkspaceTokensSurface exposes the workspace design-token and
// mode-contract stylesheets through the application boundary used by hcmnext
// serve. The tokens package remains the semantic owner; this surface only
// carries its value-owned operations into the composed application.
type ServedWorkspaceTokensSurface struct {
	WorkspaceCSS   func() string
	CSSVariables   func() string
	MotionCSS      func() string
	TextPairs      func() []struct{ Foreground, Background tokens.Color }
	ContrastRatio  func(string, string) (float64, error)
	MinNormalRatio func() float64
}

// NewServedWorkspaceTokensSurface returns the workspace-token capabilities
// reachable from the served application. It creates no process-wide or
// tenant-wide state.
func NewServedWorkspaceTokensSurface() ServedWorkspaceTokensSurface {
	return ServedWorkspaceTokensSurface{
		WorkspaceCSS:   tokens.WorkspaceCSS,
		CSSVariables:   tokens.CSSVariables,
		MotionCSS:      tokens.MotionCSS,
		TextPairs:      tokens.TextPairs,
		ContrastRatio:  tokens.ContrastRatio,
		MinNormalRatio: func() float64 { return tokens.MinRatioNormalText },
	}
}

// WorkspaceTokens returns the workspace-token capabilities exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) WorkspaceTokens() ServedWorkspaceTokensSurface {
	if a == nil {
		return ServedWorkspaceTokensSurface{}
	}
	return NewServedWorkspaceTokensSurface()
}
