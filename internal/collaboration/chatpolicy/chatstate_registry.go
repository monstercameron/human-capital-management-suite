package chatpolicy

// ChannelStatus is persisted in the conversation lifecycle column.
type ChannelStatus string

const (
	StatusOpen          ChannelStatus = "ACTIVE"
	StatusAnnouncements ChannelStatus = "ANNOUNCEMENTS"
	StatusLocked        ChannelStatus = "LOCKED"
	StatusArchived      ChannelStatus = "ARCHIVED"
)

type StatusAction uint8

const (
	StatusPost StatusAction = iota
	StatusReply
	StatusReact
	StatusEdit
	StatusPin
	StatusManageMembers
	StatusRename
)

type StatusRule struct {
	Status  ChannelStatus
	Allowed [7]bool
}

// StatusRegistry returns a copy so callers cannot mutate the policy table.
func StatusRegistry() [4]StatusRule {
	return [4]StatusRule{
		{StatusOpen, [7]bool{true, true, true, true, true, true, true}},
		{StatusAnnouncements, [7]bool{false, true, true, false, true, true, true}},
		{StatusLocked, [7]bool{false, false, false, false, false, true, true}},
		{StatusArchived, [7]bool{}},
	}
}

func NormalizeChannelStatus(s ChannelStatus) ChannelStatus {
	if s == "" || s == "OPEN" {
		return StatusOpen
	}
	return s
}

func StatusAllows(s ChannelStatus, action StatusAction, announcer, publicDelivery bool) bool {
	s = NormalizeChannelStatus(s)
	if action > StatusRename {
		return false
	}
	for _, rule := range StatusRegistry() {
		if rule.Status == s {
			if s == StatusAnnouncements && (action == StatusPost || action == StatusEdit) {
				return announcer || publicDelivery
			}
			return rule.Allowed[action]
		}
	}
	return false
}

const PermissionChangeChannelStatus = "Change channel status"
const PermissionPostAnnouncements = "Post announcements"

// WorkspaceAdministratorRole is the existing tenant administrator role.
const WorkspaceAdministratorRole = "hcm_admin"

type StatusPermissions struct {
	ChannelAdmin, WorkspaceAdmin      bool
	ChangeOpen, ChangeRestricted      bool
	PostAnnouncements, PublicDelivery bool
}

func CanChangeChannelStatus(from, to ChannelStatus, p StatusPermissions) bool {
	from, to = NormalizeChannelStatus(from), NormalizeChannelStatus(to)
	valid, validFrom := false, false
	for _, rule := range StatusRegistry() {
		if rule.Status == to {
			valid = true
		}
		if rule.Status == from {
			validFrom = true
		}
	}
	if !valid || !validFrom || from == to {
		return false
	}
	if from == StatusLocked || from == StatusArchived || to == StatusLocked || to == StatusArchived {
		return p.WorkspaceAdmin || p.ChangeRestricted
	}
	return p.ChannelAdmin || p.WorkspaceAdmin || p.ChangeOpen
}
