package chatui

import (
	"strings"
	"testing"
)

// CHAT-3701: quiet hours with the same From and Until time read as a zero-length
// or a 24-hour window, and the summary said "Paused 9:00 AM–9:00 AM". The
// recipient rule (ShouldNotify) treats equal times as paused all day, so the
// page must say that in words rather than repeat the two identical clocks.
func TestQuietHoursEqualTimesSayPausedAllDay3701(t *testing.T) {
	m := Model{State: StateReady, Locale: "en-US", Preferences: Preferences{QuietHours: true, QuietTimezone: "UTC", QuietStartMinute: 9 * 60, QuietEndMinute: 9 * 60}, Callbacks: Callbacks{SavePreferences: func(Preferences) {}}}
	markup := renderNode(t, railPreferences(m, handlers{}))
	if strings.Contains(markup, "Paused 9:00 AM–9:00 AM") || strings.Contains(markup, "9:00 AM–9:00 AM") {
		t.Fatalf("equal quiet-hours times still render as an ambiguous range: %s", markup)
	}
	if !strings.Contains(markup, "All day") {
		t.Fatalf("equal quiet-hours times do not state that they pause all day: %s", markup)
	}
}

// The same window with distinct times keeps its range wording.
func TestQuietHoursDistinctTimesKeepRange3701(t *testing.T) {
	m := Model{State: StateReady, Locale: "en-US", Preferences: Preferences{QuietHours: true, QuietTimezone: "UTC", QuietStartMinute: 9 * 60, QuietEndMinute: 17 * 60}, Callbacks: Callbacks{SavePreferences: func(Preferences) {}}}
	markup := renderNode(t, railPreferences(m, handlers{}))
	if !strings.Contains(markup, "Paused 9:00 AM–5:00 PM") {
		t.Fatalf("distinct quiet-hours times lost their range: %s", markup)
	}
}

// The all-day wording is translated, and the row value and moon tip use it too.
func TestQuietHoursAllDayLocales3701(t *testing.T) {
	for _, loc := range []string{"en-US", "de-DE", "ar"} {
		m := Model{State: StateReady, Locale: loc, Preferences: Preferences{QuietHours: true, QuietTimezone: "UTC", QuietStartMinute: 540, QuietEndMinute: 540}}
		for _, key := range []string{keyQuiet3701Summary, keyQuiet3701Value, keyQuiet3701Tip} {
			if got := quiet3701Text(m, key); got == "" || got == key {
				t.Fatalf("%s %s: untranslated %q", loc, key, got)
			}
		}
		if got := chatux002QuietValue(m); got != quiet3701Text(m, keyQuiet3701Value) {
			t.Fatalf("%s: row value %q", loc, got)
		}
	}
}
