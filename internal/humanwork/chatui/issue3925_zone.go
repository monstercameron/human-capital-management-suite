package chatui

import (
	"strconv"
	"time"
)

// A closing time is shown with the offset of the zone it is written in
// ("2026-11-12 23:59 UTC+1") and says whose zone that is: the viewer's own, or,
// when the page has no valid zone to use, the device's (#3925). Every viewer
// reads the same instant in their own zone, so a colleague elsewhere sees a
// different clock time for the same deadline and is told it is theirs.

// chatcmd002Offset is the offset of at's zone from UTC in words a reader can
// compare: "UTC", "UTC+1", "UTC-5", "UTC+5:30".
func chatcmd002Offset(at time.Time) string {
	_, seconds := at.Zone()
	if seconds == 0 {
		return "UTC"
	}
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	label := "UTC" + sign + strconv.Itoa(seconds/3600)
	if minutes := seconds % 3600 / 60; minutes != 0 {
		label += ":" + twoDigits(minutes)
	}
	return label
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// chatcmd002ZoneValid reports whether the page was given a zone it can load.
func chatcmd002ZoneValid(m Model) bool {
	if m.Chatcmd002TimeZone == "" {
		return false
	}
	_, err := time.LoadLocation(m.Chatcmd002TimeZone)
	return err == nil
}

// chatcmd002Deadline is a closing time with its zone and whose zone it is.
func chatcmd002Deadline(m Model, at time.Time) string {
	key := "zone-yours"
	if !chatcmd002ZoneValid(m) {
		key = "zone-device"
	}
	return chatcmd002Fill(chatcmd003Text(m, key), "date", chatcmd002Stamp(m, at))
}
