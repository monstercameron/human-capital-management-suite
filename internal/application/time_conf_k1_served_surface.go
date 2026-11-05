package application

import timeconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/time"

// ServedTimeConformanceSurface exposes the shared Time
// conformance oracle through the application boundary used by hcmnext serve.
// The oracle remains a pure semantic owner: environments are supplied by the
// caller and no process-wide registry is created here.
type ServedTimeConformanceSurface struct {
	GoldenEnvironment         func() *timeconformance.Environment
	DuplicatePunchEnvironment func() *timeconformance.Environment
	PostLockEditEnvironment   func() *timeconformance.Environment
	StaleReopenEnvironment    func() *timeconformance.Environment
	SpoofSuspectedEnvironment func() *timeconformance.Environment
	OfflineReplayEnvironment  func() *timeconformance.Environment
	DSTFoldEnvironment        func() *timeconformance.Environment
	ClockSkewEnvironment      func() *timeconformance.Environment
	DegradedBridgeEnvironment func() *timeconformance.Environment
	NewSetup                  func(*timeconformance.Environment) (*timeconformance.Setup, error)
	ReferenceWorkflowID       func() string
	CapabilityIDs             func() []string
}

// NewServedTimeConformanceSurface returns the Time conformance
// capabilities reachable from the served application.
func NewServedTimeConformanceSurface() ServedTimeConformanceSurface {
	return ServedTimeConformanceSurface{
		GoldenEnvironment:         timeconformance.GoldenEnvironment,
		DuplicatePunchEnvironment: timeconformance.DuplicatePunchEnvironment,
		PostLockEditEnvironment:   timeconformance.PostLockEditEnvironment,
		StaleReopenEnvironment:    timeconformance.StaleReopenEnvironment,
		SpoofSuspectedEnvironment: timeconformance.SpoofSuspectedEnvironment,
		OfflineReplayEnvironment:  timeconformance.OfflineReplayEnvironment,
		DSTFoldEnvironment:        timeconformance.DSTFoldEnvironment,
		ClockSkewEnvironment:      timeconformance.ClockSkewEnvironment,
		DegradedBridgeEnvironment: timeconformance.DegradedBridgeEnvironment,
		NewSetup:                  timeconformance.NewSetup,
		ReferenceWorkflowID:       func() string { return timeconformance.WorkflowID },
		CapabilityIDs: func() []string {
			keys := timeconformance.CapabilityIDs()
			ids := make([]string, 0, len(keys))
			for _, key := range keys {
				ids = append(ids, key.ID)
			}
			return ids
		},
	}
}

// TimeConformance returns the immutable Time conformance surface
// exposed by a composed application. A nil application has no capabilities.
func (a *App) TimeConformance() ServedTimeConformanceSurface {
	if a == nil {
		return ServedTimeConformanceSurface{}
	}
	return NewServedTimeConformanceSurface()
}
