package chat

// Channel extension values are transport-neutral application contracts. The
// data store keeps its own persistence models and the application layer maps
// between them.
type ChannelTodoSelectedMember struct {
	HomeTenantID string
	SubjectID    string
}

type ChannelTodoItem struct {
	ID, Text, CreatedBy, CreatedByHomeTenantID string
	Completed                                  bool
	CreatedAtUnix                              int64
	SourcePostID, CompletedBySubjectID         string
	CompletedByHomeTenantID                    string
	CompletedAtUnix                            int64
	CompletionMode                             string
	SelectedCompleters                         []ChannelTodoSelectedMember
	CanToggle, CanManageCompletionPolicy       bool
}

type ChannelTodoList struct {
	ConversationID string
	Revision       uint64
	Pinned         bool
	Items          []ChannelTodoItem
}

type ChannelTodoMutation struct {
	Operation, ItemID, Text string
	Completed, Pinned       bool
	SourcePostID            string
	CompletionMode          string
	SelectedCompleters      []ChannelTodoSelectedMember
}

type ChannelTeamMember struct {
	HomeTenantID, SubjectID, Role, RoleLabel string
}

type ChannelTeamWidget struct {
	ConversationID string
	Revision       uint64
	Pinned         bool
	Purpose        string
	Members        []ChannelTeamMember
	CanPin         bool
}

type ChannelProjectMilestone struct {
	ID, Text, Status, OwnerHomeTenantID, OwnerSubjectID, DueDate string
}

type ChannelProjectWidget struct {
	ConversationID string
	Revision       uint64
	Pinned         bool
	Title, Summary string
	Milestones     []ChannelProjectMilestone
	CanPin         bool
}

type ChannelWidgets struct {
	Team    ChannelTeamWidget
	Project ChannelProjectWidget
}

type ChannelWidgetMutation struct {
	Kind, Operation                                string
	Pinned                                         bool
	Purpose                                        string
	MemberHomeTenantID, MemberSubjectID, RoleLabel string
	Title, Summary, MilestoneID                    string
	Milestone                                      ChannelProjectMilestone
}

type ChannelPollOption struct {
	ID, Text string
	Count    int
}

type ChannelPoll struct {
	ConversationID string
	Revision       uint64
	Question       string
	Options        []ChannelPollOption
	MyOptionID     string
	TotalVotes     int
}

type ChannelPollMutation struct {
	Operation, Question string
	Options             []string
	OptionID            string
}
