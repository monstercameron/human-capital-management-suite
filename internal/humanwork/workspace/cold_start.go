package workspace

import "time"

// ColdContentBudget is the longest a first visit may show the loading
// skeleton: from the product document's navigation start to the first frame
// holding the resolved page, with nothing cached (UXBLIND-089). The live
// browser suite enforces it (tools/uxqual/browser/uxblind089_cold_load.spec.mjs).
const ColdContentBudget = 3 * time.Second

// ColdStartMarkPrefix namespaces the User Timing marks a cold start records.
const ColdStartMarkPrefix = "hcm:"

// ColdStartLoaderPhases returns the marks the shell's loader records, in order:
// the loader ran, the asset manifest answered, the bundle's response arrived,
// the bundle compiled, and the module was instantiated with the runtime shim.
func ColdStartLoaderPhases() []string {
	return []string{"loader", "manifest", "wasm-response", "compiled", "instantiated"}
}

// ColdStartClientPhases returns the marks the Go client records after it starts
// (tools/uxqual/cmd/journeywasm's boot marks), in the order they begin.
func ColdStartClientPhases() []string {
	return []string{"go-main", "dial", "hydrate-start", "load-start", "hydrated", "load-done"}
}
