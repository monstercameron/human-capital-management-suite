package chatui

import "strings"

// chatux019ReactionLabel is what a reaction chip is called to a screen reader
// and in its tooltip: who reacted and with what, in words. "You and Sam reacted
// with eyes", not "2 reacted with" and a glyph. Names are listed when every
// reactor is known and there are at most three; otherwise it is the count.
func chatux019ReactionLabel(m Model, chip ReactionChip) string {
	emoji := chatux019EmojiName(m, chip.Emoji)
	names := make([]string, 0, len(chip.PeopleIDs)+1)
	if chip.Mine {
		names = append(names, laneText(m, chatux021Copy, keyChatux019You))
	}
	for _, id := range chip.PeopleIDs {
		if name := strings.TrimSpace(chatKnownName(m, id)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 || len(names) != chip.Count || len(names) > 3 {
		return laneTextf(m, chatux021Copy, keyChatux019ReactedCount, map[string]string{"n": m.n(chip.Count), "emoji": emoji})
	}
	// "You" takes the plural verb in the languages that tell them apart.
	key := keyChatux019ReactedOne
	if len(names) > 1 || chip.Mine {
		key = keyChatux019ReactedMany
	}
	return laneTextf(m, chatux021Copy, key, map[string]string{"names": chatux019JoinNames(m, names), "emoji": emoji})
}

// chatux019JoinNames writes a short list the way the reader's language does:
// "A", "A and B", "A, B and C".
func chatux019JoinNames(m Model, names []string) string {
	and := laneText(m, chatux021Copy, keyChatux019And)
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	list := strings.Join(names[:len(names)-1], ", ")
	if chatEmojiLocale(m.Locale) == "ar" {
		// Arabic writes "and" as a prefix of the next word.
		return list + " " + and + names[len(names)-1]
	}
	return list + " " + and + " " + names[len(names)-1]
}

// chatux019EmojiName is the emoji's name in the reader's language: from the
// emoji data once it has loaded, from the short table of the reactions people
// reach for until then, and "an emoji" for anything else. The glyph itself is
// never the name, since a screen reader may skip it or read it differently.
func chatux019EmojiName(m Model, glyph string) string {
	if ix := chatEmojiData.index; ix != nil {
		if i, ok := ix.Lookup(glyph); ok {
			if name := ix.Name(i); name != "" {
				return name
			}
		}
	} else {
		chatEmojiEnsureData(false)
	}
	key := map[string]string{
		"👍": keyChatux019EmojiThumbsUp, "❤": keyChatux019EmojiHeart, "😂": keyChatux019EmojiJoy, "🎉": keyChatux019EmojiParty,
		"👀": keyChatux019EmojiEyes, "🙏": keyChatux019EmojiFolded, "✅": keyChatux019EmojiCheck, "🔥": keyChatux019EmojiFire,
	}[strings.ReplaceAll(glyph, "️", "")]
	if key == "" {
		key = keyChatux019EmojiUnnamed
	}
	return laneText(m, chatux021Copy, key)
}
