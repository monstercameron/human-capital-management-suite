package pageledger

import (
	"context"
	"testing"
)

type contractStore struct {
	tenant  string
	page    string
	version int64
	digest  string
	payload []byte
}

func (s *contractStore) PutRevision(_ context.Context, tenant, page string, version int64, digest string, payload []byte) error {
	s.tenant, s.page, s.version, s.digest, s.payload = tenant, page, version, digest, payload
	return nil
}
func (*contractStore) LoadRevisions(context.Context, string) ([]RevisionRow, error) { return nil, nil }
func (*contractStore) PutRollout(context.Context, string, string, int64, int64, string, []byte) error {
	return nil
}
func (*contractStore) LoadRollouts(context.Context, string) ([]RolloutRow, error) { return nil, nil }
func (*contractStore) PutRetirement(context.Context, string, string, string, []byte) error {
	return nil
}
func (*contractStore) LoadRetirements(context.Context, string) ([]RetirementRow, error) {
	return nil, nil
}

func TestTodo_REV_067_02_Port(t *testing.T) {
	var store Store = &contractStore{}
	want := []byte("opaque")
	if err := store.PutRevision(context.Background(), "tenant", "studio", 2, "digest", want); err != nil {
		t.Fatal(err)
	}
	got := store.(*contractStore)
	if got.tenant != "tenant" || got.page != "studio" || got.version != 2 || got.digest != "digest" || string(got.payload) != string(want) {
		t.Fatalf("storage port lost tenant key or payload: %+v", got)
	}
}
