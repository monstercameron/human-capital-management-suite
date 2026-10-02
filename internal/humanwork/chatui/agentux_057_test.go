package chatui

import (
	"testing"
	"unicode/utf16"
)

// agentux057Step is one key press in the composer, with what the field and
// the open lists look like at that moment, and what the key must do.
type agentux057Step struct {
	what  string
	key   string
	state composerKeyState
	want  composerAction
}

// The whole keyboard sequence, without a pointer: "@pol", pick the agent from
// the list, type the question, Enter. Enter sends whenever no list is open,
// including directly after the mention was inserted by Enter, by Tab or by a
// click; while a list is open Enter picks from it and the next Enter sends.
// It is walked in a channel and in a direct conversation, for a one-word and a
// two-word agent name.
func TestTodo_AGENTUX_057(t *testing.T) {
	const question = "how many PTO hours carry over?"
	for _, direct := range []bool{false, true} {
		for _, name := range []string{"Assistant", "Policy Helper"} {
			for _, insert := range []string{"Enter", "Tab", "click"} {
				model := chat4Fixture("en-US", "sent", direct)
				reference := model.ResolvedPersonaMentions[0].Reference
				reference.Display = name
				store := mentionStore{box: &mentionBox{}}
				store.ForConversation(model.SelectedID)
				open := mentionState{Target: "chat-composer", Query: "pol", Start: 0, End: 4, Open: true}
				store.Set(open)

				// 1. The list is open on "@pol": the key picks, it does not send.
				if insert != "click" {
					if got := composerKeyAction(composerKeyState{Draft: "@pol", MentionOpen: true, MentionHighlighted: true}, insert); got != composerPickMention {
						t.Fatalf("direct=%v %s %s: with the list open the key did %q, want a pick", direct, name, insert, got)
					}
				}
				// The pick, as mentionPick does it for an agent: the typed token goes,
				// the agent becomes a chip beside the field, and the list closes.
				value, caret := insertEmojiAtUTF16("@pol", "", open.Start, open.End)
				store.AddPersonaToken("chat-composer", model.SelectedID, reference)
				store.Set(mentionState{})
				if value != "" || caret != 0 || store.Get().Open {
					t.Fatalf("direct=%v %s %s: the pick left %q in the field or the list open", direct, name, insert, value)
				}

				steps := []agentux057Step{
					// 2. Enter with only the chip: nothing to send, and nothing is latched.
					{"Enter on the chip alone", "Enter", composerKeyState{Draft: value}, composerEmpty},
				}
				// 3. The question is typed; no "@" is in the field, so no list opens.
				value = question
				units := len(utf16.Encode([]rune(value)))
				if live := liveComposerMention(store.Get(), "chat-composer", value, units); live.Open {
					t.Fatalf("direct=%v %s %s: typing the question reopened the list", direct, name, insert)
				}
				// A state left over from before the pick cannot own Enter either.
				if stale := liveComposerMention(open, "chat-composer", value, units); stale.Open {
					t.Fatalf("direct=%v %s %s: a list closed by the pick still owns the keys", direct, name, insert)
				}
				steps = append(steps,
					agentux057Step{"Enter after the question", "Enter", composerKeyState{Draft: value}, composerSend},
					agentux057Step{"Enter again", "Enter", composerKeyState{Draft: value}, composerSend},
					agentux057Step{"Tab does not send", "Tab", composerKeyState{Draft: value}, composerNative},
					agentux057Step{"Shift+Enter is a new line", "Enter", composerKeyState{Draft: value, Shift: true}, composerNative},
					// 4. An emoji list opened while typing: Enter picks from it once, then sends.
					agentux057Step{"Enter with the emoji list open", "Enter", composerKeyState{Draft: value + " :smi", EmojiOpen: true, EmojiHighlighted: true}, composerPickEmoji},
					agentux057Step{"Enter after the emoji was picked", "Enter", composerKeyState{Draft: value + " 🙂"}, composerSend},
					// A list that is open with nothing to pick does not swallow Enter.
					agentux057Step{"Enter with an empty mention list", "Enter", composerKeyState{Draft: value + " @zz", MentionOpen: true}, composerSend},
				)
				for _, step := range steps {
					if got := composerKeyAction(step.state, step.key); got != step.want {
						t.Fatalf("direct=%v %s %s: %s did %q, want %q", direct, name, insert, step.what, got, step.want)
					}
				}

				// 5. What is sent: the question, with the agent it was put to.
				body, references, ready := composerSendPayload(value, "", store.PersonaReferences("chat-composer", model.SelectedID, value))
				if !ready || body != question || len(references) != 1 || references[0].ID != reference.ID {
					t.Fatalf("direct=%v %s %s: sends %q with %+v", direct, name, insert, body, references)
				}
				if !direct {
					if shown := bodyWithAgentMentions(body, references); shown != "@"+name+" "+question {
						t.Fatalf("%s %s: the message reads %q", name, insert, shown)
					}
				}
			}
		}
	}
}
