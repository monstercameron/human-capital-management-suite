//go:build !js || !wasm

package journey

// Static rendering has no browser navigation to focus after: the SSR/test
// path never transitions between Page.List and Page.Proposal, it only ever
// renders whichever one it was given once. See proposal_view_focus_wasm.go
// for the live behavior.
func useFocusOnMount(targetID string) {}
