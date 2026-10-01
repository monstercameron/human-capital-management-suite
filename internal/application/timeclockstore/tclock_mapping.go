package timeclockstore

import (
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

func sessionRow(r clockservice.SessionRecord) timestore.SessionRow {
	return timestore.SessionRow{ID: r.ID, TenantID: r.TenantID, WorkerRef: r.WorkerRef, AssignmentRef: r.AssignmentRef, Status: r.Status, Source: r.Source, ProjectRef: r.ProjectRef, Revision: int64(r.Revision), OpenedAt: r.OpenedAt, ClosedAt: r.ClosedAt, Payload: json.RawMessage(append([]byte(nil), r.Payload...))}
}

func sessionRecord(r timestore.SessionRow) clockservice.SessionRecord {
	return clockservice.SessionRecord{ID: r.ID, TenantID: r.TenantID, WorkerRef: r.WorkerRef, AssignmentRef: r.AssignmentRef, Status: r.Status, Source: r.Source, ProjectRef: r.ProjectRef, Revision: uint64(r.Revision), OpenedAt: r.OpenedAt, ClosedAt: r.ClosedAt, Payload: append([]byte(nil), r.Payload...)}
}

func eventRow(sessionID string, e clockservice.SessionEvent) timestore.EventRow {
	return timestore.EventRow{SessionID: sessionID, Kind: e.Kind, ActorRef: e.ActorRef, IdempotencyKey: e.IdempotencyKey, Digest: e.Digest, Payload: json.RawMessage(append([]byte(nil), e.Payload...))}
}

func deviceRecord(d timestore.Device) clockservice.DeviceRecord {
	return clockservice.DeviceRecord{TenantID: d.TenantID, ID: d.ID, PublicKey: append([]byte(nil), d.PublicKey...), SiteID: d.SiteID, ProfileID: d.ProfileID, Timezone: d.Timezone, State: d.State, Revision: d.Revision}
}

func credentialRecord(c timestore.Credential) clockservice.CredentialRecord {
	return clockservice.CredentialRecord{ID: c.ID, WorkerID: c.WorkerID, Kind: c.Kind, ExternalID: c.ExternalID, State: c.State, Revision: c.Revision}
}
