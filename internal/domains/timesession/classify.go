package timesession

// Reason is the classification route key WTIME-003's DECISION uses. Names
// that overlap internal/workflow/conformance/time's fixture routes
// (definition.go's RouteDuplicatePunch, RouteReviewSpoofSuspected,
// RouteReviewOfflineReplay, RouteReviewDSTFold, RouteReviewClockSkew) reuse
// those exact strings so a rule pack can share vocabulary across the pure
// domain layer and the workflow layer; the four legal/compliance holds this
// package adds (rest breach, minor window, early lockout, geofence) are not
// in that fixture and get their own names.
type Reason string

const (
	ReasonDuplicatePunch = Reason("DUPLICATE_PUNCH")
	ReasonRestBreach     = Reason("REST_BREACH")
	ReasonMinorWindow    = Reason("MINOR_WINDOW")
	ReasonEarlyLockout   = Reason("EARLY_LOCKOUT")
	ReasonGeofence       = Reason("GEOFENCE")
	ReasonSpoofSuspected = Reason("REVIEW_SPOOF_SUSPECTED")
	ReasonOfflineReplay  = Reason("REVIEW_OFFLINE_REPLAY")
	ReasonDSTFold        = Reason("REVIEW_DST_FOLD")
	ReasonClockSkew      = Reason("REVIEW_CLOCK_SKEW")
)

// Decision is Classify's coarse-grained result.
type Decision string

const (
	DecisionAccept    Decision = "ACCEPT"
	DecisionReview    Decision = "REVIEW"
	DecisionHold      Decision = "HOLD"
	DecisionDuplicate Decision = "DUPLICATE"
)

// PunchFacts is the resolved evidence a punch's classification is based on.
// Classify never resolves any of these itself; an earlier capability read
// (device/clock context, geofence, worker age/permit, rest-period ledger)
// supplies them as plain facts.
type PunchFacts struct {
	DuplicatePunchDetected     bool
	RestBreachDetected         bool
	MinorWindowViolation       bool
	EarlyLockout               bool
	OutsideGeofence            bool
	SharedDeviceSpoofSuspected bool
	OfflineReplayDetected      bool
	DSTFoldAmbiguous           bool
	ClockSkewWithinTolerance   bool
}

// Classification is Classify's pure result: one decision and, for every
// decision but ACCEPT, the reason and fixed precedence that produced it.
type Classification struct {
	Decision   Decision
	Reason     Reason
	Precedence int
}

type classifyRule struct {
	precedence int
	decision   Decision
	reason     Reason
	match      func(PunchFacts) bool
}

// classifyRules is the fixed, ascending precedence table: a duplicate punch
// is checked before any legal/compliance hold, every hold before every
// merely reviewable integrity concern, and every reason gets its own route
// rather than a generic "needs review" or "blocked".
var classifyRules = [...]classifyRule{
	{10, DecisionDuplicate, ReasonDuplicatePunch, func(f PunchFacts) bool { return f.DuplicatePunchDetected }},
	{20, DecisionHold, ReasonRestBreach, func(f PunchFacts) bool { return f.RestBreachDetected }},
	{30, DecisionHold, ReasonMinorWindow, func(f PunchFacts) bool { return f.MinorWindowViolation }},
	{40, DecisionHold, ReasonEarlyLockout, func(f PunchFacts) bool { return f.EarlyLockout }},
	{50, DecisionHold, ReasonGeofence, func(f PunchFacts) bool { return f.OutsideGeofence }},
	{60, DecisionReview, ReasonSpoofSuspected, func(f PunchFacts) bool { return f.SharedDeviceSpoofSuspected }},
	{70, DecisionReview, ReasonOfflineReplay, func(f PunchFacts) bool { return f.OfflineReplayDetected }},
	{80, DecisionReview, ReasonDSTFold, func(f PunchFacts) bool { return f.DSTFoldAmbiguous }},
	{90, DecisionReview, ReasonClockSkew, func(f PunchFacts) bool { return !f.ClockSkewWithinTolerance }},
}

// Classify applies the fixed-precedence classifier over supplied facts. It
// is allocation-light: classifyRules is built once at package
// initialization, and Classify itself allocates nothing beyond the returned
// value, so it is cheap to run per punch.
func Classify(facts PunchFacts) Classification {
	for _, rule := range classifyRules {
		if rule.match(facts) {
			return Classification{Decision: rule.decision, Reason: rule.reason, Precedence: rule.precedence}
		}
	}
	return Classification{Decision: DecisionAccept, Precedence: 100}
}
