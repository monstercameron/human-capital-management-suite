package timeclock

// EngineGap names one engine feature a template needs but the kernel does not
// provide yet, the workflow-engine todo that closes it and how the template
// expresses the behaviour today. Nodes that stand in for a missing feature
// carry the gap's ID in their Metadata under MetadataEngineGap, so an
// inspector can list every stand-in without reading source.
type EngineGap struct {
	ID          string
	Feature     string
	ClosestForm string
}

// MetadataEngineGap is the node metadata key naming the gap a node stands in
// for. Metadata stays out of the plan digest, so closing a gap and removing
// the marker does not by itself change a pinned plan.
const MetadataEngineGap = "hcmnext.time.engine_gap"

// Engine gap identifiers.
const (
	GapPlanResolution  = "WF-EXT-008"
	GapTimeExpr        = "WF-EXT-012"
	GapTypedSignal     = "WF-EXT-014"
	GapSpawn           = "WF-EXT-019"
	GapReducingJoin    = "WF-EXT-020"
	GapVendorRoundTrip = "WF-EXT-022"
)

// EngineGaps lists every gap the time templates work around, in ID order.
func EngineGaps() []EngineGap {
	return []EngineGap{
		{GapPlanResolution, "version registry match predicate reads the resolved time profile",
			"ResolvePlan and the candidate registration's Match resolve the profile and compare templates"},
		{GapTimeExpr, "TimeExpr WAIT anchored on run data (scheduled end, period end)",
			"WAIT with a fixed placeholder instant the timer factory replaces; SIGNAL close_after_seconds bounds a subscription"},
		{GapTypedSignal, "typed SIGNAL correlation on worker, assignment and session with multi-accept",
			"one SIGNAL correlated on the subject:time_session start fact inside a declared cycle guarded by a DECISION"},
		{GapSpawn, "COLLECTION/EVENT spawn of session runs from the period run",
			"session runs start from their own trigger; the period run collects their obligations with a read capability"},
		{GapReducingJoin, "reducing JOIN over child outputs",
			"a folding TRANSFORM over the collected obligations"},
		{GapVendorRoundTrip, "vendor round-trip fragment",
			"dispatch CAPABILITY, correlated SIGNAL with timeout, then a bounded OBSERVE with a repair terminal"},
	}
}

func gapMetadata(id string) map[string]string {
	return map[string]string{MetadataEngineGap: id}
}
