// Package timeclockstore contains production adapters from the clock
// application ports to the durable timestore.
package timeclockstore

import (
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"time"
)

// Ports is the production clock persistence bundle. The composition root can
// assign these values directly to clockservice.Service without a memory store.
type Ports struct {
	Sessions     clockservice.SessionStore
	Observations clockservice.ObservationStore
	Receipts     clockservice.ReceiptStore
	Devices      clockservice.DeviceStore
	Enrollments  clockservice.EnrollmentStore
	Credentials  clockservice.CredentialStore
	Heartbeats   clockservice.HeartbeatStore
	Retention    RetentionAdapter
}

// NewPorts returns adapters backed by store. A nil store is rejected so a
// production composition cannot silently bind a partial adapter bundle.
func NewPorts(store *timestore.Store) (Ports, error) {
	return NewPortsWithRegistry(store, nil, nil)
}

// NewPortsWithRegistry returns production ports with proof-bound enrollment
// enabled by the caller's authenticated registry source.
func NewPortsWithRegistry(store *timestore.Store, registry VerifiedRegistrySource, now func() time.Time) (Ports, error) {
	if store == nil {
		return Ports{}, ErrNilStore
	}
	retention, err := NewRetentionAdapter(clockservice.DefaultTimeRecordRetentionPolicy())
	if err != nil {
		return Ports{}, err
	}
	return Ports{
		Sessions:     SessionAdapter{Store: store},
		Observations: ObservationAdapter{Store: store},
		Receipts:     ReceiptAdapter{Store: store},
		Devices:      DeviceAdapter{Store: store},
		Enrollments:  EnrollmentAdapter{Store: store, Registry: registry, Clock: now},
		Credentials:  CredentialAdapter{Store: store},
		Heartbeats:   HeartbeatAdapter{Store: store},
		Retention:    retention,
	}, nil
}
