package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentUX073Text(locale LocaleContext, key string, replacements ...string) string {
	copy := map[string][3]string{
		"agents":             {"Agents", "Agenten", "الوكلاء"},
		"agent_version":      {"Version {version}", "Version {version}", "الإصدار {version}"},
		"state_answering":    {"Answering", "Antwortet", "يجيب"},
		"state_paused":       {"Paused", "Pausiert", "متوقف مؤقتًا"},
		"filter_agent":       {"Agent", "Agent", "الوكيل"},
		"failed_after":       {"Failed after {duration}", "Nach {duration} fehlgeschlagen", "فشل بعد {duration}"},
		"agents_unavailable": {"The list of agents could not be loaded here.", "Die Liste der Agenten konnte hier nicht geladen werden.", "تعذر تحميل قائمة الوكلاء هنا."},
		"empty_help_runs":    {"Recent finished runs are listed below.", "Kürzlich beendete Ausführungen stehen unten.", "تظهر عمليات التشغيل المنتهية حديثًا أدناه."},
		"failed_at_once":     {"Failed in under a second", "In unter einer Sekunde fehlgeschlagen", "فشل في أقل من ثانية"},
		"and":                {"{hours} {minutes}", "{hours} {minutes}", "{hours} و{minutes}"},
		"cadence_once":       {"Once", "Einmalig", "مرة واحدة"},
		"cadence_daily":      {"Every day at {time}", "Täglich um {time}", "كل يوم في {time}"},
		"cadence_weekly":     {"Every {days} at {time}", "Jeden {days} um {time}", "كل {days} في {time}"},
		"cadence_monthly":    {"Day {day} of every month at {time}", "Am {day}. jedes Monats um {time}", "اليوم {day} من كل شهر في {time}"},
	}
	text := copy[key][agentRPLocaleIndex(locale)]
	if len(replacements) > 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	return text
}

