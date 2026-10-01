package workspace

// TranslateSelfClockStatus returns reviewed clock status vocabulary for the
// supported presentation locales. Unknown locales fail visibly through the
// ordinary catalog fallback behavior.
func TranslateSelfClockStatus(locale LocaleContext, status string) string {
	key := "time.clock.clocked_out"
	if status == "CLOCKED_IN" || status == "OPEN" {
		key = "time.clock.clocked_in"
	} else if status == "ON_BREAK" {
		key = "time.clock.on_break"
	}
	english := map[string]string{"time.clock.clocked_in": "Clocked in", "time.clock.clocked_out": "Clocked out", "time.clock.on_break": "On break"}[key]
	return clockCatalogValue(locale, key, english)
}

// TranslateSelfClockLastEvent returns the reviewed no-event label or the
// caller's already formatted localized event text.
func TranslateSelfClockLastEvent(locale LocaleContext, event string) string {
	if event == "" {
		event = "No event"
	} else if event != "No event" {
		return event
	}
	return clockCatalogValue(locale, "time.last_event", event)
}

func clockCatalogValue(locale LocaleContext, key, fallback string) string {
	switch locale.Resolved {
	case "en-US":
		if key == "time.clock.clocked_in" {
			return "Clocked in"
		}
		if key == "time.clock.clocked_out" {
			return "Clocked out"
		}
		if key == "time.clock.on_break" {
			return "On break"
		}
		if key == "time.last_event" {
			return "No event"
		}
	case "de-DE":
		if key == "time.clock.clocked_in" {
			return "Eingestempelt"
		}
		if key == "time.clock.clocked_out" {
			return "Ausgestempelt"
		}
		if key == "time.clock.on_break" {
			return "In Pause"
		}
		if key == "time.last_event" {
			return "Kein Ereignis"
		}
	case "ar":
		if key == "time.clock.clocked_in" {
			return "تم تسجيل الدخول"
		}
		if key == "time.clock.clocked_out" {
			return "تم تسجيل الخروج"
		}
		if key == "time.clock.on_break" {
			return "في استراحة"
		}
		if key == "time.last_event" {
			return "لا يوجد حدث"
		}
	}
	return "[missing translation: " + key + "] " + fallback
}
