package journey

// JourneyDetailLayoutState is the business state used by the reviewed
// journey-detail hierarchy. It is deliberately separate from localized
// status copy so the renderer cannot infer layout from translated text.
type JourneyDetailLayoutState string

const (
	JourneyDetailActive   JourneyDetailLayoutState = "active"
	JourneyDetailBlocked  JourneyDetailLayoutState = "blocked"
	JourneyDetailWaiting  JourneyDetailLayoutState = "waiting"
	JourneyDetailRejected JourneyDetailLayoutState = "rejected"
	JourneyDetailFailed   JourneyDetailLayoutState = "failed"
	JourneyDetailRecorded JourneyDetailLayoutState = "recorded"
)

// JourneyDetailHierarchyFixture is the executable golden decision for one
// lifecycle state. Section names are semantic slots, not CSS selectors or
// page-local markup.
type JourneyDetailHierarchyFixture struct {
	State               JourneyDetailLayoutState
	FirstViewport       []string
	PrimaryAction       string
	SecondaryDisclosure bool
	Terminal            bool
}

// ReviewedJourneyDetailHierarchy is the reusable UXLIVE-035 decision:
// status and progress answer the reader's first question, one safe next step
// follows, and rare or destructive interventions are secondary. Terminal
// journeys show their outcome and an appropriate follow-up instead of a wall
// of disabled controls.
func ReviewedJourneyDetailHierarchy() []JourneyDetailHierarchyFixture {
	return []JourneyDetailHierarchyFixture{
		{State: JourneyDetailActive, FirstViewport: []string{"status-summary", "current-stage", "primary-action"}, PrimaryAction: "complete-current-task", SecondaryDisclosure: true},
		{State: JourneyDetailBlocked, FirstViewport: []string{"status-summary", "blocking-reason", "primary-action"}, PrimaryAction: "correct-or-recover", SecondaryDisclosure: true},
		{State: JourneyDetailWaiting, FirstViewport: []string{"status-summary", "current-stage", "wait-explanation"}, PrimaryAction: "track-until-safe-action", SecondaryDisclosure: true},
		{State: JourneyDetailRejected, FirstViewport: []string{"status-summary", "outcome", "primary-action"}, PrimaryAction: "review-decision", SecondaryDisclosure: true, Terminal: true},
		{State: JourneyDetailFailed, FirstViewport: []string{"status-summary", "outcome", "primary-action"}, PrimaryAction: "review-recovery", SecondaryDisclosure: true, Terminal: true},
		{State: JourneyDetailRecorded, FirstViewport: []string{"status-summary", "outcome", "primary-action"}, PrimaryAction: "view-recorded-change", SecondaryDisclosure: true, Terminal: true},
	}
}
