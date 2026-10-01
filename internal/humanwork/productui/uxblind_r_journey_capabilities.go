package productui

// JourneyCapabilityAvailability is the narrow presentation signal for
// optional actions attached to a promotion journey. It is a published
// capability verdict, not an authorization decision; the target services
// still authorize every action when it runs.
type JourneyCapabilityAvailability struct {
	Documents bool
	Chat      bool
	Projects  bool
}
