package demoworkforce

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"time"
)

// DemoBirthday assigns fictional month/day data from the immutable worker key.
// The workforce seed has no birth dates. No birth year is generated or retained.
func DemoBirthday(workerKey string) (time.Month, int, bool) {
	if strings.TrimSpace(workerKey) == "" || workerKey != strings.TrimSpace(workerKey) {
		return 0, 0, false
	}
	sum := sha256.Sum256([]byte("hcmnext.demo.birthday:" + workerKey))
	// A leap-year calendar permits Feb 29 without creating an age-bearing date.
	ordinal := int(binary.BigEndian.Uint16(sum[:2])%366) + 1
	date := time.Date(2000, time.January, ordinal, 0, 0, 0, 0, time.UTC)
	return date.Month(), date.Day(), true
}

// DemoBirthdayToday applies the declared Feb 28 rule in non-leap years.
func DemoBirthdayToday(month time.Month, day int, today time.Time) bool {
	if month < time.January || month > time.December || day < 1 || day > 31 {
		return false
	}
	valid := time.Date(2000, month, day, 0, 0, 0, 0, time.UTC)
	if valid.Month() != month || valid.Day() != day {
		return false
	}
	if month == time.February && day == 29 && time.Date(today.Year(), time.March, 0, 0, 0, 0, 0, today.Location()).Day() == 28 {
		day = 28
	}
	return today.Month() == month && today.Day() == day
}
