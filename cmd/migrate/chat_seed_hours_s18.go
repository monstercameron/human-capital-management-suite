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

// chatSeedWorkday lays a moment of the seeded timeline onto office hours. The
// seeder schedules posts evenly across fourteen whole days, which put messages
// at 2:46 AM and 11:04 PM for an office that is closed then. Each local day is
// squeezed into 08:00 to 18:00 in proportion, so the mapping never reorders two
// moments: the day's earlier posts stay earlier, and the next day starts after
// the last of the one before.
func chatSeedWorkday(at time.Time, office *time.Location) time.Time {
	local := at.In(office)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office)
	fraction := float64(local.Sub(midnight)) / float64(24*time.Hour)
	span := chatSeedWorkdayEnd - chatSeedWorkdayStart
	return midnight.Add(chatSeedWorkdayStart + time.Duration(fraction*float64(span))).UTC()
}

// chatSeedReplyTime carries a reply that would land after closing to the next
// morning, keeping the overflow, so a long thread never runs into the night.
func chatSeedReplyTime(at time.Time, office *time.Location) time.Time {
	local := at.In(office)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, office)
	sinceMidnight := local.Sub(midnight)
	switch {
	case sinceMidnight < chatSeedWorkdayStart:
		return midnight.Add(chatSeedWorkdayStart + sinceMidnight/8).UTC()
	case sinceMidnight >= chatSeedWorkdayEnd:
		next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, office)
		return next.Add(chatSeedWorkdayStart + (sinceMidnight - chatSeedWorkdayEnd)).UTC()
	}
	return at
}
