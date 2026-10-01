package scheduled

import (
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MarshalJSON excludes unset values in inactive source branches. Kernel time
// types deliberately refuse serializing their unset zero values.
func (s Schedule) MarshalJSON() ([]byte, error) {
	type alias Schedule
	d := s.Trigger.Definition
	source := map[string]any{"Kind": d.Source.Kind}
	switch d.Source.Kind {
	case schedule.SourceCron:
		source["Cron"] = d.Source.Cron
	case schedule.SourceCalendar:
		c := d.Source.Calendar
		fields := map[string]any{}
		if c.CalendarRef.Ref != "" {
			fields["CalendarRef"] = c.CalendarRef
		}
		if c.TenantCalendarRef.Ref != "" {
			fields["TenantCalendarRef"] = c.TenantCalendarRef
		}
		if c.Cutoff.PhaseID != "" {
			fields["Cutoff"] = c.Cutoff
		}
		if c.CutoffRule.PhaseID != "" {
			fields["CutoffRule"] = c.CutoffRule
		}
		source["Calendar"] = fields
	default:
		return nil, ErrInvalidSchedule
	}
	definition := map[string]any{"ID": d.ID, "Version": d.Version, "TenantID": d.TenantID, "Target": d.Target, "TargetKind": d.TargetKind, "AgentRun": d.AgentRun, "InputTemplateDigest": d.InputTemplateDigest, "Purpose": d.Purpose, "Owner": d.Owner, "Source": source, "Overlap": d.Overlap, "OverlapPolicy": d.OverlapPolicy, "Storm": d.Storm, "StormPolicy": d.StormPolicy, "ExecutionMode": d.ExecutionMode, "ExecutionEnvironment": d.ExecutionEnvironment}
	trigger := struct {
		Definition     map[string]any
		Digest         string
		CanonicalBytes []byte
	}{definition, s.Trigger.Digest, s.Trigger.CanonicalBytes}
	var cursor *string
	if s.Cursor.IsSet() {
		value := s.Cursor.String()
		cursor = &value
	}
	return json.Marshal(struct {
		alias
		Trigger any
		Cursor  *string
	}{alias(s), trigger, cursor})
}
func (s *Schedule) UnmarshalJSON(data []byte) error {
	type alias Schedule
	var wire struct {
		alias
		Trigger schedule.PublishedTrigger
		Cursor  *string
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*s = Schedule(wire.alias)
	s.Trigger = wire.Trigger
	if wire.Cursor != nil {
		if err := s.Cursor.UnmarshalText([]byte(*wire.Cursor)); err != nil {
			return err
		}
	}
	return nil
}

// ZonedDateTime retains unexported DST evidence, so deliveries encode its
// civil components and verify their resolution against the persisted instant.
func (d Delivery) MarshalJSON() ([]byte, error) {
	type alias Delivery
	occ := d.Firing.Occurrence
	z := occ.ScheduledAt
	if err := z.Validate(); err != nil {
		return nil, err
	}
	wire := struct {
		Trigger        schedule.TriggerRef
		Key            string
		Date           values.LocalDate
		Time           values.LocalTime
		Zone           values.ZoneRef
		Offset         int
		Disambiguation values.Disambiguation
		At             values.Instant
		NominalDate    values.LocalDate
		Source         schedule.SourceKind
		Misfire        schedule.MisfireDecision
	}{occ.Trigger, occ.Key, z.Date(), z.TimeOfDay(), z.Zone(), z.OffsetSeconds(), z.Disambiguation(), occ.At, occ.NominalDate, occ.Source, occ.Misfire}
	return json.Marshal(struct {
		alias
		Firing any
	}{alias(d), struct {
		Occurrence any
		Target     schedule.AgentRunTarget
	}{wire, d.Firing.Target}})
}
func (d *Delivery) UnmarshalJSON(data []byte) error {
	type alias Delivery
	var wire struct {
		alias
		Firing struct {
			Occurrence struct {
				Trigger        schedule.TriggerRef
				Key            string
				Date           values.LocalDate
				Time           values.LocalTime
				Zone           values.ZoneRef
				Offset         int
				Disambiguation values.Disambiguation
				At             values.Instant
				NominalDate    values.LocalDate
				Source         schedule.SourceKind
				Misfire        schedule.MisfireDecision
			}
			Target schedule.AgentRunTarget
		}
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*d = Delivery(wire.alias)
	occ := wire.Firing.Occurrence
	var z values.ZonedDateTime
	var err error
	if occ.Disambiguation == values.DisambiguationExplicitOffset {
		z, err = values.NewZonedDateTimeAtOffset(occ.Date, occ.Time, occ.Zone, occ.Offset)
	} else {
		z, err = values.NewZonedDateTime(occ.Date, occ.Time, occ.Zone, occ.Disambiguation)
	}
	if err != nil {
		return err
	}
	if z.Instant().Compare(occ.At) != 0 || z.OffsetSeconds() != occ.Offset {
		return fmt.Errorf("%w: occurrence instant changed", ErrInvalidFiring)
	}
	d.Firing = Firing{schedule.Occurrence{Trigger: occ.Trigger, Key: occ.Key, ScheduledAt: z, At: occ.At, NominalDate: occ.NominalDate, Source: occ.Source, Misfire: occ.Misfire}, wire.Firing.Target}
	return nil
}
