package chatui

import "strings"

// CHATBUG-028: the "/" list names what each command takes. A person choosing
// between /poll and /todo saw two names and a description each and had to pick
// one to find out how to write it; the form of the arguments is on the row.

// ChatBug028Styles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatBug028Styles = `
.chat-workspace .command-option{flex-wrap:wrap}
.chat-workspace .command-option .command-args{flex:0 0 100%;min-width:0;margin:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font:.72rem/1.35 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
.chat-workspace .command-option.active .command-args{color:inherit;opacity:.85}
`

// composerCommandArguments is the form of what follows the command's name, as
// the registry gives it ("question? option, option or option"), or "" for a
// command that takes none.
func composerCommandArguments(command composerCommand) string {
	usage := strings.TrimSpace(command.Usage)
	usage = strings.TrimSpace(strings.TrimPrefix(usage, "/"+command.Name))
	return usage
}
