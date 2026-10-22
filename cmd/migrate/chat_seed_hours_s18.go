package main

import (
	"strings"
	"time"
	// The seeder runs on machines with no zone database; the office zone must
	// resolve there too, or the history would be left at its raw hours.
	_ "time/tzdata"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
)

const (
	chatSeedWorkdayStart = 8 * time.Hour
	chatSeedWorkdayEnd   = 18 * time.Hour
)

// chatSeedOfficeZone is the zone the demo company's office keeps: Denver for
// Ironridge, Boston (US Eastern) for every other demo tenant.
func chatSeedOfficeZone(tenant string) *time.Location {
	name, offset := "America/New_York", -5*3600
	if strings.TrimSpace(tenant) == demoworkforce.IronridgeKey {
		name, offset = "America/Denver", -7*3600
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.FixedZone(name, offset)
}

// chatSeedReplyTime carries a moment that would land outside office hours into
// them: after closing it goes to the next morning, keeping the overflow, so a
// long exchange never runs into the night, and a weekend goes to Monday. The
// chat history itself is placed on a chatSeedTimeline; this is for the
// moderation story, which lays single moments after one another.
func chatSeedReplyTime(at time.Time, office *time.Location) time.Time {
	local := at.In(office)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office)
	sinceMidnight := local.Sub(midnight)
	switch {
	case sinceMidnight < chatSeedWorkdayStart:
		local = midnight.Add(chatSeedWorkdayStart + sinceMidnight/8)
	case sinceMidnight >= chatSeedWorkdayEnd:
		next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, office)
		local = next.Add(chatSeedWorkdayStart + (sinceMidnight - chatSeedWorkdayEnd))
	}
	for chatSeedIsWeekend(local, office) {
		today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office)
		tomorrow := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, office)
		local = tomorrow.Add(local.Sub(today))
	}
	return local.UTC()
}
