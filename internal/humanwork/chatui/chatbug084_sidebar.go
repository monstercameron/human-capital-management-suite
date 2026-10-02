package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"

// chatbug084Sections drops archived channels from the sidebar sections: an
// archived channel is listed only under Archived at the foot of the rail. The
// channel that is open stays where it was, so the row that is highlighted never
// disappears under the person reading it.
func chatbug084Sections(m Model, sections []SidebarSection) []SidebarSection {
	if len(m.ChannelStatuses) == 0 {
		return sections
	}
	out := make([]SidebarSection, 0, len(sections))
	for _, section := range sections {
		kept := make([]Conversation, 0, len(section.Chats))
		for _, c := range section.Chats {
			if view, ok := m.ChannelStatuses[c.ID]; ok && view.Status.Status == chatpolicy.StatusArchived && c.ID != m.SelectedID {
				continue
			}
			kept = append(kept, c)
		}
		section.Chats = kept
		out = append(out, section)
	}
	return out
}
