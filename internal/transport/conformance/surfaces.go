package conformance

import (
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ALIGN-057: SSR and enhanced-browser semantic parity. SSR renders the
// first paint and the enhanced browser hydrates it client-side; both are
// projections of the same semantic envelope. AssertAllParity projects one
// envelope onto every qualified surface — SSR, browser, enhanced browser,
// RPC, and export — and proves they all carry the same semantic result
// with no hidden subject on any surface.

// AssertAllParity projects envelope onto every qualified surface and
// checks parity plus noninterference. Different encodings are expected;
// different semantic digests are not.
func AssertAllParity(envelope QueryEnvelope, hidden []values.EntityRef) error {
	projections := make([]SurfaceProjection, 0, len(qualifiedSurfaces()))
	for _, surface := range qualifiedSurfaces() {
		projection, err := ProjectSurface(surface, envelope)
		if err != nil {
			return err
		}
		projections = append(projections, projection)
	}
	return CheckNoninterference(projections, hidden)
}

// qualifiedSurfaces lists every surface the contract qualifies, in stable
// order.
func qualifiedSurfaces() []Surface {
	return []Surface{SurfaceSSR, SurfaceBrowser, SurfaceEnhancedBrowser, SurfaceRPC, SurfaceExport}
}
