package timeclockstore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
)

type enrollmentCoreRegistry struct {
	clients           map[string]partnerapp.MachineClient
	keys              map[string][]partnerapp.MachineClientKey
	registerClientErr error
	registerKeyErr    error
}

func (r *enrollmentCoreRegistry) RegisterClient(_ context.Context, tenant string, client partnerapp.MachineClient) error {
	if r.registerClientErr != nil {
		return r.registerClientErr
	}
	if r.clients == nil {
		r.clients = map[string]partnerapp.MachineClient{}
	}
	r.clients[tenant+"/"+client.ClientID] = client
	return nil
}
func (r *enrollmentCoreRegistry) RegisterClientKey(_ context.Context, tenant string, key partnerapp.MachineClientKey) error {
	if r.registerKeyErr != nil {
		return r.registerKeyErr
	}
	if r.keys == nil {
		r.keys = map[string][]partnerapp.MachineClientKey{}
	}
	r.keys[tenant+"/"+key.ClientID] = append(r.keys[tenant+"/"+key.ClientID], key)
	return nil
}
func (r *enrollmentCoreRegistry) LoadClient(_ context.Context, tenant, id string) (partnerapp.MachineClient, error) {
	v, ok := r.clients[tenant+"/"+id]
	if !ok {
		return partnerapp.MachineClient{}, errors.New("not found")
	}
	return v, nil
}
func (r *enrollmentCoreRegistry) LoadClientKeys(_ context.Context, tenant, id string) ([]partnerapp.MachineClientKey, error) {
	v, ok := r.keys[tenant+"/"+id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}
func (*enrollmentCoreRegistry) RecordClientUse(context.Context, string, string, time.Time) error {
	return nil
}

func TestTodo_FTIME_003_MachineEnrollmentPersistsAndActivatesAgainstCoreIdentity(t *testing.T) {
	store, tenant := adapterFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second).Add(30 * time.Minute)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{Class: clock.SourceManagedKiosk, Transport: "https", Authentication: "ed25519", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "test", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	profiles := staticRegistry{registry: clock.VerifiedProfileRegistry{Registry: registry, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}}
	if err := store.CreateEnrollmentCode(ctx, tenant, "machine-enrollment-code", "site-1", string(clock.SourceManagedKiosk), "UTC", "admin", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	proof := clock.KeyPossessionProof{PublicKey: pub, Challenge: []byte("machine-enrollment-code"), Signature: ed25519.Sign(priv, []byte("machine-enrollment-code"))}
	input := clockservice.MachineClientEnrollment{Tenant: tenant, EnrollmentCode: "machine-enrollment-code", DeviceID: "machine-device", MachineClientID: "machine-client", Owner: "owner-1", SiteID: "site-1", ProfileID: string(clock.SourceManagedKiosk), Timezone: "UTC", PublicKey: pub, KeyID: "key-1", Proof: proof, Scopes: []string{"clock:punch"}, Purpose: "clock:kiosk", ExpiresAt: now.Add(45 * time.Minute), IdempotencyKey: "machine-enroll-1", Now: now}
	core := &enrollmentCoreRegistry{}
	adapter := MachineEnrollmentAdapter{Store: store, Registry: core, Profiles: profiles, Clock: func() time.Time { return now }}
	loaded, profile, err := adapter.LoadEnrollment(ctx, tenant, input.EnrollmentCode)
	_, profileFound := profile.Profile(clock.SourceManagedKiosk)
	if err != nil || loaded.Profile != clock.SourceManagedKiosk || !profileFound {
		t.Fatalf("load=%+v profile=%+v err=%v", loaded, profile, err)
	}
	pending, err := adapter.PrepareMachineEnrollment(ctx, input)
	if err != nil || pending.Device.State != "PENDING" {
		t.Fatalf("prepare=%+v err=%v", pending, err)
	}
	recovered, found, err := adapter.PendingMachineEnrollmentFor(ctx, tenant, input.DeviceID)
	if err != nil || !found || recovered.Enrollment.MachineClientID != input.MachineClientID || string(recovered.Enrollment.Proof.Signature) != string(input.Proof.Signature) {
		t.Fatalf("recovered=%+v found=%v err=%v", recovered, found, err)
	}
	if _, found, err := adapter.PendingMachineEnrollmentFor(ctx, tenant, "missing-device"); err != nil || found {
		t.Fatalf("missing pending found=%v err=%v", found, err)
	}
	credential, err := adapter.EnsureMachineClient(ctx, recovered)
	if err != nil || credential != input.MachineClientID {
		t.Fatalf("credential=%q err=%v", credential, err)
	}
	active, err := adapter.ActivateMachineEnrollment(ctx, tenant, input.DeviceID, credential)
	if err != nil || active.State != "ACTIVE" || active.ProfileID != input.ProfileID {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	replay, err := adapter.ActivateMachineEnrollment(ctx, tenant, input.DeviceID, credential)
	if err != nil || replay.ID != active.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := adapter.ActivateMachineEnrollment(ctx, tenant, input.DeviceID, "other-client"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("mismatched credential error=%v", err)
	}
	if _, found, err := adapter.PendingMachineEnrollmentFor(ctx, "other-tenant", input.DeviceID); err != nil || found {
		t.Fatalf("cross-tenant pending found=%v err=%v", found, err)
	}
}

func TestTodo_FTIME_003_MachineEnrollmentFailClosedOnRegistryMismatch(t *testing.T) {
	ctx := context.Background()
	in := clockservice.MachineClientEnrollment{Tenant: "t", MachineClientID: "c", Owner: "o", Scopes: []string{"clock:punch"}, Purpose: "clock:kiosk", ExpiresAt: time.Unix(20, 0), Now: time.Unix(10, 0), PublicKey: make([]byte, ed25519.PublicKeySize), KeyID: "key"}
	pending := clockservice.MachineBootstrapPending{Enrollment: in}
	for _, tc := range []struct {
		name     string
		registry *enrollmentCoreRegistry
	}{
		{"client registration failure", &enrollmentCoreRegistry{registerClientErr: errors.New("write failed"), clients: map[string]partnerapp.MachineClient{}}},
		{"key registration failure", &enrollmentCoreRegistry{registerKeyErr: errors.New("write failed"), clients: map[string]partnerapp.MachineClient{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (MachineEnrollmentAdapter{Registry: tc.registry}).EnsureMachineClient(ctx, pending); err == nil {
				t.Fatal("registry failure accepted")
			}
		})
	}
	if _, err := (MachineEnrollmentAdapter{}).EnsureMachineClient(ctx, pending); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing registry error=%v", err)
	}
	if _, err := (MachineEnrollmentAdapter{}).PrepareMachineEnrollment(ctx, in); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing store error=%v", err)
	}
	if _, found, err := (MachineEnrollmentAdapter{}).PendingMachineEnrollmentFor(ctx, "t", "d"); found || !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing store found=%v err=%v", found, err)
	}
	if _, _, err := (MachineEnrollmentAdapter{}).LoadEnrollment(ctx, "t", "c"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing deps error=%v", err)
	}
	if _, err := (MachineEnrollmentAdapter{}).ActivateMachineEnrollment(ctx, "t", "d", "c"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("missing deps error=%v", err)
	}
}
