//go:build !js || !wasm

package journey

// Static rendering has no browser toggle lifecycle: the SSR/test path
// always renders the <details> element closed and never opens it, so there
// is nothing to mirror or collapse. See review_surface_sync_wasm.go for the
// live behavior.
func useReviewDetailsSync(detailsID string, onToggle func(open bool)) {}

func closeReviewDetails(detailsID string) {}
