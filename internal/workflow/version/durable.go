package version

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TxBinder is implemented by a durable [Store] whose reads and writes must
// join a caller's open transaction instead of opening their own. A workflow
// start resolves its pinned version inside the start transaction, so the
// version it pins and the instance it creates are read and written under one
// snapshot (WF-COMP-006, WF-RUN-035).
type TxBinder interface {
	Store
	BindTx(ctx context.Context, ex dbport.Conn) Store
}

// BindTx returns store bound to ex when store is a [TxBinder], and store
// itself otherwise (the in-memory [Registry] has no transaction to join).
func BindTx(ctx context.Context, ex dbport.Conn, store Store) Store {
	if binder, ok := store.(TxBinder); ok && ex != nil {
		return binder.BindTx(ctx, ex)
	}
	return store
}

// Restore rebuilds a [CompiledVersion] read back from durable storage with
// the record digest minted at publication, and refuses it with
// [CodeRecordMutated] unless its content still digests to that value. It is
// the only way a value this package did not mint in-process gets a digest,
// so a tampered row can never be handed to a resolver as an intact version.
func Restore(v CompiledVersion, recordDigest string) (CompiledVersion, error) {
	restored := v.clone()
	restored.digest = recordDigest
	if err := restored.Verify(); err != nil {
		return CompiledVersion{}, err
	}
	return restored, nil
}
