package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"

// A task has one owner. When it names a second person with @, the preview says
// who owns it, who does not, and lets the author choose (#3952).
var chatcmd003Issue3952Copy = map[string][3]string{
	"extra-person": {"“{task}” is assigned to {owner}. {token} is named too, but a task has one owner, so {token} is not assigned. Pick the owner below, or post one task for each person.",
		"„{task}“ ist {owner} zugewiesen. {token} wird auch genannt, aber eine Aufgabe hat nur eine zuständige Person, deshalb ist {token} nicht zugewiesen. Wählen Sie unten die zuständige Person oder veröffentlichen Sie für jede Person eine eigene Aufgabe.",
		"«{task}» مسندة إلى {owner}. ورد {token} أيضًا، لكن للمهمة مسؤول واحد فقط، لذا لم تُسند إليه. اختر المسؤول أدناه أو انشر مهمة منفصلة لكل شخص."},
	"assign-keep": {"Keep {owner} as the owner", "{owner} als zuständige Person behalten", "إبقاء {owner} مسؤولًا"},
}

func init() {
	for key, value := range chatcmd003Issue3952Copy {
		chatcmd003Copy[key] = value
	}
}

// chatcmd003UnassignedText is the status line of a name left unassigned: that
// it fits nobody, or, for a second person named in a task that already has an
// owner, who owns it and that the second person is not assigned.
func chatcmd003UnassignedText(m Model, u chat.Chatcmd004Unresolved, task string) string {
	if u.Owner != "" {
		return chatcmd002Fill(chatcmd003Text(m, "extra-person"), "token", u.Token, "task", task, "owner", u.Owner)
	}
	return chatcmd002Fill(chatcmd003Text(m, "unassigned"), "token", u.Token, "task", task)
}

// chatcmd003KeepText is the choice that leaves the task as it was read.
func chatcmd003KeepText(m Model, u chat.Chatcmd004Unresolved) string {
	if u.Owner != "" {
		return chatcmd002Fill(chatcmd003Text(m, "assign-keep"), "owner", u.Owner)
	}
	return chatcmd002Fill(chatcmd003Text(m, "assign-text"), "token", u.Token)
}

// chatcmd003DropOwnerChange removes the change line that named the owner a
// choice replaced, so the list of changes does not name two owners.
func chatcmd003DropOwnerChange(changes []chat.Chatcmd003Change, u chat.Chatcmd004Unresolved) []chat.Chatcmd003Change {
	if u.Owner == "" {
		return changes
	}
	for i, change := range changes {
		if change.Kind == chat.Chatcmd004ChangeAssignee && change.After == u.Owner {
			return append(changes[:i:i], changes[i+1:]...)
		}
	}
	return changes
}