// agentUX073AgentRows lists the agents on Activity as rows: the icon the agent
// wears everywhere, its name and version, whether it is answering, and the
// control that pauses or resumes it. Only an agent that is live or paused has
// a state an owner can change here, so drafts and retired agents are left to
// Agent setup.
func agentUX073AgentRows(locale LocaleContext, snapshot AgentControlsSnapshot) ui.Node {
	siblings := make([]string, 0, len(snapshot.Agents))
	for _, persona := range snapshot.Agents {
		siblings = append(siblings, persona.ID)
	}
	rows := make([]ui.Node, 0, len(snapshot.Agents))
	for _, persona := range snapshot.Agents {
		if persona.Lifecycle != PersonaPublished && persona.Lifecycle != PersonaSuspended {
			continue
		}
		state, tone := agentUX073Text(locale, "state_answering"), "success"
		if persona.Lifecycle == PersonaSuspended {
			state, tone = agentUX073Text(locale, "state_paused"), "warning"
		}
		identity := []ui.Node{html.Strong(html.Props{Dir: "auto"}, ui.Text(persona.Name))}
		if version := strings.TrimSpace(persona.Version); version != "" {
			identity = append(identity, html.Small(html.Props{}, ui.Text(agentUX073Text(locale, "agent_version", "{version}", personaAdminLocalizedNumber(locale, version)))))
		}
		rows = append(rows, html.Li(html.Props{Class: "agent-owner-pause-row", Raw: map[string]any{"data-persona-id": persona.ID}},
			html.Span(html.Props{Class: "agent-activity-icon", Aria: map[string]string{"hidden": "true"}}, agentUX074Icon(persona.Icon, persona.ID, siblings)),
			html.Div(html.Props{Class: "agent-activity-identity"}, identity...),
			html.Span(html.Props{Class: "status agent-activity-state", Raw: map[string]any{"data-tone": tone}}, ui.Text(state)),
			personaAdminPauseButton(locale, persona, PersonaAdminSnapshot{CommandPermissionsAvailable: true, AllowedCommands: snapshot.AllowedCommands}),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "agent-activity-agent-section", Aria: map[string]string{"labelledby": "agent-activity-agents-title"}},
		html.H3(html.Props{ID: "agent-activity-agents-title", Class: "agent-activity-heading"}, ui.Text(agentUX073Text(locale, "agents"))),
		html.Ul(html.Props{Class: "agent-activity-agents"}, rows...),
	)
}

// agentUX073Duration writes a length of time the way a person would say it:
// "1 hour", "45 minutes", "1 hour 5 minutes". The unit agrees with the number
// in each language, including the Arabic singular, dual and plural.
func agentUX073Duration(locale LocaleContext, duration time.Duration) string {
	if duration < time.Second {
		return agentUXR7Text(locale, "under_second")
	}
	if duration < time.Minute {
		return agentUX073Unit(locale, "second", int(duration.Round(time.Second)/time.Second))
	}
	if duration < time.Hour {
		minutes := int(duration.Round(time.Minute) / time.Minute)
		if minutes < 60 {
			return agentUX073Unit(locale, "minute", minutes)
		}
		duration = time.Hour
	}
	hours := int(duration / time.Hour)
	minutes := int((duration - time.Duration(hours)*time.Hour) / time.Minute)
	if minutes == 0 {
		return agentUX073Unit(locale, "hour", hours)
	}
	return agentUX073Text(locale, "and", "{hours}", agentUX073Unit(locale, "hour", hours), "{minutes}", agentUX073Unit(locale, "minute", minutes))
}

func agentUX073Unit(locale LocaleContext, unit string, count int) string {
	number := locale.FormatNumber(strconv.Itoa(count), 0)
	switch agentRPLocaleIndex(locale) {
	case 1:
		forms := map[string][2]string{"second": {"Sekunde", "Sekunden"}, "minute": {"Minute", "Minuten"}, "hour": {"Stunde", "Stunden"}}[unit]
		if count == 1 {
			return number + " " + forms[0]
		}
		return number + " " + forms[1]
	case 2:
		// Arabic counts one and two with the noun alone, three to ten with the
		// plural, and eleven and above with the singular.
		forms := map[string][4]string{
			"second": {"ثانية واحدة", "ثانيتان", "ثوانٍ", "ثانية"},
			"minute": {"دقيقة واحدة", "دقيقتان", "دقائق", "دقيقة"},
			"hour":   {"ساعة واحدة", "ساعتان", "ساعات", "ساعة"},
		}[unit]
		switch {
		case count == 1:
			return forms[0]
		case count == 2:
			return forms[1]
		case count <= 10:
			return number + " " + forms[2]
		default:
			return number + " " + forms[3]
		}
	default:
		if count == 1 {
			return number + " " + unit
		}
		return number + " " + unit + "s"
	}
}

// agentUX073RunDuration is the duration cell of one run. A failed run says how
// long it ran before it failed, so the number is not read as a slow success.
func agentUX073RunDuration(locale LocaleContext, run AgentControlRun) string {
	label := agentRunDurationLabel(locale, run.Duration)
	if strings.EqualFold(strings.TrimSpace(run.State), "FAILED") && strings.TrimSpace(label) != "" {
		if duration, err := time.ParseDuration(run.Duration); err == nil {
			// "Under a second" is a sentence of its own; inside this one it is
			// written out so no capital lands mid-sentence.
			if duration < time.Second {
				return agentUX073Text(locale, "failed_at_once")
			}
			return agentUX073Text(locale, "failed_after", "{duration}", label)
		}
	}
	return label
}

// agentUX073AnnouncementCadence says when an announcement posts, from the same
// values its editor holds. The time is the owner's chosen local time and zone.
func agentUX073AnnouncementCadence(locale LocaleContext, value AgentAnnouncementEditorValue) string {
	at := strings.TrimSpace(value.Time)
	if zone := strings.TrimSpace(value.Zone); zone != "" && at != "" {
		at += " (" + zone + ")"
	}
	switch strings.ToUpper(strings.TrimSpace(value.Cadence)) {
	case "NOW", "ONCE":
		return agentUX073Text(locale, "cadence_once")
	case "DAILY":
		return agentUX073Text(locale, "cadence_daily", "{time}", at)
	case "WEEKLY":
		names := announcementWeekdayNames(locale)
		days := make([]string, 0, len(value.Weekdays))
		for _, day := range value.Weekdays {
			if day >= 0 && day < len(names) {
				days = append(days, names[day])
			}
		}
		if len(days) == 0 {
			return ""
		}
		separator := ", "
		if locale.Resolved == "ar" {
			separator = "، "
		}
		return agentUX073Text(locale, "cadence_weekly", "{days}", strings.Join(days, separator), "{time}", at)
	case "MONTHLY":
		if value.MonthDay < 1 {
			return ""
		}
		return agentUX073Text(locale, "cadence_monthly", "{day}", locale.FormatNumber(strconv.Itoa(value.MonthDay), 0), "{time}", at)
	}
	return ""
}
