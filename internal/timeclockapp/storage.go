package timeclockapp

import (
	"encoding/json"
	"errors"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"strconv"
)

// Storage is the device's durable key/value store: localStorage in the
// browser, a map in tests. It holds exactly three keys (StorageKeys); worker
// display names, statuses and PINs are never written to it.
type Storage interface {
	Load(key string) (string, bool)
	Save(key, value string) error
}

// The persisted keys.
const (
	keyDevice   = "hcm.timeclock.device"
	keySequence = "hcm.timeclock.sequence"
	keyQueue    = "hcm.timeclock.queue"
)

// StorageKeys lists every key the kiosk ever writes.
func StorageKeys() []string { return []string{keyDevice, keySequence, keyQueue} }

// ErrStorage wraps a failure to persist device state.
var ErrStorage = errors.New("timeclockapp: device storage failed")

// DeviceRecord is the enrolled identity kept on the device. KeySeed is the
// Ed25519 private key seed; it is written once at enrollment and never
// rendered.
type DeviceRecord struct {
	DeviceID      string                  `json:"device_id"`
	SiteID        string                  `json:"site_id"`
	Timezone      string                  `json:"timezone"`
	CredentialRef string                  `json:"credential_ref,omitempty"`
	KeySeed       []byte                  `json:"key_seed"`
	State         timev1.ClockDeviceState `json:"state"`
}

// persisted is the device state the App restores on boot.
type persisted struct {
	device  *DeviceRecord
	lastSeq uint64
	queue   Queue
	// corrupt reports that a stored value could not be read back; the
	// unreadable value is dropped rather than trusted.
	corrupt bool
}

func loadState(s Storage) persisted {
	var p persisted
	if raw, ok := s.Load(keyDevice); ok && raw != "" {
		var rec DeviceRecord
		if err := json.Unmarshal([]byte(raw), &rec); err == nil && rec.DeviceID != "" {
			p.device = &rec
		} else {
			p.corrupt = true
		}
	}
	if raw, ok := s.Load(keySequence); ok && raw != "" {
		if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			p.lastSeq = n
		} else {
			p.corrupt = true
		}
	}
	if raw, ok := s.Load(keyQueue); ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &p.queue); err != nil {
			p.corrupt = true
			p.queue = nil
		}
	}
	p.queue = p.queue.Sorted()
	// The sequence must never go backwards, even if the counter write was
	// lost after the queue write.
	if last := p.queue.LastSequence(); last > p.lastSeq {
		p.lastSeq = last
	}
	return p
}

func saveDevice(s Storage, rec DeviceRecord) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return errors.Join(ErrStorage, err)
	}
	if err := s.Save(keyDevice, string(raw)); err != nil {
		return errors.Join(ErrStorage, err)
	}
	return nil
}

// saveQueue writes the queue first and the counter second: loadState
// recovers the counter from the queue if the second write is lost.
func saveQueue(s Storage, q Queue, lastSeq uint64) error {
	raw, err := json.Marshal(q)
	if err != nil {
		return errors.Join(ErrStorage, err)
	}
	if err := s.Save(keyQueue, string(raw)); err != nil {
		return errors.Join(ErrStorage, err)
	}
	if err := s.Save(keySequence, strconv.FormatUint(lastSeq, 10)); err != nil {
		return errors.Join(ErrStorage, err)
	}
	return nil
}
