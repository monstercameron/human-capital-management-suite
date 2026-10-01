package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestPersonaInstallationConversationLimitRace(t *testing.T) {
	f := newFixture(t, "installation-limit")
	s := f.store(t, "installation-limit")
	ctx := context.Background()
	personas := make([]PersonaVersion, 6)
	for i := range personas {
		personas[i] = createCatalogPersona(t, s, "installation-limit", fmt.Sprintf("persona-%d", i), StatePublished)
	}
	installation := func(i int) PersonaInstallation {
		return PersonaInstallation{TenantID: "installation-limit", InstallationID: fmt.Sprintf("install-%d", i), PersonaID: personas[i].PersonaID,
			PersonaVersion: 1, ConversationID: "shared-conversation", ConversationClass: ConversationPrivate,
			InstallerID: "user:manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}
	}
	for i := 0; i < 4; i++ {
		if err := s.Install(ctx, installation(i)); err != nil {
			t.Fatal(err)
		}
	}
	stores := []*TenantStore{f.store(t, "installation-limit"), f.store(t, "installation-limit")}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, store := range stores {
		wg.Add(1)
		go func(i int, store *TenantStore) { defer wg.Done(); results <- store.Install(ctx, installation(i+4)) }(i, store)
	}
	wg.Wait()
	close(results)
	succeeded, rejected := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrConflict) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("sixth concurrent installation bypassed limit: succeeded=%d rejected=%d", succeeded, rejected)
	}
	duplicate := installation(0)
	duplicate.InstallationID = "duplicate-persona"
	if err := s.Install(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active persona installation = %v", err)
	}
}
