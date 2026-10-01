package main

// bootMarkPrefix namespaces the User Timing marks the cold start records.
const bootMarkPrefix = "hcm:"

// Cold-start phases this client marks, in the order they happen. The shell's
// loader marks the phases before Go runs (internal/humanwork/workspace's
// journeyLoaderSource); these are the ones only the Go client can see.
const (
	bootPhaseGoMain       = "go-main"
	bootPhaseDial         = "dial"
	bootPhaseHydrateStart = "hydrate-start"
	bootPhaseHydrated     = "hydrated"
	bootPhaseLoadStart    = "load-start"
	bootPhaseLoadDone     = "load-done"
)

// bootPhases lists the client's phases in the order they begin.
func bootPhases() []string {
	return []string{bootPhaseGoMain, bootPhaseDial, bootPhaseHydrateStart, bootPhaseLoadStart, bootPhaseHydrated, bootPhaseLoadDone}
}
