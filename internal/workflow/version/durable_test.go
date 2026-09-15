package version_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type bindingStore struct {
	*version.Registry
	bound bool
}

func (s *bindingStore) BindTx(context.Context, dbport.Conn) version.Store {
	s.bound = true
	return s.Registry
}

type nopConn struct{ dbport.Conn }

func TestRestoreVerifiesTheRecordDigest(t *testing.T) {
	published := publishPromotion(t, version.NewRegistry())

	restored, err := version.Restore(published, published.Digest())
	if err != nil || restored.Digest() != published.Digest() {
		t.Fatalf("Restore of an intact record = %v, %v", restored.Digest(), err)
	}
	tampered := published
	tampered.PublishedBy = "principal:attacker"
	if _, err := version.Restore(tampered, published.Digest()); version.CodeOf(err) != version.CodeRecordMutated {
		t.Fatalf("Restore of a tampered record = %v, want %s", err, version.CodeRecordMutated)
	}
	if _, err := version.Restore(published, "sha256:not-the-minted-digest"); version.CodeOf(err) != version.CodeRecordMutated {
		t.Fatalf("Restore under a foreign digest = %v, want %s", err, version.CodeRecordMutated)
	}
}

func TestBindTxOnlyBindsTxBinders(t *testing.T) {
	ctx := context.Background()
	registry := version.NewRegistry()
	if got := version.BindTx(ctx, nopConn{}, registry); got != version.Store(registry) {
		t.Fatal("BindTx wrapped a store with no transaction to join")
	}
	binder := &bindingStore{Registry: registry}
	if got := version.BindTx(ctx, nil, binder); got != version.Store(binder) || binder.bound {
		t.Fatal("BindTx bound a store to a nil executor")
	}
	if got := version.BindTx(ctx, nopConn{}, binder); got != version.Store(registry) || !binder.bound {
		t.Fatal("BindTx did not join a TxBinder to the caller's executor")
	}
}
