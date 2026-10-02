package chatui

import (
	"strings"
	"time"
)

// quietClock writes a quiet-hours boundary the way the locale's time inputs
// show it: 12-hour for US English, 24-hour elsewhere, so the summary and the
// fields never disagree ("10:00 PM" beside "22:00").
func quietClock(m Model, minute int) string {
	if !strings.HasPrefix(m.Locale, "en-US") && m.Locale != "" {
		return minuteClock(minute)
	}
	return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(parseClock(minuteClock(minute))) * time.Minute).Format("3:04 PM")
}

// commonTimeZones seeds the quiet-hours zone list after the saved zone, the
// device's zone and UTC; IANA names are what the preference stores.
var commonTimeZones = []string{
	"America/Los_Angeles", "America/Denver", "America/Chicago", "America/New_York",
	"America/Sao_Paulo", "Europe/London", "Europe/Berlin", "Europe/Paris",
	"Africa/Johannesburg", "Asia/Dubai", "Asia/Kolkata", "Asia/Singapore",
	"Asia/Tokyo", "Australia/Sydney",
}

// quietZone is the zone quiet hours run in: the one the person saved, else the
// device's own, else UTC when the device does not say. Quiet hours used to start
// on UTC, a zone nobody in the conversation lives in (CHATUX-020).
func quietZone(saved, device string) string {
	if zone := strings.TrimSpace(saved); zone != "" {
		return zone
	}
	if zone := strings.TrimSpace(device); zone != "" {
		return zone
	}
	return "UTC"
}
