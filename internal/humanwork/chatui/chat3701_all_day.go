package chatui

// ISSUE-3701: the recipient rule treats an equal From and Until as paused all
// day, so the page says that in words instead of repeating one clock twice.
const (
	keyQuiet3701Summary = "chat.quiet3701.summary"
	keyQuiet3701Value   = "chat.quiet3701.value"
	keyQuiet3701Tip     = "chat.quiet3701.tip"
)

var quiet3701Copy = map[string]map[string]string{
	"en-US": {keyQuiet3701Summary: "Paused all day", keyQuiet3701Value: "All day", keyQuiet3701Tip: "Quiet hours are on: all day"},
	"de-DE": {keyQuiet3701Summary: "Ganztägig pausiert", keyQuiet3701Value: "Ganztägig", keyQuiet3701Tip: "Ruhezeiten sind aktiv: ganztägig"},
	"ar":    {keyQuiet3701Summary: "متوقف طوال اليوم", keyQuiet3701Value: "طوال اليوم", keyQuiet3701Tip: "ساعات الهدوء مفعّلة: طوال اليوم"},
}

// quietAllDay reports a window whose boundaries are equal.
func quietAllDay(p Preferences) bool { return p.QuietStartMinute == p.QuietEndMinute }

func quiet3701Text(m Model, key string) string { return laneText(m, quiet3701Copy, key) }
