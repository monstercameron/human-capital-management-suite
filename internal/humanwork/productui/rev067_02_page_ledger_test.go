package productui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pageledger"
)

type memoryPageLedgerStore struct {
	revisions   map[string][]pageledger.RevisionRow
	rollouts    map[string][]pageledger.RolloutRow
	retirements map[string][]pageledger.RetirementRow
	fail        bool
}

func (s *memoryPageLedgerStore) PutRevision(_ context.Context, tenant string, page string, version int64, digest string, body []byte) error {
	if s.fail {
		return errors.New("injected write failure")
	}
	s.revisions[tenant] = append(s.revisions[tenant], pageledger.RevisionRow{Page: page, Version: version, Digest: digest, Payload: append([]byte(nil), body...)})
	return nil
}
func (s *memoryPageLedgerStore) LoadRevisions(_ context.Context, tenant string) ([]pageledger.RevisionRow, error) {
	return append([]pageledger.RevisionRow(nil), s.revisions[tenant]...), nil
}
func (s *memoryPageLedgerStore) PutRollout(_ context.Context, tenant string, page string, recordVersion int64, targetVersion int64, digest string, body []byte) error {
	if s.fail {
		return errors.New("injected write failure")
	}
	s.rollouts[tenant] = append(s.rollouts[tenant], pageledger.RolloutRow{Page: page, RecordVersion: recordVersion, TargetVersion: targetVersion, Digest: digest, Payload: append([]byte(nil), body...)})
	return nil
}
func (s *memoryPageLedgerStore) LoadRollouts(_ context.Context, tenant string) ([]pageledger.RolloutRow, error) {
	return append([]pageledger.RolloutRow(nil), s.rollouts[tenant]...), nil
}
func (s *memoryPageLedgerStore) PutRetirement(_ context.Context, tenant, page, digest string, body []byte) error {
	if s.fail {
		return errors.New("injected write failure")
	}
	s.retirements[tenant] = append(s.retirements[tenant], pageledger.RetirementRow{Page: page, Digest: digest, Payload: append([]byte(nil), body...)})
	return nil
}
func (s *memoryPageLedgerStore) LoadRetirements(_ context.Context, tenant string) ([]pageledger.RetirementRow, error) {
	return append([]pageledger.RetirementRow(nil), s.retirements[tenant]...), nil
}

func TestTodo_REV_067_02(t *testing.T) {
	ctx := context.Background()
	store := &memoryPageLedgerStore{revisions: map[string][]pageledger.RevisionRow{}, rollouts: map[string][]pageledger.RolloutRow{}, retirements: map[string][]pageledger.RetirementRow{}}
	log, err := NewDurablePageRevisionLog(ctx, "tenant-a", store)
	if err != nil {
		t.Fatal(err)
	}
	snap := PageDefinitionSnapshot{Page: "studio", Route: "/studio", Title: "Studio", SearchTerms: []string{"builder"}}
	revision, err := log.Record("studio", snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.revisions["tenant-a"]) != 1 || len(store.revisions["tenant-b"]) != 0 {
		t.Fatalf("revision tenant partition wrong: %+v", store.revisions)
	}
	if _, err := log.Record("studio", PageDefinitionSnapshot{Page: "studio", Title: "changed"}, 1); err == nil {
		t.Fatal("revision overwrite accepted")
	}
	if len(store.revisions["tenant-a"]) != 1 {
		t.Fatal("conflicting revision reached durable store")
	}
	rollout := PageRollout{Page: "studio", Version: 1, Digest: revision.Digest, Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 10}}}
	if err := log.RecordRollout(rollout); err != nil {
		t.Fatal(err)
	}
	if err := log.RecordRollout(rollout); err != nil {
		t.Fatalf("identical rollout not idempotent: %v", err)
	}
	changed := rollout
	changed.Scopes = []RolloutScope{{Scope: "org-b", EffectiveFrom: 10}}
	if err := log.RecordRollout(changed); err == nil {
		t.Fatal("same-key rollout mutation accepted")
	}
	if len(store.rollouts["tenant-a"]) != 1 {
		t.Fatal("rollout conflict reached durable store")
	}
	store.fail = true
	if _, err := log.Record("studio", snap, 2); err == nil {
		t.Fatal("storage failure did not fail closed")
	}
	if _, ok := log.Revision("studio", 2); ok {
		t.Fatal("failed durable write leaked into log")
	}
}

