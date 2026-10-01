package subscription

// Clock event kinds share the managed subscription authorization and delivery
// contract. Payload schemas are registered by the time application service.
const (
	// EventClockPunchAccepted reports a committed punch receipt.
	EventClockPunchAccepted EventKind = "clock.punch.accepted"
	// EventClockPunchRejected reports a durably recorded rejection.
	EventClockPunchRejected EventKind = "clock.punch.rejected"
	// EventClockSessionOpened reports a committed session opening.
	EventClockSessionOpened EventKind = "clock.session.opened"
	// EventClockSessionClosed reports a committed session closure.
	EventClockSessionClosed EventKind = "clock.session.closed"
	// EventClockExceptionRaised reports a scoped reconciliation exception.
	EventClockExceptionRaised EventKind = "clock.exception.raised"
	// EventClockTimecardApproved reports an approved timecard revision.
	EventClockTimecardApproved EventKind = "clock.timecard.approved"
	// EventClockTimecardReopened reports a governed timecard reopening.
	EventClockTimecardReopened EventKind = "clock.timecard.reopened"
	// EventClockDeviceOffline reports a fleet health alert.
	EventClockDeviceOffline EventKind = "clock.device.offline"
)
