package productui

// ReviewParticipantsProjection is a request-scoped, server-authorized view of
// frozen participant/reviewer graph edges. It contains no authority to load
// or broaden graph access.
type ReviewParticipantsProjection struct {
	Cycles []ReviewParticipantsCycleProjection
}

type ReviewParticipantsCycleProjection struct {
	CycleID       string
	CycleRevision uint64
	GraphRevision uint64
	GraphDigest   string
	Assignments   []ReviewParticipantAssignmentProjection
}

type ReviewParticipantAssignmentProjection struct {
	ParticipantID string
	ReviewerID    string
	Relationship  string
}