func TestTodo_REV_067_02_Golden(t *testing.T) {
	rollout := PageRollout{Page: "studio", Version: 7, Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 1700000000}, {Scope: "org-b", EffectiveFrom: 1700000300}}}
	body := MarshalPageRollout(rollout)
	parsed, err := ParsePageRollout(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, rollout) {
		t.Fatalf("rollout round trip = %#v, want %#v", parsed, rollout)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	const want = "5416010f7c36fd3d3837e7a7aed3584abcbf736d228038e3aaaeb275e9002250"
	if got != want {
		t.Fatalf("page rollout persisted-byte digest = %s, want %s (bytes %s)", got, want, body)
	}
	body[len(body)-3] ^= 1
	if _, err := ParsePageRollout(body); err == nil {
		t.Fatal("tampered rollout bytes accepted")
	}
}

func TestTodo_REV_067_02_Recovery(t *testing.T) {
	ctx := context.Background()
	store := &memoryPageLedgerStore{revisions: map[string][]pageledger.RevisionRow{}, rollouts: map[string][]pageledger.RolloutRow{}, retirements: map[string][]pageledger.RetirementRow{}}
	first, err := NewDurablePageRevisionLog(ctx, "tenant-a", store)
	if err != nil {
		t.Fatal(err)
	}
	snap := PageDefinitionSnapshot{Page: "studio", Route: "/studio", Title: "Studio", SearchTerms: []string{"pages"}}
	revision, err := first.Record("studio", snap, 3)
	if err != nil {
		t.Fatal(err)
	}
	rollout := PageRollout{Page: "studio", Version: 3, Digest: revision.Digest, Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 120}}}
	if err := first.RecordRollout(rollout); err != nil {
		t.Fatal(err)
	}
	rollout.RecordVersion = 1
	retirement := PageRetirement{Page: "studio", EffectiveFrom: 150, Reason: "scheduled closure"}
	if err := first.RecordRetirementContext(ctx, retirement); err != nil {
		t.Fatal(err)
	}
	second, err := NewDurablePageRevisionLog(ctx, "tenant-a", store)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := second.Revision("studio", 3)
	if !ok || !reflect.DeepEqual(got, revision) {
		t.Fatalf("recovered revision = %#v, %v", got, ok)
	}
	recovered := second.Rollouts()
	if len(recovered) != 1 || !reflect.DeepEqual(recovered[0], rollout) || !RolloutLiveAt(recovered[0], "org-a", 120) {
		t.Fatalf("recovered live rollout = %#v", recovered)
	}
	if retirements := second.Retirements(); len(retirements) != 1 || retirements[0] != retirement {
		t.Fatalf("recovered retirement = %#v", retirements)
	}
	store.revisions["tenant-a"][0].Payload[20] ^= 1
	if _, err := NewDurablePageRevisionLog(ctx, "tenant-a", store); err == nil {
		t.Fatal("tampered durable revision recovered")
	}
	if _, err := ParseRevision([]byte(`{"snapshot":{"page":"studio"},"version":1,"digest":"x","unexpected":true}`)); err == nil {
		t.Fatal("revision with unauthenticated extra fields was accepted")
	}
	store.revisions["tenant-a"] = []pageledger.RevisionRow{{Page: "studio", Version: 3, Digest: revision.Digest, Payload: MarshalRevision(revision)}}
	store.rollouts["tenant-a"][0].Payload[20] ^= 1
	if _, err := NewDurablePageRevisionLog(ctx, "tenant-a", store); err == nil {
		t.Fatal("tampered durable rollout recovered")
	}
	store.rollouts["tenant-a"][0].Payload = MarshalPageRollout(rollout)
	store.retirements["tenant-a"][0].Payload[20] ^= 1
	if _, err := NewDurablePageRevisionLog(ctx, "tenant-a", store); err == nil {
		t.Fatal("tampered durable retirement recovered")
	}
	if _, err := NewDurablePageRevisionLog(ctx, "tenant-b", store); err != nil {
		t.Fatal(err)
	}
}
