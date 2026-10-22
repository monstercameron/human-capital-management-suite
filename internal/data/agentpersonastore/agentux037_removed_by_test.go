package agentpersonastore

import (
	"context"
	"testing"
	"time"
)

// TestTodo_AGENTUX_037_RemovedByIsRecorded: retiring an installation records the
// person who did it and when, the catalog reads both back, a placement that was
// replaced by a reinstall names no one, and a row cannot carry a remover while it
// is still active.
func TestTodo_AGENTUX_037_RemovedByIsRecorded(t *testing.T) {
	f := newFixture(t, "removed-by", "removed-by-other")
	s, other := f.store(t, "removed-by"), f.store(t, "removed-by-other")
	ctx := context.Background()
	v := createCatalogPersona(t, s, "removed-by", "assistant", StatePublished)
	place := func(id, room string) PersonaInstallation {
		return PersonaInstallation{TenantID: v.TenantID, InstallationID: id, PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: room, ConversationClass: ConversationPublic, InstallerID: "admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}
	}
	for _, installation := range []PersonaInstallation{place("in-a", "room-a"), place("in-b", "room-b")} {
		if err := s.Install(ctx, installation); err != nil {
			t.Fatal(err)
		}
	}
	before := time.Now().UTC().Add(-time.Minute)
	if _, changed, err := s.RetireActiveInstallation(ctx, v.PersonaID, "room-a", "wanda", "removed"); err != nil || !changed {
		t.Fatalf("retire changed=%t err=%v", changed, err)
	}
	// Reinstalling a different version replaces the placement; nobody removed it.
	replacement := place("in-b2", "room-b")
	replacement.ChannelPolicy.AllowedDataClasses = append([]string{"PUBLIC"}, replacement.ChannelPolicy.AllowedDataClasses...)
	if _, _, err := s.ReplaceActiveInstallation(ctx, replacement); err != nil {
		t.Logf("replacement was not applied in this fixture: %v", err)
	}
	catalog, err := s.ListCatalog(ctx)
	if err != nil || len(catalog) != 1 {
		t.Fatalf("catalog = %+v, %v", catalog, err)
	}
	seen := map[string]CatalogInstallation{}
	for _, in := range catalog[0].Installations {
		seen[in.ID] = in
	}
	removed := seen["in-a"]
	if removed.State != string(InstallationRetired) || removed.RemovedBy != "wanda" || removed.RemovedAt.Before(before) || removed.RemovedAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("removed placement = %+v", removed)
	}
	if live := seen["in-b"]; live.State != string(InstallationRetired) || live.RemovedBy != "" || !live.RemovedAt.IsZero() {
		t.Fatalf("a placement nobody removed names %q: %+v", live.RemovedBy, live)
	}
	if foreign, err := other.ListCatalog(ctx); err != nil || len(foreign) != 0 {
		t.Fatalf("another tenant reads the history: %+v %v", foreign, err)
	}
	if _, err := f.db.SQL.ExecContext(ctx, `UPDATE persona_installations SET retired_by='someone',retired_at=now() WHERE installation_id='in-b2' AND state='ACTIVE'`); err == nil {
		t.Fatal("an active placement was given a remover")
	}
}
