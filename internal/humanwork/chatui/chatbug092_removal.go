package chatui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// moderationRemovalLines are the lines a removal item states about the message:
// who removed it and when, and, once it is back, who restored it and when. They
// are two people with two times. The item's last decision is not used for the
// removal line, because after a restore it names the restorer.
func moderationRemovalLines(m ModerationPageModel, item chat.ModerationItem, removedWhen string, now time.Time) []ui.Node {
	t := func(key string) string { return chatremoveText(m.Locale, key) }
	removedBy, restoredBy, restoredAt := item.RemovedBy, item.RestoredBy, item.RestoredAt
	// Items that carry only the last decision (an older reader) fall back to it:
	// a "remove" decision is the remover, a "restore" decision the restorer.
	if removedBy == "" && item.Decision == "remove" {
		removedBy = item.DecidedBy
	}
	if restoredBy == "" && item.Decision == "restore" {
		restoredBy, restoredAt = item.DecidedBy, item.DecidedAt
	}
	var lines []ui.Node
	if removedBy != "" {
		lines = append(lines, moderationRemovalLine("chatmod005-removal-line", moderationFill(t("removal_line"), "{name}", chatremoveName(m, removedBy), "{when}", removedWhen)))
	}
	if restoredBy != "" {
		lines = append(lines, moderationRemovalLine("chatmod005-restoration-line", moderationFill(t("decided_restore"), "{name}", chatremoveName(m, restoredBy), "{when}", moderationWhen(m.Locale, restoredAt, m.TimeZone, now))))
	}
	return lines
}

func moderationRemovalLine(class, text string) ui.Node {
	return html.P(html.Props{Class: "chatmod005-muted " + class, Dir: "auto", Text: text})
}

// moderationRestoredTag is the tag a removal item carries once the message is
// back, so the card does not read as a removal that still stands.
func moderationRestoredTag(m ModerationPageModel, item chat.ModerationItem) ui.Node {
	if item.Kind != "removal" || (item.RestoredBy == "" && item.Decision != "restore") {
		return nil
	}
	copy := [3]string{"Restored", "Wiederhergestellt", "مستعادة"}
	return html.Span(html.Props{Class: "chatmod005-kind chatmod005-restored", Text: copy[chatbug039LocaleIndex(m.Locale)]})
}
