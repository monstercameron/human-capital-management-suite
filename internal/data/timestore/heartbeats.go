package timestore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Heartbeat is one device's fleet-health report: app version, buffered
// queue depth and age, power state and measured offset against server
// time.
type Heartbeat struct {
	TenantID, DeviceID, AppVersion, PowerState string
	QueueDepth, OldestUnsentAgeSeconds         int
	BatteryPercent                             *int
	OffsetMillis                               int64
	ObservedAt                                 time.Time
}

// RecordHeartbeat upserts the device's latest-state row and appends one
// history entry, in a single round trip: one INSERT for the latest row
// (ON CONFLICT DO UPDATE) and one INSERT into the history table, both in
// the same transaction so a heartbeat can never update one and miss the
// other.
func (s *Store) RecordHeartbeat(ctx context.Context, tenant string, hb Heartbeat) error {
	if tenant == "" || hb.DeviceID == "" || hb.AppVersion == "" || hb.ObservedAt.IsZero() {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO time_device_heartbeat_latest(tenant_id,device_id,last_seen,app_version,queue_depth,oldest_unsent_age_seconds,battery_percent,power_state,offset_millis,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
			ON CONFLICT (tenant_id,device_id) DO UPDATE SET last_seen=EXCLUDED.last_seen,app_version=EXCLUDED.app_version,queue_depth=EXCLUDED.queue_depth,
				oldest_unsent_age_seconds=EXCLUDED.oldest_unsent_age_seconds,battery_percent=EXCLUDED.battery_percent,power_state=EXCLUDED.power_state,offset_millis=EXCLUDED.offset_millis,updated_at=now()
			WHERE time_device_heartbeat_latest.last_seen<=EXCLUDED.last_seen`,
			tenant, hb.DeviceID, hb.ObservedAt, hb.AppVersion, hb.QueueDepth, hb.OldestUnsentAgeSeconds, hb.BatteryPercent, hb.PowerState, hb.OffsetMillis); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO time_device_heartbeat_history(tenant_id,id,device_id,observed_at,app_version,queue_depth,oldest_unsent_age_seconds,battery_percent,power_state,offset_millis)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			tenant, uuid.NewString(), hb.DeviceID, hb.ObservedAt, hb.AppVersion, hb.QueueDepth, hb.OldestUnsentAgeSeconds, hb.BatteryPercent, hb.PowerState, hb.OffsetMillis)
		return err
	})
}

// LatestHeartbeat returns a device's current latest-state row.
func (s *Store) LatestHeartbeat(ctx context.Context, tenant, deviceID string) (Heartbeat, error) {
	if tenant == "" || deviceID == "" {
		return Heartbeat{}, ErrInvalid
	}
	var out Heartbeat
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT tenant_id,device_id,last_seen,app_version,queue_depth,oldest_unsent_age_seconds,battery_percent,power_state,offset_millis FROM time_device_heartbeat_latest WHERE tenant_id=$1 AND device_id=$2`, tenant, deviceID).
			Scan(&out.TenantID, &out.DeviceID, &out.ObservedAt, &out.AppVersion, &out.QueueDepth, &out.OldestUnsentAgeSeconds, &out.BatteryPercent, &out.PowerState, &out.OffsetMillis)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return out, err
}

// HeartbeatHistory returns a device's bounded heartbeat history, most
// recent first, up to limit entries.
func (s *Store) HeartbeatHistory(ctx context.Context, tenant, deviceID string, limit int) ([]Heartbeat, error) {
	if tenant == "" || deviceID == "" || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]Heartbeat, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,device_id,observed_at,app_version,queue_depth,oldest_unsent_age_seconds,battery_percent,power_state,offset_millis FROM time_device_heartbeat_history WHERE tenant_id=$1 AND device_id=$2 ORDER BY observed_at DESC LIMIT $3`, tenant, deviceID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h Heartbeat
			if err := rows.Scan(&h.TenantID, &h.DeviceID, &h.ObservedAt, &h.AppVersion, &h.QueueDepth, &h.OldestUnsentAgeSeconds, &h.BatteryPercent, &h.PowerState, &h.OffsetMillis); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// DeviceStatus is one row of the admin fleet view: a device joined with
// its latest heartbeat (if any has ever arrived) and a derived online/
// offline/never-seen status.
type DeviceStatus struct {
	Device      Device
	Heartbeat   *Heartbeat
	FleetStatus string
}

// Fleet statuses derived from a device's state and last heartbeat.
const (
	FleetStatusOnline    = "ONLINE"
	FleetStatusOffline   = "OFFLINE"
	FleetStatusNeverSeen = "NEVER_SEEN"
	FleetStatusSuspended = "SUSPENDED"
	FleetStatusRevoked   = "REVOKED"
)

// DevicesByStatus lists a site's devices with their derived fleet status as
// of asOf, for the admin device-fleet view. A device is ONLINE when its
// last heartbeat is within offlineAfter of asOf, OFFLINE when it has a
// heartbeat older than that, and NEVER_SEEN when no heartbeat has arrived;
// a SUSPENDED or REVOKED device reports that state regardless of its
// heartbeat recency. Passing "" for status returns every device at the
// site.
func (s *Store) DevicesByStatus(ctx context.Context, tenant, siteID, status string, asOf time.Time, offlineAfter time.Duration, limit int) ([]DeviceStatus, error) {
	if tenant == "" || siteID == "" || asOf.IsZero() || offlineAfter <= 0 || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]DeviceStatus, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+prefixColumns("d", deviceColumns)+`,
				h.device_id,h.last_seen,h.app_version,h.queue_depth,h.oldest_unsent_age_seconds,h.battery_percent,h.power_state,h.offset_millis
			FROM time_device d
			LEFT JOIN time_device_heartbeat_latest h ON h.tenant_id=d.tenant_id AND h.device_id=d.id
			WHERE d.tenant_id=$1 AND d.site_id=$2
			ORDER BY d.id LIMIT $3`, tenant, siteID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Device
			var hbDeviceID, appVersion, powerState *string
			var lastSeen *time.Time
			var queueDepth, oldestUnsentAge *int
			var battery *int
			var offsetMillis *int64
			if err := rows.Scan(&d.TenantID, &d.ID, &d.PublicKey, &d.SiteID, &d.ProfileID, &d.Timezone, &d.State, &d.Revision, &d.CreatedAt, &d.UpdatedAt,
				&hbDeviceID, &lastSeen, &appVersion, &queueDepth, &oldestUnsentAge, &battery, &powerState, &offsetMillis); err != nil {
				return err
			}
			ds := DeviceStatus{Device: d}
			if hbDeviceID != nil {
				ds.Heartbeat = &Heartbeat{TenantID: tenant, DeviceID: *hbDeviceID, ObservedAt: *lastSeen, AppVersion: *appVersion, QueueDepth: *queueDepth, OldestUnsentAgeSeconds: *oldestUnsentAge, BatteryPercent: battery, PowerState: *powerState, OffsetMillis: *offsetMillis}
			}
			ds.FleetStatus = deriveFleetStatus(d, ds.Heartbeat, asOf, offlineAfter)
			if status == "" || ds.FleetStatus == status {
				out = append(out, ds)
			}
		}
		return rows.Err()
	})
	return out, err
}

func deriveFleetStatus(d Device, hb *Heartbeat, asOf time.Time, offlineAfter time.Duration) string {
	switch d.State {
	case DeviceStateSuspended:
		return FleetStatusSuspended
	case DeviceStateRevoked:
		return FleetStatusRevoked
	}
	if hb == nil {
		return FleetStatusNeverSeen
	}
	if asOf.Sub(hb.ObservedAt) > offlineAfter {
		return FleetStatusOffline
	}
	return FleetStatusOnline
}

// prefixColumns qualifies a comma-separated column list with a table alias,
// so a single column-list constant can be reused in a join without
// spelling every "alias.column" pairing out twice.
func prefixColumns(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + "." + p
	}
	return strings.Join(parts, ",")
}
