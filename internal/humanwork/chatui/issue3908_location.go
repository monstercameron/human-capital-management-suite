package chatui

import (
	"fmt"
	"strings"
)

// Location sheet copy and hints for #3908 (destination named), #3863 (duration
// hint follows the choice and the policy) and #3862 (address lookup
// unavailable). ChatmapText asks chatmapSweepText first.

var chatmapSweepCopy = map[string][3]string{
	"audience_direct": {"Only you and %s can see this.", "Nur Sie und %s können dies sehen.", "أنت و%s فقط يمكنكما رؤية هذا."},
	"audience":        {"Everyone in %s can see this.", "Alle in %s können dies sehen.", "يستطيع الجميع في %s رؤية هذا."},
	"retain_keep":     {"The location stays with the message, but it is removed after at most %s, even if the message stays. After that, readers see that it is no longer shared.", "Der Standort bleibt bei der Nachricht, wird aber nach höchstens %s entfernt, auch wenn die Nachricht bleibt. Danach sehen Leser, dass er nicht mehr geteilt wird.", "يبقى الموقع مع الرسالة، لكنه يُزال بعد %s على الأكثر حتى لو بقيت الرسالة. بعد ذلك يرى القراء أنه لم يعد مشاركاً."},
	"retain_timed":    {"Sharing stops at the time you choose. Your workspace keeps a location for at most %s.", "Das Teilen endet zur gewählten Zeit. Ihr Arbeitsbereich bewahrt einen Standort höchstens %s auf.", "تنتهي المشاركة في الوقت الذي تختاره. تحتفظ مساحة العمل بالموقع %s على الأكثر."},
	"lookup_alt":      {"Choose %s or %s to share a place instead.", "Wählen Sie stattdessen %s oder %s, um einen Ort zu teilen.", "اختر %s أو %s لمشاركة مكان بدلاً من ذلك."},
	"unresolved":      {"Find the address first, so you can check where it will be shared. Send stays off until then.", "Suchen Sie zuerst die Adresse, damit Sie prüfen können, wo sie geteilt wird. Senden bleibt bis dahin aus.", "ابحث عن العنوان أولاً لتتحقق من مكان مشاركته. يبقى الإرسال متوقفاً حتى ذلك الحين."},
}

// chatmapSweepText answers the keys above, and "lookup_full" (the lookup
// failure with its alternative). It returns "" for any other key.
func chatmapSweepText(locale, key string) string {
	if key == "lookup_full" {
		return ChatmapText(locale, "lookup") + " " + fmt.Sprintf(ChatmapText(locale, "lookup_alt"), ChatmapText(locale, "where"), ChatmapText(locale, "site"))
	}
	if key == "retention" {
		// The hint a sheet opens with, before a duration or policy is known.
		return ChatmapDurationHint(locale, "hour", 0)
	}
	values, ok := chatmapSweepCopy[key]
	if !ok {
		return ""
	}
	index := 0
	switch {
	case locale == "de-DE":
		index = 1
	case locale == "ar":
		index = 2
	}
	return values[index]
}

// ChatmapAudienceText names who will see a share and where it goes, for
// example "Everyone in #benefits can see this."
func ChatmapAudienceText(locale, destination string) string {
	return fmt.Sprintf(ChatmapText(locale, "audience"), destination)
}

// ChatmapDurationHint explains what the chosen duration means for how long the
// location lasts. maxRetentionSeconds is the longest the channel keeps a
// location (zero when the policy is not known yet: the 24 hour limit holds).
// Live choices explain themselves in the live note, so they give "".
func ChatmapDurationHint(locale, choice string, maxRetentionSeconds int) string {
	if strings.HasPrefix(choice, "live") {
		return ""
	}
	if maxRetentionSeconds <= 0 || maxRetentionSeconds > 86400 {
		maxRetentionSeconds = 86400
	}
	limit := chatmapAdminDuration(locale, maxRetentionSeconds)
	if choice == "keep" {
		return fmt.Sprintf(ChatmapText(locale, "retain_keep"), limit)
	}
	return fmt.Sprintf(ChatmapText(locale, "retain_timed"), limit)
}

// chatmapDestination is the name the sheet gives the open conversation: the
// channel as "#name", a direct conversation by the other person's name.
func chatmapDestination(m Model) string {
	if m.SelectedID == "" {
		return ""
	}
	if name := agentLimitConversation(m); name != "" {
		return name
	}
	return strings.TrimSpace(displayName(m, m.selected()))
}

// chatmapAudience is the line the sheet shows under its explanation: who will
// see a share in the open conversation. A direct conversation is only the other
// person, so it does not say "everyone".
func chatmapAudience(m Model) string {
	destination := chatmapDestination(m)
	if destination == "" {
		return ""
	}
	if m.selected().Kind == DirectMessage {
		return fmt.Sprintf(ChatmapText(m.Locale, "audience_direct"), destination)
	}
	return ChatmapAudienceText(m.Locale, destination)
}
