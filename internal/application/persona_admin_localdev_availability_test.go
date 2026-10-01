package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type localDevAvailabilityClient struct {
	productui.PersonaAdminClient
	snapshot productui.PersonaAdminSnapshot
	err      error
}

func (c localDevAvailabilityClient) Snapshot(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return c.snapshot, c.err
}

type localDevAvailabilityFactory struct{ client productui.PersonaAdminClient }

func (f localDevAvailabilityFactory) ClientForRequest(context.Context) productui.PersonaAdminClient {
	return f.client
}

func TestTodo_AGENTP_018_LocalBootstrapAvailabilityPreservesAdmission(t *testing.T) {
	ctx := context.Background()
	if withLocalDevPersonaBootstrapAvailability(nil, true) != nil {
		t.Fatal("nil factory became available")
	}
	if withLocalDevPersonaBootstrapAvailability(localDevAvailabilityFactory{}, true).ClientForRequest(ctx) != nil {
		t.Fatal("denied client became available")
	}
	denied := errors.New("denied")
	for _, tc := range []struct {
		name            string
		local, readable bool
		err             error
		want            bool
	}{
		{"production", false, true, nil, false}, {"local authorized", true, true, nil, true}, {"unavailable", true, false, nil, false}, {"denied", true, false, denied, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := localDevAvailabilityClient{snapshot: productui.PersonaAdminSnapshot{Available: tc.readable}, err: tc.err}
			client := withLocalDevPersonaBootstrapAvailability(localDevAvailabilityFactory{inner}, tc.local).ClientForRequest(ctx)
			got, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
			if !errors.Is(err, tc.err) || got.LocalDevBootstrapAvailable != tc.want {
				t.Fatalf("snapshot=%+v err=%v", got, err)
			}
		})
	}
}
