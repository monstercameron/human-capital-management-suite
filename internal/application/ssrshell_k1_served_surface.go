package application

import (
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/ssrshell"
)

// ServedSSRShellSurface exposes the semantic SSR shell renderer through the
// application boundary used by hcmnext serve. The ssrshell package remains
// the semantic owner; this surface only carries its value-owned operations
// into the composed application.
type ServedSSRShellSurface struct {
	Render                func(pagedef.PageDefinition) (ssrshell.RenderedShell, error)
	ShellCSS              func() string
	ContentSecurityPolicy func() string
	LandmarkForRegionKind func(pagedef.RegionKind) (string, string, bool)
}

// NewServedSSRShellSurface returns the SSR shell capabilities reachable from
// the served application. It creates no process-wide or tenant-wide state.
func NewServedSSRShellSurface() ServedSSRShellSurface {
	return ServedSSRShellSurface{
		Render:                ssrshell.Render,
		ShellCSS:              ssrshell.ShellCSS,
		ContentSecurityPolicy: ssrshell.ContentSecurityPolicy,
		LandmarkForRegionKind: ssrshell.LandmarkForRegionKind,
	}
}

// SSRShell returns the SSR shell capabilities exposed by a composed
// application. A nil application has no served capabilities.
func (a *App) SSRShell() ServedSSRShellSurface {
	if a == nil {
		return ServedSSRShellSurface{}
	}
	return NewServedSSRShellSurface()
}
