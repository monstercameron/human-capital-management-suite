package productui

import (
	"fmt"
	"strings"
)

// AGENTUX-050. The last step of a staged rollout says what happened in one
// sentence, with the version, instead of the generic "check progress here".
// Every earlier stage keeps the sentence agentRolloutStage gives it.
var agentux050Done = map[string]string{
	"en-US": "Done: every selected conversation runs version {version}.",
	"de-DE": "Fertig: Jede ausgewählte Unterhaltung nutzt jetzt Version {version}.",
	"ar":    "تم: تعمل كل محادثة محددة الآن بالإصدار {version}.",
}

// agentRolloutStageFor is the status sentence under the rollout's buttons.
func agentRolloutStageFor(locale LocaleContext, stage string, version int64) string {
	if strings.EqualFold(stage, "COMPLETE") && version > 0 {
		text := agentux050Done[locale.Resolved]
		if text == "" {
			text = agentux050Done["en-US"]
		}
		return strings.ReplaceAll(text, "{version}", locale.FormatNumber(fmt.Sprint(version), 0))
	}
	return agentRolloutStage(locale, stage)
}
